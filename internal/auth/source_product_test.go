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
	"time"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
)

const (
	// siblingNamespace sorts after "reference" so the fixture's login product
	// (seeded by seedBrowserSession, under the bare session ref) stays the one
	// Identity.LoginAccess picks: it names the first direct product by
	// namespace, and a namespace that sorted earlier would make the sibling
	// itself the login instead of a second session beside it.
	siblingNamespace = "scim"
	siblingAudience  = "https://localhost:8090/mcp"
	siblingScope     = "system"
)

// thunderLikeDeployment is a resource-bound issuer with the login session
// seeded for the reference product and, optionally, a sibling session for
// a second resource.
func thunderLikeDeployment(t *testing.T, seedSibling bool) browserDeployment {
	t.Helper()
	deployment := seedBrowserSession(t, fakeissuer.Options{RequireResource: true})
	if seedSibling {
		seeded := deployment.issuer.SeedSessionFor([]string{siblingScope}, siblingAudience)
		store := session.Store{StateRoot: deployment.stateRoot}
		if err := store.Save(contexts.ProductSessionRef(sessionRef, siblingNamespace),
			session.Session{Issuer: deployment.issuer.URL, RefreshToken: seeded}); err != nil {
			t.Fatal(err)
		}
	}
	return deployment
}

// siblingBroker is the broker the scim module would build on that deployment.
func siblingBroker(t *testing.T, deployment browserDeployment) *auth.Broker {
	t.Helper()
	broker := deployment.broker(t)
	broker.Selection.Identity.Auth.Provider = contexts.ProviderThunder
	broker.Selection.Identity.Products[siblingNamespace] = contexts.Product{
		Endpoint: deployment.issuer.URL, Audience: siblingAudience, Scopes: []string{siblingScope},
	}
	broker.Namespace = siblingNamespace
	broker.Capabilities.AuthAudiences = []string{siblingAudience}
	broker.Capabilities.AuthScopes = []string{siblingScope}
	return broker
}

func TestASiblingProductIsAnsweredFromItsOwnSession(t *testing.T) {
	deployment := thunderLikeDeployment(t, true)
	broker := siblingBroker(t, deployment)
	grant, err := broker.Acquire(auth.Request{Audience: siblingAudience, Scopes: []string{siblingScope}})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	active, scopes, audiences := deployment.issuer.Introspect(t, grant.Token)
	if !active || len(scopes) != 1 || scopes[0] != siblingScope || audiences[0] != siblingAudience {
		t.Fatalf("token scopes %v audiences %v", scopes, audiences)
	}
	// The login session is untouched: it still holds the seeded refresh token.
	if deployment.storedSession(t).RefreshToken != deployment.seeded {
		t.Fatal("the sibling derivation rotated the login session")
	}
}

func TestAMissingSiblingSessionIsEstablishedOnFirstUse(t *testing.T) {
	deployment := thunderLikeDeployment(t, false)
	broker := siblingBroker(t, deployment)
	var asked contexts.ProductAccess
	broker.EstablishSession = func(access contexts.ProductAccess) error {
		asked = access
		seeded := deployment.issuer.SeedSessionFor(access.Scopes, access.Resource)
		return session.Store{StateRoot: deployment.stateRoot}.Save(access.SessionRef,
			session.Session{Issuer: access.Issuer, RefreshToken: seeded, Strategy: access.Strategy})
	}
	if _, err := broker.Acquire(auth.Request{Audience: siblingAudience, Scopes: []string{siblingScope}}); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if asked.Namespace != siblingNamespace || asked.Strategy != contexts.StrategySibling ||
		asked.Resource != siblingAudience || asked.SessionRef != contexts.ProductSessionRef(sessionRef, siblingNamespace) {
		t.Fatalf("the hook was asked for %+v", asked)
	}
}

func TestAMissingSiblingSessionWithNoWayToEstablishItIsRefused(t *testing.T) {
	deployment := thunderLikeDeployment(t, false)
	broker := siblingBroker(t, deployment)
	_, err := broker.Acquire(auth.Request{Audience: siblingAudience, Scopes: []string{siblingScope}})
	var denial auth.Denial
	if !errors.As(err, &denial) || denial.Problem.Code != "auth.session_required" {
		t.Fatalf("got %v, want auth.session_required", err)
	}
	if !containsText(denial.Problem.Recovery, "wso2 login --only scim") {
		t.Fatalf("recovery %q does not name the login to run", denial.Problem.Recovery)
	}
}

