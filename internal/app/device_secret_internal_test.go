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

package app

import (
	"bytes"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/contexts/fixture"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/sdk/problem"
)

const (
	deviceRef            = "device-login"
	deviceFirstAudience  = "https://first.example/api"
	deviceSecondAudience = "https://second.example/api"
)

var deviceSecondScopes = []string{"second:read", "second:write"}

// deviceSecretLogin is a shell against a ThunderID-like issuer, recording
// every authorization it opens the browser for.
type deviceSecretLogin struct {
	shell  Shell
	issuer *fakeissuer.Issuer
	out    *bytes.Buffer
	errOut *bytes.Buffer
	mutex  sync.Mutex
	opened []url.Values
}

func newDeviceSecretLogin(t *testing.T, options fakeissuer.Options, provider string,
	edits ...func(*contexts.Login)) *deviceSecretLogin {
	t.Helper()
	keyring.MockInit()
	for _, name := range []string{"WSO2_NO_INPUT", "WSO2_CONTEXT", CloudEnvVar} {
		t.Setenv(name, "")
	}
	options.RequireResource = provider == contexts.ProviderThunder
	issuer := fakeissuer.New(t, options)
	login := &deviceSecretLogin{issuer: issuer, out: &bytes.Buffer{}, errOut: &bytes.Buffer{}}
	login.shell = Shell{StateRoot: t.TempDir(), Streams: output.Streams{Out: login.out, Err: login.errOut}}
	login.shell.OpenBrowser = func(authURL string) error {
		if parsed, err := url.Parse(authURL); err == nil {
			login.mutex.Lock()
			login.opened = append(login.opened, parsed.Query())
			login.mutex.Unlock()
		}
		go func() {
			if response, err := http.Get(authURL); err == nil {
				_ = response.Body.Close()
			}
		}()
		return nil
	}
	document := contexts.Document{
		SchemaVersion: contexts.SchemaVersion, DefaultContext: "cloud",
		Contexts: []contexts.Context{{Name: "cloud", Type: contexts.TypeCloud, CredentialRef: deviceRef,
			Login: contexts.Login{Kind: contexts.KindOAuthBrowser, Issuer: issuer.URL, ClientID: "WSO2_CLI",
				Provider: provider},
			Products: map[string]contexts.Product{
				"first":  {Endpoint: "https://first.example", Audience: deviceFirstAudience, Scopes: []string{"first:read"}},
				"second": {Endpoint: "https://second.example", Audience: deviceSecondAudience, Scopes: deviceSecondScopes},
			}}},
	}
	for _, edit := range edits {
		edit(&document.Contexts[0].Login)
	}
	if err := fixture.WriteV2(login.shell.StateRoot, document); err != nil {
		t.Fatal(err)
	}
	return login
}

func productSessions(method string) func(*contexts.Login) {
	return func(login *contexts.Login) { login.ProductSessions = method }
}

// refusedForTheSetting asserts err is the refusal that names the
// productSessions setting, and that nothing was opened in the browser.
func (d *deviceSecretLogin) refusedForTheSetting(t *testing.T, err error, opensBefore int) {
	t.Helper()
	var refusal problem.Problem
	if !errors.As(err, &refusal) || refusal.Code != "auth.device_secret_unavailable" {
		t.Fatalf("error = %v, want auth.device_secret_unavailable", err)
	}
	if !strings.Contains(refusal.Recovery, "productSessions: browser") {
		t.Errorf("the recovery does not name the setting: %q", refusal.Recovery)
	}
	if got := d.browserOpens(); got != opensBefore {
		t.Errorf("opened the browser %d times, want %d: no silent fallback", got, opensBefore)
	}
}

func (d *deviceSecretLogin) run(t *testing.T, args ...string) {
	t.Helper()
	if code := d.shell.Run(args); code != 0 {
		t.Fatalf("%v: exit %d, stderr %s", args, code, d.errOut)
	}
}

