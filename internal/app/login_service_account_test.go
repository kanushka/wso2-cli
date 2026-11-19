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
	"slices"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/modules/fixture"
)

const serviceAccountSecret = "s3cret-value"

type serviceAccountLogin struct {
	shell  app.Shell
	out    *bytes.Buffer
	errOut *bytes.Buffer
	issuer *fakeissuer.Issuer
}

func newServiceAccountLogin(t *testing.T, options fakeissuer.Options) serviceAccountLogin {
	t.Helper()
	return newServiceAccountLoginWith(t, options, []string{modules.MachineInline})
}

// newServiceAccountLoginWith installs apim declaring apimMachine as the ways a
// client-credentials context may reach it.
func newServiceAccountLoginWith(t *testing.T, options fakeissuer.Options, apimMachine []string) serviceAccountLogin {
	t.Helper()
	keyring.MockInit()
	options.Host = "localhost"
	options.RequireResource = true
	options.ClientSecret = serviceAccountSecret
	switch options.OUClaim {
	case "":
		options.OUClaim = "ou-ci"
	case "none":
		options.OUClaim = ""
	}
	issuer := fakeissuer.New(t, options)
	shell, out, errOut := newLoginShell(t)
	shell.Reader = failIfReadReader{t}
	t.Setenv(app.CloudEnvVar, "dev")
	t.Setenv("WSO2_CLIENT_ID", "ci-client")
	t.Setenv("WSO2_CLIENT_SECRET", serviceAccountSecret)
	writeCloudEnvironment(t, shell.StateRoot, "dev", cloudEnvironmentBody(issuer.URL))
	installFixture(t, shell, fixture.Module{Namespace: "apim", Version: "0.1.0",
		Product: &modules.ProductDescriptor{
			Audience: modules.AudienceResource, Grant: contexts.GrantExchange, Scopes: cloudAPIMScopes,
			Machine: apimMachine,
		}})
	installFixture(t, shell, fixture.Module{Namespace: "iam", Version: "0.1.0",
		Product: &modules.ProductDescriptor{
			Provider: contexts.ProviderThunder, Audience: modules.AudienceResource,
			Scopes: []string{"system", "iam:system"}, Machine: []string{modules.MachineInline},
		}})
	return serviceAccountLogin{shell: shell, out: out, errOut: errOut, issuer: issuer}
}

func (l serviceAccountLogin) run(args ...string) exit.Code {
	l.out.Reset()
	l.errOut.Reset()
	return l.shell.Run(args)
}

func TestServiceAccountLoginWritesACloudClientCredentialsContext(t *testing.T) {
	login := newServiceAccountLogin(t, fakeissuer.Options{})
	if code := login.run("login", "--service-account", "--no-input"); code != exit.OK {
		t.Fatalf("exit %d, stderr %s", code, login.errOut)
	}

	document := loadDocument(t, login.shell)
	written, found := document.Find("cloud-dev-ci")
	if !found || document.DefaultContext != "cloud-dev-ci" {
		t.Fatalf("document = %+v, want cloud-dev-ci written and selected", document)
	}
	if written.Type != contexts.TypeCloud || written.Login.Kind != contexts.KindClientCredentials ||
		written.Login.Provider != contexts.ProviderThunder || written.Login.Issuer != login.issuer.URL ||
		written.Login.ClientID != "ci-client" || written.Login.ClientSecretVariable != "WSO2_CLIENT_SECRET" ||
		written.CloudEnvironment != "dev" {
		t.Errorf("context = %+v, want a client-credentials Cloud context naming the secret variable", written)
	}
	if written.Organization != "ou-ci" || written.Login.Tenant != "ou-ci" {
		t.Errorf("organization = %q, want the token's ouId", written.Organization)
	}
	if _, recorded := written.Products["apim"]; !recorded {
		t.Errorf("apim is not recorded: %+v", written.Products)
	}
	if _, recorded := written.Products["iam"]; !recorded {
		t.Errorf("iam is not recorded: %+v", written.Products)
	}

	resources := map[string]string{}
	for _, grant := range login.issuer.ClientGrants() {
		if grant.Get("client_id") != "ci-client" {
			t.Errorf("grant presented client %q, want ci-client", grant.Get("client_id"))
		}
		resources[grant.Get("resource")] = grant.Get("scope")
	}
	if len(resources) != 2 || resources["urn:wso2:cloud"] != strings.Join(cloudAPIMScopes, " ") {
		t.Errorf("grants by resource = %v, want one per product with its own scopes", resources)
	}

	both := login.out.String() + login.errOut.String()
	if strings.Contains(both, serviceAccountSecret) {
		t.Errorf("the secret was printed:\n%s", both)
	}
	if !strings.Contains(login.out.String(), "cloud-dev-ci") {
		t.Errorf("stdout does not report the context:\n%s", login.out)
	}
}

