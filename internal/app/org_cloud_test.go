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
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
)

func newSignedInCloudOrgs(t *testing.T) *cloudLogin {
	t.Helper()
	return newSignedInCloudOrgsWith(t, fakeissuer.Options{})
}

func newSignedInCloudOrgsWith(t *testing.T, options fakeissuer.Options) *cloudLogin {
	t.Helper()
	options.OrganizationUnits, options.RefreshScopeMode = cloudOrgs, "honor"
	login := newCloudLoginWith(t, options)
	installCloudAPIM(t, login.shell)
	installCloudIAM(t, login.shell)
	login.run(t, "login")
	login.mutex.Lock()
	login.paths = nil
	login.opened = 0
	login.mutex.Unlock()
	return login
}

func TestOrgUseOnCloudSignsInAgainForTheOtherOrganization(t *testing.T) {
	login := newSignedInCloudOrgs(t)
	before := len(login.issuer.AuthorizationPaths())

	login.run(t, "org", "use", betaUnit.ID)

	login.mutex.Lock()
	opened := login.opened
	login.mutex.Unlock()
	if opened != 1 {
		t.Errorf("org use opened the browser %d times, want one tab", opened)
	}
	chained := login.issuer.AuthorizationPaths()[before:]
	if len(chained) != 1 {
		t.Errorf("org use authorized %d times in the tab, want the login alone, iam from its device secret: %v",
			len(chained), chained)
	}
	for _, path := range chained {
		if path != "/ou/"+betaUnit.ID+"/authorize" {
			t.Errorf("the tab authorized at %q, want the organization's own authorization endpoint", path)
		}
	}

	if got := cloudContext(t, login.shell).Organization; got != betaUnit.ID {
		t.Errorf("context organization = %q, want %q", got, betaUnit.ID)
	}
	login.mutex.Lock()
	paths := append([]string(nil), login.paths...)
	login.mutex.Unlock()
	if len(paths) == 0 {
		t.Fatal("org use did not sign in again")
	}
	for _, path := range paths {
		if path != "/ou/"+betaUnit.ID+"/authorize" {
			t.Errorf("authorized at %q, want the organization's own authorization endpoint", path)
		}
	}
	store := session.Store{StateRoot: login.shell.StateRoot}
	context := cloudContext(t, login.shell)
	for _, ref := range []string{context.CredentialRef, contexts.ProductSessionRef(context.CredentialRef, "iam")} {
		stored, err := store.Load(ref)
		if err != nil {
			t.Fatalf("session %s: %v", ref, err)
		}
		if got := auth.TokenOrganization(stored.AccessToken).ID; got != betaUnit.ID {
			t.Errorf("session %s acts in %q, want %q", ref, got, betaUnit.ID)
		}
	}
	if !hasField(login.out.String(), "Organization", "Beta Labs (beta)") {
		t.Errorf("the sign-in does not report the new organization:\n%s", login.out)
	}
	if !strings.Contains(login.out.String(), `Logged the "cloud-dev" context in to the Beta Labs (beta) organization.`) {
		t.Errorf("the switch does not name the organization the way whoami does:\n%s", login.out)
	}

	login.out.Reset()
	login.run(t, "org", "current")
	if !strings.Contains(login.out.String(), "Beta Labs (beta)") {
		t.Errorf("org current after org use:\n%s", login.out)
	}
	login.out.Reset()
	login.run(t, "whoami")
	if !hasField(login.out.String(), "Organization", "Beta Labs (beta)") {
		t.Errorf("whoami after org use:\n%s", login.out)
	}
}

