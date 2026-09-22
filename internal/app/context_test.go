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
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/modules/fixture"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/internal/state"
)

const (
	thunderURL    = "http://localhost:8501"
	apiURL        = "http://localhost:9251"
	apiGatewayURL = "http://localhost:9091"
	apimURL       = "https://localhost:9443"
	apimClient    = "DgP2V4Arw9KYeo2ltIm4r8r19vca"
)

// installContextModules installs the products the setup commands resolve
// against: iam, a thunder login provider; api, reached by exchanging the login
// session and carrying a gateway; apim, reached by a federated grant; and
// reference, which declares no product descriptor at all.
func installContextModules(t *testing.T, shell app.Shell) {
	t.Helper()
	installFixture(t, shell, fixture.Module{Namespace: "iam", Version: "0.1.0",
		AuthAudiences: []string{"thunder-system"}, AuthScopes: []string{"system"},
		Product: &modules.ProductDescriptor{
			Provider: contexts.ProviderThunder, ClientID: "wso2-cli",
			Audience: modules.AudienceResource, DefaultAudience: "https://localhost:8090/mcp",
			Scopes: []string{"system"}, Machine: []string{modules.MachineInline},
		}})
	installFixture(t, shell, fixture.Module{Namespace: "api", Version: "0.1.0",
		AuthAudiences: []string{"api-platform"}, AuthScopes: []string{},
		Product: &modules.ProductDescriptor{
			Audience: modules.AudienceResource, Grant: contexts.GrantExchange,
			Gateway: &modules.GatewayDescriptor{Audience: modules.AudienceResource},
		}})
	installFixture(t, shell, fixture.Module{Namespace: "apim", Version: "0.1.0",
		AuthAudiences: []string{"apim-publisher"}, AuthScopes: []string{"apim:api_view"},
		Product: &modules.ProductDescriptor{
			IssuerPath: "/oauth2/token", Audience: modules.AudienceClient,
			Scopes: []string{"apim:api_view"}, Grant: contexts.GrantFederated,
			Machine: []string{modules.MachineCredential},
		}})
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})
}

// newContextShell is a shell with the setup products installed and the
// developer's own selection neutralized.
func newContextShell(t *testing.T) (app.Shell, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	keyring.MockInit()
	t.Setenv("WSO2_CONTEXT", "")
	t.Setenv("WSO2_NO_INPUT", "")
	shell, out, errOut := newShell(t)
	installContextModules(t, shell)
	return shell, out, errOut
}

// run runs one command line and returns what the shell wrote.
func run(t *testing.T, shell app.Shell, args ...string) (exit.Code, string, string) {
	t.Helper()
	out, errOut := shell.Streams.Out.(*bytes.Buffer), shell.Streams.Err.(*bytes.Buffer)
	out.Reset()
	errOut.Reset()
	code := shell.Run(args)
	return code, out.String(), errOut.String()
}

// mustRun runs one command line and fails the test unless it succeeds.
func mustRun(t *testing.T, shell app.Shell, args ...string) string {
	t.Helper()
	code, out, errOut := run(t, shell, args...)
	if code != exit.OK {
		t.Fatalf("%v: exit %d\nstdout: %s\nstderr: %s", args, code, out, errOut)
	}
	return out
}

// oneContextDocument is one browser context with no products, as a creating
// login leaves it.
func oneContextDocument() contexts.Document {
	return contexts.Document{
		SchemaVersion:  contexts.SchemaVersion,
		DefaultContext: "acme",
		Contexts: []contexts.Context{{Name: "acme", Type: "cloud", CredentialRef: "acme",
			Login:        contexts.Login{Kind: contexts.KindOAuthBrowser, Issuer: "https://idp.example", ClientID: "wso2-cli"},
			Organization: "acme"}},
	}
}

// twoContextDocument adds a second context, beta, beside acme.
func twoContextDocument() contexts.Document {
	document := oneContextDocument()
	beta := document.Contexts[0]
	beta.Name, beta.CredentialRef, beta.Organization = "beta", "beta", "beta"
	document.Contexts = append(document.Contexts, beta)
	return document
}

// loadDocument reads what a command actually wrote, through the shell's own
// reader rather than through a second parser that could disagree with it.
func loadDocument(t *testing.T, shell app.Shell) contexts.Document {
	t.Helper()
	document, err := contexts.Load(shell.StateRoot)
	if err != nil {
		t.Fatalf("contexts.Load: %v", err)
	}
	return document
}

// contextNamed reports the named context, or fails the test.
func contextNamed(t *testing.T, document contexts.Document, name string) contexts.Context {
	t.Helper()
	found, ok := document.Find(name)
	if !ok {
		t.Fatalf("the document declares no context named %q: %+v", name, document.Contexts)
	}
	return found
}

// localSetup creates the local context through iam and adds api with its
// gateway, the demo notebook's setup.
func localSetup(t *testing.T, shell app.Shell) {
	t.Helper()
	mustRun(t, shell, "context", "create", "local", "--login-product", "iam", "--url", thunderURL, "--use")
	mustRun(t, shell, "context", "product", "add", "api", "--url", apiURL, "--gateway", apiGatewayURL)
}

func TestContextCreateThroughALoginProductWritesTheCompleteRecord(t *testing.T) {
	shell, _, _ := newContextShell(t)
	out := mustRun(t, shell, "context", "create", "local", "--login-product", "iam", "--url", thunderURL+"/")

	document := loadDocument(t, shell)
	local := contextNamed(t, document, "local")
	if local.CredentialRef != "local" || local.Type != contexts.TypeOnprem {
		t.Errorf("context = %+v", local)
	}
	want := contexts.Login{Kind: contexts.KindOAuthBrowser, Issuer: thunderURL, ClientID: "wso2-cli",
		Provider: contexts.ProviderThunder, Product: "iam"}
	if local.Login != want {
		t.Errorf("login = %+v, want %+v", local.Login, want)
	}
	iam := local.Products["iam"]
	if iam.Endpoint != thunderURL || iam.Audience != "https://localhost:8090/mcp" ||
		!slices.Equal(iam.Scopes, []string{"system"}) {
		t.Errorf("iam = %+v", iam)
	}
	// Nothing is selected unless asked: one selection rule, --use.
	if document.DefaultContext != "" {
		t.Errorf("create selected %q without --use", document.DefaultContext)
	}
	if !strings.Contains(out, "wso2 context use local") {
		t.Errorf("the report does not say how to select it:\n%s", out)
	}
}

