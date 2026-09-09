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

package auth_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/zalando/go-keyring"
)

const (
	// targetAudience is what the product's own issuer stamps into aud: on
	// API Manager, the public client's identifier.
	targetAudience = "apim-public-client"
	// loginClient is the client the login session was established as, and
	// so the audience of every identity token the login issuer mints for it.
	loginClient = "wso2cli"
)

// derivedDeployment is a login issuer holding the session, and a second
// issuer, the product's own, configured to trust the first.
type derivedDeployment struct {
	login, target *fakeissuer.Issuer
	stateRoot     string
	seeded        string
}

// seedDerivedDeployment stores a session granted openid and the assertion
// claims at the login issuer, and stands up the product issuer that will take
// the identity token as a bearer assertion.
func seedDerivedDeployment(t *testing.T, login, target fakeissuer.Options) derivedDeployment {
	t.Helper()
	keyring.MockInit()
	if login.RefreshScopeMode == "" {
		login.RefreshScopeMode = "honor"
	}
	login.RotateRefreshTokens = true
	loginIssuer := fakeissuer.New(t, login)
	target.TrustedIssuer = loginIssuer
	target.Audience = targetAudience
	if target.BearerAlias == "" {
		target.BearerAlias = loginClient
	}
	targetIssuer := fakeissuer.New(t, target)
	root := t.TempDir()
	seeded := loginIssuer.SeedSession([]string{"openid", "groups", "reference:status:read"})
	if err := (session.Store{StateRoot: root}).Save(contexts.ProductSessionRef(sessionRef, "reference"),
		session.Session{Issuer: loginIssuer.URL, RefreshToken: seeded}); err != nil {
		t.Fatalf("seeding the stored session: %v", err)
	}
	return derivedDeployment{login: loginIssuer, target: targetIssuer, stateRoot: root, seeded: seeded}
}

func (d derivedDeployment) broker(t *testing.T) *auth.Broker {
	t.Helper()
	broker := productionBroker(t, contexts.KindOAuthBrowser)
	broker.Selection.Identity.Auth.Issuer = d.login.URL
	broker.Selection.Identity.Auth.ClientID = loginClient
	broker.Selection.Identity.Auth.CredentialRef = sessionRef
	withProduct(broker, contexts.Product{
		Endpoint: "https://apim.example.test",
		Audience: targetAudience,
		Scopes:   []string{readScope, writeScope},
		Grant: &contexts.Grant{
			Kind:     contexts.GrantJWTBearer,
			Issuer:   d.target.URL,
			ClientID: targetAudience,
			Scopes:   []string{"groups"},
		},
	})
	broker.StateRoot = d.stateRoot
	broker.HTTPClient = d.login.HTTPClient()
	broker.Now = nil
	return broker
}

func (d derivedDeployment) storedSession(t *testing.T) session.Session {
	t.Helper()
	stored, err := session.Store{StateRoot: d.stateRoot}.Load(contexts.ProductSessionRef(sessionRef, "reference"))
	if err != nil {
		t.Fatalf("loading the stored session: %v", err)
	}
	return stored
}

func TestAGrantProductIsAnsweredFromTheSessionAtItsOwnIssuer(t *testing.T) {
	deployment := seedDerivedDeployment(t, fakeissuer.Options{}, fakeissuer.Options{})
	grant, err := deployment.broker(t).Acquire(declaredRequest())
	if err != nil {
		t.Fatalf("a grant product was refused: %v", err)
	}
	active, scopes, audiences := deployment.target.Introspect(t, grant.Token)
	if !active {
		t.Fatal("the token was not minted by the product's own issuer")
	}
	if !slices.Equal(scopes, []string{readScope}) {
		t.Fatalf("the derived token carries %v, want exactly %v", scopes, []string{readScope})
	}
	if !slices.Contains(audiences, targetAudience) {
		t.Fatalf("the derived token is bound to %v, want %q", audiences, targetAudience)
	}
	if active, _, _ := deployment.login.Introspect(t, grant.Token); active {
		t.Fatal("the module was handed the login issuer's own token, not a derived one")
	}
	if grant.ExpiresAt.IsZero() {
		t.Fatal("the grant states no expiry")
	}
}