func (d *deviceSecretLogin) browserOpens() int {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	return len(d.opened)
}

func (d *deviceSecretLogin) askedForDeviceSecret() bool {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	for _, query := range d.opened {
		if slices.Contains(strings.Fields(query.Get("scope")), "device_sso") {
			return true
		}
	}
	return false
}

func (d *deviceSecretLogin) load(t *testing.T, ref string) session.Session {
	t.Helper()
	stored, err := session.Store{StateRoot: d.shell.StateRoot}.Load(ref)
	if err != nil {
		t.Fatalf("session %s: %v", ref, err)
	}
	return stored
}

func (d *deviceSecretLogin) selection(t *testing.T) contexts.Selection {
	t.Helper()
	document, err := contexts.Load(d.shell.StateRoot)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := document.Select("")
	if err != nil {
		t.Fatal(err)
	}
	return selected
}

// acquireSecond is what a module of the second product gets in a command.
func (d *deviceSecretLogin) acquireSecond(t *testing.T) auth.Grant {
	t.Helper()
	selected := d.selection(t)
	broker := &auth.Broker{
		Namespace: "second",
		Capabilities: modules.Capabilities{AuthAudiences: []string{deviceSecondAudience},
			AuthScopes: deviceSecondScopes},
		Selection: selected, InvocationID: "invocation", StateRoot: d.shell.StateRoot,
	}
	grant, err := broker.Acquire(auth.Request{Audience: deviceSecondAudience})
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	return grant
}

func TestALoginThatReturnsADeviceSecretOpensTheBrowserOnce(t *testing.T) {
	login := newDeviceSecretLogin(t, fakeissuer.Options{DeviceSSO: true}, contexts.ProviderThunder)

	login.run(t, "--verbose", "login")

	if got := login.browserOpens(); got != 1 {
		t.Fatalf("opened the browser %d times, want once", got)
	}
	if !strings.Contains(login.errOut.String(), "establishing a product session from the device secret") {
		t.Fatalf("the verbose log has no device secret lines to search:\n%s", login.errOut)
	}
	first := login.load(t, deviceRef)
	if first.DeviceSecret == "" || time.Until(first.DeviceSecretExpiresAt) < 23*time.Hour {
		t.Fatalf("login session secret present=%v expiry=%v, want a secret good for a day",
			first.DeviceSecret != "", first.DeviceSecretExpiresAt)
	}
	second := login.load(t, contexts.ProductSessionRef(deviceRef, "second"))
	if second.Resource != deviceSecondAudience || !slices.Equal(second.Scopes, deviceSecondScopes) ||
		second.Strategy != contexts.StrategySibling || second.EstablishedBy != session.EstablishedByDeviceSecret ||
		second.DeviceSecret != "" {
		t.Fatalf("second session = resource %q scopes %v strategy %q established %q, secret copied %v",
			second.Resource, second.Scopes, second.Strategy, second.EstablishedBy, second.DeviceSecret != "")
	}

	grant := login.acquireSecond(t)
	active, scopes, audiences := login.issuer.Introspect(t, grant.Token)
	slices.Sort(scopes)
	if !active || !slices.Equal(audiences, []string{deviceSecondAudience}) || !slices.Equal(scopes, deviceSecondScopes) {
		t.Errorf("second token active=%v aud=%v scopes=%v", active, audiences, scopes)
	}

	login.out.Reset()
	login.run(t, "whoami", "--output", "json")
	if !strings.Contains(login.out.String(), `"establishedBy": "device-secret"`) {
		t.Errorf("whoami does not say how the second session was established:\n%s", login.out)
	}

	document, err := os.ReadFile(filepath.Join(login.shell.StateRoot, "cli", contexts.FileName))
	if err != nil {
		t.Fatal(err)
	}
	for where, text := range map[string]string{
		"stdout": login.out.String(), "stderr": login.errOut.String(), "context document": string(document),
		"module grant": grant.Token,
	} {
		if strings.Contains(text, first.DeviceSecret) {
			t.Errorf("the device secret reached the %s", where)
		}
	}
}

