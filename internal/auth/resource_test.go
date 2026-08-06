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
	"testing"

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
