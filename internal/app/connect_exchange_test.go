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
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/modules/fixture"
)

const (
	platformURL        = "http://localhost:9251"
	platformGatewayURL = "http://localhost:9091"
)

// newExchangeConnectShell adds, beside the connect modules, a product reached
// by exchanging the login session: the API Platform's shape, whose control
// plane and gateway validate the login provider's tokens themselves.
func newExchangeConnectShell(t *testing.T) app.Shell {
	t.Helper()
	shell, _, _ := newConnectShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "platform", Version: "0.1.0",
		AuthAudiences: []string{"platform-management", "platform-gateway"},
		Product: &modules.ProductDescriptor{
			Audience: modules.AudienceResource, Grant: contexts.GrantExchange,
			Gateway: &modules.GatewayDescriptor{Audience: modules.AudienceResource},
		}})
	return shell
}

func TestConnectAnExchangedProductRecordsNoClientAndBindsItsURL(t *testing.T) {
	shell := newExchangeConnectShell(t)
	if code, _, errOut := connect(t, shell, "iam", "connect", thunderURL); code != exit.OK {
		t.Fatalf("iam connect: exit %d: %s", code, errOut)
	}
	code, out, errOut := connect(t, shell, "platform", "connect", platformURL+"/")
	if code != exit.OK {
		t.Fatalf("platform connect: exit %d: %s", code, errOut)
	}
	// loadDocument validates the document, which is what refused the record
	// connect used to write: an exchange grant naming an issuer and a client.
	identity := loadDocument(t, shell).Accounts[0]
	product := identity.Products["platform"]
	if product.Endpoint != platformURL || product.Audience != platformURL ||
		product.Grant == nil || product.Grant.Kind != contexts.GrantExchange ||
		product.Grant.Issuer != "" || product.Grant.ClientID != "" {
		t.Errorf("product = %+v, grant = %+v", product, product.Grant)
	}
	access, _ := identity.Access("platform")
	if access.Strategy != contexts.StrategyExchanged || access.ClientID != "wso2-cli" {
		t.Errorf("access = %+v", access)
	}
	if !hasField(out, "Strategy", contexts.StrategyExchanged) {
		t.Errorf("the report does not name the strategy:\n%s", out)
	}
}

func TestConnectAnExchangedProductTakesAnAudienceOverride(t *testing.T) {
	shell := newExchangeConnectShell(t)
	connect(t, shell, "iam", "connect", thunderURL)
	code, _, errOut := connect(t, shell, "platform", "connect", platformURL, "--audience", "https://api.example")
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if got := loadDocument(t, shell).Accounts[0].Products["platform"].Audience; got != "https://api.example" {
		t.Errorf("audience = %q", got)
	}
}

func TestConnectAnExchangedProductRefusesAClientOfItsOwn(t *testing.T) {
	shell := newExchangeConnectShell(t)
	connect(t, shell, "iam", "connect", thunderURL)
	for _, flags := range [][]string{
		{"--client-id", "platform-cli"},
		{"--client-secret-variable", "PLATFORM_SECRET"},
	} {
		args := append([]string{"platform", "connect", platformURL}, flags...)
		code, _, errOut := connect(t, shell, args...)
		if code != exit.Usage || !strings.Contains(errOut, "shell.conflicting_arguments") ||
			!strings.Contains(errOut, "exchanging the account's own login session") {
			t.Errorf("%v: exit %d, stderr:\n%s", flags, code, errOut)
		}
	}
	if _, recorded := loadDocument(t, shell).Accounts[0].Products["platform"]; recorded {
		t.Error("a refused connect recorded the product")
	}
}

func TestConnectAnExchangedGatewayDefaultsToItsURL(t *testing.T) {
	shell := newExchangeConnectShell(t)
	connect(t, shell, "iam", "connect", thunderURL)
	connect(t, shell, "platform", "connect", platformURL)
	code, _, errOut := connect(t, shell, "platform", "connect", platformGatewayURL, "--gateway")
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	identity := loadDocument(t, shell).Accounts[0]
	gateway := identity.Products["platform"].Gateway
	if gateway == nil || gateway.Endpoint != platformGatewayURL || gateway.Audience != platformGatewayURL {
		t.Fatalf("gateway = %+v", gateway)
	}
	if access, _ := identity.Access(contexts.GatewayKey("platform")); access.Strategy != contexts.StrategyExchanged {
		t.Errorf("gateway strategy = %q", access.Strategy)
	}
}

func TestConnectAnExchangedProductSkipsTheLoginWhenTheSessionIsHeld(t *testing.T) {
	shell := newExchangeConnectShell(t)
	connect(t, shell, "iam", "connect", thunderURL)
	// Without the login session, the product is reached only after a login.
	code, out, errOut := connect(t, shell, "platform", "connect", platformURL)
	if code != exit.OK || !hasField(out, "Next", "Run `wso2 login`.") {
		t.Fatalf("exit %d, stderr %s, out:\n%s", code, errOut, out)
	}
	if err := (session.Store{StateRoot: shell.StateRoot}).Save("account-1",
		session.Session{Issuer: thunderURL, RefreshToken: "rt"}); err != nil {
		t.Fatal(err)
	}
	// With it, the exchange needs nothing more, for the product or its gateway.
	code, out, errOut = connect(t, shell, "platform", "connect", platformURL, "--replace")
	if code != exit.OK || !strings.Contains(out, "Run `wso2 platform --help`.") {
		t.Errorf("product: exit %d, stderr %s, out:\n%s", code, errOut, out)
	}
	code, out, errOut = connect(t, shell, "platform", "connect", platformGatewayURL, "--gateway")
	if code != exit.OK || !strings.Contains(out, "Run `wso2 platform --help`.") {
		t.Errorf("gateway: exit %d, stderr %s, out:\n%s", code, errOut, out)
	}
}

func TestConnectWithNoAccountNamesTheInstalledLoginProvider(t *testing.T) {
	shell := newExchangeConnectShell(t)
	code, _, errOut := connect(t, shell, "platform", "connect", platformURL)
	if code != exit.Usage || !strings.Contains(errOut, "shell.login_provider_required") ||
		!strings.Contains(errOut, "wso2 iam connect <login-provider-url>") {
		t.Fatalf("exit %d, stderr:\n%s", code, errOut)
	}
}