func TestOrgUseOnCloudTakesTheSignedInOrganizationsHandle(t *testing.T) {
	login := newSignedInCloudOrgs(t)

	login.run(t, "org", "use", "acme")

	if got := cloudContext(t, login.shell).Organization; got != acmeUnit.ID {
		t.Errorf("context organization = %q, want the signed-in organization's id %q", got, acmeUnit.ID)
	}
	login.mutex.Lock()
	defer login.mutex.Unlock()
	if len(login.paths) != 0 {
		t.Errorf("org use signed in again for the organization already signed in to: %v", login.paths)
	}
	if !strings.Contains(login.errOut.String(), "Already signed in to this organization, so no new sign-in was needed.") {
		t.Errorf("skipping the sign-in was not announced:\n%s", login.errOut)
	}
}

func TestOrgUseOnCloudWithoutInputRefusesAndChangesNothing(t *testing.T) {
	login := newSignedInCloudOrgs(t)
	t.Setenv(app.NoInputEnvVar, "1")
	login.errOut.Reset()

	if code := login.shell.Run([]string{"org", "use", betaUnit.ID}); code != exit.AuthPolicy {
		t.Fatalf("exit %d, want %d; stderr %s", code, exit.AuthPolicy, login.errOut)
	}
	if got := cloudContext(t, login.shell).Organization; got != "" {
		t.Errorf("context organization = %q, want it unchanged", got)
	}
	if _, err := (session.Store{StateRoot: login.shell.StateRoot}).Load(cloudContext(t, login.shell).CredentialRef); err != nil {
		t.Errorf("the session was ended by a switch that did not run: %v", err)
	}
	if !strings.Contains(login.errOut.String(), "auth.non_interactive") {
		t.Errorf("refusal:\n%s", login.errOut)
	}
}

func TestOrgUseOnCloudWhoseSignInFailsKeepsThePreviousOrganization(t *testing.T) {
	login := newSignedInCloudOrgs(t)
	login.errOut.Reset()

	if code := login.shell.Run([]string{"org", "use", "11111111-2222-4333-8444-555555555555"}); code == exit.OK {
		t.Fatal("a switch to an organization the provider refused succeeded")
	}
	if got := cloudContext(t, login.shell).Organization; got != "" {
		t.Errorf("context organization = %q, want the previous one kept", got)
	}
	store := session.Store{StateRoot: login.shell.StateRoot}
	context := cloudContext(t, login.shell)
	for _, ref := range []string{context.CredentialRef, contexts.ProductSessionRef(context.CredentialRef, "iam")} {
		if _, err := store.Load(ref); err == nil {
			t.Errorf("session %s survived a failed switch", ref)
		}
	}
	if refusal := login.errOut.String(); !strings.Contains(refusal, "wso2 login") || !strings.Contains(refusal, "ended") {
		t.Errorf("the refusal does not say the sessions ended and wso2 login restores them:\n%s", refusal)
	}
}

func TestOrgListRefusesNamingWhyAndTheWayToChoose(t *testing.T) {
	login := newSignedInCloudOrgs(t)
	login.errOut.Reset()
	if code := login.shell.Run([]string{"org", "list"}); code == exit.OK {
		t.Fatal("org list succeeded with no source of organizations")
	}
	refusal := login.errOut.String()
	if !strings.Contains(refusal, "shell.organization_list_unavailable") || !strings.Contains(refusal, "`wso2 org use` with no argument") {
		t.Errorf("org list refusal:\n%s", refusal)
	}
}

func TestOrgUseOnCloudRefusesAHandleItCannotResolveBeforeEndingAnything(t *testing.T) {
	login := newSignedInCloudOrgs(t)
	login.errOut.Reset()

	if code := login.shell.Run([]string{"org", "use", "beta"}); code != exit.Usage {
		t.Fatalf("exit %d, want %d; stderr %s", code, exit.Usage, login.errOut)
	}
	if !strings.Contains(login.errOut.String(), "shell.invalid_argument") {
		t.Errorf("refusal:\n%s", login.errOut)
	}
	if _, err := (session.Store{StateRoot: login.shell.StateRoot}).Load(cloudContext(t, login.shell).CredentialRef); err != nil {
		t.Errorf("the session was ended by a switch that did not run: %v", err)
	}
}

