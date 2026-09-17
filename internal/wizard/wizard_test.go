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

package wizard_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/wizard"
)

func linePrompter(input string) (wizard.Prompter, *bytes.Buffer) {
	var out bytes.Buffer
	return wizard.New(strings.NewReader(input), &out, false), &out
}

func TestSelectAsksAgainUntilAnAvailableOptionIsPicked(t *testing.T) {
	prompter, out := linePrompter("9\nx\n1\n2\n")
	picked, err := prompter.Select("Deployment:", []wizard.Option{
		{Label: "Cloud", Unavailable: "Cloud is coming soon."},
		{Label: "Local"},
	}, 1)
	if err != nil || picked != 1 {
		t.Fatalf("Select = %d, %v; want 1", picked, err)
	}
	for _, want := range []string{"Deployment:\n  1. Cloud\n  2. Local\n", "Enter a number from 1 to 2.", "Cloud is coming soon."} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if got := strings.Count(out.String(), "Choose [2]: "); got != 4 {
		t.Errorf("asked %d times, want 4", got)
	}
}

func TestSelectTakesTheFallbackOnReturnAndAtEndOfInput(t *testing.T) {
	for _, input := range []string{"\n", ""} {
		prompter, _ := linePrompter(input)
		picked, err := prompter.Select("Q", []wizard.Option{{Label: "a"}, {Label: "b"}}, 1)
		if err != nil || picked != 1 {
			t.Errorf("input %q: Select = %d, %v; want the fallback 1", input, picked, err)
		}
	}
}

func TestConsecutiveQuestionsEachGetTheirOwnLine(t *testing.T) {
	prompter, _ := linePrompter("2\nhttps://example.com\ny\n")
	if picked, _ := prompter.Select("Q", []wizard.Option{{Label: "a"}, {Label: "b"}}, 0); picked != 1 {
		t.Fatalf("Select = %d", picked)
	}
	if answer, _ := prompter.Input("URL", "", nil); answer != "https://example.com" {
		t.Fatalf("Input = %q", answer)
	}
	if yes, _ := prompter.Confirm("Go?", false); !yes {
		t.Fatal("Confirm = false")
	}
}

func TestInputAsksAgainWithTheRefusalShown(t *testing.T) {
	prompter, out := linePrompter("\nbad\ngood\n")
	answer, err := prompter.Input("Name", "", func(value string) error {
		if value != "good" {
			return errors.New("not good")
		}
		return nil
	})
	if err != nil || answer != "good" {
		t.Fatalf("Input = %q, %v", answer, err)
	}
	if got := strings.Count(out.String(), "not good"); got != 2 {
		t.Errorf("refused %d times, want 2:\n%s", got, out)
	}
	if got := strings.Count(out.String(), "Name: "); got != 3 {
		t.Errorf("asked %d times, want 3", got)
	}
}

func TestInputFallsBackAndShowsTheDefault(t *testing.T) {
	prompter, out := linePrompter("\n")
	answer, err := prompter.Input("Context name", "context-1", nil)
	if err != nil || answer != "context-1" {
		t.Fatalf("Input = %q, %v", answer, err)
	}
	if !strings.Contains(out.String(), "Context name [context-1]: ") {
		t.Errorf("default not shown:\n%s", out)
	}
}

func TestInputAtEndOfInput(t *testing.T) {
	prompter, _ := linePrompter("")
	if answer, err := prompter.Input("Name", "dflt", nil); err != nil || answer != "dflt" {
		t.Errorf("with a default: %q, %v", answer, err)
	}
	prompter, _ = linePrompter("")
	if _, err := prompter.Input("Name", "", nil); !errors.Is(err, wizard.ErrNoAnswer) {
		t.Errorf("without a default: %v, want ErrNoAnswer", err)
	}
	prompter, _ = linePrompter("half")
	if answer, err := prompter.Input("Name", "", nil); err != nil || answer != "half" {
		t.Errorf("an unterminated last line: %q, %v", answer, err)
	}
}

// errWriter fails its first write with err, and succeeds every write after
// (recording it in Buffer), so a test can make exactly one of a prompter's
// several writes fail without wedging the ones that come after it.
type errWriter struct {
	bytes.Buffer
	err     error
	failed  bool
	failAll bool
}

func (w *errWriter) Write(p []byte) (int, error) {
	if w.err != nil && (w.failAll || !w.failed) {
		w.failed = true
		return 0, w.err
	}
	return w.Buffer.Write(p)
}

var errWrite = errors.New("write failed")

// errReader always fails its Read with a non-EOF error, the way a broken
// terminal would.
type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

var errRead = errors.New("read failed")

func TestSelectReportsAWriteFailureOnTheTitle(t *testing.T) {
	out := &errWriter{err: errWrite, failAll: true}
	prompter := wizard.New(strings.NewReader("1\n"), out, false)
	if _, err := prompter.Select("Q", []wizard.Option{{Label: "a"}}, 0); !errors.Is(err, errWrite) {
		t.Fatalf("Select err = %v, want %v", err, errWrite)
	}
}

