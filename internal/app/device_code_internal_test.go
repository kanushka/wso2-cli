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
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/sdk/problem"
)

func signsInWithADeviceCode(login *contexts.Login) { login.Kind = contexts.KindOAuthDevice }

func newDeviceCodeLogin(t *testing.T, options fakeissuer.Options, edits ...func(*contexts.Login)) *deviceSecretLogin {
	t.Helper()
	options.DeviceInterval = 1
	login := newDeviceSecretLogin(t, options, contexts.ProviderThunder, append(edits, signsInWithADeviceCode)...)
	login.shell.OpenBrowser = func(string) error {
		t.Error("a device-code context opened the browser")
		return nil
	}
	return login
}

func TestADeviceCodeSignInAtThunderIDBindsTheResourceAndKeepsTheDeviceSecret(t *testing.T) {
	login := newDeviceCodeLogin(t, fakeissuer.Options{DeviceSSO: true})

	login.run(t, "login")

	requests := login.issuer.DeviceAuthorizations()
	if len(requests) != 1 {
		t.Fatalf("%d device authorizations, want one for the sign-in", len(requests))
	}
	if got := requests[0].Get("resource"); got != deviceFirstAudience {
		t.Errorf("the device authorization named resource %q, want %q", got, deviceFirstAudience)
	}
	if !slices.Contains(strings.Fields(requests[0].Get("scope")), "device_sso") {
		t.Errorf("the device authorization did not ask for a device secret: %q", requests[0].Get("scope"))
	}
	first := login.load(t, deviceRef)
	if first.Resource != deviceFirstAudience || first.DeviceSecret == "" ||
		first.EstablishedBy != session.EstablishedByDeviceCode {
		t.Errorf("login session resource %q secret %v established by %q", first.Resource,
			first.DeviceSecret != "", first.EstablishedBy)
	}
	second := login.load(t, contexts.ProductSessionRef(deviceRef, "second"))
	if second.Resource != deviceSecondAudience || second.EstablishedBy != session.EstablishedByDeviceSecret {
		t.Errorf("second session resource %q established by %q, want it from the device secret",
			second.Resource, second.EstablishedBy)
	}
	if !strings.Contains(login.errOut.String(),
		`Authorized the "second" product from this sign-in, without the browser.`) {
		t.Errorf("stderr does not say the second product came from the sign-in:\n%s", login.errOut)
	}
	if !userCodeOnItsOwnLine(login.errOut.String()) {
		t.Errorf("stderr does not show the user code on a line of its own:\n%s", login.errOut)
	}

	login.out.Reset()
	login.run(t, "whoami", "--output", "json")
	if !strings.Contains(login.out.String(), `"establishedBy": "device-code"`) {
		t.Errorf("whoami does not say the sign-in was by device code:\n%s", login.out)
	}
	if strings.Contains(login.out.String()+login.errOut.String(), first.DeviceSecret) {
		t.Error("the device secret reached an output stream")
	}
}

func TestADeviceCodeSignInWithNoSecretRefusesTheNextProductWithoutABrowser(t *testing.T) {
	login := newDeviceCodeLogin(t, fakeissuer.Options{DeviceSSO: true, ReusedBrowserSession: true})

	if code := login.shell.Run([]string{"login"}); code != 77 {
		t.Fatalf("login exit %d, want 77; stderr %s", code, login.errOut)
	}
	if !strings.Contains(login.errOut.String(), "auth.device_secret_unavailable") ||
		!strings.Contains(login.errOut.String(), "productSessions: browser") {
		t.Errorf("the refusal does not name the code and the setting:\n%s", login.errOut)
	}
	if got := len(login.issuer.DeviceAuthorizations()); got != 1 {
		t.Errorf("%d device authorizations, want only the sign-in's", got)
	}
}

func TestAClientNotRegisteredForTheDeviceGrantIsToldWhichSettingToChange(t *testing.T) {
	login := newDeviceCodeLogin(t, fakeissuer.Options{DeviceSSO: true, DeviceUnauthorizedClient: true})

	if code := login.shell.Run([]string{"login"}); code != 77 {
		t.Fatalf("login exit %d, want 77", code)
	}
	for _, want := range []string{"not registered for the device authorization grant", "kind: oauth-browser"} {
		if !strings.Contains(login.errOut.String(), want) {
			t.Errorf("stderr does not say %q:\n%s", want, login.errOut)
		}
	}
}

func userCodeOnItsOwnLine(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if len(line) == 9 && line[4] == '-' && strings.ToUpper(line) == line {
			return true
		}
	}
	return false
}

