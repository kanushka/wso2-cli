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
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/contexts/fixture"
	"github.com/wso2/wso2-cli/internal/exit"
)

// credentialRef is the secure-store entry the login fixtures store under.
const credentialRef = "acme-cloud-login"

// browserDoc is the document a browser login runs against.
func browserDoc(issuerURL string) contexts.Document {
	return contexts.Document{
		SchemaVersion:  contexts.SchemaVersion,
		DefaultContext: "acme-dev",
		Identities: []contexts.Identity{{
			Name: "acme-cloud",
			Type: "cloud",
			Auth: contexts.IdentityAuth{
				Kind:          contexts.KindOAuthBrowser,
				Issuer:        issuerURL,
				ClientID:      "client-123",
				Tenant:        "acme",
				CredentialRef: credentialRef,
			},
			Products: map[string]contexts.Product{
				"reference": {
					Endpoint: "https://reference.example.test",
					Audience: "reference-status",
					Scopes:   []string{"reference:status:read"},
				},
			},
		}},
		Contexts: []contexts.Context{{Name: "acme-dev", Identity: "acme-cloud", Organization: "acme"}},
	}
}

// identityDoc is a document declaring one identity of the given kind.
func identityDoc(kind string) func(string) contexts.Document {
	return func(issuerURL string) contexts.Document {
		document := browserDoc(issuerURL)
		document.Identities[0].Auth.Kind = kind
		if kind == contexts.KindClientCredentials {
			document.Identities[0].Auth.CredentialRef = ""
			document.Identities[0].Auth.ClientSecretVariable = "WSO2_ACME_CLIENT_SECRET"
		}
		return document
	}
}

// installLogin writes a v2 context document into the shell's isolated state.
func installLogin(t *testing.T, shell app.Shell, document contexts.Document) {
	t.Helper()
	if err := fixture.WriteV2(shell.StateRoot, document); err != nil {
		t.Fatalf("install context document: %v", err)
	}
}

// newLoginShell is a shell with the login environment neutralized, so a
// developer's own WSO2 variables cannot decide what a test proves. Its browser
// hook fails the test if it is reached, which is how every refusal below proves
// it refused before starting a login rather than during one.
func newLoginShell(t *testing.T) (app.Shell, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	t.Setenv("WSO2_CONTEXT", "")
	t.Setenv("WSO2_NO_INPUT", "")
	shell, out, errOut := newShell(t)
	shell.OpenBrowser = func(string) error {
		t.Error("the shell opened a browser for a login it should have refused")
		return nil
	}
	return shell, out, errOut
}

func TestLoginRefusals(t *testing.T) {
	cases := []struct {
		name string
		doc  func(string) contexts.Document // nil installs no document
		args []string
		env  map[string]string
		code string
	}{
		{"no context", nil, nil, nil, "auth.context_not_selected"},
		{"no-input flag", browserDoc, []string{"--no-input"}, nil, "auth.non_interactive"},
		{"no-input environment", browserDoc, nil,
			map[string]string{"WSO2_NO_INPUT": "1"}, "auth.non_interactive"},
		{"client credentials", identityDoc(contexts.KindClientCredentials), nil, nil, "auth.login_not_required"},
		{"no-input with an inline identity", identityDoc(contexts.KindClientCredentials),
			[]string{"--no-input"}, nil, "auth.login_not_required"},
		// A device login is interactive too, so it is refused in CI for the
		// same reason a browser login is: nothing may wait on a human there.
		// Both doors are checked, because a job that sets the variable rather
		// than passing the flag is the commoner of the two.
		{"no-input flag with a device identity", identityDoc(contexts.KindOAuthDevice),
			[]string{"--no-input"}, nil, "auth.non_interactive"},
		{"no-input environment with a device identity", identityDoc(contexts.KindOAuthDevice),
			nil, map[string]string{"WSO2_NO_INPUT": "1"}, "auth.non_interactive"},
		{"personal access token kind", identityDoc(contexts.KindPAT), nil, nil, "auth.kind_not_implemented"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			shell, out, errOut := newLoginShell(t)
			for name, value := range testCase.env {
				t.Setenv(name, value)
			}
			if testCase.doc != nil {
				installLogin(t, shell, testCase.doc("https://issuer.example.test"))
			}

			if code := shell.Run(append([]string{"login"}, testCase.args...)); code != exit.AuthPolicy {
				t.Fatalf("exit code = %d, want %d (auth policy); stderr: %s", code, exit.AuthPolicy, errOut)
			}
			requireRefusal(t, errOut.String(), testCase.code)
			if out.String() != "" {
				t.Fatalf("a refused login wrote to standard output:\n%s", out)
			}
		})
	}
}