func TestATargetThatIssuesNoneOfTheScopesAskedSaysTheUserIsNotAuthorized(t *testing.T) {
	deployment := seedDerivedDeployment(t, fakeissuer.Options{}, fakeissuer.Options{BearerScopeMode: "default"})
	_, err := deployment.broker(t).Acquire(declaredRequest())
	assertDenialCode(t, err, "auth.narrowing_unavailable")
	if !strings.Contains(err.Error(), "not authorized for the product") {
		t.Fatalf("the refusal does not say the user is not authorized: %v", err)
	}
	var denial auth.Denial
	if !errors.As(err, &denial) {
		t.Fatalf("the refusal is not a denial: %v", err)
	}
	// Both causes reach this state and the shell cannot tell them apart here,
	// so the recovery has to name both: the role the user may not hold, and
	// the assertion scopes that decide what the assertion carries to say so.
	if !strings.Contains(denial.Problem.Recovery, deployment.target.URL) {
		t.Fatalf("the recovery does not name the issuer to ask: %q", denial.Problem.Recovery)
	}
	if !strings.Contains(denial.Problem.Recovery, "--grant-scopes") {
		t.Fatalf("the recovery does not name the assertion scopes: %q", denial.Problem.Recovery)
	}
	if strings.Contains(denial.Problem.Recovery, "API resource registration") {
		t.Fatalf("the recovery still points at the deployment's registration: %q", denial.Problem.Recovery)
	}
}

// A deployment that grants some of what was asked for is a different fault
// from one that grants none: the user holds a role, it just does not carry
// everything. That keeps the registration-shaped refusal, which is the true
// one for it.
func TestATargetThatIssuesSomeOfTheScopesAskedIsRefusedAsANarrowing(t *testing.T) {
	deployment := seedDerivedDeployment(t, fakeissuer.Options{}, fakeissuer.Options{BearerScopeMode: "partial"})
	_, err := deployment.broker(t).Acquire(auth.Request{
		Audience: audience, Scopes: []string{readScope, writeScope},
	})
	assertDenialCode(t, err, "auth.narrowing_unavailable")
	if !strings.Contains(err.Error(), "the deployment issued") {
		t.Fatalf("the refusal does not say what was issued: %v", err)
	}
	if strings.Contains(err.Error(), "not authorized for the product") {
		t.Fatalf("a partial grant was reported as the user not being authorized: %v", err)
	}
}

func TestATargetThatRefusesTheAssertionIsReportedAsTrustNotConfigured(t *testing.T) {
	deployment := seedDerivedDeployment(t, fakeissuer.Options{}, fakeissuer.Options{BearerScopeMode: "refuse"})
	_, err := deployment.broker(t).Acquire(declaredRequest())
	assertDenialCode(t, err, "auth.trust_not_configured")
	var denial auth.Denial
	if !errors.As(err, &denial) {
		t.Fatalf("not a denial: %v", err)
	}
	reported := denial.Reported()
	if !strings.Contains(reported.Recovery, deployment.target.URL) {
		t.Fatalf("the guidance does not name the grant issuer: %q", reported.Recovery)
	}
	stored := deployment.storedSession(t)
	for _, text := range []string{denial.Error(), reported.Error(), reported.Recovery} {
		if strings.Contains(text, stored.RefreshToken) || strings.Contains(text, deployment.seeded) {
			t.Fatal("a refusal carried session material")
		}
	}
}

func TestAnAliasTheAssertionDoesNotCarryIsRefused(t *testing.T) {
	deployment := seedDerivedDeployment(t, fakeissuer.Options{}, fakeissuer.Options{BearerAlias: "someone-else"})
	_, err := deployment.broker(t).Acquire(declaredRequest())
	assertDenialCode(t, err, "auth.trust_not_configured")
}