func TestServiceAccountLoginReportsAsOneJSONDocument(t *testing.T) {
	login := newServiceAccountLogin(t, fakeissuer.Options{
		ClientScopeMode: "filter", ClientScopes: []string{"ap:api:view", "iam:system"},
	})
	if code := login.run("login", "--service-account", "--no-input", "-o", "json"); code != exit.OK {
		t.Fatalf("exit %d, stderr %s", code, login.errOut)
	}
	var report struct {
		Context      string `json:"context"`
		Environment  string `json:"environment"`
		Organization string `json:"organization"`
		Products     []struct {
			Namespace string   `json:"namespace"`
			Scopes    []string `json:"scopes"`
			Withheld  []string `json:"withheld"`
		} `json:"products"`
		Next []string `json:"next"`
	}
	if err := json.Unmarshal(login.out.Bytes(), &report); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, login.out)
	}
	if report.Context != "cloud-dev-ci" || report.Environment != "dev" || report.Organization != "ou-ci" {
		t.Errorf("report = %+v", report)
	}
	if len(report.Products) != 2 || report.Products[0].Namespace != "apim" ||
		!slices.Equal(report.Products[0].Scopes, []string{"ap:api:view"}) ||
		!slices.Equal(report.Products[0].Withheld, []string{"ap:api:manage"}) {
		t.Errorf("products = %+v, want apim granted ap:api:view and withheld ap:api:manage", report.Products)
	}
	if len(report.Next) == 0 {
		t.Errorf("no next step: %+v", report)
	}
	if said := login.errOut.String(); !strings.Contains(said, "apim") || strings.Contains(said, "ap:api:manage") {
		t.Errorf("stderr does not say plainly that apim lacks permissions:\n%s", said)
	}

	written := cloudServiceAccountContext(t, login.shell)
	if !slices.Equal(written.Products["apim"].Scopes, []string{"ap:api:view"}) {
		t.Errorf("apim records %v, want only what the account was granted", written.Products["apim"].Scopes)
	}
}

func TestServiceAccountLoginRefusals(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T)
		args    []string
		status  exit.Code
		says    []string
	}{
		{name: "no environment", prepare: func(t *testing.T) { t.Setenv(app.CloudEnvVar, "") },
			status: exit.Usage, says: []string{"shell.non_interactive", app.CloudEnvVar}},
		{name: "no client id", prepare: func(t *testing.T) { t.Setenv("WSO2_CLIENT_ID", "") },
			status: exit.Usage, says: []string{"shell.non_interactive", "WSO2_CLIENT_ID"}},
		{name: "no secret", prepare: func(t *testing.T) { t.Setenv("WSO2_CLIENT_SECRET", " ") },
			status: exit.Usage, says: []string{"shell.non_interactive", "WSO2_CLIENT_SECRET"}},
		{name: "wrong secret", prepare: func(t *testing.T) { t.Setenv("WSO2_CLIENT_SECRET", "wrong-one") },
			status: exit.AuthPolicy, says: []string{"auth.credential_unavailable", "WSO2_CLIENT_SECRET"}},
		{name: "with --url", args: []string{"--url", "https://example.com"},
			status: exit.Usage, says: []string{"shell.conflicting_arguments"}},
		{name: "with --only", args: []string{"--only", "apim"},
			status: exit.Usage, says: []string{"shell.conflicting_arguments"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			login := newServiceAccountLogin(t, fakeissuer.Options{})
			if testCase.prepare != nil {
				testCase.prepare(t)
			}
			args := append([]string{"login", "--service-account", "--no-input"}, testCase.args...)
			if code := login.run(args...); code != testCase.status {
				t.Fatalf("exit %d, want %d; stderr %s", code, testCase.status, login.errOut)
			}
			for _, said := range testCase.says {
				if !strings.Contains(login.errOut.String(), said) {
					t.Errorf("stderr does not say %q:\n%s", said, login.errOut)
				}
			}
			if strings.Contains(login.errOut.String(), "wrong-one") {
				t.Errorf("the secret was printed:\n%s", login.errOut)
			}
			if _, found := loadDocument(t, login.shell).Find("cloud-dev-ci"); found {
				t.Errorf("a refused login wrote the context")
			}
		})
	}
}

