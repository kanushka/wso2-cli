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

package acceptance_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/contexts"
	contextfixture "github.com/wso2/wso2-cli/internal/contexts/fixture"
	"github.com/wso2/wso2-cli/internal/modules/fixture"
	"github.com/wso2/wso2-cli/internal/state"
)

// The amp module is the worked example the module-author guide follows, so
// this file proves under the real shell what the guide claims about it: the
// module is launched from a receipt, asks the broker for its declared audience
// and no scopes, is handed a token minted for exactly the scopes the identity's
// product entry records, and presents that token to the product API under the
// path Agent Manager serves. The product is a fake; the shell, the module, and
// the token are real.
const (
	ampAudience       = "amp-api"
	ampProjectRead    = "amp:project:read"
	ampAgentRead      = "amp:agent:read"
	ampOrganization   = "acme"
	ampIdentityName   = "amp-machine"
	ampContextName    = "amp-local"
	ampSecretVariable = "WSO2_AMP_CLIENT_SECRET"
	ampClientSecret   = "canary-amp-client-secret-9c2e"
)

// fakeAgentManager answers the two listings the module reads and records the
// bearer each request presented.
type fakeAgentManager struct {
	server    *httptest.Server
	mu        sync.Mutex
	presented []string
	paths     []string
}

func startFakeAgentManager(t *testing.T) *fakeAgentManager {
	t.Helper()
	fake := &fakeAgentManager{}
	record := func(r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		fake.presented = append(fake.presented, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		fake.paths = append(fake.paths, r.URL.RequestURI())
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/orgs/"+ampOrganization+"/projects", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		_ = json.NewEncoder(w).Encode(map[string]any{"total": 1, "projects": []map[string]any{
			{"name": "payments", "displayName": "Payments", "createdAt": "2026-09-01T10:00:00Z"},
		}})
	})
	mux.HandleFunc("GET /api/v1/orgs/"+ampOrganization+"/projects/payments/agents", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		_ = json.NewEncoder(w).Encode(map[string]any{"total": 1, "agents": []map[string]any{
			{"name": "refund-bot", "displayName": "Refund bot", "status": "Running", "createdAt": "2026-09-02T10:00:00Z"},
		}})
	})
	fake.server = httptest.NewServer(mux)
	t.Cleanup(fake.server.Close)
	return fake
}

// buildAmpModule builds the amp module the way the reference module is built
// for these tests: against the test protocol version, so the receipt and the
// handshake agree.
func buildAmpModule(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "wso2-module-amp"+executableSuffix())
	ldflags := strings.Join([]string{
		"-X main.moduleVersion=" + testModuleVersion,
		"-X github.com/wso2/wso2-cli/sdk/module.SDKVersion=" + testSDKVersion,
		"-X github.com/wso2/wso2-cli/sdk/protocol.Version=" + testProtocolVersion,
	}, " ")
	build(t, filepath.Join(repoRoot(t), "modules", "amp"), binary, ldflags, "./cmd/wso2-module-amp")
	return binary
}

// deployAmp installs the amp module and writes a client-credentials identity
// whose amp product entry points at the fake, exactly as wso2 identity
// add-product would record it.
func deployAmp(t *testing.T) (stateRoot string, environment []string, product *fakeAgentManager, issuer *fakeissuer.Issuer) {
	t.Helper()
	stateRoot = isolatedStateRoot(t)
	binary := buildAmpModule(t)
	if _, err := fixture.Install(state.ModuleStore(stateRoot), fixture.Module{
		Namespace:        "amp",
		Version:          testModuleVersion,
		ShellRange:       ">=0.1.0 <1.0.0",
		ProtocolVersions: []int{testProtocolVersionNumber},
		SourcePath:       binary,
		AuthAudiences:    []string{ampAudience},
		AuthScopes:       []string{ampProjectRead, ampAgentRead},
		// The tree the module declares, as the installer records it, so the
		// shell parses this module's flags and --output can follow them.
		CommandTree: extractCommandTree(t, binary),
	}); err != nil {
		t.Fatalf("fixture.Install returned %v", err)
	}
	issuer = fakeissuer.New(t, fakeissuer.Options{Audience: ampAudience, ClientSecret: ampClientSecret})
	product = startFakeAgentManager(t)
	if err := contextfixture.WriteV2(stateRoot, contexts.Document{
		SchemaVersion:  contexts.SchemaVersion,
		DefaultContext: ampContextName,
		Identities: []contexts.Identity{{
			Name: ampIdentityName,
			Type: "cloud",
			Auth: contexts.IdentityAuth{
				Kind:                 contexts.KindClientCredentials,
				Issuer:               issuer.URL,
				ClientID:             oauthClientID,
				Tenant:               ampOrganization,
				ClientSecretVariable: ampSecretVariable,
			},
			Products: map[string]contexts.Product{
				"amp": {Endpoint: product.server.URL, Audience: ampAudience, Scopes: []string{ampProjectRead, ampAgentRead}},
			},
		}},
		Contexts: []contexts.Context{{Name: ampContextName, Identity: ampIdentityName, Organization: ampOrganization}},
	}); err != nil {
		t.Fatalf("installing the context document: %v", err)
	}
	return stateRoot, shellEnvironment(stateRoot, ampSecretVariable+"="+ampClientSecret), product, issuer
}