func TestAWithheldScopeIsNamedForASessionFromTheDeviceSecret(t *testing.T) {
	login := newDeviceSecretLogin(t, fakeissuer.Options{DeviceSSO: true, WithheldScopes: []string{"second:write"}},
		contexts.ProviderThunder)

	login.run(t, "login")

	if !strings.Contains(login.out.String(), "second (without second:write)") {
		t.Errorf("the login report does not name the withheld scope:\n%s", login.out)
	}
	if strings.Contains(login.errOut.String(), "left out") {
		t.Errorf("the withheld scope is named twice, on stderr as well:\n%s", login.errOut)
	}
}

func TestAReusedBrowserSessionWithNoSecretRefusesTheNextProduct(t *testing.T) {
	login := newDeviceSecretLogin(t, fakeissuer.Options{DeviceSSO: true, ReusedBrowserSession: true},
		contexts.ProviderThunder)

	if code := login.shell.Run([]string{"login"}); code != 77 {
		t.Fatalf("login exit %d, want 77; stderr %s", code, login.errOut)
	}
	if !strings.Contains(login.errOut.String(), "auth.device_secret_unavailable") ||
		!strings.Contains(login.errOut.String(), "productSessions: browser") {
		t.Errorf("the refusal does not name the code and the setting:\n%s", login.errOut)
	}
	if strings.Contains(login.errOut.String(), "--only second") {
		t.Errorf("the refusal offers a retry that presents the same missing secret:\n%s", login.errOut)
	}
	if got := login.browserOpens(); got != 1 {
		t.Errorf("opened the browser %d times, want only the sign-in", got)
	}
	if _, err := (session.Store{StateRoot: login.shell.StateRoot}).Load(
		contexts.ProductSessionRef(deviceRef, "second")); err == nil {
		t.Error("the second product got a session though no secret was issued")
	}
}

func TestProductSessionsByBrowserAuthorizeEachProductInOneBrowserTab(t *testing.T) {
	login := newDeviceSecretLogin(t, fakeissuer.Options{DeviceSSO: true, ReusedBrowserSession: true},
		contexts.ProviderThunder, productSessions(contexts.ProductSessionsBrowser))

	login.run(t, "login")

	if got := login.browserOpens(); got != 1 {
		t.Fatalf("opened the browser %d times, want once for both products", got)
	}
	if login.askedForDeviceSecret() {
		t.Error("a login that authorizes products in the browser asked for a device secret")
	}
	if second := login.load(t, contexts.ProductSessionRef(deviceRef, "second")); second.EstablishedBy != "" ||
		second.Resource != deviceSecondAudience {
		t.Errorf("second session = %+v, want a browser authorization bound to its audience", second.Resource)
	}
}

func TestProductSessionsByBrowserNeverExchangeASecret(t *testing.T) {
	login := newDeviceSecretLogin(t, fakeissuer.Options{DeviceSSO: true}, contexts.ProviderThunder,
		productSessions(contexts.ProductSessionsBrowser))
	login.run(t, "login", "--only", "first")

	selected := login.selection(t)
	access, _ := selected.Identity.Access("second")
	var unavailable auth.BrowserUnavailable
	if err := login.shell.sessionEstablisher(selected, "second", true)(access); !errors.As(err, &unavailable) {
		t.Fatalf("first use under --no-input = %v, want the browser refused", err)
	}
}

func TestFirstUseUnderNoInputIsServedByTheDeviceSecret(t *testing.T) {
	login := newDeviceSecretLogin(t, fakeissuer.Options{DeviceSSO: true}, contexts.ProviderThunder)
	login.run(t, "login", "--only", "first")
	login.shell.OpenBrowser = func(string) error {
		t.Error("first use opened a browser")
		return nil
	}

	selected := login.selection(t)
	access, _ := selected.Identity.Access("second")
	if err := login.shell.sessionEstablisher(selected, "second", true)(access); err != nil {
		t.Fatalf("first use under --no-input: %v", err)
	}
	if second := login.load(t, contexts.ProductSessionRef(deviceRef, "second")); second.Resource != deviceSecondAudience {
		t.Errorf("second session resource = %q", second.Resource)
	}
}

