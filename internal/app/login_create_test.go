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
	"os"
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/contexts/fixture"
	"github.com/wso2/wso2-cli/internal/exit"
)

// newCreatingLogin is a shell whose browser hook follows the authorization URL,
// against an issuer whose identifier carries a hostname. The hostname matters:
// an issuer at 127.0.0.1 has no name to derive an identity name from, which is
// the refusal TestLoginRefusesAnIssuerNoNameCanBeDerivedFrom covers instead.
func newCreatingLogin(t *testing.T) (app.Shell, *bytes.Buffer, *bytes.Buffer, *fakeissuer.Issuer) {
	t.Helper()
	keyring.MockInit()
	issuer := fakeissuer.New(t, fakeissuer.Options{Host: "localhost"})
	shell, out, errOut := newLoginShell(t)
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

func TestLoginCreatesAnIdentityAndAContextWhenNoneMatches(t *testing.T) {
	shell, out, errOut, issuer := newCreatingLogin(t)

	code := shell.Run([]string{"login", "--url", issuer.URL, "--client-id", "wso2-cli", "--context", "customer"})
	if code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}

	document, err := contexts.Load(shell.StateRoot)
	if err != nil {
		t.Fatalf("Load after login: %v", err)
	}
	if len(document.Identities) != 1 || len(document.Contexts) != 1 {
		t.Fatalf("login wrote %d identities and %d contexts, want one of each",
			len(document.Identities), len(document.Contexts))
	}
	identity := document.Identities[0]
	if identity.Name != "customer" {
		t.Errorf("identity name = %q, want customer", identity.Name)
	}
	if identity.Auth.Issuer != issuer.URL {
		t.Errorf("identity issuer = %q, want %q", identity.Auth.Issuer, issuer.URL)
	}
	if identity.Auth.ClientID != "wso2-cli" {
		t.Errorf("identity clientId = %q, want wso2-cli", identity.Auth.ClientID)
	}
	if identity.Auth.CredentialRef != identity.Name {
		t.Errorf("credentialRef = %q, want the identity name %q",
			identity.Auth.CredentialRef, identity.Name)
	}
	if identity.Auth.Kind != contexts.KindOAuthBrowser {
		t.Errorf("identity kind = %q, want %q", identity.Auth.Kind, contexts.KindOAuthBrowser)
	}
	if len(identity.Products) != 0 {
		t.Errorf("login wrote a products block: %v", identity.Products)
	}
	if document.Contexts[0].Name != "customer" || document.Contexts[0].Identity != "customer" {
		t.Errorf("context = %+v, want customer authenticating as customer", document.Contexts[0])
	}
	// The names it assigned, because a name the user is not told is a name they
	// have to go and read out of a JSON file.
	for _, expected := range []string{`Created identity "customer"`, `context "customer"`} {
		if !strings.Contains(out.String(), expected) {
			t.Errorf("the report is missing %q in:\n%s", expected, out)
		}
	}
	// The session went where every other login puts one.
	if _, err := (session.Store{StateRoot: shell.StateRoot}).Load("customer"); err != nil {
		t.Fatalf("session not stored under the identity's credentialRef: %v", err)
	}
}

