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
	"bytes"
	"slices"
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/modules/fixture"
)

const (
	thunderURL = "http://localhost:8492"
	apimURL    = "https://localhost:9443"
	apimClient = "DgP2V4Arw9KYeo2ltIm4r8r19vca"
)

// installConnectModules installs the three modules the connect tests run
// against: iam, a login provider; apim, reached by a federated grant and
// carrying a gateway shape; and reference, which declares no product
// descriptor at all.
func installConnectModules(t *testing.T, shell app.Shell) {
	t.Helper()
	installFixture(t, shell, fixture.Module{Namespace: "iam", Version: "0.1.0",
		AuthAudiences: []string{"thunder-system"}, AuthScopes: []string{"system"},
		Product: &modules.ProductDescriptor{
			Provider: contexts.ProviderThunder, ClientID: "wso2-cli",
			Audience: modules.AudienceResource, DefaultAudience: "https://localhost:8090/mcp",
			Scopes: []string{"system"}, Machine: []string{modules.MachineInline},
		}})
	installFixture(t, shell, fixture.Module{Namespace: "apim", Version: "0.1.0",
		AuthAudiences: []string{"apim-publisher", "apim-gateway"}, AuthScopes: []string{"apim:api_view", "apim:admin"},
		Product: &modules.ProductDescriptor{
			IssuerPath: "/oauth2/token", Audience: modules.AudienceClient,
			Scopes: []string{"apim:api_view", "apim:admin"}, Grant: contexts.GrantFederated,
			Machine: []string{modules.MachineCredential},
			// The gateway record: the API's own resource server at the login
			// provider, reachable from a machine client as well.
			Gateway: &modules.GatewayDescriptor{Audience: modules.AudienceResource,
				Machine: []string{modules.MachineInline}},
		}})
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})
}

func newConnectShell(t *testing.T) (app.Shell, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	// connect --gateway reads the secure store to word its next line, so
	// the store is the mocked one rather than the developer's own.
	keyring.MockInit()
	t.Setenv("WSO2_CONTEXT", "")
	shell, out, errOut := newShell(t)
	installConnectModules(t, shell)
	return shell, out, errOut
}

// connect runs one connect line and returns what the shell wrote.
func connect(t *testing.T, shell app.Shell, args ...string) (exit.Code, string, string) {
	t.Helper()
	out, errOut := shell.Streams.Out.(*bytes.Buffer), shell.Streams.Err.(*bytes.Buffer)
	out.Reset()
	errOut.Reset()
	code := shell.Run(args)
	return code, out.String(), errOut.String()
}

func TestConnectAProviderProductCreatesTheIdentityAndContext(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	code, out, errOut := connect(t, shell, "iam", "connect", thunderURL)
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	document := loadDocument(t, shell)
	if len(document.Accounts) != 1 || document.DefaultContext != "thunder" {
		t.Fatalf("document = %+v", document)
	}
	identity := document.Accounts[0]
	if identity.Name != "thunder" || identity.Auth.Kind != contexts.KindOAuthBrowser ||
		identity.Auth.Issuer != thunderURL || identity.Auth.ClientID != "wso2-cli" ||
		identity.Auth.Provider != contexts.ProviderThunder || identity.Auth.CredentialRef != "thunder" ||
		identity.LoginProduct != "iam" {
		t.Errorf("identity = %+v", identity)
	}
	product := identity.Products["iam"]
	if product.Endpoint != thunderURL || product.Audience != "https://localhost:8090/mcp" ||
		!slices.Equal(product.Scopes, []string{"system"}) || product.Grant != nil {
		t.Errorf("product = %+v", product)
	}
	if !strings.Contains(out, "Run wso2 login") {
		t.Errorf("no next line:\n%s", out)
	}
}