func TestFirstUseWithoutADeviceSecretIsRefusedNamingTheSetting(t *testing.T) {
	login := newDeviceSecretLogin(t, fakeissuer.Options{DeviceSSO: true, ReusedBrowserSession: true},
		contexts.ProviderThunder)
	login.run(t, "login", "--only", "first")

	selected := login.selection(t)
	access, _ := selected.Identity.Access("second")
	for _, noInput := range []bool{true, false} {
		err := login.shell.sessionEstablisher(selected, "second", noInput)(access)
		login.refusedForTheSetting(t, err, 1)
	}
}

func TestAnEndedBrowserSessionDiscardsTheSecretAndRefuses(t *testing.T) {
	login := newDeviceSecretLogin(t, fakeissuer.Options{DeviceSSO: true}, contexts.ProviderThunder)
	login.run(t, "login", "--only", "first")
	login.issuer.EndBrowserSession()

	selected := login.selection(t)
	access, _ := selected.Identity.Access("second")
	login.refusedForTheSetting(t, login.shell.sessionEstablisher(selected, "second", false)(access), 1)
	if stored := login.load(t, deviceRef); stored.DeviceSecret != "" || stored.RefreshToken == "" {
		t.Errorf("login session kept secret=%v refresh=%v; want the secret discarded and the session kept",
			stored.DeviceSecret != "", stored.RefreshToken != "")
	}
}

func TestLogoutRevokesEveryProductSessionAndTheSecret(t *testing.T) {
	login := newDeviceSecretLogin(t, fakeissuer.Options{DeviceSSO: true}, contexts.ProviderThunder)
	login.run(t, "login")
	tokens := []string{
		login.load(t, deviceRef).RefreshToken,
		login.load(t, contexts.ProductSessionRef(deviceRef, "second")).RefreshToken,
	}

	login.run(t, "logout", "--keep-browser-session")

	for _, token := range tokens {
		if login.issuer.RefreshTokenLive(token) {
			t.Error("a refresh token is still live at the provider after logout")
		}
	}
	store := session.Store{StateRoot: login.shell.StateRoot}
	for _, ref := range []string{deviceRef, contexts.ProductSessionRef(deviceRef, "second")} {
		if _, err := store.Load(ref); err == nil {
			t.Errorf("session %s, and with it any device secret, survived logout", ref)
		}
	}
}

func TestOnlyAThunderIDProviderThatAdvertisesDeviceSSOIsAskedForASecret(t *testing.T) {
	for name, testCase := range map[string]struct {
		options  fakeissuer.Options
		provider string
	}{
		"another provider":                       {fakeissuer.Options{DeviceSSO: true}, contexts.ProviderAsgardeo},
		"a ThunderID not advertising device_sso": {fakeissuer.Options{}, contexts.ProviderThunder},
	} {
		t.Run(name, func(t *testing.T) {
			login := newDeviceSecretLogin(t, testCase.options, testCase.provider)
			login.run(t, "login", "--only", "first")
			if login.askedForDeviceSecret() {
				t.Error("the login asked for a device secret")
			}
		})
	}
}

