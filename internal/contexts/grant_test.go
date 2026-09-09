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
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// withGrantProduct adds a second product reached by a JWT bearer grant at its
// own issuer, as an API Manager behind a ThunderID login is.
func withGrantProduct(document, grant string) string {
	return strings.Replace(document,
		`"reference": {`,
		`"apim": {"endpoint": "https://apim.example.test", "audience": "apim-client", `+
			`"scopes": ["apim:api_view"], "grant": `+grant+`}, "reference": {`, 1)
}

const validGrant = `{"kind": "jwt-bearer", "issuer": "https://apim.example.test/oauth2/token", ` +
	`"clientId": "apim-client", "scopes": ["email", "groups"], ` +
	`"resource": "https://apim.example.test/oauth2/token"}`

func TestAProductMayNameAGrantAtAnotherIssuer(t *testing.T) {
	document, err := contexts.Decode([]byte(withGrantProduct(validV2(), validGrant)))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	product := document.Identities[0].Products["apim"]
	if product.Grant == nil {
		t.Fatal("the grant was not read")
	}
	if product.Grant.Kind != contexts.GrantJWTBearer ||
		product.Grant.Issuer != "https://apim.example.test/oauth2/token" ||
		product.Grant.ClientID != "apim-client" ||
		!slices.Equal(product.Grant.Scopes, []string{"email", "groups"}) {
		t.Fatalf("the grant was read as %+v", *product.Grant)
	}
	encoded, err := document.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(string(encoded), `"grant"`) || !strings.Contains(string(encoded), `"jwt-bearer"`) {
		t.Fatalf("the grant did not survive encoding:\n%s", encoded)
	}
	if !document.Identities[0].Products["reference"].Direct() || product.Direct() {
		t.Fatal("Direct did not tell the two products apart")
	}
}

func TestAssertionScopesAlwaysIncludeOpenID(t *testing.T) {
	grant := contexts.Grant{Kind: contexts.GrantJWTBearer, Scopes: []string{"groups", "email"}}
	got := grant.AssertionScopes()
	want := []string{"email", "groups", "openid"}
	if !slices.Equal(got, want) {
		t.Fatalf("assertion scopes were %v, want %v", got, want)
	}
	if got := (contexts.Grant{Scopes: []string{"openid"}}).AssertionScopes(); !slices.Equal(got, []string{"openid"}) {
		t.Fatalf("openid was duplicated or dropped: %v", got)
	}
}