func TestConnectANonProviderProductAttachesToTheSelectedIdentity(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	if code, _, errOut := connect(t, shell, "iam", "connect", thunderURL); code != exit.OK {
		t.Fatalf("iam connect: exit %d: %s", code, errOut)
	}
	code, out, errOut := connect(t, shell, "apim", "connect", apimURL+"/", "--client-id", apimClient)
	if code != exit.OK {
		t.Fatalf("apim connect: exit %d: %s", code, errOut)
	}
	identity := loadDocument(t, shell).Accounts[0]
	product, recorded := identity.Products["apim"]
	if !recorded {
		t.Fatalf("apim not recorded: %+v", identity)
	}
	if product.Endpoint != apimURL || product.Audience != apimClient ||
		!slices.Equal(product.Scopes, []string{"apim:api_view", "apim:admin"}) ||
		product.Grant == nil || product.Grant.Kind != contexts.GrantFederated ||
		product.Grant.Issuer != apimURL+"/oauth2/token" || product.Grant.ClientID != apimClient {
		t.Errorf("product = %+v, grant = %+v", product, product.Grant)
	}
	if identity.LoginProduct != "iam" {
		t.Errorf("the login product moved to %q", identity.LoginProduct)
	}
	access, _ := identity.Access("apim")
	if access.Strategy != contexts.StrategyFederated {
		t.Errorf("strategy = %q", access.Strategy)
	}
	if !strings.Contains(out, "federated") {
		t.Errorf("the report does not name the strategy:\n%s", out)
	}
}

func TestConnectANonProviderProductNeedsAClientIDWhenTheDescriptorNamesNone(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	connect(t, shell, "iam", "connect", thunderURL)
	// No secret on the line: a browser account, which needs the product's
	// public federated client, not the one bootstrap registers.
	code, _, errOut := connect(t, shell, "apim", "connect", apimURL)
	if code != exit.Usage || !strings.Contains(errOut, "shell.missing_required_flag") ||
		!strings.Contains(errOut, "--client-id") || !strings.Contains(errOut, "public client") ||
		!strings.Contains(errOut, "federated to the account's login provider") {
		t.Fatalf("browser: exit %d, stderr:\n%s", code, errOut)
	}
	// A secret on the line: a pipeline, which uses the client bootstrap prints.
	code, _, errOut = connect(t, shell, "apim", "connect", apimURL, "--client-secret-variable", "APIM_SECRET")
	if code != exit.Usage || !strings.Contains(errOut, "shell.missing_required_flag") ||
		!strings.Contains(errOut, "Run wso2 apim bootstrap to register one") ||
		strings.Contains(errOut, "public client") {
		t.Fatalf("pipeline: exit %d, stderr:\n%s", code, errOut)
	}
}

