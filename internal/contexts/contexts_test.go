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
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/contexts/fixture"
	"github.com/wso2/wso2-cli/sdk/problem"
)

func validV2() string {
	return `{
  "schemaVersion": 2,
  "defaultContext": "acme-dev",
  "identities": [
    {
      "name": "acme-cloud",
      "type": "cloud",
      "auth": {
        "kind": "oauth-browser",
        "issuer": "https://issuer.example.test/t/acme/oauth2/token",
        "clientId": "client-123",
        "tenant": "acme",
        "credentialRef": "acme-cloud-login"
      },
      "products": {
        "reference": {
          "endpoint": "https://api.example.test",
          "audience": "reference-status",
          "scopes": ["reference:status:read"]
        }
      }
    }
  ],
  "contexts": [
    {"name": "acme-dev", "identity": "acme-cloud", "organization": "acme"}
  ]
}`
}

// documentV2 is the in-memory equivalent of validV2 for tests that encode.
func documentV2() contexts.Document {
	return contexts.Document{
		SchemaVersion:  contexts.SchemaVersion,
		DefaultContext: "acme-dev",
		Identities: []contexts.Identity{{
			Name: "acme-cloud",
			Type: "cloud",
			Auth: contexts.IdentityAuth{
				Kind:          contexts.KindOAuthBrowser,
				Issuer:        "https://issuer.example.test/t/acme/oauth2/token",
				ClientID:      "client-123",
				Tenant:        "acme",
				CredentialRef: "acme-cloud-login",
			},
			Products: map[string]contexts.Product{
				"reference": {
					Endpoint: "https://api.example.test",
					Audience: "reference-status",
					Scopes:   []string{"reference:status:read"},
				},
			},
		}},
		Contexts: []contexts.Context{
			{Name: "acme-dev", Identity: "acme-cloud", Organization: "acme"},
		},
	}
}

func TestDecodeV2(t *testing.T) {
	document, err := contexts.Decode([]byte(validV2()))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(document.Identities) != 1 || document.Identities[0].Auth.Kind != contexts.KindOAuthBrowser {
		t.Fatalf("identity not decoded: %+v", document.Identities)
	}
	if document.Contexts[0].Identity != "acme-cloud" {
		t.Fatalf("context does not reference its identity: %+v", document.Contexts[0])
	}
}

func TestAllLegalKindsValidate(t *testing.T) {
	// Which kinds this release implements is broker policy; every legal kind
	// stays readable so a document written for a newer shell still loads.
	for name, mutate := range map[string]func(doc string) string{
		"oauth-browser": func(doc string) string { return doc },
		"oauth-device":  replace(`"kind": "oauth-browser"`, `"kind": "oauth-device"`),
		"pat":           replace(`"kind": "oauth-browser"`, `"kind": "pat"`),
		"client-credentials": func(doc string) string {
			return strings.NewReplacer(
				`"kind": "oauth-browser"`, `"kind": "client-credentials"`,
				`"credentialRef": "acme-cloud-login"`, `"clientSecretVariable": "WSO2_ACME_SECRET"`,
			).Replace(doc)
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := contexts.Decode([]byte(mutate(validV2()))); err != nil {
				t.Fatalf("a legal identity kind failed to validate: %v", err)
			}
		})
	}
}