func TestContextCreateWithUseSelectsTheContext(t *testing.T) {
	shell, _, _ := newContextShell(t)
	out := mustRun(t, shell, "context", "create", "local", "--login-product", "iam", "--url", thunderURL, "--use")
	if selected := loadDocument(t, shell).DefaultContext; selected != "local" {
		t.Fatalf("selected = %q", selected)
	}
	if !strings.Contains(out, "Run `wso2 login`") {
		t.Errorf("the report does not name the login:\n%s", out)
	}
}

func TestContextCreateThroughAnIssuerNeedsNoProduct(t *testing.T) {
	shell, _, _ := newContextShell(t)
	mustRun(t, shell, "context", "create", "corp", "--issuer", "https://idp.corp.example/oauth2/token",
		"--client-id", "cli", "--provider", contexts.ProviderIdentityServer, "--organization", "retail")
	corp := contextNamed(t, loadDocument(t, shell), "corp")
	if corp.Login.Product != "" || len(corp.Products) != 0 || corp.Login.Issuer != "https://idp.corp.example/oauth2/token" ||
		corp.Login.Provider != contexts.ProviderIdentityServer || corp.Organization != "retail" {
		t.Errorf("context = %+v", corp)
	}
}

func TestContextCreateWithAClientSecretVariableIsAMachineContext(t *testing.T) {
	shell, _, _ := newContextShell(t)
	mustRun(t, shell, "context", "create", "ci", "--login-product", "iam", "--url", thunderURL,
		"--client-id", "ci-client", "--client-secret-variable", "CI_SECRET")
	ci := contextNamed(t, loadDocument(t, shell), "ci")
	if ci.Login.Kind != contexts.KindClientCredentials || ci.CredentialRef != "" ||
		ci.Login.ClientSecretVariable != "CI_SECRET" || ci.Login.ClientID != "ci-client" {
		t.Errorf("context = %+v", ci)
	}
}

func TestContextCreateRefusals(t *testing.T) {
	cases := map[string]struct {
		args []string
		code string
	}{
		"a url alone":           {[]string{"--url", thunderURL}, "shell.missing_required_flag"},
		"a login product alone": {[]string{"--login-product", "iam"}, "shell.missing_required_flag"},
		"neither form":          {nil, "shell.missing_required_flag"},
		"both forms": {[]string{"--login-product", "iam", "--url", thunderURL, "--issuer", thunderURL,
			"--client-id", "x"}, "shell.conflicting_arguments"},
		"an issuer without a client":    {[]string{"--issuer", thunderURL}, "shell.missing_required_flag"},
		"thunder through an issuer":     {[]string{"--issuer", thunderURL, "--client-id", "x", "--provider", "thunder"}, "shell.conflicting_arguments"},
		"a product that is no provider": {[]string{"--login-product", "api", "--url", apiURL}, "shell.invalid_argument"},
		"an unknown provider":           {[]string{"--issuer", thunderURL, "--client-id", "x", "--provider", "okta"}, "shell.invalid_argument"},
		"a secret where a name belongs": {[]string{"--login-product", "iam", "--url", thunderURL,
			"--client-secret-variable", "s3cr3t-value"}, "shell.invalid_argument"},
		"a missing product under --no-install": {[]string{"--login-product", "nosuch", "--url", thunderURL,
			"--no-install"}, "shell.product_not_installed"},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			shell, _, _ := newContextShell(t)
			code, _, errOut := run(t, shell, append([]string{"context", "create", "local"}, testCase.args...)...)
			if code != exit.Usage && code != exit.AuthPolicy {
				t.Fatalf("exit %d, want a refusal: %s", code, errOut)
			}
			if !strings.Contains(errOut, testCase.code) {
				t.Errorf("stderr does not carry %s:\n%s", testCase.code, errOut)
			}
			if strings.Contains(errOut, "s3cr3t-value") {
				t.Errorf("the refusal echoes the value:\n%s", errOut)
			}
			if _, err := os.Stat(contexts.Path(shell.StateRoot)); err == nil {
				t.Error("a refused create wrote the document")
			}
		})
	}
}

func TestContextCreateRefusesATakenNameAndAnIllegalOne(t *testing.T) {
	for _, name := range []string{"acme", "Acme", "a b", "1acme"} {
		t.Run(name, func(t *testing.T) {
			shell, _, _ := newContextShell(t)
			installLogin(t, shell, oneContextDocument())
			code, _, errOut := run(t, shell, "context", "create", name, "--login-product", "iam", "--url", thunderURL)
			if code != exit.Usage {
				t.Fatalf("exit %d: %s", code, errOut)
			}
			if strings.Contains(errOut, "remove it") {
				t.Errorf("the refusal offers to remove the user's document:\n%s", errOut)
			}
		})
	}
}

func TestProductAddResolvesAnExchangedProductAndItsGateway(t *testing.T) {
	shell, _, _ := newContextShell(t)
	localSetup(t, shell)
	local := contextNamed(t, loadDocument(t, shell), "local")
	api := local.Products["api"]
	if api.Endpoint != apiURL || api.Audience != apiURL || api.Grant == nil || api.Grant.Kind != contexts.GrantExchange {
		t.Fatalf("api = %+v", api)
	}
	if api.Gateway == nil || api.Gateway.Endpoint != apiGatewayURL || api.Gateway.Audience != apiGatewayURL {
		t.Fatalf("gateway = %+v", api.Gateway)
	}
	if local.Login.Product != "iam" {
		t.Errorf("the login product moved to %q", local.Login.Product)
	}
	access, _ := local.Account().Access("api")
	if access.Strategy != contexts.StrategyExchanged {
		t.Errorf("strategy = %q", access.Strategy)
	}
}

