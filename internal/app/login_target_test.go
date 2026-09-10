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
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/exit"
)

// The prompt tests below hand the shell a scripted reader, one answer per
// line, in the order #186 asks: existing or new, deployment, issuer URL,
// client ID, then name.

func TestLoginAsksForANewContextWhenNoneExist(t *testing.T) {
	shell, out, errOut, issuer := newCreatingLogin(t)
	shell.Reader = strings.NewReader("2\n" + issuer.URL + "\nwso2-cli\nlocal-is\n")

	if code := shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	for _, want := range []string{"No contexts yet.", "Deployment:", "WSO2 Cloud (coming soon)",
		"Issuer URL: ", "Client ID of the registered OAuth application: ", "Account name [account-1]: "} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("stderr lacks %q:\n%s", want, errOut)
		}
	}
	if strings.Contains(errOut.String(), "Log in to:") {
		t.Errorf("asked existing or new with no contexts to pick from:\n%s", errOut)
	}
	document := loadDocument(t, shell)
	if len(document.Contexts) != 1 || document.Contexts[0].Name != "local-is" ||
		document.Accounts[0].Auth.Issuer != issuer.URL || document.Accounts[0].Auth.ClientID != "wso2-cli" {
		t.Fatalf("document = %+v, want one local-is context on the issuer", document)
	}
	if !strings.Contains(out.String(), `Logged in to the "local-is" context.`) {
		t.Errorf("stdout does not report the login:\n%s", out)
	}
}

func TestLoginAsksAgainWhenCloudIsPicked(t *testing.T) {
	shell, _, errOut, issuer := newCreatingLogin(t)
	shell.Reader = strings.NewReader("1\n9\n\n" + issuer.URL + "\nwso2-cli\n\n")

	if code := shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	if !strings.Contains(errOut.String(), "WSO2 Cloud login is coming soon.") {
		t.Errorf("picking cloud did not say it is coming soon:\n%s", errOut)
	}
	if !strings.Contains(errOut.String(), "Enter a number from 1 to 2.") {
		t.Errorf("an out-of-range answer was not asked again:\n%s", errOut)
	}
	if got := strings.Count(errOut.String(), "Choose [2]: "); got != 3 {
		t.Errorf("deployment asked %d times, want 3:\n%s", got, errOut)
	}
	if name := loadDocument(t, shell).Contexts[0].Name; name != "account-1" {
		t.Errorf("context = %q, want the default account-1", name)
	}
}

func TestLoginAsksAgainForAnIssuerThatIsNotAURL(t *testing.T) {
	shell, _, errOut, issuer := newCreatingLogin(t)
	shell.Reader = strings.NewReader("2\nidp.example\n" + issuer.URL + "\nwso2-cli\n\n")

	if code := shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	if got := strings.Count(errOut.String(), "Issuer URL: "); got != 2 {
		t.Errorf("issuer asked %d times, want 2:\n%s", got, errOut)
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
	if strings.Contains(errOut.String(), "Deployment:") {
		t.Errorf("an existing context was asked for its deployment:\n%s", errOut)
	}
	if !strings.Contains(out.String(), `Logged in to the "second" context.`) {
		t.Errorf("stdout does not report the picked context:\n%s", out)
	}
	if got := len(loadDocument(t, shell).Contexts); got != 2 {
		t.Errorf("picking an existing context changed the count to %d", got)
	}
}

func TestLoginAsksWhereANamedNewContextAuthenticates(t *testing.T) {
	shell, _, errOut, issuer := newCreatingLogin(t)
	shell.Reader = strings.NewReader("2\n" + issuer.URL + "\nwso2-cli\n")

	if code := shell.Run([]string{"login", "--context", "staging"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	if strings.Contains(errOut.String(), "Account name") {
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