func TestValidateV2(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(doc string) string
		code   string // expected problem code, "" for valid
	}{
		{"unknown kind rejected", replace(`"kind": "oauth-browser"`, `"kind": "password"`), "contexts.document_malformed"},
		{"context referencing unknown identity", replace(`"identity": "acme-cloud"`, `"identity": "ghost"`), "contexts.document_malformed"},
		{"credentialRef holding a JWT-shaped value", replace(`"credentialRef": "acme-cloud-login"`, `"credentialRef": "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJ4In0.c2ln"`), "contexts.document_malformed"},
		{"clientSecretVariable on browser kind", replace(`"credentialRef": "acme-cloud-login"`, `"clientSecretVariable": "MY_SECRET"`), "contexts.document_malformed"},
		{"missing issuer on browser kind", replace(`"issuer": "https://issuer.example.test/t/acme/oauth2/token",`, ``), "contexts.document_malformed"},
		{"missing clientId on browser kind", replace(`"clientId": "client-123",`, ``), "contexts.document_malformed"},
		{"issuer embedding credentials", replace(`"issuer": "https://issuer.example.test/t/acme/oauth2/token"`, `"issuer": "https://user:pass@issuer.example.test/t/acme/oauth2/token"`), "contexts.document_malformed"},
		{"product endpoint with embedded credentials", replace(`"endpoint": "https://api.example.test"`, `"endpoint": "https://user:pass@api.example.test"`), "contexts.document_malformed"},
		{"duplicate identity name", duplicateIdentity, "contexts.document_malformed"},
		{"duplicate context name", duplicateContext, "contexts.document_malformed"},
		{"invalid product namespace", replace(`"reference":`, `"Not A Namespace!":`), "contexts.document_malformed"},
		{"invalid identity type", replace(`"type": "cloud"`, `"type": "hybrid"`), "contexts.document_malformed"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := contexts.Decode([]byte(testCase.mutate(validV2())))
			assertProblemCode(t, err, testCase.code)
		})
	}
}

func TestValidateClientCredentialsIdentity(t *testing.T) {
	doc := strings.NewReplacer(
		`"kind": "oauth-browser"`, `"kind": "client-credentials"`,
		`"credentialRef": "acme-cloud-login"`, `"clientSecretVariable": "WSO2_ACME_SECRET"`,
	).Replace(validV2())
	if _, err := contexts.Decode([]byte(doc)); err != nil {
		t.Fatalf("client-credentials identity should validate: %v", err)
	}
	// A lowercase variable name is a value-shaped mistake, not a name.
	bad := strings.Replace(doc, `"clientSecretVariable": "WSO2_ACME_SECRET"`, `"clientSecretVariable": "actual-secret-value"`, 1)
	_, err := contexts.Decode([]byte(bad))
	assertProblemCode(t, err, "contexts.document_malformed")
}

func TestTheSelectedContextIsTheDefaultOne(t *testing.T) {
	root := install(t, documentV2())

	loaded, err := contexts.Load(root)
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}
	selection, err := loaded.Select("")
	if err != nil {
		t.Fatalf("Select returned %v", err)
	}

	if selection.Context.Name != "acme-dev" || selection.Context.Organization != "acme" {
		t.Fatalf("Select(%q) = %+v, want the default context", "", selection.Context)
	}
	if selection.Identity.Name != "acme-cloud" || selection.Identity.Auth.Kind != contexts.KindOAuthBrowser {
		t.Fatalf("the selection does not carry its identity: %+v", selection.Identity)
	}
}

func TestSelectingAnUnknownContextIsRefused(t *testing.T) {
	document, err := contexts.Decode([]byte(validV2()))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	_, err = document.Select("ghost")
	assertProblemCode(t, err, "contexts.unknown_context")
}

func TestAContextRecordsNoCredentialValue(t *testing.T) {
	// The document names where credentials come from. It must have nowhere to
	// put a credential itself, so a reviewer can prove the absence from the
	// types rather than from every writer of them.
	allowedContext := []string{"name", "identity", "organization", "project"}
	allowedIdentity := []string{"name", "type", "auth", "products"}
	allowedAuth := []string{"kind", "issuer", "clientId", "tenant", "credentialRef", "clientSecretVariable"}
	allowedProduct := []string{"endpoint", "audience", "scopes"}

	if got := jsonMembers(t, contexts.Context{}); !slices.Equal(got, allowedContext) {
		t.Errorf("a context records %v; it may record only %v", got, allowedContext)
	}
	if got := jsonMembers(t, contexts.Identity{}); !slices.Equal(got, allowedIdentity) {
		t.Errorf("an identity records %v; it may record only %v", got, allowedIdentity)
	}
	if got := jsonMembers(t, contexts.IdentityAuth{}); !slices.Equal(got, allowedAuth) {
		t.Errorf("an identity's authentication records %v; it may record only %v", got, allowedAuth)
	}
	if got := jsonMembers(t, contexts.Product{}); !slices.Equal(got, allowedProduct) {
		t.Errorf("a product records %v; it may record only %v", got, allowedProduct)
	}
}

