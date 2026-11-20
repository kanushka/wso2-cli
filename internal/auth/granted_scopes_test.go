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
	"strings"
	"testing"
	"time"

	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
)

// withheldDeployment is a ThunderID session whose sign-in asked for both
// product scopes and was granted only the read scope, as ThunderID answers
// for a user whose role does not carry the other.
func withheldDeployment(t *testing.T) (browserDeployment, *auth.Broker) {
	t.Helper()
	deployment := seedBrowserSession(t, fakeissuer.Options{RequireResource: true, RefreshScopeMode: "honor"})
	store := session.Store{StateRoot: deployment.stateRoot}
	stored, err := store.Load(sessionRef)
	if err != nil {
		t.Fatal(err)
	}
	stored.RefreshToken = deployment.issuer.SeedSessionFor([]string{readScope}, audience)
	stored.Scopes = []string{readScope, writeScope}
	stored.Granted = []string{readScope}
	if err := store.Save(sessionRef, stored); err != nil {
		t.Fatal(err)
	}
	broker := deployment.broker(t)
	broker.Selection.Identity.Auth.Provider = contexts.ProviderThunder
	broker.Capabilities.AuthScopes = []string{readScope, writeScope}
	return deployment, broker
}

func TestARequestForAScopeTheSignInWasNotGrantedNamesItAndAnAdministrator(t *testing.T) {
	_, broker := withheldDeployment(t)

	refusal := denied(t, broker, auth.Request{Audience: audience, Scopes: []string{writeScope}})

	reported := refusal.Reported()
	if reported.Code != "auth.scope_not_granted" || !strings.Contains(reported.Message, writeScope) {
		t.Fatalf("refusal = %+v, want auth.scope_not_granted naming %s", reported, writeScope)
	}
	if !strings.Contains(reported.Recovery, "administrator") || !strings.Contains(reported.Recovery, writeScope) {
		t.Errorf("recovery = %q, want an administrator to grant %s", reported.Recovery, writeScope)
	}
	if strings.HasPrefix(reported.Recovery, "Run wso2 login") {
		t.Errorf("recovery = %q leads with signing in again, which cannot add the scope", reported.Recovery)
	}
}

func TestARequestNamingNoScopeIsGivenWhatTheSignInWasGranted(t *testing.T) {
	deployment, broker := withheldDeployment(t)

	grant, err := broker.Acquire(auth.Request{Audience: audience})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	_, scopes, _ := deployment.issuer.Introspect(t, grant.Token)
	if len(scopes) != 1 || scopes[0] != readScope {
		t.Errorf("access carried %v, want what the sign-in was granted, [%s]", scopes, readScope)
	}
}

func TestARequestForAScopeTheContextNeverAskedForSaysToSignInAgainAfterRecordingIt(t *testing.T) {
	_, broker := withheldDeployment(t)
	product := broker.Selection.Identity.Products["example"]
	product.Scopes = []string{readScope}
	withProduct(broker, product)
	store := session.Store{StateRoot: broker.StateRoot}
	stored, _ := store.Load(sessionRef)
	stored.Scopes = []string{readScope}
	if err := store.Save(sessionRef, stored); err != nil {
		t.Fatal(err)
	}

	refusal := denied(t, broker, auth.Request{Audience: audience, Scopes: []string{writeScope}})

	reported := refusal.Reported()
	if reported.Code != "auth.product_not_configured" || !strings.Contains(reported.Message, writeScope) {
		t.Fatalf("refusal = %+v, want auth.product_not_configured naming %s", reported, writeScope)
	}
	if !strings.Contains(reported.Recovery, "wso2 login") || !strings.Contains(reported.Recovery, writeScope) {
		t.Errorf("recovery = %q, want signing in again once the scope is recorded", reported.Recovery)
	}
	if !strings.Contains(reported.Recovery, "--replace --context "+broker.Selection.Context.Name) {
		t.Errorf("recovery = %q, want the record command to name the context", reported.Recovery)
	}
}

func TestAScopeAddedToTheRecordAfterTheSignInIsNotCalledWithheld(t *testing.T) {
	_, broker := withheldDeployment(t)
	store := session.Store{StateRoot: broker.StateRoot}
	stored, _ := store.Load(sessionRef)
	stored.Scopes = []string{readScope}
	if err := store.Save(sessionRef, stored); err != nil {
		t.Fatal(err)
	}

	_, err := broker.Acquire(auth.Request{Audience: audience, Scopes: []string{writeScope}})

	var refused auth.Denial
	if errors.As(err, &refused) && refused.Problem.Code == "auth.scope_not_granted" {
		t.Fatalf("a scope the sign-in never asked for was reported as withheld: %v", err)
	}
}

func TestASignInThatStatedNoProductScopeRefusesEachByName(t *testing.T) {
	_, broker := withheldDeployment(t)
	store := session.Store{StateRoot: broker.StateRoot}
	stored, _ := store.Load(sessionRef)
	stored.Granted = []string{}
	if err := store.Save(sessionRef, stored); err != nil {
		t.Fatal(err)
	}

	refusal := denied(t, broker, auth.Request{Audience: audience})

	if reported := refusal.Reported(); reported.Code != "auth.scope_not_granted" ||
		!strings.Contains(reported.Message, readScope) || !strings.Contains(reported.Message, writeScope) {
		t.Fatalf("refusal = %+v, want both scopes named as not granted", reported)
	}
}

