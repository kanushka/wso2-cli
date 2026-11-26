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
	"reflect"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
)

// The setup wizard's tests (#189). newContextShell installs iam (a thunder
// login provider), api (with a gateway), apim and reference (no descriptor),
// so the wizard signs in through iam, and the products offered are 1. api,
// 2. apim and 3. Skip.

// localWizardAnswers answer the create wizard the way localSetup's command
// lines state the same setup.
const localWizardAnswers = "" +
	thunderURL + "/\n" + // Thunder URL, trailing slash and all
	"\n" + // Client ID: the one iam's descriptor names
	"\n" + // Sign in using: browser
	"\n" + // Add a product: the default, api
	apiURL + "\n" + // api URL
	apiGatewayURL + "\n" + // api gateway URL
	"2\n" + // Add another product: 1. apim, 2. Skip
	"local\n" + // Context name
	"\n" + // Create: yes
	"\n" + // Select: yes
	"n\n" // Log in now: no

func TestContextCreateWizardWritesWhatTheCommandLinesWrite(t *testing.T) {
	byFlags, _, _ := newContextShell(t)
	localSetup(t, byFlags)

	byWizard, _, _ := newContextShell(t)
	byWizard.Reader = strings.NewReader(localWizardAnswers)
	code, out, errOut := run(t, byWizard, "context", "create")
	if code != exit.OK {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}

	want, got := loadDocument(t, byFlags), loadDocument(t, byWizard)
	if !reflect.DeepEqual(want, got) {
		wantJSON, _ := json.MarshalIndent(want, "", "  ")
		gotJSON, _ := json.MarshalIndent(got, "", "  ")
		t.Fatalf("the wizard wrote\n%s\nthe command lines wrote\n%s", gotJSON, wantJSON)
	}
	for _, question := range []string{"Thunder URL: ", "Client ID of the registered OAuth application [", "Sign in using:",
		"2. Device code\n", "Add a product this context reaches:",
		"1. api", "2. apim", "3. Skip", "api URL: ", "api gateway URL (empty for none): ", "Add another product:", "Context name [context-1]: ",
		`Create the "local" context? [Y/n]`, `Select "local" as the current context? [Y/n]`, "Log in now? [Y/n]"} {
		if !strings.Contains(errOut, question) {
			t.Errorf("stderr lacks %q:\n%s", question, errOut)
		}
	}
	if strings.Contains(errOut, "Sign in with:") {
		t.Errorf("asked which provider to sign in with, which the wizard no longer offers:\n%s", errOut)
	}
	for _, line := range []string{"Login provider   Thunder", "Login product", "Product api"} {
		if !strings.Contains(errOut, line) {
			t.Errorf("the summary lacks %q:\n%s", line, errOut)
		}
	}
	if strings.Contains(out, "Thunder URL") {
		t.Errorf("a question reached standard output:\n%s", out)
	}
	if !strings.Contains(out, `Created the "local" context.`) || !strings.Contains(out, `Added the "api" product`) {
		t.Errorf("stdout does not report what was written:\n%s", out)
	}
}

