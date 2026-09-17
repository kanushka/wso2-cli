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
// so "Sign in with:" Thunder uses iam, and the products offered are 1. api,
// 2. apim and 3. Skip.

// localWizardAnswers answer the create wizard the way localSetup's command
// lines state the same setup.
const localWizardAnswers = "" +
	"1\n" + // Sign in with: Thunder, through iam
	thunderURL + "/\n" + // Thunder URL, trailing slash and all
	"2\n" + // Sign in using: device, refused at Thunder
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
	for _, question := range []string{"Sign in with:", "1. Thunder", "2. WSO2 Identity Server", "3. Asgardeo",
		"4. WSO2 Cloud (coming soon)", "Thunder URL: ", "Sign in using:",
		"2. A code approved on another device (no browser here) (coming soon)",
		"Device sign-in with Thunder is coming soon.", "Add a product this context reaches:",
		"1. api", "2. apim", "3. Skip", "api URL: ", "api gateway URL (empty for none): ", "Add another product:", "Context name [context-1]: ",
		`Create the "local" context? [Y/n]`, `Select "local" as the current context? [Y/n]`, "Log in now? [Y/n]"} {
		if !strings.Contains(errOut, question) {
			t.Errorf("stderr lacks %q:\n%s", question, errOut)
		}
	}
	for _, line := range []string{"Login provider   Thunder", "Login product", "Product api"} {
		if !strings.Contains(errOut, line) {
			t.Errorf("the summary lacks %q:\n%s", line, errOut)
		}
	}
	if strings.Contains(out, "Sign in with:") {
		t.Errorf("a question reached standard output:\n%s", out)
	}
	if !strings.Contains(out, `Created the "local" context.`) || !strings.Contains(out, `Added the "api" product`) {
		t.Errorf("stdout does not report what was written:\n%s", out)
	}
}

func TestContextCreateWizardAsksOnlyWhatTheFlagsLeaveOut(t *testing.T) {
	shell, _, _ := newContextShell(t)
	// The name comes from the argument and the sign-in from --device.
	// Thunder, its URL, skip products, create, and no login now.
	shell.Reader = strings.NewReader("1\n" + thunderURL + "\n3\n\nn\n")
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

func TestContextCreateWizardThroughIdentityServer(t *testing.T) {
	shell, _, _ := newContextShell(t)
	shell.Reader = strings.NewReader("" +
		"4\n" + // Sign in with: WSO2 Cloud, refused
		"2\n" + // Sign in with: Identity Server
		"https://idp.corp.example/\n" + // its URL; the issuer path is added
		"cli\n" + // client ID
		"3\n" + // Sign in using: client credentials
		"not a name\n" + // refused
		"CI_SECRET\n" +
		"3\n" + // Skip products
		"corp\n" + "\n" + "n\n") // name, create, do not select
	code, out, errOut := run(t, shell, "context", "create")
	if code != exit.OK {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	for _, want := range []string{"WSO2 Cloud login is coming soon.", "Identity Server URL (e.g. https://localhost:9443): ", "not the secret"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr lacks %q:\n%s", want, errOut)
		}
	}
	if strings.Contains(errOut, "Log in now?") {
		t.Errorf("offered a login to a client-credentials context:\n%s", errOut)
	}
	document := loadDocument(t, shell)
	corp := contextNamed(t, document, "corp")
	want := contexts.Login{Kind: contexts.KindClientCredentials, Issuer: "https://idp.corp.example/oauth2/token",
		ClientID: "cli", Provider: contexts.ProviderIdentityServer, ClientSecretVariable: "CI_SECRET"}
	if corp.Login != want || len(corp.Products) != 0 || document.DefaultContext != "" {
		t.Errorf("context = %+v, selected %q", corp, document.DefaultContext)
	}
}

func TestContextCreateWizardDeclinedWritesNothing(t *testing.T) {
	shell, _, _ := newContextShell(t)
	shell.Reader = strings.NewReader("1\n" + thunderURL + "\n\n3\nlocal\nn\n")
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
	for name, code := range map[string]string{"local": "contexts.context_exists", "Bad_Name": "shell.invalid_argument"} {
		exitCode, _, errOut := run(t, shell, "context", "create", name)
		if exitCode != exit.Usage || !strings.Contains(errOut, code) {
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
	shell.Reader = strings.NewReader("1\n" + thunderURL + "\n\nlocal\n\n\n")
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

func TestContextCreateWizardThroughAsgardeo(t *testing.T) {
	shell, _, _ := newContextShell(t)
	// Asgardeo, a refused organization name, its name, the client, a browser,
	// skip products, the name, create, select, and no login now.
	shell.Reader = strings.NewReader("3\nacme corp\nacme\ncli\n\n3\nacme\n\n\nn\n")
	code, out, errOut := run(t, shell, "context", "create")
	if code != exit.OK {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	if !strings.Contains(errOut, "Asgardeo organization name: ") {
		t.Errorf("stderr lacks the organization question:\n%s", errOut)
	}
	acme := contextNamed(t, loadDocument(t, shell), "acme")
	want := contexts.Login{Kind: contexts.KindOAuthBrowser, Issuer: "https://api.asgardeo.io/t/acme/oauth2/token",
		ClientID: "cli", Tenant: "acme", Provider: contexts.ProviderAsgardeo}
	if acme.Login != want || acme.Type != contexts.TypeCloud || acme.Organization != "acme" {
		t.Errorf("context = %+v", acme)
	}
}