// Each way the device secret stops serving is refused with its reason, and
// nothing goes to the browser instead.
func TestADeviceSecretThatStopsServingIsRefusedWithItsReason(t *testing.T) {
	home := fakeissuer.OrganizationUnit{ID: "7f21c4aa-9b2d-4e8f-8c3a-be7a00000001", Name: "Home", Handle: "home"}
	other := fakeissuer.OrganizationUnit{ID: "7f21c4aa-9b2d-4e8f-8c3a-be7a00000002", Name: "Other", Handle: "other"}
	for name, tc := range map[string]struct {
		options fakeissuer.Options
		stop    func(t *testing.T, login *deviceSecretLogin, selected *contexts.Selection)
		want    string
	}{
		"spent": {
			options: fakeissuer.Options{DeviceSSO: true},
			stop: func(_ *testing.T, login *deviceSecretLogin, _ *contexts.Selection) {
				login.issuer.EndBrowserSession()
			},
			want: "no longer accepts the device secret",
		},
		"expired": {
			options: fakeissuer.Options{DeviceSSO: true},
			stop: func(t *testing.T, login *deviceSecretLogin, _ *contexts.Selection) {
				store := session.Store{StateRoot: login.shell.StateRoot}
				stored := login.load(t, deviceRef)
				stored.DeviceSecretExpiresAt = time.Now().Add(-time.Minute)
				if err := store.Save(deviceRef, stored); err != nil {
					t.Fatal(err)
				}
			},
			want: "the device secret has expired",
		},
		"another organization": {
			options: fakeissuer.Options{DeviceSSO: true, OrganizationUnits: []fakeissuer.OrganizationUnit{home, other}},
			stop: func(_ *testing.T, _ *deviceSecretLogin, selected *contexts.Selection) {
				selected.Context.Organization = other.ID
			},
			want: "answers in another organization",
		},
	} {
		t.Run(name, func(t *testing.T) {
			login := newDeviceSecretLogin(t, tc.options, contexts.ProviderThunder)
			login.run(t, "login", "--only", "first")
			selected := login.selection(t)
			tc.stop(t, login, &selected)
			login.errOut.Reset()

			access, _ := selected.Identity.Access("second")
			err := login.shell.sessionEstablisher(selected, "second", false)(access)
			login.refusedForTheSetting(t, err, 1)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("refusal %v does not say %q", err, tc.want)
			}
		})
	}
}

// A device secret a newer login stored in its place is not the one being
// discarded, and stays.
func TestDiscardingADeviceSecretKeepsOneANewerLoginStored(t *testing.T) {
	keyring.MockInit()
	store := session.Store{StateRoot: t.TempDir()}
	if err := store.Save("cloud-dev", session.Session{RefreshToken: "rt", DeviceSecret: "ds-newer"}); err != nil {
		t.Fatal(err)
	}
	if err := discardDeviceSecret(store, "cloud-dev", "ds-earlier"); err != nil {
		t.Fatal(err)
	}
	if stored, err := store.Load("cloud-dev"); err != nil || stored.DeviceSecret != "ds-newer" {
		t.Fatalf("after discarding another secret: %q, %v", stored.DeviceSecret, err)
	}
	if err := discardDeviceSecret(store, "cloud-dev", "ds-newer"); err != nil {
		t.Fatal(err)
	}
	if stored, err := store.Load("cloud-dev"); err != nil || stored.DeviceSecret != "" || stored.RefreshToken != "rt" {
		t.Fatalf("after discarding the stored secret: %+v, %v", stored, err)
	}
}

func TestAnotherProviderAuthorizesEveryProductInTheBrowserWhateverTheSetting(t *testing.T) {
	login := newDeviceSecretLogin(t, fakeissuer.Options{DeviceSSO: true}, contexts.ProviderAsgardeo)
	login.run(t, "login")
	if got := login.browserOpens(); got != 1 {
		t.Errorf("opened the browser %d times, want once for both products", got)
	}
}

// selfHosted makes the context a self-hosted ThunderID's rather than WSO2
// Cloud's, whose product sessions are decided without asking the provider.
func (d *deviceSecretLogin) selfHosted(t *testing.T) {
	t.Helper()
	document, err := contexts.Load(d.shell.StateRoot)
	if err != nil {
		t.Fatal(err)
	}
	context, _ := document.Find("cloud")
	context.Type = contexts.TypeOnprem
	if err := contexts.Save(d.shell.StateRoot, document.Put(context)); err != nil {
		t.Fatal(err)
	}
}

