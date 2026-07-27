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
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/auth/devtoken"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/sdk/problem"
)

const (
	sourceCredential = "canary-source-credential-2f8c"
	credentialVar    = "WSO2_REFERENCE_DEV_CREDENTIAL"
	audience         = "reference-status"
	readScope        = "reference:status:read"
	organization     = "reference-org"
	invocationID     = "invocation-7f2a"
)

var acquiredAt = time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC)

func broker(t *testing.T) *auth.Broker {
	t.Helper()
	return &auth.Broker{
		Namespace:    "reference",
		Capabilities: modules.Capabilities{AuthAudiences: []string{audience}, AuthScopes: []string{readScope}},
		Context: contexts.Context{
			Name:           "reference-local",
			OrganizationID: organization,
			Endpoint:       "http://127.0.0.1:8080",
			Auth: contexts.Auth{
				Method:             contexts.MethodDevelopmentCredential,
				CredentialVariable: credentialVar,
			},
		},
		InvocationID: invocationID,
		Credentials:  func(name string) (string, bool) { return sourceCredential, name == credentialVar },
		Now:          func() time.Time { return acquiredAt },
	}
}

func declaredRequest() auth.Request {
	return auth.Request{Audience: audience, Scopes: []string{readScope}}
}

func TestADeclaredRequestIsGrantedATokenBoundToTheInvocation(t *testing.T) {
	grant, err := broker(t).Acquire(declaredRequest())
	if err != nil {
		t.Fatalf("Acquire returned %v", err)
	}

	claims, err := devtoken.Verify(sourceCredential, grant.Token, acquiredAt)
	if err != nil {
		t.Fatalf("the granted token does not verify: %v", err)
	}
	if claims.Audience != audience {
		t.Errorf("audience = %q, want %q", claims.Audience, audience)
	}
	if !reflect.DeepEqual(claims.Scopes, []string{readScope}) {
		t.Errorf("scopes = %v, want [%s]", claims.Scopes, readScope)
	}
	if claims.Organization != organization {
		t.Errorf("organization = %q, want %q", claims.Organization, organization)
	}
	if claims.Invocation != invocationID {
		t.Errorf("invocation = %q, want %q", claims.Invocation, invocationID)
	}
	if !grant.ExpiresAt.Equal(claims.ExpiresAt) {
		t.Errorf("the grant expires at %s and the token at %s", grant.ExpiresAt, claims.ExpiresAt)
	}
	if lifetime := grant.ExpiresAt.Sub(acquiredAt); lifetime <= 0 || lifetime > 5*time.Minute {
		t.Errorf("the grant lasts %s, want a positive near-term lifetime", lifetime)
	}
}

func TestAGrantCarriesOnlyTheToken(t *testing.T) {
	// A module receives access material and nothing it could use to obtain
	// more: not the credential the shell holds, and not a reference to it.
	grant, err := broker(t).Acquire(declaredRequest())
	if err != nil {
		t.Fatalf("Acquire returned %v", err)
	}

	if strings.Contains(grant.Token, sourceCredential) {
		t.Error("the granted token carries the source credential")
	}
	if got := exportedMembers(t, grant); !reflect.DeepEqual(got, []string{"Token", "ExpiresAt"}) {
		t.Errorf("a grant carries %v; it may carry only the token and its expiry", got)
	}
}

func TestAnUndeclaredAudienceIsDenied(t *testing.T) {
	denial := denied(t, broker(t), auth.Request{Audience: "another-audience", Scopes: []string{readScope}})

	if denial.Code != "auth.audience_not_declared" {
		t.Errorf("code = %q, want auth.audience_not_declared", denial.Code)
	}
}

func TestAnExcessiveScopeIsDenied(t *testing.T) {
	// The module receipt is the ceiling: a module cannot ask at runtime for
	// more than its installation declared.
	denial := denied(t, broker(t), auth.Request{
		Audience: audience,
		Scopes:   []string{readScope, "reference:status:write"},
	})

	if denial.Code != "auth.scope_not_declared" {
		t.Errorf("code = %q, want auth.scope_not_declared", denial.Code)
	}
}

func TestARequestWithoutAnAudienceIsDenied(t *testing.T) {
	denial := denied(t, broker(t), auth.Request{Scopes: []string{readScope}})

	if denial.Code != "auth.audience_not_declared" {
		t.Errorf("code = %q, want auth.audience_not_declared", denial.Code)
	}
}

