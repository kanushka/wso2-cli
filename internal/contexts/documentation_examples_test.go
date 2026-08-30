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
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/contexts"
)

// docCredentialRefFiles are the published examples that show credentialRef
// values to a reader who is expected to copy them.
var docCredentialRefFiles = []string{
	"../../docs/examples/authentication-contexts.md",
	"../../docs/examples/login-walkthroughs.md",
}

// docCredentialRefAssignment matches a credentialRef field's value as either
// file shows it: `credentialRef: acme-cloud` in the illustrative YAML, or
// `"credentialRef": "acme-cloud"` in the one fenced block
// (authentication-contexts.md §11 is JSON; the identity examples are YAML).
//
// This extracts only the one field this task is about, rather than parsing
// the surrounding YAML structurally: the shell reads JSON, has no YAML
// reader, and must not gain one just to test its own documentation. A plain
// line match ties the test to the files' literal, current content without
// that dependency.
var docCredentialRefAssignment = regexp.MustCompile(`credentialRef['"]?\s*:\s*['"]?([A-Za-z0-9_.:/<>-]+)`)

// TestDocumentedCredentialRefsDecode reads every credentialRef value shown in
// the two example files that document the identity schema and confirms each
// one is accepted by the shell's own reader, contexts.Decode.
//
// This is the regression test for #115: both files used to show
// `keychain://wso2/<name>`, a shape internal/contexts/identity.go's
// refPattern refuses, so a reader who copied the documented example got
// "does not name a secure-store reference as its credential source" instead
// of a working configuration. See TestKeychainURICredentialRefIsRefused for
// proof this test catches that regression.
func TestDocumentedCredentialRefsDecode(t *testing.T) {
	for _, path := range docCredentialRefFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		refs := documentedCredentialRefs(string(data))
		if len(refs) == 0 {
			t.Fatalf("%s: found no credentialRef examples; has the field been renamed or removed?", path)
		}
		for _, ref := range refs {
			t.Run(filepath.Base(path)+"/"+ref, func(t *testing.T) {
				if _, err := contexts.Decode([]byte(refOnlyDocument(ref))); err != nil {
					t.Fatalf("documented credentialRef %q no longer decodes: %v", ref, err)
				}
			})
		}
	}
}

// TestKeychainURICredentialRefIsRefused proves TestDocumentedCredentialRefsDecode
// would catch the defect this task fixed, by reintroducing the shape the docs
// used to show and confirming the shell still refuses it.
func TestKeychainURICredentialRefIsRefused(t *testing.T) {
	_, err := contexts.Decode([]byte(refOnlyDocument("keychain://wso2/acme-cloud")))
	if err == nil {
		t.Fatal("a keychain:// credentialRef decoded; refPattern should have refused it")
	}
	if !strings.Contains(err.Error(), "does not name a secure-store reference") {
		t.Fatalf("refused for the wrong reason: %v", err)
	}
}

// documentedCredentialRefs extracts every concrete credentialRef value from
// doc, skipping the schema-shape placeholders (`<ref>`, `<name>`) that
// describe the field rather than giving an example of it.
func documentedCredentialRefs(doc string) []string {
	seen := make(map[string]bool)
	var refs []string
	for _, match := range docCredentialRefAssignment.FindAllStringSubmatch(doc, -1) {
		ref := match[1]
		if strings.ContainsAny(ref, "<>") || seen[ref] {
			continue
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	return refs
}

// refOnlyDocument is a minimal, otherwise-valid schema version 2 document
// declaring one pat identity. The pat kind needs nothing but a credentialRef,
// which isolates the one property under test — whether the value matches
// refPattern — from fields (issuer, clientId) an oauth-browser identity would
// also require and that most of the documented examples leave for a
// separate, not-yet-implemented default (docs/examples/login-walkthroughs.md
// gap 2).
func refOnlyDocument(credentialRef string) string {
	return fmt.Sprintf(`{
  "schemaVersion": 2,
  "defaultContext": "example",
  "identities": [
    {
      "name": "example",
      "type": "onprem",
      "auth": {
        "kind": "pat",
        "credentialRef": %q
      }
    }
  ],
  "contexts": [
    {"name": "example", "identity": "example"}
  ]
}`, credentialRef)
}
