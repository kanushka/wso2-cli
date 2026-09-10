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
	"net/http"
	"slices"
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
)

// removalAccount is the account every removal below runs against.
const removalAccount = "acme-cloud"

// removalDoc is a resource-bound account holding one record of every kind
// wso2 account remove-product has to tell apart: iam, the pinned login
// product; console, a direct product sharing the login session; api, a
// sibling with a session of its own at the login issuer; apim, federated at
// its own issuer and carrying a gateway record; agent, derived through a
// jwt-bearer assertion session; and ai, exchanged, holding no session at all.
func removalDoc(loginIssuer, productIssuer string) contexts.Document {
	document := browserDoc(loginIssuer)
	document.Accounts[0].Auth.Provider = contexts.ProviderThunder
	document.Accounts[0].LoginProduct = "iam"
	document.Accounts[0].Products = map[string]contexts.Product{
		"iam": {Endpoint: loginIssuer, Audience: "https://localhost:8090/mcp", Scopes: []string{"system"}},
		"console": {Endpoint: loginIssuer + "/console", Audience: "https://localhost:8090/mcp",
			Scopes: []string{"system"}},
		"api": {Endpoint: "https://api.example", Audience: "https://api.example", Scopes: []string{"api:read"}},
		"apim": {Endpoint: productIssuer, Audience: "apim-cli", Scopes: []string{"apim:api_view"},
			Grant: &contexts.Grant{Kind: contexts.GrantFederated, Issuer: productIssuer, ClientID: "apim-cli"},
			Gateway: &contexts.Gateway{Endpoint: gatewayURL, Audience: gatewayAudience,
				Scopes: []string{"hello:read"}}},
		"agent": {Endpoint: "https://agent.example", Audience: "agent-cli",
			Grant: &contexts.Grant{Kind: contexts.GrantJWTBearer, Issuer: productIssuer, ClientID: "agent-cli",
				Resource: "https://agent.example"}},
		"ai": {Endpoint: "https://ai.example", Audience: "https://ai.example",
			Grant: &contexts.Grant{Kind: contexts.GrantExchange}},
	}
	return document
}

// seededSession is one session a removal test stored, with the issuer that
// minted its refresh token, so the test can ask that issuer afterwards
// whether the token still works.
type seededSession struct {
	issuer       *fakeissuer.Issuer
	refreshToken string
}

// removalFixture is a shell over removalDoc with every session the account
// needs stored, each holding a refresh token its issuer really minted.
type removalFixture struct {
	shell    app.Shell
	store    session.Store
	login    *fakeissuer.Issuer
	product  *fakeissuer.Issuer
	sessions map[string]seededSession
}

func newRemovalFixture(t *testing.T) removalFixture {
	t.Helper()
	keyring.MockInit()
	login := fakeissuer.New(t, fakeissuer.Options{RequireResource: true})
	product := fakeissuer.New(t, fakeissuer.Options{Audience: "apim-cli"})
	shell, _, _ := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	t.Setenv("WSO2_NO_INPUT", "1")
	document := removalDoc(login.URL, product.URL)
	installLogin(t, shell, document)
	fixture := removalFixture{shell: shell, store: session.Store{StateRoot: shell.StateRoot},
		login: login, product: product, sessions: map[string]seededSession{}}
	for _, access := range document.Accounts[0].Accesses() {
		issuer := login
		if access.Issuer == product.URL {
			issuer = product
		}
		refreshToken := issuer.SeedSession(access.Scopes)
		if err := fixture.store.Save(access.SessionRef,
			session.Session{Issuer: access.Issuer, RefreshToken: refreshToken}); err != nil {
			t.Fatal(err)
		}
		fixture.sessions[access.SessionRef] = seededSession{issuer: issuer, refreshToken: refreshToken}
	}
	// Every record kind is seeded, so a removal that ended the wrong one is
	// caught by the survivors' assertions rather than passing by omission.
	for _, ref := range []string{credentialRef, productRef("api"), productRef("apim"),
		productRef(contexts.GatewayKey("apim")), productRef("agent")} {
		if _, seeded := fixture.sessions[ref]; !seeded {
			t.Fatalf("the fixture stored no session under %s: %v", ref, fixture.sessions)
		}
	}
	return fixture
}

// productRef is the secure-store entry a record of the fixture account keeps
// its own session under.
func productRef(key string) string { return contexts.ProductSessionRef(credentialRef, key) }