// TestLoginRefusesADevelopmentCredentialContext covers the synthetic identity a
// schema version 1 document is read as: it acquires access inline, so it has no
// login step either.
func TestLoginRefusesADevelopmentCredentialContext(t *testing.T) {
	shell, _, errOut := newLoginShell(t)
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

	if code := shell.Run([]string{"login"}); code != exit.AuthPolicy {
		t.Fatalf("exit code = %d, want %d (auth policy); stderr: %s", code, exit.AuthPolicy, errOut)
	}
	requireRefusal(t, errOut.String(), "auth.login_not_required")
}

// TestTheNoInputRefusalNamesTheControlThatFired covers the hint a developer
// needs most: the environment variable can have been set in a shell profile
// months ago, and a refusal that named neither control left them nothing to
// search for.
func TestTheNoInputRefusalNamesTheControlThatFired(t *testing.T) {
	for _, testCase := range []struct {
		name string
		args []string
		env  string
		want string
	}{
		{"the flag", []string{"--no-input"}, "", "--no-input"},
		{"the environment variable", nil, "1", "WSO2_NO_INPUT"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			shell, _, errOut := newLoginShell(t)
			if testCase.env != "" {
				t.Setenv("WSO2_NO_INPUT", testCase.env)
			}
			installLogin(t, shell, browserDoc("https://issuer.example.test"))

			if code := shell.Run(append([]string{"login"}, testCase.args...)); code != exit.AuthPolicy {
				t.Fatalf("exit code = %d, want %d (auth policy); stderr: %s", code, exit.AuthPolicy, errOut)
			}
			requireRefusal(t, errOut.String(), "auth.non_interactive")
			if !strings.Contains(errOut.String(), testCase.want) {
				t.Errorf("the refusal does not name %s:\n%s", testCase.want, errOut)
			}
			// The phrase that ties the message to auth.non_interactive and to
			// the command reference survives either way.
			if !strings.Contains(errOut.String(), "non-interactive mode") {
				t.Errorf("the refusal no longer names non-interactive mode:\n%s", errOut)
			}
		})
	}
}

func TestLoginRefusesUnknownArguments(t *testing.T) {
	// --non-interactive is the spelling wso2 login shipped before #122 renamed
	// it. It is listed here so the rename cannot be half-applied: a build that
	// still answered to it would fail this test rather than quietly accept both.
	for _, argument := range []string{"--nope", "extra", "--non-interactive"} {
		t.Run(argument, func(t *testing.T) {
			shell, _, errOut := newLoginShell(t)
			installLogin(t, shell, browserDoc("https://issuer.example.test"))

			if code := shell.Run([]string{"login", argument}); code != exit.Usage {
				t.Fatalf("exit code = %d, want %d (usage); stderr: %s", code, exit.Usage, errOut)
			}
			if !strings.Contains(errOut.String(), "wso2 login") {
				t.Fatalf("the refusal does not say how to run login:\n%s", errOut)
			}
		})
	}
}

func TestLoginHappyPathStoresSessionAndReportsIdentity(t *testing.T) {
	keyring.MockInit()
	// No AllowAnyLoopbackPort: the shell must land on one of the four
	// registered callback URLs, or the issuer refuses the redirect outright.
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

	stored, err := session.Store{StateRoot: shell.StateRoot}.Load(credentialRef)
	if err != nil {
		t.Fatalf("session not stored: %v", err)
	}
	if stored.Issuer != issuer.URL {
		t.Fatalf("stored session names the issuer %q, want %q", stored.Issuer, issuer.URL)
	}
	if stored.RefreshToken == "" {
		t.Fatal("the stored session holds no refresh token")
	}
	if stored.ExpiresAt.IsZero() {
		t.Fatal("the stored session holds no expiry")
	}

	for _, expected := range []string{"user-1", "dev@example.test", "reference"} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("the login report is missing %q in:\n%s", expected, out)
		}
	}
	// The URL is printed on the way in, whatever the browser did with it. It
	// belongs on the diagnostic stream: it is an instruction, not the result.
	if !strings.Contains(errOut.String(), issuer.URL+"/authorize") {
		t.Fatalf("the authorization URL was not printed:\n%s", errOut)
	}
	if strings.Contains(out.String(), "/authorize") {
		t.Fatalf("the authorization URL polluted the result stream:\n%s", out)
	}
	for _, secret := range []string{stored.RefreshToken, stored.AccessToken} {
		if secret == "" {
			continue
		}
		if strings.Contains(out.String(), secret) || strings.Contains(errOut.String(), secret) {
			t.Fatal("token material leaked into the login output")
		}
	}
}