func TestLoginDeviceRefusesAContextThatSignsInThroughTheBrowser(t *testing.T) {
	login := newDeviceSecretLogin(t, fakeissuer.Options{DeviceSSO: true, DeviceInterval: 1}, contexts.ProviderThunder)

	if code := login.shell.Run([]string{"login", "--device"}); code != 64 {
		t.Fatalf("login --device exit %d, want 64; stderr %s", code, login.errOut)
	}
	for _, want := range []string{"shell.invalid_argument", "kind: oauth-device", "wso2 context edit"} {
		if !strings.Contains(login.errOut.String(), want) {
			t.Errorf("the refusal does not say %q:\n%s", want, login.errOut)
		}
	}
	if login.browserOpens() != 0 || len(login.issuer.DeviceAuthorizations()) != 0 {
		t.Error("the refused login reached the identity provider")
	}
}

func TestLoginDeviceOnADeviceCodeContextSignsIn(t *testing.T) {
	login := newDeviceCodeLogin(t, fakeissuer.Options{DeviceSSO: true})

	login.run(t, "login", "--device")

	if got := len(login.issuer.DeviceAuthorizations()); got != 1 {
		t.Errorf("%d device authorizations, want one", got)
	}
}

func TestLoginDeviceWithURLCreatesADeviceCodeContext(t *testing.T) {
	login := newDeviceCodeLogin(t, fakeissuer.Options{})
	if err := os.Remove(contexts.Path(login.shell.StateRoot)); err != nil {
		t.Fatal(err)
	}

	login.run(t, "login", "--url", login.issuer.URL, "--client-id", "wso2-cli", "--context", "remote", "--device")

	created, found := loadContextDocument(t, login.shell).Find("remote")
	if !found || created.Login.Kind != contexts.KindOAuthDevice {
		t.Fatalf("created context %+v, want kind %s", created.Login, contexts.KindOAuthDevice)
	}
	if got := len(login.issuer.DeviceAuthorizations()); got != 1 {
		t.Errorf("%d device authorizations, want the sign-in's", got)
	}
}

func loadContextDocument(t *testing.T, shell Shell) contexts.Document {
	t.Helper()
	document, err := contexts.Load(shell.StateRoot)
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func TestLogoutOfADeviceCodeContextPrintsTheSignOutPageInsteadOfOpeningIt(t *testing.T) {
	login := newDeviceCodeLogin(t, fakeissuer.Options{DeviceSSO: true})
	login.run(t, "login")
	tokens := []string{
		login.load(t, deviceRef).RefreshToken,
		login.load(t, contexts.ProductSessionRef(deviceRef, "second")).RefreshToken,
	}
	login.errOut.Reset()

	login.run(t, "logout")

	if !strings.Contains(login.errOut.String(), "On the device you signed in with, open this URL to end the "+
		"browser session") ||
		strings.Contains(login.errOut.String(), "id_token_hint") {
		t.Errorf("stderr does not print the sign-out URL without the identity token:\n%s", login.errOut)
	}
	for _, token := range tokens {
		if login.issuer.RefreshTokenLive(token) {
			t.Error("a refresh token is still live at the provider after logout")
		}
	}
}

func TestLogoutOfADeviceCodeContextReportsTheSignOutPageInJSON(t *testing.T) {
	login := newDeviceCodeLogin(t, fakeissuer.Options{DeviceSSO: true})
	login.run(t, "login")
	login.out.Reset()

	login.run(t, "logout", "-o", "json")

	var report struct {
		BrowserSession string `json:"browserSession"`
		SignOutURLs    string `json:"signOutUrls"`
	}
	if err := json.Unmarshal(login.out.Bytes(), &report); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, login.out)
	}
	if !strings.HasPrefix(report.BrowserSession, "sign-out printed") ||
		!strings.HasPrefix(report.SignOutURLs, login.issuer.URL) ||
		strings.Contains(report.SignOutURLs, "id_token_hint") {
		t.Errorf("report = %+v, want the sign-out URL without the identity token", report)
	}
}

func TestLogoutOfADeviceCodeContextPrintsTheSignOutPageUnderNoInput(t *testing.T) {
	login := newDeviceCodeLogin(t, fakeissuer.Options{DeviceSSO: true})
	login.run(t, "login")
	login.errOut.Reset()
	login.out.Reset()

	login.run(t, "logout", "--no-input")

	if !strings.Contains(login.errOut.String(), "On the device you signed in with, open this URL") {
		t.Errorf("stderr does not print the sign-out URL:\n%s", login.errOut)
	}
	if !strings.Contains(login.out.String(), "sign-out printed") {
		t.Errorf("the report does not say the sign-out was printed:\n%s", login.out)
	}
}

