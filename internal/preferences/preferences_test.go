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

package preferences_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/preferences"
	"github.com/wso2/wso2-cli/sdk/problem"
)

func TestPathIsCliPreferencesJSONInsideTheStateRoot(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, "cli", "preferences.json")
	if got := preferences.Path(root); got != want {
		t.Errorf("Path(%q) = %q, want %q", root, got, want)
	}
}

func TestLockPathSitsBesideTheDocumentNotUnderCliLocks(t *testing.T) {
	root := t.TempDir()
	want := preferences.Path(root) + ".lock"
	if got := preferences.LockPath(root); got != want {
		t.Errorf("LockPath(%q) = %q, want %q", root, got, want)
	}
	if strings.Contains(preferences.LockPath(root), filepath.Join("cli", "locks")) {
		t.Error("the lock lives under cli/locks, which is the session store's namespace")
	}
}

// TestGetOnAnUnsetKeyReportsNotSet pins Document's zero value: nothing
// configured is not itself a refusal, and every key reports it the same way.
func TestGetOnAnUnsetKeyReportsNotSet(t *testing.T) {
	var document preferences.Document
	for _, key := range preferences.Keys() {
		if value, set := document.Get(key); set || value != "" {
			t.Errorf("Get(%q) on a zero Document = (%q, %v), want (\"\", false)", key, value, set)
		}
	}
}

// TestSetRefusesAnUnknownKey proves the closed key set (R8) is enforced, not
// merely documented: a key outside {output, catalog-origin} is refused
// rather than silently accepted.
func TestSetRefusesAnUnknownKey(t *testing.T) {
	var document preferences.Document
	_, err := document.Set("credential", "sh")
	if err == nil {
		t.Fatal("Set with an unknown key returned no error")
	}
	var typed problem.Problem
	if !errors.As(err, &typed) || typed.Code != "config.unknown_key" {
		t.Fatalf("Set with an unknown key returned %v, want config.unknown_key", err)
	}
	for _, key := range preferences.Keys() {
		if !strings.Contains(typed.Recovery, string(key)) {
			t.Errorf("the refusal does not name the valid key %q:\n%s", key, typed.Recovery)
		}
	}
}

// TestSetRefusesAnInvalidOutputMode pins that a value a key does not accept is
// refused, and the refusal says what is acceptable — table or json, never a
// bare "invalid value".
func TestSetRefusesAnInvalidOutputMode(t *testing.T) {
	var document preferences.Document
	_, err := document.Set(preferences.KeyOutputMode, "yaml")
	if err == nil {
		t.Fatal("Set(output, \"yaml\") returned no error")
	}
	var typed problem.Problem
	if !errors.As(err, &typed) || typed.Code != "config.invalid_value" {
		t.Fatalf("Set(output, \"yaml\") returned %v, want config.invalid_value", err)
	}
	if !strings.Contains(typed.Recovery, "table") || !strings.Contains(typed.Recovery, "json") {
		t.Errorf("the refusal does not name the acceptable values:\n%s", typed.Recovery)
	}
}

// TestSetRefusesACatalogOriginWithNoScheme pins that this key is validated as
// a URL and not merely as "non-empty": a typo here would silently point every
// module install at the wrong place.
func TestSetRefusesACatalogOriginWithNoScheme(t *testing.T) {
	var document preferences.Document
	if _, err := document.Set(preferences.KeyCatalogOrigin, "example.com/catalog"); err == nil {
		t.Fatal("Set(catalog-origin, \"example.com/catalog\") returned no error")
	}
}

// TestSetRefusesACatalogOriginWithANonHTTPScheme pins F7: a scheme other
// than http/https is refused even though it has both a scheme and a host, the
// same restriction internal/catalog's own validArtifactURL applies to an
// artifact URL. javascript: is used here because it is unambiguously never a
// catalog origin, not because this shell executes anything — the origin is
// only ever read as an HTTP(S) base URL.
func TestSetRefusesACatalogOriginWithANonHTTPScheme(t *testing.T) {
	var document preferences.Document
	if _, err := document.Set(preferences.KeyCatalogOrigin, "javascript://x"); err == nil {
		t.Fatal("Set(catalog-origin, \"javascript://x\") returned no error")
	}
	if _, err := document.Set(preferences.KeyCatalogOrigin, "ftp://example.com"); err == nil {
		t.Fatal("Set(catalog-origin, \"ftp://example.com\") returned no error")
	}
}

func TestSetAcceptsAWellFormedCatalogOrigin(t *testing.T) {
	document, err := preferences.Document{}.Set(preferences.KeyCatalogOrigin, "https://example.com/catalog")
	if err != nil {
		t.Fatalf("Set returned %v", err)
	}
	if value, set := document.Get(preferences.KeyCatalogOrigin); !set || value != "https://example.com/catalog" {
		t.Errorf("Get(catalog-origin) = (%q, %v), want (\"https://example.com/catalog\", true)", value, set)
	}
}

