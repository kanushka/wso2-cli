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
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/modules/fixture"
)

const (
	gatewayURL      = "https://localhost:8243"
	gatewayAudience = "http://localhost:18090/hello"
)

// gatewayDoc is a resource-bound identity whose apim product carries both
// records: the management one, federated at API Manager's own issuer, and a
// gateway one at the login provider for the API's own resource server.
func gatewayDoc(loginIssuer, productIssuer string) contexts.Document {
	document := browserDoc(loginIssuer)
	document.Identities[0].Auth.Provider = contexts.ProviderThunder
	document.Identities[0].LoginProduct = "iam"
	document.Identities[0].Products = map[string]contexts.Product{
		"iam": {Endpoint: loginIssuer, Audience: "https://localhost:8090/mcp", Scopes: []string{"system"}},
		"apim": {Endpoint: productIssuer, Audience: "apim-cli", Scopes: []string{"apim:api_view"},
			Grant: &contexts.Grant{Kind: contexts.GrantFederated, Issuer: productIssuer, ClientID: "apim-cli"},
			Gateway: &contexts.Gateway{Endpoint: gatewayURL, Audience: gatewayAudience,
				Scopes: []string{"hello:read", "orders:read"}}},
	}
	return document
}

func TestADocumentCarryingAGatewayRecordDecodesAndOneWithoutAnAudienceIsRefused(t *testing.T) {
	shell, _, errOut := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	installLogin(t, shell, gatewayDoc("http://login.example", "http://apim.example"))
	if code := shell.Run([]string{"identity", "list"}); code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	// The same document, edited by hand to drop the gateway's audience, on a
	// provider that binds every session to one resource server.
	data, err := os.ReadFile(filepath.Join(shell.StateRoot, "cli", contexts.FileName))
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(data), `"audience": "`+gatewayAudience+`"`, `"audience": ""`, 1)
	if edited == string(data) {
		t.Fatalf("the audience was not found in:\n%s", data)
	}
	if err := os.WriteFile(filepath.Join(shell.StateRoot, "cli", contexts.FileName), []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	errOut.Reset()
	if code := shell.Run([]string{"identity", "list"}); code != exit.Usage ||
		!strings.Contains(errOut.String(), "contexts.document_malformed") ||
		!strings.Contains(errOut.String(), "gateway") {
		t.Fatalf("exit %d, stderr:\n%s", code, errOut)
	}
}