func TestAMissingLoginSessionIsNeverEstablishedByTheBroker(t *testing.T) {
	// The bare login session is wso2 login's to establish; the broker only
	// fills in a product's own session beside an existing login.
	keyring.MockInit()
	deployment := thunderLikeDeployment(t, false)
	if _, err := (session.Store{StateRoot: deployment.stateRoot}).Delete(sessionRef); err != nil {
		t.Fatal(err)
	}
	broker := deployment.broker(t)
	broker.EstablishSession = func(contexts.ProductAccess) error {
		t.Fatal("the broker tried to establish the login session")
		return nil
	}
	_, err := broker.Acquire(auth.Request{Audience: audience, Scopes: []string{readScope}})
	var denial auth.Denial
	if !errors.As(err, &denial) || denial.Problem.Code != "auth.login_required" {
		t.Fatalf("got %v, want auth.login_required", err)
	}
}

func TestAFederatedProductIsRefreshedAtItsOwnIssuerAsItsOwnClient(t *testing.T) {
	deployment := seedBrowserSession(t, fakeissuer.Options{})
	const apimAudience = "apim-cli-client"
	product := fakeissuer.New(t, fakeissuer.Options{Audience: apimAudience})
	seeded := product.SeedSession([]string{"apim:api_view"})
	ref := contexts.ProductSessionRef(sessionRef, "apim")
	if err := (session.Store{StateRoot: deployment.stateRoot}).Save(ref,
		session.Session{Issuer: product.URL, RefreshToken: seeded, Strategy: contexts.StrategyFederated}); err != nil {
		t.Fatal(err)
	}
	broker := deployment.broker(t)
	broker.Selection.Identity.Products["apim"] = contexts.Product{
		Endpoint: product.URL, Audience: apimAudience, Scopes: []string{"apim:api_view"},
		Grant: &contexts.Grant{Kind: contexts.GrantFederated, Issuer: product.URL, ClientID: apimAudience},
	}
	broker.Namespace = "apim"
	broker.Capabilities.AuthAudiences = []string{apimAudience}
	broker.Capabilities.AuthScopes = []string{"apim:api_view"}
	broker.HTTPClient = product.HTTPClient()
	grant, err := broker.Acquire(auth.Request{Audience: apimAudience, Scopes: []string{"apim:api_view"}})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	active, scopes, audiences := product.Introspect(t, grant.Token)
	if !active || scopes[0] != "apim:api_view" || audiences[0] != apimAudience {
		t.Fatalf("token scopes %v audiences %v", scopes, audiences)
	}
	if issuedBy, _, _ := deployment.issuer.Introspect(t, grant.Token); issuedBy {
		t.Fatal("the login issuer minted the federated product's token")
	}
}

func containsText(text, want string) bool { return len(want) > 0 && strings.Contains(text, want) }

// TestALoginProductDriftIsRefused covers F1: a product whose namespace sorts
// before the identity's current login product silently becomes the login
// product itself, once it is recorded. Without a guard, the session stored
// for the old login product — established for a different client or a
// different scope set — would be presented as the new one's.
func TestALoginProductDriftIsRefused(t *testing.T) {
	deployment := seedBrowserSession(t, fakeissuer.Options{RefreshScopeMode: "honor"})
	// The stored session records what an ordinary login for "reference" —
	// today's login product — would have left behind.
	store := session.Store{StateRoot: deployment.stateRoot}
	if err := store.Save(sessionRef, session.Session{
		Issuer: deployment.issuer.URL, RefreshToken: deployment.seeded,
		Strategy: contexts.StrategyDirect, ClientID: "wso2cli", Scopes: []string{readScope, writeScope},
	}); err != nil {
		t.Fatal(err)
	}
	broker := deployment.broker(t)
	// "apim" sorts before "reference", so recording it as a second direct
	// product makes it the login product LoginAccess picks, and Access("apim")
	// would otherwise inherit the bare session ref the "reference" login
	// established, with "reference"'s scopes rather than apim's own.
	const apimAudience = "apim-status"
	const apimScope = "apim:view"
	broker.Selection.Identity.Products["apim"] = contexts.Product{
		Endpoint: "https://apim.example.test", Audience: apimAudience, Scopes: []string{apimScope},
	}
	broker.Namespace = "apim"
	broker.Capabilities.AuthAudiences = []string{apimAudience}
	broker.Capabilities.AuthScopes = []string{apimScope}

	_, err := broker.Acquire(auth.Request{Audience: apimAudience, Scopes: []string{apimScope}})
	var denial auth.Denial
	if !errors.As(err, &denial) || denial.Problem.Code != "auth.login_required" {
		t.Fatalf("got %v, want auth.login_required", err)
	}
	if !containsText(denial.Problem.Recovery, "wso2 login --only apim") {
		t.Fatalf("recovery %q does not name the login to run", denial.Problem.Recovery)
	}
	if stored, loadErr := store.Load(sessionRef); loadErr != nil || stored.RefreshToken != deployment.seeded {
		t.Fatalf("the drift refusal touched the stored session: %v %+v", loadErr, stored)
	}
}