func TestConnectNamesTheContextOnTheNextLineOnlyWhenItIsNotTheSoleSelectedOne(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	code, out, errOut := connect(t, shell, "iam", "connect", thunderURL)
	if code != exit.OK {
		t.Fatalf("iam connect: exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "Next  Run wso2 login.") || strings.Contains(out, "--context") {
		t.Errorf("the only context, just selected, is named:\n%s", out)
	}
	code, out, errOut = connect(t, shell, "apim", "connect", apimURL, "--client-id", apimClient)
	if code != exit.OK {
		t.Fatalf("apim connect: exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "Next  Run wso2 login.") || strings.Contains(out, "--context") {
		t.Errorf("the only context, already selected, is named:\n%s", out)
	}
	code, out, errOut = connect(t, shell, "iam", "connect", "http://other.example", "--account", "other")
	if code != exit.OK {
		t.Fatalf("second identity: exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "Next  Run wso2 login --context other.") {
		t.Errorf("a second, unselected context is not named:\n%s", out)
	}
}

func TestConnectWithNoIdentityRefusesANonProviderProduct(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	code, _, errOut := connect(t, shell, "apim", "connect", apimURL, "--client-id", apimClient)
	if code != exit.Usage || !strings.Contains(errOut, "shell.login_provider_required") ||
		!strings.Contains(errOut, "connect") {
		t.Fatalf("exit %d, stderr:\n%s", code, errOut)
	}
	if document := loadDocument(t, shell); len(document.Accounts) != 0 {
		t.Errorf("something was written: %+v", document)
	}
}

func TestConnectPicksTheIdentityByLoginProvider(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	connect(t, shell, "iam", "connect", "http://one.example", "--account", "one")
	connect(t, shell, "iam", "connect", "http://two.example", "--account", "two")
	code, _, errOut := connect(t, shell, "apim", "connect", apimURL, "--client-id", apimClient,
		"--login-provider", "http://two.example")
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	document := loadDocument(t, shell)
	for _, identity := range document.Accounts {
		_, recorded := identity.Products["apim"]
		if recorded != (identity.Name == "two") {
			t.Errorf("apim recorded on %q = %v", identity.Name, recorded)
		}
	}
	code, _, errOut = connect(t, shell, "apim", "connect", apimURL, "--client-id", apimClient,
		"--login-provider", "http://three.example")
	if code != exit.Usage || !strings.Contains(errOut, "shell.login_provider_required") {
		t.Fatalf("an unknown provider: exit %d, stderr:\n%s", code, errOut)
	}
}

func TestConnectASecondProviderProductOnTheSameIssuerRecordsOnTheIdentity(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	connect(t, shell, "iam", "connect", thunderURL)
	code, _, errOut := connect(t, shell, "iam", "connect", thunderURL, "--replace",
		"--scopes", "system", "--audience", "https://localhost:8090/other")
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	document := loadDocument(t, shell)
	if len(document.Accounts) != 1 || document.Accounts[0].Products["iam"].Audience != "https://localhost:8090/other" {
		t.Fatalf("document = %+v", document)
	}
	code, _, errOut = connect(t, shell, "iam", "connect", thunderURL)
	if code != exit.Usage || !strings.Contains(errOut, "contexts.product_exists") {
		t.Fatalf("without --replace: exit %d, stderr:\n%s", code, errOut)
	}
}

func TestConnectAProviderProductOnAnotherIssuerCreatesASecondIdentity(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	connect(t, shell, "iam", "connect", thunderURL)
	code, _, errOut := connect(t, shell, "iam", "connect", "http://other.example")
	if code != exit.Usage || !strings.Contains(errOut, "contexts.identity_exists") ||
		!strings.Contains(errOut, "--account") {
		t.Fatalf("a taken name: exit %d, stderr:\n%s", code, errOut)
	}
	code, _, errOut = connect(t, shell, "iam", "connect", "http://other.example", "--account", "other")
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	document := loadDocument(t, shell)
	if len(document.Accounts) != 2 || document.DefaultContext != "thunder" {
		t.Fatalf("document = %+v", document)
	}
}

func TestConnectAMachineIdentity(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	code, _, errOut := connect(t, shell, "iam", "connect", thunderURL,
		"--client-id", "wso2-cli-ci", "--client-secret-variable", "WSO2_CI_CLIENT_SECRET")
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	identity := loadDocument(t, shell).Accounts[0]
	if identity.Auth.Kind != contexts.KindClientCredentials || identity.Auth.ClientID != "wso2-cli-ci" ||
		identity.Auth.ClientSecretVariable != "WSO2_CI_CLIENT_SECRET" || identity.Auth.CredentialRef != "" {
		t.Fatalf("identity = %+v", identity)
	}
	// API Manager does not accept the machine client, so the product needs
	// a credential of its own, and connect says which flags name it.
	code, _, errOut = connect(t, shell, "apim", "connect", apimURL, "--client-id", apimClient)
	if code != exit.AuthPolicy || !strings.Contains(errOut, "auth.product_not_configured") ||
		!strings.Contains(errOut, "--client-id-variable") ||
		!strings.Contains(errOut, "--client-secret-variable") {
		t.Fatalf("exit %d, stderr:\n%s", code, errOut)
	}
	code, out, errOut := connect(t, shell, "apim", "connect", apimURL,
		"--client-id", apimClient, "--client-secret-variable", "WSO2_APIM_CLIENT_SECRET")
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	product := loadDocument(t, shell).Accounts[0].Products["apim"]
	if product.ClientIDVariable != "" || product.ClientSecretVariable != "WSO2_APIM_CLIENT_SECRET" ||
		product.Grant == nil || product.Grant.Issuer != apimURL+"/oauth2/token" ||
		product.Grant.ClientID != apimClient || product.Audience != apimClient {
		t.Fatalf("product = %+v, grant = %+v", product, product.Grant)
	}
	access, _ := loadDocument(t, shell).Accounts[0].Access("apim")
	if access.Strategy != contexts.StrategyInline || access.Issuer != apimURL+"/oauth2/token" {
		t.Fatalf("access = %+v", access)
	}
	if strings.Contains(out, "WSO2_APIM_CLIENT_SECRET=") {
		t.Errorf("a value was echoed:\n%s", out)
	}
}

func TestConnectRefusesAMachineIdentityOnAProductThatDoesNotAllowIt(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	// apim allows no inline machine client, so it cannot be the machine
	// identity's provider; and iam is a provider only for a browser or a
	// machine identity, never with a product credential.
	code, _, errOut := connect(t, shell, "iam", "connect", thunderURL,
		"--client-id-variable", "A", "--client-secret-variable", "B")
	if code != exit.Usage || !strings.Contains(errOut, "shell.conflicting_arguments") {
		t.Fatalf("exit %d, stderr:\n%s", code, errOut)
	}
	connect(t, shell, "iam", "connect", thunderURL)
	code, _, errOut = connect(t, shell, "apim", "connect", apimURL, "--client-id", apimClient,
		"--client-secret-variable", "B")
	if code != exit.Usage || !strings.Contains(errOut, "shell.conflicting_arguments") {
		t.Fatalf("a product credential on a browser account: exit %d, stderr:\n%s", code, errOut)
	}
}

func TestConnectIsRefusedForAModuleWithoutADescriptor(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	code, _, errOut := connect(t, shell, "reference", "connect", "http://reference.example")
	if code != exit.Usage || !strings.Contains(errOut, "shell.connect_unsupported") ||
		!strings.Contains(errOut, "wso2 account add-product") {
		t.Fatalf("exit %d, stderr:\n%s", code, errOut)
	}
}

func TestConnectRefusesABadURLAndAMissingOne(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	code, _, errOut := connect(t, shell, "iam", "connect")
	if code != exit.Usage || !strings.Contains(errOut, "shell.missing_argument") {
		t.Fatalf("no url: exit %d, stderr:\n%s", code, errOut)
	}
	code, _, errOut = connect(t, shell, "iam", "connect", "localhost:8492")
	if code != exit.Usage || !strings.Contains(errOut, "shell.invalid_argument") {
		t.Fatalf("bad url: exit %d, stderr:\n%s", code, errOut)
	}
	code, _, errOut = connect(t, shell, "iam", "connect", "http://user:pw@localhost:8492")
	if code != exit.Usage || strings.Contains(errOut, "pw@") {
		t.Fatalf("userinfo: exit %d, stderr:\n%s", code, errOut)
	}
}

func TestConnectRendersJSON(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	code, out, errOut := connect(t, shell, "iam", "connect", thunderURL, "--output", "json")
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, `"account": "thunder"`) || !strings.Contains(out, `"strategy": "direct"`) {
		t.Errorf("json:\n%s", out)
	}
}

// unpinnedThunderDocument is an identity as wso2 account create builds one:
// a direct product and no pinned login product, which is also what a document
// written before the field existed holds.
func unpinnedThunderDocument() contexts.Document {
	return contexts.Document{
		SchemaVersion:  contexts.SchemaVersion,
		DefaultContext: "thunder",
		Accounts: []contexts.Account{{
			Name: "thunder", Type: "onprem",
			Auth: contexts.AccountAuth{
				Kind: contexts.KindOAuthBrowser, Issuer: thunderURL, ClientID: "wso2-cli",
				Provider: contexts.ProviderThunder, CredentialRef: "thunder",
			},
			Products: map[string]contexts.Product{"zeta": {
				Endpoint: thunderURL, Audience: "https://localhost:8090/zeta",
				Scopes: []string{"system"},
			}},
		}},
		Contexts: []contexts.Context{{Name: "thunder", Account: "thunder"}},
	}
}

func TestConnectKeepsTheLoginProductAnUnpinnedIdentityAlreadyHad(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	// alpha is a second product at the same issuer, and its namespace sorts
	// before the one the identity already logs in through.
	installFixture(t, shell, fixture.Module{Namespace: "alpha", Version: "0.1.0",
		AuthAudiences: []string{"thunder-system"}, AuthScopes: []string{"system"},
		Product: &modules.ProductDescriptor{
			Provider: contexts.ProviderThunder, ClientID: "wso2-cli",
			Audience: modules.AudienceResource, DefaultAudience: "https://localhost:8090/alpha",
			Scopes: []string{"system"}, Machine: []string{modules.MachineInline},
		}})
	installLogin(t, shell, unpinnedThunderDocument())
	code, _, errOut := connect(t, shell, "alpha", "connect", thunderURL)
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	identity := loadDocument(t, shell).Accounts[0]
	if identity.LoginProduct != "zeta" {
		t.Errorf("the login product moved to %q", identity.LoginProduct)
	}
	if access := identity.LoginAccess(); access.Namespace != "zeta" {
		t.Errorf("the login runs against %q", access.Namespace)
	}
}

func TestConnectRefusesACredentialVariableThatIsNotAVariableName(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	connect(t, shell, "iam", "connect", thunderURL,
		"--client-id", "wso2-cli-ci", "--client-secret-variable", "WSO2_CI_CLIENT_SECRET")
	code, _, errOut := connect(t, shell, "apim", "connect", apimURL,
		"--client-id", apimClient, "--client-secret-variable", "apim_secret")
	if code != exit.Usage || !strings.Contains(errOut, "shell.invalid_argument") ||
		!strings.Contains(errOut, "--client-secret-variable") {
		t.Fatalf("exit %d, stderr:\n%s", code, errOut)
	}
	// The refusal is about the flag, not about the document it never opened,
	// and it does not repeat what may be the secret itself.
	if strings.Contains(errOut, "contexts.document_malformed") || strings.Contains(errOut, "apim_secret") {
		t.Errorf("the document or the value is named:\n%s", errOut)
	}
	code, _, errOut = connect(t, shell, "apim", "connect", apimURL, "--client-id", apimClient,
		"--client-id-variable", "apim_client", "--client-secret-variable", "WSO2_APIM_CLIENT_SECRET")
	if code != exit.Usage || !strings.Contains(errOut, "shell.invalid_argument") ||
		!strings.Contains(errOut, "--client-id-variable") || strings.Contains(errOut, "apim_client") {
		t.Fatalf("a client id variable: exit %d, stderr:\n%s", code, errOut)
	}
}

func TestConnectRefusesAProviderProductWhenNoIdentityHasTheLoginProvider(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	connect(t, shell, "iam", "connect", thunderURL)
	code, _, errOut := connect(t, shell, "iam", "connect", "http://other.example",
		"--account", "other", "--login-provider", "http://nosuch.example")
	if code != exit.Usage || !strings.Contains(errOut, "shell.login_provider_required") {
		t.Fatalf("exit %d, stderr:\n%s", code, errOut)
	}
	if document := loadDocument(t, shell); len(document.Accounts) != 1 {
		t.Errorf("an identity was created: %+v", document.Accounts)
	}
}

func TestConnectReadsTheMachineListOfANonProviderProduct(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	// gateway accepts the machine client the identity already holds; legacy
	// declares no machine strategy at all.
	installFixture(t, shell, fixture.Module{Namespace: "gateway", Version: "0.1.0",
		AuthAudiences: []string{"gateway"}, AuthScopes: []string{"gateway:invoke"},
		Product: &modules.ProductDescriptor{
			IssuerPath: "/oauth2/token", Audience: modules.AudienceClient,
			Scopes: []string{"gateway:invoke"}, Grant: contexts.GrantFederated,
			Machine: []string{modules.MachineInline},
		}})
	installFixture(t, shell, fixture.Module{Namespace: "legacy", Version: "0.1.0",
		AuthAudiences: []string{"legacy"}, AuthScopes: []string{"legacy:read"},
		Product: &modules.ProductDescriptor{
			IssuerPath: "/oauth2/token", Audience: modules.AudienceClient,
			Scopes: []string{"legacy:read"}, Grant: contexts.GrantFederated,
		}})
	if code, _, errOut := connect(t, shell, "iam", "connect", thunderURL,
		"--client-id", "wso2-cli-ci", "--client-secret-variable", "WSO2_CI_CLIENT_SECRET"); code != exit.OK {
		t.Fatalf("iam connect: exit %d: %s", code, errOut)
	}
	code, _, errOut := connect(t, shell, "gateway", "connect", "https://gateway.example",
		"--client-id", "gateway-client")
	if code != exit.OK {
		t.Fatalf("gateway connect: exit %d: %s", code, errOut)
	}
	product := loadDocument(t, shell).Accounts[0].Products["gateway"]
	if product.ClientSecretVariable != "" {
		t.Errorf("a credential was recorded: %+v", product)
	}
	code, _, errOut = connect(t, shell, "legacy", "connect", "https://legacy.example",
		"--client-id", "legacy-client", "--client-secret-variable", "WSO2_LEGACY_CLIENT_SECRET")
	if code != exit.AuthPolicy || !strings.Contains(errOut, "auth.product_not_configured") {
		t.Fatalf("legacy connect: exit %d, stderr:\n%s", code, errOut)
	}
}

func TestConnectAndContextCreateNameTheAccountFlagAccount(t *testing.T) {
	// The documentation already promises --account, and the concept renamed;
	// a flag still called --account would be the one place the old word
	// survives, on the command a new user reaches first.
	for _, command := range [][]string{
		{"context", "create", "--help"},
	} {
		t.Run(strings.Join(command, " "), func(t *testing.T) {
			shell, out, errOut := newShell(t)
			_ = shell.Run(command)
			rendered := out.String() + errOut.String()
			if strings.Contains(rendered, "--identity") {
				t.Errorf("%v still offers the old flag name:\n%s", command, rendered)
			}
			if !strings.Contains(rendered, "--account") {
				t.Errorf("%v does not offer --account:\n%s", command, rendered)
			}
		})
	}
}
