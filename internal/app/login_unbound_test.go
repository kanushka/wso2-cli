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
	"net/http"
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/modules/fixture"
)

// A ThunderID issuer binds every login to a resource server, and a login that
// creates its account has no product to take one from. Measured against
// ThunderID on 2026-09-10: the refusal came back as "the identity provider
// refused this login" with a recovery to retry, which no retry could satisfy.
func TestACreatingLoginAnIssuerRefusesForNoResourceNamesTheProvidersConnect(t *testing.T) {
	keyring.MockInit()
	issuer := fakeissuer.New(t, fakeissuer.Options{Host: "localhost", RequireResource: true})
	shell, _, errOut := newLoginShell(t)
	shell.OpenBrowser = func(authURL string) error {
		go func() {
			if response, err := http.Get(authURL); err == nil {
				_ = response.Body.Close()
			}
		}()
		return nil
	}
	installFixture(t, shell, fixture.Module{Namespace: "iam", Version: "0.1.0",
		AuthAudiences: []string{"thunder-system"}, AuthScopes: []string{"system"},
		Product: &modules.ProductDescriptor{
			Provider: contexts.ProviderThunder, ClientID: "wso2-cli",
			Audience: modules.AudienceResource, DefaultAudience: "https://localhost:8090/mcp",
			Scopes: []string{"system"},
		}})

	code := shell.Run([]string{"login", "--url", issuer.URL, "--client-id", "wso2-cli", "--context", "local"})
	if code == exit.OK {
		t.Fatal("a login naming no resource completed against an issuer that requires one")
	}
	for _, expected := range []string{
		"auth.product_not_configured", "invalid_target",
		"wso2 iam connect " + issuer.URL + " --account local", "wso2 login --context local",
	} {
		if !strings.Contains(errOut.String(), expected) {
			t.Errorf("stderr is missing %q:\n%s", expected, errOut)
		}
	}
	if strings.Contains(errOut.String(), "Retry wso2 login") {
		t.Errorf("the refusal still offers a retry that cannot succeed:\n%s", errOut)
	}
	document, err := contexts.Load(shell.StateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Accounts) != 0 {
		t.Errorf("a refused login wrote an account: %+v", document.Accounts)
	}
}
