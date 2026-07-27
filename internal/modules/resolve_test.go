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

package modules_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/modules/fixture"
	"github.com/wso2/wso2-cli/internal/semver"
	"github.com/wso2/wso2-cli/sdk/problem"
)

const referenceExecutable = "wso2-module-reference"

func TestResolveReturnsTheActiveIntegrityCheckedModule(t *testing.T) {
	store, receipt := installReference(t, fixture.Module{
		Namespace:     "reference",
		Version:       "0.1.0",
		ShellRange:    ">=0.1.0 <1.0.0",
		AuthAudiences: []string{"reference-status"},
		AuthScopes:    []string{"reference:status:read"},
	})

	resolved, err := store.Resolve("reference", shellIdentity())
	if err != nil {
		t.Fatalf("Resolve returned %v", err)
	}
	if resolved.Receipt.ModuleVersion != "0.1.0" {
		t.Fatalf("resolved module version = %q, want 0.1.0", resolved.Receipt.ModuleVersion)
	}
	if resolved.ProtocolVersion != 1 {
		t.Fatalf("negotiated protocol = %d, want 1", resolved.ProtocolVersion)
	}
	if want := store.VersionDir("reference", "0.1.0"); filepath.Dir(resolved.ExecutablePath) != evalSymlinks(t, want) {
		t.Fatalf("executable path %q is not inside the version directory %q", resolved.ExecutablePath, want)
	}
	if resolved.Receipt.ExecutableSHA256 != receipt.ExecutableSHA256 {
		t.Fatal("resolved receipt does not match the installed receipt")
	}
	if got := resolved.Receipt.Capabilities.AuthAudiences; len(got) != 1 || got[0] != "reference-status" {
		t.Fatalf("declared audiences = %v, want [reference-status]", got)
	}
}

func TestResolveSelectsTheActiveVersionAmongSeveralInstalled(t *testing.T) {
	store, _ := installReference(t, fixture.Module{Namespace: "reference", Version: "0.1.0"})
	if _, err := fixture.Install(store.Root(), fixture.Module{
		Namespace: "reference",
		Version:   "0.2.0",
		Contents:  []byte("second version"),
		Inactive:  true,
	}); err != nil {
		t.Fatalf("installing the second version returned %v", err)
	}

	resolved, err := store.Resolve("reference", shellIdentity())
	if err != nil {
		t.Fatalf("Resolve returned %v", err)
	}
	if resolved.Receipt.ModuleVersion != "0.1.0" {
		t.Fatalf("resolved version = %q, want the active 0.1.0", resolved.Receipt.ModuleVersion)
	}

	if err := fixture.Activate(store.Root(), "reference", "0.2.0"); err != nil {
		t.Fatalf("Activate returned %v", err)
	}
	resolved, err = store.Resolve("reference", shellIdentity())
	if err != nil {
		t.Fatalf("Resolve after activation returned %v", err)
	}
	if resolved.Receipt.ModuleVersion != "0.2.0" {
		t.Fatalf("resolved version = %q, want the newly active 0.2.0", resolved.Receipt.ModuleVersion)
	}
}

