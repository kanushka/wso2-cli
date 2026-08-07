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

package contexts_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/contexts"
)

// A document written before this shell knew any deployment derived access
// differently must keep working, and must keep deriving the way it always has.
func TestAnIdentityDeclaringNothingDerivesByScopedRefresh(t *testing.T) {
	document, err := contexts.Decode([]byte(validV2()))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := document.Identities[0].Auth.Derivation(); got != contexts.DerivationScopedRefresh {
		t.Fatalf("an identity declaring no derivation derived by %q, want %q",
			got, contexts.DerivationScopedRefresh)
	}
}

// Naming the identity provider is what a person writing the document knows.
// The derivation it implies is what the shell knows, and it should not have to
// be written twice.
func TestNamingThunderImpliesResourceBoundDerivation(t *testing.T) {
	document, err := contexts.Decode([]byte(withProvider(contexts.ProviderThunder)))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := document.Identities[0].Auth.Derivation(); got != contexts.DerivationTokenResource {
		t.Fatalf("a Thunder identity derived by %q, want %q",
			got, contexts.DerivationTokenResource)
	}
}

// Thunder is pre-1.0, and a deployment whose resource server is not registered
// is a state the first several of them will be in. Saying so explicitly has to
// win, or the provider declaration becomes a straitjacket nobody can leave.
func TestAnExplicitDerivationOverridesWhatTheProviderImplies(t *testing.T) {
	document, err := contexts.Decode([]byte(withDerivation(
		contexts.ProviderThunder, contexts.DerivationScopedRefresh)))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := document.Identities[0].Auth.Derivation(); got != contexts.DerivationScopedRefresh {
		t.Fatalf("an explicit derivation was overridden: got %q, want %q",
			got, contexts.DerivationScopedRefresh)
	}
}

// Asgardeo and Identity Server take no audience at authorization time, so one
// session serves every product there. Naming either of them must not change how
// access is derived.
func TestNamingAsgardeoOrIdentityServerLeavesTheDerivationAlone(t *testing.T) {
	for _, provider := range []string{contexts.ProviderAsgardeo, contexts.ProviderIdentityServer} {
		t.Run(provider, func(t *testing.T) {
			document, err := contexts.Decode([]byte(withProvider(provider)))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got := document.Identities[0].Auth.Derivation(); got != contexts.DerivationScopedRefresh {
				t.Fatalf("provider %q derived by %q, want %q",
					provider, got, contexts.DerivationScopedRefresh)
			}
		})
	}
}

// An identity provider this shell has never heard of is a document it cannot
// act on, and guessing a derivation for it would be the shell inventing policy.
func TestAnUnknownProviderIsRefused(t *testing.T) {
	_, err := contexts.Decode([]byte(withProvider("acme-idp")))
	assertProblemCode(t, err, "contexts.document_malformed")
}

// A derivation this shell does not implement is refused for the same reason.
func TestAnUnknownDerivationIsRefused(t *testing.T) {
	_, err := contexts.Decode([]byte(withDerivation(contexts.ProviderThunder, "token-exchange")))
	assertProblemCode(t, err, "contexts.document_malformed")
}

// Thunder requires a resource indicator at authorization time and accepts only
// one, so one session reaches exactly one product. An identity that declares
// two is refused when the document is read, rather than at the end of a browser
// sign-in the user cannot act on.
func TestAThunderIdentityServingSeveralProductsIsRefused(t *testing.T) {
	_, err := contexts.Decode([]byte(withSecondProduct(withProvider(contexts.ProviderThunder))))
	assertProblemCode(t, err, "contexts.document_malformed")
}

// The same document is legal on a deployment that binds no audience at
// authorization time, so the refusal above must be about Thunder and not about
// serving two products.
func TestSeveralProductsStayLegalWithoutThunder(t *testing.T) {
	if _, err := contexts.Decode([]byte(withSecondProduct(validV2()))); err != nil {
		t.Fatalf("two products on a non-Thunder identity should validate: %v", err)
	}
}

// Deriving access bound to a resource means naming the resource, and a product
// with no audience names none.
func TestAThunderIdentityWhoseProductNamesNoAudienceIsRefused(t *testing.T) {
	document := withAudience(contexts.ProviderThunder, "")
	document = strings.Replace(document, `"audience": "",`, ``, 1)
	_, err := contexts.Decode([]byte(document))
	assertProblemCode(t, err, "contexts.document_malformed")
}