func TestACloudContextEditedToAnotherOrganizationIsRefusedAtTheBroker(t *testing.T) {
	login := newSignedInCloudOrgs(t)
	document := loadDocument(t, login.shell)
	context := cloudContext(t, login.shell)
	context.Organization = betaUnit.ID
	if err := contexts.Save(login.shell.StateRoot, document.Put(context)); err != nil {
		t.Fatal(err)
	}

	_, err := productBroker(t, login.shell, login.issuer, "apim").Acquire(auth.Request{Audience: cloudShared})

	var refused auth.Denial
	if !errors.As(err, &refused) || refused.Problem.Code != "auth.organization_mismatch" ||
		!strings.Contains(refused.Problem.Recovery, "org use "+betaUnit.ID) {
		t.Fatalf("Acquire = %v, want auth.organization_mismatch naming org use", err)
	}
}

// With no organization named, org use asks the person to sign in again, which
// is where ThunderID shows its organization picker, and records the
// organization the sign-in came back in. The other products follow in the
// same tab at that organization's endpoint, without a prompt.
func TestOrgUseOnCloudWithNoArgumentLetsThePersonChooseInTheBrowser(t *testing.T) {
	login := newSignedInCloudOrgsWith(t, fakeissuer.Options{ChosenOrganizationUnit: betaUnit.ID})
	useProductSessions(t, login.shell, contexts.ProductSessionsBrowser)
	before := len(login.issuer.AuthorizationPaths())

	login.run(t, "org", "use")

	if got := cloudContext(t, login.shell).Organization; got != betaUnit.ID {
		t.Errorf("context organization = %q, want the chosen %q", got, betaUnit.ID)
	}
	login.mutex.Lock()
	opened := login.opened
	login.mutex.Unlock()
	if opened != 1 {
		t.Errorf("org use opened the browser %d times, want one tab", opened)
	}
	paths := login.issuer.AuthorizationPaths()[before:]
	queries := login.issuer.Authorizations()[before:]
	if len(paths) < 2 {
		t.Fatalf("org use authorized %d times, want the login and iam: %v", len(paths), paths)
	}
	if paths[0] != "/authorize" || queries[0].Get("prompt") != "login" {
		t.Errorf("the first authorization went to %q with prompt %q, want /authorize with prompt=login",
			paths[0], queries[0].Get("prompt"))
	}
	for index := 1; index < len(paths); index++ {
		if paths[index] != "/ou/"+betaUnit.ID+"/authorize" || queries[index].Get("prompt") == "login" {
			t.Errorf("authorization %d went to %q with prompt %q, want the chosen organization's endpoint "+
				"without a sign-in", index, paths[index], queries[index].Get("prompt"))
		}
	}
	store := session.Store{StateRoot: login.shell.StateRoot}
	context := cloudContext(t, login.shell)
	for _, ref := range []string{context.CredentialRef, contexts.ProductSessionRef(context.CredentialRef, "iam")} {
		stored, err := store.Load(ref)
		if err != nil {
			t.Fatalf("session %s: %v", ref, err)
		}
		if got := auth.TokenOrganization(stored.AccessToken).ID; got != betaUnit.ID {
			t.Errorf("session %s acts in %q, want %q", ref, got, betaUnit.ID)
		}
	}
	if !strings.Contains(login.out.String(), `Logged the "cloud-dev" context in to the Beta Labs (beta) organization.`) {
		t.Errorf("the switch does not name the chosen organization:\n%s", login.out)
	}
}