func TestContextCreateWizardAsksOnlyWhatTheFlagsLeaveOut(t *testing.T) {
	shell, _, _ := newContextShell(t)
	// The name comes from the argument and the sign-in from --device.
	// Its URL, the descriptor's client ID, skip products, create, and no login now.
	shell.Reader = strings.NewReader(thunderURL + "\n\n3\n\nn\n")
	code, out, errOut := run(t, shell, "context", "create", "edge", "--device", "--use")
	if code != exit.OK {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	for _, question := range []string{"Sign in using:", "Context name", "Select "} {
		if strings.Contains(errOut, question) {
			t.Errorf("asked %q, which the command line answered:\n%s", question, errOut)
		}
	}
	document := loadDocument(t, shell)
	edge := contextNamed(t, document, "edge")
	if edge.Login.Kind != contexts.KindOAuthDevice || document.DefaultContext != "edge" {
		t.Errorf("document = %+v", document)
	}
}

func TestContextCreateWizardDeclinedWritesNothing(t *testing.T) {
	shell, _, _ := newContextShell(t)
	shell.Reader = strings.NewReader(thunderURL + "\n\n\n3\nlocal\nn\n")
	code, _, errOut := run(t, shell, "context", "create")
	if code != exit.Usage || !strings.Contains(errOut, "shell.cancelled") ||
		!strings.Contains(errOut, "Nothing was written.") {
		t.Fatalf("exit %d, stderr: %s", code, errOut)
	}
	if got := len(loadDocument(t, shell).Contexts); got != 0 {
		t.Errorf("a declined wizard wrote %d contexts", got)
	}
}

func TestContextCreateWizardRefusesAKnownNameBeforeAsking(t *testing.T) {
	shell, _, _ := newContextShell(t)
	localSetup(t, shell)
	shell.Reader = failIfReadReader{t}
	for name, want := range map[string]struct {
		code   string
		status exit.Code
	}{"local": {"contexts.context_exists", exit.Conflict}, "Bad_Name": {"shell.invalid_argument", exit.Usage}} {
		exitCode, _, errOut := run(t, shell, "context", "create", name)
		if exitCode != want.status || !strings.Contains(errOut, want.code) {
			t.Errorf("%s: exit %d, stderr: %s", name, exitCode, errOut)
		}
	}
}

func TestContextCreateWizardEndOfInputIsARefusal(t *testing.T) {
	shell, _, _ := newContextShell(t)
	shell.Reader = strings.NewReader("1\n")
	code, _, errOut := run(t, shell, "context", "create")
	if code != exit.Usage || !strings.Contains(errOut, "input ended before the URL was entered") {
		t.Fatalf("exit %d, stderr: %s", code, errOut)
	}
	if got := len(loadDocument(t, shell).Contexts); got != 0 {
		t.Errorf("wrote %d contexts", got)
	}
}

func TestContextCreateWizardWithJSONOutputOffersNothingAfter(t *testing.T) {
	shell, _, _ := newContextShell(t)
	shell.Reader = strings.NewReader(thunderURL + "\n\n\nlocal\n\n\n")
	code, out, errOut := run(t, shell, "context", "create", "--output", "json")
	if code != exit.OK {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, out)
	}
	for _, question := range []string{"Add a product", "Log in now?"} {
		if strings.Contains(errOut, question) {
			t.Errorf("asked %q of a JSON caller:\n%s", question, errOut)
		}
	}
}

func TestSetupCommandsAskNothingWhenTheyMayNot(t *testing.T) {
	cases := map[string][]string{
		"create with --no-input":      {"context", "create", "--no-input"},
		"product add with --no-input": {"context", "product", "add", "--no-input"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			shell, _, _ := newContextShell(t)
			shell.Reader = failIfReadReader{t}
			code, _, errOut := run(t, shell, args...)
			if code != exit.Usage || !strings.Contains(errOut, "shell.missing_argument") {
				t.Fatalf("exit %d, stderr: %s", code, errOut)
			}
		})
	}
	t.Run("create under WSO2_NO_INPUT", func(t *testing.T) {
		shell, _, _ := newContextShell(t)
		t.Setenv("WSO2_NO_INPUT", "1")
		shell.Reader = failIfReadReader{t}
		code, _, errOut := run(t, shell, "context", "create", "local")
		if code != exit.Usage || !strings.Contains(errOut, "shell.missing_required_flag") {
			t.Fatalf("exit %d, stderr: %s", code, errOut)
		}
	})
	t.Run("the flag form asks nothing", func(t *testing.T) {
		shell, _, _ := newContextShell(t)
		shell.Reader = failIfReadReader{t}
		localSetup(t, shell)
	})
}