func TestResolveRejectsBrokenInstallations(t *testing.T) {
	tests := []struct {
		name     string
		module   fixture.Module
		breakage func(t *testing.T, store modules.Store)
		wantCode string
	}{
		{
			name:     "no active version",
			module:   fixture.Module{Namespace: "reference", Version: "0.1.0", Inactive: true},
			wantCode: "modules.no_active_version",
		},
		{
			name:   "missing receipt",
			module: fixture.Module{Namespace: "reference", Version: "0.1.0"},
			breakage: func(t *testing.T, store modules.Store) {
				remove(t, store.ReceiptPath("reference", "0.1.0"))
			},
			wantCode: "modules.receipt_missing",
		},
		{
			// Rewriting a receipt after activation changes its digest, so
			// resolution stops at the pointer before reading the document.
			name:   "receipt rewritten after activation",
			module: fixture.Module{Namespace: "reference", Version: "0.1.0"},
			breakage: func(t *testing.T, store modules.Store) {
				writeFile(t, store.ReceiptPath("reference", "0.1.0"), []byte("{ not json"))
			},
			wantCode: "modules.receipt_digest_mismatch",
		},
		{
			name:   "malformed receipt",
			module: fixture.Module{Namespace: "reference", Version: "0.1.0"},
			breakage: func(t *testing.T, store modules.Store) {
				if err := fixture.WriteRawReceipt(store.Root(), "reference", "0.1.0", []byte("{ not json")); err != nil {
					t.Fatalf("WriteRawReceipt returned %v", err)
				}
				if err := fixture.Activate(store.Root(), "reference", "0.1.0"); err != nil {
					t.Fatalf("Activate returned %v", err)
				}
			},
			wantCode: "modules.receipt_malformed",
		},
		{
			name:   "malformed active-version pointer",
			module: fixture.Module{Namespace: "reference", Version: "0.1.0"},
			breakage: func(t *testing.T, store modules.Store) {
				if err := fixture.WriteRawActive(store.Root(), "reference", []byte(`{"schemaVersion":1`)); err != nil {
					t.Fatalf("WriteRawActive returned %v", err)
				}
			},
			wantCode: "modules.active_malformed",
		},
		{
			name:   "active-version pointer naming another namespace",
			module: fixture.Module{Namespace: "reference", Version: "0.1.0"},
			breakage: func(t *testing.T, store modules.Store) {
				if err := fixture.WriteRawActive(store.Root(), "reference",
					[]byte(`{"schemaVersion":1,"namespace":"api","version":"0.1.0","receiptSha256":"`+zeroDigest+`"}`)); err != nil {
					t.Fatalf("WriteRawActive returned %v", err)
				}
			},
			wantCode: "modules.active_malformed",
		},
		{
			name:   "active-version pointer naming an escaping version",
			module: fixture.Module{Namespace: "reference", Version: "0.1.0"},
			breakage: func(t *testing.T, store modules.Store) {
				if err := fixture.WriteRawActive(store.Root(), "reference",
					[]byte(`{"schemaVersion":1,"namespace":"reference","version":"../../../etc","receiptSha256":"`+zeroDigest+`"}`)); err != nil {
					t.Fatalf("WriteRawActive returned %v", err)
				}
			},
			wantCode: "modules.active_malformed",
		},
		{
			name:   "unsupported active-version pointer schema",
			module: fixture.Module{Namespace: "reference", Version: "0.1.0"},
			breakage: func(t *testing.T, store modules.Store) {
				if err := fixture.WriteRawActive(store.Root(), "reference",
					[]byte(`{"schemaVersion":99,"namespace":"reference","version":"0.1.0","receiptSha256":"`+zeroDigest+`"}`)); err != nil {
					t.Fatalf("WriteRawActive returned %v", err)
				}
			},
			wantCode: "modules.active_schema_unsupported",
		},
		{
			name:   "unsupported receipt schema",
			module: fixture.Module{Namespace: "reference", Version: "0.1.0"},
			breakage: func(t *testing.T, store modules.Store) {
				rewriteReceipt(t, store, func(receipt *modules.Receipt) { receipt.SchemaVersion = 99 })
			},
			wantCode: "modules.receipt_schema_unsupported",
		},
		{
			name:   "receipt declaring another namespace",
			module: fixture.Module{Namespace: "reference", Version: "0.1.0"},
			breakage: func(t *testing.T, store modules.Store) {
				rewriteReceipt(t, store, func(receipt *modules.Receipt) { receipt.Namespace = "api" })
			},
			wantCode: "modules.receipt_namespace_mismatch",
		},
		{
			name:   "receipt declaring another version",
			module: fixture.Module{Namespace: "reference", Version: "0.1.0"},
			breakage: func(t *testing.T, store modules.Store) {
				rewriteReceipt(t, store, func(receipt *modules.Receipt) { receipt.ModuleVersion = "0.9.0" })
			},
			wantCode: "modules.receipt_version_mismatch",
		},
		{
			name:     "receipt escaping its version directory",
			module:   fixture.Module{Namespace: "reference", Version: "0.1.0", ExecutablePathOverride: "../../../../bin/sh"},
			wantCode: "modules.receipt_path_escape",
		},
		{
			name:     "receipt naming an absolute executable",
			module:   fixture.Module{Namespace: "reference", Version: "0.1.0", ExecutablePathOverride: absoluteProbePath()},
			wantCode: "modules.receipt_path_escape",
		},
		{
			name:   "tampered executable",
			module: fixture.Module{Namespace: "reference", Version: "0.1.0"},
			breakage: func(t *testing.T, store modules.Store) {
				if err := fixture.TamperExecutable(store.Root(), "reference", "0.1.0", referenceExecutable,
					[]byte("#!/bin/sh\necho tampered\n")); err != nil {
					t.Fatalf("TamperExecutable returned %v", err)
				}
			},
			wantCode: "modules.executable_digest_mismatch",
		},
		{
			name:   "missing executable",
			module: fixture.Module{Namespace: "reference", Version: "0.1.0"},
			breakage: func(t *testing.T, store modules.Store) {
				remove(t, filepath.Join(store.VersionDir("reference", "0.1.0"), referenceExecutable))
			},
			wantCode: "modules.executable_missing",
		},
		{
			name:     "incompatible shell version",
			module:   fixture.Module{Namespace: "reference", Version: "0.1.0", ShellRange: ">=2.0.0 <3.0.0"},
			wantCode: "modules.incompatible_shell",
		},
		{
			name:     "incompatible protocol version",
			module:   fixture.Module{Namespace: "reference", Version: "0.1.0", ProtocolVersions: []int{7}},
			wantCode: "modules.incompatible_protocol",
		},
		{
			name:     "incompatible operating system",
			module:   fixture.Module{Namespace: "reference", Version: "0.1.0", OS: "plan9"},
			wantCode: "modules.incompatible_platform",
		},
		{
			name:     "incompatible architecture",
			module:   fixture.Module{Namespace: "reference", Version: "0.1.0", Arch: "mips"},
			wantCode: "modules.incompatible_platform",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, _ := installReference(t, test.module)
			if test.breakage != nil {
				test.breakage(t, store)
			}

			_, err := store.Resolve("reference", shellIdentity())
			assertProblemCode(t, err, test.wantCode)
		})
	}
}