func TestOrgUseOnCloudWithNoArgumentAndNoInputRefusesAndChangesNothing(t *testing.T) {
	login := newSignedInCloudOrgsWith(t, fakeissuer.Options{ChosenOrganizationUnit: betaUnit.ID})
	before := len(login.issuer.AuthorizationPaths())
	t.Setenv("WSO2_NO_INPUT", "1")
	login.errOut.Reset()

	if code := login.shell.Run([]string{"org", "use"}); code != exit.AuthPolicy {
		t.Fatalf("exit %d, want %d: %s", code, exit.AuthPolicy, login.errOut)
	}
	if !strings.Contains(login.errOut.String(), "auth.non_interactive") {
		t.Errorf("refusal:\n%s", login.errOut)
	}
	if got := len(login.issuer.AuthorizationPaths()); got != before {
		t.Errorf("a refused org use authorized %d times", got-before)
	}
	context := cloudContext(t, login.shell)
	if _, err := (session.Store{StateRoot: login.shell.StateRoot}).Load(context.CredentialRef); err != nil {
		t.Errorf("a refused org use ended the login session: %v", err)
	}
}

func TestOrgUseOnCloudWithNoArgumentRefusesASignInIssuedInNoOrganization(t *testing.T) {
	login := newSignedInCloudOrgsWith(t, fakeissuer.Options{ChosenOrganizationUnit: "unlisted"})
	login.run(t, "org", "use", betaUnit.ID)
	before := len(login.issuer.AuthorizationPaths())
	login.errOut.Reset()

	if code := login.shell.Run([]string{"org", "use"}); code == exit.OK {
		t.Fatal("a switch whose sign-in named no organization succeeded")
	}
	if got := len(login.issuer.AuthorizationPaths()) - before; got != 1 {
		t.Errorf("org use authorized %d times, want the sign-in alone, refused before iam", got)
	}
	if got := cloudContext(t, login.shell).Organization; got != betaUnit.ID {
		t.Errorf("context organization = %q, want the previous %q kept", got, betaUnit.ID)
	}
	store := session.Store{StateRoot: login.shell.StateRoot}
	context := cloudContext(t, login.shell)
	for _, ref := range []string{context.CredentialRef, contexts.ProductSessionRef(context.CredentialRef, "iam")} {
		if _, err := store.Load(ref); err == nil {
			t.Errorf("session %s survived a refused switch", ref)
		}
	}
	refusal := login.errOut.String()
	for _, want := range []string{"auth.organization_not_selected",
		fmt.Sprintf("still names the %q organization", betaUnit.ID), "ended", "wso2 login"} {
		if !strings.Contains(refusal, want) {
			t.Errorf("the refusal does not say %q:\n%s", want, refusal)
		}
	}
}

func TestOrgUseOnCloudUnderJSONIsOneDocumentWithItsNextStep(t *testing.T) {
	login := newSignedInCloudOrgs(t)
	login.out.Reset()

	login.run(t, "org", "use", betaUnit.ID, "-o", "json")

	var document struct {
		Context      string   `json:"context"`
		Organization string   `json:"organization"`
		Next         []string `json:"next"`
	}
	if err := json.Unmarshal(login.out.Bytes(), &document); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, login.out)
	}
	if document.Organization != betaUnit.ID || len(document.Next) == 0 ||
		!strings.HasSuffix(document.Next[0], " status") {
		t.Errorf("document = %+v, want the organization and the status of the first ready product as next", document)
	}
}

func useProductSessions(t *testing.T, shell app.Shell, method string) {
	t.Helper()
	context := cloudContext(t, shell)
	context.Login.ProductSessions = method
	if err := contexts.Save(shell.StateRoot, loadDocument(t, shell).Put(context)); err != nil {
		t.Fatal(err)
	}
}

func newSignedInCloudOrgsByDevice(t *testing.T) *cloudLogin {
	t.Helper()
	login := newCloudLoginWith(t, fakeissuer.Options{OrganizationUnits: cloudOrgs, RefreshScopeMode: "honor",
		DeviceInterval: 1})
	installCloudAPIM(t, login.shell)
	installCloudIAM(t, login.shell)
	login.run(t, "login", "--device")
	login.errOut.Reset()
	return login
}

