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

package acceptance_test

import (
	"encoding/json"
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
)

// logout runs wso2 logout with the given extra arguments and requires success.
//
// Every outcome this command reports is a success, including the ones where the
// issuer refused or was never asked, so a test that allowed a non-zero exit
// would not be testing the decision in
// docs/adr/0010-best-effort-revocation-on-session-end.md.
func (d *loginDeployment) logout(t *testing.T, args ...string) {
	t.Helper()
	if code := d.shell.Run(append([]string{"logout"}, args...)); code != exit.OK {
		t.Fatalf("wso2 logout exited %d\nstdout:\n%s\nstderr:\n%s", code, d.out, d.errOut)
	}
}

// sessionStored reports whether the secure store still holds a session.
func (d *loginDeployment) sessionStored() bool {
	_, err := (session.Store{StateRoot: d.stateRoot}).Load(loginCredentialRef)
	return err == nil
}

// A deployment that serves revocation is told, the refresh token stops renewing
// at the issuer, and the local session is gone. The three are asserted together
// because any one of them alone would pass against a command that only did the
// other two.
func TestLogoutRevokesAtTheIssuerAndRemovesTheSession(t *testing.T) {
	deployment := deployLoginWithoutModule(t, fakeissuer.Options{AllowAnyLoopbackPort: true}, nil)
	deployment.login(t)
	refreshToken := deployment.storedSession(t).RefreshToken

	deployment.logout(t)

	if deployment.sessionStored() {
		t.Error("the secure store still holds a session after logout")
	}
	if deployment.issuer.RefreshTokenLive(refreshToken) {
		t.Error("the issuer would still renew a session with the revoked refresh token")
	}
	if !strings.Contains(deployment.out.String(), "confirmed") {
		t.Errorf("logout did not report a confirmed revocation:\n%s", deployment.out)
	}
}

// After a logout, a command that needs access is refused with the guidance that
// names how to log in again. This is the acceptance criterion a user actually
// feels: the session is not merely absent from a store they cannot see.
func TestAfterLogoutAModuleCommandIsRefusedWithLoginGuidance(t *testing.T) {
	deployment := deployLogin(t, fakeissuer.Options{AllowAnyLoopbackPort: true}, nil)
	deployment.login(t)
	deployment.logout(t)
	deployment.out.Reset()
	deployment.errOut.Reset()

	if code := deployment.status(t); code == exit.OK {
		t.Fatalf("a module command succeeded after logout\nstdout:\n%s", deployment.out)
	}
	refusal := deployment.errOut.String()
	if !strings.Contains(refusal, "auth.login_required") {
		t.Errorf("refusal does not carry auth.login_required:\n%s", refusal)
	}
	if !strings.Contains(refusal, "wso2 login") {
		t.Errorf("refusal does not name how to log in again:\n%s", refusal)
	}
}

// A deployment publishing no revocation endpoint is never asked, and the shell
// says so rather than claiming a revocation it did not obtain. The local
// session still goes: a user who asked to end a session does not keep one
// because the deployment offers no way to retract it.
func TestLogoutWhenTheIssuerAdvertisesNoRevocationEndpoint(t *testing.T) {
	deployment := deployLoginWithoutModule(t, fakeissuer.Options{
		AllowAnyLoopbackPort:   true,
		OmitRevocationEndpoint: true,
	}, nil)
	deployment.login(t)
	refreshToken := deployment.storedSession(t).RefreshToken

	deployment.logout(t)

	if deployment.sessionStored() {
		t.Error("the secure store still holds a session after logout")
	}
	if !deployment.issuer.RefreshTokenLive(refreshToken) {
		t.Error("nothing was revoked, so the refresh token should still renew at the issuer")
	}
	reported := deployment.out.String()
	if !strings.Contains(reported, "not-attempted") {
		t.Errorf("logout did not report the revocation as not attempted:\n%s", reported)
	}
	if !strings.Contains(reported, "no revocation endpoint") {
		t.Errorf("logout did not explain why the issuer was not asked:\n%s", reported)
	}
}