func TestAMissingCredentialIsDeniedWithSafeGuidance(t *testing.T) {
	for name, credentials := range map[string]func(string) (string, bool){
		"unset": func(string) (string, bool) { return "", false },
		"empty": func(string) (string, bool) { return "", true },
		"blank": func(string) (string, bool) { return "   ", true },
	} {
		t.Run(name, func(t *testing.T) {
			broker := broker(t)
			broker.Credentials = credentials

			denial := denied(t, broker, declaredRequest())

			if denial.Code != "auth.credential_unavailable" {
				t.Errorf("code = %q, want auth.credential_unavailable", denial.Code)
			}
			if !strings.Contains(denial.Recovery, credentialVar) {
				t.Errorf("the recovery guidance %q does not name the credential source", denial.Recovery)
			}
		})
	}
}

func TestAnInvocationWithoutAContextIsDenied(t *testing.T) {
	broker := broker(t)
	broker.Context = contexts.Context{Name: contexts.DefaultName}

	denial := denied(t, broker, declaredRequest())

	if denial.Code != "auth.context_not_selected" {
		t.Errorf("code = %q, want auth.context_not_selected", denial.Code)
	}
}

func TestAContextWithoutAnOrganizationIsDenied(t *testing.T) {
	// Access is bound to an organization, so a context that names none has
	// nothing to bind a token to.
	broker := broker(t)
	broker.Context.OrganizationID = ""

	denial := denied(t, broker, declaredRequest())

	if denial.Code != "auth.organization_not_selected" {
		t.Errorf("code = %q, want auth.organization_not_selected", denial.Code)
	}
}

func TestAnAuthenticationMethodThisShellDoesNotImplementIsDenied(t *testing.T) {
	broker := broker(t)
	broker.Context.Auth.Method = "browser-pkce"

	denial := denied(t, broker, declaredRequest())

	if denial.Code != "auth.method_unsupported" {
		t.Errorf("code = %q, want auth.method_unsupported", denial.Code)
	}
}

func TestAModuleCannotRefreshItsAccess(t *testing.T) {
	// The proof grants access once per invocation. A module whose token
	// expires cannot renew it; the next invocation applies policy again.
	broker := broker(t)
	if _, err := broker.Acquire(declaredRequest()); err != nil {
		t.Fatalf("the first Acquire returned %v", err)
	}

	denial := denied(t, broker, declaredRequest())

	if denial.Code != "auth.already_granted" {
		t.Errorf("code = %q, want auth.already_granted", denial.Code)
	}
}

func TestNoDenialRevealsTheSourceCredential(t *testing.T) {
	broker := broker(t)
	broker.Context.Auth.Method = "browser-pkce"

	denial := denied(t, broker, auth.Request{Audience: "another-audience", Scopes: []string{"another:scope"}})

	rendered := denial.Message + " " + denial.Recovery
	if strings.Contains(rendered, sourceCredential) {
		t.Fatalf("a denial revealed the source credential: %s", rendered)
	}
}

// denied runs one request that must be refused and returns the shell's problem.
func denied(t *testing.T, broker *auth.Broker, request auth.Request) problem.Problem {
	t.Helper()
	grant, err := broker.Acquire(request)
	if err == nil {
		t.Fatalf("Acquire granted %+v, want a denial", grant)
	}
	typed, ok := err.(problem.Problem)
	if !ok {
		t.Fatalf("Acquire returned %v, want a typed problem", err)
	}
	if typed.Category != problem.CategoryAuthPolicy {
		t.Errorf("category = %q, want %q", typed.Category, problem.CategoryAuthPolicy)
	}
	if typed.Message == "" || typed.Recovery == "" {
		t.Errorf("the denial %q states %q and offers %q; both are required",
			typed.Code, typed.Message, typed.Recovery)
	}
	return typed
}

// exportedMembers reports a value's exported field names.
func exportedMembers(t *testing.T, value any) []string {
	t.Helper()
	structure := reflect.TypeOf(value)
	members := make([]string, 0, structure.NumField())
	for index := range structure.NumField() {
		members = append(members, structure.Field(index).Name)
	}
	return members
}
