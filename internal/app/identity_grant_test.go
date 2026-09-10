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

	code := shell.Run([]string{"account", "add-product", "idp-customer-example", "apim",
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

	code := shell.Run([]string{"account", "add-product", "idp-customer-example", "apim",
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
	seeded.Accounts[0].Auth.Provider = contexts.ProviderThunder
	seeded.Accounts[0].Products = map[string]contexts.Product{
		"iam": {Endpoint: "https://thunder.customer.example", Audience: "https://thunder.customer.example/system"},
	}
	installLogin(t, shell, seeded)

	code := shell.Run([]string{"account", "add-product", "idp-customer-example", "apim",
		"--endpoint", "https://apim.customer.example", "--audience", "apim-cli-client",
		"--scopes", "apim:api_view",
		"--grant", "jwt-bearer", "--grant-issuer", "https://apim.customer.example/oauth2/token",
		"--grant-client-id", "apim-cli-client", "--grant-scopes", "groups",
		"--grant-resource", "https://apim.customer.example/oauth2/token"})
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
	code := shell.Run([]string{"account", "add-product", "idp-customer-example", "apim",
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

func TestAFederatedGrantIsRecorded(t *testing.T) {
	shell, out, errOut := newShell(t)
	seeded := selfHostedDocument()
	seeded.Accounts[0].Auth.Provider = contexts.ProviderThunder
	seeded.Accounts[0].Products = map[string]contexts.Product{
		"iam": {Endpoint: "https://thunder.customer.example", Audience: "https://thunder.customer.example/system"},
	}
	installLogin(t, shell, seeded)

	code := shell.Run([]string{"account", "add-product", "idp-customer-example", "apim",
		"--endpoint", "https://apim.customer.example", "--audience", "apim-cli-client",
		"--grant", "federated", "--grant-issuer", "https://apim.customer.example/oauth2/token",
		"--grant-client-id", "apim-cli-client"})
	if code != exit.OK {
		t.Fatalf("a federated grant was refused: exit %d, stderr %s", code, errOut)
	}
	product := identityNamed(t, loadDocument(t, shell), "idp-customer-example").Products["apim"]
	if product.Grant == nil {
		t.Fatal("no grant was recorded")
	}
	if product.Grant.Kind != contexts.GrantFederated {
		t.Errorf("grant kind = %q, want federated", product.Grant.Kind)
	}
	if product.Grant.Issuer != "https://apim.customer.example/oauth2/token" {
		t.Errorf("grant issuer = %q, want the one named", product.Grant.Issuer)
	}
	if product.Grant.ClientID != "apim-cli-client" {
		t.Errorf("grant client ID = %q, want the one named", product.Grant.ClientID)
	}
	_ = out
}

func TestGrantResourceWithoutGrantIsRefused(t *testing.T) {
	shell, _, errOut := newShell(t)
	installLogin(t, shell, selfHostedDocument())

	code := shell.Run([]string{"account", "add-product", "idp-customer-example", "apim",
		"--endpoint", "https://apim.customer.example",
		"--grant-resource", "https://apim.customer.example/oauth2/token"})
	if code != exit.Usage {
		t.Fatalf("exit = %d, want the usage class %d; stderr %s", code, exit.Usage, errOut)
	}
	if !strings.Contains(errOut.String(), "--grant") {
		t.Errorf("the refusal does not name --grant:\n%s", errOut)
	}
	if _, recorded := identityNamed(t, loadDocument(t, shell), "idp-customer-example").Products["apim"]; recorded {
		t.Fatal("a refused command with only --grant-resource reached the document")
	}
}

func TestAddProductRecordsAnExchangeGrantFromTheKindAlone(t *testing.T) {
	// An exchange runs at the account's own issuer as its own client, so
	// --grant-issuer and --grant-client-id have nothing to name. Requiring
	// them, as every other grant does, would make a user invent values the
	// shell then has to ignore.
	shell, out, errOut := newShell(t)
	installLogin(t, shell, selfHostedDocument())

	code := shell.Run([]string{"account", "add-product", "idp-customer-example", "apip",
		"--endpoint", "https://apip.customer.example",
		"--audience", "https://apip.customer.example",
		"--grant", "exchange"})
	if code != exit.OK {
		t.Fatalf("exit = %d, want %d; stdout %s stderr %s", code, exit.OK, out, errOut)
	}
	product := identityNamed(t, loadDocument(t, shell), "idp-customer-example").Products["apip"]
	if product.Grant == nil || product.Grant.Kind != contexts.GrantExchange {
		t.Fatalf("the grant was recorded as %+v", product.Grant)
	}
	if product.Grant.Issuer != "" || product.Grant.ClientID != "" {
		t.Fatalf("an exchange grant recorded an issuer or a client: %+v", *product.Grant)
	}
}

func TestAnExchangeGrantIsRefusedWithAnIssuerOrAClient(t *testing.T) {
	for _, extra := range [][]string{
		{"--grant-issuer", "https://apip.customer.example/oauth2/token"},
		{"--grant-client-id", "apip-cli-client"},
	} {
		t.Run(strings.TrimPrefix(extra[0], "--"), func(t *testing.T) {
			shell, out, errOut := newShell(t)
			installLogin(t, shell, selfHostedDocument())
			code := shell.Run(append([]string{"account", "add-product", "idp-customer-example", "apip",
				"--endpoint", "https://apip.customer.example",
				"--audience", "https://apip.customer.example",
				"--grant", "exchange"}, extra...))
			if code != exit.Usage {
				t.Fatalf("exit = %d, want the usage class %d; stdout %s stderr %s", code, exit.Usage, out, errOut)
			}
		})
	}
}

func TestAnExchangeGrantIsSummarizedWithoutAnEmptyIssuerAndClient(t *testing.T) {
	// The summary is built for grants that name an issuer and a client. An
	// exchange names neither, and rendering the template anyway prints
	// "exchange at  as ", which reads as two values the shell failed to load.
	shell, out, errOut := newShell(t)
	installLogin(t, shell, selfHostedDocument())

	code := shell.Run([]string{"account", "add-product", "idp-customer-example", "apip",
		"--endpoint", "https://apip.customer.example",
		"--audience", "https://apip.customer.example",
		"--grant", "exchange"})
	if code != exit.OK {
		t.Fatalf("exit = %d, want %d; stderr %s", code, exit.OK, errOut)
	}
	if strings.Contains(out.String(), "at  as") {
		t.Fatalf("the exchange grant was summarized with an empty issuer and client:\n%s", out)
	}
	if !strings.Contains(out.String(), "exchange at the account's own issuer") {
		t.Fatalf("the exchange grant summary does not say where it runs:\n%s", out)
	}
}
