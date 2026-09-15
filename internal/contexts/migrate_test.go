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

package contexts_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/contexts"
)

// v3 assembles a schema version 3 document from account and context JSON.
func v3(defaultContext, accounts, contextList string) string {
	return `{"schemaVersion": 3, "defaultContext": "` + defaultContext + `", "accounts": [` + accounts +
		`], "contexts": [` + contextList + `]}`
}

const browserAccount = `{"name": "demo", "type": "onprem",
  "auth": {"kind": "oauth-browser", "issuer": "http://localhost:8501", "clientId": "wso2-cli",
           "credentialRef": "demo", "provider": "thunder"},
  "products": {
    "identity": {"endpoint": "http://localhost:8501", "audience": "https://localhost:8090/mcp", "scopes": ["system"]},
    "api": {"endpoint": "http://localhost:9251", "audience": "http://localhost:9251", "grant": {"kind": "exchange"},
            "gateway": {"endpoint": "http://localhost:9091", "audience": "http://localhost:9091"}}
  },
  "loginProduct": "identity"}`

func decodeMigrated(t *testing.T, document string) (contexts.Document, contexts.Migration) {
	t.Helper()
	decoded, err := contexts.Decode([]byte(document))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	migration, migrated := decoded.Migrated()
	if !migrated {
		t.Fatal("the document was not reported as migrated")
	}
	if _, err := decoded.Encode(); err != nil {
		t.Fatalf("the migrated document does not encode: %v", err)
	}
	return decoded, migration
}

func TestMigratingOneAccountWithOneContextKeepsItsSessions(t *testing.T) {
	document, migration := decodeMigrated(t, v3("demo", browserAccount,
		`{"name": "demo", "account": "demo", "organization": "acme", "project": "p1"}`))
	if len(document.Contexts) != 1 {
		t.Fatalf("contexts = %+v", document.Contexts)
	}
	got := document.Contexts[0]
	if got.CredentialRef != "demo" || got.Login.Issuer != "http://localhost:8501" ||
		got.Login.Provider != "thunder" || got.Login.Product != "identity" ||
		got.Organization != "acme" || got.Project != "p1" || got.Type != "onprem" {
		t.Fatalf("context = %+v", got)
	}
	if len(migration.Relogin) != 0 || len(migration.Adopted) != 0 || len(migration.Notes()) != 0 {
		t.Fatalf("a one-to-one migration reported %+v", migration)
	}
	api := got.Products["api"]
	if api.Endpoint != "http://localhost:9251" || api.Gateway == nil || api.Gateway.Endpoint != "http://localhost:9091" ||
		api.Grant == nil || api.Grant.Kind != contexts.GrantExchange {
		t.Fatalf("api = %+v", api)
	}
	encoded, _ := document.Encode()
	if strings.Contains(string(encoded), `"endpoint"`) || !strings.Contains(string(encoded), `"url": "http://localhost:9091"`) {
		t.Fatalf("endpoint was not renamed url on every product and gateway:\n%s", encoded)
	}
}

func TestMigratingASharedAccountGivesItsSessionsToTheSelectedContext(t *testing.T) {
	document, migration := decodeMigrated(t, v3("beta", browserAccount,
		`{"name": "alpha", "account": "demo"}, {"name": "beta", "account": "demo"}, {"name": "gamma", "account": "demo"}`))
	refs := map[string]string{}
	for _, context := range document.Contexts {
		refs[context.Name] = context.CredentialRef
	}
	if refs["beta"] != "demo" {
		t.Fatalf("the selected context did not keep the account's reference: %v", refs)
	}
	if refs["alpha"] == "demo" || refs["gamma"] == "demo" || refs["alpha"] == refs["gamma"] {
		t.Fatalf("the other contexts share a reference: %v", refs)
	}
	if !slices.Equal(migration.Relogin, []string{"alpha", "gamma"}) {
		t.Fatalf("relogin = %v, want alpha and gamma", migration.Relogin)
	}
	notes := strings.Join(migration.Notes(), "\n")
	if !strings.Contains(notes, "wso2 login --context alpha") || !strings.Contains(notes, "wso2 login --context gamma") {
		t.Fatalf("the notes do not name the logins needed:\n%s", notes)
	}
}

