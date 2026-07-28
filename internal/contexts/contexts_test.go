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
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/contexts/fixture"
	"github.com/wso2/wso2-cli/sdk/problem"
)

func reference() contexts.Context {
	return contexts.Context{
		Name:           "reference-local",
		OrganizationID: "reference-org",
		Endpoint:       "http://127.0.0.1:8080",
		Auth: contexts.Auth{
			Method:             contexts.MethodDevelopmentCredential,
			CredentialVariable: "WSO2_REFERENCE_DEV_CREDENTIAL",
		},
	}
}

func document() contexts.Document {
	return contexts.Document{
		SchemaVersion:  contexts.SchemaVersion,
		DefaultContext: "reference-local",
		Contexts:       []contexts.Context{reference()},
	}
}

func TestTheSelectedContextIsTheDefaultOne(t *testing.T) {
	root := install(t, document())

	loaded, err := contexts.Load(root)
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}
	selected, err := loaded.Selected()
	if err != nil {
		t.Fatalf("Selected returned %v", err)
	}

	if !reflect.DeepEqual(selected, reference()) {
		t.Fatalf("Selected() = %+v, want %+v", selected, reference())
	}
}

func TestAContextRecordsNoCredentialValue(t *testing.T) {
	// The context names where a credential comes from. It must have nowhere to
	// put the credential itself, so a reviewer can prove the absence from the
	// type rather than from every writer of it.
	allowed := []string{"name", "organizationId", "endpoint", "auth"}
	allowedAuth := []string{"method", "credentialVariable"}

	if got := jsonMembers(t, contexts.Context{}); !slices.Equal(got, allowed) {
		t.Errorf("a context records %v; it may record only %v", got, allowed)
	}
	if got := jsonMembers(t, contexts.Auth{}); !slices.Equal(got, allowedAuth) {
		t.Errorf("a context's authentication records %v; it may record only %v", got, allowedAuth)
	}
}

func TestAWrittenContextCarriesNoCredentialValue(t *testing.T) {
	const credential = "canary-source-credential-2f8c"
	t.Setenv("WSO2_REFERENCE_DEV_CREDENTIAL", credential)
	root := install(t, document())

	written, err := os.ReadFile(contexts.Path(root))
	if err != nil {
		t.Fatalf("cannot read the written context: %v", err)
	}

	if strings.Contains(string(written), credential) {
		t.Fatalf("the context document carries the credential value:\n%s", written)
	}
	if !strings.Contains(string(written), "WSO2_REFERENCE_DEV_CREDENTIAL") {
		t.Fatalf("the context document does not name the credential source:\n%s", written)
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

	selected, err := loaded.Selected()
	if err != nil {
		t.Fatalf("Selected returned %v", err)
	}
	if selected.Name != contexts.DefaultName {
		t.Errorf("the fallback context is named %q, want %q", selected.Name, contexts.DefaultName)
	}
	if selected.OrganizationID != "" || selected.Endpoint != "" || selected.Auth.CredentialVariable != "" {
		t.Errorf("the fallback context is not empty: %+v", selected)
	}
}

func TestAContextNamingAnotherAuthenticationMethodStillLoads(t *testing.T) {
	// Which methods a shell implements is broker policy. A context this
	// release cannot authenticate against is refused when a command needs
	// access, not by making every other context unreadable.
	document := document()
	document.Contexts[0].Auth.Method = "browser-pkce"
	root := install(t, document)

	loaded, err := contexts.Load(root)
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}
	selected, err := loaded.Selected()
	if err != nil {
		t.Fatalf("Selected returned %v", err)
	}
	if selected.Auth.Method != "browser-pkce" {
		t.Errorf("the selected context reports method %q, want it as written", selected.Auth.Method)
	}
}

func TestADocumentThisShellCannotReadFailsClosed(t *testing.T) {
	for name, contents := range map[string]string{
		"not JSON":           "{",
		"two documents":      `{"schemaVersion":1,"defaultContext":"a","contexts":[]}{"schemaVersion":1}`,
		"unsupported schema": `{"schemaVersion":99,"defaultContext":"a","contexts":[]}`,
		"unnamed context":    `{"schemaVersion":1,"defaultContext":"a","contexts":[{"name":""}]}`,
		"duplicate context":  `{"schemaVersion":1,"defaultContext":"a","contexts":[{"name":"a"},{"name":"a"}]}`,
		"unknown default":    `{"schemaVersion":1,"defaultContext":"b","contexts":[{"name":"a"}]}`,
		"credential in variable": `{"schemaVersion":1,"defaultContext":"a","contexts":[` +
			`{"name":"a","auth":{"method":"development-credential","credentialVariable":"not a variable name"}}]}`,
		"unreadable endpoint": `{"schemaVersion":1,"defaultContext":"a","contexts":[` +
			`{"name":"a","endpoint":"127.0.0.1:8080"}]}`,
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
			if !asProblem(err, &typed) || typed.Category != problem.CategoryUsage {
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
	const embedded = "http://operator:s3cr3t@127.0.0.1:8080"
	document := document()
	document.Contexts[0].Endpoint = embedded

	_, err := document.Encode()

	var typed problem.Problem
	if err == nil {
		t.Fatal("a context embedding credentials in its endpoint was accepted")
	}
	if !asProblem(err, &typed) || typed.Category != problem.CategoryUsage {
		t.Fatalf("Encode returned %v, want a usage problem", err)
	}
	if strings.Contains(typed.Message+typed.Recovery, "s3cr3t") {
		t.Fatalf("the refusal repeats the endpoint's credentials: %+v", typed)
	}
}

func TestARejectedEndpointIsNeverEchoed(t *testing.T) {
	document := document()
	document.Contexts[0].Endpoint = "not an endpoint with s3cr3t in it"

	_, err := document.Encode()

	if err == nil {
		t.Fatal("an unreadable endpoint was accepted")
	}
	if strings.Contains(err.Error(), "s3cr3t") {
		t.Fatalf("the refusal repeats the rejected endpoint: %v", err)
	}
}

func TestTheFixtureRefusesToWriteIntoRealWSO2State(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("WSO2_HOME", "")

	if err := fixture.Install(filepath.Join(home, ".wso2"), document()); err == nil {
		t.Fatal("the fixture wrote into the developer's real WSO2 state")
	}
}

// install writes the document into an isolated state root and returns the root.
func install(t *testing.T, document contexts.Document) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "state")
	if err := fixture.Install(root, document); err != nil {
		t.Fatalf("fixture.Install returned %v", err)
	}
	return root
}

// jsonMembers reports the JSON member names a value serializes to, in order.
func jsonMembers(t *testing.T, value any) []string {
	t.Helper()
	structure := reflect.TypeOf(value)
	members := make([]string, 0, structure.NumField())
	for index := range structure.NumField() {
		tag, _, _ := strings.Cut(structure.Field(index).Tag.Get("json"), ",")
		if tag == "-" {
			continue
		}
		members = append(members, tag)
	}
	return members
}

func asProblem(err error, target *problem.Problem) bool {
	typed, ok := err.(problem.Problem)
	if ok {
		*target = typed
	}
	return ok
}