// run runs one command and returns what it wrote to each stream.
func (f removalFixture) run(t *testing.T, args ...string) (exit.Code, string, string) {
	t.Helper()
	out := &strings.Builder{}
	errOut := &strings.Builder{}
	shell := f.shell
	shell.Streams.Out = out
	shell.Streams.Err = errOut
	return shell.Run(args), out.String(), errOut.String()
}

// assertEnded asserts the session is gone from the secure store and its
// refresh token no longer works at the issuer that minted it.
func (f removalFixture) assertEnded(t *testing.T, ref string) {
	t.Helper()
	if stored, err := f.store.Stored(ref); err != nil || stored {
		t.Errorf("%s is still in the secure store (%v)", ref, err)
	}
	seeded := f.sessions[ref]
	if seeded.issuer.RefreshTokenLive(seeded.refreshToken) {
		t.Errorf("%s's refresh token still works at %s: it was not revoked", ref, seeded.issuer.URL)
	}
}

// assertKept asserts the session is still stored and still works.
func (f removalFixture) assertKept(t *testing.T, ref string) {
	t.Helper()
	if stored, err := f.store.Stored(ref); err != nil || !stored {
		t.Errorf("%s is gone from the secure store (%v)", ref, err)
	}
	seeded := f.sessions[ref]
	if !seeded.issuer.RefreshTokenLive(seeded.refreshToken) {
		t.Errorf("%s's refresh token was revoked", ref)
	}
}

// recordKeys is every record the fixture account holds now.
func (f removalFixture) recordKeys(t *testing.T) []string {
	t.Helper()
	return identityNamed(t, loadDocument(t, f.shell), removalAccount).RecordKeys()
}

