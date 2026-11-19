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
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
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
	cloudAPIMURL = "https://development-wso2cloud.gateway.dev.cloud.example/apip-platform-api-apip-platform-api-endpoint"
	cloudIAMURL  = "https://api-development.gateway.dev.cloud.example/iam/v1"
	cloudShared  = "urn:wso2:cloud"
)

var (
	cloudAPIMScopes = []string{"ap:api:manage", "ap:api:view"}
	cloudIAMScopes  = []string{"iam:system"}
)

// cloudLogin is a shell set to log in to the dev WSO2 Cloud environment,
// against an issuer that binds every authorization to one resource server,
// counting the times it opens the browser.
type cloudLogin struct {
	shell     app.Shell
	out       *bytes.Buffer
	errOut    *bytes.Buffer
	issuer    *fakeissuer.Issuer
	mutex     sync.Mutex
	opened    int
	authorize []url.Values
	// paths are the paths of the authorization URLs, in the order opened.
	paths []string
}

func newCloudLogin(t *testing.T) *cloudLogin {
	t.Helper()
	return newCloudLoginWith(t, fakeissuer.Options{})
}

// newCloudLoginWith is newCloudLogin against an issuer with further options.
func newCloudLoginWith(t *testing.T, opts fakeissuer.Options) *cloudLogin {
	t.Helper()
	keyring.MockInit()
	opts.Host, opts.RequireResource = "localhost", true
	// WSO2 Cloud's ThunderID issues a device secret on every sign-in.
	if !opts.ReusedBrowserSession {
		opts.DeviceSSO = true
	}
	issuer := fakeissuer.New(t, opts)
	shell, out, errOut := newLoginShell(t)
	login := &cloudLogin{shell: shell, out: out, errOut: errOut, issuer: issuer}
	login.shell.OpenBrowser = func(authURL string) error {
		login.mutex.Lock()
		login.opened++
		login.mutex.Unlock()
		if parsed, err := url.Parse(authURL); err == nil {
			login.mutex.Lock()
			login.authorize = append(login.authorize, parsed.Query())
			login.paths = append(login.paths, parsed.Path)
			login.mutex.Unlock()
		}
		go func() {
			if response, err := http.Get(authURL); err == nil {
				_ = response.Body.Close()
			}
		}()
		return nil
	}
	login.shell.Reader = failIfReadReader{t}
	t.Setenv(app.CloudEnvVar, "dev")
	writeCloudEnvironment(t, shell.StateRoot, "dev", cloudEnvironmentBody(issuer.URL))
	return login
}

func (c *cloudLogin) run(t *testing.T, args ...string) {
	t.Helper()
	c.errOut.Reset()
	if code := c.shell.Run(args); code != exit.OK {
		t.Fatalf("%v failed: exit %d, stderr %s", args, code, c.errOut)
	}
}

// authorizedScopes is the product scopes each authorization asked for, by the
// resource it named.
func (c *cloudLogin) authorizedScopes() map[string][]string {
	asked := map[string][]string{}
	for _, query := range c.issuer.Authorizations() {
		var scopes []string
		for _, scope := range strings.Fields(query.Get("scope")) {
			if !slices.Contains([]string{"openid", "offline_access", "profile", "email"}, scope) {
				scopes = append(scopes, scope)
			}
		}
		slices.Sort(scopes)
		asked[query.Get("resource")] = scopes
	}
	return asked
}

func installCloudAPIM(t *testing.T, shell app.Shell) {
	t.Helper()
	installFixture(t, shell, fixture.Module{Namespace: "apim", Version: "0.1.0",
		Product: &modules.ProductDescriptor{
			Audience: modules.AudienceResource, Grant: contexts.GrantExchange, Scopes: cloudAPIMScopes,
		}})
}