func TestSelectReportsAWriteFailureListingOptions(t *testing.T) {
	// The title write succeeds; the first option line fails.
	counting := &countingErrWriter{failAt: 2, err: errWrite}
	prompter := wizard.New(strings.NewReader("1\n"), counting, false)
	if _, err := prompter.Select("Q", []wizard.Option{{Label: "a"}}, 0); !errors.Is(err, errWrite) {
		t.Fatalf("Select err = %v, want %v", err, errWrite)
	}
}

func TestSelectReportsAWriteFailureOnThePrompt(t *testing.T) {
	counting := &countingErrWriter{failAt: 3, err: errWrite}
	prompter := wizard.New(strings.NewReader("1\n"), counting, false)
	if _, err := prompter.Select("Q", []wizard.Option{{Label: "a"}}, 0); !errors.Is(err, errWrite) {
		t.Fatalf("Select err = %v, want %v", err, errWrite)
	}
}

func TestSelectReportsAReadFailure(t *testing.T) {
	prompter := wizard.New(errReader{errRead}, &bytes.Buffer{}, false)
	if _, err := prompter.Select("Q", []wizard.Option{{Label: "a"}}, 0); !errors.Is(err, errRead) {
		t.Fatalf("Select err = %v, want %v", err, errRead)
	}
}

func TestSelectReportsAWriteFailureOnTheRefusal(t *testing.T) {
	// title, option line, prompt, then the refusal line fails.
	counting := &countingErrWriter{failAt: 4, err: errWrite}
	prompter := wizard.New(strings.NewReader("9\n"), counting, false)
	if _, err := prompter.Select("Q", []wizard.Option{{Label: "a"}}, 0); !errors.Is(err, errWrite) {
		t.Fatalf("Select err = %v, want %v", err, errWrite)
	}
}

func TestInputReportsAWriteFailureOnThePrompt(t *testing.T) {
	counting := &countingErrWriter{failAt: 1, err: errWrite}
	prompter := wizard.New(strings.NewReader("x\n"), counting, false)
	if _, err := prompter.Input("Name", "", nil); !errors.Is(err, errWrite) {
		t.Fatalf("Input err = %v, want %v", err, errWrite)
	}
}

func TestInputReportsAReadFailure(t *testing.T) {
	prompter := wizard.New(errReader{errRead}, &bytes.Buffer{}, false)
	if _, err := prompter.Input("Name", "", nil); !errors.Is(err, errRead) {
		t.Fatalf("Input err = %v, want %v", err, errRead)
	}
}

func TestInputReportsAWriteFailureOnTheRefusal(t *testing.T) {
	// prompt, then the refusal line fails.
	counting := &countingErrWriter{failAt: 2, err: errWrite}
	prompter := wizard.New(strings.NewReader("\n"), counting, false)
	if _, err := prompter.Input("Name", "", nil); !errors.Is(err, errWrite) {
		t.Fatalf("Input err = %v, want %v", err, errWrite)
	}
}

func TestConfirmReportsAWriteFailure(t *testing.T) {
	counting := &countingErrWriter{failAt: 1, err: errWrite}
	prompter := wizard.New(strings.NewReader("y\n"), counting, false)
	if _, err := prompter.Confirm("Go?", false); !errors.Is(err, errWrite) {
		t.Fatalf("Confirm err = %v, want %v", err, errWrite)
	}
}

func TestConfirmReportsAReadFailure(t *testing.T) {
	prompter := wizard.New(errReader{errRead}, &bytes.Buffer{}, false)
	if _, err := prompter.Confirm("Go?", false); !errors.Is(err, errRead) {
		t.Fatalf("Confirm err = %v, want %v", err, errRead)
	}
}

// countingErrWriter fails its failAt'th Write call and no other.
type countingErrWriter struct {
	bytes.Buffer
	count  int
	failAt int
	err    error
}

func (w *countingErrWriter) Write(p []byte) (int, error) {
	w.count++
	if w.count == w.failAt {
		return 0, w.err
	}
	return w.Buffer.Write(p)
}

func TestNewDrawsAFormWhenTUIIsTrue(t *testing.T) {
	// A form prompter reads keystrokes rather than lines: an immediate
	// Ctrl+C aborts it the way it aborts any other form question, which a
	// line prompter would instead read as ordinary (and empty) input.
	var out bytes.Buffer
	prompter := wizard.New(strings.NewReader("\x03"), &out, true)
	if _, err := prompter.Confirm("Go?", true); err != wizard.ErrAborted {
		t.Fatalf("Confirm err = %v, want ErrAborted", err)
	}
}

func TestConfirmFailsClosed(t *testing.T) {
	cases := map[string]bool{"y\n": true, " YES \n": true, "n\n": false, "yep\n": false, "\n": false, "": false}
	for input, want := range cases {
		prompter, _ := linePrompter(input)
		if got, err := prompter.Confirm("Go?", false); err != nil || got != want {
			t.Errorf("Confirm(%q) = %v, %v; want %v", input, got, err, want)
		}
	}
	prompter, out := linePrompter("\n")
	if got, _ := prompter.Confirm("Log in now?", true); !got {
		t.Error("an empty answer did not take the yes default")
	}
	if !strings.Contains(out.String(), "Log in now? [Y/n] ") {
		t.Errorf("hint not shown:\n%s", out)
	}
}