func TestTheProvidersRefusalOfADeviceCodeIsLoggedUnderVerbose(t *testing.T) {
	login := newDeviceCodeLogin(t, fakeissuer.Options{DeviceSSO: true, DeviceUnauthorizedClient: true})

	if code := login.shell.Run([]string{"--verbose", "login"}); code != 77 {
		t.Fatalf("login exit %d, want 77", code)
	}
	for _, line := range strings.Split(login.errOut.String(), "\n") {
		if strings.Contains(line, "provider_error") && strings.Contains(line, "unauthorized_client") {
			return
		}
	}
	t.Errorf("no debug line names the provider's error:\n%s", login.errOut)
}

func TestTheDeviceSecretReachesNeitherJSONNorVerboseOutput(t *testing.T) {
	login := newDeviceCodeLogin(t, fakeissuer.Options{DeviceSSO: true})

	login.run(t, "--verbose", "login", "-o", "json")

	secret := login.load(t, deviceRef).DeviceSecret
	if secret == "" {
		t.Fatal("no device secret was stored to look for")
	}
	if strings.Contains(login.out.String(), secret) || strings.Contains(login.errOut.String(), secret) {
		t.Error("the device secret reached an output stream")
	}
	if !strings.Contains(login.errOut.String(), "establishing a product session from the device secret") {
		t.Errorf("the verbose output has no exchange lines to search:\n%s", login.errOut)
	}
}

var deviceHomeUnit = fakeissuer.OrganizationUnit{ID: "a1b2c3d4-0000-4000-8000-000000000001", Name: "Home",
	Handle: "home"}

func TestADeviceCodeSignInThatNamesNoUserOrOrganizationSaysSoOnce(t *testing.T) {
	login := newDeviceCodeLogin(t, fakeissuer.Options{DeviceSSO: true, OrganizationUnits: []fakeissuer.OrganizationUnit{
		deviceHomeUnit}, DeviceWithoutUserAttributes: true})

	login.run(t, "login")

	stderr := login.errOut.String()
	if got := strings.Count(stderr, unnamedSignInNotice); got != 1 {
		t.Errorf("stderr says the notice %d times, want once:\n%s", got, stderr)
	}
	if strings.Contains(stderr, "6991") || strings.Contains(stderr, "ouId") {
		t.Errorf("the notice names an issue or a claim without --verbose:\n%s", stderr)
	}
}

func TestADeviceCodeSignInThatNamesNoUserOrOrganizationLogsWhatWasMissingUnderVerbose(t *testing.T) {
	login := newDeviceCodeLogin(t, fakeissuer.Options{DeviceSSO: true, OrganizationUnits: []fakeissuer.OrganizationUnit{
		deviceHomeUnit}, DeviceWithoutUserAttributes: true})

	login.run(t, "--verbose", "login")

	stderr := login.errOut.String()
	for _, want := range []string{"email name ouId", "thunder-id/thunderid#6991"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the verbose output does not say %q:\n%s", want, stderr)
		}
	}
}

func TestADeviceCodeSignInThatNamesTheUserAndOrganizationSaysNothingMore(t *testing.T) {
	login := newDeviceCodeLogin(t, fakeissuer.Options{DeviceSSO: true, OrganizationUnits: []fakeissuer.OrganizationUnit{
		deviceHomeUnit}})

	login.run(t, "login")

	if strings.Contains(login.errOut.String(), unnamedSignInNotice) {
		t.Errorf("a sign-in that named the user and organization printed the notice:\n%s", login.errOut)
	}
}

func TestADeviceSecretInAnotherOrganizationOnADeviceCodeContextNamesTheOrganizationToChoose(t *testing.T) {
	other := fakeissuer.OrganizationUnit{ID: "a1b2c3d4-0000-4000-8000-000000000002", Name: "Other", Handle: "other"}
	login := newDeviceCodeLogin(t, fakeissuer.Options{DeviceSSO: true,
		OrganizationUnits: []fakeissuer.OrganizationUnit{deviceHomeUnit, other}})
	login.run(t, "login", "--only", "first")
	selected := login.selection(t)
	selected.Context.Organization = other.ID

	access, _ := selected.Identity.Access("second")
	err := login.shell.sessionEstablisher(selected, "second", false)(access)

	var refusal problem.Problem
	if !errors.As(err, &refusal) || refusal.Code != "auth.device_secret_unavailable" {
		t.Fatalf("error = %v, want auth.device_secret_unavailable", err)
	}
	for _, want := range []string{"choose the organization whose id is " + other.ID, "wso2 logout"} {
		if !strings.Contains(refusal.Recovery, want) {
			t.Errorf("the recovery does not say %q: %s", want, refusal.Recovery)
		}
	}
}
