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

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
)

func newSignedInCloudOrgs(t *testing.T) *cloudLogin {
	t.Helper()
	login := newCloudLoginWith(t, fakeissuer.Options{OrganizationUnits: cloudOrgs, RefreshScopeMode: "honor"})
	installCloudAPIM(t, login.shell)
	installCloudIAM(t, login.shell)
	login.run(t, "login")
	login.mutex.Lock()
	login.paths = nil
	login.mutex.Unlock()
	return login
}

func TestOrgUseOnCloudSignsInAgainForTheOtherOrganization(t *testing.T) {
	login := newSignedInCloudOrgs(t)

	login.run(t, "org", "use", betaUnit.ID)

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

	if code := login.shell.Run([]string{"org", "use", "ou-this-user-is-not-in"}); code == exit.OK {
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
	if !strings.Contains(refusal, "shell.organization_list_unavailable") || !strings.Contains(refusal, "wso2 org use") {
		t.Errorf("org list refusal:\n%s", refusal)
	}
}