func TestOrgUseOnADeviceCodeCloudContextRecordsTheOrganizationChosenOnThePage(t *testing.T) {
	login := newSignedInCloudOrgsByDevice(t)
	login.issuer.ChooseOrganizationUnit(betaUnit.ID)

	login.run(t, "org", "use", betaUnit.ID)

	if got := cloudContext(t, login.shell).Organization; got != betaUnit.ID {
		t.Errorf("context organization = %q, want %q", got, betaUnit.ID)
	}
	if login.opened != 0 {
		t.Errorf("org use on a device-code context opened the browser %d times", login.opened)
	}
	if !strings.Contains(login.errOut.String(), "choose the organization whose id is "+betaUnit.ID) {
		t.Errorf("the instructions do not say which organization to choose:\n%s", login.errOut)
	}
	if !strings.Contains(login.errOut.String(), "open this URL there first to sign out of WSO2 Cloud:\n"+
		login.issuer.URL) {
		t.Errorf("the instructions do not print the sign-out URL:\n%s", login.errOut)
	}
	stored, err := session.Store{StateRoot: login.shell.StateRoot}.Load(
		contexts.ProductSessionRef(cloudContext(t, login.shell).CredentialRef, "iam"))
	if err != nil || auth.TokenOrganization(stored.AccessToken).ID != betaUnit.ID {
		t.Errorf("iam session %v acts in %q, want %q", err, auth.TokenOrganization(stored.AccessToken).ID, betaUnit.ID)
	}
}

func TestOrgUseOnADeviceCodeCloudContextRefusesASignInInAnotherOrganization(t *testing.T) {
	login := newSignedInCloudOrgsByDevice(t)
	before := cloudContext(t, login.shell).Organization

	if code := login.shell.Run([]string{"org", "use", betaUnit.ID}); code != exit.AuthPolicy {
		t.Fatalf("exit %d, want %d; stderr %s", code, exit.AuthPolicy, login.errOut)
	}
	if !strings.Contains(login.errOut.String(), "auth.organization_mismatch") ||
		!strings.Contains(login.errOut.String(), "choose one on the sign-in page") {
		t.Errorf("refusal:\n%s", login.errOut)
	}
	if got := cloudContext(t, login.shell).Organization; got != before {
		t.Errorf("context organization = %q, want the previous %q kept", got, before)
	}
}

func TestOrgUseOnADeviceCodeCloudContextRefusesASignInThatNamesNoOrganization(t *testing.T) {
	login := newSignedInCloudOrgsByDevice(t)
	login.issuer.ChooseOrganizationUnit(betaUnit.ID)
	login.run(t, "org", "use", betaUnit.ID)
	before := cloudContext(t, login.shell).Organization
	login.issuer.ChooseOrganizationUnit("unlisted")
	login.errOut.Reset()

	if code := login.shell.Run([]string{"org", "use", cloudOrgs[0].ID}); code != exit.AuthPolicy {
		t.Fatalf("exit %d, want %d; stderr %s", code, exit.AuthPolicy, login.errOut)
	}
	for _, want := range []string{"auth.organization_unconfirmed", "could not be confirmed",
		fmt.Sprintf("still names the %q organization", before)} {
		if !strings.Contains(login.errOut.String(), want) {
			t.Errorf("the refusal does not say %q:\n%s", want, login.errOut)
		}
	}
	if got := cloudContext(t, login.shell).Organization; got != before {
		t.Errorf("context organization = %q, want the previous %q kept", got, before)
	}
	store := session.Store{StateRoot: login.shell.StateRoot}
	if _, err := store.Load(cloudContext(t, login.shell).CredentialRef); err == nil {
		t.Error("the unconfirmed sign-in's session was kept")
	}
}

