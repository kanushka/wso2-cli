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
	"reflect"
	"strings"
	"testing"
	"time"

	keyring "github.com/zalando/go-keyring"

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
	homeTenant       = organization
)

var acquiredAt = time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC)

func broker(t *testing.T) *auth.Broker {
	t.Helper()
	return &auth.Broker{
		Namespace:    "reference",
		Capabilities: modules.Capabilities{AuthAudiences: []string{audience}, AuthScopes: []string{readScope}},
		Selection: contexts.Selection{
			Context: contexts.Context{
				Name:         "reference-local",
				Identity:     "reference-local",
				Organization: organization,
			},
			Identity: contexts.Identity{
				Name: "reference-local",
				Type: "onprem",
				Auth: contexts.IdentityAuth{
					Kind:               contexts.MethodDevelopmentCredential,
					CredentialVariable: credentialVar,
				},
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
	refusal := denied(t, broker(t), auth.Request{Audience: "another-audience", Scopes: []string{readScope}})

	if refusal.Problem.Code != "auth.audience_not_declared" {
		t.Errorf("code = %q, want auth.audience_not_declared", refusal.Problem.Code)
	}
}

func TestAnExcessiveScopeIsDenied(t *testing.T) {
	// The module receipt is the ceiling: a module cannot ask at runtime for
	// more than its installation declared.
	refusal := denied(t, broker(t), auth.Request{
		Audience: audience,
		Scopes:   []string{readScope, "reference:status:write"},
	})

	if refusal.Problem.Code != "auth.scope_not_declared" {
		t.Errorf("code = %q, want auth.scope_not_declared", refusal.Problem.Code)
	}
}

func TestARequestWithoutAnAudienceIsDenied(t *testing.T) {
	refusal := denied(t, broker(t), auth.Request{Scopes: []string{readScope}})

	if refusal.Problem.Code != "auth.audience_not_declared" {
		t.Errorf("code = %q, want auth.audience_not_declared", refusal.Problem.Code)
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

			refusal := denied(t, broker, declaredRequest())

			if refusal.Problem.Code != "auth.credential_unavailable" {
				t.Errorf("code = %q, want auth.credential_unavailable", refusal.Problem.Code)
			}
			// The user is told what to set. The module is not: where a
			// credential comes from is the shell's business.
			if !strings.Contains(refusal.Reported().Recovery, credentialVar) {
				t.Errorf("the reported guidance %q does not name the credential source",
					refusal.Reported().Recovery)
			}
			if strings.Contains(refusal.Problem.Recovery, credentialVar) {
				t.Errorf("the denial sent to the module names the credential source: %q",
					refusal.Problem.Recovery)
			}
			if strings.Contains(refusal.Problem.Message, credentialVar) {
				t.Errorf("the denial sent to the module names the credential source: %q",
					refusal.Problem.Message)
			}
		})
	}
}

func TestAModuleOutsideTheProofNamespaceIsNeverBrokeredAccess(t *testing.T) {
	// The issuer behind this broker is a development fixture. A product
	// namespace reaching it is refused rather than handed fixture access.
	broker := broker(t)
	broker.Namespace = "api"

	refusal := denied(t, broker, declaredRequest())

	if refusal.Problem.Code != "auth.namespace_not_brokered" {
		t.Errorf("code = %q, want auth.namespace_not_brokered", refusal.Problem.Code)
	}
}

func TestAnInvocationWithoutAContextIsDenied(t *testing.T) {
	broker := broker(t)
	broker.Selection = contexts.Selection{}

	refusal := denied(t, broker, declaredRequest())

	if refusal.Problem.Code != "auth.context_not_selected" {
		t.Errorf("code = %q, want auth.context_not_selected", refusal.Problem.Code)
	}
}

func TestAContextWithoutAnOrganizationIsDenied(t *testing.T) {
	// Access is bound to an organization, so a context that names none has
	// nothing to bind a token to.
	broker := broker(t)
	broker.Selection.Context.Organization = ""

	refusal := denied(t, broker, declaredRequest())

	if refusal.Problem.Code != "auth.organization_not_selected" {
		t.Errorf("code = %q, want auth.organization_not_selected", refusal.Problem.Code)
	}
}

func TestAnAuthenticationMethodThisShellDoesNotImplementIsDenied(t *testing.T) {
	broker := broker(t)
	broker.Selection.Identity.Auth.Kind = "browser-pkce"

	refusal := denied(t, broker, declaredRequest())

	if refusal.Problem.Code != "auth.method_unsupported" {
		t.Errorf("code = %q, want auth.method_unsupported", refusal.Problem.Code)
	}
}

func TestAModuleCannotRefreshItsAccess(t *testing.T) {
	// The proof grants access once per invocation. A module whose token
	// expires cannot renew it; the next invocation applies policy again.
	broker := broker(t)
	if _, err := broker.Acquire(declaredRequest()); err != nil {
		t.Fatalf("the first Acquire returned %v", err)
	}

	refusal := denied(t, broker, declaredRequest())

	if refusal.Problem.Code != "auth.already_granted" {
		t.Errorf("code = %q, want auth.already_granted", refusal.Problem.Code)
	}
}

func TestNoDenialRevealsTheSourceCredential(t *testing.T) {
	broker := broker(t)
	broker.Selection.Identity.Auth.Kind = "browser-pkce"

	refusal := denied(t, broker, auth.Request{Audience: "another-audience", Scopes: []string{"another:scope"}})

	rendered := refusal.Problem.Message + " " + refusal.Problem.Recovery +
		" " + refusal.Reported().Message + " " + refusal.Reported().Recovery
	if strings.Contains(rendered, sourceCredential) {
		t.Fatalf("a denial revealed the source credential: %s", rendered)
	}
}

// productionBroker builds a broker over a schema version 2 identity of the
// given kind, configured so every production policy check passes. Each test
// breaks exactly one of them, so a refusal names the check it broke.
func productionBroker(t *testing.T, kind string) *auth.Broker {
	t.Helper()
	return &auth.Broker{
		Namespace:    "reference",
		Capabilities: modules.Capabilities{AuthAudiences: []string{audience}, AuthScopes: []string{readScope}},
		Selection: contexts.Selection{
			Context: contexts.Context{
				Name:         "reference-cloud",
				Identity:     "reference-cloud",
				Organization: homeTenant,
			},
			Identity: contexts.Identity{
				Name: "reference-cloud",
				Type: "cloud",
				Auth: contexts.IdentityAuth{
					Kind:          kind,
					Issuer:        "https://issuer.example.test",
					ClientID:      "wso2cli",
					Tenant:        homeTenant,
					CredentialRef: "reference-cloud",
				},
				Products: map[string]contexts.Product{
					"reference": {
						Endpoint: "https://reference.example.test",
						Audience: audience,
						Scopes:   []string{readScope},
					},
				},
			},
		},
		InvocationID: invocationID,
		// A production identity derives access under a session lock, and a
		// broker with no state root would take that lock relative to whatever
		// directory the test happens to run in — inside the source tree.
		StateRoot: t.TempDir(),
		Now:       func() time.Time { return acquiredAt },
	}
}

// withProduct replaces the reference product registration, which a table
// literal cannot assign through a map member.
func withProduct(broker *auth.Broker, product contexts.Product) {
	broker.Selection.Identity.Products["reference"] = product
}

func TestTheIdentityKindDecidesWhichPolicyTheBrokerApplies(t *testing.T) {
	// One switch answers "what kind of identity is this?", and every refusal
	// below is reached through it. The order matters as much as the codes: an
	// identity that configures no product is refused for the product it does
	// not configure, never for the organization it happens to name.
	for name, testcase := range map[string]struct {
		kind   string
		mutate func(*auth.Broker)
		code   string
	}{
		"no identity is no selection": {
			kind: "",
			code: "auth.context_not_selected",
		},
		// A device identity reaches the same session source a browser one does,
		// so it is refused for the same product reasons and never for its kind.
		"a device identity that configures no product": {
			kind:   contexts.KindOAuthDevice,
			mutate: func(b *auth.Broker) { b.Selection.Identity.Products = nil },
			code:   "auth.product_not_configured",
		},
		"a personal access token is legal but unimplemented": {
			kind: contexts.KindPAT,
			code: "auth.kind_not_implemented",
		},
		"an unreadable kind is unsupported": {
			kind: "browser-pkce",
			code: "auth.method_unsupported",
		},
		"a browser identity that configures no product": {
			kind:   contexts.KindOAuthBrowser,
			mutate: func(b *auth.Broker) { b.Selection.Identity.Products = nil },
			code:   "auth.product_not_configured",
		},
		// A registration naming an audience this module does not name is not
		// refused here. The two vocabularies are not comparable, and the
		// binding is proved against the issued token instead; see
		// TestAccessIsGrantedWhenTheDeploymentBindsTheRegisteredAudience.
		"a browser identity that registers no audience": {
			kind: contexts.KindOAuthBrowser,
			mutate: func(b *auth.Broker) {
				withProduct(b, contexts.Product{
					Endpoint: "https://reference.example.test",
					Scopes:   []string{readScope},
				})
			},
			code: "auth.product_not_configured",
		},
		"a browser identity whose product does not carry the scope": {
			kind: contexts.KindOAuthBrowser,
			mutate: func(b *auth.Broker) {
				withProduct(b, contexts.Product{
					Endpoint: "https://reference.example.test",
					Audience: audience,
					Scopes:   []string{"reference:status:write"},
				})
			},
			code: "auth.product_not_configured",
		},
		"a browser identity asked to act outside its home tenant": {
			kind:   contexts.KindOAuthBrowser,
			mutate: func(b *auth.Broker) { b.Selection.Context.Organization = "another-org" },
			code:   "auth.organization_switch_unsupported",
		},
		"a client-credentials identity that configures no product": {
			kind:   contexts.KindClientCredentials,
			mutate: func(b *auth.Broker) { b.Selection.Identity.Products = nil },
			code:   "auth.product_not_configured",
		},
		"a client-credentials identity asked to act outside its home tenant": {
			kind:   contexts.KindClientCredentials,
			mutate: func(b *auth.Broker) { b.Selection.Context.Organization = "another-org" },
			code:   "auth.organization_switch_unsupported",
		},
	} {
		t.Run(name, func(t *testing.T) {
			broker := productionBroker(t, testcase.kind)
			if testcase.mutate != nil {
				testcase.mutate(broker)
			}

			refusal := denied(t, broker, declaredRequest())

			if refusal.Problem.Code != testcase.code {
				t.Errorf("code = %q, want %q", refusal.Problem.Code, testcase.code)
			}
		})
	}
}

func TestAFullyConfiguredProductionIdentityIsAdmittedByPolicy(t *testing.T) {
	// Policy admits this request, so only the token source can refuse it now.
	// Separating the two is what the source seam is for, and this pins the
	// hand-off exactly: with nothing stored to derive from, what comes back is
	// the source asking for a login, not policy turning the identity away.
	keyring.MockInit()
	broker := productionBroker(t, contexts.KindOAuthBrowser)

	refusal := denied(t, broker, declaredRequest())

	if refusal.Problem.Code != "auth.login_required" {
		t.Errorf("code = %q, want auth.login_required", refusal.Problem.Code)
	}
}

func TestAnUnselectedContextIsRefusedBeforeTheProofNamespaceGuard(t *testing.T) {
	// Ordering. The proof-namespace guard belongs to the development source, so
	// reaching it means an identity was resolved first. A namespace outside the
	// proof with no identity at all is told what is actually wrong — nothing is
	// selected — rather than that its namespace is not brokered.
	broker := broker(t)
	broker.Namespace = "api"
	broker.Selection = contexts.Selection{}

	refusal := denied(t, broker, declaredRequest())

	if refusal.Problem.Code != "auth.context_not_selected" {
		t.Errorf("code = %q, want auth.context_not_selected", refusal.Problem.Code)
	}
}

func TestAProductNamespaceTheIdentityDoesNotConfigureIsRefused(t *testing.T) {
	// A production identity reaching the broker for a namespace it does not
	// register is told so, rather than being handed another product's audience.
	broker := productionBroker(t, contexts.KindOAuthBrowser)
	broker.Namespace = "api"

	refusal := denied(t, broker, declaredRequest())

	if refusal.Problem.Code != "auth.product_not_configured" {
		t.Errorf("code = %q, want auth.product_not_configured", refusal.Problem.Code)
	}
	if !strings.Contains(refusal.Problem.Message, "api") {
		t.Errorf("the refusal %q does not name the product namespace", refusal.Problem.Message)
	}
	// The command that records a product registration exists, so the recovery
	// names it — with this identity and namespace filled in — rather than
	// sending the user to edit a file by hand.
	if !strings.Contains(refusal.Problem.Recovery, "wso2 identity add-product reference-cloud api") {
		t.Errorf("the recovery %q does not name wso2 identity add-product for this identity and namespace",
			refusal.Problem.Recovery)
	}
}

// denied runs one request that must be refused and returns the shell's denial.
func denied(t *testing.T, broker *auth.Broker, request auth.Request) auth.Denial {
	t.Helper()
	grant, err := broker.Acquire(request)
	if err == nil {
		t.Fatalf("Acquire granted %+v, want a denial", grant)
	}
	var refusal auth.Denial
	if !errors.As(err, &refusal) {
		t.Fatalf("Acquire returned %v, want a typed denial", err)
	}
	for _, stated := range []problem.Problem{refusal.Problem, refusal.Reported()} {
		if stated.Category != problem.CategoryAuthPolicy {
			t.Errorf("category = %q, want %q", stated.Category, problem.CategoryAuthPolicy)
		}
		if stated.Message == "" || stated.Recovery == "" {
			t.Errorf("the denial %q states %q and offers %q; both are required",
				stated.Code, stated.Message, stated.Recovery)
		}
	}
	return refusal
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