func TestDoctorCountsTheGatewaySessionBesideTheManagementOne(t *testing.T) {
	keyring.MockInit()
	shell, out, _ := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	installLogin(t, shell, gatewayDoc("http://login.example", "http://apim.example"))
	store := session.Store{StateRoot: shell.StateRoot}
	for _, ref := range []string{credentialRef, contexts.ProductSessionRef(credentialRef, "apim")} {
		if err := store.Save(ref, session.Session{Issuer: "http://login.example", RefreshToken: "rt"}); err != nil {
			t.Fatal(err)
		}
	}
	if code := shell.Run([]string{"doctor", "--output", "json"}); code == exit.OK {
		t.Fatal("doctor passed with the gateway session missing")
	}
	finding := decodeDoctorReport(t, out.Bytes()).findingFor(t, "session")
	if finding.Status != "fail" || !strings.Contains(finding.Detail, "apim/gateway") ||
		strings.Contains(finding.Detail, "iam") {
		t.Fatalf("finding %+v", finding)
	}
	// With the gateway session stored the check passes: every session the
	// identity needs exists, the gateway's among them.
	if err := store.Save(contexts.ProductSessionRef(credentialRef, contexts.GatewayKey("apim")),
		session.Session{Issuer: "http://login.example", RefreshToken: "rt"}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := shell.Run([]string{"doctor", "--output", "json"}); code != exit.OK {
		t.Fatalf("doctor failed with every session stored:\n%s", out)
	}
}

// hasField reports whether a rendered report carries the label with exactly
// that value, however wide the label column is.
func hasField(out, label, value string) bool {
	pattern := `(?m)^` + regexp.QuoteMeta(label) + `\s+` + regexp.QuoteMeta(value) + `$`
	return regexp.MustCompile(pattern).MatchString(out)
}

// connectBoth records the login provider and the apim management product,
// the state a developer is in before recording the gateway.
func connectBoth(t *testing.T, shell app.Shell) {
	t.Helper()
	if code, _, errOut := connect(t, shell, "iam", "connect", thunderURL); code != exit.OK {
		t.Fatalf("iam connect: exit %d: %s", code, errOut)
	}
	if code, _, errOut := connect(t, shell, "apim", "connect", apimURL, "--client-id", apimClient); code != exit.OK {
		t.Fatalf("apim connect: exit %d: %s", code, errOut)
	}
}

func TestConnectGatewayRecordsTheGatewayOnTheProductAndReportsItsStrategy(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	connectBoth(t, shell)
	code, out, errOut := connect(t, shell, "apim", "connect", gatewayURL+"/", "--gateway",
		"--audience", gatewayAudience, "--scopes", "hello:read,orders:read")
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	identity := loadDocument(t, shell).Identities[0]
	product := identity.Products["apim"]
	if product.Gateway == nil || product.Gateway.Endpoint != gatewayURL || product.Gateway.Audience != gatewayAudience ||
		!slices.Equal(product.Gateway.Scopes, []string{"hello:read", "orders:read"}) {
		t.Fatalf("gateway = %+v", product.Gateway)
	}
	// The management record is untouched, and so is the login pin.
	if product.Endpoint != apimURL || product.Grant == nil || identity.LoginProduct != "iam" {
		t.Errorf("the management record changed: %+v, login %q", product, identity.LoginProduct)
	}
	for label, value := range map[string]string{"Record": "gateway", "Endpoint": gatewayURL,
		"Audience": gatewayAudience, "Scopes": "hello:read,orders:read", "Strategy": "sibling",
		"Next": "Run wso2 login."} {
		if !hasField(out, label, value) {
			t.Errorf("the report lacks %s %s:\n%s", label, value, out)
		}
	}
	// A second gateway on the product is refused without --replace, and
	// replaces the record with it.
	code, _, errOut = connect(t, shell, "apim", "connect", gatewayURL, "--gateway", "--audience", gatewayAudience)
	if code != exit.Usage || !strings.Contains(errOut, "contexts.product_exists") {
		t.Fatalf("without --replace: exit %d, stderr:\n%s", code, errOut)
	}
	code, out, errOut = connect(t, shell, "apim", "connect", "https://other.example:8243", "--gateway",
		"--audience", "http://localhost:18090/orders", "--replace")
	if code != exit.OK {
		t.Fatalf("with --replace: exit %d: %s", code, errOut)
	}
	replaced := loadDocument(t, shell).Identities[0].Products["apim"].Gateway
	if replaced.Endpoint != "https://other.example:8243" || replaced.Audience != "http://localhost:18090/orders" ||
		len(replaced.Scopes) != 0 || !strings.Contains(out, "Replaced") {
		t.Fatalf("gateway = %+v\n%s", replaced, out)
	}
}

func TestConnectGatewayNamesLoginOnlyWhenTheIdentityAlreadyHoldsItsSessions(t *testing.T) {
	keyring.MockInit()
	shell, _, _ := newConnectShell(t)
	connectBoth(t, shell)
	store := session.Store{StateRoot: shell.StateRoot}
	if err := store.Save("thunder", session.Session{Issuer: thunderURL, RefreshToken: "rt"}); err != nil {
		t.Fatal(err)
	}
	// The login session alone is not "the other sessions": the product's
	// own session must be stored too before --only narrows the login.
	code, out, errOut := connect(t, shell, "apim", "connect", gatewayURL, "--gateway", "--audience", gatewayAudience)
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !hasField(out, "Next", "Run wso2 login.") {
		t.Errorf("with only the login session the next line should be the whole login:\n%s", out)
	}
	if err := store.Save(contexts.ProductSessionRef("thunder", "apim"),
		session.Session{Issuer: apimURL + "/oauth2/token", RefreshToken: "rt"}); err != nil {
		t.Fatal(err)
	}
	code, out, errOut = connect(t, shell, "apim", "connect", gatewayURL, "--gateway", "--audience", gatewayAudience, "--replace")
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !hasField(out, "Next", "Run wso2 login --only apim.") {
		t.Errorf("the next line does not narrow the login to the product:\n%s", out)
	}
}

func TestConnectGatewayNeedsTheManagementRecordFirst(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	// No identity at all, then an identity without the apim product: both
	// are the same refusal, naming the management connect to run first.
	for _, step := range []string{"none", "iam"} {
		if step == "iam" {
			connect(t, shell, "iam", "connect", thunderURL)
		}
		code, _, errOut := connect(t, shell, "apim", "connect", gatewayURL, "--gateway", "--audience", gatewayAudience)
		if code != exit.Usage || !strings.Contains(errOut, "shell.product_required") ||
			!strings.Contains(errOut, "wso2 apim connect <management-url>") {
			t.Fatalf("%s: exit %d, stderr:\n%s", step, code, errOut)
		}
	}
	if identity := loadDocument(t, shell).Identities[0]; identity.Products["apim"].Gateway != nil {
		t.Errorf("a gateway was written: %+v", identity.Products)
	}
}

func TestConnectGatewayIsRefusedForADescriptorWithoutAGatewayBlock(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	connect(t, shell, "iam", "connect", thunderURL)
	code, _, errOut := connect(t, shell, "iam", "connect", gatewayURL, "--gateway", "--audience", gatewayAudience)
	if code != exit.Usage || !strings.Contains(errOut, "shell.connect_unsupported") ||
		!strings.Contains(errOut, "iam") {
		t.Fatalf("exit %d, stderr:\n%s", code, errOut)
	}
}

func TestConnectGatewayRefusesAClientAndACredentialAndNeedsAnAudience(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	connectBoth(t, shell)
	for name, args := range map[string][]string{
		"a client id":          {"--client-id", apimClient},
		"a secret variable":    {"--client-secret-variable", "WSO2_APIM_CLIENT_SECRET"},
		"a client id variable": {"--client-id-variable", "WSO2_APIM_CLIENT_ID", "--client-secret-variable", "WSO2_APIM_CLIENT_SECRET"},
	} {
		line := append([]string{"apim", "connect", gatewayURL, "--gateway", "--audience", gatewayAudience}, args...)
		code, _, errOut := connect(t, shell, line...)
		if code != exit.Usage || !strings.Contains(errOut, "shell.conflicting_arguments") {
			t.Fatalf("%s: exit %d, stderr:\n%s", name, code, errOut)
		}
	}
	code, _, errOut := connect(t, shell, "apim", "connect", gatewayURL, "--gateway")
	if code != exit.Usage || !strings.Contains(errOut, "shell.missing_required_flag") ||
		!strings.Contains(errOut, "--audience") {
		t.Fatalf("no audience: exit %d, stderr:\n%s", code, errOut)
	}
}

func TestConnectGatewayOnAMachineIdentityFollowsTheDescriptorsMachineList(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	// closed carries a gateway a machine client may not reach inline.
	installFixture(t, shell, fixture.Module{Namespace: "closed", Version: "0.1.0",
		AuthAudiences: []string{"closed", "closed-gateway"}, AuthScopes: []string{"closed:read"},
		Product: &modules.ProductDescriptor{
			IssuerPath: "/oauth2/token", Audience: modules.AudienceClient,
			Scopes: []string{"closed:read"}, Grant: contexts.GrantFederated,
			Machine: []string{modules.MachineCredential},
			Gateway: &modules.GatewayDescriptor{Audience: modules.AudienceResource},
		}})
	if code, _, errOut := connect(t, shell, "iam", "connect", thunderURL,
		"--client-id", "wso2-cli-ci", "--client-secret-variable", "WSO2_CI_CLIENT_SECRET"); code != exit.OK {
		t.Fatalf("iam connect: exit %d: %s", code, errOut)
	}
	for _, namespace := range []string{"apim", "closed"} {
		if code, _, errOut := connect(t, shell, namespace, "connect", apimURL, "--client-id", apimClient,
			"--client-secret-variable", "WSO2_APIM_CLIENT_SECRET"); code != exit.OK {
			t.Fatalf("%s connect: exit %d: %s", namespace, code, errOut)
		}
	}
	code, out, errOut := connect(t, shell, "apim", "connect", gatewayURL, "--gateway", "--audience", gatewayAudience)
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !hasField(out, "Strategy", "inline") || !hasField(out, "Next", "Run wso2 apim status --context thunder.") {
		t.Errorf("report:\n%s", out)
	}
	code, _, errOut = connect(t, shell, "closed", "connect", gatewayURL, "--gateway", "--audience", gatewayAudience)
	if code != exit.AuthPolicy || !strings.Contains(errOut, "auth.product_not_configured") {
		t.Fatalf("closed: exit %d, stderr:\n%s", code, errOut)
	}
}

func TestLoginEstablishesTheGatewaySessionBesideTheOthers(t *testing.T) {
	keyring.MockInit()
	login := fakeissuer.New(t, fakeissuer.Options{RequireResource: true})
	product := fakeissuer.New(t, fakeissuer.Options{Audience: "apim-cli"})
	shell, out, errOut := newLoginShell(t)
	installLogin(t, shell, gatewayDoc(login.URL, product.URL))
	followBrowser(&shell)
	if code := shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	store := session.Store{StateRoot: shell.StateRoot}
	gateway, err := store.Load(contexts.ProductSessionRef(credentialRef, contexts.GatewayKey("apim")))
	if err != nil || gateway.Strategy != contexts.StrategySibling || gateway.Issuer != login.URL ||
		!slices.Equal(gateway.Scopes, []string{"hello:read", "orders:read"}) {
		t.Fatalf("gateway session %+v, %v", gateway, err)
	}
	if _, err := store.Load(contexts.ProductSessionRef(credentialRef, "apim")); err != nil {
		t.Fatalf("the management session was not established: %v", err)
	}
	if !hasField(out.String(), "apim/gateway", "sibling, established") {
		t.Fatalf("the report does not list the gateway session:\n%s", out)
	}
	if got := strings.Count(errOut.String(), "/authorize?"); got != 3 {
		t.Fatalf("printed %d authorization URLs, want 3:\n%s", got, errOut)
	}
	if !strings.Contains(errOut.String(), "resource="+url.QueryEscape(gatewayAudience)) {
		t.Fatalf("the gateway authorization carried no resource indicator:\n%s", errOut)
	}
}

func TestLoginOnlySelectsBothRecordsOfAProductOrTheGatewayAlone(t *testing.T) {
	keyring.MockInit()
	login := fakeissuer.New(t, fakeissuer.Options{RequireResource: true})
	product := fakeissuer.New(t, fakeissuer.Options{Audience: "apim-cli"})
	for only, wantKeys := range map[string][]string{
		"apim":         {"apim", "apim/gateway"},
		"apim/gateway": {"apim/gateway"},
	} {
		shell, _, errOut := newLoginShell(t)
		installLogin(t, shell, gatewayDoc(login.URL, product.URL))
		followBrowser(&shell)
		if code := shell.Run([]string{"login", "--only", only}); code != exit.OK {
			t.Fatalf("--only %s: exit %d, stderr %s", only, code, errOut)
		}
		store := session.Store{StateRoot: shell.StateRoot}
		for _, key := range []string{"apim", "apim/gateway"} {
			_, err := store.Load(contexts.ProductSessionRef(credentialRef, key))
			if stored := err == nil; stored != slices.Contains(wantKeys, key) {
				t.Errorf("--only %s: %s session stored = %v", only, key, stored)
			}
		}
		if _, err := store.Load(credentialRef); err == nil {
			t.Errorf("--only %s established the login session too", only)
		}
	}
}

func TestLoginNoProductsLeavesTheGatewayUnestablished(t *testing.T) {
	keyring.MockInit()
	login := fakeissuer.New(t, fakeissuer.Options{RequireResource: true})
	shell, _, errOut := newLoginShell(t)
	installLogin(t, shell, gatewayDoc(login.URL, login.URL))
	followBrowser(&shell)
	if code := shell.Run([]string{"login", "--no-products"}); code != exit.OK {
		t.Fatalf("exit %d, stderr %s", code, errOut)
	}
	store := session.Store{StateRoot: shell.StateRoot}
	if _, err := store.Load(contexts.ProductSessionRef(credentialRef, contexts.GatewayKey("apim"))); err == nil {
		t.Fatal("--no-products established the gateway session")
	}
}

func TestLoginNamesTheGatewayKeyWhenItFailsMidLogin(t *testing.T) {
	keyring.MockInit()
	// The login provider registers the iam resource server alone, so the
	// gateway's authorization, the last one, is refused.
	login := fakeissuer.New(t, fakeissuer.Options{RequireResource: true,
		RegisteredResource: "https://localhost:8090/mcp"})
	product := fakeissuer.New(t, fakeissuer.Options{Audience: "apim-cli"})
	shell, _, errOut := newLoginShell(t)
	installLogin(t, shell, gatewayDoc(login.URL, product.URL))
	followBrowser(&shell)
	if code := shell.Run([]string{"login"}); code == exit.OK {
		t.Fatalf("login succeeded despite the gateway resource being unregistered; stderr %s", errOut)
	}
	if !strings.Contains(errOut.String(), "Established: iam, apim. Not established: apim/gateway.") ||
		!strings.Contains(errOut.String(), "wso2 login --only apim/gateway") {
		t.Fatalf("stderr does not name the gateway key:\n%s", errOut)
	}
}

func TestWhoamiShowsTheGatewayRecordBesideTheManagementOne(t *testing.T) {
	keyring.MockInit()
	shell, out, errOut := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	installLogin(t, shell, gatewayDoc("http://login.example", "http://apim.example"))
	store := session.Store{StateRoot: shell.StateRoot}
	if err := store.Save(credentialRef, session.Session{Issuer: "http://login.example", RefreshToken: "rt", Subject: "user-1"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(contexts.ProductSessionRef(credentialRef, contexts.GatewayKey("apim")),
		session.Session{Issuer: "http://login.example", RefreshToken: "rt2", Strategy: contexts.StrategySibling}); err != nil {
		t.Fatal(err)
	}
	if code := shell.Run([]string{"whoami"}); code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, want := range []string{"iam: direct, present", "apim: federated, none", "apim/gateway: sibling, present"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	out.Reset()
	if code := shell.Run([]string{"whoami", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	report := decodeWhoamiReport(t, out.Bytes())
	if len(report.Products) != 3 || report.Products[1].Namespace != "apim/gateway" ||
		report.Products[1].Strategy != contexts.StrategySibling || report.Products[1].Session != "present" {
		t.Fatalf("products %+v", report.Products)
	}
	// A client-credentials identity reaches the gateway inline, like every
	// other record.
	machine := gatewayDoc("http://login.example", "http://apim.example")
	machine.Identities[0].Auth.Kind = contexts.KindClientCredentials
	machine.Identities[0].Auth.CredentialRef = ""
	machine.Identities[0].Auth.ClientSecretVariable = "WSO2_CI_CLIENT_SECRET"
	machine.Identities[0].LoginProduct = ""
	installLogin(t, shell, machine)
	out.Reset()
	if code := shell.Run([]string{"whoami"}); code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out.String(), "apim/gateway: inline, inline") {
		t.Fatalf("report:\n%s", out)
	}
}

func TestLogoutEndsTheGatewaySession(t *testing.T) {
	keyring.MockInit()
	login := fakeissuer.New(t, fakeissuer.Options{RequireResource: true})
	product := fakeissuer.New(t, fakeissuer.Options{Audience: "apim-cli"})
	shell, out, errOut := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	t.Setenv("WSO2_NO_INPUT", "1")
	installLogin(t, shell, gatewayDoc(login.URL, product.URL))
	store := session.Store{StateRoot: shell.StateRoot}
	gatewayRef := contexts.ProductSessionRef(credentialRef, contexts.GatewayKey("apim"))
	for ref, issuer := range map[string]string{
		credentialRef: login.URL,
		contexts.ProductSessionRef(credentialRef, "apim"): product.URL,
		gatewayRef: login.URL,
	} {
		if err := store.Save(ref, session.Session{Issuer: issuer, RefreshToken: "rt-" + ref}); err != nil {
			t.Fatal(err)
		}
	}
	if code := shell.Run([]string{"logout"}); code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if _, err := store.Load(gatewayRef); err == nil {
		t.Fatal("the gateway session survived logout")
	}
	if !strings.Contains(out.String(), "apim/gateway ended") || !strings.Contains(out.String(), "apim ended") {
		t.Fatalf("report:\n%s", out)
	}
}

func TestIdentityListShowsTheGatewayRecordUnderTheProduct(t *testing.T) {
	shell, out, errOut := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	installLogin(t, shell, gatewayDoc("http://login.example", "http://apim.example"))
	if code := shell.Run([]string{"identity", "list"}); code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !regexp.MustCompile(`apim/gateway\s+` + regexp.QuoteMeta(gatewayURL) + `\s+hello:read,orders:read`).MatchString(out.String()) {
		t.Fatalf("no gateway row:\n%s", out)
	}
	out.Reset()
	if code := shell.Run([]string{"identity", "list", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out.String(), `"gateway": {`) || !strings.Contains(out.String(), `"endpoint": "`+gatewayURL+`"`) {
		t.Fatalf("json:\n%s", out)
	}
}
