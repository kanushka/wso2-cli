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

// The name wso2 login --url gives an account it creates (#175). The browser
// flow refuses under --no-input, so the silent default is driven through the
// process's own standard input, which under go test is not a terminal, and
// the prompt through an injected reader.
package app_test

import (
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/exit"
)

func TestLoginWithoutTheContextFlagNamesTheAccountAccountOne(t *testing.T) {
	shell, out, errOut, issuer := newCreatingLogin(t)

	if code := shell.Run([]string{"login", "--url", issuer.URL, "--client-id", "wso2-cli"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	document := loadDocument(t, shell)
	if len(document.Accounts) != 1 || document.Accounts[0].Name != "account-1" ||
		document.Accounts[0].Auth.CredentialRef != "account-1" {
		t.Fatalf("accounts = %+v, want one named account-1", document.Accounts)
	}
	if len(document.Contexts) != 1 || document.Contexts[0].Name != "account-1" {
		t.Fatalf("contexts = %+v, want one named account-1", document.Contexts)
	}
	if strings.Contains(errOut.String(), "Account name") {
		t.Errorf("asked with no terminal to answer:\n%s", errOut)
	}
	for _, expected := range []string{`Created account "account-1"`, "--context <name>",
		"wso2 account rename account-1 <name>"} {
		if !strings.Contains(out.String(), expected) {
			t.Errorf("the report is missing %q in:\n%s", expected, out)
		}
	}
	if _, err := (session.Store{StateRoot: shell.StateRoot}).Load("account-1"); err != nil {
		t.Fatalf("session not stored under the account's credentialRef: %v", err)
	}
}

// An issuer at a bare address has no host a name could be derived from, which
// was a refusal while names came from the host.
func TestLoginAtABareAddressNamesTheAccountAccountOne(t *testing.T) {
	shell, _, errOut, _ := newCreatingLogin(t)
	// Without a Host, the fake issuer's identifier is its listener's address.
	issuer := fakeissuer.New(t, fakeissuer.Options{}).URL
	if !strings.Contains(issuer, "://127.0.0.1:") {
		t.Fatalf("the issuer %q is not at a bare address", issuer)
	}

	if code := shell.Run([]string{"login", "--url", issuer, "--client-id", "wso2-cli"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	if name := loadDocument(t, shell).Accounts[0].Name; name != "account-1" {
		t.Errorf("account = %q, want account-1", name)
	}
}

func TestLoginWithoutTheContextFlagReusesTheAccountOnTheSameIssuerAndClient(t *testing.T) {
	shell, out, errOut, issuer := newCreatingLogin(t)
	arguments := []string{"login", "--url", issuer.URL, "--client-id", "wso2-cli"}

	if code := shell.Run(arguments); code != exit.OK {
		t.Fatalf("first login failed: exit %d, stderr %s", code, errOut)
	}
	out.Reset()
	// Re-running the same login out of shell history is the common case, and
	// must not grow an account-2 beside the first.
	if code := shell.Run(arguments); code != exit.OK {
		t.Fatalf("second login failed: exit %d, stderr %s", code, errOut)
	}
	document := loadDocument(t, shell)
	if len(document.Accounts) != 1 || len(document.Contexts) != 1 {
		t.Fatalf("two logins wrote %+v and %+v, want one of each", document.Accounts, document.Contexts)
	}
	if strings.Contains(out.String(), "was assigned") {
		t.Errorf("a reused account is reported as newly named:\n%s", out)
	}
}

func TestLoginWithoutTheContextFlagGivesAnotherClientTheNextNumber(t *testing.T) {
	shell, _, errOut, issuer := newCreatingLogin(t)

	if code := shell.Run([]string{"login", "--url", issuer.URL, "--client-id", "wso2-cli"}); code != exit.OK {
		t.Fatalf("first login failed: exit %d, stderr %s", code, errOut)
	}
	if code := shell.Run([]string{"login", "--url", issuer.URL, "--client-id", "other-client"}); code != exit.OK {
		t.Fatalf("second login failed: exit %d, stderr %s", code, errOut)
	}
	document := loadDocument(t, shell)
	if len(document.Accounts) != 2 || document.Accounts[1].Name != "account-2" ||
		document.Accounts[1].Auth.ClientID != "other-client" {
		t.Fatalf("accounts = %+v, want account-2 for the second client", document.Accounts)
	}
}

func TestLoginAsksForTheAccountNameItCreates(t *testing.T) {
	shell, out, errOut, issuer := newCreatingLogin(t)
	shell.Reader = strings.NewReader("Customer IdP\nlocal-idp\n")

	if code := shell.Run([]string{"login", "--url", issuer.URL, "--client-id", "wso2-cli"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	if got := strings.Count(errOut.String(), "Account name [account-1]: "); got != 2 {
		t.Errorf("asked %d times, want 2:\n%s", got, errOut)
	}
	document := loadDocument(t, shell)
	if len(document.Accounts) != 1 || document.Accounts[0].Name != "local-idp" ||
		document.Contexts[0].Name != "local-idp" {
		t.Fatalf("document = %+v, want local-idp", document)
	}
	if strings.Contains(out.String(), "was assigned") {
		t.Errorf("a name the user typed is reported as assigned:\n%s", out)
	}
}

func TestLoginAsksNothingWhenTheContextFlagNamesTheAccount(t *testing.T) {
	shell, _, errOut, issuer := newCreatingLogin(t)
	shell.Reader = failIfReadReader{t}

	code := shell.Run([]string{"login", "--url", issuer.URL, "--client-id", "wso2-cli", "--context", "customer"})
	if code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	if name := loadDocument(t, shell).Accounts[0].Name; name != "customer" {
		t.Errorf("account = %q, want customer", name)
	}
}