// TestSetDoesNotDisturbTheOtherKey proves the closed set's two keys are
// independent: setting one leaves the other exactly as it was.
func TestSetDoesNotDisturbTheOtherKey(t *testing.T) {
	document := preferences.Document{OutputMode: "json", CatalogOrigin: "https://example.com"}
	next, err := document.Set(preferences.KeyCatalogOrigin, "https://different.example")
	if err != nil {
		t.Fatalf("Set returned %v", err)
	}
	if value, _ := next.Get(preferences.KeyOutputMode); value != "json" {
		t.Errorf("output mode changed to %q, want it undisturbed", value)
	}
}

// TestLoadOnAnAbsentDocumentReturnsDefaultsWithNoDiagnostic pins the half of
// R9 that is not a fallback at all: a machine nobody has configured has done
// nothing wrong.
func TestLoadOnAnAbsentDocumentReturnsDefaultsWithNoDiagnostic(t *testing.T) {
	document, diagnostic := preferences.Load(t.TempDir())
	if diagnostic != nil {
		t.Errorf("Load on an absent document returned a diagnostic: %v", diagnostic)
	}
	if document != (preferences.Document{}) {
		t.Errorf("Load on an absent document = %+v, want the zero value", document)
	}
}

// TestLoadOnACorruptDocumentFallsBackWithADiagnostic pins R9's lenient
// read: a document that is not even valid JSON must not fail every command
// that reads it, but the fallback must be diagnosed, not silent.
func TestLoadOnACorruptDocumentFallsBackWithADiagnostic(t *testing.T) {
	root := t.TempDir()
	writeRawDocument(t, root, "{not valid json")

	document, diagnostic := preferences.Load(root)
	if document != (preferences.Document{}) {
		t.Errorf("Load on a corrupt document = %+v, want defaults", document)
	}
	if diagnostic == nil {
		t.Fatal("Load on a corrupt document returned no diagnostic")
	}
	if diagnostic.Code != "preferences.document_malformed" {
		t.Errorf("diagnostic code = %q, want preferences.document_malformed", diagnostic.Code)
	}
}

// TestLoadOnANewerSchemaVersionFallsBackWithADiagnostic is the read half of
// the version-freeze story; TestUpdateRefusesToOverwriteANewerSchemaVersion
// below is the write half.
//
// F4 (fix round 1): the diagnostic must correctly report which of the two
// distinct problems this is. Before the fix, Load wrapped every Decode error
// — including the typed preferences.schema_unsupported problem
// schemaUnsupported constructs — inside a generic
// preferences.document_malformed with a doubly-prefixed message ("...could
// not be used: usage: preferences.schema_unsupported: ...") and the corrupt-
// document recovery ("Correct the document..."). A user whose file was
// written by a newer CLI needs to be told to update the shell, not that
// their perfectly valid, too-new file is broken.
func TestLoadOnANewerSchemaVersionFallsBackWithADiagnostic(t *testing.T) {
	root := t.TempDir()
	writeRawDocument(t, root, `{"schemaVersion":999,"outputMode":"json"}`)

	document, diagnostic := preferences.Load(root)
	if document != (preferences.Document{}) {
		t.Errorf("Load on a newer schema version = %+v, want defaults", document)
	}
	if diagnostic == nil {
		t.Fatal("Load on a newer schema version returned no diagnostic")
	}
	if diagnostic.Code != "preferences.schema_unsupported" {
		t.Errorf("diagnostic code = %q, want preferences.schema_unsupported", diagnostic.Code)
	}
	if strings.Contains(diagnostic.Message, "usage:") {
		t.Errorf("the diagnostic message is doubly-prefixed with the underlying problem's own rendering:\n%s",
			diagnostic.Message)
	}
	if !strings.Contains(diagnostic.Recovery, "Update the WSO2 CLI") {
		t.Errorf("the recovery does not point at updating the CLI, the actual fix for a too-new file:\n%s",
			diagnostic.Recovery)
	}
}

// TestDecodeRefusesAnUnknownField pins F6: a hand-added member this store
// does not define is refused rather than silently dropped on the next
// write. This is a deliberate contrast with internal/contexts, which
// tolerates unknown members on purpose so a newer shell can add non-secret
// context facts within one schema version — that forward-compatibility
// story does not apply here, because this store's whole point is a CLOSED
// key set (R8): a member outside it is drift the set exists to catch, not a
// future key to preserve blindly.
func TestDecodeRefusesAnUnknownField(t *testing.T) {
	_, err := preferences.Decode([]byte(`{"schemaVersion":1,"outputMode":"json","accessToken":"sh"}`))
	if err == nil {
		t.Fatal("Decode with an unknown field returned no error")
	}
	var typed problem.Problem
	if !errors.As(err, &typed) || typed.Code != "preferences.document_malformed" {
		t.Fatalf("Decode with an unknown field returned %v, want preferences.document_malformed", err)
	}
}

// writeRawDocument plants a file at the preferences path without going
// through this package's own writer, so a test can construct a document this
// shell would refuse to write itself.
func writeRawDocument(t *testing.T, stateRoot, contents string) {
	t.Helper()
	path := preferences.Path(stateRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