func TestALoginOfADeviceCodeCloudContextThatNamesNoOrganizationIsRefusedAndRevoked(t *testing.T) {
	login := newCloudLoginWith(t, fakeissuer.Options{OrganizationUnits: cloudOrgs, RefreshScopeMode: "honor",
		DeviceInterval: 1, DeviceSSO: true, DeviceWithoutUserAttributes: true})
	installCloudAPIM(t, login.shell)
	installCloudIAM(t, login.shell)
	login.run(t, "login", "--device")
	context := cloudContext(t, login.shell)
	context.Organization = acmeUnit.ID
	if err := contexts.Save(login.shell.StateRoot, loadDocument(t, login.shell).Put(context)); err != nil {
		t.Fatal(err)
	}
	login.run(t, "logout", "--keep-browser-session")
	live := login.issuer.LiveRefreshTokens()
	login.errOut.Reset()

	if code := login.shell.Run([]string{"login"}); code != exit.AuthPolicy {
		t.Fatalf("exit %d, want %d; stderr %s", code, exit.AuthPolicy, login.errOut)
	}
	for _, want := range []string{"auth.organization_unconfirmed", "could not be confirmed",
		"choose the organization whose id is " + acmeUnit.ID, "kind: oauth-browser"} {
		if !strings.Contains(login.errOut.String(), want) {
			t.Errorf("the refusal does not say %q:\n%s", want, login.errOut)
		}
	}
	if got := login.issuer.LiveRefreshTokens(); got != live {
		t.Errorf("%d refresh tokens live after the refused sign-in, want the %d before it", got, live)
	}
	if _, err := (session.Store{StateRoot: login.shell.StateRoot}).Load(context.CredentialRef); err == nil {
		t.Error("the unconfirmed sign-in was stored")
	}
	if got := cloudContext(t, login.shell).Organization; got != acmeUnit.ID {
		t.Errorf("context organization = %q, want %q kept", got, acmeUnit.ID)
	}
}

func TestALoginOfADeviceCodeCloudContextInAnotherOrganizationIsRefusedAndRevoked(t *testing.T) {
	login := newSignedInCloudOrgsByDevice(t)
	login.issuer.ChooseOrganizationUnit(betaUnit.ID)
	login.run(t, "org", "use", betaUnit.ID)
	login.issuer.ChooseOrganizationUnit(cloudOrgs[0].ID)
	live := login.issuer.LiveRefreshTokens()
	login.errOut.Reset()

	if code := login.shell.Run([]string{"login"}); code != exit.AuthPolicy {
		t.Fatalf("exit %d, want %d; stderr %s", code, exit.AuthPolicy, login.errOut)
	}
	if !strings.Contains(login.errOut.String(), "auth.organization_mismatch") ||
		!strings.Contains(login.errOut.String(), "on the sign-in page") {
		t.Errorf("refusal:\n%s", login.errOut)
	}
	if got := login.issuer.LiveRefreshTokens(); got != live {
		t.Errorf("%d refresh tokens live after the refused sign-in, want the %d before it", got, live)
	}
	if got := cloudContext(t, login.shell).Organization; got != betaUnit.ID {
		t.Errorf("context organization = %q, want %q kept", got, betaUnit.ID)
	}
}

func TestOrgUseWithNoArgumentOnADeviceCodeCloudContextRecordsWhatTheSignInChose(t *testing.T) {
	login := newSignedInCloudOrgsByDevice(t)
	login.issuer.ChooseOrganizationUnit(betaUnit.ID)

	login.run(t, "org", "use")

	if got := cloudContext(t, login.shell).Organization; got != betaUnit.ID {
		t.Errorf("context organization = %q, want the chosen %q", got, betaUnit.ID)
	}
	if !strings.Contains(login.errOut.String(), "choose the organization to switch to") {
		t.Errorf("the instructions do not ask to choose:\n%s", login.errOut)
	}
	if login.opened != 0 {
		t.Errorf("opened the browser %d times", login.opened)
	}
}