// A deployment that advertises revocation and then declines the request is
// reported as a refusal, and the user is told the issuer's copy may remain
// usable. This is the outcome the command's name most invites lying about.
func TestLogoutWhenTheIssuerRefusesRevocation(t *testing.T) {
	deployment := deployLoginWithoutModule(t, fakeissuer.Options{
		AllowAnyLoopbackPort: true,
		RefuseRevocation:     true,
	}, nil)
	deployment.login(t)

	deployment.logout(t)

	if deployment.sessionStored() {
		t.Error("the secure store still holds a session after logout")
	}
	reported := deployment.out.String()
	if !strings.Contains(reported, "failed") {
		t.Errorf("logout did not report the revocation as failed:\n%s", reported)
	}
	if !strings.Contains(reported, "may remain usable") {
		t.Errorf("logout did not say the issuer's copy may survive:\n%s", reported)
	}
}

// Logging out twice is not an error the second time. Nothing is stored, which
// is the state the user asked for.
func TestLogoutWithNoSessionSucceeds(t *testing.T) {
	deployment := deployLoginWithoutModule(t, fakeissuer.Options{AllowAnyLoopbackPort: true}, nil)

	deployment.logout(t)

	reported := deployment.out.String()
	if !strings.Contains(reported, "No session was stored") {
		t.Errorf("logout did not state plainly that there was nothing to end:\n%s", reported)
	}
	if strings.Contains(reported, "error") || strings.Contains(deployment.errOut.String(), "error") {
		t.Errorf("logout reported an error for having nothing to do:\nstdout:\n%s\nstderr:\n%s",
			reported, deployment.errOut)
	}
}

// A stored session too stale for the shell to read is still a session on this
// machine. Ending it must be reported as ending one: the store cannot tell a
// stale entry from a missing one, so a command that inferred existence from what
// it managed to read would delete a real entry and tell the user nothing was
// stored.
func TestLogoutEndsASessionItCannotRead(t *testing.T) {
	deployment := deployLoginWithoutModule(t, fakeissuer.Options{AllowAnyLoopbackPort: true}, nil)
	if err := keyring.Set(session.Service, loginCredentialRef, "not json"); err != nil {
		t.Fatalf("seeding a stale session: %v", err)
	}

	deployment.logout(t, "--output", "json")

	var reported struct {
		Session    string `json:"session"`
		Revocation string `json:"revocation"`
	}
	if err := json.Unmarshal(deployment.out.Bytes(), &reported); err != nil {
		t.Fatalf("logout did not render a JSON document: %v\n%s", err, deployment.out)
	}
	if reported.Session != "ended" {
		t.Errorf("session = %q, want ended: a stale entry was removed and must be reported as one",
			reported.Session)
	}
	// Nothing readable came out of the entry, so there was no refresh token to
	// name in a revocation request. Never asked is the honest report.
	if reported.Revocation != "not-attempted" {
		t.Errorf("revocation = %q, want not-attempted", reported.Revocation)
	}
	if deployment.sessionStored() {
		t.Error("the stale entry is still in the secure store")
	}
}

// Every outcome the command establishes is readable by a script, because what
// the issuer was told is not observable any other way.
func TestLogoutRendersJSON(t *testing.T) {
	deployment := deployLoginWithoutModule(t, fakeissuer.Options{AllowAnyLoopbackPort: true}, nil)
	deployment.login(t)
	deployment.out.Reset()

	deployment.logout(t, "--output", "json")

	var reported struct {
		Context        string `json:"context"`
		Identity       string `json:"identity"`
		Session        string `json:"session"`
		Revocation     string `json:"revocation"`
		SharedContexts string `json:"sharedContexts"`
		BrowserSession string `json:"browserSession"`
	}
	if err := json.Unmarshal(deployment.out.Bytes(), &reported); err != nil {
		t.Fatalf("logout did not render a JSON document: %v\n%s", err, deployment.out)
	}
	if reported.Context != referenceContextName {
		t.Errorf("context = %q, want %q", reported.Context, referenceContextName)
	}
	if reported.Identity != loginIdentityName {
		t.Errorf("identity = %q, want %q", reported.Identity, loginIdentityName)
	}
	if reported.Session != "ended" {
		t.Errorf("session = %q, want ended", reported.Session)
	}
	if reported.Revocation != "confirmed" {
		t.Errorf("revocation = %q, want confirmed", reported.Revocation)
	}
	if reported.SharedContexts != referenceContextName {
		t.Errorf("sharedContexts = %q, want %q", reported.SharedContexts, referenceContextName)
	}
	// The caveat the table prints as a note has to reach a JSON caller too: it
	// is the one thing users read into this command that is not true.
	if reported.BrowserSession != "unaffected" {
		t.Errorf("browserSession = %q, want unaffected", reported.BrowserSession)
	}
}

