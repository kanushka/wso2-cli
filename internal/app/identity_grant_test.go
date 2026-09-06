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

package app_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
)

func TestAddProductRecordsAGrantAtItsOwnIssuer(t *testing.T) {
	shell, out, errOut := newShell(t)
	installLogin(t, shell, selfHostedDocument())

	code := shell.Run([]string{"identity", "add-product", "idp-customer-example", "apim",
		"--endpoint", "https://apim.customer.example",
		"--audience", "apim-cli-client",
		"--scopes", "apim:api_view,apim:api_create",
		"--grant", "jwt-bearer",
		"--grant-issuer", "https://apim.customer.example/oauth2/token",
		"--grant-client-id", "apim-cli-client",
		"--grant-scopes", "email,groups"})
	if code != exit.OK {
		t.Fatalf("exit = %d, want %d; stdout %s stderr %s", code, exit.OK, out, errOut)
	}
	product := identityNamed(t, loadDocument(t, shell), "idp-customer-example").Products["apim"]
	if product.Grant == nil {
		t.Fatal("no grant was recorded")
	}
	if product.Grant.Kind != contexts.GrantJWTBearer ||
		product.Grant.Issuer != "https://apim.customer.example/oauth2/token" ||
		product.Grant.ClientID != "apim-cli-client" ||
		!slices.Equal(product.Grant.Scopes, []string{"email", "groups"}) {
		t.Fatalf("the grant was recorded as %+v", *product.Grant)
	}
}

func TestAPartialGrantIsRefusedInTheUsageClass(t *testing.T) {
	shell, out, errOut := newShell(t)
	installLogin(t, shell, selfHostedDocument())

	code := shell.Run([]string{"identity", "add-product", "idp-customer-example", "apim",
		"--endpoint", "https://apim.customer.example", "--audience", "apim-cli-client",
		"--grant", "jwt-bearer"})
	if code != exit.Usage {
		t.Fatalf("exit = %d, want the usage class %d; stdout %s stderr %s", code, exit.Usage, out, errOut)
	}
	if _, recorded := identityNamed(t, loadDocument(t, shell), "idp-customer-example").Products["apim"]; recorded {
		t.Fatal("a refused partial grant still reached the document")
	}
}

func TestAGrantProductJoinsAThunderIdentityWithADirectOne(t *testing.T) {
	shell, out, errOut := newShell(t)
	seeded := selfHostedDocument()
	seeded.Identities[0].Auth.Provider = contexts.ProviderThunder
	seeded.Identities[0].Products = map[string]contexts.Product{
		"iam": {Endpoint: "https://thunder.customer.example", Audience: "https://thunder.customer.example/system"},
	}
	installLogin(t, shell, seeded)

	code := shell.Run([]string{"identity", "add-product", "idp-customer-example", "apim",
		"--endpoint", "https://apim.customer.example", "--audience", "apim-cli-client",
		"--scopes", "apim:api_view",
		"--grant", "jwt-bearer", "--grant-issuer", "https://apim.customer.example/oauth2/token",
		"--grant-client-id", "apim-cli-client", "--grant-scopes", "groups"})
	if code != exit.OK {
		t.Fatalf("a grant product was refused beside a Thunder direct product: exit %d, stderr %s", code, errOut)
	}
	if products := identityNamed(t, loadDocument(t, shell), "idp-customer-example").Products; len(products) != 2 {
		t.Fatalf("recorded %d products, want 2", len(products))
	}
	_ = out
}

func TestAnUnknownGrantKindIsRefused(t *testing.T) {
	shell, _, errOut := newShell(t)
	installLogin(t, shell, selfHostedDocument())
	code := shell.Run([]string{"identity", "add-product", "idp-customer-example", "apim",
		"--endpoint", "https://apim.customer.example", "--audience", "apim-cli-client",
		"--grant", "saml-bearer", "--grant-issuer", "https://apim.customer.example/oauth2/token",
		"--grant-client-id", "apim-cli-client"})
	if code != exit.Usage {
		t.Fatalf("exit = %d, want the usage class %d; stderr %s", code, exit.Usage, errOut)
	}
	if !strings.Contains(errOut.String(), "jwt-bearer") {
		t.Fatalf("the refusal does not name the grant that works:\n%s", errOut)
	}
}
