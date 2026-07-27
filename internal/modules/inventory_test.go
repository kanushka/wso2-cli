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
	"path/filepath"
	"testing"

	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/modules/fixture"
)

func TestInventoryIsEmptyWhenNoModuleIsInstalled(t *testing.T) {
	store := modules.NewStore(filepath.Join(t.TempDir(), "cli", "modules"))

	installed, problems, err := store.Inventory()
	if err != nil {
		t.Fatalf("Inventory returned %v", err)
	}
	if len(installed) != 0 || len(problems) != 0 {
		t.Fatalf("Inventory of an absent store = %v, %v; want empty", installed, problems)
	}
}

func TestInventoryReportsActiveModulesSortedByNamespace(t *testing.T) {
	store, _ := installReference(t, fixture.Module{Namespace: "reference", Version: "0.1.0"})
	if _, err := fixture.Install(store.Root(), fixture.Module{Namespace: "example", Version: "1.2.3"}); err != nil {
		t.Fatalf("fixture.Install returned %v", err)
	}

	installed, problems, err := store.Inventory()
	if err != nil {
		t.Fatalf("Inventory returned %v", err)
	}
	if len(problems) != 0 {
		t.Fatalf("Inventory reported problems %v, want none", problems)
	}
	if len(installed) != 2 {
		t.Fatalf("Inventory returned %d entries, want 2", len(installed))
	}
	if installed[0].Namespace != "example" || installed[1].Namespace != "reference" {
		t.Fatalf("inventory order = %q, %q; want example then reference", installed[0].Namespace, installed[1].Namespace)
	}
	if installed[1].Version != "0.1.0" {
		t.Fatalf("reference version = %q, want 0.1.0", installed[1].Version)
	}
}

func TestInventoryReportsBrokenInstallationsWithoutHidingTheRest(t *testing.T) {
	tests := []struct {
		name     string
		breakage func(t *testing.T, store modules.Store)
		wantCode string
	}{
		{
			name: "missing receipt",
			breakage: func(t *testing.T, store modules.Store) {
				remove(t, store.ReceiptPath("broken", "0.1.0"))
			},
			wantCode: "modules.receipt_missing",
		},
		{
			name: "malformed receipt",
			breakage: func(t *testing.T, store modules.Store) {
				if err := fixture.WriteRawReceipt(store.Root(), "broken", "0.1.0", []byte("{{")); err != nil {
					t.Fatalf("WriteRawReceipt returned %v", err)
				}
				if err := fixture.Activate(store.Root(), "broken", "0.1.0"); err != nil {
					t.Fatalf("Activate returned %v", err)
				}
			},
			wantCode: "modules.receipt_malformed",
		},
		{
			// Inventory must not report a module whose receipt changed after
			// activation, even though it never hashes the executable.
			name: "receipt rewritten after activation",
			breakage: func(t *testing.T, store modules.Store) {
				if err := fixture.WriteRawReceipt(store.Root(), "broken", "0.1.0", []byte("{{")); err != nil {
					t.Fatalf("WriteRawReceipt returned %v", err)
				}
			},
			wantCode: "modules.receipt_digest_mismatch",
		},
		{
			name: "no active version",
			breakage: func(t *testing.T, store modules.Store) {
				remove(t, store.ActivePath("broken"))
			},
			wantCode: "modules.no_active_version",
		},
		{
			name: "receipt declaring another namespace",
			breakage: func(t *testing.T, store modules.Store) {
				receipt, err := modules.ReadReceipt(store.ReceiptPath("broken", "0.1.0"))
				if err != nil {
					t.Fatalf("ReadReceipt returned %v", err)
				}
				receipt.Namespace = "reference"
				data, err := receipt.Encode()
				if err != nil {
					t.Fatalf("Encode returned %v", err)
				}
				if err := fixture.WriteRawReceipt(store.Root(), "broken", "0.1.0", data); err != nil {
					t.Fatalf("WriteRawReceipt returned %v", err)
				}
				if err := fixture.Activate(store.Root(), "broken", "0.1.0"); err != nil {
					t.Fatalf("Activate returned %v", err)
				}
			},
			wantCode: "modules.receipt_namespace_mismatch",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, _ := installReference(t, fixture.Module{Namespace: "reference", Version: "0.1.0"})
			if _, err := fixture.Install(store.Root(), fixture.Module{Namespace: "broken", Version: "0.1.0"}); err != nil {
				t.Fatalf("fixture.Install returned %v", err)
			}
			test.breakage(t, store)

			installed, problems, err := store.Inventory()
			if err != nil {
				t.Fatalf("Inventory returned %v", err)
			}
			if len(installed) != 1 || installed[0].Namespace != "reference" {
				t.Fatalf("Inventory entries = %+v, want only the healthy reference module", installed)
			}
			if len(problems) != 1 {
				t.Fatalf("Inventory problems = %+v, want exactly one", problems)
			}
			if problems[0].Namespace != "broken" || problems[0].Problem.Code != test.wantCode {
				t.Fatalf("problem = %+v, want namespace broken with code %s", problems[0], test.wantCode)
			}
		})
	}
}

func TestInventoryDoesNotVerifyTheExecutableDigest(t *testing.T) {
	// Reporting inventory neither launches nor trusts the executable, so a
	// tampered binary must not prevent offline version reporting. Resolution
	// before launch is where the digest is enforced.
	store, _ := installReference(t, fixture.Module{Namespace: "reference", Version: "0.1.0"})
	if err := fixture.TamperExecutable(store.Root(), "reference", "0.1.0", referenceExecutable, []byte("tampered")); err != nil {
		t.Fatalf("TamperExecutable returned %v", err)
	}

	installed, problems, err := store.Inventory()
	if err != nil {
		t.Fatalf("Inventory returned %v", err)
	}
	if len(installed) != 1 || len(problems) != 0 {
		t.Fatalf("Inventory = %+v, %+v; want the tampered module still listed", installed, problems)
	}

	if _, err := store.Resolve("reference", shellIdentity()); err == nil {
		t.Fatal("Resolve accepted a tampered executable")
	}
}
