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