func installCloudIAM(t *testing.T, shell app.Shell) {
	t.Helper()
	installFixture(t, shell, fixture.Module{Namespace: "iam", Version: "0.1.0",
		Product: &modules.ProductDescriptor{
			Provider: contexts.ProviderThunder, Audience: modules.AudienceResource,
			Scopes: []string{"system", "iam:system"},
		}})
}

func cloudContext(t *testing.T, shell app.Shell) contexts.Context {
	t.Helper()
	found, ok := loadDocument(t, shell).Find("cloud-dev")
	if !ok {
		t.Fatal("no cloud-dev context")
	}
	return found
}

func TestCloudLoginRecordsEveryInstalledCloudProductAndAuthorizesEach(t *testing.T) {
	login := newCloudLogin(t)
	installCloudAPIM(t, login.shell)
	installCloudIAM(t, login.shell)

	login.run(t, "login")

	recorded := cloudContext(t, login.shell).Products
	apim, iam := recorded["apim"], recorded["iam"]
	if apim.Endpoint != cloudAPIMURL || apim.Audience != cloudShared || !slices.Equal(apim.Scopes, cloudAPIMScopes) {
		t.Errorf("apim = %+v, want the environment's apim URL, the shared resource and the descriptor's scopes", apim)
	}
	if apim.Grant != nil {
		t.Errorf("apim grant = %+v; a Cloud product is reached by its own authorization, never by exchange", apim.Grant)
	}
	if iam.Endpoint != cloudIAMURL || iam.Audience != cloudIAMURL || !slices.Equal(iam.Scopes, cloudIAMScopes) ||
		iam.Grant != nil {
		t.Errorf("iam = %+v, want its own URL as URL and audience and the descriptor's scopes", iam)
	}

	asked := login.authorizedScopes()
	if len(asked) != 1 || !slices.Equal(slices.DeleteFunc(asked[cloudShared], func(scope string) bool {
		return scope == "device_sso"
	}), cloudAPIMScopes) {
		t.Errorf("authorizations asked for %v, want the sign-in alone with apim's resource and scopes", asked)
	}
	if login.opened != 1 {
		t.Errorf("the browser was opened %d times, want once for both products", login.opened)
	}
	store := session.Store{StateRoot: login.shell.StateRoot}
	for ref, resource := range map[string]string{
		cloudContext(t, login.shell).CredentialRef:                                    cloudShared,
		contexts.ProductSessionRef(cloudContext(t, login.shell).CredentialRef, "iam"): cloudIAMURL,
	} {
		stored, err := store.Load(ref)
		if err != nil || stored.Resource != resource {
			t.Errorf("session %s = %+v, %v; want one bound to %s", ref, stored, err, resource)
		}
		if resource == cloudIAMURL && (!slices.Equal(stored.Scopes, cloudIAMScopes) ||
			stored.EstablishedBy != session.EstablishedByDeviceSecret) {
			t.Errorf("iam session scopes %v established by %q, want %v from the device secret",
				stored.Scopes, stored.EstablishedBy, cloudIAMScopes)
		}
	}

	login.run(t, "context", "show")
	for _, want := range []string{"apim", "iam", cloudAPIMURL, cloudIAMURL} {
		if !strings.Contains(login.out.String(), want) {
			t.Errorf("context show does not show %q:\n%s", want, login.out)
		}
	}
	login.out.Reset()
	login.run(t, "whoami", "--output", "json")
	strategies := map[string]string{}
	for _, product := range decodeWhoamiReport(t, login.out.Bytes()).Products {
		strategies[product.Namespace] = product.Strategy
	}
	if strategies["apim"] != contexts.StrategyDirect || strategies["iam"] != contexts.StrategySibling {
		t.Errorf("whoami reports %v, want apim direct and iam sibling", strategies)
	}
}