// federatedDeployment seeds a login session and a federated apim product
// whose own issuer is product, storing under the product's ref whatever
// stored describes. It returns the broker the apim module would build.
func federatedDeployment(t *testing.T, product *fakeissuer.Issuer, stored session.Session) (browserDeployment, *auth.Broker) {
	t.Helper()
	deployment := seedBrowserSession(t, fakeissuer.Options{})
	const apimAudience = "apim-cli-client"
	stored.Issuer = product.URL
	stored.Strategy = contexts.StrategyFederated
	ref := contexts.ProductSessionRef(sessionRef, "apim")
	if err := (session.Store{StateRoot: deployment.stateRoot}).Save(ref, stored); err != nil {
		t.Fatal(err)
	}
	broker := deployment.broker(t)
	broker.Selection.Identity.Products["apim"] = contexts.Product{
		Endpoint: product.URL, Audience: apimAudience, Scopes: []string{"apim:api_view"},
		Grant: &contexts.Grant{Kind: contexts.GrantFederated, Issuer: product.URL, ClientID: apimAudience},
	}
	broker.Namespace = "apim"
	broker.Capabilities.AuthAudiences = []string{apimAudience}
	broker.Capabilities.AuthScopes = []string{"apim:api_view"}
	broker.HTTPClient = product.HTTPClient()
	return deployment, broker
}

// The code exchange at a federated issuer answers with an access token the
// product accepts; some issuers (API Manager, measured) then refuse to renew
// the management scope on a refresh. While that token is valid it is the
// answer, and a refresh the issuer would refuse is never attempted.
func TestAFederatedSessionServesItsStoredAccessTokenWhileValid(t *testing.T) {
	product := fakeissuer.New(t, fakeissuer.Options{Audience: "apim-cli-client", RefreshScopeMode: "reject"})
	minted := product.MintAccessToken([]string{"openid", "offline_access", "apim:api_view"}, "")
	_, broker := federatedDeployment(t, product, session.Session{
		RefreshToken: product.SeedSession([]string{"apim:api_view"}),
		AccessToken:  minted, ExpiresAt: time.Now().Add(time.Hour),
	})
	grant, err := broker.Acquire(auth.Request{Audience: "apim-cli-client", Scopes: []string{"apim:api_view"}})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if grant.Token != minted {
		t.Fatal("the broker did not serve the stored access token")
	}
}

// An expired stored token is not served; the session is refreshed as before.
// The issuer rotates on refresh, so a rotated stored refresh token is the
// proof the refresh happened (a re-minted token can be byte-identical to the
// stale one within the same second, so the token itself proves nothing).
func TestAnExpiredFederatedAccessTokenIsRefreshed(t *testing.T) {
	product := fakeissuer.New(t, fakeissuer.Options{Audience: "apim-cli-client", RotateRefreshTokens: true})
	seeded := product.SeedSession([]string{"apim:api_view"})
	deployment, broker := federatedDeployment(t, product, session.Session{
		RefreshToken: seeded,
		AccessToken:  product.MintAccessToken([]string{"apim:api_view"}, ""), ExpiresAt: time.Now().Add(-time.Minute),
	})
	grant, err := broker.Acquire(auth.Request{Audience: "apim-cli-client", Scopes: []string{"apim:api_view"}})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if active, _, _ := product.Introspect(t, grant.Token); !active {
		t.Fatal("the refreshed token was not minted by the product issuer")
	}
	stored, err := session.Store{StateRoot: deployment.stateRoot}.Load(contexts.ProductSessionRef(sessionRef, "apim"))
	if err != nil || stored.RefreshToken == seeded {
		t.Fatalf("the session was not refreshed (rotated): %v", err)
	}
}