func TestAWrittenDocumentCarriesNoCredentialValue(t *testing.T) {
	const secret = "canary-client-secret-2f8c"
	t.Setenv("WSO2_ACME_SECRET", secret)
	document := documentV2()
	document.Identities[0].Auth = contexts.IdentityAuth{
		Kind:                 contexts.KindClientCredentials,
		Issuer:               "https://issuer.example.test/t/acme/oauth2/token",
		ClientID:             "client-123",
		ClientSecretVariable: "WSO2_ACME_SECRET",
	}
	root := install(t, document)

	written, err := os.ReadFile(contexts.Path(root))
	if err != nil {
		t.Fatalf("cannot read the written context: %v", err)
	}

	if strings.Contains(string(written), secret) {
		t.Fatalf("the context document carries the secret value:\n%s", written)
	}
	if !strings.Contains(string(written), "WSO2_ACME_SECRET") {
		t.Fatalf("the context document does not name the secret source:\n%s", written)
	}
}

func TestAMissingContextDocumentSelectsAnEmptyDefaultContext(t *testing.T) {
	// A shell with no context store still runs a command. A module that needs
	// access is refused by the broker, with guidance; one that does not is
	// unaffected.
	loaded, err := contexts.Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}

	selection, err := loaded.Select("")
	if err != nil {
		t.Fatalf("Select returned %v", err)
	}
	if selection.Context.Name != contexts.DefaultName {
		t.Errorf("the fallback context is named %q, want %q", selection.Context.Name, contexts.DefaultName)
	}
	if selection.Context.Organization != "" || selection.Identity.Name != "" || selection.Identity.Auth.Kind != "" {
		t.Errorf("the fallback selection is not empty: %+v", selection)
	}
}

