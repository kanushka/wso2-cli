// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

// Package runlog is the file in the state root that outlives a run: a start
// and an end record for every run, and the run's diagnostics when they were
// asked for. See docs/adr/0026-debug-logging-from-the-environment-and-a-run-log.md.
//
// It is a privacy surface, which is why it is small, private to its owner, and
// fed only through the same redaction as standard error.
package runlog

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const (
	// MaxSize is the size a log file rotates at. A run's start and end
	// records are about 300 bytes, so this holds thousands of runs, or a
	// handful of debug runs.
	MaxSize = 1 << 20
	// Kept is how many rotated files are kept beside the current one.
	Kept = 2
)

// The record messages and attribute keys other packages read back.
const (
	StartedMessage = "a run started"
	EndedMessage   = "a run ended"

	KeyInvocation = "invocation"
	KeyCommand    = "command"
	KeyExitCode   = "exit_code"
	KeyProblem    = "problem"
)

// Path reports the current log file of a state root.
func Path(root string) string {
	return filepath.Join(root, "cli", "logs", "wso2.log")
}

// File is the current log file, rotating as it fills.
//
// Rotation is best effort across processes: two runs crossing the cap at the
// same moment can each rotate, and the lines one of them wrote in between
// land in a file that is then shifted out early. A diagnostic log is not
// worth a lock every run would have to take.
type File struct {
	mutex sync.Mutex
	path  string
	file  *os.File
	size  int64
	// err is the first write that failed. slog drops a handler's error, so
	// this is the only place a log that stopped being written is remembered.
	err error
}

// Open opens the state root's log file for appending, creating it and its
// directory private to the current user.
func Open(root string) (*File, error) {
	path := Path(root)
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("runlog: cannot create %s: %w", directory, err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return nil, fmt.Errorf("runlog: cannot restrict %s: %w", directory, err)
	}
	log := &File{path: path}
	if err := log.open(); err != nil {
		return nil, err
	}
	return log, nil
}

func (f *File) open() error {
	file, err := os.OpenFile(f.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("runlog: cannot open %s: %w", f.path, err)
	}
	// OpenFile's mode applies only to a file it creates; one left by an
	// older shell or copied in keeps its own unless narrowed here.
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("runlog: cannot restrict %s: %w", f.path, err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("runlog: cannot read %s: %w", f.path, err)
	}
	f.file, f.size = file, info.Size()
	return nil
}

// Write appends one record, rotating first when it would carry the file past
// MaxSize.
func (f *File) Write(record []byte) (int, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	written, err := f.write(record)
	if err != nil && f.err == nil {
		f.err = err
	}
	return written, err
}

func (f *File) write(record []byte) (int, error) {
	if f.file == nil {
		return 0, os.ErrClosed
	}
	// Other runs append to the same file, so the size read at open is stale.
	if info, err := f.file.Stat(); err == nil {
		f.size = info.Size()
	}
	if f.size > 0 && f.size+int64(len(record)) > MaxSize {
		if err := f.rotate(); err != nil {
			return 0, err
		}
	}
	written, err := f.file.Write(record)
	f.size += int64(written)
	return written, err
}

// Err reports the first write that failed, or nil.
func (f *File) Err() error {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return f.err
}

func (f *File) rotate() error {
	if err := f.file.Close(); err != nil {
		return err
	}
	f.file = nil
	_ = os.Remove(f.path + "." + strconv.Itoa(Kept))
	for index := Kept - 1; index >= 1; index-- {
		_ = os.Rename(f.path+"."+strconv.Itoa(index), f.path+"."+strconv.Itoa(index+1))
	}
	// A rename can fail where another process holds the file open; the run
	// goes on appending to it rather than losing its records.
	_ = os.Rename(f.path, f.path+".1")
	return f.open()
}

// Close closes the file.
func (f *File) Close() error {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if f.file == nil {
		return nil
	}
	err := f.file.Close()
	f.file = nil
	return err
}

// Run is what an end record says about one run.
type Run struct {
	Time       time.Time
	Invocation string
	Command    string
	ExitCode   int
	Problem    string
}

// LastFinished reports the run with the latest end record that skip does not
// pass over, across the current file and every rotated one; a nil skip passes
// over nothing. Every file is read because a run that opened the log before
// another rotated it goes on appending to the older file, so the newest finish
// is not always in the current one.
func LastFinished(root string, skip func(Run) bool) (Run, bool) {
	if skip == nil {
		skip = func(Run) bool { return false }
	}
	var (
		latest Run
		found  bool
	)
	for index := 0; index <= Kept; index++ {
		path := Path(root)
		if index > 0 {
			path += "." + strconv.Itoa(index)
		}
		if run, ok := lastFinishedIn(path, skip); ok && (!found || run.Time.After(latest.Time)) {
			latest, found = run, true
		}
	}
	return latest, found
}

func lastFinishedIn(path string, skip func(Run) bool) (Run, bool) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Run{}, false
	}
	var (
		last  Run
		found bool
	)
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64<<10), MaxSize)
	for scanner.Scan() {
		var record struct {
			Time       time.Time `json:"time"`
			Message    string    `json:"msg"`
			Invocation string    `json:"invocation"`
			Command    string    `json:"command"`
			ExitCode   int       `json:"exit_code"`
			Problem    string    `json:"problem"`
		}
		if json.Unmarshal(scanner.Bytes(), &record) != nil || record.Message != EndedMessage {
			continue
		}
		run := Run{Time: record.Time, Invocation: record.Invocation, Command: record.Command,
			ExitCode: record.ExitCode, Problem: record.Problem}
		if skip(run) {
			continue
		}
		// Runs finish out of order in a shared file; on a tie the record
		// written later wins.
		if !found || !run.Time.Before(last.Time) {
			last, found = run, true
		}
	}
	return last, found
}