func TestServiceAccountLoginRefusesATokenWithNoOrganization(t *testing.T) {
	login := newServiceAccountLogin(t, fakeissuer.Options{OUClaim: "none"})
	if code := login.run("login", "--service-account", "--no-input"); code != exit.AuthPolicy {
		t.Fatalf("exit %d, want %d; stderr %s", code, exit.AuthPolicy, login.errOut)
	}
	if !strings.Contains(login.errOut.String(), "auth.organization_not_selected") {
		t.Errorf("stderr does not name the missing organization:\n%s", login.errOut)
	}
	if _, found := loadDocument(t, login.shell).Find("cloud-dev-ci"); found {
		t.Errorf("a refused login wrote the context")
	}
}

func TestServiceAccountLoginRefusedByEveryProductWritesNothing(t *testing.T) {
	login := newServiceAccountLogin(t, fakeissuer.Options{ClientScopeMode: "filter"})
	if code := login.run("login", "--service-account", "--no-input"); code != exit.AuthPolicy {
		t.Fatalf("exit %d, want %d; stderr %s", code, exit.AuthPolicy, login.errOut)
	}
	if _, found := loadDocument(t, login.shell).Find("cloud-dev-ci"); found {
		t.Errorf("a login no product accepted wrote the context")
	}
}

func TestTheServiceAccountContextGrantsWhatTheAccountHolds(t *testing.T) {
	login := newServiceAccountLogin(t, fakeissuer.Options{
		ClientScopeMode: "filter", ClientScopes: []string{"ap:api:view", "iam:system"},
	})
	if code := login.run("login", "--service-account", "--no-input"); code != exit.OK {
		t.Fatalf("login: exit %d, stderr %s", code, login.errOut)
	}
	selection, err := loadDocument(t, login.shell).Select("")
	if err != nil {
		t.Fatal(err)
	}
	before := len(login.issuer.ClientGrants())
	broker := auth.Broker{
		Namespace:    "apim",
		Capabilities: modules.Capabilities{AuthAudiences: []string{"apim"}, AuthScopes: cloudAPIMScopes},
		Selection:    selection,
		InvocationID: "invocation-1",
		StateRoot:    login.shell.StateRoot,
	}
	if _, err := broker.Acquire(auth.Request{Audience: "apim"}); err != nil {
		t.Fatalf("a command on the service-account context was refused: %v", err)
	}
	grants := login.issuer.ClientGrants()[before:]
	if len(grants) != 1 || grants[0].Get("scope") != "ap:api:view" || grants[0].Get("resource") != "urn:wso2:cloud" {
		t.Errorf("grants = %v, want one for apim's granted scope at its resource", grants)
	}
}

func TestAPlainLoginSaysItIgnoredTheServiceAccountVariables(t *testing.T) {
	login := newCloudLogin(t)
	installCloudAPIM(t, login.shell)
	t.Setenv("WSO2_CLIENT_ID", "ci-client")
	t.Setenv("WSO2_CLIENT_SECRET", "")
	login.run(t, "login")
	said := login.errOut.String()
	if strings.Count(said, "--service-account") != 1 || !strings.Contains(said, "WSO2_CLIENT_ID") ||
		strings.Contains(said, "WSO2_CLIENT_SECRET") {
		t.Errorf("stderr does not say once that the set variable was ignored for --service-account:\n%s", said)
	}
	if strings.Contains(said, serviceAccountSecret) {
		t.Errorf("the secret was printed:\n%s", said)
	}
	if cloudContext(t, login.shell).Login.Kind != contexts.KindOAuthBrowser {
		t.Errorf("a plain login did not sign in in the browser")
	}
}

func cloudServiceAccountContext(t *testing.T, shell app.Shell) contexts.Context {
	t.Helper()
	found, ok := loadDocument(t, shell).Find("cloud-dev-ci")
	if !ok {
		t.Fatal("no cloud-dev-ci context")
	}
	return found
}

