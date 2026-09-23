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
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/modules/fixture"
)

const gwmURL = "http://localhost:9611"
const gwmGatewayURL = "http://localhost:9612"

// installGWMModule installs a product whose gateway is stricter than the
// product itself: a client-credentials context may reach gwm directly
// (Machine: MachineInline), but its gateway declares no machine strategy at
// all, so a machine context is refused only at the gateway.
func installGWMModule(t *testing.T, shell app.Shell) {
	t.Helper()
	installFixture(t, shell, fixture.Module{Namespace: "gwm", Version: "0.1.0",
		Product: &modules.ProductDescriptor{
			Audience: modules.AudienceResource, Grant: contexts.GrantExchange,
			Machine: []string{modules.MachineInline},
			Gateway: &modules.GatewayDescriptor{Audience: modules.AudienceResource},
		}})
}

func TestContextProductAddResolutionRefusals(t *testing.T) {
	type setup func(t *testing.T, shell app.Shell)
	cases := map[string]struct {
		setup setup
		args  []string
		code  string
		exit  exit.Code
	}{
		"a login provider recorded on a context that logs in elsewhere": {
			setup: func(t *testing.T, shell app.Shell) {
				mustRun(t, shell, "context", "create", "other", "--issuer", "https://idp.other.example",
					"--client-id", "cli", "--use")
			},
			args: []string{"iam", "--url", thunderURL},
			code: "shell.conflicting_arguments",
			exit: exit.Usage,
		},
		"a client-secret-variable on a browser context": {
			setup: func(t *testing.T, shell app.Shell) {
				mustRun(t, shell, "context", "create", "local", "--login-product", "iam", "--url", thunderURL, "--use")
			},
			args: []string{"api", "--url", apiURL, "--client-secret-variable", "API_SECRET"},
			code: "shell.conflicting_arguments",
			exit: exit.Usage,
		},
		"a grant needing a client the descriptor names none for": {
			setup: func(t *testing.T, shell app.Shell) { localSetup(t, shell) },
			args:  []string{"apim", "--url", apimURL},
			code:  "shell.missing_required_flag",
			exit:  exit.Usage,
		},
		"a product url in plain http off loopback": {
			setup: func(t *testing.T, shell app.Shell) { localSetup(t, shell) },
			args:  []string{"api", "--url", "http://api.example"},
			code:  "shell.invalid_argument",
			exit:  exit.Usage,
		},
		"a gateway url in plain http off loopback": {
			setup: func(t *testing.T, shell app.Shell) { localSetup(t, shell) },
			args:  []string{"api", "--url", "https://api.example", "--gateway", "http://gw.example"},
			code:  "shell.invalid_argument",
			exit:  exit.Usage,
		},
		"a gateway on a product declaring none": {
			setup: func(t *testing.T, shell app.Shell) { localSetup(t, shell) },
			args:  []string{"reference", "--url", "https://ref.example", "--gateway", "https://ref.example/gw"},
			code:  "shell.invalid_argument",
			exit:  exit.Usage,
		},
		"a machine client the product declares no way to reach": {
			setup: func(t *testing.T, shell app.Shell) {
				mustRun(t, shell, "context", "create", "ci", "--login-product", "iam", "--url", thunderURL,
					"--client-id", "ci-client", "--client-secret-variable", "CI_SECRET", "--use")
			},
			args: []string{"api", "--url", apiURL},
			code: "auth.product_not_configured",
			exit: exit.AuthPolicy,
		},
		"a client-secret-variable the product accepts only inline": {
			setup: func(t *testing.T, shell app.Shell) {
				installGWMModule(t, shell)
				mustRun(t, shell, "context", "create", "ci", "--login-product", "iam", "--url", thunderURL,
					"--client-id", "ci-client", "--client-secret-variable", "CI_SECRET", "--use")
			},
			args: []string{"gwm", "--url", gwmURL, "--client-secret-variable", "GWM_SECRET"},
			code: "auth.product_not_configured",
			exit: exit.AuthPolicy,
		},
		"a gateway the product's own machine client is not accepted at": {
			setup: func(t *testing.T, shell app.Shell) {
				installGWMModule(t, shell)
				mustRun(t, shell, "context", "create", "ci", "--login-product", "iam", "--url", thunderURL,
					"--client-id", "ci-client", "--client-secret-variable", "CI_SECRET", "--use")
			},
			args: []string{"gwm", "--url", gwmURL, "--gateway", gwmGatewayURL},
			code: "auth.product_not_configured",
			exit: exit.AuthPolicy,
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			shell, _, _ := newContextShell(t)
			testCase.setup(t, shell)
			code, _, errOut := run(t, shell, append([]string{"context", "product", "add"}, testCase.args...)...)
			if code != testCase.exit {
				t.Fatalf("exit %d, want %d: %s", code, testCase.exit, errOut)
			}
			if !strings.Contains(errOut, testCase.code) {
				t.Errorf("stderr does not carry %s:\n%s", testCase.code, errOut)
			}
		})
	}
}