func TestAnUnrecordedScopeOnAnExchangedProductSaysToRetryNotToLogIn(t *testing.T) {
	broker := productionBroker(t, contexts.KindOAuthBrowser)
	broker.Capabilities.AuthScopes = []string{readScope, writeScope}
	withProduct(broker, contexts.Product{
		Endpoint: "https://reference.example.test", Audience: audience, Scopes: []string{readScope},
		Grant: &contexts.Grant{Kind: contexts.GrantExchange},
	})

	recovery := denied(t, broker, auth.Request{Audience: audience, Scopes: []string{writeScope}}).Reported().Recovery

	if !strings.Contains(recovery, "--scopes "+readScope+","+writeScope) || !strings.Contains(recovery, "retry the command") ||
		strings.Contains(recovery, "login --only") {
		t.Errorf("recovery = %q, want the record command and a retry: an exchanged product has no login of its own", recovery)
	}
}

// A token whose claims state no scope says nothing about the grant; one that
// states an empty scope granted nothing.
func TestAnAbsentScopeClaimIsNotAnEmptyGrant(t *testing.T) {
	if granted := auth.GrantedScopes("", unsignedJWT(t, map[string]any{"sub": "user-1"})); granted != nil {
		t.Errorf("GrantedScopes = %#v for a token stating no scope, want nil", granted)
	}
	if granted := auth.GrantedScopes("", unsignedJWT(t, map[string]any{"sub": "user-1", "scope": ""})); granted == nil || len(granted) != 0 {
		t.Errorf("GrantedScopes = %#v for a token stating an empty scope, want an empty grant", granted)
	}
}

// A product authorized again is held to what that authorization granted, not
// to what the session it replaced was.
func TestAProductAuthorizedAgainIsHeldToItsNewGrant(t *testing.T) {
	const otherScope = "other"
	deployment := seedBrowserSession(t, fakeissuer.Options{RequireResource: true, RefreshScopeMode: "ignore"})
	broker := siblingBroker(t, deployment)
	product := broker.Selection.Identity.Products[siblingNamespace]
	product.Scopes = []string{siblingScope, otherScope}
	broker.Selection.Identity.Products[siblingNamespace] = product
	broker.Capabilities.AuthScopes = product.Scopes
	store := session.Store{StateRoot: deployment.stateRoot}
	ref := contexts.ProductSessionRef(sessionRef, siblingNamespace)
	save := func(granted []string) error {
		return store.Save(ref, session.Session{Issuer: deployment.issuer.URL,
			RefreshToken: deployment.issuer.SeedSessionFor([]string{siblingScope}, siblingAudience),
			Strategy:     contexts.StrategySibling, Bound: true, Resource: siblingAudience,
			Scopes: product.Scopes, Granted: granted})
	}
	if err := save(product.Scopes); err != nil {
		t.Fatal(err)
	}
	broker.EstablishSession = func(contexts.ProductAccess) error { return save([]string{siblingScope}) }

	grant, err := broker.Acquire(auth.Request{Audience: siblingAudience})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if _, scopes, _ := deployment.issuer.Introspect(t, grant.Token); len(scopes) != 1 || scopes[0] != siblingScope {
		t.Errorf("access carried %v, want what the new authorization granted, [%s]", scopes, siblingScope)
	}
}

// A sign-in that replaces the session while a request waits for the rotation
// lock decides what that request is held to.
func TestARequestIsHeldToTheGrantCurrentWhenItRenews(t *testing.T) {
	const otherScope = "other"
	deployment := seedBrowserSession(t, fakeissuer.Options{RequireResource: true, RefreshScopeMode: "ignore"})
	broker := siblingBroker(t, deployment)
	product := broker.Selection.Identity.Products[siblingNamespace]
	product.Scopes = []string{siblingScope, otherScope}
	broker.Selection.Identity.Products[siblingNamespace] = product
	broker.Capabilities.AuthScopes = product.Scopes
	store := session.Store{StateRoot: deployment.stateRoot}
	ref := contexts.ProductSessionRef(sessionRef, siblingNamespace)
	save := func(seeded, granted []string) error {
		return store.Save(ref, session.Session{Issuer: deployment.issuer.URL,
			RefreshToken: deployment.issuer.SeedSessionFor(seeded, siblingAudience),
			Strategy:     contexts.StrategySibling, Bound: true, Resource: siblingAudience,
			Scopes: product.Scopes, Granted: granted})
	}
	if err := save(product.Scopes, product.Scopes); err != nil {
		t.Fatal(err)
	}
	broker.EstablishSession = func(contexts.ProductAccess) error {
		t.Error("the request was authorized again")
		return save([]string{siblingScope}, []string{siblingScope})
	}
	locked, release := make(chan struct{}), make(chan struct{})
	replaced := make(chan error, 1)
	go func() {
		replaced <- store.WithLock(ref, func() error {
			close(locked)
			<-release
			return save([]string{siblingScope}, []string{siblingScope})
		})
	}()
	<-locked
	acquired := make(chan error, 1)
	go func() {
		_, err := broker.Acquire(auth.Request{Audience: siblingAudience})
		acquired <- err
	}()
	time.Sleep(100 * time.Millisecond)
	close(release)

	if err := <-replaced; err != nil {
		t.Fatal(err)
	}
	if err := <-acquired; err != nil {
		t.Errorf("acquire: %v", err)
	}
}