func (d *deviceSecretLogin) recordedProductSessions(t *testing.T) string {
	t.Helper()
	return d.selection(t).Context.Login.ProductSessions
}

func TestUnsetProductSessionsAreRecordedFromWhatTheProviderOffers(t *testing.T) {
	for name, testCase := range map[string]struct {
		deviceSSO bool
		want      string
		notice    bool
	}{
		"device_sso advertised":  {true, contexts.ProductSessionsExchange, false},
		"device_sso not offered": {false, contexts.ProductSessionsBrowser, true},
	} {
		t.Run(name, func(t *testing.T) {
			login := newDeviceSecretLogin(t, fakeissuer.Options{DeviceSSO: testCase.deviceSSO},
				contexts.ProviderThunder)
			login.selfHosted(t)

			login.run(t, "login")

			if got := login.recordedProductSessions(t); got != testCase.want {
				t.Errorf("recorded productSessions %q, want %q", got, testCase.want)
			}
			if got := strings.Count(login.errOut.String(), browserProductsNotice); got != map[bool]int{true: 1, false: 0}[testCase.notice] {
				t.Errorf("the notice appears %d times:\n%s", got, login.errOut)
			}
			if _, err := (session.Store{StateRoot: login.shell.StateRoot}).Load(
				contexts.ProductSessionRef(deviceRef, "second")); err != nil {
				t.Errorf("the second product has no session: %v", err)
			}
			if got := login.browserOpens(); got != 1 {
				t.Errorf("opened the browser %d times, want one tab", got)
			}

			login.errOut.Reset()
			login.run(t, "login")
			if strings.Contains(login.errOut.String(), browserProductsNotice) {
				t.Errorf("the notice repeats once the value is recorded:\n%s", login.errOut)
			}
		})
	}
}

func TestAnExplicitExchangeAtAProviderWithoutSharedSignInIsRefusedAndKept(t *testing.T) {
	login := newDeviceSecretLogin(t, fakeissuer.Options{}, contexts.ProviderThunder,
		productSessions(contexts.ProductSessionsExchange))
	login.selfHosted(t)

	if code := login.shell.Run([]string{"login"}); code != 77 {
		t.Fatalf("login exit %d, want 77; stderr %s", code, login.errOut)
	}
	refusal := login.errOut.String()
	for _, want := range []string{"auth.device_secret_unavailable", login.issuer.URL, "does not offer",
		"productSessions: browser", "wso2 context edit"} {
		if !strings.Contains(refusal, want) {
			t.Errorf("the refusal does not say %q:\n%s", want, refusal)
		}
	}
	if strings.Contains(refusal, "sign in again") {
		t.Errorf("the refusal suggests a sign-in that cannot help:\n%s", refusal)
	}
	if got := login.recordedProductSessions(t); got != contexts.ProductSessionsExchange {
		t.Errorf("an explicit setting became %q", got)
	}
	if got := login.browserOpens(); got != 1 {
		t.Errorf("opened the browser %d times, want the sign-in alone", got)
	}
}

func TestAnExplicitBrowserIsKeptWhereTheProviderOffersSharedSignIn(t *testing.T) {
	login := newDeviceSecretLogin(t, fakeissuer.Options{DeviceSSO: true}, contexts.ProviderThunder,
		productSessions(contexts.ProductSessionsBrowser))
	login.run(t, "login")
	if got := login.recordedProductSessions(t); got != contexts.ProductSessionsBrowser {
		t.Errorf("an explicit setting became %q", got)
	}
	if strings.Contains(login.errOut.String(), browserProductsNotice) {
		t.Errorf("an explicit setting was announced as decided:\n%s", login.errOut)
	}
}