func TestTheServiceAccountWizardChecksTypedValuesAndWritesNothing(t *testing.T) {
	login := newServiceAccountLogin(t, fakeissuer.Options{})
	t.Setenv(app.CloudEnvVar, "")
	t.Setenv("WSO2_CLIENT_ID", "")
	t.Setenv("WSO2_CLIENT_SECRET", "")
	login.shell.Reader = strings.NewReader("dev\nci-client\n" + serviceAccountSecret + "\n")
	if code := login.run("login", "--service-account"); code != exit.OK {
		t.Fatalf("exit %d, stderr %s", code, login.errOut)
	}
	if len(login.issuer.ClientGrants()) != 2 {
		t.Errorf("the wizard did not check the account: %v", login.issuer.ClientGrants())
	}
	if _, found := loadDocument(t, login.shell).Find("cloud-dev-ci"); found {
		t.Errorf("the wizard wrote a context naming variables this machine does not set")
	}
	said := login.out.String()
	for _, want := range []string{"WSO2_CLOUD_ENV=dev", "WSO2_CLIENT_ID=", "WSO2_CLIENT_SECRET",
		"login --service-account --no-input", "ou-ci"} {
		if !strings.Contains(said, want) {
			t.Errorf("stdout does not say %q:\n%s", want, said)
		}
	}
	if strings.Contains(said+login.errOut.String(), serviceAccountSecret) {
		t.Errorf("the secret was printed:\n%s%s", said, login.errOut)
	}
}

func TestTheServiceAccountWizardUsesTheVariablesWhenSet(t *testing.T) {
	login := newServiceAccountLogin(t, fakeissuer.Options{})
	login.shell.Reader = strings.NewReader("")
	if code := login.run("login", "--service-account"); code != exit.OK {
		t.Fatalf("exit %d, stderr %s", code, login.errOut)
	}
	if strings.Count(login.errOut.String(), "WSO2_CLIENT_SECRET") != 1 {
		t.Errorf("stderr does not say once which variables it used:\n%s", login.errOut)
	}
	cloudServiceAccountContext(t, login.shell)
	if !strings.Contains(login.out.String(), "login --service-account --no-input") {
		t.Errorf("stdout does not print the CI command:\n%s", login.out)
	}
}

func TestANonInteractiveCloudLoginPointsAtTheServiceAccount(t *testing.T) {
	login := newCloudLogin(t)
	installCloudAPIM(t, login.shell)
	login.errOut.Reset()
	if code := login.shell.Run([]string{"login", "--no-input"}); code != exit.AuthPolicy {
		t.Fatalf("exit %d, want %d; stderr %s", code, exit.AuthPolicy, login.errOut)
	}
	if !strings.Contains(login.errOut.String(), "login --service-account") {
		t.Errorf("the refusal does not name the service-account login:\n%s", login.errOut)
	}
}

func TestAPlainLoginOutsideWSO2CloudSaysNothingOfTheServiceAccount(t *testing.T) {
	shell, _, errOut, issuer := newCreatingLogin(t)
	if code := shell.Run([]string{"login", "--url", issuer.URL, "--client-id", "wso2-cli", "--context", "local"}); code != exit.OK {
		t.Fatalf("first login: exit %d, stderr %s", code, errOut)
	}
	t.Setenv("WSO2_CLIENT_ID", "ci-client")
	errOut.Reset()
	if code := shell.Run([]string{"login", "--context", "local"}); code != exit.OK {
		t.Fatalf("login: exit %d, stderr %s", code, errOut)
	}
	if strings.Contains(errOut.String(), "--service-account") {
		t.Errorf("a login outside WSO2 Cloud mentioned the service account:\n%s", errOut)
	}
}

func TestAProductTheAccountCannotReachDoesNotStopTheLogin(t *testing.T) {
	login := newServiceAccountLogin(t, fakeissuer.Options{RegisteredResource: "urn:wso2:cloud"})
	if code := login.run("login", "--service-account", "--no-input", "-o", "json"); code != exit.OK {
		t.Fatalf("exit %d, stderr %s", code, login.errOut)
	}
	var report struct {
		Products []struct {
			Namespace string   `json:"namespace"`
			Scopes    []string `json:"scopes"`
			Withheld  []string `json:"withheld"`
		} `json:"products"`
		Next []string `json:"next"`
	}
	if err := json.Unmarshal(login.out.Bytes(), &report); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, login.out)
	}
	if len(report.Products) != 2 || report.Products[1].Namespace != "iam" || len(report.Products[1].Scopes) != 0 ||
		!slices.Equal(report.Products[1].Withheld, []string{"iam:system"}) {
		t.Errorf("products = %+v, want iam reported with nothing granted", report.Products)
	}
	if strings.Count(login.errOut.String(), "iam") == 0 {
		t.Errorf("stderr does not name the product that was refused:\n%s", login.errOut)
	}
	if len(report.Next) != 1 || !strings.HasPrefix(report.Next[0], "wso2 apim") {
		t.Errorf("next = %q, want a command of a product the account reaches", report.Next)
	}
	cloudServiceAccountContext(t, login.shell)
}