func TestResolveRejectsAReceiptWhoseSymbolicLinkLeavesTheVersionDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic link creation needs elevation on Windows")
	}

	store, _ := installReference(t, fixture.Module{Namespace: "reference", Version: "0.1.0"})
	outside := filepath.Join(t.TempDir(), "outside")
	writeFile(t, outside, []byte("#!/bin/sh\nexit 0\n"))

	link := filepath.Join(store.VersionDir("reference", "0.1.0"), "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("Symlink returned %v", err)
	}
	rewriteReceiptAndActivate(t, store, func(receipt *modules.Receipt) {
		receipt.Executable = "linked"
		receipt.ExecutableSHA256 = modules.BytesDigest([]byte("#!/bin/sh\nexit 0\n"))
	})

	_, err := store.Resolve("reference", shellIdentity())
	assertProblemCode(t, err, "modules.receipt_path_escape")
}

func TestResolveIgnoresAnExecutableOnPathOrInTheWorkingDirectory(t *testing.T) {
	store, _ := installReference(t, fixture.Module{Namespace: "reference", Version: "0.1.0"})
	remove(t, store.ActivePath("reference"))

	// A same-named executable on PATH and in the working directory must not
	// make an unmanaged module resolvable.
	shadowDir := t.TempDir()
	writeFile(t, filepath.Join(shadowDir, referenceExecutable), []byte("#!/bin/sh\necho shadow\n"))
	t.Setenv("PATH", shadowDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Chdir(shadowDir)

	_, err := store.Resolve("reference", shellIdentity())
	assertProblemCode(t, err, "modules.no_active_version")
}

func TestResolveRejectsAnInvalidNamespaceAsUsage(t *testing.T) {
	store := modules.NewStore(filepath.Join(t.TempDir(), "modules"))

	_, err := store.Resolve("../escape", shellIdentity())

	var typed problem.Problem
	if !errors.As(err, &typed) {
		t.Fatalf("Resolve returned %v, want a typed problem", err)
	}
	if typed.Category != problem.CategoryUsage || typed.Code != "modules.invalid_namespace" {
		t.Fatalf("problem = %+v, want a usage problem with code modules.invalid_namespace", typed)
	}
}

func TestResolveRecomputesTheDigestOnEveryCall(t *testing.T) {
	store, _ := installReference(t, fixture.Module{Namespace: "reference", Version: "0.1.0"})
	if _, err := store.Resolve("reference", shellIdentity()); err != nil {
		t.Fatalf("first Resolve returned %v", err)
	}

	if err := fixture.TamperExecutable(store.Root(), "reference", "0.1.0", referenceExecutable, []byte("tampered")); err != nil {
		t.Fatalf("TamperExecutable returned %v", err)
	}

	_, err := store.Resolve("reference", shellIdentity())
	assertProblemCode(t, err, "modules.executable_digest_mismatch")
}

func shellIdentity() modules.ShellIdentity {
	return modules.ShellIdentity{
		Version:          semver.Version{Minor: 1},
		ProtocolVersions: []int{1},
		Platform:         modules.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH},
	}
}