func TestCloudLoginRecordsAModuleInstalledAfterTheFirstLogin(t *testing.T) {
	login := newCloudLogin(t)
	installCloudAPIM(t, login.shell)
	login.run(t, "login")
	before := cloudContext(t, login.shell).Products["apim"]

	installCloudIAM(t, login.shell)
	login.run(t, "login")

	after := cloudContext(t, login.shell)
	if iam, found := after.Products["iam"]; !found || iam.Endpoint != cloudIAMURL || iam.Audience != cloudIAMURL {
		t.Fatalf("iam = %+v (recorded %v), want it recorded on the next login", iam, found)
	}
	if apim := after.Products["apim"]; apim.Endpoint != before.Endpoint || apim.Audience != before.Audience {
		t.Errorf("apim changed from %+v to %+v", before, apim)
	}
	if after.Login.Product != "apim" {
		t.Errorf("login product = %q; recording a product must not move the login to it", after.Login.Product)
	}
}

func TestCloudLoginLeavesAProductEditedByHandAlone(t *testing.T) {
	login := newCloudLogin(t)
	installCloudAPIM(t, login.shell)
	installCloudIAM(t, login.shell)
	login.run(t, "login")

	edited := contexts.Product{Endpoint: "https://apim.elsewhere.example", Audience: "https://apim.elsewhere.example",
		Scopes: []string{"ap:api:view"}}
	if err := contexts.Update(login.shell.StateRoot, func(document contexts.Document) (contexts.Document, error) {
		target, _ := document.Find("cloud-dev")
		products := map[string]contexts.Product{"apim": edited}
		target.Products = products
		return document.Put(target), nil
	}); err != nil {
		t.Fatal(err)
	}

	login.run(t, "login")

	recorded := cloudContext(t, login.shell).Products
	if apim := recorded["apim"]; apim.Endpoint != edited.Endpoint || !slices.Equal(apim.Scopes, edited.Scopes) {
		t.Errorf("apim = %+v, want the hand-edited record kept", apim)
	}
	if _, found := recorded["iam"]; !found {
		t.Error("iam, removed by the edit and still installed, was not recorded again")
	}
}

func TestCloudLoginRecordsNothingOutsideTheCloudProducts(t *testing.T) {
	login := newCloudLogin(t)
	installCloudAPIM(t, login.shell)
	installFixture(t, login.shell, fixture.Module{Namespace: "reference", Version: "0.1.0",
		Product: &modules.ProductDescriptor{Audience: modules.AudienceResource, Grant: contexts.GrantExchange,
			Scopes: []string{"reference:status:read"}}})

	login.run(t, "login")

	recorded := cloudContext(t, login.shell).Products
	if _, found := recorded["reference"]; found || len(recorded) != 1 {
		t.Errorf("products = %v, want apim alone", slices.Sorted(maps.Keys(recorded)))
	}
}

func TestCloudLoginWithoutTheEnvironmentFileRecordsNothingAndStillLogsIn(t *testing.T) {
	login := newCloudLogin(t)
	installCloudAPIM(t, login.shell)
	login.run(t, "login")
	if err := os.RemoveAll(filepath.Join(login.shell.StateRoot, "cloud")); err != nil {
		t.Fatal(err)
	}
	installCloudIAM(t, login.shell)

	login.run(t, "login")

	if _, found := cloudContext(t, login.shell).Products["iam"]; found {
		t.Error("iam was recorded with no environment file to derive it from")
	}
	if !strings.Contains(login.errOut.String(), "iam") ||
		!strings.Contains(login.errOut.String(), filepath.Join("cloud", "dev.yaml")) {
		t.Errorf("stderr does not say which product went unrecorded and which file would record it:\n%s",
			login.errOut)
	}
}

func TestOnlyACloudContextRecordsProductsAtLogin(t *testing.T) {
	login := newCloudLogin(t)
	t.Setenv(app.CloudEnvVar, "")
	installCloudAPIM(t, login.shell)

	plain := fakeissuer.New(t, fakeissuer.Options{Host: "localhost"})
	login.run(t, "login", "--url", plain.URL, "--client-id", "wso2-cli", "--context", "local")
	login.run(t, "login", "--context", "local")

	local, _ := loadDocument(t, login.shell).Find("local")
	if len(local.Products) != 0 {
		t.Errorf("a login to a context that is not a Cloud one recorded %v", local.Products)
	}
}

