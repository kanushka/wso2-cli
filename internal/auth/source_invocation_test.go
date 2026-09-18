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
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/modules"
)

const (
	// invocationAudience is the logical audience a module declares for
	// calling an API the way its consumer would.
	invocationAudience = "reference-invocation"
	// invokedResource is the identifier one API declares as its audience. No
	// context records it.
	invokedResource = "https://gateway.example.test/hello"
)

// invocationBroker is an exchanged product whose module declares invocation.
func (d exchangeDeployment) invocationBroker(t *testing.T) *auth.Broker {
	t.Helper()
	broker := d.broker(t)
	broker.Capabilities.AuthAudiences = append(broker.Capabilities.AuthAudiences, invocationAudience)
	broker.Capabilities.Product = &modules.ProductDescriptor{
		Audience:   modules.AudienceResource,
		Grant:      contexts.GrantExchange,
		Invocation: &modules.InvocationDescriptor{Audience: modules.AudienceResource},
	}
	return broker
}

func invocationRequest(resource string) auth.Request {
	return auth.Request{Audience: invocationAudience, Record: contexts.APIRecord, Resource: resource}
}

// tokenAudiences reads the aud claim of an unverified fixture token.
func tokenAudiences(t *testing.T, token string) []string {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("the granted token is not a JWT: %q", token)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decoding the token payload: %v", err)
	}
	var claims struct {
		Audience any `json:"aud"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("reading the token claims: %v", err)
	}
	switch aud := claims.Audience.(type) {
	case string:
		return []string{aud}
	case []any:
		var audiences []string
		for _, entry := range aud {
			audiences = append(audiences, entry.(string))
		}
		return audiences
	}
	return nil
}

func denialCode(t *testing.T, err error) auth.Denial {
	t.Helper()
	var denial auth.Denial
	if !errors.As(err, &denial) {
		t.Fatalf("the request was not refused with a denial: %v", err)
	}
	return denial
}

func TestAnAPIResourceIsAnsweredWithATokenBoundToIt(t *testing.T) {
	deployment := seedExchangeDeployment(t, fakeissuer.Options{})
	grant, err := deployment.invocationBroker(t).Acquire(invocationRequest(invokedResource))
	if err != nil {
		t.Fatalf("Acquire returned %v", err)
	}
	if !slices.Contains(tokenAudiences(t, grant.Token), invokedResource) {
		t.Fatalf("the granted token is bound to %v, want %q", tokenAudiences(t, grant.Token), invokedResource)
	}
}

func TestAnAPITokenBoundElsewhereIsRefused(t *testing.T) {
	deployment := seedExchangeDeployment(t, fakeissuer.Options{ExchangeAudience: "https://elsewhere.example.test"})
	_, err := deployment.invocationBroker(t).Acquire(invocationRequest(invokedResource))
	if code := denialCode(t, err).Problem.Code; code != "auth.exchange_unusable" {
		t.Fatalf("refusal code = %q, want auth.exchange_unusable", code)
	}
}

func TestAModuleThatDeclaresNoInvocationIsRefusedTheAPIRecord(t *testing.T) {
	deployment := seedExchangeDeployment(t, fakeissuer.Options{})
	broker := deployment.invocationBroker(t)
	broker.Capabilities.Product.Invocation = nil
	_, err := broker.Acquire(invocationRequest(invokedResource))
	if code := denialCode(t, err).Problem.Code; code != "auth.product_not_configured" {
		t.Fatalf("refusal code = %q, want auth.product_not_configured", code)
	}
}

func TestTheAPIRecordNeverYieldsARecordedAudience(t *testing.T) {
	// The api record exists to call an API as its consumer. Asked for the
	// audience a record of the context already holds, it would be a second
	// way to a management token, past that record's own scope checks.
	deployment := seedExchangeDeployment(t, fakeissuer.Options{})
	_, err := deployment.invocationBroker(t).Acquire(invocationRequest(exchangedAudience))
	if code := denialCode(t, err).Problem.Code; code != "auth.invocation_refused" {
		t.Fatalf("refusal code = %q, want auth.invocation_refused", code)
	}
}

func TestAnAPIRecordRequestMustNameAnAbsoluteResource(t *testing.T) {
	deployment := seedExchangeDeployment(t, fakeissuer.Options{})
	for _, resource := range []string{"", "hello", "/hello"} {
		_, err := deployment.invocationBroker(t).Acquire(invocationRequest(resource))
		if code := denialCode(t, err).Problem.Code; code != "auth.invocation_refused" {
			t.Fatalf("resource %q: refusal code = %q, want auth.invocation_refused", resource, code)
		}
	}
}

func TestAResourceOutsideTheAPIRecordIsRefused(t *testing.T) {
	deployment := seedExchangeDeployment(t, fakeissuer.Options{})
	request := declaredRequest()
	request.Resource = invokedResource
	_, err := deployment.invocationBroker(t).Acquire(request)
	if code := denialCode(t, err).Problem.Code; code != "auth.invocation_refused" {
		t.Fatalf("refusal code = %q, want auth.invocation_refused", code)
	}
}

func TestAnAPIRecordIsRefusedForAProductThatIsNotExchanged(t *testing.T) {
	deployment := seedExchangeDeployment(t, fakeissuer.Options{})
	broker := deployment.invocationBroker(t)
	withProduct(broker, contexts.Product{
		Endpoint: "https://apip.example.test",
		Audience: exchangedAudience,
		Scopes:   []string{readScope},
	})
	_, err := broker.Acquire(invocationRequest(invokedResource))
	if code := denialCode(t, err).Problem.Code; code != "auth.invocation_unavailable" {
		t.Fatalf("refusal code = %q, want auth.invocation_unavailable", code)
	}
}

func TestAnUnregisteredAPIResourceNamesTheRegistrationToMake(t *testing.T) {
	deployment := seedExchangeDeployment(t, fakeissuer.Options{RegisteredResource: "https://other.example.test"})
	_, err := deployment.invocationBroker(t).Acquire(invocationRequest(invokedResource))
	denial := denialCode(t, err)
	if denial.Problem.Code != "auth.exchange_unavailable" {
		t.Fatalf("refusal code = %q, want auth.exchange_unavailable", denial.Problem.Code)
	}
	if !strings.Contains(denial.Problem.Recovery, invokedResource) {
		t.Fatalf("the recovery does not name the resource to register: %q", denial.Problem.Recovery)
	}
}

func TestOneCommandMayHoldItsOwnRecordAndAnAPIRecord(t *testing.T) {
	// Resolving an API needs the control plane, and calling it needs the
	// API's own token: two records, each granted once.
	deployment := seedExchangeDeployment(t, fakeissuer.Options{})
	broker := deployment.invocationBroker(t)
	if _, err := broker.Acquire(declaredRequest()); err != nil {
		t.Fatalf("the product's own record was refused: %v", err)
	}
	if _, err := broker.Acquire(invocationRequest(invokedResource)); err != nil {
		t.Fatalf("the api record was refused after the product's own: %v", err)
	}
	_, err := broker.Acquire(invocationRequest(invokedResource))
	if code := denialCode(t, err).Problem.Code; code != "auth.already_granted" {
		t.Fatalf("a second api grant: refusal code = %q, want auth.already_granted", code)
	}
}
