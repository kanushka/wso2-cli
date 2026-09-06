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
	"time"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
)

func TestLogoutEndsEveryProductSession(t *testing.T) {
	keyring.MockInit()
	login := fakeissuer.New(t, fakeissuer.Options{RequireResource: true})
	product := fakeissuer.New(t, fakeissuer.Options{Audience: "apim-cli"})
	shell, out, errOut := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	installLogin(t, shell, thunderDoc(login.URL, product.URL))
	store := session.Store{StateRoot: shell.StateRoot}
	for ref, issuer := range map[string]string{
		credentialRef: login.URL,
		contexts.ProductSessionRef(credentialRef, "iam"):  login.URL,
		contexts.ProductSessionRef(credentialRef, "apim"): product.URL,
	} {
		if err := store.Save(ref, session.Session{Issuer: issuer, RefreshToken: "rt-" + ref}); err != nil {
			t.Fatal(err)
		}
	}
	if code := shell.Run([]string{"logout"}); code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, ref := range []string{credentialRef, contexts.ProductSessionRef(credentialRef, "iam"),
		contexts.ProductSessionRef(credentialRef, "apim")} {
		if _, err := store.Load(ref); err == nil {
			t.Fatalf("%s survived logout", ref)
		}
	}
	if !strings.Contains(out.String(), "apim ended") || !strings.Contains(out.String(), "iam ended") {
		t.Fatalf("report:\n%s", out)
	}
}

func TestLogoutOnAClientCredentialsIdentityExitsCleanly(t *testing.T) {
	keyring.MockInit()
	shell, out, errOut := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	installLogin(t, shell, identityDoc(contexts.KindClientCredentials)("http://login.example"))
	if code := shell.Run([]string{"logout"}); code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out.String(), "none") {
		t.Fatalf("report:\n%s", out)
	}
}

// seedSignedInSessions stores a login session, a sibling and a federated
// product session, each with the identity token its authorization returned.
func seedSignedInSessions(t *testing.T, shell app.Shell, login, product *fakeissuer.Issuer) {
	t.Helper()
	installLogin(t, shell, thunderDoc(login.URL, product.URL))
	store := session.Store{StateRoot: shell.StateRoot}
	for ref, stored := range map[string]session.Session{
		credentialRef: {Issuer: login.URL, RefreshToken: "rt-login", IDToken: "idt-login"},
		contexts.ProductSessionRef(credentialRef, "iam"):  {Issuer: login.URL, RefreshToken: "rt-iam", IDToken: "idt-iam"},
		contexts.ProductSessionRef(credentialRef, "apim"): {Issuer: product.URL, RefreshToken: "rt-apim", IDToken: "idt-apim"},
	} {
		if err := store.Save(ref, stored); err != nil {
			t.Fatal(err)
		}
	}
}

// Ending the shell's sessions leaves the providers' browser sessions in
// place, and a later login is then silent. So logout also opens each
// provider's end-session page, once per provider, naming the client and the
// identity token the session was established with.
func TestLogoutOpensEachProvidersSignOutPage(t *testing.T) {
	keyring.MockInit()
	login := fakeissuer.New(t, fakeissuer.Options{RequireResource: true})
	product := fakeissuer.New(t, fakeissuer.Options{Audience: "apim-cli"})
	shell, out, errOut := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	t.Setenv("WSO2_NO_INPUT", "")
	seedSignedInSessions(t, shell, login, product)
	followBrowser(&shell)
	if code := shell.Run([]string{"logout"}); code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	waitForLogouts(t, login, 1)
	waitForLogouts(t, product, 1)
	if got := login.LogoutRequests()[0]; got.Get("client_id") != "client-123" || got.Get("id_token_hint") != "idt-login" {
		t.Fatalf("login provider was told %v", got)
	}
	if got := product.LogoutRequests()[0]; got.Get("client_id") != "apim-cli" || got.Get("id_token_hint") != "idt-apim" {
		t.Fatalf("product provider was told %v", got)
	}
	if !strings.Contains(out.String(), "sign-out opened") {
		t.Fatalf("report:\n%s", out)
	}
	if !strings.Contains(errOut.String(), login.URL+"/logout?") || !strings.Contains(errOut.String(), product.URL+"/logout?") {
		t.Fatalf("the sign-out URLs were not printed:\n%s", errOut)
	}
}

func TestLogoutKeepsTheBrowserSessionWhenAsked(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		env  string
		want string
	}{
		{"flag", []string{"logout", "--keep-browser-session"}, "", "kept"},
		{"no-input", []string{"logout"}, "1", "kept"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keyring.MockInit()
			login := fakeissuer.New(t, fakeissuer.Options{RequireResource: true})
			product := fakeissuer.New(t, fakeissuer.Options{Audience: "apim-cli"})
			shell, out, errOut := newShell(t)
			t.Setenv("WSO2_CONTEXT", "")
			t.Setenv("WSO2_NO_INPUT", tc.env)
			seedSignedInSessions(t, shell, login, product)
			shell.OpenBrowser = func(string) error {
				t.Error("logout opened a browser it was asked not to")
				return nil
			}
			if code := shell.Run(tc.args); code != exit.OK {
				t.Fatalf("exit %d: %s", code, errOut)
			}
			if len(login.LogoutRequests())+len(product.LogoutRequests()) != 0 {
				t.Fatal("a provider received an end-session request")
			}
			if line := browserSessionLine(out.String()); !strings.HasSuffix(strings.TrimSpace(line), tc.want) {
				t.Fatalf("report:\n%s", out)
			}
		})
	}
}

// waitForLogouts waits for the browser stub's asynchronous fetch to land.
func waitForLogouts(t *testing.T, issuer *fakeissuer.Issuer, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(issuer.LogoutRequests()) >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the provider at %s received %d end-session requests, want %d", issuer.URL, len(issuer.LogoutRequests()), want)
}

// browserSessionLine is the report line carrying the browser-session outcome.
func browserSessionLine(report string) string {
	for _, line := range strings.Split(report, "\n") {
		if strings.HasPrefix(line, "Browser session") {
			return line
		}
	}
	return ""
}