func TestAModuleThatRefusesServiceAccountsIsLeftOut(t *testing.T) {
	login := newServiceAccountLoginWith(t, fakeissuer.Options{}, nil)
	if code := login.run("login", "--service-account", "--no-input"); code != exit.OK {
		t.Fatalf("exit %d, stderr %s", code, login.errOut)
	}
	if !strings.Contains(login.errOut.String(), "apim") {
		t.Errorf("stderr does not say apim was left out:\n%s", login.errOut)
	}
	written := cloudServiceAccountContext(t, login.shell)
	if _, recorded := written.Products["apim"]; recorded {
		t.Errorf("apim was recorded although its module does not admit a service account")
	}
	if _, recorded := written.Products["iam"]; !recorded {
		t.Errorf("iam was not recorded")
	}
}

func TestTheServiceAccountVariablesCanBeNamed(t *testing.T) {
	login := newServiceAccountLogin(t, fakeissuer.Options{})
	t.Setenv("WSO2_CLIENT_ID", "")
	t.Setenv("WSO2_CLIENT_SECRET", "")
	t.Setenv("CI_ID", "ci-client")
	t.Setenv("CI_SECRET", serviceAccountSecret)
	if code := login.run("login", "--service-account", "--no-input", "--client-id-variable", "CI_ID",
		"--client-secret-variable", "CI_SECRET"); code != exit.OK {
		t.Fatalf("exit %d, stderr %s", code, login.errOut)
	}
	if written := cloudServiceAccountContext(t, login.shell); written.Login.ClientSecretVariable != "CI_SECRET" {
		t.Errorf("secret variable = %q, want CI_SECRET", written.Login.ClientSecretVariable)
	}
	if code := login.run("login", "--client-secret-variable", "CI_SECRET"); code != exit.Usage {
		t.Errorf("--client-secret-variable without --service-account: exit %d, want %d", code, exit.Usage)
	}
}

func TestTheServiceAccountLoginReplacesOnlyAServiceAccountContext(t *testing.T) {
	login := newServiceAccountLogin(t, fakeissuer.Options{})
	if code := login.run("context", "create", "machine", "--issuer", login.issuer.URL, "--client-id", "other",
		"--client-secret-variable", "OTHER_SECRET"); code != exit.OK {
		t.Fatalf("context create: exit %d, stderr %s", code, login.errOut)
	}
	if code := login.run("login", "--service-account", "--no-input", "--context", "machine"); code == exit.OK {
		t.Fatalf("the login replaced a client-credentials context it did not write")
	}
	if found, _ := loadDocument(t, login.shell).Find("machine"); found.Login.ClientID != "other" {
		t.Errorf("machine = %+v, want it left as it was", found.Login)
	}
}

func TestTheServiceAccountWizardSaysWhereTheAccountComesFrom(t *testing.T) {
	login := newServiceAccountLogin(t, fakeissuer.Options{})
	t.Setenv("WSO2_CLIENT_ID", "")
	t.Setenv("WSO2_CLIENT_SECRET", "")
	login.shell.Reader = strings.NewReader("ci-client\nwrong-one\n")
	if code := login.run("login", "--service-account"); code != exit.AuthPolicy {
		t.Fatalf("exit %d, want %d; stderr %s", code, exit.AuthPolicy, login.errOut)
	}
	said := login.errOut.String()
	if !strings.Contains(said, "WSO2 Cloud console") {
		t.Errorf("the wizard does not say where to create the account:\n%s", said)
	}
	if strings.Contains(said, "WSO2_CLIENT_SECRET") || strings.Contains(said, "cloud-dev-ci") {
		t.Errorf("the refusal of a typed secret names a variable or a context that does not hold it:\n%s", said)
	}
}

func TestATypedEnvironmentStillWritesTheContext(t *testing.T) {
	login := newServiceAccountLogin(t, fakeissuer.Options{})
	t.Setenv(app.CloudEnvVar, "")
	login.shell.Reader = strings.NewReader("dev\n")
	if code := login.run("login", "--service-account"); code != exit.OK {
		t.Fatalf("exit %d, stderr %s", code, login.errOut)
	}
	cloudServiceAccountContext(t, login.shell)
}