func TestTheAmpModuleListsProjectsAndAgentsWithIssuerMintedAccess(t *testing.T) {
	shell := buildShell(t)
	_, environment, product, issuer := deployAmp(t)

	stdout, stderr, err := runShellWith(shell, environment, "amp", "projects", "list")
	if err != nil {
		t.Fatalf("wso2 amp projects list failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	for _, want := range []string{ampOrganization, "payments (Payments, 2026-09-01)", "agents list"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the table does not report %q:\n%s", want, stdout)
		}
	}

	// --org is the module's own flag and the organization is otherwise the
	// context's; both reach the same path.
	stdout, stderr, err = runShellWith(shell, environment, "amp", "agents", "list", "--project", "payments", "--output", "json")
	if err != nil {
		t.Fatalf("wso2 amp agents list failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var rendered map[string]string
	if decodeErr := json.Unmarshal([]byte(stdout), &rendered); decodeErr != nil {
		t.Fatalf("the JSON output is not an object of strings: %v\n%s", decodeErr, stdout)
	}
	if rendered["project"] != "payments" || rendered["count"] != "1" ||
		!strings.Contains(rendered["agents"], "refund-bot (Refund bot, Running)") {
		t.Errorf("agents list rendered %+v", rendered)
	}

	product.mu.Lock()
	defer product.mu.Unlock()
	wantPaths := []string{"/api/v1/orgs/acme/projects", "/api/v1/orgs/acme/projects/payments/agents"}
	if !slices.Equal(product.paths, wantPaths) {
		t.Errorf("the product was called at %v, want %v", product.paths, wantPaths)
	}
	// The module asked for no scopes, so what it presented is a token the
	// issuer minted for exactly the scopes the identity's product entry
	// records, bound to the audience the module declared.
	for _, token := range product.presented {
		active, scopes, audiences := issuer.Introspect(t, token)
		if !active {
			t.Fatal("the module presented a token the issuer did not mint")
		}
		if got := slices.Sorted(slices.Values(scopes)); !slices.Equal(got, []string{ampAgentRead, ampProjectRead}) {
			t.Errorf("presented scopes %v, want exactly the recorded %v", scopes, []string{ampAgentRead, ampProjectRead})
		}
		if !slices.Contains(audiences, ampAudience) {
			t.Errorf("presented audience %v, want %s", audiences, ampAudience)
		}
	}
	assertNoCredentialDisclosure(t, stdout, stderr)
}

func TestTheAmpModuleRefusesConnectAndNamesAddProduct(t *testing.T) {
	// The module declares no product descriptor, because Agent Manager's issuer
	// lives at a host the product URL does not name. The shell has to say so
	// and point at the command that records the product by hand.
	shell := buildShell(t)
	_, environment, _, _ := deployAmp(t)

	stdout, stderr, err := runShellWith(shell, environment, "amp", "connect", "http://localhost:9000")
	if err == nil {
		t.Fatalf("connect succeeded on a module with no descriptor:\n%s", stdout)
	}
	if !strings.Contains(stderr, "shell.connect_unsupported") || !strings.Contains(stderr, "wso2 identity add-product") {
		t.Errorf("the refusal does not name the code and the recovery:\n%s", stderr)
	}
}
