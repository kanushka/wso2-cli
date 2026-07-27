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

// Command fakemodule is a scriptable stand-in for a product module, used by the
// shell's launcher tests.
//
// It is not a module: it links no SDK and speaks no contract. It replays bytes
// the test prepared, so a test can present protocol output that a conforming
// module could never produce — a truncated frame, silence, a flood of
// diagnostics, or a crash.
//
// It is driven by files beside its own executable rather than by arguments or
// environment variables, because the shell launches a module with neither: it
// passes no arguments and sanitizes the environment to nothing.
//
// The recognized files, all optional, are:
//
//	stdout.bin    bytes to write to standard output
//	stderr.bin    bytes to write to standard error, before standard output
//	dump-env      write one NAME=VALUE line per environment entry to standard
//	              error, so a test can prove the environment was sanitized
//	delay-ms      milliseconds to wait before writing standard output
//	linger-ms     milliseconds to stay alive after writing, or "forever"
//	exit-code     the process exit status
package main

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func main() {
	executable, err := os.Executable()
	if err != nil {
		os.Exit(90)
	}
	directory := filepath.Dir(executable)

	if _, err := os.Stat(filepath.Join(directory, "dump-env")); err == nil {
		for _, entry := range os.Environ() {
			os.Stderr.WriteString(entry + "\n")
		}
	}
	if diagnostics, err := os.ReadFile(filepath.Join(directory, "stderr.bin")); err == nil {
		os.Stderr.Write(diagnostics)
	}

	if delay, ok := readDuration(directory, "delay-ms"); ok {
		time.Sleep(delay)
	}
	if frames, err := os.ReadFile(filepath.Join(directory, "stdout.bin")); err == nil {
		os.Stdout.Write(frames)
	}

	// A module asked to linger keeps standard output open, so the shell stays
	// blocked reading and its deadline is what ends the invocation.
	if lingering(directory) {
		linger(directory)
		os.Exit(readExitCode(directory))
	}

	// Otherwise this module ends the exchange the way a conforming one does:
	// it closes standard output so the shell sees the stream end, then stays
	// alive until the shell closes its input in reply.
	//
	// Exiting straight after the write would instead race the shell's own
	// handshake, which would fail on a broken pipe against a peer no real
	// module resembles.
	_ = os.Stdout.Close()
	_, _ = io.Copy(io.Discard, os.Stdin)
	os.Exit(readExitCode(directory))
}

// lingering reports whether this module was asked to stay alive rather than
// end the exchange.
func lingering(directory string) bool {
	_, err := os.Stat(filepath.Join(directory, "linger-ms"))
	return err == nil
}

// linger keeps the process alive so a test can drive the shell's deadline and
// termination behaviour.
func linger(directory string) {
	raw, err := os.ReadFile(filepath.Join(directory, "linger-ms"))
	if err != nil {
		return
	}
	if strings.TrimSpace(string(raw)) == "forever" {
		// Sleeping rather than blocking on a channel, because the Go runtime
		// aborts a program whose every goroutine is asleep, which would make
		// this module exit instead of hang.
		for {
			time.Sleep(time.Hour)
		}
	}
	if duration, ok := parseDuration(string(raw)); ok {
		time.Sleep(duration)
	}
}

func readExitCode(directory string) int {
	raw, err := os.ReadFile(filepath.Join(directory, "exit-code"))
	if err != nil {
		return 0
	}
	code, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return 0
	}
	return code
}

func readDuration(directory, name string) (time.Duration, bool) {
	raw, err := os.ReadFile(filepath.Join(directory, name))
	if err != nil {
		return 0, false
	}
	return parseDuration(string(raw))
}

func parseDuration(raw string) (time.Duration, bool) {
	milliseconds, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, false
	}
	return time.Duration(milliseconds) * time.Millisecond, true
}