// A product recorded by hand on a Cloud context is reached the Cloud way too,
// whatever grant its descriptor names, while the same product on another
// context keeps the descriptor's grant.
func TestTheResolverRecordsACloudProductDirect(t *testing.T) {
	login := newCloudLogin(t)
	installCloudAPIM(t, login.shell)
	installCloudIAM(t, login.shell)
	installFixture(t, login.shell, fixture.Module{Namespace: "reference", Version: "0.1.0",
		Product: &modules.ProductDescriptor{Audience: modules.AudienceResource, Grant: contexts.GrantExchange,
			Scopes: []string{"reference:status:read"}}})
	login.run(t, "login")

	login.run(t, "context", "product", "add", "apim", "--replace", "--url", "https://apim.elsewhere.example",
		"--context", "cloud-dev")
	login.run(t, "context", "product", "add", "reference", "--url", "https://reference.cloud.example",
		"--context", "cloud-dev")
	recorded := cloudContext(t, login.shell).Products
	if apim := recorded["apim"]; apim.Grant != nil || apim.Audience != cloudShared {
		t.Errorf("apim = %+v on a Cloud context, want no grant and the Cloud resource server", apim)
	}
	if reference := recorded["reference"]; reference.Grant != nil || reference.Audience != "https://reference.cloud.example" {
		t.Errorf("reference = %+v on a Cloud context, want no grant and its own URL as audience", reference)
	}

	login.run(t, "context", "create", "local", "--login-product", "iam", "--url", "http://localhost:8501",
		"--client-id", "wso2-cli", "--audience", "https://local.example/iam")
	login.run(t, "context", "product", "add", "apim", "--url", "https://apim.local.example", "--context", "local")
	local, _ := loadDocument(t, login.shell).Find("local")
	if apim := local.Products["apim"]; apim.Grant == nil || apim.Grant.Kind != contexts.GrantExchange {
		t.Errorf("apim = %+v on a context that is not a Cloud one, want the descriptor's exchange grant", apim)
	}
}

// Only the descriptor's grant is set aside on a Cloud context: a grant a
// person states is recorded as stated, as on any other context.
func TestACloudContextKeepsAStatedGrant(t *testing.T) {
	login := newCloudLogin(t)
	installCloudAPIM(t, login.shell)
	login.run(t, "login")

	file := filepath.Join(t.TempDir(), "cloud.yaml")
	body := "contexts:\n  - name: cloud-dev\n    type: cloud\n    login:\n      issuer: " + login.issuer.URL +
		"\n      clientId: WSO2_CLI\n      provider: thunder\n    products:\n      apim:\n        url: " + cloudAPIMURL +
		"\n        grant:\n          kind: exchange\n"
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	login.run(t, "context", "apply", "-f", file)

	apim := cloudContext(t, login.shell).Products["apim"]
	if apim.Grant == nil || apim.Grant.Kind != contexts.GrantExchange || apim.Audience != cloudShared {
		t.Errorf("apim = %+v, want the stated exchange grant under the Cloud resource server", apim)
	}
}

func TestAFirstCloudLoginSkipsAProductTheCatalogCannotSupply(t *testing.T) {
	login := newCloudLogin(t)
	installCloudAPIM(t, login.shell)

	login.run(t, "login")

	if strings.Contains(login.errOut.String(), "Skipped") || strings.Contains(login.errOut.String(), "catalog") {
		t.Errorf("an unpublished product was reported without --verbose:\n%s", login.errOut)
	}
	recorded := cloudContext(t, login.shell).Products
	if _, found := recorded["apim"]; !found || len(recorded) != 1 {
		t.Errorf("products = %v, want apim alone", slices.Sorted(maps.Keys(recorded)))
	}
}