// whoamiNamespaces is every record wso2 whoami reports for the account.
func (f removalFixture) whoamiNamespaces(t *testing.T) []string {
	t.Helper()
	code, out, errOut := f.run(t, "whoami", "--output", "json")
	if code != exit.OK {
		t.Fatalf("whoami exited %d: %s", code, errOut)
	}
	var report struct {
		Products []struct {
			Namespace string `json:"namespace"`
		} `json:"products"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("whoami did not render JSON: %v\n%s", err, out)
	}
	var namespaces []string
	for _, product := range report.Products {
		namespaces = append(namespaces, product.Namespace)
	}
	return namespaces
}

// doctorSession runs wso2 doctor and returns its session finding.
func (f removalFixture) doctorSession(t *testing.T) (exit.Code, string, string) {
	t.Helper()
	code, out, _ := f.run(t, "doctor", "--output", "json")
	finding := decodeDoctorReport(t, []byte(out)).findingFor(t, "session")
	return code, finding.Status, finding.Detail
}

func TestRemoveProductEndsAndRevokesAProductsOwnSessionBeforeDroppingItsRecord(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		ended []string
		gone  []string
	}{
		{"sibling", "api", []string{productRef("api")}, []string{"api"}},
		{"federated, with its gateway record", "apim",
			[]string{productRef("apim"), productRef(contexts.GatewayKey("apim"))},
			[]string{"apim", contexts.GatewayKey("apim")}},
		{"the gateway record alone", contexts.GatewayKey("apim"),
			[]string{productRef(contexts.GatewayKey("apim"))}, []string{contexts.GatewayKey("apim")}},
		{"derived", "agent", []string{productRef("agent")}, []string{"agent"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newRemovalFixture(t)
			code, out, errOut := fixture.run(t, "account", "remove-product", removalAccount, tc.key)
			if code != exit.OK {
				t.Fatalf("exit %d: %s", code, errOut)
			}
			for ref := range fixture.sessions {
				if slices.Contains(tc.ended, ref) {
					fixture.assertEnded(t, ref)
				} else {
					fixture.assertKept(t, ref)
				}
			}
			keys := fixture.recordKeys(t)
			for _, key := range tc.gone {
				if slices.Contains(keys, key) {
					t.Errorf("the account still records %s: %v", key, keys)
				}
				if slices.Contains(fixture.whoamiNamespaces(t), key) {
					t.Errorf("whoami still lists %s", key)
				}
			}
			if tc.key == contexts.GatewayKey("apim") && !slices.Contains(keys, "apim") {
				t.Errorf("removing the gateway record took its product with it: %v", keys)
			}
			if code, status, detail := fixture.doctorSession(t); code != exit.OK || status != "pass" {
				t.Errorf("doctor exited %d with the session check %s: %s", code, status, detail)
			}
			if !strings.Contains(out, "revocation confirmed") {
				t.Errorf("the report does not say the session was revoked:\n%s", out)
			}
		})
	}
}

// TestRemovingADirectProductLeavesTheLoginSessionIntact is the case that
// costs the most to get wrong. A direct product is reached through the login
// session itself, so ending "its" session would be ending the login: the user
// would be silently logged out of every other product, by a command that
// asked to stop reaching one.
func TestRemovingADirectProductLeavesTheLoginSessionIntact(t *testing.T) {
	fixture := newRemovalFixture(t)
	code, out, errOut := fixture.run(t, "account", "remove-product", removalAccount, "console")
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for ref := range fixture.sessions {
		fixture.assertKept(t, ref)
	}
	if slices.Contains(fixture.recordKeys(t), "console") {
		t.Fatalf("console is still recorded: %v", fixture.recordKeys(t))
	}
	if slices.Contains(fixture.whoamiNamespaces(t), "console") {
		t.Error("whoami still lists console")
	}
	if code, status, detail := fixture.doctorSession(t); code != exit.OK || status != "pass" {
		t.Errorf("doctor exited %d with the session check %s: %s", code, status, detail)
	}
	if !hasField(out, "Login session", "kept") || !hasField(out, "Sessions ended", "none") {
		t.Errorf("report:\n%s", out)
	}
}

// TestRemovingTheLoginProductIsRefusedBeforeAnythingIsTouched pins the rule
// that the product an account logs in through cannot be removed out from under
// it. The login session was authorized for that product's resource and scope
// set; removed, the session would answer for a product the account no longer
// records, and every command against whichever product became the login next
// would be refused until the next login. Moving the pin silently was tried and
// ended a working session for a product that happened to sort first, so the
// command refuses instead and says what a person can actually do.
func TestRemovingTheLoginProductIsRefusedBeforeAnythingIsTouched(t *testing.T) {
	fixture := newRemovalFixture(t)
	guardNetwork(t)
	before := fixture.recordKeys(t)
	code, _, errOut := fixture.run(t, "account", "remove-product", removalAccount, "iam")
	if code != exit.Usage {
		t.Fatalf("exit %d, want the usage class %d: %s", code, exit.Usage, errOut)
	}
	if !strings.Contains(errOut, "contexts.login_product") {
		t.Errorf("the refusal does not carry contexts.login_product:\n%s", errOut)
	}
	// Nothing changed: every session is where it was and every record stays.
	for ref := range fixture.sessions {
		fixture.assertKept(t, ref)
	}
	if after := fixture.recordKeys(t); !slices.Equal(before, after) {
		t.Errorf("records changed on a refusal: before %v, after %v", before, after)
	}
	if pin := identityNamed(t, loadDocument(t, fixture.shell), removalAccount).LoginProduct; pin != "iam" {
		t.Errorf("login product = %q, want it left on iam", pin)
	}
	// The recovery has to name commands that do the job. No command changes an
	// account's login product, so it must not send the reader to wso2 login
	// as though one did; it names creating an account that logs in through
	// the other product instead.
	if !strings.Contains(errOut, "wso2 account create") || !strings.Contains(errOut, "--product") {
		t.Errorf("the recovery does not name how to log in through another product:\n%s", errOut)
	}
}

func TestRemovingTheLoginProductsGatewayRecordIsAllowed(t *testing.T) {
	// Only the product's own record is the login. Its gateway record is a
	// second record the login never binds to, so removing it alone is fine.
	// The fixture's login product records no gateway, so one is added here:
	// without it this test would pass by being refused as an unknown record,
	// which proves nothing about the login rule.
	fixture := newRemovalFixture(t)
	if err := contexts.Update(fixture.shell.StateRoot, func(d contexts.Document) (contexts.Document, error) {
		for i := range d.Accounts {
			if d.Accounts[i].Name != removalAccount {
				continue
			}
			product := d.Accounts[i].Products["iam"]
			product.Gateway = &contexts.Gateway{Endpoint: "https://gw.example", Audience: "https://gw.example/api"}
			d.Accounts[i].Products["iam"] = product
		}
		return d, nil
	}); err != nil {
		t.Fatalf("giving the login product a gateway: %v", err)
	}
	code, _, errOut := fixture.run(t, "account", "remove-product", removalAccount, contexts.GatewayKey("iam"))
	if code != exit.OK {
		t.Fatalf("removing the login product's gateway record exited %d: %s", code, errOut)
	}
	if slices.Contains(fixture.recordKeys(t), contexts.GatewayKey("iam")) {
		t.Fatal("the gateway record is still recorded")
	}
	if !slices.Contains(fixture.recordKeys(t), "iam") {
		t.Fatal("removing the gateway record took the login product with it")
	}
	fixture.assertKept(t, credentialRef)
}

func TestRemovingAProductThatHoldsNoSessionEndsNothing(t *testing.T) {
	t.Run("exchanged", func(t *testing.T) {
		fixture := newRemovalFixture(t)
		guardNetwork(t)
		code, out, errOut := fixture.run(t, "account", "remove-product", removalAccount, "ai")
		if code != exit.OK {
			t.Fatalf("exit %d: %s", code, errOut)
		}
		for ref := range fixture.sessions {
			fixture.assertKept(t, ref)
		}
		if slices.Contains(fixture.recordKeys(t), "ai") {
			t.Fatalf("ai is still recorded: %v", fixture.recordKeys(t))
		}
		if !hasField(out, "Sessions ended", "none") {
			t.Errorf("report:\n%s", out)
		}
	})
	t.Run("client credentials", func(t *testing.T) {
		keyring.MockInit()
		shell, out, errOut := newShell(t)
		installLogin(t, shell, identityDoc(contexts.KindClientCredentials)("https://login.example"))
		guardNetwork(t)
		if code := shell.Run([]string{"account", "remove-product", removalAccount, "reference"}); code != exit.OK {
			t.Fatalf("exit %d: %s", code, errOut)
		}
		if products := identityNamed(t, loadDocument(t, shell), removalAccount).Products; len(products) != 0 {
			t.Fatalf("the product is still recorded: %+v", products)
		}
		if !hasField(out.String(), "Login session", "none") || !hasField(out.String(), "Sessions ended", "none") {
			t.Errorf("report:\n%s", out)
		}
	})
}

func TestRemovingARecordTheAccountDoesNotHoldIsRefusedNamingWhatItDoes(t *testing.T) {
	for _, key := range []string{"nosuch", contexts.GatewayKey("iam")} {
		t.Run(key, func(t *testing.T) {
			fixture := newRemovalFixture(t)
			guardNetwork(t)
			code, _, errOut := fixture.run(t, "account", "remove-product", removalAccount, key)
			if code != exit.Usage || !strings.Contains(errOut, "contexts.unknown_product") {
				t.Fatalf("exit %d, stderr:\n%s", code, errOut)
			}
			if !strings.Contains(errOut, "agent, ai, api, apim, apim/gateway, console, iam") {
				t.Errorf("the refusal does not name what the account records:\n%s", errOut)
			}
			for ref := range fixture.sessions {
				fixture.assertKept(t, ref)
			}
		})
	}
}

func TestRemoveProductRefusalsEndNoSession(t *testing.T) {
	cases := []struct {
		name string
		args []string
		code string
	}{
		{"unknown account", []string{"account", "remove-product", "nosuch", "api"}, "contexts.unknown_identity"},
		{"one argument", []string{"account", "remove-product", removalAccount}, "shell.missing_argument"},
		{"three arguments", []string{"account", "remove-product", removalAccount, "api", "apim"},
			"shell.unexpected_argument"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newRemovalFixture(t)
			guardNetwork(t)
			code, _, errOut := fixture.run(t, tc.args...)
			if code != exit.Usage || !strings.Contains(errOut, tc.code) {
				t.Fatalf("exit %d, stderr:\n%s", code, errOut)
			}
			for ref := range fixture.sessions {
				fixture.assertKept(t, ref)
			}
		})
	}
}

// A removal the document would refuse to write is refused before any session
// is ended. Ending first would leave the record in place with its session
// gone, which is a product the user asked to keep reaching nothing.
func TestARemovalTheDocumentRefusesEndsNoSession(t *testing.T) {
	keyring.MockInit()
	login := fakeissuer.New(t, fakeissuer.Options{RequireResource: true})
	product := fakeissuer.New(t, fakeissuer.Options{Audience: "apim-cli"})
	shell, _, errOut := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	document := removalDoc(login.URL, product.URL)
	// A deployment that binds a login to a product needs the account to
	// record at least one, so taking away the last is not a document the
	// shell writes.
	document.Accounts[0].LoginProduct = ""
	document.Accounts[0].Products = map[string]contexts.Product{"apim": document.Accounts[0].Products["apim"]}
	installLogin(t, shell, document)
	store := session.Store{StateRoot: shell.StateRoot}
	refreshToken := product.SeedSession([]string{"apim:api_view"})
	if err := store.Save(productRef("apim"), session.Session{Issuer: product.URL, RefreshToken: refreshToken}); err != nil {
		t.Fatal(err)
	}
	if code := shell.Run([]string{"account", "remove-product", removalAccount, "apim"}); code != exit.Usage {
		t.Fatalf("exit %d, want the usage class; stderr:\n%s", code, errOut)
	}
	if stored, err := store.Stored(productRef("apim")); err != nil || !stored {
		t.Errorf("the refused removal ended the session anyway (%v)", err)
	}
	if !product.RefreshTokenLive(refreshToken) {
		t.Error("the refused removal revoked the refresh token anyway")
	}
	if !strings.Contains(errOut.String(), "No session was ended") {
		t.Errorf("the refusal does not say nothing was ended:\n%s", errOut)
	}
}

func TestRemoveProductRendersJSON(t *testing.T) {
	fixture := newRemovalFixture(t)
	code, out, errOut := fixture.run(t, "--output", "json", "account", "remove-product", removalAccount, "apim")
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var removed struct {
		Account   string   `json:"account"`
		Namespace string   `json:"namespace"`
		Records   []string `json:"records"`
		Sessions  []struct {
			Record     string `json:"record"`
			Session    string `json:"session"`
			Revocation string `json:"revocation"`
		} `json:"sessions"`
		LoginSession string `json:"loginSession"`
	}
	if err := json.Unmarshal([]byte(out), &removed); err != nil {
		t.Fatalf("the output is not one JSON document: %v\n%s", err, out)
	}
	if removed.Account != removalAccount || removed.Namespace != "apim" ||
		!slices.Equal(removed.Records, []string{"apim", "apim/gateway"}) || removed.LoginSession != "kept" {
		t.Fatalf("result = %+v", removed)
	}
	if len(removed.Sessions) != 2 || removed.Sessions[0].Record != "apim" ||
		removed.Sessions[1].Record != "apim/gateway" {
		t.Fatalf("sessions = %+v", removed.Sessions)
	}
	for _, ended := range removed.Sessions {
		if ended.Session != "ended" || ended.Revocation != "confirmed" {
			t.Errorf("session = %+v, want ended and confirmed", ended)
		}
	}
}

// guardNetwork fails the test on any HTTP request the shell makes, for the
// rest of it: a removal that ends no session has nothing to revoke.
func guardNetwork(t *testing.T) {
	t.Helper()
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = failingTransport{t: t, family: "account"}
}

// TestADocumentChangedWhileSessionsAreEndedIsNotWritten covers the one race a
// command that revokes and then writes has to survive. Revocation reaches the
// network, so it runs outside the document lock, and another invocation may
// change the account in between. Here it gives the product being removed a
// gateway record with a session of its own. Removing the product now takes
// that record too, which would strand a session this run never ended — and the
// secure store cannot be listed, so nothing could ever find it again. The
// command re-plans under the lock, sees it, and refuses rather than dropping
// the record.
func TestADocumentChangedWhileSessionsAreEndedIsNotWritten(t *testing.T) {
	fixture := newRemovalFixture(t)
	gatewayRef := productRef(contexts.GatewayKey("api"))
	fixture.login.OnRevoke(func() {
		if err := contexts.Update(fixture.shell.StateRoot, func(d contexts.Document) (contexts.Document, error) {
			product := d.Accounts[0].Products["api"]
			product.Gateway = &contexts.Gateway{Endpoint: "https://api-gw.example",
				Audience: "https://api-gw.example/hello", Scopes: []string{"hello:read"}}
			d.Accounts[0].Products["api"] = product
			return d, nil
		}); err != nil {
			t.Errorf("the concurrent change could not be written: %v", err)
		}
		if err := fixture.store.Save(gatewayRef, session.Session{
			Issuer: fixture.login.URL, RefreshToken: fixture.login.SeedSession([]string{"hello:read"}),
		}); err != nil {
			t.Errorf("the concurrent session could not be stored: %v", err)
		}
	})

	code, _, errOut := fixture.run(t, "account", "remove-product", removalAccount, "api")
	if code == exit.OK {
		t.Fatal("the removal was written over a document that changed under it")
	}
	if !strings.Contains(errOut, "contexts.document_busy") {
		t.Errorf("the refusal does not carry contexts.document_busy:\n%s", errOut)
	}
	// The record was not dropped, so the session the other invocation stored
	// is still named by a record and can still be ended.
	if !slices.Contains(fixture.recordKeys(t), "api") {
		t.Error("the product was removed despite the refusal")
	}
	if !slices.Contains(fixture.recordKeys(t), contexts.GatewayKey("api")) {
		t.Error("the gateway record the other invocation wrote was lost")
	}
	if stored, err := fixture.store.Stored(gatewayRef); err != nil || !stored {
		t.Errorf("the other invocation's session was ended by a run that never planned it: %v", err)
	}
}