func TestProductAddFreezesTheLoginBeforeAnEarlierSortingProduct(t *testing.T) {
	shell, _, _ := newContextShell(t)
	mustRun(t, shell, "context", "create", "local", "--issuer", "https://idp.corp.example", "--client-id", "cli", "--use")
	mustRun(t, shell, "context", "product", "add", "reference", "--url", "https://ref.example",
		"--audience", "reference-status", "--scopes", "reference:status:read")
	local := contextNamed(t, loadDocument(t, shell), "local")
	if local.Login.Product != "reference" {
		t.Fatalf("login product = %q, want the only direct product frozen", local.Login.Product)
	}
}

func TestProductAddRefusesAnExistingProductWithoutReplace(t *testing.T) {
	shell, _, _ := newContextShell(t)
	localSetup(t, shell)
	code, _, errOut := run(t, shell, "context", "product", "add", "api", "--url", apiURL)
	if code != exit.Usage || !strings.Contains(errOut, "contexts.product_exists") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

func TestProductAddReplaceEndsTheSessionsItRebinds(t *testing.T) {
	shell, _, _ := newContextShell(t)
	mustRun(t, shell, "context", "create", "local", "--login-product", "iam", "--url", thunderURL, "--use")
	mustRun(t, shell, "context", "product", "add", "reference", "--url", "https://ref.example",
		"--audience", "https://ref.example", "--scopes", "reference:status:read")
	store := session.Store{StateRoot: shell.StateRoot}
	sibling := contexts.ProductSessionRef("local", "reference")
	if err := store.Save(sibling, session.Session{Issuer: thunderURL, RefreshToken: "sibling-token"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("local", session.Session{Issuer: thunderURL, RefreshToken: "login-token"}); err != nil {
		t.Fatal(err)
	}

	// --dry-run names the session and ends nothing.
	out := mustRun(t, shell, "context", "product", "add", "reference", "--url", "https://ref.example",
		"--audience", "https://ref.example/v2", "--scopes", "reference:status:read", "--replace", "--dry-run")
	if !strings.Contains(out, "reference session") {
		t.Errorf("the dry run does not name the session it would end:\n%s", out)
	}
	if present, _ := store.Stored(sibling); !present {
		t.Fatal("the dry run ended a session")
	}
	if got := contextNamed(t, loadDocument(t, shell), "local").Products["reference"].Audience; got != "https://ref.example" {
		t.Fatalf("the dry run wrote the record: audience %q", got)
	}

	mustRun(t, shell, "context", "product", "add", "reference", "--url", "https://ref.example",
		"--audience", "https://ref.example/v2", "--scopes", "reference:status:read", "--replace")
	if present, _ := store.Stored(sibling); present {
		t.Error("the rebound sibling session was kept")
	}
	if present, _ := store.Stored("local"); !present {
		t.Error("the login session, whose binding did not change, was ended")
	}
	if got := contextNamed(t, loadDocument(t, shell), "local").Products["reference"].Audience; got != "https://ref.example/v2" {
		t.Errorf("audience = %q", got)
	}
}

func TestProductAddTargetsTheContextFlagOverTheSelection(t *testing.T) {
	shell, _, _ := newContextShell(t)
	localSetup(t, shell)
	mustRun(t, shell, "context", "create", "other", "--login-product", "iam", "--url", "https://other:8501")
	mustRun(t, shell, "context", "product", "add", "api", "--url", "http://other:9251", "--context", "other")
	document := loadDocument(t, shell)
	if _, recorded := contextNamed(t, document, "other").Products["api"]; !recorded {
		t.Error("--context did not target the other context")
	}
	if got := contextNamed(t, document, "local").Products["api"].Endpoint; got != apiURL {
		t.Errorf("the selected context changed: %q", got)
	}
}

func TestProductAddWithNothingSelectedIsRefused(t *testing.T) {
	shell, _, _ := newContextShell(t)
	mustRun(t, shell, "context", "create", "local", "--login-product", "iam", "--url", thunderURL)
	code, _, errOut := run(t, shell, "context", "product", "add", "api", "--url", apiURL)
	if code != exit.Usage || !strings.Contains(errOut, "contexts.no_context_selected") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

func TestProductRemoveEndsTheProductsOwnSessionFirst(t *testing.T) {
	shell, _, _ := newContextShell(t)
	mustRun(t, shell, "context", "create", "local", "--login-product", "iam", "--url", thunderURL, "--use")
	mustRun(t, shell, "context", "product", "add", "reference", "--url", "https://ref.example",
		"--audience", "https://ref.example", "--scopes", "reference:status:read")
	store := session.Store{StateRoot: shell.StateRoot}
	sibling := contexts.ProductSessionRef("local", "reference")
	if err := store.Save(sibling, session.Session{Issuer: thunderURL, RefreshToken: "sibling-token"}); err != nil {
		t.Fatal(err)
	}
	mustRun(t, shell, "context", "product", "remove", "reference")
	if present, _ := store.Stored(sibling); present {
		t.Error("the removed product's session was kept")
	}
	if _, recorded := contextNamed(t, loadDocument(t, shell), "local").Products["reference"]; recorded {
		t.Error("the product is still recorded")
	}
}

func TestProductRemoveRefusesTheLoginProduct(t *testing.T) {
	shell, _, _ := newContextShell(t)
	localSetup(t, shell)
	code, _, errOut := run(t, shell, "context", "product", "remove", "iam")
	if code != exit.Usage || !strings.Contains(errOut, "contexts.login_product") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

func TestProductRemoveTakesAGatewayAlone(t *testing.T) {
	shell, _, _ := newContextShell(t)
	localSetup(t, shell)
	mustRun(t, shell, "context", "product", "remove", "api/gateway")
	api := contextNamed(t, loadDocument(t, shell), "local").Products["api"]
	if api.Gateway != nil || api.Endpoint != apiURL {
		t.Fatalf("api = %+v", api)
	}
}

func TestContextDeleteEndsEverySessionAndClearsTheSelection(t *testing.T) {
	shell, _, _ := newContextShell(t)
	localSetup(t, shell)
	store := session.Store{StateRoot: shell.StateRoot}
	if err := store.Save("local", session.Session{Issuer: thunderURL, RefreshToken: "login-token"}); err != nil {
		t.Fatal(err)
	}
	out := mustRun(t, shell, "context", "delete", "local", "--dry-run")
	if present, _ := store.Stored("local"); !present || len(loadDocument(t, shell).Contexts) != 1 {
		t.Fatalf("the dry run changed something:\n%s", out)
	}
	mustRun(t, shell, "context", "delete", "local")
	if present, _ := store.Stored("local"); present {
		t.Error("the deleted context's login session was kept")
	}
	document := loadDocument(t, shell)
	if len(document.Contexts) != 0 || document.DefaultContext != "" {
		t.Errorf("document = %+v", document)
	}
}

func TestContextRenameKeepsTheSessionsAndTheSelection(t *testing.T) {
	shell, _, _ := newContextShell(t)
	localSetup(t, shell)
	mustRun(t, shell, "context", "rename", "local", "demo")
	document := loadDocument(t, shell)
	demo := contextNamed(t, document, "demo")
	if demo.CredentialRef != "local" || document.DefaultContext != "demo" {
		t.Fatalf("renamed = %+v, selected %q", demo, document.DefaultContext)
	}
	code, _, errOut := run(t, shell, "context", "rename", "demo", "Bad Name")
	if code != exit.Usage {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

// teamFile is a short input file of the kind a platform team shares.
const teamFile = `{
  "contexts": [
    {
      "name": "local",
      "login": {"product": "iam"},
      "products": {
        "iam": {"url": "http://localhost:8501"},
        "api": {"url": "http://localhost:9251", "gateway": {"url": "http://localhost:9091"}}
      }
    },
    {
      "name": "staging",
      "login": {"product": "iam", "clientId": "staging-cli"},
      "products": {"iam": {"url": "https://idp.staging.example"}}
    }
  ]
}`

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "team-context.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestApplyWritesCompleteRecordsAndSelectsOnlyWithUse(t *testing.T) {
	shell, _, _ := newContextShell(t)
	file := writeFile(t, teamFile)
	out := mustRun(t, shell, "context", "apply", "-f", file)
	document := loadDocument(t, shell)
	if document.DefaultContext != "" {
		t.Errorf("apply selected %q without --use", document.DefaultContext)
	}
	if !strings.Contains(out, "wso2 context use") {
		t.Errorf("the report does not say how to select one:\n%s", out)
	}
	local := contextNamed(t, document, "local")
	if local.Login.Issuer != thunderURL || local.Login.ClientID != "wso2-cli" || local.Login.Provider != contexts.ProviderThunder ||
		local.CredentialRef != "local" || local.Products["iam"].Audience != "https://localhost:8090/mcp" ||
		local.Products["api"].Grant.Kind != contexts.GrantExchange || local.Products["api"].Gateway.Audience != apiGatewayURL {
		t.Fatalf("local = %+v", local)
	}
	if contextNamed(t, document, "staging").Login.ClientID != "staging-cli" {
		t.Error("the file's own client was not kept")
	}

	mustRun(t, shell, "context", "apply", "-f", file, "--use", "local")
	if selected := loadDocument(t, shell).DefaultContext; selected != "local" {
		t.Fatalf("--use selected %q", selected)
	}
}

func TestApplyKeepsAnExistingSelectionAndOtherContexts(t *testing.T) {
	shell, _, _ := newContextShell(t)
	installLogin(t, shell, oneContextDocument())
	mustRun(t, shell, "context", "apply", "-f", writeFile(t, teamFile))
	document := loadDocument(t, shell)
	if document.DefaultContext != "acme" || len(document.Contexts) != 3 {
		t.Fatalf("document = %+v", document)
	}
}

func TestApplyReplacesByNameKeepingTheReferenceAndReportsChanges(t *testing.T) {
	shell, _, _ := newContextShell(t)
	mustRun(t, shell, "context", "apply", "-f", writeFile(t, teamFile))
	changed := strings.Replace(teamFile, `"http://localhost:9251"`, `"http://localhost:9999"`, 1)
	out := mustRun(t, shell, "context", "apply", "-f", writeFile(t, changed), "--dry-run")
	if !strings.Contains(out, "products.api.url: http://localhost:9251 -> http://localhost:9999") {
		t.Errorf("the dry run does not show the change:\n%s", out)
	}
	if got := contextNamed(t, loadDocument(t, shell), "local").Products["api"].Endpoint; got != apiURL {
		t.Fatalf("the dry run wrote: %q", got)
	}
	mustRun(t, shell, "context", "apply", "-f", writeFile(t, changed))
	local := contextNamed(t, loadDocument(t, shell), "local")
	if local.Products["api"].Endpoint != "http://localhost:9999" || local.CredentialRef != "local" {
		t.Fatalf("local = %+v", local)
	}
	out = mustRun(t, shell, "context", "apply", "-f", writeFile(t, changed))
	if !strings.Contains(out, "unchanged") {
		t.Errorf("a second apply does not report the contexts unchanged:\n%s", out)
	}
}

func TestApplyRefusesMachineSpecificAndUnknownMembers(t *testing.T) {
	cases := map[string]string{
		"a selection":            `{"defaultContext": "local", "contexts": []}`,
		"a credential ref":       `{"contexts": [{"name": "local", "credentialRef": "x", "login": {"issuer": "https://i.example", "clientId": "c"}}]}`,
		"a misspelled member":    `{"contexts": [{"name": "local", "logn": {}}]}`,
		"no login":               `{"contexts": [{"name": "local"}]}`,
		"a login product absent": `{"contexts": [{"name": "local", "login": {"product": "iam"}, "products": {}}]}`,
		"two of one name":        `{"contexts": [{"name": "a", "login": {"issuer": "https://i.example", "clientId": "c"}}, {"name": "a", "login": {"issuer": "https://i.example", "clientId": "c"}}]}`,
		"a url with a password":  `{"contexts": [{"name": "a", "login": {"product": "iam"}, "products": {"iam": {"url": "http://u:s3cr3t@h"}}}]}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			shell, _, _ := newContextShell(t)
			code, _, errOut := run(t, shell, "context", "apply", "-f", writeFile(t, content))
			if code != exit.Usage {
				t.Fatalf("exit %d: %s", code, errOut)
			}
			if strings.Contains(errOut, "s3cr3t") {
				t.Errorf("the refusal echoes the credential:\n%s", errOut)
			}
			if _, err := os.Stat(contexts.Path(shell.StateRoot)); err == nil {
				t.Error("a refused apply wrote the document")
			}
		})
	}
}

func TestApplyWithNoInstallTakesACompleteRecordAsWritten(t *testing.T) {
	shell, _, _ := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	complete := `{"contexts": [{"name": "corp", "login": {"issuer": "https://idp.corp.example", "clientId": "cli"},
	  "products": {"orders": {"url": "https://orders.example", "audience": "orders", "scopes": ["orders:read"]}}}]}`
	mustRun(t, shell, "context", "apply", "-f", writeFile(t, complete), "--no-install", "--use", "corp")
	corp := contextNamed(t, loadDocument(t, shell), "corp")
	if corp.Products["orders"].Audience != "orders" || corp.Login.Product != "orders" {
		t.Fatalf("corp = %+v", corp)
	}
}

func TestExportRoundTripsThroughApplyWithoutMachineFacts(t *testing.T) {
	shell, _, _ := newContextShell(t)
	localSetup(t, shell)
	exported := mustRun(t, shell, "context", "export")
	if strings.Contains(exported, "credentialRef") || strings.Contains(exported, "defaultContext") {
		t.Fatalf("the export carries machine facts:\n%s", exported)
	}
	other, _, _ := newContextShell(t)
	mustRun(t, other, "context", "apply", "-f", writeFile(t, exported), "--use", "local")
	mine := contextNamed(t, loadDocument(t, shell), "local")
	theirs := contextNamed(t, loadDocument(t, other), "local")
	mineJSON, _ := json.Marshal(mine)
	theirsJSON, _ := json.Marshal(theirs)
	if string(mineJSON) != string(theirsJSON) {
		t.Fatalf("the round trip changed the context:\nbefore %s\nafter  %s", mineJSON, theirsJSON)
	}
}

func TestEditWritesAValidChangeAndRefusesAnInvalidOne(t *testing.T) {
	shell, _, _ := newContextShell(t)
	localSetup(t, shell)
	shell.RunEditor = func(path string) error {
		data, _ := os.ReadFile(path)
		edited := strings.Replace(string(data), `"name": "local",`, `"name": "local", "project": "retail",`, 1)
		return os.WriteFile(path, []byte(edited), 0o600)
	}
	mustRun(t, shell, "context", "edit")
	if got := contextNamed(t, loadDocument(t, shell), "local").Project; got != "retail" {
		t.Fatalf("project = %q", got)
	}
	before, _ := os.ReadFile(contexts.Path(shell.StateRoot))
	shell.RunEditor = func(path string) error {
		data, _ := os.ReadFile(path)
		return os.WriteFile(path, []byte(strings.Replace(string(data), `"kind": "oauth-browser"`, `"kind": "password"`, 1)), 0o600)
	}
	code, _, errOut := run(t, shell, "context", "edit")
	if code != exit.Usage {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	after, _ := os.ReadFile(contexts.Path(shell.StateRoot))
	if string(before) != string(after) {
		t.Error("an invalid edit was written")
	}
}

func TestEditWithoutATerminalPointsAtTheFile(t *testing.T) {
	shell, _, _ := newContextShell(t)
	localSetup(t, shell)
	code, _, errOut := run(t, shell, "context", "edit", "--no-input")
	if code != exit.Usage || !strings.Contains(errOut, contexts.Path(shell.StateRoot)) {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

func TestRemovedCommandsNameTheirReplacement(t *testing.T) {
	cases := map[string]struct {
		args []string
		want string
	}{
		"account list":   {[]string{"account", "list"}, "wso2 context show"},
		"bare account":   {[]string{"account"}, "wso2 context --help"},
		"account create": {[]string{"account", "create", "demo", "--issuer", thunderURL, "--client-id", "cli"}, "wso2 context create demo --issuer " + thunderURL + " --client-id cli"},
		"account add-product": {[]string{"account", "add-product", "demo", "orders", "--endpoint", "https://o.example"},
			"wso2 context product add orders --url https://o.example --context demo"},
		"account remove-product": {[]string{"account", "remove-product", "demo", "orders"}, "wso2 context product remove orders --context demo"},
		"account rename":         {[]string{"account", "rename", "demo", "local"}, "wso2 context rename demo local"},
		"login provider connect": {[]string{"iam", "connect", thunderURL, "--account", "demo"},
			"wso2 context create demo --login-product iam --url " + thunderURL + " --use"},
		"product connect": {[]string{"api", "connect", apiURL, "--account", "demo"},
			"wso2 context product add api --url " + apiURL + " --context demo"},
		"gateway connect": {[]string{"api", "connect", apiGatewayURL, "--gateway", "--account", "demo"},
			"wso2 context product add api --url <api-url> --gateway " + apiGatewayURL + " --replace --context demo"},
		"connect under an uninstalled namespace": {[]string{"orders", "connect", "https://o.example"},
			"wso2 context product add orders --url https://o.example"},
		"the identity verbs ADR 0015 moved": {[]string{"identity", "list"}, "wso2 context show"},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			shell, _, _ := newContextShell(t)
			code, _, errOut := run(t, shell, testCase.args...)
			if code != exit.Usage {
				t.Fatalf("exit %d, want usage: %s", code, errOut)
			}
			if !strings.Contains(errOut, "shell.command_moved") || !strings.Contains(errOut, testCase.want) {
				t.Errorf("stderr does not name %q:\n%s", testCase.want, errOut)
			}
		})
	}
}

func TestContextUseSelectsAndWritesNothingElse(t *testing.T) {
	shell, _, errOut := newShell(t)
	installLogin(t, shell, twoContextDocument())
	before := loadDocument(t, shell)

	if code := shell.Run([]string{"context", "use", "beta"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	after := loadDocument(t, shell)
	if after.DefaultContext != "beta" {
		t.Errorf("defaultContext = %q, want %q", after.DefaultContext, "beta")
	}
	before.DefaultContext = after.DefaultContext
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	if string(beforeJSON) != string(afterJSON) {
		t.Errorf("wso2 context use changed more than the selection:\nbefore %s\nafter  %s", beforeJSON, afterJSON)
	}
}

func TestContextUseIsRefusedForAnUnknownName(t *testing.T) {
	shell, _, errOut := newShell(t)
	installLogin(t, shell, oneContextDocument())

	if code := shell.Run([]string{"context", "use", "nosuch"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want the usage class %d; stderr: %s", code, exit.Usage, errOut)
	}
	if !strings.Contains(errOut.String(), "contexts.unknown_context") {
		t.Errorf("stderr does not carry contexts.unknown_context:\n%s", errOut)
	}
	if selected := loadDocument(t, shell).DefaultContext; selected != "acme" {
		t.Errorf("a refused use changed the selection to %q", selected)
	}
}

func TestContextListRendersEveryContextAndMarksTheDefault(t *testing.T) {
	shell, out, errOut := newShell(t)
	seeded := twoContextDocument()
	seeded.DefaultContext = "beta"
	installLogin(t, shell, seeded)

	if code := shell.Run([]string{"context", "list"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	selected := ""
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.Contains(line, "*") {
			selected = line
		}
	}
	if !strings.Contains(selected, "beta") || !strings.Contains(out.String(), "acme") {
		t.Errorf("the listing does not mark beta as the selected context:\n%s", out)
	}
	header := strings.SplitN(out.String(), "\n", 2)[0]
	if strings.Contains(header, "ACCOUNT") || strings.Contains(header, "IDENTITY") {
		t.Errorf("wso2 context list still has an account column: %q", header)
	}
}

func TestContextListWithNothingSelectedSaysSo(t *testing.T) {
	shell, out, errOut := newShell(t)
	seeded := twoContextDocument()
	seeded.DefaultContext = ""
	installLogin(t, shell, seeded)
	if code := shell.Run([]string{"context", "list"}); code != exit.OK {
		t.Fatalf("exit code = %d; stderr: %s", code, errOut)
	}
	if !strings.Contains(out.String(), "No context is selected") {
		t.Errorf("the listing does not say nothing is selected:\n%s", out)
	}
}

func TestContextListOnAMachineWithNoDocumentSaysSoPlainly(t *testing.T) {
	shell, out, errOut := newShell(t)
	if code := shell.Run([]string{"context", "list"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.Contains(out.String(), "context apply") || !strings.Contains(out.String(), "context create") {
		t.Errorf("an empty listing does not name the commands that fill it:\n%s", out)
	}
}

func TestContextCurrentReportsTheSelectedContext(t *testing.T) {
	shell, out, errOut := newShell(t)
	installLogin(t, shell, oneContextDocument())
	if code := shell.Run([]string{"context", "current"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.Contains(out.String(), "https://idp.example") {
		t.Errorf("the report does not name the issuer the context logs in through:\n%s", out)
	}
}

func TestContextCurrentOnAMachineWithNoDocumentSaysSoPlainly(t *testing.T) {
	shell, out, errOut := newShell(t)
	if code := shell.Run([]string{"context", "current"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stdout: %s stderr: %s", code, exit.OK, out, errOut)
	}
	if errOut.Len() != 0 {
		t.Errorf("an unconfigured machine wrote to stderr:\n%s", errOut)
	}
	if !strings.Contains(out.String(), "wso2 context") {
		t.Errorf("the report does not name what to run next:\n%s", out)
	}
}

func TestContextCurrentReportsAnUnconfiguredMachineInBothRenderings(t *testing.T) {
	shell, out, errOut := newShell(t)
	if code := shell.Run([]string{"context", "current", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d; stderr: %s", code, errOut)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("the output is not JSON: %v\n%s", err, out)
	}
	if configured, ok := decoded["configured"].(bool); !ok || configured {
		t.Fatalf("configured = %v", decoded["configured"])
	}
}

func TestEveryContextSubcommandRendersJSON(t *testing.T) {
	for name, args := range map[string][]string{
		"create":         {"context", "create", "gamma", "--issuer", "https://i.example", "--client-id", "c", "--output", "json"},
		"use":            {"context", "use", "beta", "--output", "json"},
		"list":           {"context", "list", "--output", "json"},
		"current":        {"context", "current", "--output", "json"},
		"show":           {"context", "show", "--output", "json"},
		"rename":         {"context", "rename", "beta", "gamma", "--output", "json"},
		"delete":         {"context", "delete", "beta", "--output", "json"},
		"product add":    {"context", "product", "add", "reference", "--url", "https://r.example", "--output", "json"},
		"product remove": {"context", "product", "remove", "orders", "--output", "json"},
		"export":         {"context", "export"},
	} {
		t.Run(name, func(t *testing.T) {
			shell, out, errOut := newContextShell(t)
			seeded := twoContextDocument()
			acme := seeded.Contexts[0]
			acme.Products = map[string]contexts.Product{"reference": {Endpoint: "https://r.example"},
				"orders": {Endpoint: "https://o.example"}}
			acme.Login.Product = "reference"
			seeded = seeded.Put(acme)
			installLogin(t, shell, seeded)
			if name == "product add" {
				installLogin(t, shell, twoContextDocument())
			}
			if code := shell.Run(args); code != exit.OK {
				t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
			}
			var decoded map[string]any
			if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
				t.Fatalf("the output is not one JSON document: %v\n%s", err, out)
			}
			if _, published := decoded["schema"]; published {
				t.Errorf("the result publishes a schema key the rest of the shell suppresses:\n%s", out)
			}
		})
	}
}

// failingTransport fails the test if anything it is installed on dials.
type failingTransport struct {
	t      *testing.T
	family string
}

func (f failingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	f.t.Errorf("a wso2 %s subcommand made a request to %s", f.family, request.URL.Redacted())
	return nil, errNoNetwork
}

var errNoNetwork = errors.New("this test permits no network call")

// TestNoContextSubcommandOpensANetworkConnection is the D8 guard: an issuer
// typo has to surface at wso2 login, never at wso2 context create, which is
// what makes ADR 0011's claim checkable. Installing is the one network use a
// setup command makes, and every product here is installed already.
func TestNoContextSubcommandOpensANetworkConnection(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = failingTransport{t: t, family: "context"}

	invocations := map[string][]string{
		"create":                 {"context", "create", "gamma", "--login-product", "iam", "--url", thunderURL},
		"create, issuer":         {"context", "create", "delta", "--issuer", "https://i.example", "--client-id", "c"},
		"product add":            {"context", "product", "add", "api", "--url", apiURL, "--gateway", apiGatewayURL},
		"use":                    {"context", "use", "beta"},
		"list":                   {"context", "list"},
		"current":                {"context", "current"},
		"show":                   {"context", "show"},
		"export":                 {"context", "export"},
		"create, taken name":     {"context", "create", "acme", "--login-product", "iam", "--url", thunderURL},
		"create, illegal name":   {"context", "create", "Delta", "--issuer", "https://i.example", "--client-id", "c"},
		"use, unknown name":      {"context", "use", "nosuch"},
		"unsupported shell flag": {"--context", "acme", "context", "list"},
	}
	for name, args := range invocations {
		t.Run(name, func(t *testing.T) {
			shell, _, _ := newContextShell(t)
			installLogin(t, shell, twoContextDocument())
			shell.Run(args)
		})
	}
}

// TestTheNetworkGuardWouldNoticeARequest proves the guard is not vacuous.
func TestTheNetworkGuardWouldNoticeARequest(t *testing.T) {
	watched := &testing.T{}
	transport := failingTransport{t: watched, family: "context"}
	request, err := http.NewRequest(http.MethodGet, "https://idp.example/.well-known/openid-configuration", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if _, err := transport.RoundTrip(request); !errors.Is(err, errNoNetwork) {
		t.Errorf("RoundTrip returned %v, want the guard's own error", err)
	}
	if !watched.Failed() {
		t.Error("the guard did not fail the test it was watching, so it would miss a real request")
	}
}

// legacyDocumentJSON is a schema version 1 document, of the shape the
// architecture proof published and a user could still have on disk.
const legacyDocumentJSON = `{
  "schemaVersion": 1,
  "defaultContext": "legacy",
  "contexts": [
    {
      "name": "legacy",
      "organizationId": "acme",
      "endpoint": "https://api.example",
      "auth": {"method": "development-credential", "credentialVariable": "WSO2_DEV_CREDENTIAL"}
    }
  ]
}
`

// installLegacy writes a version 1 document into the shell's isolated state.
func installLegacy(t *testing.T, shell app.Shell) {
	t.Helper()
	path := contexts.Path(shell.StateRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(legacyDocumentJSON), 0o600); err != nil {
		t.Fatalf("seed a version 1 document: %v", err)
	}
}

func TestContextCreateOnAVersionOneDocumentExplainsWhatToDo(t *testing.T) {
	shell, _, errOut := newShell(t)
	installLegacy(t, shell)

	if code := shell.Run([]string{"context", "create", "acme", "--issuer", "https://i.example", "--client-id", "c"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want the usage class %d; stderr: %s", code, exit.Usage, errOut)
	}
	reported := errOut.String()
	for _, wanted := range []string{contexts.Path(shell.StateRoot), "version 1", "wso2 context list"} {
		if !strings.Contains(reported, wanted) {
			t.Errorf("the refusal does not mention %q:\n%s", wanted, reported)
		}
	}
	data, err := os.ReadFile(contexts.Path(shell.StateRoot))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != legacyDocumentJSON {
		t.Errorf("the refused create modified the user's document:\n%s", data)
	}
}

func TestTheContextFamilyRefusesTheContextFlag(t *testing.T) {
	shell, _, errOut := newShell(t)
	installLogin(t, shell, oneContextDocument())

	if code := shell.Run([]string{"--context", "acme", "context", "list"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want the usage class %d; stderr: %s", code, exit.Usage, errOut)
	}
	if !strings.Contains(errOut.String(), "shell.unsupported_flag") {
		t.Errorf("stderr does not carry shell.unsupported_flag:\n%s", errOut)
	}
}

func TestAWrongArgumentCountIsAUsageRefusal(t *testing.T) {
	for name, args := range map[string][]string{
		"create with no name":      {"context", "create"},
		"create with two names":    {"context", "create", "one", "two"},
		"use with no name":         {"context", "use"},
		"use with two names":       {"context", "use", "one", "two"},
		"list with an argument":    {"context", "list", "extra"},
		"current with an argument": {"context", "current", "extra"},
		"rename with one name":     {"context", "rename", "one"},
		"export with two names":    {"context", "export", "one", "two"},
	} {
		t.Run(name, func(t *testing.T) {
			shell, _, errOut := newShell(t)
			if code := shell.Run(args); code != exit.Usage {
				t.Fatalf("exit code = %d, want the usage class %d; stderr: %s", code, exit.Usage, errOut)
			}
			if !strings.Contains(errOut.String(), "Run `wso2 context") {
				t.Errorf("the refusal names no way back:\n%s", errOut)
			}
		})
	}
}

// contextShowReport mirrors what wso2 context show --output json publishes.
type contextShowReport struct {
	Path           string `json:"path"`
	Written        bool   `json:"written"`
	SchemaVersion  int    `json:"schemaVersion"`
	DefaultContext string `json:"defaultContext"`
	Contexts       []struct {
		contexts.Context
		CredentialSource string `json:"credentialSource"`
	} `json:"contexts"`
	Notes []string `json:"notes"`
}

func decodeContextShowReport(t *testing.T, rendered []byte) contextShowReport {
	t.Helper()
	var report contextShowReport
	if err := json.Unmarshal(rendered, &report); err != nil {
		t.Fatalf("the output is not one JSON document: %v\n%s", err, rendered)
	}
	return report
}

func TestContextShowReportsThePathEvenWithNothingWritten(t *testing.T) {
	shell, out, errOut := newShell(t)
	if code := shell.Run([]string{"context", "show", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	report := decodeContextShowReport(t, out.Bytes())
	if report.Path != contexts.Path(shell.StateRoot) || report.Written {
		t.Errorf("report = %+v", report)
	}
	if report.Contexts == nil || len(report.Contexts) != 0 {
		t.Errorf("contexts = %v, want an empty list rather than null", report.Contexts)
	}
	tableShell, tableOut, tableErrOut := newShell(t)
	if code := tableShell.Run([]string{"context", "show"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, tableErrOut)
	}
	if !strings.Contains(tableOut.String(), "No context document has been written yet") {
		t.Errorf("table rendering does not say that no document has been written:\n%s", tableOut)
	}
}

func TestContextShowReflectsWSO2Home(t *testing.T) {
	home := t.TempDir()
	t.Setenv(state.RootEnvVar, home)
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	shell := app.Shell{Streams: output.Streams{Out: out, Err: errOut}}
	if code := shell.Run([]string{"context", "show", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if report := decodeContextShowReport(t, out.Bytes()); report.Path != contexts.Path(home) {
		t.Errorf("path = %q, want %q", report.Path, contexts.Path(home))
	}
}

func TestContextShowRendersTheWholeDocumentAndNamesSourcesNeverSecrets(t *testing.T) {
	keyring.MockInit()
	shell, _, _ := newContextShell(t)
	localSetup(t, shell)
	mustRun(t, shell, "context", "create", "ci", "--login-product", "iam", "--url", thunderURL,
		"--client-id", "ci", "--client-secret-variable", "WSO2_CI_SECRET")
	const canary = "canary-refresh-token-2f8c-do-not-disclose"
	if err := (session.Store{StateRoot: shell.StateRoot}).Save("local",
		session.Session{Issuer: thunderURL, RefreshToken: canary}); err != nil {
		t.Fatal(err)
	}

	report := decodeContextShowReport(t, []byte(mustRun(t, shell, "context", "show", "--output", "json")))
	if report.SchemaVersion != contexts.SchemaVersion || report.DefaultContext != "local" || len(report.Contexts) != 2 {
		t.Fatalf("report = %+v", report)
	}
	sources := map[string]string{}
	for _, context := range report.Contexts {
		sources[context.Name] = context.CredentialSource
	}
	if sources["local"] != "secure store: local" || sources["ci"] != "env: WSO2_CI_SECRET" {
		t.Errorf("sources = %v", sources)
	}
	table := mustRun(t, shell, "context", "show")
	for _, want := range []string{"Products", "https://localhost:8090/mcp", "api/gateway", apiGatewayURL, "exchange",
		"secure store: local", "env: WSO2_CI_SECRET"} {
		if !strings.Contains(table, want) {
			t.Errorf("wso2 context show does not show %q:\n%s", want, table)
		}
	}
	for _, rendered := range []string{table} {
		if strings.Contains(rendered, canary) {
			t.Fatal("a stored refresh token reached wso2 context show")
		}
	}
}

func TestContextShowReportsAnUpgradeAndDrift(t *testing.T) {
	shell, _, _ := newContextShell(t)
	localSetup(t, shell)
	// Pretend an older version of iam was applied: the record asks for other
	// scopes than the installed descriptor would write now.
	document := loadDocument(t, shell)
	local := contextNamed(t, document, "local")
	iam := local.Products["iam"]
	iam.Scopes = []string{"legacy"}
	local.Products["iam"] = iam
	if err := contexts.Save(shell.StateRoot, document.Put(local)); err != nil {
		t.Fatal(err)
	}
	out := mustRun(t, shell, "context", "show")
	if !strings.Contains(out, `records the scopes "legacy"`) || !strings.Contains(out, "--replace --context local") {
		t.Errorf("wso2 context show does not flag the drift:\n%s", out)
	}
	_, doctorOut, _ := run(t, shell, "doctor", "--output", "json")
	if !strings.Contains(doctorOut, `"check": "defaults"`) || !strings.Contains(doctorOut, `"status": "differs"`) {
		t.Errorf("wso2 doctor does not flag the drift:\n%s", doctorOut)
	}
}

func TestContextShowKeepsAVersionOneDocumentsCredentialSource(t *testing.T) {
	shell, out, errOut := newShell(t)
	installLegacy(t, shell)
	if code := shell.Run([]string{"context", "show"}); code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out.String(), "WSO2_DEV_CREDENTIAL") {
		t.Errorf("the table drops the version 1 credential variable:\n%s", out)
	}
	out.Reset()
	if code := shell.Run([]string{"--output", "json", "context", "show"}); code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out.String(), "WSO2_DEV_CREDENTIAL") {
		t.Errorf("the JSON drops the version 1 credential variable:\n%s", out)
	}
}

func TestAnEarlierDocumentIsUpgradedOnceWithANoticeWhenSessionsMove(t *testing.T) {
	shell, _, _ := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	path := contexts.Path(shell.StateRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	shared := `{"schemaVersion": 3, "defaultContext": "alpha",
	  "accounts": [{"name": "demo", "type": "onprem",
	    "auth": {"kind": "oauth-browser", "issuer": "https://idp.example", "clientId": "cli", "credentialRef": "demo"}}],
	  "contexts": [{"name": "alpha", "account": "demo"}, {"name": "beta", "account": "demo"}]}`
	if err := os.WriteFile(path, []byte(shared), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, errOut := run(t, shell, "context", "list")
	if !strings.Contains(errOut, "schema version 4") || !strings.Contains(errOut, "wso2 login --context beta") {
		t.Errorf("the upgrade was not reported:\n%s", errOut)
	}
	_, _, errOut = run(t, shell, "context", "list")
	if strings.Contains(errOut, "Upgraded") {
		t.Errorf("the upgrade was reported twice:\n%s", errOut)
	}
	if !strings.Contains(string(mustReadFile(t, path)), `"schemaVersion": 4`) {
		t.Error("the document was not rewritten")
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// identityOnlyDocument is one browser context's login under the name
// acme-cloud, selecting nothing: the starting point tests copy into the
// contexts they need with acmeCloud.
func identityOnlyDocument() contexts.Document {
	return contexts.Document{
		SchemaVersion: contexts.SchemaVersion,
		Contexts:      []contexts.Context{acmeCloud("acme-cloud")},
	}
}

// acmeCloud is the acme-cloud login under the given name, optionally within an
// organization and a project. The acme context keeps its sessions under
// acme-cloud, the reference of the account it was folded from; any other name
// holds sessions of its own under its name.
func acmeCloud(name string, organizationAndProject ...string) contexts.Context {
	ref := name
	if name == "acme" {
		ref = "acme-cloud"
	}
	context := contexts.Context{Name: name, Type: "cloud", CredentialRef: ref,
		Login: contexts.Login{Kind: contexts.KindOAuthBrowser, Issuer: "https://idp.example", ClientID: "wso2-cli"}}
	if len(organizationAndProject) > 0 {
		context.Organization = organizationAndProject[0]
	}
	if len(organizationAndProject) > 1 {
		context.Project = organizationAndProject[1]
	}
	return context
}