// TestWithoutTheContextFlagTheIdentityNameIsDerivedAndReported covers the D6
// derivation end to end rather than only in contexts: the name in the document
// and the name in the report both have to be the derived one.
func TestWithoutTheContextFlagTheIdentityNameIsDerivedAndReported(t *testing.T) {
	shell, out, errOut, issuer := newCreatingLogin(t)
	derived, err := contexts.IdentityNameForIssuer(issuer.URL)
	if err != nil {
		t.Fatalf("the test issuer has no derivable name: %v", err)
	}

	if code := shell.Run([]string{"login", "--url", issuer.URL, "--client-id", "wso2-cli"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}

	document, err := contexts.Load(shell.StateRoot)
	if err != nil {
		t.Fatalf("Load after login: %v", err)
	}
	if len(document.Identities) != 1 || document.Identities[0].Name != derived {
		t.Fatalf("login wrote %+v, want one identity named %q", document.Identities, derived)
	}
	if len(document.Contexts) != 1 || document.Contexts[0].Name != derived {
		t.Fatalf("login wrote %+v, want one context named %q", document.Contexts, derived)
	}
	if !strings.Contains(out.String(), derived) {
		t.Errorf("the report does not name the derived identity %q:\n%s", derived, out)
	}
}

func TestTheFirstContextLoginCreatesBecomesTheDefaultAndTheOutputSaysSo(t *testing.T) {
	shell, out, errOut, issuer := newCreatingLogin(t)

	if code := shell.Run([]string{"login", "--url", issuer.URL,
		"--client-id", "wso2-cli", "--context", "customer"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}

	document, err := contexts.Load(shell.StateRoot)
	if err != nil {
		t.Fatalf("Load after login: %v", err)
	}
	if document.DefaultContext != "customer" {
		t.Errorf("defaultContext = %q, want customer", document.DefaultContext)
	}
	if !strings.Contains(out.String(), "selected") {
		t.Errorf("the report does not say the context was selected:\n%s", out)
	}
}

// TestASelfHostedLoginNamesIdentityAddProduct is what keeps B.1 out of an
// editor: a created identity reaches no product until one is recorded, and the
// command that records it has to be named where the user is standing.
func TestASelfHostedLoginNamesIdentityAddProduct(t *testing.T) {
	shell, out, errOut, issuer := newCreatingLogin(t)

	if code := shell.Run([]string{"login", "--url", issuer.URL,
		"--client-id", "wso2-cli", "--context", "customer"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	if !strings.Contains(out.String(), "wso2 identity add-product customer") {
		t.Errorf("the report does not name wso2 identity add-product:\n%s", out)
	}
}

func TestLoginReusesAnIdentityWhoseIssuerAndClientMatch(t *testing.T) {
	shell, _, errOut, issuer := newCreatingLogin(t)
	arguments := []string{"login", "--url", issuer.URL, "--client-id", "wso2-cli", "--context", "customer"}

	if code := shell.Run(arguments); code != exit.OK {
		t.Fatalf("first login failed: exit %d, stderr %s", code, errOut)
	}
	// Re-running the same login is the common case: a session expired and
	// someone repeated the command out of shell history.
	if code := shell.Run(arguments); code != exit.OK {
		t.Fatalf("second login failed: exit %d, stderr %s", code, errOut)
	}

	document, err := contexts.Load(shell.StateRoot)
	if err != nil {
		t.Fatalf("Load after login: %v", err)
	}
	if len(document.Identities) != 1 || len(document.Contexts) != 1 {
		t.Fatalf("two logins wrote %d identities and %d contexts, want one of each",
			len(document.Identities), len(document.Contexts))
	}
}

func TestLoginIsRefusedWhenAnIdentityOfThatNameDiffers(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		issuer   string
		clientID string
	}{
		{"a different issuer", "https://other.example", "wso2-cli"},
		{"a different client", "", "other-client"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			// The browser hook fails the test if it is reached, which is how
			// this proves the mismatch was refused before a login started.
			shell, out, errOut := newLoginShell(t)
			issuerURL := testCase.issuer
			if issuerURL == "" {
				issuerURL = "https://idp.customer.example"
			}
			installLogin(t, shell, contexts.Document{
				SchemaVersion:  contexts.SchemaVersion,
				DefaultContext: "customer",
				Identities: []contexts.Identity{{
					Name: "customer", Type: "onprem",
					Auth: contexts.IdentityAuth{
						Kind:          contexts.KindOAuthBrowser,
						Issuer:        "https://idp.customer.example",
						ClientID:      "wso2-cli",
						CredentialRef: "customer",
					},
				}},
				Contexts: []contexts.Context{{Name: "customer", Identity: "customer"}},
			})
			before, err := os.ReadFile(contexts.Path(shell.StateRoot))
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}

			code := shell.Run([]string{"login", "--url", issuerURL,
				"--client-id", testCase.clientID, "--context", "customer"})
			if code != exit.Usage {
				t.Fatalf("exit code = %d, want %d (usage); stderr: %s", code, exit.Usage, errOut)
			}
			requireRefusal(t, errOut.String(), "contexts.identity_exists")
			after, err := os.ReadFile(contexts.Path(shell.StateRoot))
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}
			if !bytes.Equal(before, after) {
				t.Error("a refused login rewrote the document")
			}
			if out.String() != "" {
				t.Errorf("a refused login wrote to standard output:\n%s", out)
			}
		})
	}
}

func TestOmittingTheClientIdUnderNoInputIsATypedProblem(t *testing.T) {
	shell, out, errOut := newLoginShell(t)
	t.Setenv("WSO2_NO_INPUT", "1")

	code := shell.Run([]string{"login", "--url", "https://idp.customer.example"})
	if code != exit.Usage {
		t.Fatalf("exit code = %d, want %d (usage); stderr: %s", code, exit.Usage, errOut)
	}
	requireRefusal(t, errOut.String(), "shell.missing_required_flag")
	if !strings.Contains(errOut.String(), "--client-id") {
		t.Errorf("the refusal does not name --client-id:\n%s", errOut)
	}
	if _, err := os.Stat(contexts.Path(shell.StateRoot)); !os.IsNotExist(err) {
		t.Error("a refused login wrote a context document")
	}
	if out.String() != "" {
		t.Errorf("a refused login wrote to standard output:\n%s", out)
	}
}

