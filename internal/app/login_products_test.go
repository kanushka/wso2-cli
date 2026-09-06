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
	"net/http"
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
)

// thunderDoc is a resource-bound identity with two direct products and one
// federated product at a second issuer.
func thunderDoc(loginIssuer, productIssuer string) contexts.Document {
	document := browserDoc(loginIssuer)
	document.Identities[0].Auth.Provider = contexts.ProviderThunder
	document.Identities[0].Products = map[string]contexts.Product{
		"iam": {Endpoint: loginIssuer, Audience: "https://localhost:8090/mcp", Scopes: []string{"system"}},
		"gateway": {Endpoint: "https://gw.example", Audience: "http://localhost:18080/mockapi",
			Scopes: []string{"orders:read"}},
		"apim": {Endpoint: productIssuer, Audience: "apim-cli", Scopes: []string{"apim:api_view"},
			Grant: &contexts.Grant{Kind: contexts.GrantFederated, Issuer: productIssuer, ClientID: "apim-cli"}},
	}
	return document
}

// followBrowser answers every printed authorization URL by fetching it,
// as a browser holding the sign-on cookie would.
func followBrowser(shell *app.Shell) {
	shell.OpenBrowser = func(authURL string) error {
		go func() {
			if response, err := http.Get(authURL); err == nil {
				_ = response.Body.Close()
			}
		}()
		return nil
	}
}

func TestLoginEstablishesOneSessionPerProduct(t *testing.T) {
	keyring.MockInit()
	login := fakeissuer.New(t, fakeissuer.Options{RequireResource: true})
	product := fakeissuer.New(t, fakeissuer.Options{Audience: "apim-cli"})
	shell, out, errOut := newLoginShell(t)
	installLogin(t, shell, thunderDoc(login.URL, product.URL))
	followBrowser(&shell)

	if code := shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	store := session.Store{StateRoot: shell.StateRoot}
	bare, err := store.Load(credentialRef)
	if err != nil || bare.Strategy != contexts.StrategyDirect {
		t.Fatalf("login session %+v, %v", bare, err)
	}
	iam, err := store.Load(contexts.ProductSessionRef(credentialRef, "iam"))
	if err != nil || iam.Strategy != contexts.StrategySibling || iam.Issuer != login.URL {
		t.Fatalf("iam session %+v, %v", iam, err)
	}
	apim, err := store.Load(contexts.ProductSessionRef(credentialRef, "apim"))
	if err != nil || apim.Strategy != contexts.StrategyFederated || apim.Issuer != product.URL ||
		apim.ClientID != "apim-cli" {
		t.Fatalf("apim session %+v, %v", apim, err)
	}
	if _, err := store.Load(contexts.ProductSessionRef(credentialRef, "gateway")); err == nil {
		t.Fatal("the login product got a session of its own beside the login session")
	}
	for _, line := range []string{"gateway", "direct", "iam", "sibling", "apim", "federated"} {
		if !strings.Contains(out.String(), line) {
			t.Fatalf("report lacks %q:\n%s", line, out)
		}
	}
	if got := strings.Count(errOut.String(), "/authorize?"); got != 3 {
		t.Fatalf("printed %d authorization URLs, want 3:\n%s", got, errOut)
	}
}

func TestLoginOnlyEstablishesTheNamedProduct(t *testing.T) {
	keyring.MockInit()
	login := fakeissuer.New(t, fakeissuer.Options{RequireResource: true})
	product := fakeissuer.New(t, fakeissuer.Options{Audience: "apim-cli"})
	shell, _, errOut := newLoginShell(t)
	installLogin(t, shell, thunderDoc(login.URL, product.URL))
	followBrowser(&shell)
	if code := shell.Run([]string{"login", "--only", "iam"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	store := session.Store{StateRoot: shell.StateRoot}
	if _, err := store.Load(contexts.ProductSessionRef(credentialRef, "iam")); err != nil {
		t.Fatalf("iam session: %v", err)
	}
	if _, err := store.Load(credentialRef); err == nil {
		t.Fatal("--only iam established the login session too")
	}
}

func TestLoginNoProductsEstablishesTheLoginSessionAlone(t *testing.T) {
	keyring.MockInit()
	login := fakeissuer.New(t, fakeissuer.Options{RequireResource: true})
	product := fakeissuer.New(t, fakeissuer.Options{Audience: "apim-cli"})
	shell, _, errOut := newLoginShell(t)
	installLogin(t, shell, thunderDoc(login.URL, product.URL))
	followBrowser(&shell)
	if code := shell.Run([]string{"login", "--no-products"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	if got := strings.Count(errOut.String(), "/authorize?"); got != 1 {
		t.Fatalf("printed %d authorization URLs, want 1", got)
	}
}

func TestLoginOnlyRefusesAProductTheIdentityDoesNotRecord(t *testing.T) {
	keyring.MockInit()
	login := fakeissuer.New(t, fakeissuer.Options{RequireResource: true})
	shell, _, errOut := newLoginShell(t)
	installLogin(t, shell, thunderDoc(login.URL, login.URL))
	if code := shell.Run([]string{"login", "--only", "nothing"}); code != exit.Usage {
		t.Fatalf("exit %d, want usage; stderr %s", code, errOut)
	}
	if !strings.Contains(errOut.String(), "auth.product_not_configured") &&
		!strings.Contains(errOut.String(), "shell.invalid_argument") {
		t.Fatalf("stderr %s", errOut)
	}
}

// TestLoginNamesWhichSessionsSurviveAMidLoginFailure covers a login that gets
// partway through a thunderDoc identity's accesses before one of them fails.
// Accesses() orders them login (gateway, direct), apim (federated), iam
// (sibling) - see internal/contexts/access.go's Accesses, which starts from
// LoginAccess (the first direct product by namespace, here "gateway") and
// then walks the remaining namespaces alphabetically, skipping any whose
// session ref already equals the login's. With apim's issuer refusing
// discovery (no S256), apim is the second access attempted and the first to
// fail, so the loop returns before iam - the third access - is ever
// attempted. That leaves the login session established, apim not
// established, and iam neither established nor mentioned as a candidate to
// retry (it was never reached).
func TestLoginNamesWhichSessionsSurviveAMidLoginFailure(t *testing.T) {
	keyring.MockInit()
	login := fakeissuer.New(t, fakeissuer.Options{RequireResource: true})
	product := fakeissuer.New(t, fakeissuer.Options{Audience: "apim-cli", OmitS256: true})
	shell, _, errOut := newLoginShell(t)
	installLogin(t, shell, thunderDoc(login.URL, product.URL))
	followBrowser(&shell)

	if code := shell.Run([]string{"login"}); code == exit.OK {
		t.Fatalf("login succeeded despite apim's issuer refusing discovery; stderr %s", errOut)
	}

	store := session.Store{StateRoot: shell.StateRoot}
	if _, err := store.Load(credentialRef); err != nil {
		t.Fatalf("the login session was not established before apim failed: %v", err)
	}
	if _, err := store.Load(contexts.ProductSessionRef(credentialRef, "apim")); err == nil {
		t.Fatal("apim got a session despite its issuer refusing discovery")
	}
	if _, err := store.Load(contexts.ProductSessionRef(credentialRef, "iam")); err == nil {
		t.Fatal("iam was never attempted (apim fails first) yet got a session")
	}
	if !strings.Contains(errOut.String(), "--only apim") {
		t.Fatalf("stderr does not name the retry command for apim:\n%s", errOut)
	}
	if !strings.Contains(errOut.String(), "gateway") {
		t.Fatalf("stderr does not mention the established login session (gateway):\n%s", errOut)
	}
}