// TestLoginCompletesFromThePrintedURL proves the printed URL is enough: the
// browser hook fails outright, and the login still finishes when the URL it
// printed is followed.
func TestLoginCompletesFromThePrintedURL(t *testing.T) {
	keyring.MockInit()
	issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "reference-status"})
	shell, _, _ := newLoginShell(t)
	installLogin(t, shell, browserDoc(issuer.URL))
	printed := &syncBuilder{}
	shell.Streams.Err = printed
	shell.OpenBrowser = func(string) error {
		return errNoBrowser
	}

	codes := make(chan exit.Code, 1)
	go func() { codes <- shell.Run([]string{"login"}) }()

	response, err := http.Get(waitForURL(t, printed))
	if err != nil {
		t.Fatalf("follow the printed URL: %v", err)
	}
	_ = response.Body.Close()

	if code := <-codes; code != exit.OK {
		t.Fatalf("login driven from the printed URL failed: exit %d, stderr %s", code, printed)
	}
	if _, err := (session.Store{StateRoot: shell.StateRoot}).Load(credentialRef); err != nil {
		t.Fatalf("session not stored: %v", err)
	}
}

// TestLoginSelectsTheContextNamedByTheFlag proves --context overrides the
// default rather than merely being read: the default context would start a
// browser login, and the named one has no login step at all.
func TestLoginSelectsTheContextNamedByTheFlag(t *testing.T) {
	shell, _, errOut := newLoginShell(t)
	document := browserDoc("https://issuer.example.test")
	document.Identities = append(document.Identities, contexts.Identity{
		Name: "acme-ci",
		Type: "cloud",
		Auth: contexts.IdentityAuth{
			Kind:                 contexts.KindClientCredentials,
			Issuer:               "https://issuer.example.test",
			ClientID:             "client-ci",
			ClientSecretVariable: "WSO2_ACME_CLIENT_SECRET",
		},
	})
	document.Contexts = append(document.Contexts,
		contexts.Context{Name: "acme-ci", Identity: "acme-ci", Organization: "acme"})
	installLogin(t, shell, document)

	if code := shell.Run([]string{"login", "--context", "acme-ci"}); code != exit.AuthPolicy {
		t.Fatalf("exit code = %d, want %d (auth policy); stderr: %s", code, exit.AuthPolicy, errOut)
	}
	// The default context is a browser identity, so this code can only come
	// from the context the flag named.
	requireRefusal(t, errOut.String(), "auth.login_not_required")
}

func TestLoginRefusesAContextTheDocumentDoesNotDeclare(t *testing.T) {
	shell, _, errOut := newLoginShell(t)
	installLogin(t, shell, browserDoc("https://issuer.example.test"))

	if code := shell.Run([]string{"login", "--context", "nonexistent"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want %d (usage); stderr: %s", code, exit.Usage, errOut)
	}
	if !strings.Contains(errOut.String(), "contexts.unknown_context") {
		t.Fatalf("an undeclared context was not refused by name:\n%s", errOut)
	}
}

// TestLoginRefusesAnIssuerWithoutS256 proves the discovery refusal row reaches
// the user through wso2 login, with the auth-policy exit class, and not only
// through the flow package that raises it.
func TestLoginRefusesAnIssuerWithoutS256(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "reference-status", OmitS256: true})
	shell, _, errOut := newLoginShell(t)
	installLogin(t, shell, browserDoc(issuer.URL))
	shell.OpenBrowser = func(string) error {
		t.Error("the shell opened a browser against an issuer without S256")
		return nil
	}

	if code := shell.Run([]string{"login"}); code != exit.AuthPolicy {
		t.Fatalf("exit code = %d, want %d (auth policy); stderr: %s", code, exit.AuthPolicy, errOut)
	}
	requireRefusal(t, errOut.String(), "auth.discovery_failed")
}

// TestLoginRefusesALoginThatYieldsNoRefreshToken covers an issuer that never
// granted offline access: storing the access token alone would leave a session
// that expires in minutes and cannot renew itself.
func TestLoginRefusesALoginThatYieldsNoRefreshToken(t *testing.T) {
	keyring.MockInit()
	issuer := fakeissuer.New(t, fakeissuer.Options{
		Audience: "reference-status", OmitRefreshToken: true,
	})
	shell, _, errOut := newLoginShell(t)
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

	if code := shell.Run([]string{"login"}); code != exit.AuthPolicy {
		t.Fatalf("exit code = %d, want %d (auth policy); stderr: %s", code, exit.AuthPolicy, errOut)
	}
	requireRefusal(t, errOut.String(), "auth.credential_unavailable")
	if _, err := (session.Store{StateRoot: shell.StateRoot}).Load(credentialRef); err == nil {
		t.Fatal("a login without a refresh token was stored as a session")
	}
}

// requireRefusal asserts the rendered problem carries the stable code and
// actionable recovery text.
func requireRefusal(t *testing.T, rendered, code string) {
	t.Helper()
	if !strings.Contains(rendered, "("+code+")") {
		t.Fatalf("expected the problem %q in:\n%s", code, rendered)
	}
	lines := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[1]) == "" {
		t.Fatalf("the problem %q states no recovery:\n%s", code, rendered)
	}
}
