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

package app

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/output"
)

// atATerminal makes wso2 completion <shell> believe a person is reading.
func atATerminal(t *testing.T) {
	t.Helper()
	previous := completionStdoutIsTerminal
	completionStdoutIsTerminal = func(io.Writer) bool { return true }
	t.Cleanup(func() { completionStdoutIsTerminal = previous })
}

func runCompletion(t *testing.T, args ...string) string {
	t.Helper()
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	shell := Shell{StateRoot: t.TempDir(), Streams: output.Streams{Out: out, Err: errOut}}
	if code := shell.Run(args); code != exit.OK {
		t.Fatalf("%q exited %d; stderr: %s", args, code, errOut)
	}
	return out.String()
}

func TestACompletionScriptAtATerminalIsAHint(t *testing.T) {
	atATerminal(t)
	got := runCompletion(t, "completion", "zsh")
	if strings.Contains(got, "#compdef") {
		t.Fatalf("the script was printed to a terminal:\n%.200s", got)
	}
	for _, want := range []string{"wso2 completion install", "wso2 completion zsh --print"} {
		if !strings.Contains(got, want) {
			t.Errorf("the hint does not name %q:\n%s", want, got)
		}
	}
}

func TestPrintWritesTheScriptToATerminal(t *testing.T) {
	atATerminal(t)
	if got := runCompletion(t, "completion", "zsh", "--print"); !strings.HasPrefix(got, "#compdef wso2") {
		t.Fatalf("--print did not write the script:\n%.200s", got)
	}
}

// A script sourced through a pipe has to be exactly what Cobra generates.
func TestAPipedCompletionScriptIsWhatCobraGenerates(t *testing.T) {
	for _, shell := range completionShells {
		t.Run(shell, func(t *testing.T) {
			var want bytes.Buffer
			root := Shell{Streams: output.Streams{Out: &want, Err: io.Discard}}.rootCommand()
			var err error
			switch shell {
			case shellBash:
				err = root.GenBashCompletionV2(&want, true)
			case shellZsh:
				err = root.GenZshCompletion(&want)
			case shellFish:
				err = root.GenFishCompletion(&want, true)
			case shellPowerShell:
				err = root.GenPowerShellCompletionWithDesc(&want)
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := runCompletion(t, "completion", shell); got != want.String() {
				t.Fatalf("the piped %s script differs from Cobra's", shell)
			}
		})
	}
}

func TestCompletionLinesGoInsideTheWso2Block(t *testing.T) {
	lines := []string{"first", "load"}
	installed := "# existing\n\n" + profileBlockBegin + "\nexport PATH=\"/bin:$PATH\"\n" + profileBlockEnd + "\n# after\n"
	for _, test := range []struct {
		name     string
		contents string
		want     string
		changed  bool
	}{
		{"empty profile", "", "\n" + profileBlockBegin + "\nfirst\nload\n" + profileBlockEnd + "\n", true},
		{"no trailing newline", "# mine", "# mine\n\n" + profileBlockBegin + "\nfirst\nload\n" + profileBlockEnd + "\n", true},
		{"installer block", installed,
			"# existing\n\n" + profileBlockBegin + "\nexport PATH=\"/bin:$PATH\"\nfirst\nload\n" + profileBlockEnd + "\n# after\n", true},
		{"already set up", "# x\n" + profileBlockBegin + "\nfirst\nload\n" + profileBlockEnd + "\n",
			"# x\n" + profileBlockBegin + "\nfirst\nload\n" + profileBlockEnd + "\n", false},
		{"windows line endings", "# x\r\n" + profileBlockBegin + "\r\n" + profileBlockEnd + "\r\n",
			"# x\r\n" + profileBlockBegin + "\r\nfirst\r\nload\r\n" + profileBlockEnd + "\r\n", true},
		{"a lost line is put back before the load line", profileBlockBegin + "\nexport X=1\nload\n" + profileBlockEnd + "\n",
			profileBlockBegin + "\nexport X=1\nfirst\nload\n" + profileBlockEnd + "\n", true},
		{"load line outside the block does not count", "load\n",
			"load\n\n" + profileBlockBegin + "\nfirst\nload\n" + profileBlockEnd + "\n", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, changed, err := withCompletionLines(test.contents, lines, shellZsh, "profile")
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want || changed != test.changed {
				t.Errorf("got changed=%v\n%q\nwant changed=%v\n%q", changed, got, test.changed, test.want)
			}
		})
	}
}

func TestABlockWithNoEndIsLeftAlone(t *testing.T) {
	_, _, err := withCompletionLines(profileBlockBegin+"\nexport X=1\n", []string{"load"}, shellZsh, "profile")
	if err == nil || !strings.Contains(err.Error(), profileBlockEnd) {
		t.Fatalf("err = %v, want a refusal naming the end marker", err)
	}
}