// A session is keyed by the identity's credential reference, so contexts
// sharing an identity share one session. Ending it ends theirs, and a user who
// named one context by hand would not otherwise learn that.
func TestLogoutNamesEveryContextSharingTheSession(t *testing.T) {
	const secondContext = "reference-staging"
	deployment := deployLoginWithoutModule(t, fakeissuer.Options{AllowAnyLoopbackPort: true},
		func(document *contexts.Document) {
			document.Contexts = append(document.Contexts, contexts.Context{
				Name:         secondContext,
				Identity:     loginIdentityName,
				Organization: referenceOrganization,
			})
		})
	deployment.login(t)

	deployment.logout(t)

	reported := deployment.out.String()
	if !strings.Contains(reported, "share one session") {
		t.Errorf("logout did not warn that the session is shared:\n%s", reported)
	}
	if !strings.Contains(reported, secondContext) {
		t.Errorf("logout did not name the other affected context:\n%s", reported)
	}
}

// No token material reaches any output surface, under any outcome. The refresh
// token is the value logout handles most directly, so this is asserted where it
// is most at risk of being echoed.
func TestLogoutRevealsNoTokenMaterial(t *testing.T) {
	for name, options := range map[string]fakeissuer.Options{
		"revoked":       {AllowAnyLoopbackPort: true},
		"not-attempted": {AllowAnyLoopbackPort: true, OmitRevocationEndpoint: true},
		"refused":       {AllowAnyLoopbackPort: true, RefuseRevocation: true},
	} {
		t.Run(name, func(t *testing.T) {
			deployment := deployLoginWithoutModule(t, options, nil)
			deployment.login(t)
			stored := deployment.storedSession(t)
			deployment.out.Reset()
			deployment.errOut.Reset()

			deployment.logout(t)

			for label, secret := range map[string]string{
				"refresh token": stored.RefreshToken,
				"access token":  stored.AccessToken,
			} {
				if secret == "" {
					continue
				}
				if strings.Contains(deployment.out.String(), secret) {
					t.Errorf("the %s reached standard output", label)
				}
				if strings.Contains(deployment.errOut.String(), secret) {
					t.Errorf("the %s reached the diagnostic stream", label)
				}
			}
		})
	}
}

// An identity that acquires access inline holds no session, and is told so
// rather than being reported as having ended one.
func TestLogoutRefusesAnIdentityThatHoldsNoSession(t *testing.T) {
	deployment := deployLoginWithoutModule(t, fakeissuer.Options{AllowAnyLoopbackPort: true},
		func(document *contexts.Document) {
			document.Identities[0].Auth.Kind = contexts.KindClientCredentials
			document.Identities[0].Auth.ClientSecretVariable = inlineSecretVariable
			// The schema refuses a secure-store reference on a non-interactive
			// identity, which is the same fact this test is about: such an
			// identity has no session, so it has nowhere to keep one.
			document.Identities[0].Auth.CredentialRef = ""
		})

	if code := deployment.shell.Run([]string{"logout"}); code == exit.OK {
		t.Fatalf("logout succeeded for an identity that holds no session\nstdout:\n%s",
			deployment.out)
	}
	if !strings.Contains(deployment.errOut.String(), "auth.logout_not_required") {
		t.Errorf("refusal does not carry auth.logout_not_required:\n%s", deployment.errOut)
	}
}
