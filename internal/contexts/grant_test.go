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
