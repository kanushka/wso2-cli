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

package app_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/exit"
)

func TestEveryShellCommandAcceptsOutputAndNoInput(t *testing.T) {
	shell, _, _ := newShell(t)
	code, out, errOut := run(t, shell, "help", "--output", "json")
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var document struct {
		Flags []struct {
			Name string `json:"name"`
		} `json:"flags"`
		Commands []struct {
			Path  []string `json:"path"`
			Flags []struct {
				Name string `json:"name"`
			} `json:"flags"`
		} `json:"commands"`
	}
	if err := json.Unmarshal([]byte(out), &document); err != nil {
		t.Fatalf("help is not JSON: %v\n%s", err, out)
	}
	names := func(flags []struct {
		Name string `json:"name"`
	}) []string {
		var collected []string
		for _, flag := range flags {
			collected = append(collected, flag.Name)
		}
		return collected
	}
	for _, want := range []string{"output", "no-input"} {
		if !slices.Contains(names(document.Flags), want) {
			t.Errorf("the shell flags do not list --%s: %v", want, names(document.Flags))
		}
	}
	for _, command := range document.Commands {
		for _, want := range []string{"output", "no-input"} {
			if !slices.Contains(names(command.Flags), want) {
				t.Errorf("ws %s does not accept --%s", strings.Join(command.Path, " "), want)
			}
		}
	}
}

func TestNoInputIsAcceptedOnCommandsThatNeverPrompt(t *testing.T) {
	for _, line := range [][]string{
		{"whoami", "--no-input"},
		{"context", "list", "--no-input"},
		{"--no-input", "version"},
		{"version", "--no-input"},
	} {
		shell, _, _ := newShell(t)
		if code, _, errOut := run(t, shell, line...); code != exit.OK {
			t.Errorf("ws %s: exit %d: %s", strings.Join(line, " "), code, errOut)
		}
	}
}

func TestNoInputBeforeTheCommandNameStopsItsPrompt(t *testing.T) {
	shell, _, _ := newContextShell(t)
	shell.Reader = failIfReadReader{t}
	code, _, errOut := run(t, shell, "--no-input", "context", "create", "local")
	if code != exit.Usage {
		t.Fatalf("exit %d, want a usage refusal: %s", code, errOut)
	}
	if !strings.Contains(errOut, "shell.missing_required_flag") {
		t.Errorf("the refusal does not name the missing flag:\n%s", errOut)
	}
}

func TestVersionRendersJSON(t *testing.T) {
	for _, line := range [][]string{{"version", "-o", "json"}, {"--output", "json", "version"}} {
		shell, _, _ := newShell(t)
		code, out, errOut := run(t, shell, line...)
		if code != exit.OK {
			t.Fatalf("ws %s: exit %d: %s", strings.Join(line, " "), code, errOut)
		}
		var document struct {
			Shell    string            `json:"shell"`
			Protocol string            `json:"protocol"`
			Platform string            `json:"platform"`
			Modules  []json.RawMessage `json:"modules"`
		}
		if err := json.Unmarshal([]byte(out), &document); err != nil {
			t.Fatalf("ws %s is not JSON: %v\n%s", strings.Join(line, " "), err, out)
		}
		if document.Shell == "" || document.Protocol == "" || document.Platform == "" {
			t.Errorf("the version document leaves a fact out: %s", out)
		}
		if document.Modules == nil {
			t.Errorf("modules is absent rather than an empty list: %s", out)
		}
	}
}

func TestProductListRendersJSONOffline(t *testing.T) {
	shell, _, _ := newShell(t)
	t.Setenv(catalog.OriginEnvVar, unreachableCatalogOrigin(t))
	code, out, errOut := run(t, shell, "product", "list", "-o", "json")
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var document struct {
		Products         []json.RawMessage `json:"products"`
		CatalogReachable *bool             `json:"catalogReachable"`
	}
	if err := json.Unmarshal([]byte(out), &document); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if document.Products == nil || len(document.Products) != 0 {
		t.Errorf("products = %s, want an empty list", out)
	}
	if document.CatalogReachable == nil || *document.CatalogReachable {
		t.Errorf("catalogReachable does not say the catalog was unreachable: %s", out)
	}
	if !strings.Contains(errOut, "catalog.origin_unreachable") {
		t.Errorf("the warning left standard error:\n%s", errOut)
	}
}

func TestAProseCommandWritesOneJSONDocumentUnderOutputJSON(t *testing.T) {
	shell, _, _ := newShell(t)
	code, out, errOut := run(t, shell, "--output", "json")
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var document struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(out), &document); err != nil {
		t.Fatalf("not one JSON document: %v\n%s", err, out)
	}
	if !strings.Contains(document.Message, "Usage:") {
		t.Errorf("message does not carry the page: %.200s", document.Message)
	}
}

func TestACompletionScriptIsPrintedAsItIsUnderOutputJSON(t *testing.T) {
	for _, line := range [][]string{
		{"completion", "zsh", "-o", "json"},
		{"-o", "json", "completion", "zsh"},
	} {
		shell, _, _ := newShell(t)
		code, out, errOut := run(t, shell, line...)
		if code != exit.OK {
			t.Fatalf("ws %s: exit %d: %s", strings.Join(line, " "), code, errOut)
		}
		if !strings.HasPrefix(out, "#compdef") {
			t.Errorf("ws %s did not print the script a shell sources: %.80s", strings.Join(line, " "), out)
		}
	}
}

func TestVersionFlagRendersJSON(t *testing.T) {
	shell, _, _ := newShell(t)
	code, out, errOut := run(t, shell, "--version", "-o", "json")
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, `"protocol"`) {
		t.Errorf("--version -o json is not the version document:\n%s", out)
	}
}