func TestEveryCloudLoginInstallsTheCloudProductsItLacks(t *testing.T) {
	login := newCloudLogin(t)
	installCloudAPIM(t, login.shell)
	login.run(t, "login")

	login.run(t, "login", "--verbose")

	assertTriedToInstall(t, login.errOut.String(), "am", "iam", "intg")
	if strings.Contains(login.errOut.String(), "namespace=apim") {
		t.Error("an installed product was installed again")
	}
}

// assertTriedToInstall checks a --verbose login's diagnostics name each
// namespace as one the catalog does not publish.
func assertTriedToInstall(t *testing.T, stderr string, namespaces ...string) {
	t.Helper()
	for _, namespace := range namespaces {
		line := regexp.MustCompile(`(?m)^.*not published in the module catalog.*namespace=` +
			regexp.QuoteMeta(namespace) + `( |$)`)
		if !line.MatchString(stderr) {
			t.Errorf("the login did not try to install %s:\n%s", namespace, stderr)
		}
	}
}

func TestACloudContextRecordsItsEnvironment(t *testing.T) {
	login := newCloudLogin(t)
	installCloudAPIM(t, login.shell)
	login.run(t, "login")

	if got := cloudContext(t, login.shell).CloudEnvironment; got != "dev" {
		t.Errorf("cloudEnvironment = %q, want dev", got)
	}
}

func TestAnyLoginOfACloudContextRecordsItsProducts(t *testing.T) {
	for name, testCase := range map[string]struct {
		args        []string
		environment string
	}{
		"named by --context":    {[]string{"login", "--context", "cloud-dev"}, ""},
		"named by WSO2_CONTEXT": {[]string{"login"}, "cloud-dev"},
	} {
		t.Run(name, func(t *testing.T) {
			login := newCloudLogin(t)
			installCloudAPIM(t, login.shell)
			login.run(t, "login")
			t.Setenv(app.CloudEnvVar, "")
			t.Setenv("WSO2_CONTEXT", testCase.environment)
			args := append(testCase.args, "--verbose")
			installCloudIAM(t, login.shell)

			login.run(t, args...)

			if _, found := cloudContext(t, login.shell).Products["iam"]; !found {
				t.Error("iam, installed after the first login, was not recorded")
			}
			assertTriedToInstall(t, login.errOut.String(), "am")
		})
	}
}

