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
	"github.com/wso2/wso2-cli/internal/contexts"
)

// A deployment that decides the audience per request will not issue at all
// without being told which resource the access is for. The module asked for an
// audience; the shell has to carry it as the indicator the deployment reads.
func TestAnInlineIdentityBindsAccessToTheResourceItsProductNames(t *testing.T) {
	deployment := deployInline(t, fakeissuer.Options{RequireResource: true})
	broker := deployment.broker(t)
	broker.Selection.Identity.Auth.Provider = contexts.ProviderThunder

	grant, err := broker.Acquire(declaredRequest())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	_, scopes, audiences := deployment.issuer.Introspect(t, grant.Token)
	if len(audiences) != 1 || audiences[0] != audience {
		t.Fatalf("access was not bound to the resource the module named: audience %v", audiences)
	}
	if len(scopes) != 1 || scopes[0] != readScope {
		t.Fatalf("access carried %v, want exactly [%s]", scopes, readScope)
	}
}

// The whole reason the refresh grant was left alone is that a deployment which
// binds by resource carries the binding forward from the authorization that
// established the session. If that were not so, every module's access would
// come back bound to nothing and the broker would refuse it.
//
// Measured on ThunderID v1.0.0-beta: a refresh carrying no indicator returned a
// token still bound to the resource the login named. This proves the shell
// against that behaviour rather than against a transcript of it.
func TestABrowserSessionKeepsItsResourceBindingAcrossARefresh(t *testing.T) {
	// The registration's own audience is deliberately not the resource the
	// session was established for. If the refresh lost the binding and fell back
	// to the registration, the token would carry this instead — which is the
	// only way this test can tell a retained binding from a coincidence.
	deployment := seedBrowserSession(t, fakeissuer.Options{
		RequireResource:     true,
		Audience:            "registration-audience-not-the-resource",
		RotateRefreshTokens: true,
	})
	broker := deployment.broker(t)
	broker.Selection.Identity.Auth.Provider = contexts.ProviderThunder

	grant, err := broker.Acquire(auth.Request{Audience: audience, Scopes: []string{readScope}})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	_, scopes, audiences := deployment.issuer.Introspect(t, grant.Token)
	if len(audiences) != 1 || audiences[0] != audience {
		t.Fatalf("the refresh lost the session's resource binding: audience %v", audiences)
	}
	if len(scopes) != 1 || scopes[0] != readScope {
		t.Fatalf("the refresh issued %v, want exactly [%s]", scopes, readScope)
	}
}

// invalid_target says the deployment would not issue for the target it was
// given. That is two different situations, and they send the user to two
// different places: an identity that named no provider asked in a shape this
// deployment does not accept, and one that did name a resource named a resource
// the deployment does not recognise. Telling the second to go and name a
// provider it has already named would be advice it cannot act on.
func TestAResourceTheDeploymentRejectsIsNotReportedAsAMissingOne(t *testing.T) {
	deployment := deployInline(t, fakeissuer.Options{
		RequireResource:    true,
		RegisteredResource: "https://deployment.example.test/some-other-api",
	})
	broker := deployment.broker(t)
	broker.Selection.Identity.Auth.Provider = contexts.ProviderThunder

	refusal := denied(t, broker, declaredRequest())

	if refusal.Problem.Code != "auth.narrowing_unavailable" {
		t.Fatalf("code = %q, want auth.narrowing_unavailable", refusal.Problem.Code)
	}
	// The resource the module asked for is not a secret, and naming it is the
	// difference between a refusal and a registration someone can go and fix.
	if !strings.Contains(refusal.Problem.Message, audience) {
		t.Fatalf("the refusal does not name the resource it sent: %q", refusal.Problem.Message)
	}
	if strings.Contains(refusal.Reported().Recovery, "identity provider") {
		t.Fatalf("the refusal told the user to name a provider the identity already names: %q",
			refusal.Reported().Recovery)
	}
}

// The indicator is sent because the identity says this deployment reads one.
// An identity that says nothing must keep asking exactly as it did before, or
// every deployment already working would start receiving a parameter it never
// agreed to interpret.
func TestAnInlineIdentityWithoutAResourceDerivationSendsNoIndicator(t *testing.T) {
	deployment := deployInline(t, fakeissuer.Options{RequireResource: true})
	broker := deployment.broker(t)

	refusal := denied(t, broker, declaredRequest())

	if refusal.Problem.Code != "auth.narrowing_unavailable" {
		t.Errorf("code = %q, want auth.narrowing_unavailable", refusal.Problem.Code)
	}
}
