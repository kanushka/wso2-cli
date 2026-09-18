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

// More of the setup wizard's tests (#189), reusing newContextShell's fixture
// (iam, api, apim, reference) and localWizardAnswers' harness from
// context_wizard_test.go, plus fixtures of their own for the branches that
// fixture does not reach: the path from context create into login, a login
// product with no descriptor or one that is not a login provider, a Thunder
// descriptor that names no client or audience, and the wizards' edges around
// a context that is not selected or a product list that is empty.
package app_test

import (
	"net/http"
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/modules/fixture"
)

// TestContextCreateWizardLogsInAfterCreate proves the path from wso2 context
// create's wizard into a login (#189): agreeing to "Log in now?" runs the
// same wso2 login the person would have typed next, against the context the
// wizard just wrote.
func TestContextCreateWizardLogsInAfterCreate(t *testing.T) {
	keyring.MockInit()
	issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "https://localhost:8090/mcp"})
	shell, _, _ := newContextShell(t)
	shell.OpenBrowser = func(authURL string) error {
		go func() {
			response, err := http.Get(authURL)
			if err == nil {
				_ = response.Body.Close()
			}
		}()
		return nil
	}
	shell.Reader = strings.NewReader("" +
		"1\n" + // Sign in with: Thunder, through iam
		issuer.URL + "\n" + // Thunder URL
		"\n" + // Sign in using: browser
		"3\n" + // Skip products
		"\n" + // Create: yes
		"\n" + // Select: yes
		"\n") // Log in now: yes (the default)
	code, out, errOut := run(t, shell, "context", "create", "wizlogin")
	if code != exit.OK {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	if !strings.Contains(out, `Created the "wizlogin" context.`) {
		t.Errorf("stdout does not report the create:\n%s", out)
	}
	for _, want := range []string{"user-1", "dev@example.test"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout does not report the login that followed:\n%s", out)
		}
	}
	stored, err := session.Store{StateRoot: shell.StateRoot}.Load("wizlogin")
	if err != nil {
		t.Fatalf("the wizard's login did not store a session: %v", err)
	}
	if stored.Issuer != issuer.URL {
		t.Errorf("stored session names the issuer %q, want %q", stored.Issuer, issuer.URL)
	}
}

// TestAskThunderLoginRefusesAProductThatDoesNotLoginProvide proves the two
// ways picking Thunder can find nothing to sign in through: the suggested
// product (identity, installed first when nothing installed names Thunder)
// declares no descriptor at all, or declares one that is not a login
// provider. Both are refused the way --login-product already is for the same
// reason (shell.invalid_argument).
func TestAskThunderLoginRefusesAProductThatDoesNotLoginProvide(t *testing.T) {
	cases := map[string]*modules.ProductDescriptor{
		"no descriptor":                   nil,
		"a descriptor naming no provider": {Audience: modules.AudienceResource, Scopes: []string{"system"}},
	}
	for name, descriptor := range cases {
		t.Run(name, func(t *testing.T) {
			shell, _, _ := newShell(t)
			module := fixture.Module{Namespace: "identity", Version: "0.1.0", Product: descriptor}
			installFixture(t, shell, module)
			shell.Reader = strings.NewReader("1\n") // Sign in with: Thunder
			code, _, errOut := run(t, shell, "context", "create", "local")
			if code != exit.Usage {
				t.Fatalf("exit %d, want usage: %s", code, errOut)
			}
			for _, want := range []string{"shell.invalid_argument",
				"the identity product is not a login provider"} {
				if !strings.Contains(errOut, want) {
					t.Errorf("stderr lacks %q:\n%s", want, errOut)
				}
			}
			if len(loadDocument(t, shell).Contexts) != 0 {
				t.Error("a refused wizard wrote a context")
			}
		})
	}
}

// TestAskThunderLoginPromptsForAMissingClientIDAndAudience proves that when
// the suggested Thunder product's descriptor names no default client or
// audience, the wizard asks for both, in place of the flags --client-id and
// --audience. The descriptor also declares no MachineInline strategy, so this
// doubles as the "Sign in using:" client-credentials option being marked
// unavailable for a product that does not accept one.
func TestAskThunderLoginPromptsForAMissingClientIDAndAudience(t *testing.T) {
	shell, _, _ := newShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "identity", Version: "0.1.0",
		Product: &modules.ProductDescriptor{
			Provider: contexts.ProviderThunder, Audience: modules.AudienceResource, Scopes: []string{"system"},
		}})
	shell.Reader = strings.NewReader("" +
		"1\n" + // Sign in with: Thunder, through identity
		"https://thunder.example\n" + // Thunder URL
		"cli-manual\n" + // Client ID, which the descriptor names none of
		"https://aud.example\n" + // Audience, which the descriptor names none of
		"\n" + // Sign in using: browser
		"\n" + // Create: yes
		"\n" + // Select: yes
		"n\n") // Log in now: no
	code, out, errOut := run(t, shell, "context", "create", "widget")
	if code != exit.OK {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	for _, question := range []string{"Client ID of the registered OAuth application: ",
		"Audience (the resource server access is bound to): "} {
		if !strings.Contains(errOut, question) {
			t.Errorf("stderr lacks %q:\n%s", question, errOut)
		}
	}
	widget := contextNamed(t, loadDocument(t, shell), "widget")
	if widget.Login.ClientID != "cli-manual" {
		t.Errorf("client ID = %q, want %q", widget.Login.ClientID, "cli-manual")
	}
	if got := widget.Products["identity"].Audience; got != "https://aud.example" {
		t.Errorf("audience = %q, want %q", got, "https://aud.example")
	}
}

