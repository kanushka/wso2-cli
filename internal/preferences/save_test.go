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
	"runtime"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/preferences"
	"github.com/wso2/wso2-cli/sdk/problem"
)

func TestSaveWritesADocumentThisPackageReadsBack(t *testing.T) {
	root := t.TempDir()
	document := preferences.Document{SchemaVersion: preferences.SchemaVersion, OutputMode: "json"}
	if err := preferences.Save(root, document); err != nil {
		t.Fatalf("Save returned %v", err)
	}

	loaded, diagnostic := preferences.Load(root)
	if diagnostic != nil {
		t.Fatalf("Load after Save returned a diagnostic: %v", diagnostic)
	}
	if loaded != document {
		t.Errorf("Load after Save = %+v, want %+v", loaded, document)
	}
}

// TestSaveWritesTheDocumentAt0600InA0700Directory pins the state-root
// convention: the document is private, and so is the directory that names
// its existence.
func TestSaveWritesTheDocumentAt0600InA0700Directory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes are not enforced on Windows")
	}
	root := t.TempDir()
	if err := preferences.Save(root, preferences.Document{SchemaVersion: preferences.SchemaVersion}); err != nil {
		t.Fatalf("Save returned %v", err)
	}

	path := preferences.Path(root)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%q): %v", path, err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("document mode = %o, want 0600", perm)
	}

	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Stat(dir): %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("directory mode = %o, want 0700", perm)
	}
}

// TestUpdatePreservesTheOtherKey proves the read-modify-write actually reads:
// setting one key through Update must not erase what an earlier Update wrote
// for the other.
func TestUpdatePreservesTheOtherKey(t *testing.T) {
	root := t.TempDir()
	set := func(key preferences.Key, value string) {
		t.Helper()
		err := preferences.Update(root, func(document preferences.Document) (preferences.Document, error) {
			document.SchemaVersion = preferences.SchemaVersion
			return document.Set(key, value)
		})
		if err != nil {
			t.Fatalf("Update(%q, %q) returned %v", key, value, err)
		}
	}
	set(preferences.KeyOutputMode, "json")
	set(preferences.KeyCatalogOrigin, "https://example.com")

	loaded, diagnostic := preferences.Load(root)
	if diagnostic != nil {
		t.Fatalf("Load returned a diagnostic: %v", diagnostic)
	}
	if loaded.OutputMode != "json" {
		t.Errorf("outputMode = %q, want json", loaded.OutputMode)
	}
	if loaded.CatalogOrigin != "https://example.com" {
		t.Errorf("catalogOrigin = %q, want https://example.com", loaded.CatalogOrigin)
	}
}

// TestUpdateWritesNothingWhenChangeFails pins that a refused Set (an unknown
// key or an invalid value) leaves the document on disk untouched, the same
// guarantee internal/contexts.Update makes.
func TestUpdateWritesNothingWhenChangeFails(t *testing.T) {
	root := t.TempDir()
	if err := preferences.Save(root, preferences.Document{
		SchemaVersion: preferences.SchemaVersion, OutputMode: "table",
	}); err != nil {
		t.Fatalf("Save returned %v", err)
	}

	err := preferences.Update(root, func(document preferences.Document) (preferences.Document, error) {
		document.SchemaVersion = preferences.SchemaVersion
		return document.Set(preferences.KeyOutputMode, "yaml")
	})
	if err == nil {
		t.Fatal("Update with an invalid value returned no error")
	}

	loaded, _ := preferences.Load(root)
	if loaded.OutputMode != "table" {
		t.Errorf("outputMode = %q after a failed Update, want the original table", loaded.OutputMode)
	}
}