func TestAFailureUnderOutputJSONIsAnErrorDocumentOnStandardOutput(t *testing.T) {
	for _, line := range [][]string{
		{"context", "use", "nosuch", "-o", "json"},
		{"--output", "json", "context", "use", "nosuch"},
	} {
		shell, _, _ := newShell(t)
		installLogin(t, shell, oneContextDocument())
		code, out, errOut := run(t, shell, line...)
		if code != exit.NotFound {
			t.Fatalf("ws %s: exit %d: %s", strings.Join(line, " "), code, errOut)
		}
		var document struct {
			Error struct {
				Code     string   `json:"code"`
				Category string   `json:"category"`
				Message  string   `json:"message"`
				Next     []string `json:"next"`
				Exit     int      `json:"exit"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(out), &document); err != nil {
			t.Fatalf("standard output is not one JSON document: %v\n%s", err, out)
		}
		got := document.Error
		if got.Code != "contexts.unknown_context" || got.Category != "not_found" || got.Exit != int(exit.NotFound) {
			t.Errorf("error = %+v", got)
		}
		if len(got.Next) == 0 || got.Next[0] != "wso2 context list" {
			t.Errorf("next = %+v", got.Next)
		}
		if !strings.Contains(errOut, "contexts.unknown_context") {
			t.Errorf("standard error no longer reports the failure:\n%s", errOut)
		}
	}
}

func TestAFlagParseFailureUnderOutputJSONIsAnErrorDocument(t *testing.T) {
	shell, _, _ := newShell(t)
	code, out, errOut := run(t, shell, "whoami", "--bogus", "-o", "json")
	if code != exit.Usage {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, `"code": "shell.unknown_flag"`) {
		t.Errorf("standard output does not carry the refusal:\n%s", out)
	}
}

func TestATableFailureLeavesStandardOutputEmpty(t *testing.T) {
	shell, _, _ := newShell(t)
	installLogin(t, shell, oneContextDocument())
	if _, out, _ := run(t, shell, "context", "use", "nosuch"); out != "" {
		t.Errorf("a table-mode failure wrote to standard output:\n%s", out)
	}
}

func TestWhoamiJSONCarriesTheNextStepAsCommands(t *testing.T) {
	keyring.MockInit()
	t.Setenv("WSO2_CONTEXT", "")
	shell, _, _ := newShell(t)
	installLogin(t, shell, whoamiSeededDocument())
	code, out, errOut := run(t, shell, "whoami", "-o", "json")
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var document struct {
		Recovery string   `json:"recovery"`
		Next     []string `json:"next"`
	}
	if err := json.Unmarshal([]byte(out), &document); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if !slices.Contains(document.Next, "wso2 login") {
		t.Errorf("next = %q, want wso2 login; recovery %q", document.Next, document.Recovery)
	}
}

func TestContextCreateJSONCarriesTheNextStepWithItsNameFilledIn(t *testing.T) {
	shell, _, _ := newContextShell(t)
	installLogin(t, shell, oneContextDocument())
	code, out, errOut := run(t, shell, "context", "create", "second", "--login-product", "iam",
		"--url", thunderURL, "-o", "json")
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var document struct {
		Next []string `json:"next"`
	}
	if err := json.Unmarshal([]byte(out), &document); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if !slices.Contains(document.Next, "wso2 context use second") {
		t.Errorf("next = %q, want wso2 context use second", document.Next)
	}
}

func TestAJSONResultWithNoNextStepHasNoNextMember(t *testing.T) {
	shell, _, _ := newShell(t)
	_, out, _ := run(t, shell, "context", "list", "-o", "json")
	if strings.Contains(out, `"next"`) {
		t.Errorf("a result with no next step carries one:\n%s", out)
	}
}

func TestUnsettingAJSONOutputPreferenceAnswersInTheModeItLeaves(t *testing.T) {
	shell, _, _ := newShell(t)
	mustRun(t, shell, "config", "set", "output", "json")
	code, out, errOut := run(t, shell, "config", "unset", "output")
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if json.Valid([]byte(out)) || !strings.Contains(out, `Unset "output"`) {
		t.Errorf("the confirmation is not the table the unset leaves in force:\n%s", out)
	}
}

func TestAFailedWriteOfTheDocumentIsNotFollowedByAnother(t *testing.T) {
	shell, _, errOut := newShell(t)
	failing := &countingFailWriter{}
	shell.Streams.Out = failing
	if code := shell.Run([]string{"version", "-o", "json"}); code == exit.OK {
		t.Fatalf("a failed write exited 0: %s", errOut)
	}
	if failing.writes != 1 {
		t.Errorf("standard output was written %d times after the first write failed", failing.writes)
	}
}

type countingFailWriter struct{ writes int }

func (w *countingFailWriter) Write([]byte) (int, error) {
	w.writes++
	return 0, errors.New("broken pipe")
}

func TestACompletionScriptThatFailsToStartIsAnErrorDocument(t *testing.T) {
	shell, _, _ := newShell(t)
	code, out, errOut := run(t, shell, "completion", "zsh", "--bogus", "-o", "json")
	if code != exit.Usage {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var document struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &document); err != nil || document.Error.Code != "shell.unknown_flag" {
		t.Errorf("standard output is not the error document (%v):\n%s", err, out)
	}
}

func TestTheVersionFlagRefusesAnUnknownOutputMode(t *testing.T) {
	shell, _, _ := newShell(t)
	code, _, errOut := run(t, shell, "--version", "--output", "xml")
	if code != exit.Usage || !strings.Contains(errOut, "shell.unknown_output_mode") {
		t.Errorf("exit %d, want the refusal ws version gives:\n%s", code, errOut)
	}
}