func TestARefusalForAContextThatIsNotSelectedNamesIt(t *testing.T) {
	login := newDeviceSecretLogin(t, fakeissuer.Options{DeviceSSO: true, ReusedBrowserSession: true},
		contexts.ProviderThunder)
	document, err := contexts.Load(login.shell.StateRoot)
	if err != nil {
		t.Fatal(err)
	}
	document.DefaultContext = ""
	if err := contexts.Save(login.shell.StateRoot, document); err != nil {
		t.Fatal(err)
	}

	if code := login.shell.Run([]string{"login", "--context", "cloud"}); code != 77 {
		t.Fatalf("login exit %d, want 77; stderr %s", code, login.errOut)
	}
	if !strings.Contains(login.errOut.String(), "wso2 logout --context cloud") {
		t.Errorf("the recovery does not name the context:\n%s", login.errOut)
	}

	login.errOut.Reset()
	if code := login.shell.Run([]string{"login", "--context", "cloud", "--device"}); code != 64 {
		t.Fatalf("login --device exit %d, want 64; stderr %s", code, login.errOut)
	}
	if !strings.Contains(login.errOut.String(), "without --device, run `wso2 login --context cloud`.") {
		t.Errorf("the --device refusal does not name the context:\n%s", login.errOut)
	}
}

func TestARecoveryNamesTheContextWhenWSO2ContextWouldSelectAnother(t *testing.T) {
	login := newDeviceSecretLogin(t, fakeissuer.Options{DeviceSSO: true, ReusedBrowserSession: true},
		contexts.ProviderThunder)
	t.Setenv("WSO2_CONTEXT", "elsewhere")

	if code := login.shell.Run([]string{"login", "--context", "cloud"}); code != 77 {
		t.Fatalf("login exit %d, want 77; stderr %s", code, login.errOut)
	}
	if !strings.Contains(login.errOut.String(), "wso2 logout --context cloud") {
		t.Errorf("the recovery runs against what WSO2_CONTEXT selects:\n%s", login.errOut)
	}
}

func TestAnEstablisherSettlesProductSessionsOnce(t *testing.T) {
	login := newDeviceSecretLogin(t, fakeissuer.Options{DeviceSSO: true}, contexts.ProviderThunder)
	login.selfHosted(t)
	unsettled := login.selection(t)
	login.run(t, "login", "--only", "first")
	access, _ := unsettled.Identity.Access("second")
	establish := login.shell.sessionEstablisher(unsettled, "second", false)

	before := login.issuer.DiscoveryRequests()
	if err := establish(access); err != nil {
		t.Fatal(err)
	}
	first := login.issuer.DiscoveryRequests() - before
	if err := establish(access); err != nil {
		t.Fatal(err)
	}
	if second := login.issuer.DiscoveryRequests() - before - first; second >= first {
		t.Errorf("the second establishment read discovery %d times, the first %d: it settled again", second, first)
	}
}

func TestSettlingKeepsTheProductSessionsAnotherWriterRecorded(t *testing.T) {
	login := newDeviceSecretLogin(t, fakeissuer.Options{}, contexts.ProviderThunder)
	login.selfHosted(t)
	stale := login.selection(t)
	document, err := contexts.Load(login.shell.StateRoot)
	if err != nil {
		t.Fatal(err)
	}
	context, _ := document.Find("cloud")
	context.Login.ProductSessions = contexts.ProductSessionsExchange
	if err := contexts.Save(login.shell.StateRoot, document.Put(context)); err != nil {
		t.Fatal(err)
	}

	settled, err := login.shell.settleProductSessions(stale)
	if err != nil {
		t.Fatal(err)
	}
	if got := settled.Context.Login.ProductSessions; got != contexts.ProductSessionsExchange {
		t.Errorf("settled productSessions %q, want the recorded %q", got, contexts.ProductSessionsExchange)
	}
	if strings.Contains(login.errOut.String(), browserProductsNotice) {
		t.Errorf("the browser notice was said for a context that records exchange:\n%s", login.errOut)
	}
}