func TestACloudContextWithNoStoredEnvironmentIsLoggedInToAsBefore(t *testing.T) {
	login := newCloudLogin(t)
	installCloudAPIM(t, login.shell)
	login.run(t, "login")
	if err := contexts.Update(login.shell.StateRoot, func(document contexts.Document) (contexts.Document, error) {
		target, _ := document.Find("cloud-dev")
		target.CloudEnvironment = ""
		return document.Put(target), nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv(app.CloudEnvVar, "")
	installCloudIAM(t, login.shell)

	login.run(t, "login", "--context", "cloud-dev", "--verbose")

	if _, found := cloudContext(t, login.shell).Products["iam"]; found {
		t.Error("a Cloud context with no stored environment recorded a product")
	}
	if strings.Contains(login.errOut.String(), "module catalog") || strings.Contains(login.errOut.String(), "Not recorded") {
		t.Errorf("the login said something about products:\n%s", login.errOut)
	}
}

// The preview iam module declares system beside iam:system; on WSO2 Cloud only
// iam:system is a permission, so the Cloud table supplies it.
func TestACloudIAMRecordTakesTheCloudScope(t *testing.T) {
	login := newCloudLogin(t)
	installCloudIAM(t, login.shell)

	login.run(t, "login")

	if got := cloudContext(t, login.shell).Products["iam"].Scopes; !slices.Equal(got, []string{"iam:system"}) {
		t.Errorf("iam scopes = %v, want [iam:system]", got)
	}
}

// What a Cloud login records — the table's scopes, no descriptor grant — is
// what the installed product would write on that context, so neither doctor
// nor context show calls it drift.
func TestACloudLoginsRecordsDoNotDrift(t *testing.T) {
	login := newCloudLogin(t)
	installCloudIAM(t, login.shell)
	installCloudAPIM(t, login.shell)
	login.run(t, "login")
	if err := contexts.Update(login.shell.StateRoot, func(document contexts.Document) (contexts.Document, error) {
		target, _ := document.Find("cloud-dev")
		target.Login.Product = "iam"
		return document.Put(target), nil
	}); err != nil {
		t.Fatal(err)
	}

	login.errOut.Reset()
	login.out.Reset()
	login.shell.Run([]string{"doctor", "--output", "json"})
	if strings.Contains(login.out.String(), `"status": "differs"`) {
		t.Errorf("doctor reports drift on a Cloud login's own records:\n%s", login.out)
	}
	login.out.Reset()
	login.run(t, "context", "show")
	if strings.Contains(login.out.String(), "records the") {
		t.Errorf("context show reports drift on a Cloud login's own records:\n%s", login.out)
	}
}

// Everything a Cloud login says about installing, recording and authorizing
// products is on standard error, so standard output under -o json is the one
// document the command's result is.
func TestACloudLoginUnderJSONWritesOneDocument(t *testing.T) {
	login := newCloudLogin(t)
	installCloudAPIM(t, login.shell)
	installCloudIAM(t, login.shell)

	login.run(t, "login", "--output", "json", "--verbose")

	var document map[string]any
	decoder := json.NewDecoder(login.out)
	if err := decoder.Decode(&document); err != nil {
		t.Fatalf("standard output is not a JSON document: %v\n%s", err, login.out)
	}
	if decoder.More() {
		t.Errorf("standard output holds more than one document")
	}
	if !strings.Contains(login.errOut.String(), `Authorized the "iam" product from this sign-in, without the browser.`) {
		t.Errorf("the progress line is not on standard error:\n%s", login.errOut)
	}
}

func TestCloudLoginWithDeviceRecordsADeviceCodeContext(t *testing.T) {
	login := newCloudLoginWith(t, fakeissuer.Options{DeviceInterval: 1})
	installCloudAPIM(t, login.shell)
	installCloudIAM(t, login.shell)

	login.run(t, "login", "--device")

	if kind := cloudContext(t, login.shell).Login.Kind; kind != contexts.KindOAuthDevice {
		t.Errorf("the Cloud context signs in with %q, want %q", kind, contexts.KindOAuthDevice)
	}
	if login.opened != 0 {
		t.Errorf("a device-code login opened the browser %d times", login.opened)
	}
	if got := len(login.issuer.DeviceAuthorizations()); got != 1 {
		t.Errorf("%d device authorizations, want the sign-in alone, every product from its secret", got)
	}
	stored, err := session.Store{StateRoot: login.shell.StateRoot}.Load(
		contexts.ProductSessionRef(cloudContext(t, login.shell).CredentialRef, "iam"))
	if err != nil || stored.EstablishedBy != session.EstablishedByDeviceSecret {
		t.Errorf("iam session %+v, %v; want it from the device secret", stored.EstablishedBy, err)
	}
}

func TestCloudLoginWithDeviceOnABrowserContextChangesNothing(t *testing.T) {
	login := newCloudLogin(t)
	installCloudAPIM(t, login.shell)
	login.run(t, "login")
	login.run(t, "context", "create", "other", "--issuer", login.issuer.URL, "--client-id", "x", "--use")
	if got := loadDocument(t, login.shell).DefaultContext; got != "other" {
		t.Fatalf("setup: default context %q", got)
	}

	if code := login.shell.Run([]string{"login", "--device"}); code != exit.Usage {
		t.Fatalf("exit %d, want %d; stderr %s", code, exit.Usage, login.errOut)
	}
	if got := loadDocument(t, login.shell).DefaultContext; got != "other" {
		t.Errorf("a refused login selected %q as the current context", got)
	}
}