// TestOmittingTheClientIdWithoutNoInputRefusesOnStandardInput pins the
// behaviour internal/output.StdinIsTerminal replaced stdinIsTerminal with:
// neither --no-input nor WSO2_NO_INPUT is set here, so resolveClientID falls
// through to asking whether standard input is a terminal.
//
// `go test` connects the test binary's standard input to /dev/null, which
// the old os.ModeCharDevice check treated as a terminal (a documented hole)
// and the new ioctl/GetConsoleMode-backed check does not, so this is
// deterministic under `go test` rather than depending on however the test
// runner happens to be invoked. Before the fold-in, this same command
// prompted and then failed with "nothing was entered at the prompt" instead
// of refusing immediately; this test pins the new, correct answer at the
// login contract, not just inside internal/output.
func TestOmittingTheClientIdWithoutNoInputRefusesOnStandardInput(t *testing.T) {
	shell, out, errOut := newLoginShell(t)

	code := shell.Run([]string{"login", "--url", "https://idp.customer.example"})
	if code != exit.Usage {
		t.Fatalf("exit code = %d, want %d (usage); stderr: %s", code, exit.Usage, errOut)
	}
	requireRefusal(t, errOut.String(), "shell.missing_required_flag")
	if !strings.Contains(errOut.String(), "standard input is not a terminal") {
		t.Errorf("the refusal does not name standard input:\n%s", errOut)
	}
	if _, err := os.Stat(contexts.Path(shell.StateRoot)); !os.IsNotExist(err) {
		t.Error("a refused login wrote a context document")
	}
	if out.String() != "" {
		t.Errorf("a refused login wrote to standard output:\n%s", out)
	}
}

// TestLoginRefusesAnIssuerNoNameCanBeDerivedFrom covers a plausible self-hosted
// first run: an issuer at a bare IP address, whose host cannot make a legal
// name.
func TestLoginRefusesAnIssuerNoNameCanBeDerivedFrom(t *testing.T) {
	shell, out, errOut := newLoginShell(t)

	code := shell.Run([]string{"login", "--url", "https://10.0.0.5:9443", "--client-id", "wso2-cli"})
	if code != exit.Usage {
		t.Fatalf("exit code = %d, want %d (usage); stderr: %s", code, exit.Usage, errOut)
	}
	requireRefusal(t, errOut.String(), "contexts.identity_name_underivable")
	if !strings.Contains(errOut.String(), "--context") {
		t.Errorf("the refusal does not name --context:\n%s", errOut)
	}
	if _, err := os.Stat(contexts.Path(shell.StateRoot)); !os.IsNotExist(err) {
		t.Error("a refused login wrote a context document")
	}
	if out.String() != "" {
		t.Errorf("a refused login wrote to standard output:\n%s", out)
	}
}

// TestNothingWrittenByLoginIsACredential is ADR 0011's claim, checked rather
// than asserted: the document is searched for the exact token values this
// login's issuer minted, so a member added later that carried one would fail
// here without anyone having to remember to add it to a list.
func TestNothingWrittenByLoginIsACredential(t *testing.T) {
	shell, _, errOut, issuer := newCreatingLogin(t)

	if code := shell.Run([]string{"login", "--url", issuer.URL,
		"--client-id", "wso2-cli", "--context", "customer"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}

	stored, err := (session.Store{StateRoot: shell.StateRoot}).Load("customer")
	if err != nil {
		t.Fatalf("session not stored: %v", err)
	}
	if stored.RefreshToken == "" || stored.AccessToken == "" {
		t.Fatal("the fixture minted no tokens, so this test would prove nothing")
	}
	written, err := os.ReadFile(contexts.Path(shell.StateRoot))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	for _, secret := range []string{stored.RefreshToken, stored.AccessToken} {
		if bytes.Contains(written, []byte(secret)) {
			t.Fatalf("token material reached the context document:\n%s", written)
		}
	}
}

// TestLoginWithoutTheURLFlagStillLogsInToTheSelectedContext pins D5: --url is
// what turns the creating path on, and a login without it behaves exactly as it
// did before this command could write anything.
func TestLoginWithoutTheURLFlagStillLogsInToTheSelectedContext(t *testing.T) {
	keyring.MockInit()
	issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "reference-status"})
	shell, out, errOut := newLoginShell(t)
	installLogin(t, shell, browserDoc(issuer.URL))
	shell.OpenBrowser = func(authURL string) error {
		go func() {
			response, err := http.Get(authURL)
			if err == nil {
				_ = response.Body.Close()
			}
		}()
		return nil
	}

	if code := shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	if strings.Contains(out.String(), "Created identity") {
		t.Errorf("a login without --url created something:\n%s", out)
	}
	document, err := contexts.Load(shell.StateRoot)
	if err != nil {
		t.Fatalf("Load after login: %v", err)
	}
	if len(document.Identities) != 1 || len(document.Contexts) != 1 {
		t.Errorf("a login without --url changed the document: %+v", document)
	}
}

