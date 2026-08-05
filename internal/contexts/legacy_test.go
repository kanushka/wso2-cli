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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/contexts/fixture"
	"github.com/wso2/wso2-cli/sdk/problem"
)

func validV1() string {
	return `{
  "schemaVersion": 1,
  "defaultContext": "reference-local",
  "contexts": [
    {
      "name": "reference-local",
      "organizationId": "reference-org",
      "endpoint": "https://service.example.test",
      "auth": {"method": "development-credential", "credentialVariable": "WSO2_REFERENCE_DEV_CREDENTIAL"}
    }
  ]
}`
}

func TestLegacyReadMapsToSyntheticIdentity(t *testing.T) {
	document, err := contexts.Decode([]byte(validV1()))
	if err != nil {
		t.Fatalf("legacy decode: %v", err)
	}
	selection, err := document.Select("")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	identity := selection.Identity
	if !identity.Synthetic() {
		t.Fatal("v1 identity must be marked synthetic")
	}
	if identity.Auth.Kind != contexts.MethodDevelopmentCredential {
		t.Fatalf("kind = %q", identity.Auth.Kind)
	}
	if identity.Auth.CredentialVariable != "WSO2_REFERENCE_DEV_CREDENTIAL" {
		t.Fatalf("credential variable = %q", identity.Auth.CredentialVariable)
	}
	if identity.Products["reference"].Endpoint != "https://service.example.test" {
		t.Fatalf("products = %+v", identity.Products)
	}
	if selection.Context.Organization != "reference-org" {
		t.Fatalf("organization = %q", selection.Context.Organization)
	}
}

func TestALegacyContextWithoutAnEndpointCarriesNoProduct(t *testing.T) {
	doc := strings.Replace(validV1(), `"endpoint": "https://service.example.test",`, ``, 1)
	document, err := contexts.Decode([]byte(doc))
	if err != nil {
		t.Fatalf("legacy decode: %v", err)
	}
	if products := document.Identities[0].Products; len(products) != 0 {
		t.Fatalf("an endpointless v1 context grew products: %+v", products)
	}
}

func TestALegacyContextNamingAnotherMethodStillLoads(t *testing.T) {
	// Which methods a shell implements is broker policy. A v1 context this
	// release cannot authenticate against is refused when a command needs
	// access, not by making the document unreadable.
	doc := strings.Replace(validV1(), `"method": "development-credential"`, `"method": "browser-pkce"`, 1)
	document, err := contexts.Decode([]byte(doc))
	if err != nil {
		t.Fatalf("legacy decode: %v", err)
	}
	if document.Identities[0].Auth.Kind != "browser-pkce" {
		t.Fatalf("the mapped identity reports kind %q, want it as written", document.Identities[0].Auth.Kind)
	}
}

func TestSyntheticIdentityIsNotEncodable(t *testing.T) {
	document, err := contexts.Decode([]byte(validV1()))
	if err != nil {
		t.Fatalf("legacy decode: %v", err)
	}
	_, err = document.Encode()
	if err == nil {
		t.Fatal("encoding a compatibility document must fail: no automatic rewrite")
	}
	// The code matters: refusing because the schema version is unsupported
	// would pass this test with the compatibility guard deleted.
	assertProblemCode(t, err, "contexts.document_malformed")
}

func TestAnEmptyLegacyDocumentIsNotEncodable(t *testing.T) {
	// A v1 document with no contexts produces no synthetic identity to mark,
	// so the refusal cannot rest on the identities alone.
	document, err := contexts.Decode([]byte(`{"schemaVersion":1,"defaultContext":"","contexts":[]}`))
	if err != nil {
		t.Fatalf("legacy decode: %v", err)
	}

	_, err = document.Encode()

	assertProblemCode(t, err, "contexts.document_malformed")
}

func TestUnknownSchemaVersionFailsClosed(t *testing.T) {
	_, err := contexts.Decode([]byte(`{"schemaVersion": 3}`))
	assertProblemCode(t, err, "contexts.schema_unsupported")
}

func TestALegacyDocumentThisShellCannotReadFailsClosed(t *testing.T) {
	for name, contents := range map[string]string{
		"unnamed context":   `{"schemaVersion":1,"defaultContext":"a","contexts":[{"name":""}]}`,
		"duplicate context": `{"schemaVersion":1,"defaultContext":"a","contexts":[{"name":"a"},{"name":"a"}]}`,
		"unknown default":   `{"schemaVersion":1,"defaultContext":"b","contexts":[{"name":"a"}]}`,
		"credential in variable": `{"schemaVersion":1,"defaultContext":"a","contexts":[` +
			`{"name":"a","auth":{"method":"development-credential","credentialVariable":"not a variable name"}}]}`,
		"unreadable endpoint": `{"schemaVersion":1,"defaultContext":"a","contexts":[` +
			`{"name":"a","endpoint":"127.0.0.1:8080"}]}`,
		"endpoint embedding credentials": `{"schemaVersion":1,"defaultContext":"a","contexts":[` +
			`{"name":"a","endpoint":"http://operator:s3cr3t@127.0.0.1:8080"}]}`,
		"trailing document": `{"schemaVersion":1,"defaultContext":"a","contexts":[]}{"schemaVersion":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := contexts.Decode([]byte(contents))

			var typed problem.Problem
			if err == nil {
				t.Fatal("Decode accepted a legacy document this shell cannot read")
			}
			if !asProblem(t, err, &typed) || typed.Category != problem.CategoryUsage {
				t.Fatalf("Decode returned %v, want a usage problem", err)
			}
			if typed.Recovery == "" {
				t.Errorf("the problem %q offers no recovery guidance", typed.Code)
			}
			if strings.Contains(typed.Message+typed.Recovery, "s3cr3t") {
				t.Errorf("the refusal repeats the rejected endpoint: %+v", typed)
			}
		})
	}
}

func TestTheLegacyFixtureStillWritesAReadableV1Document(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	if err := fixture.Install(root, fixture.LegacyDocument{
		SchemaVersion:  contexts.SchemaVersionLegacy,
		DefaultContext: "reference-local",
		Contexts: []fixture.LegacyContext{{
			Name:           "reference-local",
			OrganizationID: "reference-org",
			Endpoint:       "https://service.example.test",
			Auth: fixture.LegacyAuth{
				Method:             contexts.MethodDevelopmentCredential,
				CredentialVariable: "WSO2_REFERENCE_DEV_CREDENTIAL",
			},
		}},
	}); err != nil {
		t.Fatalf("fixture.Install returned %v", err)
	}

	written, err := os.ReadFile(contexts.Path(root))
	if err != nil {
		t.Fatalf("cannot read the written context: %v", err)
	}
	if !strings.Contains(string(written), `"schemaVersion": 1`) {
		t.Fatalf("the legacy fixture did not write a v1 document:\n%s", written)
	}

	document, err := contexts.Load(root)
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}
	selection, err := document.Select("")
	if err != nil {
		t.Fatalf("Select returned %v", err)
	}
	if !selection.Identity.Synthetic() {
		t.Fatal("the loaded v1 document did not map to a synthetic identity")
	}
}

func TestTheLegacyFixtureRefusesToWriteIntoRealWSO2State(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("WSO2_HOME", "")

	if err := fixture.Install(filepath.Join(home, ".wso2"), fixture.LegacyDocument{
		SchemaVersion:  contexts.SchemaVersionLegacy,
		DefaultContext: "",
	}); err == nil {
		t.Fatal("the fixture wrote into the developer's real WSO2 state")
	}
}

func asProblem(t *testing.T, err error, target *problem.Problem) bool {
	t.Helper()
	return errors.As(err, target)
}