// A stored token that does not carry the scopes asked for is not served
// either: the request is what the module is owed, whatever the exchange
// granted.
func TestAStoredAccessTokenWithOtherScopesIsNotServed(t *testing.T) {
	product := fakeissuer.New(t, fakeissuer.Options{Audience: "apim-cli-client"})
	other := product.MintAccessToken([]string{"openid", "apim:api_create"}, "")
	_, broker := federatedDeployment(t, product, session.Session{
		RefreshToken: product.SeedSession([]string{"apim:api_view"}),
		AccessToken:  other, ExpiresAt: time.Now().Add(time.Hour),
	})
	grant, err := broker.Acquire(auth.Request{Audience: "apim-cli-client", Scopes: []string{"apim:api_view"}})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if grant.Token == other {
		t.Fatal("the broker served a token carrying other permissions")
	}
}

// When the issuer refuses to renew the session to what was asked, the shell
// authorizes the product again through the hook, outside the lock, and
// serves the access token that authorization stored.
func TestAFederatedSessionTheIssuerWillNotRenewIsAuthorizedAgain(t *testing.T) {
	product := fakeissuer.New(t, fakeissuer.Options{Audience: "apim-cli-client", RefreshScopeMode: "reject"})
	deployment, broker := federatedDeployment(t, product, session.Session{
		RefreshToken: product.SeedSession([]string{"apim:api_view"}),
	})
	calls := 0
	fresh := ""
	broker.EstablishSession = func(access contexts.ProductAccess) error {
		calls++
		fresh = product.MintAccessToken([]string{"openid", "offline_access", "apim:api_view"}, "")
		return session.Store{StateRoot: deployment.stateRoot}.Save(access.SessionRef, session.Session{
			Issuer: access.Issuer, RefreshToken: product.SeedSession([]string{"apim:api_view"}),
			AccessToken: fresh, ExpiresAt: time.Now().Add(time.Hour), Strategy: access.Strategy,
		})
	}
	grant, err := broker.Acquire(auth.Request{Audience: "apim-cli-client", Scopes: []string{"apim:api_view"}})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if calls != 1 || grant.Token != fresh {
		t.Fatalf("hook called %d times, served the fresh token: %v", calls, grant.Token == fresh)
	}
}

// One re-authorization, never a loop: when the session it establishes still
// cannot serve the request, the narrowing refusal is reported as it always was.
func TestAReauthorizationThatStillCannotServeIsRefusedOnce(t *testing.T) {
	product := fakeissuer.New(t, fakeissuer.Options{Audience: "apim-cli-client", RefreshScopeMode: "reject"})
	deployment, broker := federatedDeployment(t, product, session.Session{
		RefreshToken: product.SeedSession([]string{"apim:api_view"}),
	})
	calls := 0
	broker.EstablishSession = func(access contexts.ProductAccess) error {
		calls++
		return session.Store{StateRoot: deployment.stateRoot}.Save(access.SessionRef, session.Session{
			Issuer: access.Issuer, RefreshToken: product.SeedSession([]string{"apim:api_view"}), Strategy: access.Strategy,
		})
	}
	_, err := broker.Acquire(auth.Request{Audience: "apim-cli-client", Scopes: []string{"apim:api_view"}})
	var denial auth.Denial
	if !errors.As(err, &denial) || denial.Problem.Code != "auth.narrowing_unavailable" {
		t.Fatalf("got %v, want auth.narrowing_unavailable", err)
	}
	if calls != 1 {
		t.Fatalf("hook called %d times, want exactly once", calls)
	}
	// The refusal says what an administrator has to do, not what the
	// protocol did: the user is not authorized for the product.
	if !containsText(denial.Problem.Message, "not authorized for the product") ||
		!containsText(denial.Problem.Recovery, "map this user's group to a role that carries apim:api_view") ||
		!containsText(denial.Problem.Recovery, "wso2 login --only apim") {
		t.Fatalf("refusal reads: %s / %s", denial.Problem.Message, denial.Problem.Recovery)
	}
}

// TestARequestWithNoScopesMeansTheProductsRecordedScopes is what lets a module
// stop carrying a --scope flag on every command: the record consents to the
// scopes, and a request naming none asks for exactly those.
func TestARequestWithNoScopesMeansTheProductsRecordedScopes(t *testing.T) {
	deployment := thunderLikeDeployment(t, true)
	broker := siblingBroker(t, deployment)
	grant, err := broker.Acquire(auth.Request{Audience: siblingAudience})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	active, scopes, audience := deployment.issuer.Introspect(t, grant.Token)
	if !active || !slices.Equal(scopes, []string{siblingScope}) || !slices.Equal(audience, []string{siblingAudience}) {
		t.Fatalf("token: active %v scopes %q audience %q", active, scopes, audience)
	}
}