// TestUpdateRefusesToOverwriteANewerSchemaVersion is the write half of the
// version-freeze allowlist (R9): kept unchanged from internal/contexts, an
// unrecognised version this shell did not write is never replaced.
func TestUpdateRefusesToOverwriteANewerSchemaVersion(t *testing.T) {
	root := t.TempDir()
	path := preferences.Path(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"schemaVersion":999,"outputMode":"json"}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := preferences.Update(root, func(document preferences.Document) (preferences.Document, error) {
		document.SchemaVersion = preferences.SchemaVersion
		return document.Set(preferences.KeyOutputMode, "table")
	})
	if err == nil {
		t.Fatal("Update over a newer schema version returned no error")
	}
	var typed problem.Problem
	if !errors.As(err, &typed) || typed.Code != "preferences.document_frozen" {
		t.Fatalf("Update over a newer schema version returned %v, want preferences.document_frozen", err)
	}

	on, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got := string(on); got != `{"schemaVersion":999,"outputMode":"json"}` {
		t.Errorf("the newer-version document was modified: %s", got)
	}
}

// TestUpdateRefusesRatherThanResetsACorruptDocument is the write-side half of
// F2 (fix round 1): Update must not silently treat a document Load could not
// parse as the zero Document, because that would overwrite the document with
// one holding only the key the caller is setting, discarding everything else
// it held. refuseFrozenDocument still agrees this file may be written (it is
// not a newer CLI's document — see TestUpdateRefusesToOverwriteANewerSchema
// Version for that case), but Update itself now refuses rather than guessing
// "nothing was there".
func TestUpdateRefusesRatherThanResetsACorruptDocument(t *testing.T) {
	root := t.TempDir()
	path := preferences.Path(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := preferences.Update(root, func(document preferences.Document) (preferences.Document, error) {
		document.SchemaVersion = preferences.SchemaVersion
		return document.Set(preferences.KeyOutputMode, "json")
	})
	if err == nil {
		t.Fatal("Update over a corrupt document returned no error")
	}
	var typed problem.Problem
	if !errors.As(err, &typed) || typed.Code != "preferences.document_unreadable_for_update" {
		t.Fatalf("Update over a corrupt document returned %v, want preferences.document_unreadable_for_update", err)
	}
	// The refusal must not repeat Load's diagnostic wholesale: that sentence
	// ends in "falls back to default preferences", which is true of the read
	// but false of this refusal, which falls back to nothing and writes
	// nothing. The user would read one sentence twice and be told the second
	// time that a fallback they had just been refused had happened.
	if strings.Contains(typed.Message, "falls back") {
		t.Errorf("the refusal claims a fallback it does not perform: %s", typed.Message)
	}
	if !strings.Contains(typed.Message, "preferences.document_malformed") {
		t.Errorf("the refusal does not name what is wrong with the document: %s", typed.Message)
	}

	on, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	if got := string(on); got != "{not valid json" {
		t.Errorf("the corrupt document was modified: %s", got)
	}
}

// TestUpdateRefusesRatherThanResetsADocumentWithOneInvalidField is F2's
// exact reported shape: a document that parses as JSON and carries this
// shell's own current schema version, but fails validation on one field,
// passes refuseFrozenDocument (which only probes schemaVersion) and used to
// have Update silently treat it as the zero Document — destroying a
// perfectly valid catalogOrigin nobody asked to touch, just because
// outputMode had been hand-corrupted. Update must refuse instead, leaving
// the file exactly as it was found.
func TestUpdateRefusesRatherThanResetsADocumentWithOneInvalidField(t *testing.T) {
	root := t.TempDir()
	path := preferences.Path(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	const onDisk = `{"schemaVersion":1,"outputMode":"yaml","catalogOrigin":"https://keepme.example"}`
	if err := os.WriteFile(path, []byte(onDisk), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := preferences.Update(root, func(document preferences.Document) (preferences.Document, error) {
		document.SchemaVersion = preferences.SchemaVersion
		return document.Set(preferences.KeyCatalogOrigin, "https://different.example")
	})
	if err == nil {
		t.Fatal("Update over a document with one invalid field returned no error")
	}
	var typed problem.Problem
	if !errors.As(err, &typed) || typed.Code != "preferences.document_unreadable_for_update" {
		t.Fatalf("Update returned %v, want preferences.document_unreadable_for_update", err)
	}

	on, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	if got := string(on); got != onDisk {
		t.Errorf("the document was modified; the valid catalogOrigin was at risk: %s", got)
	}
}