// TestAskThunderLoginEndOfInputRefusesBeforeAClientIDOrAudience proves the
// same two questions end the wizard, rather than writing anything, when input
// runs out before they are answered.
func TestAskThunderLoginEndOfInputRefusesBeforeAClientIDOrAudience(t *testing.T) {
	cases := map[string]struct {
		answers string
		want    string
	}{
		"before the client ID": {"1\nhttps://thunder.example\n",
			"input ended before the client ID was entered"},
		"before the audience": {"1\nhttps://thunder.example\ncli-manual\n",
			"input ended before the audience was entered"},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			shell, _, _ := newShell(t)
			installFixture(t, shell, fixture.Module{Namespace: "identity", Version: "0.1.0",
				Product: &modules.ProductDescriptor{
					Provider: contexts.ProviderThunder, Audience: modules.AudienceResource, Scopes: []string{"system"},
				}})
			shell.Reader = strings.NewReader(testCase.answers)
			code, _, errOut := run(t, shell, "context", "create", "widget")
			if code != exit.Usage || !strings.Contains(errOut, testCase.want) {
				t.Fatalf("exit %d, want %q: %s", code, testCase.want, errOut)
			}
			if len(loadDocument(t, shell).Contexts) != 0 {
				t.Error("a refused wizard wrote a context")
			}
		})
	}
}

// TestContextProductAddWizardAcceptsAnEmptyGatewayAnswer proves that typing
// nothing for the optional gateway question, rather than ending input
// outright, records the product with no gateway.
func TestContextProductAddWizardAcceptsAnEmptyGatewayAnswer(t *testing.T) {
	shell, _, _ := newContextShell(t)
	mustRun(t, shell, "context", "create", "local", "--login-product", "iam", "--url", thunderURL, "--use")
	shell.Reader = strings.NewReader("1\n" + apiURL + "\n" + "\n" + "\n") // api, its URL, no gateway, add it
	code, out, errOut := run(t, shell, "context", "product", "add")
	if code != exit.OK {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	api := contextNamed(t, loadDocument(t, shell), "local").Products["api"]
	if api.Gateway != nil {
		t.Errorf("an empty answer recorded a gateway: %+v", api.Gateway)
	}
}

// TestContextProductAddWizardLeavesTheGatewayEmptyOnEndOfInput proves the
// same for input that runs out at the gateway question instead of answering
// it empty: unlike the wizard's required questions, this one takes the
// question's own end-of-input as leaving nothing changed rather than as a
// refusal, and the confirmation after it still defaults to yes.
func TestContextProductAddWizardLeavesTheGatewayEmptyOnEndOfInput(t *testing.T) {
	shell, _, _ := newContextShell(t)
	mustRun(t, shell, "context", "create", "local", "--login-product", "iam", "--url", thunderURL, "--use")
	shell.Reader = strings.NewReader("1\n" + apiURL + "\n") // pick api, its URL, then input ends
	code, out, errOut := run(t, shell, "context", "product", "add")
	if code != exit.OK {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	api := contextNamed(t, loadDocument(t, shell), "local").Products["api"]
	if api.Gateway != nil {
		t.Errorf("end of input recorded a gateway: %+v", api.Gateway)
	}
	if api.Endpoint != apiURL {
		t.Errorf("endpoint = %q, want %q", api.Endpoint, apiURL)
	}
}

// TestContextProductAddWizardRefusesWithNoContextSelected proves that with
// nothing selected and no --context, the wizard is refused before asking
// anything: recordedProducts needs a target context to read from before the
// wizard can offer what is left to add.
func TestContextProductAddWizardRefusesWithNoContextSelected(t *testing.T) {
	shell, _, _ := newContextShell(t)
	mustRun(t, shell, "context", "create", "local", "--login-product", "iam", "--url", thunderURL)
	shell.Reader = failIfReadReader{t}
	code, _, errOut := run(t, shell, "context", "product", "add")
	if code != exit.Usage || !strings.Contains(errOut, "contexts.no_context_selected") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

// TestContextProductAddWizardRefusesWhenNothingIsLeftToAdd proves the wizard
// is refused, unasked, when the selected context's login product is the only
// one installed: a login provider is never offered as a product to add
// (reachable), so there is nothing left.
func TestContextProductAddWizardRefusesWhenNothingIsLeftToAdd(t *testing.T) {
	shell, _, _ := newShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "iam", Version: "0.1.0",
		Product: &modules.ProductDescriptor{
			Provider: contexts.ProviderThunder, ClientID: "wso2-cli",
			Audience: modules.AudienceResource, DefaultAudience: "https://localhost:8090/mcp",
			Scopes: []string{"system"}, Machine: []string{modules.MachineInline},
		}})
	mustRun(t, shell, "context", "create", "local", "--login-product", "iam", "--url", thunderURL, "--use")
	shell.Reader = failIfReadReader{t}
	code, _, errOut := run(t, shell, "context", "product", "add")
	if code != exit.Usage || !strings.Contains(errOut, "shell.missing_argument") ||
		!strings.Contains(errOut, "no installed product is left to add") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}
