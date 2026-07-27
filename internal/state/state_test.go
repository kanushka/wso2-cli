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

package state

import (
	"path/filepath"
	"testing"
)

func TestRootPrefersTheOverrideEnvironmentVariable(t *testing.T) {
	isolated := t.TempDir()
	t.Setenv(RootEnvVar, isolated)

	root, err := Root()
	if err != nil {
		t.Fatalf("Root returned %v", err)
	}
	if root != isolated {
		t.Fatalf("Root() = %q, want the isolated override %q", root, isolated)
	}
}

func TestRootFallsBackToTheUserHomeDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv(RootEnvVar, "")
	t.Setenv("HOME", home)

	root, err := Root()
	if err != nil {
		t.Fatalf("Root returned %v", err)
	}
	if want := filepath.Join(home, ".wso2"); root != want {
		t.Fatalf("Root() = %q, want %q", root, want)
	}
}

func TestRootRejectsARelativeOverrideRatherThanGuessing(t *testing.T) {
	t.Setenv(RootEnvVar, "relative/state")

	if _, err := Root(); err == nil {
		t.Fatal("Root accepted a relative override; an ambiguous state root must fail closed")
	}
}

func TestModuleStorePathsAreDerivedFromTheRoot(t *testing.T) {
	store := ModuleStore("/isolated")

	if want := filepath.Join("/isolated", "cli", "modules"); store != want {
		t.Fatalf("ModuleStore = %q, want %q", store, want)
	}
}
