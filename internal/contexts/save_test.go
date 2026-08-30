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
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/contexts/fixture"
)

func TestSaveWritesADocumentTheShellReadsBack(t *testing.T) {
	root := t.TempDir()
	if err := contexts.Save(root, documentV2()); err != nil {
		t.Fatalf("Save returned %v", err)
	}

	loaded, err := contexts.Load(root)
	if err != nil {
		t.Fatalf("Load after Save returned %v", err)
	}
	if loaded.DefaultContext != "acme-dev" || len(loaded.Contexts) != 1 {
		t.Fatalf("the round trip lost content: %+v", loaded)
	}
	if len(loaded.Identities) != 1 || loaded.Identities[0].Name != "acme-cloud" {
		t.Fatalf("the round trip lost the identity: %+v", loaded.Identities)
	}
}

func TestSaveRefusesADocumentTheShellWouldNotRead(t *testing.T) {
	// The property the whole writer exists for: the shell cannot write a
	// document it would then refuse to load.
	root := t.TempDir()
	invalid := contexts.Document{
		SchemaVersion:  contexts.SchemaVersion,
		DefaultContext: "acme-dev",
		Contexts:       []contexts.Context{{Name: "acme-dev", Identity: "missing"}},
	}

	err := contexts.Save(root, invalid)
	if err == nil {
		t.Fatal("Save accepted a document referencing an undeclared identity")
	}
	assertProblemCode(t, err, "contexts.document_malformed")
	if _, err := os.Stat(contexts.Path(root)); !os.IsNotExist(err) {
		t.Errorf("a refused Save left a file behind: %v", err)
	}
}