func TestAMalformedGrantIsRefused(t *testing.T) {
	for name, grant := range map[string]string{
		"unknown kind":            `{"kind": "saml-bearer", "issuer": "https://apim.example.test/oauth2/token", "clientId": "c"}`,
		"no client":               `{"kind": "jwt-bearer", "issuer": "https://apim.example.test/oauth2/token"}`,
		"no issuer":               `{"kind": "jwt-bearer", "clientId": "c"}`,
		"issuer without a scheme": `{"kind": "jwt-bearer", "issuer": "apim.example.test", "clientId": "c"}`,
		"issuer with credentials": `{"kind": "jwt-bearer", "issuer": "https://admin:secret@apim.example.test/oauth2/token", "clientId": "c"}`,
		"jwt-bearer resource that is not an absolute URI": `{"kind": "jwt-bearer", ` +
			`"issuer": "https://apim.example.test/oauth2/token", "clientId": "c", "resource": "not-a-uri"}`,
		"federated resource that is not an absolute URI": `{"kind": "federated", ` +
			`"issuer": "https://apim.example.test/oauth2/token", "clientId": "c", "resource": "not-a-uri"}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := contexts.Decode([]byte(withGrantProduct(validV2(), grant)))
			var typed problem.Problem
			if !errors.As(err, &typed) || typed.Code != "contexts.document_malformed" {
				t.Fatalf("a malformed grant was not refused as such: %v", err)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("the refusal echoed the issuer URL: %v", err)
			}
		})
	}
}

func TestAGrantProductMustNameTheAudienceItIsProvedAgainst(t *testing.T) {
	document := strings.Replace(withGrantProduct(validV2(), validGrant),
		`"audience": "apim-client", `, "", 1)
	_, err := contexts.Decode([]byte(document))
	var typed problem.Problem
	if !errors.As(err, &typed) || typed.Code != "contexts.document_malformed" {
		t.Fatalf("a grant product without an audience was not refused: %v", err)
	}
}

func TestAThunderIdentityMayServeASecondProductByGrant(t *testing.T) {
	document, err := contexts.Decode([]byte(withGrantProduct(
		withProvider(contexts.ProviderThunder), validGrant)))
	if err != nil {
		t.Fatalf("a Thunder identity with one direct product and one grant product was refused: %v", err)
	}
	if len(document.Identities[0].Products) != 2 {
		t.Fatalf("read %d products, want 2", len(document.Identities[0].Products))
	}
	// A second direct product is a sibling session under its own resource
	// indicator, exactly as it is without a grant product alongside it: the
	// grant does not change how many direct products the identity may record.
	_, err = contexts.Decode([]byte(withGrantProduct(
		withSecondProduct(withProvider(contexts.ProviderThunder)), validGrant)))
	if err != nil {
		t.Fatalf("a second direct product alongside a grant product was refused: %v", err)
	}
}

func TestAThunderJWTBearerGrantDecodesWithOrWithoutAnAssertionResource(t *testing.T) {
	// A document written before a jwt-bearer grant's resource was required to
	// carry out an assertion session still has to decode: the resource is
	// needed only at the moment the broker actually derives that product's
	// access (internal/auth), not at document load.
	grantWithoutResource := `{"kind": "jwt-bearer", ` +
		`"issuer": "https://apim.example.test/oauth2/token", "clientId": "apim-client", ` +
		`"scopes": ["email", "groups"]}`
	document, err := contexts.Decode([]byte(withGrantProduct(
		withProvider(contexts.ProviderThunder), grantWithoutResource)))
	if err != nil {
		t.Fatalf("a Thunder jwt-bearer grant without resource was refused: %v", err)
	}
	if len(document.Identities[0].Products) != 2 {
		t.Fatalf("read %d products, want 2", len(document.Identities[0].Products))
	}

	grantWithResource := `{"kind": "jwt-bearer", ` +
		`"issuer": "https://apim.example.test/oauth2/token", "clientId": "apim-client", ` +
		`"scopes": ["email", "groups"], "resource": "https://apim.example.test/oauth2/token"}`
	document, err = contexts.Decode([]byte(withGrantProduct(
		withProvider(contexts.ProviderThunder), grantWithResource)))
	if err != nil {
		t.Fatalf("a Thunder jwt-bearer grant with resource was refused: %v", err)
	}
	if len(document.Identities[0].Products) != 2 {
		t.Fatalf("read %d products, want 2", len(document.Identities[0].Products))
	}
}

func TestAnExchangeGrantNamesNeitherAnIssuerNorAClient(t *testing.T) {
	// The exchange runs at the identity's own issuer as its own client, so a
	// document that repeated either would be stating something the shell
	// already knows and could contradict.
	document, err := contexts.Decode([]byte(strings.Replace(
		withGrantProduct(validV2(), `{"kind": "exchange"}`),
		`"audience": "apim-client"`, `"audience": "https://apim.example.test"`, 1)))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	product := document.Identities[0].Products["apim"]
	if product.Grant == nil || product.Grant.Kind != contexts.GrantExchange {
		t.Fatalf("the exchange grant was read as %+v", product.Grant)
	}
	if product.Direct() {
		t.Fatal("an exchange product was read as direct")
	}
	encoded, err := document.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(string(encoded), `"exchange"`) {
		t.Fatalf("the exchange grant did not survive encoding:\n%s", encoded)
	}
}

func TestAnExchangeGrantIsRefusedWhenItNamesAnIssuerOrAClient(t *testing.T) {
	for name, grant := range map[string]string{
		"names an issuer": `{"kind": "exchange", "issuer": "https://apim.example.test/oauth2/token"}`,
		"names a client":  `{"kind": "exchange", "clientId": "apim-client"}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := contexts.Decode([]byte(withGrantProduct(validV2(), grant)))
			var typed problem.Problem
			if !errors.As(err, &typed) || typed.Code != "contexts.document_malformed" {
				t.Fatalf("an exchange grant naming its own issuer or client was not refused: %v", err)
			}
		})
	}
}

func TestAnExchangeProductMustRegisterAnAbsoluteURIAudience(t *testing.T) {
	// An exchange sends the audience as an RFC 8707 resource indicator, which
	// must be an absolute URI. Measured against ThunderID: a bare name comes
	// back as invalid_target — an error that reads as an unregistered
	// resource server, sending the reader to create one that already exists.
	// Refusing when the product is recorded says the true cause once.
	// "apim-client" is the shape a client-id audience takes on Asgardeo, and
	// it is legal for every other strategy — which is why this is refused for
	// the exchange grant alone rather than for audiences in general.
	_, err := contexts.Decode([]byte(withGrantProduct(validV2(), `{"kind": "exchange"}`)))
	var typed problem.Problem
	if !errors.As(err, &typed) || typed.Code != "contexts.document_malformed" {
		t.Fatalf("a non-URI audience on an exchange product was not refused: %v", err)
	}
	if !strings.Contains(typed.Message, "absolute URI") {
		t.Fatalf("the refusal does not name the cause: %q", typed.Message)
	}
}

func TestAnExchangeGrantWritesNoEmptyIssuerAndClient(t *testing.T) {
	// An exchange uses the identity's own issuer and client, so writing the
	// members as empty strings puts two fields in the document that are not
	// merely unset but meaningless — and that a reader would try to fill in.
	document, err := contexts.Decode([]byte(strings.Replace(
		withGrantProduct(validV2(), `{"kind": "exchange"}`),
		`"audience": "apim-client"`, `"audience": "https://apim.example.test"`, 1)))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	encoded, err := document.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if strings.Contains(string(encoded), `"issuer": ""`) ||
		strings.Contains(string(encoded), `"clientId": ""`) {
		t.Fatalf("the exchange grant wrote empty issuer and client members:\n%s", encoded)
	}
}
