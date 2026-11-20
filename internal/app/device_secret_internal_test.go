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

func newDeviceSecretLogin(t *testing.T, options fakeissuer.Options, provider string) *deviceSecretLogin {
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
	if err := fixture.WriteV2(login.shell.StateRoot, document); err != nil {
		t.Fatal(err)
	}
	return login
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

func TestAReusedBrowserSessionAuthorizesEachProductInOneBrowserTab(t *testing.T) {
	login := newDeviceSecretLogin(t, fakeissuer.Options{DeviceSSO: true, ReusedBrowserSession: true},
		contexts.ProviderThunder)

	login.run(t, "login")

	if got := login.browserOpens(); got != 1 {
		t.Fatalf("opened the browser %d times, want once for both products", got)
	}
	if !login.askedForDeviceSecret() {
		t.Error("the login did not ask for a device secret")
	}
	if login.load(t, deviceRef).DeviceSecret != "" {
		t.Error("a device secret was stored though none was issued")
	}
	if second := login.load(t, contexts.ProductSessionRef(deviceRef, "second")); second.EstablishedBy != "" ||
		second.Resource != deviceSecondAudience {
		t.Errorf("second session = %+v, want a browser authorization bound to its audience", second.Resource)
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

func TestFirstUseUnderNoInputWithoutADeviceSecretIsRefused(t *testing.T) {
	login := newDeviceSecretLogin(t, fakeissuer.Options{DeviceSSO: true, ReusedBrowserSession: true},
		contexts.ProviderThunder)
	login.run(t, "login", "--only", "first")

	selected := login.selection(t)
	access, _ := selected.Identity.Access("second")
	var unavailable auth.BrowserUnavailable
	if err := login.shell.sessionEstablisher(selected, "second", true)(access); !errors.As(err, &unavailable) {
		t.Fatalf("first use = %v, want the browser refused", err)
	}
}

func TestAnEndedBrowserSessionDiscardsTheSecretAndFallsBack(t *testing.T) {
	login := newDeviceSecretLogin(t, fakeissuer.Options{DeviceSSO: true}, contexts.ProviderThunder)
	login.run(t, "login", "--only", "first")
	login.issuer.EndBrowserSession()

	selected := login.selection(t)
	access, _ := selected.Identity.Access("second")
	var unavailable auth.BrowserUnavailable
	if err := login.shell.sessionEstablisher(selected, "second", true)(access); !errors.As(err, &unavailable) {
		t.Fatalf("first use = %v, want the browser path, refused under --no-input", err)
	}
	if stored := login.load(t, deviceRef); stored.DeviceSecret != "" || stored.RefreshToken == "" {
		t.Errorf("login session kept secret=%v refresh=%v; want the secret discarded and the session kept",
			stored.DeviceSecret != "", stored.RefreshToken != "")
	}
	if !strings.Contains(login.errOut.String(),
		`Your earlier sign-in has ended; the "second" product needs the browser.`) {
		t.Errorf("the fallback was not said:\n%s", login.errOut)
	}

	if err := login.shell.sessionEstablisher(selected, "second", false)(access); err != nil {
		t.Fatalf("first use with a browser: %v", err)
	}
	if got := login.browserOpens(); got != 2 {
		t.Errorf("opened the browser %d times, want the login and the fallback", got)
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
			login.run(t, "login")
			if login.askedForDeviceSecret() {
				t.Error("the login asked for a device secret")
			}
			if got := login.browserOpens(); got != 1 {
				t.Errorf("opened the browser %d times, want once for both products", got)
			}
		})
	}
}

// Each way the device secret stops serving says so in one plain line before
// the browser is needed instead.
func TestADeviceSecretThatStopsServingSaysTheBrowserIsNeeded(t *testing.T) {
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
			want: `Your earlier sign-in has ended; the "second" product needs the browser.`,
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
			want: `Your earlier sign-in has ended; the "second" product needs the browser.`,
		},
		"another organization": {
			options: fakeissuer.Options{DeviceSSO: true, OrganizationUnits: []fakeissuer.OrganizationUnit{home, other}},
			stop: func(_ *testing.T, _ *deviceSecretLogin, selected *contexts.Selection) {
				selected.Context.Organization = other.ID
			},
			want: `Your earlier sign-in is in another organization; the "second" product needs the browser.`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			login := newDeviceSecretLogin(t, tc.options, contexts.ProviderThunder)
			login.run(t, "login", "--only", "first")
			selected := login.selection(t)
			tc.stop(t, login, &selected)
			login.errOut.Reset()

			access, _ := selected.Identity.Access("second")
			_ = login.shell.sessionEstablisher(selected, "second", true)(access)
			if !strings.Contains(login.errOut.String(), tc.want) {
				t.Errorf("stderr does not say %q:\n%s", tc.want, login.errOut)
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
