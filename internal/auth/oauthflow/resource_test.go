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

package oauthflow_test

import (
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
)

// theResource is the protected resource a resource-binding deployment mints
// access for. It is an absolute URI because the deployments that require one
// refuse anything else.
const theResource = "https://deployment.example.test/reference-status"

// A deployment that decides the audience at authorization time gives the
// session one protected resource, and the session is only useful for it. The
// login has to say which, because nothing later in the exchange can.
func TestLoginBindsTheSessionToTheResourceItWasGiven(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{
		RequireResource:      true,
		AllowAnyLoopbackPort: true,
	})
	printed := &recorder{}
	login := browserLogin(issuer, printed, func(authURL string) error {
		go visit(issuer, authURL)
		return nil
	})
	login.Resource = theResource

	result, err := login.Run(testContext(t, 30*time.Second))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	_, _, audience := issuer.Introspect(t, result.Token.AccessToken)
	if !slices.Contains(audience, theResource) {
		t.Fatalf("the session was not bound to the resource it named: audience %v", audience)
	}
	if !strings.Contains(printed.String(), url.QueryEscape(theResource)) {
		t.Fatalf("the authorization URL carried no resource indicator:\n%s", printed.String())
	}
}

// The refusal belongs to the deployment, and the login reports it as a login
// that did not complete rather than inventing a reason of its own.
func TestALoginWithoutAResourceIsRefusedByADeploymentThatRequiresOne(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{
		RequireResource:      true,
		AllowAnyLoopbackPort: true,
	})
	printed := &recorder{}
	login := browserLogin(issuer, printed, func(authURL string) error {
		go visit(issuer, authURL)
		return nil
	})

	_, err := login.Run(testContext(t, 30*time.Second))
	if err == nil {
		t.Fatal("a login carrying no resource indicator completed against a deployment that requires one")
	}
	_ = requireProblem(t, err, "auth.credential_unavailable")
}

// A deployment that binds no audience at authorization time must be unaffected,
// or naming a resource would change what every existing login asks for.
func TestALoginCarriesNoResourceIndicatorUnlessItWasGivenOne(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{
		Audience:             "reference-status",
		AllowAnyLoopbackPort: true,
	})
	printed := &recorder{}
	login := browserLogin(issuer, printed, func(authURL string) error {
		go visit(issuer, authURL)
		return nil
	})

	if _, err := login.Run(testContext(t, 30*time.Second)); err != nil {
		t.Fatalf("login: %v", err)
	}
	if strings.Contains(printed.String(), "resource=") {
		t.Fatalf("a login that was given no resource sent one anyway:\n%s", printed.String())
	}
}
