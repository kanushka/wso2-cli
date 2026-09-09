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

	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/zalando/go-keyring"
)

// exchangedAudience is what the product's own record registers, and so what
// the exchange asks for as its resource and what the issued token must be
// bound to.
const exchangedAudience = "https://apip.example.test"

// exchangeGrantName is the grant an administrator has to register on the
// shell's own OAuth application, and so the string a recovery must carry.
const exchangeGrantName = "urn:ietf:params:oauth:grant-type:token-exchange"

// exchangeDeployment is one login issuer that runs RFC 8693: the session is
// the login product's, and the product under test holds none of its own.
type exchangeDeployment struct {
	issuer    *fakeissuer.Issuer
	stateRoot string
}

// seedExchangeDeployment stores the login session an exchange derives from.
//
// There is deliberately only one issuer and one stored session: an exchanged
// product's whole claim is that it needs no authorization and no session of
// its own, so a fixture that seeded a second would be proving something else.
func seedExchangeDeployment(t *testing.T, options fakeissuer.Options) exchangeDeployment {
	t.Helper()
	keyring.MockInit()
	if options.RefreshScopeMode == "" {
		options.RefreshScopeMode = "honor"
	}
	options.ExchangeGrant = true
	issuer := fakeissuer.New(t, options)
	root := t.TempDir()
	seeded := issuer.SeedSession([]string{"openid", "system"})
	if err := (session.Store{StateRoot: root}).Save(sessionRef,
		session.Session{Issuer: issuer.URL, RefreshToken: seeded}); err != nil {
		t.Fatalf("seeding the login session: %v", err)
	}
	return exchangeDeployment{issuer: issuer, stateRoot: root}
}

func (d exchangeDeployment) broker(t *testing.T) *auth.Broker {
	t.Helper()
	broker := productionBroker(t, contexts.KindOAuthBrowser)
	broker.Selection.Identity.Auth.Issuer = d.issuer.URL
	broker.Selection.Identity.Auth.ClientID = loginClient
	broker.Selection.Identity.Auth.CredentialRef = sessionRef
	withProduct(broker, contexts.Product{
		Endpoint: "https://apip.example.test",
		Audience: exchangedAudience,
		Scopes:   []string{readScope},
		Grant:    &contexts.Grant{Kind: contexts.GrantExchange},
	})
	broker.StateRoot = d.stateRoot
	broker.HTTPClient = d.issuer.HTTPClient()
	broker.Now = nil
	return broker
}

func TestAnExchangedProductIsAnsweredWithoutASessionOfItsOwn(t *testing.T) {
	deployment := seedExchangeDeployment(t, fakeissuer.Options{})
	grant, err := deployment.broker(t).Acquire(declaredRequest())
	if err != nil {
		t.Fatalf("Acquire returned %v", err)
	}
	if grant.Token == "" {
		t.Fatal("the exchange granted no token")
	}
	if grant.ExpiresAt.IsZero() {
		t.Fatal("the granted token states no expiry")
	}
	// The whole point of the strategy: nothing was stored for the product, so
	// there is no second credential to rotate, revoke or lose.
	if _, err := (session.Store{StateRoot: deployment.stateRoot}).Load(
		contexts.ProductSessionRef(sessionRef, "reference")); err == nil {
		t.Fatal("the exchange stored a session for the product, and an exchanged product keeps none")
	}
}

func TestAnExchangedTokenMustBeBoundToTheRegisteredAudience(t *testing.T) {
	// A deployment that answers an exchange with a token bound elsewhere has
	// not refused — it returned 200 — so nothing but this check stands
	// between the module and a token its audience will reject. Measured
	// against Thunder: passing `audience` instead of `resource` produces
	// exactly this shape.
	deployment := seedExchangeDeployment(t, fakeissuer.Options{ExchangeAudience: "https://elsewhere.example.test"})
	_, err := deployment.broker(t).Acquire(declaredRequest())
	var denial auth.Denial
	if !errors.As(err, &denial) {
		t.Fatalf("an exchange bound to another audience was not refused: %v", err)
	}
	if denial.Problem.Code != "auth.exchange_unusable" {
		t.Fatalf("refusal code = %q, want auth.exchange_unusable", denial.Problem.Code)
	}
	if !strings.Contains(denial.Problem.Message, exchangedAudience) {
		t.Fatalf("the refusal does not name the audience the identity registers: %q", denial.Problem.Message)
	}
}

func TestAnIssuerThatDoesNotRegisterTheExchangeGrantIsNamedAsSuch(t *testing.T) {
	// Measured against Thunder: a client without the grant enabled answers
	// unauthorized_client, and the recovery is a registration change nobody
	// could guess from the protocol error alone.
	deployment := seedExchangeDeployment(t, fakeissuer.Options{})
	deployment.issuer.RefuseExchange()
	_, err := deployment.broker(t).Acquire(declaredRequest())
	var denial auth.Denial
	if !errors.As(err, &denial) {
		t.Fatalf("a refused exchange was not reported as a denial: %v", err)
	}
	if denial.Problem.Code != "auth.exchange_unavailable" {
		t.Fatalf("refusal code = %q, want auth.exchange_unavailable", denial.Problem.Code)
	}
	// The recovery has to name the grant to add, because the protocol error
	// on its own reads as a credential problem and is not one.
	if !strings.Contains(denial.Problem.Recovery, exchangeGrantName) {
		t.Fatalf("the recovery does not name the grant to register: %q", denial.Problem.Recovery)
	}
}
