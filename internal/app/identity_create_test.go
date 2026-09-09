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
	"slices"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
)

func TestIdentityCreateWritesABrowserIdentityWithOneProduct(t *testing.T) {
	shell, out, errOut := newShell(t)
	code := shell.Run([]string{"identity", "create", "thunder-admin",
		"--issuer", "http://localhost:8490", "--client-id", "wso2-cli", "--provider", "thunder",
		"--product", "iam", "--endpoint", "http://localhost:8490",
		"--audience", "https://localhost:8090/mcp", "--scope", "system"})
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	document := loadDocument(t, shell)
	if len(document.Identities) != 1 {
		t.Fatalf("identities = %+v", document.Identities)
	}
	identity := document.Identities[0]
	if identity.Name != "thunder-admin" || identity.Type != "onprem" ||
		identity.Auth.Kind != contexts.KindOAuthBrowser || identity.Auth.Provider != "thunder" ||
		identity.Auth.Issuer != "http://localhost:8490" || identity.Auth.ClientID != "wso2-cli" ||
		identity.Auth.CredentialRef != "thunder-admin" || identity.Auth.ClientSecretVariable != "" {
		t.Errorf("identity = %+v", identity)
	}
	if got := identity.Products["iam"]; got.Endpoint != "http://localhost:8490" ||
		got.Audience != "https://localhost:8090/mcp" || !slices.Equal(got.Scopes, []string{"system"}) {
		t.Errorf("product = %+v", got)
	}
	if document.DefaultContext != "thunder-admin" || len(document.Contexts) != 1 ||
		document.Contexts[0].Identity != "thunder-admin" {
		t.Errorf("context not written or selected: %+v", document)
	}
	if !strings.Contains(out.String(), "Next  Run wso2 login --context thunder-admin") {
		t.Errorf("no next line:\n%s", out)
	}
}

func TestIdentityCreateWithASecretVariableIsClientCredentials(t *testing.T) {
	shell, out, errOut := newShell(t)
	code := shell.Run([]string{"identity", "create", "apim-admin",
		"--issuer", "https://localhost:9443/oauth2/token", "--client-id", "abc",
		"--client-secret-variable", "WSO2_APIM_CLIENT_SECRET",
		"--product", "apim", "--endpoint", "https://localhost:9443", "--audience", "abc",
		"--scope", "apim:api_view", "--scope", "apim:admin", "--output", "json"})
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	identity := loadDocument(t, shell).Identities[0]
	if identity.Auth.Kind != contexts.KindClientCredentials ||
		identity.Auth.ClientSecretVariable != "WSO2_APIM_CLIENT_SECRET" || identity.Auth.CredentialRef != "" {
		t.Errorf("identity = %+v", identity)
	}
	if !slices.Equal(identity.Products["apim"].Scopes, []string{"apim:api_view", "apim:admin"}) {
		t.Errorf("scopes = %v", identity.Products["apim"].Scopes)
	}
	if !strings.Contains(out.String(), `"next": "Run wso2 apim status --context apim-admin.`) ||
		!strings.Contains(out.String(), `"kind": "client-credentials"`) {
		t.Errorf("json:\n%s", out)
	}
}

func TestIdentityCreateRefusesAThunderProductWithoutAnAudience(t *testing.T) {
	shell, _, errOut := newShell(t)
	code := shell.Run([]string{"identity", "create", "thunder-admin",
		"--issuer", "http://localhost:8490", "--client-id", "wso2-cli", "--provider", "thunder",
		"--product", "iam", "--endpoint", "http://localhost:8490"})
	if code != exit.Usage {
		t.Fatalf("exit %d, want %d: %s", code, exit.Usage, errOut)
	}
	if !strings.Contains(errOut.String(), "shell.missing_required_flag") ||
		!strings.Contains(errOut.String(), "--audience") {
		t.Errorf("stderr:\n%s", errOut)
	}
	if document := loadDocument(t, shell); len(document.Identities) != 0 {
		t.Errorf("a refused identity was written: %+v", document.Identities)
	}
}

func TestIdentityCreateRefusesADuplicateAndAHalfProduct(t *testing.T) {
	shell, _, errOut := newShell(t)
	args := []string{"identity", "create", "one", "--issuer", "https://issuer.example", "--client-id", "c"}
	if code := shell.Run(args); code != exit.OK {
		t.Fatalf("first create: exit %d: %s", code, errOut)
	}
	errOut.Reset()
	if code := shell.Run(args); code != exit.Usage || !strings.Contains(errOut.String(), "contexts.identity_exists") {
		t.Errorf("duplicate: exit %d, stderr:\n%s", code, errOut)
	}
	errOut.Reset()
	code := shell.Run([]string{"identity", "create", "two", "--issuer", "https://issuer.example",
		"--client-id", "c", "--endpoint", "https://product.example"})
	if code != exit.Usage || !strings.Contains(errOut.String(), "shell.conflicting_arguments") {
		t.Errorf("half product: exit %d, stderr:\n%s", code, errOut)
	}
	errOut.Reset()
	code = shell.Run([]string{"identity", "create", "three", "--issuer", "https://issuer.example",
		"--client-id", "c", "--provider", "nosuch"})
	if code != exit.Usage || !strings.Contains(errOut.String(), "shell.invalid_argument") {
		t.Errorf("provider: exit %d, stderr:\n%s", code, errOut)
	}
}
