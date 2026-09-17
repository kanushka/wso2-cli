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

package wizard

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestFormSelectMovesWithTheArrowKeys(t *testing.T) {
	var out bytes.Buffer
	p := formPrompter{in: terminal(t, "\x1b[B\r"), out: &out}
	picked, err := p.Select("Q", []Option{{Label: "a"}, {Label: "b"}}, 0)
	if err != nil || picked != 1 {
		t.Fatalf("Select = %d, %v; want 1", picked, err)
	}
}

func TestFormSelectRefusesAnUnavailableOption(t *testing.T) {
	var out bytes.Buffer
	// Enter on the unavailable first option is refused; down then enter picks.
	p := formPrompter{in: terminal(t, "\r\x1b[B\r"), out: &out}
	picked, err := p.Select("Q", []Option{{Label: "cloud", Unavailable: "soon"}, {Label: "local"}}, 0)
	if err != nil || picked != 1 {
		t.Fatalf("Select = %d, %v; want 1", picked, err)
	}
}

func TestFormInputTakesTheDefaultAndChecksTyping(t *testing.T) {
	var out bytes.Buffer
	p := formPrompter{in: terminal(t, "\r"), out: &out}
	if answer, err := p.Input("Name", "context-1", nil); err != nil || answer != "context-1" {
		t.Fatalf("Input = %q, %v", answer, err)
	}
	p = formPrompter{in: terminal(t, "\rok\r"), out: &out}
	answer, err := p.Input("Name", "", nil)
	if err != nil || answer != "ok" {
		t.Fatalf("Input = %q, %v", answer, err)
	}
}

func TestFormCancelIsReportedAsAborted(t *testing.T) {
	var out bytes.Buffer
	p := formPrompter{in: terminal(t, "\x03"), out: &out}
	if _, err := p.Confirm("Go?", true); err != ErrAborted {
		t.Fatalf("Confirm err = %v, want ErrAborted", err)
	}
}

func TestFormConfirmTakesTheDefault(t *testing.T) {
	var out bytes.Buffer
	p := formPrompter{in: terminal(t, "\r"), out: &out}
	if yes, err := p.Confirm("Go?", true); err != nil || !yes {
		t.Fatalf("Confirm = %v, %v", yes, err)
	}
}

// terminal is input that carries keys and then stays open, as a terminal
// does: huh ends its program when its input ends.
func terminal(t *testing.T, keys string) io.Reader {
	t.Helper()
	reader, writer := io.Pipe()
	go func() { _, _ = writer.Write([]byte(keys)) }()
	t.Cleanup(func() { _ = writer.Close() })
	return reader
}

func TestOnlyASizedTerminalIsDrawable(t *testing.T) {
	var out bytes.Buffer
	if Drawable(&out) {
		t.Error("a buffer is drawable")
	}
}

// fdWriter is an io.Writer that also reports a file descriptor, the way
// *os.File does, without being a terminal. It exercises the branch of
// Drawable and width that gets as far as asking the descriptor's size,
// which term.GetSize refuses for a plain pipe the way it would for
// anything that is not a terminal device.
type fdWriter struct {
	io.Writer
	fd uintptr
}

func (f fdWriter) Fd() uintptr { return f.fd }

func TestAPipeReportsAFdButIsNotDrawable(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()
	out := fdWriter{Writer: w, fd: w.Fd()}
	if Drawable(out) {
		t.Error("a plain pipe is drawable")
	}
}

func TestWidthFallsBackWhenTheDescriptorIsNotATerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()
	p := formPrompter{out: fdWriter{Writer: w, fd: w.Fd()}}
	if got := p.width(); got != defaultWidth {
		t.Errorf("width() = %d, want the default %d", got, defaultWidth)
	}
}