func TestContextProductAddWizardPreviewsThenAdds(t *testing.T) {
	shell, _, _ := newContextShell(t)
	mustRun(t, shell, "context", "create", "local", "--login-product", "iam", "--url", thunderURL, "--use")
	// iam is recorded already and is a login provider, so api and apim are
	// offered.
	shell.Reader = strings.NewReader("1\n" + apiURL + "\n" + apiGatewayURL + "\n\n")
	code, out, errOut := run(t, shell, "context", "product", "add")
	if code != exit.OK {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	if strings.Contains(errOut, "iam") {
		t.Errorf("offered iam, which the context records:\n%s", errOut)
	}
	for _, question := range []string{"Product:", "1. api", "2. apim", "api URL: ", "api gateway URL",
		"Add the api product? [Y/n]"} {
		if !strings.Contains(errOut, question) {
			t.Errorf("stderr lacks %q:\n%s", question, errOut)
		}
	}
	if !strings.Contains(out, `Would add the "api" product`) || !strings.Contains(out, `Added the "api" product`) {
		t.Errorf("stdout lacks the preview or the result:\n%s", out)
	}

	byFlags, _, _ := newContextShell(t)
	localSetup(t, byFlags)
	if want, got := loadDocument(t, byFlags), loadDocument(t, shell); !reflect.DeepEqual(want, got) {
		t.Errorf("the wizard wrote %+v, the command lines %+v", got, want)
	}
}

func TestContextProductAddWizardForANamedProductDeclined(t *testing.T) {
	shell, _, _ := newContextShell(t)
	mustRun(t, shell, "context", "create", "local", "--login-product", "iam", "--url", thunderURL, "--use")
	shell.Reader = strings.NewReader("https://apim.example\n\n" + apimClient + "\nn\n")
	code, out, errOut := run(t, shell, "context", "product", "add", "apim")
	if code != exit.Usage || !strings.Contains(errOut, "the product was not added") {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	if got := strings.Count(errOut, "Client ID apim's deployment registered for this CLI: "); got != 2 {
		t.Errorf("client ID asked %d times, want 2 (an empty answer is asked again):\n%s", got, errOut)
	}
	if !strings.Contains(out, `Would add the "apim" product`) {
		t.Errorf("no preview:\n%s", out)
	}
	if strings.Contains(errOut, "Product:") || strings.Contains(errOut, "gateway") {
		t.Errorf("asked what the argument and the descriptor answered:\n%s", errOut)
	}
	if _, found := contextNamed(t, loadDocument(t, shell), "local").Products["apim"]; found {
		t.Error("a declined product was written")
	}
}

// TestContextCreateWizardPrintsTheCommandLinesItRan proves the wizard ends
// with the command lines that do the same with no question, by running them
// in another shell and finding the same document.
func TestContextCreateWizardPrintsTheCommandLinesItRan(t *testing.T) {
	byWizard, _, _ := newContextShell(t)
	byWizard.Reader = strings.NewReader(localWizardAnswers)
	// A flag the wizard does not ask about is part of the line it prints.
	code, out, errOut := run(t, byWizard, "context", "create", "--organization", "o1", "--project", "p1")
	if code != exit.OK {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	_, printed, found := strings.Cut(errOut, "The same without questions:\n")
	if !found {
		t.Fatalf("the wizard printed no command lines:\n%s", errOut)
	}

	byLines, _, _ := newContextShell(t)
	byLines.Reader = failIfReadReader{t}
	ran := 0
	for _, line := range strings.Split(strings.TrimSpace(printed), "\n") {
		words := strings.Fields(line)
		if len(words) == 0 || words[0] != "wso2" {
			break
		}
		mustRun(t, byLines, append(words[1:], "--no-input")...)
		ran++
	}
	if ran != 2 {
		t.Errorf("ran %d printed lines, want the create and one product add:\n%s", ran, printed)
	}
	if want, got := loadDocument(t, byWizard), loadDocument(t, byLines); !reflect.DeepEqual(want, got) {
		t.Errorf("the printed lines wrote\n%+v\nthe wizard wrote\n%+v", got, want)
	}
}