func TestADocumentThisShellCannotReadFailsClosed(t *testing.T) {
	for name, contents := range map[string]string{
		"not JSON":           "{",
		"two documents":      validV2() + `{"schemaVersion":2}`,
		"unsupported schema": `{"schemaVersion":99,"defaultContext":"a","contexts":[]}`,
		"unnamed context": `{"schemaVersion":2,"defaultContext":"a","identities":[],` +
			`"contexts":[{"name":"","identity":"x"}]}`,
		"unknown default": `{"schemaVersion":2,"defaultContext":"b",` +
			`"identities":[{"name":"i","type":"onprem","auth":{"kind":"pat","credentialRef":"i-login"}}],` +
			`"contexts":[{"name":"a","identity":"i"}]}`,
		"unreadable product endpoint": `{"schemaVersion":2,"defaultContext":"a",` +
			`"identities":[{"name":"i","type":"onprem",` +
			`"auth":{"kind":"pat","credentialRef":"i-login"},` +
			`"products":{"reference":{"endpoint":"127.0.0.1:8080"}}}],` +
			`"contexts":[{"name":"a","identity":"i"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Dir(contexts.Path(root)), 0o755); err != nil {
				t.Fatalf("cannot create the context directory: %v", err)
			}
			if err := os.WriteFile(contexts.Path(root), []byte(contents), 0o644); err != nil {
				t.Fatalf("cannot write the context document: %v", err)
			}

			_, err := contexts.Load(root)

			var typed problem.Problem
			if err == nil {
				t.Fatal("Load accepted a document this shell cannot read")
			}
			if !errors.As(err, &typed) || typed.Category != problem.CategoryUsage {
				t.Fatalf("Load returned %v, want a usage problem", err)
			}
			if typed.Recovery == "" {
				t.Errorf("the problem %q offers no recovery guidance", typed.Code)
			}
		})
	}
}

func TestAnEndpointThatEmbedsCredentialsIsRefused(t *testing.T) {
	// The endpoint reaches the module. A credential written into its URL would
	// hand one over through the member nobody thinks of as carrying
	// credentials, so the document is refused and the endpoint is not echoed.
	document := documentV2()
	product := document.Identities[0].Products["reference"]
	product.Endpoint = "http://operator:s3cr3t@127.0.0.1:8080"
	document.Identities[0].Products["reference"] = product

	_, err := document.Encode()

	var typed problem.Problem
	if err == nil {
		t.Fatal("a product embedding credentials in its endpoint was accepted")
	}
	if !errors.As(err, &typed) || typed.Category != problem.CategoryUsage {
		t.Fatalf("Encode returned %v, want a usage problem", err)
	}
	if strings.Contains(typed.Message+typed.Recovery, "s3cr3t") {
		t.Fatalf("the refusal repeats the endpoint's credentials: %+v", typed)
	}
}

func TestARejectedEndpointIsNeverEchoed(t *testing.T) {
	document := documentV2()
	product := document.Identities[0].Products["reference"]
	product.Endpoint = "not an endpoint with s3cr3t in it"
	document.Identities[0].Products["reference"] = product

	_, err := document.Encode()

	if err == nil {
		t.Fatal("an unreadable endpoint was accepted")
	}
	if strings.Contains(err.Error(), "s3cr3t") {
		t.Fatalf("the refusal repeats the rejected endpoint: %v", err)
	}
}

func TestARejectedReferenceIsNeverEchoed(t *testing.T) {
	// What was pasted where a reference belongs may be a credential, so the
	// refusal must not repeat it.
	doc := replace(`"credentialRef": "acme-cloud-login"`,
		`"credentialRef": "eyJhbGciOiJSUzI1NiJ9.c3VidGxlLXNlY3JldA.c2ln"`)(validV2())

	_, err := contexts.Decode([]byte(doc))

	if err == nil {
		t.Fatal("a credential-shaped reference was accepted")
	}
	if strings.Contains(err.Error(), "eyJhbGciOiJSUzI1NiJ9") {
		t.Fatalf("the refusal repeats the rejected value: %v", err)
	}
}

func TestTheFixtureRefusesToWriteIntoRealWSO2State(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("WSO2_HOME", "")

	if err := fixture.WriteV2(filepath.Join(home, ".wso2"), documentV2()); err == nil {
		t.Fatal("the fixture wrote into the developer's real WSO2 state")
	}
}

// install writes the document into an isolated state root and returns the root.
func install(t *testing.T, document contexts.Document) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "state")
	if err := fixture.WriteV2(root, document); err != nil {
		t.Fatalf("fixture.WriteV2 returned %v", err)
	}
	return root
}

// replace builds a document mutation that swaps one exact substring.
func replace(old, new string) func(doc string) string {
	return func(doc string) string {
		return strings.Replace(doc, old, new, 1)
	}
}

// duplicateIdentity appends a copy of the first identity to the document.
func duplicateIdentity(doc string) string {
	return duplicateElement(doc, "identities")
}

// duplicateContext appends a copy of the first context to the document.
func duplicateContext(doc string) string {
	return duplicateElement(doc, "contexts")
}

func duplicateElement(doc, member string) string {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(doc), &parsed); err != nil {
		panic(err)
	}
	elements := parsed[member].([]any)
	parsed[member] = append(elements, elements[0])
	out, err := json.Marshal(parsed)
	if err != nil {
		panic(err)
	}
	return string(out)
}

// assertProblemCode proves an error is a typed problem with the given code.
func assertProblemCode(t *testing.T, err error, code string) {
	t.Helper()
	if code == "" {
		if err != nil {
			t.Fatalf("expected the document to be accepted, got %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("expected the problem %q, got no error", code)
	}
	var typed problem.Problem
	if !errors.As(err, &typed) {
		t.Fatalf("expected a typed problem, got %v", err)
	}
	if typed.Code != code {
		t.Fatalf("problem code = %q, want %q", typed.Code, code)
	}
}

// jsonMembers reports the JSON member names a value serializes to, in order.
func jsonMembers(t *testing.T, value any) []string {
	t.Helper()
	structure := reflect.TypeOf(value)
	members := make([]string, 0, structure.NumField())
	for index := range structure.NumField() {
		tag, _, _ := strings.Cut(structure.Field(index).Tag.Get("json"), ",")
		if tag == "" || tag == "-" {
			continue
		}
		members = append(members, tag)
	}
	return members
}
