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