// TestAFrozenDocumentIsRefusedBeforeTheBrowserOpens is the ordering ruling's
// missing half. Authenticating first is right, but a document the shell will
// not write is a refusal it can make before a session exists: minting one
// against a deterministic write failure leaves a refresh token in the secure
// store that no identity names, that no command reaches, and that a retry only
// duplicates.
func TestAFrozenDocumentIsRefusedBeforeTheBrowserOpens(t *testing.T) {
	shell, out, errOut, issuer := newCreatingLogin(t)
	shell.OpenBrowser = func(string) error {
		t.Error("the shell opened a browser for a login whose write was already refused")
		return nil
	}
	if err := fixture.Install(shell.StateRoot, fixture.LegacyDocument{
		SchemaVersion:  1,
		DefaultContext: "proof",
		Contexts: []fixture.LegacyContext{{
			Name:           "proof",
			OrganizationID: "acme",
			Endpoint:       "https://reference.example.test",
			Auth: fixture.LegacyAuth{
				Method:             contexts.MethodDevelopmentCredential,
				CredentialVariable: "WSO2_DEV_CREDENTIAL",
			},
		}},
	}); err != nil {
		t.Fatalf("install legacy document: %v", err)
	}

	code := shell.Run([]string{"login", "--url", issuer.URL,
		"--client-id", "wso2-cli", "--context", "customer"})
	if code != exit.Usage {
		t.Fatalf("exit code = %d, want %d (usage); stderr: %s", code, exit.Usage, errOut)
	}
	requireRefusal(t, errOut.String(), "contexts.document_frozen")
	// The point of refusing early: no session was minted, so there is nothing
	// orphaned in the secure store for the user to be unable to reach.
	if _, err := (session.Store{StateRoot: shell.StateRoot}).Load("customer"); err == nil {
		t.Error("a refused login left a session in the secure store")
	}
	if out.String() != "" {
		t.Errorf("a refused login wrote to standard output:\n%s", out)
	}
}

// TestLoginRefusesAnIssuerThatIsNotAURL covers one of the two commonest
// first-run typos. Left to the name derivation, a missing scheme is reported as
// a name that cannot be derived, and following that advice produces a second
// wrong message from the OIDC client.
func TestLoginRefusesAnIssuerThatIsNotAURL(t *testing.T) {
	for _, issuer := range []string{"idp.customer.example", "ftp://idp.customer.example", "https://"} {
		t.Run(issuer, func(t *testing.T) {
			shell, out, errOut := newLoginShell(t)

			// --context is given, so nothing else in the command would look at
			// the URL: without this refusal it reaches the OIDC client.
			code := shell.Run([]string{"login", "--url", issuer,
				"--client-id", "wso2-cli", "--context", "customer"})
			if code != exit.Usage {
				t.Fatalf("exit code = %d, want %d (usage); stderr: %s", code, exit.Usage, errOut)
			}
			requireRefusal(t, errOut.String(), "shell.invalid_argument")
			if !strings.Contains(errOut.String(), "--url") {
				t.Errorf("the refusal does not name --url:\n%s", errOut)
			}
			if out.String() != "" {
				t.Errorf("a refused login wrote to standard output:\n%s", out)
			}
		})
	}
}

// TestTheClientIdFlagWithoutTheURLFlagIsRefused covers a plain usage mistake
// that was reported as a missing context document: the advice was to write one
// by hand, which is the instruction #112 exists to delete.
func TestTheClientIdFlagWithoutTheURLFlagIsRefused(t *testing.T) {
	shell, out, errOut := newLoginShell(t)

	code := shell.Run([]string{"login", "--client-id", "wso2-cli"})
	if code != exit.Usage {
		t.Fatalf("exit code = %d, want %d (usage); stderr: %s", code, exit.Usage, errOut)
	}
	requireRefusal(t, errOut.String(), "shell.missing_required_flag")
	if !strings.Contains(errOut.String(), "--url") {
		t.Errorf("the refusal does not name --url:\n%s", errOut)
	}
	if out.String() != "" {
		t.Errorf("a refused login wrote to standard output:\n%s", out)
	}
}

