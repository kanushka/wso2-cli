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
	"strings"
	"testing"

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
}
