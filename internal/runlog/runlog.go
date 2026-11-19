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
}

// Open opens the state root's log file for appending, creating it and its
// directory private to the current user.
func Open(root string) (*File, error) {
	path := Path(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("runlog: cannot create %s: %w", filepath.Dir(path), err)
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
	if f.file == nil {
		return 0, os.ErrClosed
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

// LastFinished reports the newest run with an end record that skip does not
// pass over; a nil skip passes over nothing. The current file is read first
// and the newest rotated one after it, because a rotation can leave the
// current file holding nothing but the asking run's start.
func LastFinished(root string, skip func(Run) bool) (Run, bool) {
	if skip == nil {
		skip = func(Run) bool { return false }
	}
	for _, path := range []string{Path(root), Path(root) + ".1"} {
		if run, found := lastFinishedIn(path, skip); found {
			return run, true
		}
	}
	return Run{}, false
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
		last, found = run, true
	}
	return last, found
}