// A resource indicator is an absolute URI by RFC 8707 section 2, so a document
// that derives by resource and names a bare identifier describes a request the
// deployment will refuse. Catching it here is the difference between a document
// that cannot be loaded and a browser sign-in that ends in invalid_target.
func TestAResourceBoundAudienceMustBeAnAbsoluteURI(t *testing.T) {
	for name, audience := range map[string]string{
		"a bare identifier": "reference-status",
		"a path":            "/reference-status",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := contexts.Decode([]byte(withAudience(contexts.ProviderThunder, audience)))
			assertProblemCode(t, err, "contexts.document_malformed")
		})
	}
}

// A fragment is the one thing RFC 8707 rules out beyond absoluteness, and it is
// worth refusing for the same reason: the deployment will.
func TestAResourceBoundAudienceMayNotCarryAFragment(t *testing.T) {
	_, err := contexts.Decode([]byte(withAudience(contexts.ProviderThunder,
		"https://localhost:8490/reference-status#read")))
	assertProblemCode(t, err, "contexts.document_malformed")
}

// The rule is RFC 8707's, not a guess about which schemes a deployment serves.
// A URN is an absolute URI and is refused by nothing in that specification, so
// this shell does not refuse it either.
func TestAResourceBoundAudienceAcceptsAnyAbsoluteURI(t *testing.T) {
	// The last case is the limit of what this check can claim. "localhost:8490/x"
	// is an absolute URI whose scheme happens to be "localhost", almost
	// certainly a missing "https://" — but the specification accepts it, this
	// shell never dereferences the value, and refusing it would mean inventing a
	// rule about plausibility that nothing here is entitled to hold. It fails at
	// the deployment, which knows which resource servers it registered.
	for name, audience := range map[string]string{
		"an https URL":                   "https://localhost:8490/reference-status",
		"a urn":                          "urn:wso2:reference-status",
		"a scheme that is likely a typo": "localhost:8490/reference-status",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := contexts.Decode([]byte(withAudience(contexts.ProviderThunder, audience))); err != nil {
				t.Fatalf("an absolute URI audience was refused: %v", err)
			}
		})
	}
}

// The rule belongs to the derivation, not to the schema. A deployment that
// binds audiences from the application's registration takes whatever identifier
// it registered, and Identity Server's is a bare name.
func TestABareAudienceStaysLegalWithoutAResourceBoundDerivation(t *testing.T) {
	if _, err := contexts.Decode([]byte(validV2())); err != nil {
		t.Fatalf("a bare audience was refused on a scoped-refresh identity: %v", err)
	}
	if _, err := contexts.Decode([]byte(withAudience(contexts.ProviderIdentityServer, "reference-status"))); err != nil {
		t.Fatalf("a bare audience was refused on an Identity Server identity: %v", err)
	}
}

// Every provider this shell reads is one the smoke harness may describe, so the
// list has one home rather than a copy per caller.
func TestEveryReadableProviderIsNamed(t *testing.T) {
	named := contexts.Providers()
	for _, provider := range []string{
		contexts.ProviderAsgardeo, contexts.ProviderIdentityServer, contexts.ProviderThunder,
	} {
		if !slices.Contains(named, provider) {
			t.Fatalf("Providers() omits %q: %v", provider, named)
		}
	}
	if !slices.IsSorted(named) {
		t.Fatalf("Providers() is unsorted, so a refusal naming them would vary between runs: %v", named)
	}
}

// withProvider names the identity provider, and gives the product an audience
// that provider would accept. Thunder derives by resource and therefore needs an
// absolute URI; using one for every provider keeps the helper honest without
// making the caller think about it.
func withProvider(provider string) string {
	return withAudience(provider, "https://issuer.example.test/reference-status")
}

func withAudience(provider, audience string) string {
	document := strings.Replace(validV2(),
		`"kind": "oauth-browser",`,
		`"kind": "oauth-browser", "provider": "`+provider+`",`, 1)
	return strings.Replace(document,
		`"audience": "reference-status",`,
		`"audience": "`+audience+`",`, 1)
}

func withDerivation(provider, derivation string) string {
	return strings.Replace(withProvider(provider),
		`"kind": "oauth-browser",`,
		`"kind": "oauth-browser", "narrowing": "`+derivation+`",`, 1)
}

func withSecondProduct(document string) string {
	return strings.Replace(document,
		`"reference": {`,
		`"second": {"endpoint": "https://api.example.test", "audience": "second-api", "scopes": ["second:read"]}, "reference": {`, 1)
}
