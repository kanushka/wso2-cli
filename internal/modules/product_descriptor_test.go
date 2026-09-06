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

package modules_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/sdk/problem"
)

func thunderDescriptor() *modules.ProductDescriptor {
	return &modules.ProductDescriptor{
		Provider: "thunder", ClientID: "wso2-cli",
		Audience: modules.AudienceResource, DefaultAudience: "https://localhost:8090/mcp",
		Scopes: []string{"system"}, Machine: []string{modules.MachineInline},
	}
}

func TestAProductDescriptorRoundTripsThroughTheReceipt(t *testing.T) {
	receipt := validReceipt()
	receipt.Capabilities.Product = &modules.ProductDescriptor{
		IssuerPath: "/oauth2/token", Audience: modules.AudienceClient,
		Scopes: []string{"apim:api_view"}, Grant: "federated", Machine: []string{modules.MachineCredential},
	}
	encoded, err := receipt.Encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := modules.DecodeReceipt(encoded)
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}
	got := decoded.Capabilities.Product
	if got == nil || got.IssuerPath != "/oauth2/token" || got.Audience != modules.AudienceClient ||
		got.Grant != "federated" || len(got.Scopes) != 1 || len(got.Machine) != 1 {
		t.Errorf("descriptor = %+v", got)
	}
	if !strings.Contains(string(encoded), `"product"`) {
		t.Errorf("the descriptor is not encoded under capabilities:\n%s", encoded)
	}
}

func TestAReceiptWithoutADescriptorIsUnchanged(t *testing.T) {
	receipt := validReceipt()
	if err := receipt.Validate(); err != nil {
		t.Fatal(err)
	}
	encoded, err := receipt.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "product") {
		t.Errorf("an absent descriptor is encoded:\n%s", encoded)
	}
}

func TestAMalformedProductDescriptorIsRefused(t *testing.T) {
	cases := map[string]func(*modules.ProductDescriptor){
		"an audience kind that is neither resource nor client": func(d *modules.ProductDescriptor) { d.Audience = "uri" },
		"a provider the shell does not know":                   func(d *modules.ProductDescriptor) { d.Provider = "okta" },
		"a grant the shell does not implement":                 func(d *modules.ProductDescriptor) { d.Grant = "saml" },
		"a machine strategy the shell does not implement":      func(d *modules.ProductDescriptor) { d.Machine = []string{"derived"} },
		"no scopes":                                            func(d *modules.ProductDescriptor) { d.Scopes = nil },
		"an issuer path that is not a path":                    func(d *modules.ProductDescriptor) { d.IssuerPath = "oauth2/token" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			receipt := validReceipt()
			receipt.Capabilities.Product = thunderDescriptor()
			mutate(receipt.Capabilities.Product)
			err := receipt.Validate()
			var reported problem.Problem
			if !errors.As(err, &reported) || reported.Code != "modules.receipt_malformed" {
				t.Fatalf("validation returned %v, want modules.receipt_malformed", err)
			}
			if !strings.Contains(reported.Message, "product descriptor") {
				t.Errorf("the refusal %q does not name the descriptor", reported.Message)
			}
		})
	}
}

func TestTheDescriptorDerivesTheIssuerAndAudienceFromTheURL(t *testing.T) {
	apim := &modules.ProductDescriptor{IssuerPath: "/oauth2/token", Audience: modules.AudienceClient,
		Scopes: []string{"apim:api_view"}, Grant: "federated"}
	if got := apim.Issuer("https://localhost:9443/"); got != "https://localhost:9443/oauth2/token" {
		t.Errorf("issuer = %q", got)
	}
	if got := apim.AudienceFor("DgP2"); got != "DgP2" {
		t.Errorf("client audience = %q", got)
	}
	thunder := thunderDescriptor()
	if got := thunder.Issuer("http://localhost:8492"); got != "http://localhost:8492" {
		t.Errorf("issuer = %q", got)
	}
	if got := thunder.AudienceFor("wso2-cli"); got != "https://localhost:8090/mcp" {
		t.Errorf("resource audience = %q", got)
	}
	if !thunder.LoginProvider() || apim.LoginProvider() {
		t.Error("LoginProvider is wrong")
	}
	if !thunder.AllowsMachine(modules.MachineInline) || thunder.AllowsMachine(modules.MachineCredential) {
		t.Error("AllowsMachine is wrong")
	}
}
