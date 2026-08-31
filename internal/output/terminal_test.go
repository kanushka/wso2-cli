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

package output

import (
	"bytes"
	"os"
	"testing"
)

// fdWriter stands in for a writer that reports a file descriptor without
// being backed by a real one, so a test can hand IsTerminal a descriptor of
// its choosing.
type fdWriter struct {
	fd uintptr
}

func (fdWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w fdWriter) Fd() uintptr               { return w.fd }

// panicIfAskedWriter's Fd panics if it is ever called. It proves that a code
// path never reaches the platform terminal call, rather than merely reaching
// it and getting a "no" answer that a mutant could also produce by accident.
type panicIfAskedWriter struct{}

func (panicIfAskedWriter) Write(p []byte) (int, error) { return len(p), nil }
func (panicIfAskedWriter) Fd() uintptr {
	panic("Fd was called; NO_COLOR should have short-circuited before this")
}

// recordingFDWriter notes whether Fd was ever called, so a test can prove a
// code path DID reach the platform terminal call, the opposite proof from
// panicIfAskedWriter.
type recordingFDWriter struct {
	fd     uintptr
	called *bool
}

func (recordingFDWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w recordingFDWriter) Fd() uintptr {
	*w.called = true
	return w.fd
}

// TestABufferIsNotATerminal pins the package's default: a writer with no
// Fd method — every *bytes.Buffer this repository's tests hand around — is
// never mistaken for a terminal. Removing the type assertion in IsTerminal
// (so the code always tried the platform call) would fail to compile, and
// making IsTerminal unconditionally return true would fail this test.
func TestABufferIsNotATerminal(t *testing.T) {
	var buffer bytes.Buffer
	if IsTerminal(&buffer) {
		t.Error("a *bytes.Buffer was reported as a terminal")
	}
}

// TestAPipeIsNotATerminal exercises the real platform call against a
// descriptor that does implement Fd but names a pipe, not a terminal. A pipe
// is a case the old os.ModeCharDevice check also got right — it is not a
// character device, so that check already said "no" here too. What this
// test proves instead is that the new ioctl/GetConsoleMode-based check gives
// the same right answer for a real, open, valid, non-terminal descriptor,
// which a test against only a *bytes.Buffer (no descriptor at all) or a
// nonsense one (no open descriptor) cannot show.
func TestAPipeIsNotATerminal(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer func() { _ = read.Close() }()
	defer func() { _ = write.Close() }()

	if IsTerminal(write) {
		t.Error("a pipe was reported as a terminal")
	}
}

// TestANonsenseDescriptorDoesNotPanic pins that a Fd() value with no
// corresponding open descriptor is answered "not a terminal" rather than
// crashing the process. A confirmation prompt or a progress indicator calls
// this on every invocation, so a bad descriptor must be a "no", not a panic.
func TestANonsenseDescriptorDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("IsTerminal panicked on a nonsense descriptor: %v", r)
		}
	}()
	if IsTerminal(fdWriter{fd: ^uintptr(0)}) {
		t.Error("a nonsense descriptor was reported as a terminal")
	}
}

// TestNoColorSetEmptyDoesNotDisableColor pins the no-color.org spec's own
// distinction: the variable must be "present and not an empty string" to
// disable color, so NO_COLOR= (present, empty) leaves the decision to
// IsTerminal rather than forcing color off. This is the opposite of
// WSO2_NO_INPUT's emptiness test — deliberately, since NO_COLOR is a
// cross-tool convention this shell does not get to redefine.
//
// It uses recordingFDWriter, not a plain fdWriter, so the test proves
// ColorEnabled actually reached IsTerminal (Fd was called) rather than
// merely landing on "false" by coincidence — a nonsense descriptor is not a
// terminal, so a mutant that always disabled color on any NO_COLOR value
// would also return false here without ever consulting IsTerminal.
func TestNoColorSetEmptyDoesNotDisableColor(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	var called bool
	if ColorEnabled(recordingFDWriter{fd: ^uintptr(0), called: &called}) {
		t.Error("a nonsense descriptor was reported as a terminal")
	}
	if !called {
		t.Error("ColorEnabled did not consult IsTerminal for NO_COLOR set to empty")
	}
}

// TestNoColorSetNonEmptyDisablesColor pins the other half of the spec: a
// non-empty value disables color outright. It uses panicIfAskedWriter so the
// test proves IsTerminal is never consulted, not just that the final answer
// happens to be false.
func TestNoColorSetNonEmptyDisablesColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if ColorEnabled(panicIfAskedWriter{}) {
		t.Error("NO_COLOR set to a non-empty value did not disable color")
	}
}

// TestNoColorUnsetFallsBackToTerminalDetection pins that, absent NO_COLOR,
// ColorEnabled defers to IsTerminal rather than defaulting color on. A
// fdWriter with a nonsense descriptor is not a terminal on every platform
// this call reaches, so this also doubles as a second nonsense-descriptor
// check via the ColorEnabled path.
func TestNoColorUnsetFallsBackToTerminalDetection(t *testing.T) {
	if previous, wasSet := os.LookupEnv("NO_COLOR"); wasSet {
		if err := os.Unsetenv("NO_COLOR"); err != nil {
			t.Fatalf("unset NO_COLOR: %v", err)
		}
		t.Cleanup(func() {
			if err := os.Setenv("NO_COLOR", previous); err != nil {
				t.Errorf("restore NO_COLOR: %v", err)
			}
		})
	}
	if ColorEnabled(fdWriter{fd: ^uintptr(0)}) {
		t.Error("a non-terminal writer had color enabled with NO_COLOR unset")
	}
}

// This package cannot test that a real terminal is reported as one: nothing
// in this suite runs attached to a pty, and CI does not provide one. The
// platform call is exercised only against writers this test can prove are
// NOT terminals (a buffer, a pipe, a nonsense descriptor). The "yes" branch
// of isTerminal is therefore unverified by any automated test in this
// repository — no manual test is recorded anywhere either — and stays that
// way until something (a pty-backed test harness, or a documented manual
// check) actually exercises it.
