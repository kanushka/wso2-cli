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

package atomicfile_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/wso2/wso2-cli/internal/atomicfile"
)

func TestWriteReplacesTheFileAtTheRequestedMode(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "document.json")
	if err := atomicfile.Write(target, []byte("first\n"), 0o600); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := atomicfile.Write(target, []byte("second\n"), 0o600); err != nil {
		t.Fatalf("Write over an existing file: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "second\n" {
		t.Errorf("content = %q, want %q", data, "second\n")
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	// Chmod is a no-op on Windows, so the mode is only a claim on unix.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestWriteAtAWiderModeDoesNotInheritTheTemporaryFilesMode(t *testing.T) {
	// os.CreateTemp opens at 0600. The store's documents are 0644, so a mode
	// that is not carried through would leave every module receipt private to
	// the installing user.
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not enforced here")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "receipt.json")
	if err := atomicfile.Write(target, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("Write: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644", info.Mode().Perm())
	}
}

func TestWriteLeavesNoTemporaryFileBehindOnSuccess(t *testing.T) {
	dir := t.TempDir()
	if err := atomicfile.Write(filepath.Join(dir, "document.json"), []byte("x"), 0o600); err != nil {
		t.Fatalf("Write: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		var names []string
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("directory holds %v, want only the target", names)
	}
}

func TestWriteLeavesTheExistingFileIntactWhenTheTargetDirectoryIsUnwritable(t *testing.T) {
	// The property that makes this worth having: a failed write must not
	// destroy the document that was already there.
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions do not refuse a write here")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "document.json")
	if err := os.WriteFile(target, []byte("original\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := atomicfile.Write(target, []byte("replacement\n"), 0o600); err == nil {
		t.Fatal("Write into an unwritable directory succeeded")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "original\n" {
		t.Errorf("a failed write destroyed the original: %q", data)
	}
}
