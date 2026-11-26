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
	"bytes"
	"net/http"
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/exit"
)

// The prompt tests below hand the shell a scripted reader, one answer per
// line, in the order the setup wizard asks a new context's questions: the
// Thunder URL, the client ID, how to sign in, the products, the name, then
// whether to create and select it. The context modules are installed, so the
// login goes through iam to a local fake issuer, and api and apim are offered.

// newWizardLogin is a shell with the context modules installed whose browser
// completes every authorization at a fake Thunder.
func newWizardLogin(t *testing.T) (app.Shell, *bytes.Buffer, *bytes.Buffer, *fakeissuer.Issuer) {
	t.Helper()
	keyring.MockInit()
	issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "https://localhost:8090/mcp"})
	shell, out, errOut := newContextShell(t)
	shell.OpenBrowser = func(authURL string) error {
		go func() {
			response, err := http.Get(authURL)
			if err == nil {
				_ = response.Body.Close()
			}
		}()
		return nil
	}
	return shell, out, errOut, issuer
}

// wizardAnswers set up a browser context at issuer with no products, taking
// the descriptor's client ID and every default but the name.
func wizardAnswers(issuer, name string) string {
	return issuer + "\n\n\n3\n" + name + "\n\n\n"
}

func TestLoginAsksForANewContextWhenNoneExist(t *testing.T) {
	shell, out, errOut, issuer := newWizardLogin(t)
	shell.Reader = strings.NewReader(wizardAnswers(issuer.URL, "local"))

	if code := shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	for _, want := range []string{"No contexts yet.", "Thunder URL: ",
		"Client ID of the registered OAuth application [wso2-cli]: ",
		"Sign in using:", "Context name [context-1]: ", `Create the "local" context? [Y/n]`} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("stderr lacks %q:\n%s", want, errOut)
		}
	}
	for _, unwanted := range []string{"Log in to:", "Sign in with:"} {
		if strings.Contains(errOut.String(), unwanted) {
			t.Errorf("asked %q:\n%s", unwanted, errOut)
		}
	}
	document := loadDocument(t, shell)
	if len(document.Contexts) != 1 || document.Contexts[0].Name != "local" ||
		document.Contexts[0].Login.Issuer != issuer.URL || document.Contexts[0].Login.ClientID != "wso2-cli" {
		t.Fatalf("document = %+v, want one local context on the issuer", document)
	}
	if !strings.Contains(out.String(), "local") {
		t.Errorf("stdout does not report the login:\n%s", out)
	}
}

func TestLoginAsksAgainForAnIssuerThatIsNotAURL(t *testing.T) {
	shell, _, errOut, issuer := newWizardLogin(t)
	shell.Reader = strings.NewReader("idp.example\n" + wizardAnswers(issuer.URL, ""))

	if code := shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	if got := strings.Count(errOut.String(), "Thunder URL: "); got != 2 {
		t.Errorf("the URL was asked %d times, want 2:\n%s", got, errOut)
	}
	if strings.Contains(errOut.String(), "idp.example\n") {
		t.Errorf("the rejected answer was echoed:\n%s", errOut)
	}
}

func TestLoginOffersTheExistingContexts(t *testing.T) {
	shell, out, errOut, issuer := newCreatingLogin(t)
	for _, name := range []string{"first", "second"} {
		if code := shell.Run([]string{"login", "--url", issuer.URL, "--client-id", name,
			"--context", name}); code != exit.OK {
			t.Fatalf("setting up %s failed: exit %d, stderr %s", name, code, errOut)
		}
	}
	out.Reset()
	errOut.Reset()
	// Return takes "An existing context"; 2 picks the second one listed.
	shell.Reader = strings.NewReader("\n2\n")

	if code := shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	for _, want := range []string{"Log in to:", "1. first (selected)", "2. second"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("stderr lacks %q:\n%s", want, errOut)
		}
	}
	if strings.Contains(errOut.String(), "Sign in with:") {
		t.Errorf("an existing context was asked for its deployment:\n%s", errOut)
	}
	if !strings.Contains(out.String(), "second") {
		t.Errorf("stdout does not report the picked context:\n%s", out)
	}
	if got := len(loadDocument(t, shell).Contexts); got != 2 {
		t.Errorf("picking an existing context changed the count to %d", got)
	}
}

func TestLoginAsksWhereANamedNewContextAuthenticates(t *testing.T) {
	shell, _, errOut, issuer := newWizardLogin(t)
	// No name is asked: --context gave it.
	shell.Reader = strings.NewReader(issuer.URL + "\n\n\n3\n\n\n")

	if code := shell.Run([]string{"login", "--context", "staging"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	if strings.Contains(errOut.String(), "Context name") {
		t.Errorf("asked for a name --context already gave:\n%s", errOut)
	}
	if name := loadDocument(t, shell).Contexts[0].Name; name != "staging" {
		t.Errorf("context = %q, want staging", name)
	}
}

func TestLoginAsksNothingForAnExistingNamedContext(t *testing.T) {
	shell, _, errOut, issuer := newCreatingLogin(t)
	if code := shell.Run([]string{"login", "--url", issuer.URL, "--client-id", "wso2-cli",
		"--context", "staging"}); code != exit.OK {
		t.Fatalf("setup failed: exit %d, stderr %s", code, errOut)
	}
	shell.Reader = failIfReadReader{t}

	if code := shell.Run([]string{"login", "--context", "staging"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
}

func TestLoginWithNoInputAsksNothing(t *testing.T) {
	shell, _, errOut, issuer := newCreatingLogin(t)
	shell.Reader = failIfReadReader{t}

	// Every flag is given, so nothing is asked, and the browser login that
	// follows is refused by its own non-interactive rule rather than by a
	// prompt that should never have run.
	code := shell.Run([]string{"login", "--no-input", "--context", "ci",
		"--url", issuer.URL, "--client-id", "wso2-cli"})
	if code != exit.AuthPolicy || !strings.Contains(errOut.String(), "auth.non_interactive") {
		t.Fatalf("a fully flagged --no-input login: exit %d, stderr %s", code, errOut)
	}
	errOut.Reset()
	code = shell.Run([]string{"login", "--no-input", "--context", "missing"})
	if code != exit.Usage || !strings.Contains(errOut.String(), "shell.missing_required_flag") {
		t.Fatalf("a missing context under --no-input: exit %d, stderr:\n%s", code, errOut)
	}
}

func TestLoginSettingUpAMachineContextEndsWithoutALogin(t *testing.T) {
	shell, out, errOut, issuer := newWizardLogin(t)
	// Where it is, its client, client credentials and the variable, no
	// products, the name, then the defaults.
	shell.Reader = strings.NewReader(issuer.URL + "\nci-client\n3\nCI_SECRET\n3\nci\n\n\n")

	if code := shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	if !strings.Contains(out.String(), "needs no login") {
		t.Errorf("stdout does not say the context needs no login:\n%s", out)
	}
	if strings.Contains(out.String(), "Logged in") {
		t.Errorf("reported a login that did not happen:\n%s", out)
	}
	if name := loadDocument(t, shell).Contexts[0].Name; name != "ci" {
		t.Errorf("context = %q, want ci", name)
	}
}