func TestMigratingASharedAccountOutsideTheSelectionGivesItsSessionsToTheFirstByName(t *testing.T) {
	other := strings.NewReplacer(`"name": "demo"`, `"name": "other"`, `"credentialRef": "demo"`,
		`"credentialRef": "other"`).Replace(browserAccount)
	document, migration := decodeMigrated(t, v3("solo", browserAccount+","+other,
		`{"name": "zeta", "account": "demo"}, {"name": "beta", "account": "demo"}, {"name": "solo", "account": "other"}`))
	refs := map[string]string{}
	for _, context := range document.Contexts {
		refs[context.Name] = context.CredentialRef
	}
	if refs["beta"] != "demo" || refs["solo"] != "other" || refs["zeta"] == "demo" {
		t.Fatalf("refs = %v, want beta to keep demo", refs)
	}
	if !slices.Equal(migration.Relogin, []string{"zeta"}) {
		t.Fatalf("relogin = %v", migration.Relogin)
	}
}

func TestAnAccountNoContextNamedBecomesAContext(t *testing.T) {
	orphan := strings.NewReplacer(`"name": "demo"`, `"name": "spare"`, `"credentialRef": "demo"`,
		`"credentialRef": "spare"`).Replace(browserAccount)
	document, migration := decodeMigrated(t, v3("demo", browserAccount+","+orphan,
		`{"name": "demo", "account": "demo"}`))
	adopted, found := document.Find("spare")
	if !found || adopted.CredentialRef != "spare" {
		t.Fatalf("the orphan account was not kept as a context: %+v", document.Contexts)
	}
	if !slices.Equal(migration.Adopted, []string{"spare"}) {
		t.Fatalf("adopted = %v", migration.Adopted)
	}
}

func TestAnOrphanAccountWhoseNameIsTakenGetsASuffix(t *testing.T) {
	orphan := strings.NewReplacer(`"name": "demo"`, `"name": "prod"`, `"credentialRef": "demo"`,
		`"credentialRef": "prod-login"`).Replace(browserAccount)
	document, migration := decodeMigrated(t, v3("prod", browserAccount+","+orphan,
		`{"name": "prod", "account": "demo"}`))
	if _, found := document.Find("prod-2"); !found {
		t.Fatalf("the orphan was not given a suffixed name: %+v", document.Contexts)
	}
	if migration.Renamed["prod"] != "prod-2" || !strings.Contains(strings.Join(migration.Notes(), " "), "prod-2") {
		t.Fatalf("the rename was not reported: %+v", migration)
	}
}

func TestMigrationFreezesAnUnpinnedLoginProduct(t *testing.T) {
	unpinned := strings.Replace(browserAccount, `,
  "loginProduct": "identity"`, "", 1)
	document, _ := decodeMigrated(t, v3("demo", unpinned, `{"name": "demo", "account": "demo"}`))
	// identity is the only direct product, so it is the effective login
	// product, and it is written out.
	if got := document.Contexts[0].Login.Product; got != "identity" {
		t.Fatalf("login product = %q, want the effective identity frozen", got)
	}
}

func TestMigrationCarriesEveryKind(t *testing.T) {
	cases := map[string]string{
		"oauth-device": strings.Replace(browserAccount, `"oauth-browser"`, `"oauth-device"`, 1),
		"pat":          strings.Replace(browserAccount, `"oauth-browser"`, `"pat"`, 1),
		"client-credentials": `{"name": "demo", "type": "onprem",
  "auth": {"kind": "client-credentials", "issuer": "https://is.example", "clientId": "ci", "clientSecretVariable": "CI_SECRET"},
  "products": {"apim": {"endpoint": "https://apim.example", "audience": "https://apim.example",
    "grant": {"kind": "jwt-bearer", "issuer": "https://apim.example/oauth2/token", "clientId": "apim-cli", "resource": "https://apim.example"},
    "clientIdVariable": "APIM_ID", "clientSecretVariable": "APIM_SECRET"}}}`,
	}
	for kind, account := range cases {
		t.Run(kind, func(t *testing.T) {
			document, _ := decodeMigrated(t, v3("demo", account, `{"name": "demo", "account": "demo"}`))
			got := document.Contexts[0]
			if got.Login.Kind != kind {
				t.Fatalf("kind = %q", got.Login.Kind)
			}
			if kind == "client-credentials" {
				apim := got.Products["apim"]
				if got.CredentialRef != "" || got.Login.ClientSecretVariable != "CI_SECRET" ||
					apim.ClientIDVariable != "APIM_ID" || apim.ClientSecretVariable != "APIM_SECRET" ||
					apim.Grant.Resource != "https://apim.example" || got.Login.Product != "" {
					t.Fatalf("client-credentials context = %+v", got)
				}
			}
		})
	}
}