func TestTheRotatedSessionIsPersistedBeforeTheAssertionIsPresented(t *testing.T) {
	deployment := seedDerivedDeployment(t, fakeissuer.Options{}, fakeissuer.Options{BearerScopeMode: "refuse"})
	_, err := deployment.broker(t).Acquire(declaredRequest())
	assertDenialCode(t, err, "auth.trust_not_configured")
	stored := deployment.storedSession(t)
	if stored.RefreshToken == deployment.seeded {
		t.Fatal("the rotated refresh token was not persisted")
	}
	if !deployment.login.RefreshTokenLive(stored.RefreshToken) {
		t.Fatal("the stored refresh token is not the live one")
	}
}

func TestASessionThatYieldsNoIdentityTokenIsRefused(t *testing.T) {
	// A login issuer that ignores the requested scopes renews only what the
	// session was granted; seeded without openid, it mints no identity token.
	deployment := seedDerivedDeployment(t, fakeissuer.Options{RefreshScopeMode: "ignore"}, fakeissuer.Options{})
	withoutOpenID := deployment.login.SeedSession([]string{"groups", "reference:status:read"})
	if err := (session.Store{StateRoot: deployment.stateRoot}).Save(
		contexts.ProductSessionRef(sessionRef, "reference"),
		session.Session{Issuer: deployment.login.URL, RefreshToken: withoutOpenID}); err != nil {
		t.Fatalf("reseeding: %v", err)
	}
	_, err := deployment.broker(t).Acquire(declaredRequest())
	assertDenialCode(t, err, "auth.narrowing_unavailable")
	if !strings.Contains(err.Error(), "identity token") {
		t.Fatalf("the refusal does not say what was missing: %v", err)
	}
}

func TestAClientCredentialsIdentityCannotUseAGrant(t *testing.T) {
	deployment := seedDerivedDeployment(t, fakeissuer.Options{}, fakeissuer.Options{})
	broker := deployment.broker(t)
	broker.Selection.Identity.Auth.Kind = contexts.KindClientCredentials
	broker.Selection.Identity.Auth.CredentialRef = ""
	broker.Selection.Identity.Auth.ClientSecretVariable = "WSO2_SECRET"
	broker.Credentials = func(string) (string, bool) { return "secret", true }
	_, err := broker.Acquire(declaredRequest())
	assertDenialCode(t, err, "auth.kind_not_implemented")
}

// TestAThunderJWTBearerGrantWithNoResourceIsRefusedAtUse covers F3: a
// document from before a jwt-bearer grant's resource was required to bind an
// assertion session still decodes, but a Thunder-like deployment (a
// token-resource derivation) cannot actually derive that product's access
// without one, so the broker refuses it here, at the point that needs it.
func TestAThunderJWTBearerGrantWithNoResourceIsRefusedAtUse(t *testing.T) {
	deployment := seedDerivedDeployment(t, fakeissuer.Options{}, fakeissuer.Options{})
	broker := deployment.broker(t)
	broker.Selection.Identity.Auth.Provider = contexts.ProviderThunder
	_, err := broker.Acquire(declaredRequest())
	assertDenialCode(t, err, "auth.product_not_configured")
	if !strings.Contains(err.Error(), "resource") {
		t.Fatalf("the refusal does not mention the missing resource: %v", err)
	}
}

// assertDenialCode fails unless err is a broker denial carrying code.
func assertDenialCode(t *testing.T, err error, code string) {
	t.Helper()
	var refusal auth.Denial
	if !errors.As(err, &refusal) {
		t.Fatalf("not a denial: %v", err)
	}
	if refusal.Problem.Code != code {
		t.Fatalf("denied with %q, want %q: %v", refusal.Problem.Code, code, err)
	}
}