// TestContextProductAddNamesTheProviderAProductServes proves the refusal a
// login provider recorded elsewhere gives says which provider each side is,
// and adds the line that ends the search when no module serves the context's
// provider at all. Logging in at Asgardeo works; reaching iam from there does
// not, and the difference is what the message has to carry.
func TestContextProductAddNamesTheProviderAProductServes(t *testing.T) {
	cases := map[string]struct {
		setup      func(t *testing.T, shell app.Shell)
		args       []string
		want, none []string
	}{
		"a provider no module serves": {
			setup: func(t *testing.T, shell app.Shell) {
				mustRun(t, shell, "context", "create", "acme", "--issuer",
					"https://api.asgardeo.io/t/acme/oauth2/token", "--provider", contexts.ProviderAsgardeo,
					"--client-id", "cli", "--use")
			},
			args: []string{"iam", "--url", thunderURL},
			want: []string{`the iam product signs in at its own Thunder issuer, and the "acme" context ` +
				"signs in at Asgardeo", "No module serves Asgardeo yet."},
		},
		"the same provider at another URL": {
			setup: func(t *testing.T, shell app.Shell) {
				mustRun(t, shell, "context", "create", "local", "--login-product", "iam",
					"--url", thunderURL, "--use")
			},
			args: []string{"iam", "--url", "https://thunder.corp.example", "--replace"},
			want: []string{`the iam product signs in at its own Thunder issuer, and the "local" context ` +
				"signs in at Thunder"},
			// A second Thunder is a mistyped URL, not a missing module.
			none: []string{"No module serves"},
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			shell, _, _ := newContextShell(t)
			testCase.setup(t, shell)
			code, _, errOut := run(t, shell, append([]string{"context", "product", "add"}, testCase.args...)...)
			if code != exit.Usage {
				t.Fatalf("exit %d, want %d: %s", code, exit.Usage, errOut)
			}
			for _, want := range testCase.want {
				if !strings.Contains(errOut, want) {
					t.Errorf("stderr lacks %q:\n%s", want, errOut)
				}
			}
			for _, none := range testCase.none {
				if strings.Contains(errOut, none) {
					t.Errorf("stderr carries %q, which this refusal is not:\n%s", none, errOut)
				}
			}
		})
	}
}

// TestContextProductAddResolvesAMachineClientTheProductAcceptsInline proves
// the gwm fixture's happy path: a client-credentials context reaches it
// directly, without a gateway, when the gateway is left out.
func TestContextProductAddResolvesAMachineClientTheProductAcceptsInline(t *testing.T) {
	shell, _, _ := newContextShell(t)
	installGWMModule(t, shell)
	mustRun(t, shell, "context", "create", "ci", "--login-product", "iam", "--url", thunderURL,
		"--client-id", "ci-client", "--client-secret-variable", "CI_SECRET", "--use")
	mustRun(t, shell, "context", "product", "add", "gwm", "--url", gwmURL)
	ci := contextNamed(t, loadDocument(t, shell), "ci")
	if ci.Products["gwm"].Endpoint != gwmURL {
		t.Fatalf("gwm = %+v", ci.Products["gwm"])
	}
}