func TestMigrationCarriesGrantsGatewaysProviderAndNarrowing(t *testing.T) {
	account := `{"name": "demo", "type": "onprem",
  "auth": {"kind": "oauth-browser", "issuer": "https://is.example", "clientId": "cli", "credentialRef": "demo",
           "provider": "thunder", "narrowing": "scoped-refresh"},
  "products": {
    "reference": {"endpoint": "https://ref.example", "audience": "reference", "scopes": ["r"]},
    "agent": {"endpoint": "https://agent.example", "audience": "agent",
              "grant": {"kind": "jwt-bearer", "issuer": "https://agent.example/token", "clientId": "agent-cli"}},
    "apim": {"endpoint": "https://apim.example", "audience": "apim",
             "grant": {"kind": "federated", "issuer": "https://apim.example/token", "clientId": "apim-cli", "resource": "https://apim.example"},
             "gateway": {"endpoint": "https://gw.example", "audience": "https://gw.example/hello", "scopes": ["hello:read"]}}
  }}`
	document, _ := decodeMigrated(t, v3("demo", account, `{"name": "demo", "account": "demo"}`))
	got := document.Contexts[0]
	if got.Login.Narrowing != "scoped-refresh" || got.Account().Auth.Derivation() != contexts.DerivationScopedRefresh {
		t.Fatalf("narrowing did not override the provider: %+v", got.Login)
	}
	if got.Products["agent"].Grant.Kind != contexts.GrantJWTBearer || got.Products["apim"].Grant.Resource != "https://apim.example" {
		t.Fatalf("grants = %+v", got.Products)
	}
	gateway := got.Products["apim"].Gateway
	if gateway == nil || gateway.Endpoint != "https://gw.example" || !slices.Equal(gateway.Scopes, []string{"hello:read"}) {
		t.Fatalf("gateway = %+v", gateway)
	}
}

func TestAContextNamingAMissingAccountIsStillRefused(t *testing.T) {
	_, err := contexts.Decode([]byte(v3("demo", browserAccount, `{"name": "demo", "account": "ghost"}`)))
	assertProblemCode(t, err, "contexts.document_malformed")
}

func TestTwoContextsDeclaringOneCredentialReferenceAreRefused(t *testing.T) {
	document, _ := decodeMigrated(t, v3("demo", browserAccount, `{"name": "demo", "account": "demo"}`))
	twin := document.Contexts[0]
	twin.Name = "twin"
	document.Contexts = append(document.Contexts, twin)
	_, err := document.Encode()
	assertProblemCode(t, err, "contexts.document_malformed")
	if err == nil || !strings.Contains(err.Error(), "same credential reference") {
		t.Fatalf("err = %v, want the shared reference named", err)
	}
}

func TestAnInteractiveContextWithoutItsLoginProductIsRefusedOnRead(t *testing.T) {
	document, _ := decodeMigrated(t, v3("demo", browserAccount, `{"name": "demo", "account": "demo"}`))
	encoded, _ := document.Encode()
	stripped := strings.Replace(string(encoded), `"product": "identity"`, `"product": ""`, 1)
	_, err := contexts.Decode([]byte(stripped))
	assertProblemCode(t, err, "contexts.document_malformed")
}

func TestUpgradeRewritesAnEarlierDocumentOnceAndReportsIt(t *testing.T) {
	root := t.TempDir()
	path := contexts.Path(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(v3("alpha", browserAccount,
		`{"name": "alpha", "account": "demo"}, {"name": "beta", "account": "demo"}`)), 0o600); err != nil {
		t.Fatal(err)
	}
	migration, migrated, err := contexts.Upgrade(root)
	if err != nil || !migrated || !slices.Equal(migration.Relogin, []string{"beta"}) {
		t.Fatalf("Upgrade = %+v, %v, %v", migration, migrated, err)
	}
	written, _ := os.ReadFile(path)
	if !strings.Contains(string(written), `"schemaVersion": 4`) {
		t.Fatalf("the document was not rewritten:\n%s", written)
	}
	if _, migrated, _ := contexts.Upgrade(root); migrated {
		t.Fatal("a current document was upgraded again")
	}
}

func TestANewerSchemaIsNeitherReadNorOverwritten(t *testing.T) {
	root := t.TempDir()
	path := contexts.Path(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	newer := []byte(`{"schemaVersion": 5, "contexts": []}`)
	if err := os.WriteFile(path, newer, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := contexts.Load(root); err == nil {
		t.Fatal("a newer schema was read")
	}
	err := contexts.Save(root, contexts.Document{SchemaVersion: contexts.SchemaVersion})
	assertProblemCode(t, err, "contexts.document_frozen")
	after, _ := os.ReadFile(path)
	if string(after) != string(newer) {
		t.Fatal("a newer document was overwritten")
	}
}