func TestSaveWritesTheDocumentPrivately(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not enforced here")
	}
	root := t.TempDir()
	if err := contexts.Save(root, documentV2()); err != nil {
		t.Fatalf("Save returned %v", err)
	}

	info, err := os.Stat(contexts.Path(root))
	if err != nil {
		t.Fatalf("Stat returned %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestSaveDoesNotPreserveFieldsTheSchemaDoesNotKnow(t *testing.T) {
	// D13: a strict read is already the design's position on unknown members,
	// and a round trip through the shell's own types cannot carry one.
	root := t.TempDir()
	seeded := addMember(`"unknownMember": "kept?"`)(validV2())
	seed(t, root, seeded)

	if err := contexts.Update(root, func(d contexts.Document) (contexts.Document, error) {
		return d, nil
	}); err != nil {
		t.Fatalf("Update returned %v", err)
	}

	data, err := os.ReadFile(contexts.Path(root))
	if err != nil {
		t.Fatalf("ReadFile returned %v", err)
	}
	if strings.Contains(string(data), "unknownMember") {
		t.Errorf("a round trip preserved an unknown member:\n%s", data)
	}
}

func TestUpdateAppliesTheChange(t *testing.T) {
	root := t.TempDir()
	if err := contexts.Save(root, documentV2()); err != nil {
		t.Fatalf("Save returned %v", err)
	}

	err := contexts.Update(root, func(d contexts.Document) (contexts.Document, error) {
		d.Contexts = append(d.Contexts, contexts.Context{
			Name: "acme-prod", Identity: "acme-cloud", Organization: "acme",
		})
		return d, nil
	})
	if err != nil {
		t.Fatalf("Update returned %v", err)
	}

	loaded, err := contexts.Load(root)
	if err != nil {
		t.Fatalf("Load after Update returned %v", err)
	}
	if len(loaded.Contexts) != 2 {
		t.Fatalf("Update did not write the change: %+v", loaded.Contexts)
	}
}

func TestUpdateDoesNotWriteWhenTheChangeFails(t *testing.T) {
	root := t.TempDir()
	if err := contexts.Save(root, documentV2()); err != nil {
		t.Fatalf("Save returned %v", err)
	}
	before, err := os.ReadFile(contexts.Path(root))
	if err != nil {
		t.Fatalf("ReadFile returned %v", err)
	}

	sentinel := errors.New("sentinel")
	if err := contexts.Update(root, func(contexts.Document) (contexts.Document, error) {
		return contexts.Document{}, sentinel
	}); !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want the change's own error", err)
	}

	after, err := os.ReadFile(contexts.Path(root))
	if err != nil {
		t.Fatalf("ReadFile returned %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("a failed change rewrote the document")
	}
}

func TestUpdateRefusesAChangeTheShellWouldNotRead(t *testing.T) {
	root := t.TempDir()
	if err := contexts.Save(root, documentV2()); err != nil {
		t.Fatalf("Save returned %v", err)
	}
	before, err := os.ReadFile(contexts.Path(root))
	if err != nil {
		t.Fatalf("ReadFile returned %v", err)
	}

	err = contexts.Update(root, func(d contexts.Document) (contexts.Document, error) {
		d.Contexts = append(d.Contexts, contexts.Context{Name: "acme-prod", Identity: "missing"})
		return d, nil
	})
	assertProblemCode(t, err, "contexts.document_malformed")

	after, err := os.ReadFile(contexts.Path(root))
	if err != nil {
		t.Fatalf("ReadFile returned %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("a refused change rewrote the document")
	}
}

func TestUpdateOnAnAbsentDocumentStartsFromAnEmptyOne(t *testing.T) {
	// A fresh machine has no document. The first write must not be a special
	// case in every caller.
	root := t.TempDir()
	err := contexts.Update(root, func(d contexts.Document) (contexts.Document, error) {
		if len(d.Contexts) != 0 || len(d.Identities) != 0 {
			t.Errorf("a fresh root produced %d contexts and %d identities", len(d.Contexts), len(d.Identities))
		}
		return documentV2(), nil
	})
	if err != nil {
		t.Fatalf("Update returned %v", err)
	}
	if _, err := contexts.Load(root); err != nil {
		t.Fatalf("Load after Update returned %v", err)
	}
}

func TestUpdateRefusesACompatibilityReadDocument(t *testing.T) {
	// The shell never rewrites a version 1 document into version 2 behind its
	// author's back. Encode already refuses one; Update has to surface that
	// refusal rather than write something else.
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
	before, err := os.ReadFile(contexts.Path(root))
	if err != nil {
		t.Fatalf("ReadFile returned %v", err)
	}

	err = contexts.Update(root, func(d contexts.Document) (contexts.Document, error) {
		return d, nil
	})
	assertProblemCode(t, err, "contexts.document_malformed")

	after, err := os.ReadFile(contexts.Path(root))
	if err != nil {
		t.Fatalf("ReadFile returned %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("Update rewrote a version 1 document")
	}
}

func TestConcurrentUpdatesDoNotDiscardEachOther(t *testing.T) {
	// The lock spans the read as well as the write. Two invocations that each
	// read, then each write, would have one silently drop the other's context
	// however atomic each individual write was.
	root := t.TempDir()
	if err := contexts.Save(root, documentV2()); err != nil {
		t.Fatalf("Save returned %v", err)
	}

	names := []string{"acme-one", "acme-two", "acme-three", "acme-four"}
	var group sync.WaitGroup
	errs := make(chan error, len(names))
	for _, name := range names {
		group.Add(1)
		go func() {
			defer group.Done()
			errs <- contexts.Update(root, func(d contexts.Document) (contexts.Document, error) {
				d.Contexts = append(d.Contexts, contexts.Context{
					Name: name, Identity: "acme-cloud", Organization: "acme",
				})
				return d, nil
			})
		}()
	}
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Update returned %v", err)
		}
	}

	loaded, err := contexts.Load(root)
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}
	if len(loaded.Contexts) != len(names)+1 {
		t.Errorf("the document holds %d contexts, want %d; an update was discarded",
			len(loaded.Contexts), len(names)+1)
	}
}

func TestTheDocumentLockDoesNotShareANamespaceWithACredentialReference(t *testing.T) {
	// cli/locks is the session store's per-credential-reference namespace, and
	// a reference is a bare word. A document lock placed there under any fixed
	// name could collide with a real identity whose reference happened to be
	// that word.
	root := t.TempDir()
	lock := contexts.LockPath(root)
	if lock != contexts.Path(root)+".lock" {
		t.Errorf("LockPath = %q, want the document's own path plus .lock", lock)
	}
	if strings.Contains(lock, filepath.Join("cli", "locks")) {
		t.Errorf("the document lock sits in the session store's namespace: %q", lock)
	}
}

// seed writes document bytes straight to the context document's path, without
// going through the writer under test.
func seed(t *testing.T, root, document string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(contexts.Path(root)), 0o700); err != nil {
		t.Fatalf("MkdirAll returned %v", err)
	}
	if err := os.WriteFile(contexts.Path(root), []byte(document), 0o600); err != nil {
		t.Fatalf("WriteFile returned %v", err)
	}
}