const zeroDigest = "0000000000000000000000000000000000000000000000000000000000000000"

func installReference(t *testing.T, module fixture.Module) (modules.Store, modules.Receipt) {
	t.Helper()
	storeRoot := filepath.Join(t.TempDir(), "cli", "modules")
	receipt, err := fixture.Install(storeRoot, module)
	if err != nil {
		t.Fatalf("fixture.Install returned %v", err)
	}
	return modules.NewStore(storeRoot), receipt
}

func rewriteReceipt(t *testing.T, store modules.Store, change func(*modules.Receipt)) {
	t.Helper()
	receipt, err := modules.ReadReceipt(store.ReceiptPath("reference", "0.1.0"))
	if err != nil {
		t.Fatalf("ReadReceipt returned %v", err)
	}
	change(&receipt)
	data, err := receipt.Encode()
	if err != nil {
		t.Fatalf("Encode returned %v", err)
	}
	if err := fixture.WriteRawReceipt(store.Root(), "reference", "0.1.0", data); err != nil {
		t.Fatalf("WriteRawReceipt returned %v", err)
	}
	if err := fixture.Activate(store.Root(), "reference", "0.1.0"); err != nil {
		t.Fatalf("Activate returned %v", err)
	}
}

func rewriteReceiptAndActivate(t *testing.T, store modules.Store, change func(*modules.Receipt)) {
	t.Helper()
	rewriteReceipt(t, store, change)
}

func assertProblemCode(t *testing.T, err error, wantCode string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected a problem with code %s, got success", wantCode)
	}
	var typed problem.Problem
	if !errors.As(err, &typed) {
		t.Fatalf("error %v is not a typed problem", err)
	}
	if typed.Code != wantCode {
		t.Fatalf("problem code = %s (%s), want %s", typed.Code, typed.Message, wantCode)
	}
	if typed.Recovery == "" {
		t.Errorf("problem %s has no recovery guidance", typed.Code)
	}
}

func writeFile(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned %v", err)
	}
	if err := os.WriteFile(path, contents, 0o755); err != nil {
		t.Fatalf("WriteFile returned %v", err)
	}
}

func remove(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove returned %v", err)
	}
}

func evalSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q) returned %v", path, err)
	}
	return resolved
}

func absoluteProbePath() string {
	if runtime.GOOS == "windows" {
		return `C:\Windows\System32\cmd.exe`
	}
	return "/bin/sh"
}
