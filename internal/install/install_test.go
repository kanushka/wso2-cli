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

// This package had no tests of its own until the atomic write moved out to
// internal/atomicfile. Its behaviour was covered end to end by test/acceptance,
// which installs a module and checks that it runs — a run that succeeds whether
// the store's documents land at 0644 or at the 0600 os.CreateTemp opens a
// temporary file at. So the one property the extraction could quietly break was
// the one nothing asserted.
//
// These tests are in-package because writeAtomically is the seam that changed;
// they pin what a caller of it is entitled to assume, and nothing about how the
// store is assembled.
package install

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/sdk/problem"
)

func TestWriteAtomicallyLeavesStoreDocumentsReadable(t *testing.T) {
	// The store's documents are 0644 and the receipts beside them are read by
	// tooling that is not the installing user. os.CreateTemp opens at 0600, so
	// a mode that is not carried through to the target makes every receipt
	// private without failing anything.
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("file modes are not meaningful here")
	}
	target := filepath.Join(t.TempDir(), "receipt.json")
	if err := writeAtomically("recording the receipt", target, []byte("{}\n")); err != nil {
		t.Fatalf("writeAtomically returned %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("Stat returned %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644", info.Mode().Perm())
	}
}

func TestWriteAtomicallyLeavesNoTemporaryFileBehind(t *testing.T) {
	// A temporary file left in the store is a file the store's own readers
	// would have to learn to skip.
	directory := t.TempDir()
	if err := writeAtomically("writing the version policy",
		filepath.Join(directory, "policy.json"), []byte("{}\n")); err != nil {
		t.Fatalf("writeAtomically returned %v", err)
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("ReadDir returned %v", err)
	}
	if len(entries) != 1 {
		var names []string
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("the store holds %v, want only the document", names)
	}
}

func TestAnUnwrappedCauseIsStillReportedAsItself(t *testing.T) {
	// storeFailure formats the cause with %v, so a nil would reach a user as
	// "<nil>". atomicfile wraps both of its return paths today and this cannot
	// fire; it is pinned so that an edit over there degrades the message rather
	// than replacing it with nonsense.
	bare := errors.New("something the write helper did not wrap")
	if got := writeCause(bare); got != bare {
		t.Errorf("writeCause(%v) = %v, want the error itself", bare, got)
	}
}

func TestAFailedWriteNamesWhatFailedAndNotHowItIsImplemented(t *testing.T) {
	// action is in the message so the store's documents are told apart when one
	// cannot be written. The helper package that performs the rename is not:
	// a user cannot act on it, and it repeats the path the store already named.
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions do not refuse a write here")
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o500); err != nil {
		t.Fatalf("Chmod returned %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(directory, 0o700) })

	err := writeAtomically("recording the receipt", filepath.Join(directory, "receipt.json"), []byte("{}\n"))
	if err == nil {
		t.Fatal("writeAtomically into an unwritable directory succeeded")
	}
	typed, ok := err.(problem.Problem)
	if !ok {
		t.Fatalf("expected a typed problem, got %T", err)
	}
	if !strings.Contains(typed.Message, "recording the receipt failed") {
		t.Errorf("the message does not name what failed: %q", typed.Message)
	}
	if !strings.Contains(typed.Message, "permission denied") {
		t.Errorf("the message does not say why: %q", typed.Message)
	}
	if strings.Contains(typed.Message, "atomicfile") {
		t.Errorf("the message leaks an internal package name: %q", typed.Message)
	}
}