// TestTheUnselectedContextRefusalNamesTheCommandsThatFixIt covers the first
// command a user is likely to type on a clean machine. The advice it used to
// give — author a context document — is the editor round trip this wave exists
// to remove, and the commands that replace it now exist.
func TestTheUnselectedContextRefusalNamesTheCommandsThatFixIt(t *testing.T) {
	// The two commands name different ways out, because the ways out differ:
	// login creates the context it is missing, and logout has nothing to end
	// until a login has created something.
	for _, testCase := range []struct {
		command string
		names   []string
	}{
		{"login", []string{"wso2 context use", "wso2 login --url"}},
		{"logout", []string{"wso2 context use", "wso2 context list"}},
	} {
		t.Run(testCase.command, func(t *testing.T) {
			shell, _, errOut := newLoginShell(t)

			if code := shell.Run([]string{testCase.command}); code != exit.AuthPolicy {
				t.Fatalf("exit code = %d, want %d (auth policy); stderr: %s",
					code, exit.AuthPolicy, errOut)
			}
			requireRefusal(t, errOut.String(), "auth.context_not_selected")
			if strings.Contains(errOut.String(), "Author a context document") {
				t.Errorf("the refusal still sends the user to an editor:\n%s", errOut)
			}
			for _, named := range testCase.names {
				if !strings.Contains(errOut.String(), named) {
					t.Errorf("the refusal does not name %s:\n%s", named, errOut)
				}
			}
		})
	}
}

// TestLoginRefusesAnIssuerCarryingUserinfoBeforeTheBrowserOpens is the
// stranded-session case again, from the other end. Userinfo in an issuer URL is
// refused by the document, which is only consulted after the login, so without
// this the shell would mint a session and then refuse to record it. What was in
// the userinfo may well be a password, so nothing echoes it.
func TestLoginRefusesAnIssuerCarryingUserinfoBeforeTheBrowserOpens(t *testing.T) {
	shell, out, errOut, issuer := newCreatingLogin(t)
	shell.OpenBrowser = func(string) error {
		t.Error("the shell opened a browser for a login whose issuer was already refused")
		return nil
	}
	secret := "hunter2"
	withUserinfo := strings.Replace(issuer.URL, "http://", "http://admin:"+secret+"@", 1)

	code := shell.Run([]string{"login", "--url", withUserinfo,
		"--client-id", "wso2-cli", "--context", "customer"})
	if code != exit.Usage {
		t.Fatalf("exit code = %d, want %d (usage); stderr: %s", code, exit.Usage, errOut)
	}
	requireRefusal(t, errOut.String(), "shell.invalid_argument")
	if strings.Contains(errOut.String(), secret) {
		t.Errorf("the refusal echoed what was in the URL's userinfo:\n%s", errOut)
	}
	if _, err := (session.Store{StateRoot: shell.StateRoot}).Load("customer"); err == nil {
		t.Error("a refused login left a session in the secure store")
	}
	if _, err := os.Stat(contexts.Path(shell.StateRoot)); !os.IsNotExist(err) {
		t.Error("a refused login wrote a context document")
	}
	if out.String() != "" {
		t.Errorf("a refused login wrote to standard output:\n%s", out)
	}
}

// TestARefusedIssuerURLIsNotEchoed keeps the refusal in step with the rule
// internal/contexts follows for the same reason: a value pasted where a URL
// belongs may be a credential, so a refusal names the flag and not the value.
func TestARefusedIssuerURLIsNotEchoed(t *testing.T) {
	shell, _, errOut := newLoginShell(t)
	pasted := "eyJhbGciOiJIUzI1NiJ9.pasted-where-a-url-belongs"

	if code := shell.Run([]string{"login", "--url", pasted, "--client-id", "x",
		"--context", "customer"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want %d (usage); stderr: %s", code, exit.Usage, errOut)
	}
	if strings.Contains(errOut.String(), pasted) {
		t.Errorf("the refusal echoed the rejected value:\n%s", errOut)
	}
	if !strings.Contains(errOut.String(), "--url") {
		t.Errorf("the refusal does not name --url:\n%s", errOut)
	}
}
