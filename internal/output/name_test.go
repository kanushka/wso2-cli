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
	"encoding/json"
	"os"
	"testing"

	"github.com/wso2/wso2-cli/sdk/result"
)

func TestRenameNamesCommandsAfterTheInvokedShell(t *testing.T) {
	cases := map[string]string{
		"Run wso2 login.":                                      "Run ws login.",
		"Run wso2 help to see the shell commands.":             "Run ws help to see the shell commands.",
		"Run wso2 context list, or wso2 context apply -f <f>.": "Run ws context list, or ws context apply -f <f>.",
		"Run wso2 product install reference to install it.":    "Run ws product install reference to install it.",
		`"product list" is not a command; try "wso2 help".`:    `"product list" is not a command; try "ws help".`,
		"Did you mean wso2 reference status?":                  "Did you mean ws reference status?",
		"Nothing named wso2 is installed.":                     "Nothing named wso2 is installed.",
		"Build wso2-cli from ~/dev/wso2/wso2-cli first.":       "Build wso2-cli from ~/dev/wso2/wso2-cli first.",
		"Set WSO2_HOME before running the WSO2 CLI.":           "Set WSO2_HOME before running the WSO2 CLI.",
		"wso2 version takes no arguments":                      "ws version takes no arguments",
		"Valid keys: output, catalog-origin.":                  "Valid keys: output, catalog-origin.",
	}
	for text, want := range cases {
		if got := Rename(text, "ws"); got != want {
			t.Errorf("Rename(%q)\n got  %q\n want %q", text, got, want)
		}
	}
}

func TestRenameLeavesTextAloneForTheDefaultName(t *testing.T) {
	const text = "Run wso2 login."
	for _, name := range []string{"", DefaultName} {
		if got := Rename(text, name); got != text {
			t.Errorf("Rename(%q, %q) = %q, want it unchanged", text, name, got)
		}
	}
}

func TestNamedWriterCarriesTheNameAndPassesWritesThrough(t *testing.T) {
	var buffer bytes.Buffer
	w := Named(&buffer, "ws")
	if got := NameOf(w); got != "ws" {
		t.Errorf("NameOf(Named(w, ws)) = %q, want ws", got)
	}
	if got := NameOf(&buffer); got != DefaultName {
		t.Errorf("NameOf(plain writer) = %q, want %q", got, DefaultName)
	}
	if _, err := w.Write([]byte("wso2 login")); err != nil {
		t.Fatal(err)
	}
	// The writer never rewrites bytes: only the renderers rename, and only prose.
	if buffer.String() != "wso2 login" {
		t.Errorf("wrote %q, want the bytes unchanged", buffer.String())
	}
	if Named(&buffer, DefaultName) != any(&buffer) {
		t.Error("Named with the default name should return the writer itself")
	}
}

func TestNamedWriterKeepsTheFileDescriptor(t *testing.T) {
	w, ok := Named(os.Stdout, "ws").(interface{ Fd() uintptr })
	if !ok {
		t.Fatal("Named(os.Stdout) hides the file descriptor, so terminal detection would fail")
	}
	if w.Fd() != os.Stdout.Fd() {
		t.Errorf("Fd() = %d, want %d", w.Fd(), os.Stdout.Fd())
	}
}

func TestHintMarksCommandsUnderTheInvokedName(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "")
	w := Named(&bytes.Buffer{}, "ws")
	if got, want := Hint(w, "Run wso2 login --context demo."), "Run `ws login --context demo`."; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestProblemAndNextStepUseTheInvokedName(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "")
	var buffer bytes.Buffer
	w := Named(&buffer, "ws")
	if err := NextStep(w, "Run wso2 whoami."); err != nil {
		t.Fatal(err)
	}
	if got, want := buffer.String(), "\nNext  Run `ws whoami`.\n"; got != want {
		t.Errorf("NextStep wrote %q, want %q", got, want)
	}
}

func TestJSONRenamesOnlyTheNextField(t *testing.T) {
	produced := result.New("reference.status/v1").
		With("service", "Service", "wso2 gateway").
		With(NextField, "Next", "Run wso2 whoami.")
	var buffer bytes.Buffer
	if err := Result(Named(&buffer, "ws"), ModeJSON, produced); err != nil {
		t.Fatal(err)
	}
	var document map[string]string
	if err := json.Unmarshal(buffer.Bytes(), &document); err != nil {
		t.Fatalf("invalid JSON %q: %v", buffer.String(), err)
	}
	if document["next"] != "Run ws whoami." {
		t.Errorf("next = %q, want the invoked name", document["next"])
	}
	if document["service"] != "wso2 gateway" {
		t.Errorf("service = %q, want the data value unchanged", document["service"])
	}
}

func TestModuleDiagnosticsUseTheInvokedName(t *testing.T) {
	var buffer bytes.Buffer
	ModuleDiagnostics(Named(&buffer, "ws"), "api", []string{"run wso2 login first"}, false)
	if got, want := buffer.String(), "api: run ws login first\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenameLinesRenamesEveryLine(t *testing.T) {
	text := "Usage:\n  wso2 context list [flags]\n\nRun wso2 help for more.\n"
	want := "Usage:\n  ws context list [flags]\n\nRun ws help for more.\n"
	if got := RenameLines(text, "ws"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenameJSONRenamesOnlyProseMembers(t *testing.T) {
	document := "{\n  \"name\": \"wso2 gateway\",\n  \"recovery\": \"Run wso2 login.\",\n  \"next\": \"Run wso2 whoami.\"\n}"
	want := "{\n  \"name\": \"wso2 gateway\",\n  \"recovery\": \"Run ws login.\",\n  \"next\": \"Run ws whoami.\"\n}"
	if got := string(RenameJSON([]byte(document), "ws")); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResultRenamesARecoveryFieldInEveryMode(t *testing.T) {
	produced := result.New("reference.status/v1").
		With("access", "Access", "refused").
		With("recovery", "Recovery", "Run wso2 login.")
	for _, mode := range []Mode{ModeTable, ModeJSON} {
		var buffer bytes.Buffer
		if err := Result(Named(&buffer, "ws"), mode, produced); err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(buffer.Bytes(), []byte("Run ws login.")) {
			t.Errorf("%s output does not rename the recovery field:\n%s", mode, buffer.String())
		}
	}
}
