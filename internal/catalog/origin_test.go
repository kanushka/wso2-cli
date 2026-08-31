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

package catalog

import (
	"testing"

	"github.com/wso2/wso2-cli/internal/preferences"
)

// TestOriginFallsBackToTheDefault pins the innermost layer: with nothing set
// anywhere, Origin reports DefaultOrigin.
func TestOriginFallsBackToTheDefault(t *testing.T) {
	// Cleared explicitly: this variable is set by the acceptance harness and
	// by anyone pointing the shell at a local origin, and Origin reads it
	// first, so a test asserting a lower layer fails in that environment
	// rather than in this code.
	t.Setenv(OriginEnvVar, "")
	if got := Origin(t.TempDir()); got != DefaultOrigin {
		t.Errorf("Origin() = %q, want DefaultOrigin %q", got, DefaultOrigin)
	}
}

// TestOriginConfigurationWinsOverTheDefault pins the middle layer: a
// configured catalog-origin preference is used when the environment variable
// is not set.
func TestOriginConfigurationWinsOverTheDefault(t *testing.T) {
	// Cleared explicitly: this variable is set by the acceptance harness and
	// by anyone pointing the shell at a local origin, and Origin reads it
	// first, so a test asserting a lower layer fails in that environment
	// rather than in this code.
	t.Setenv(OriginEnvVar, "")
	root := t.TempDir()
	setCatalogOrigin(t, root, "https://configured.example/catalog")

	if got, want := Origin(root), "https://configured.example/catalog"; got != want {
		t.Errorf("Origin() = %q, want %q", got, want)
	}
}

// TestOriginEnvVarWinsOverConfiguration is this task's most load-bearing
// test: WSO2_CLI_CATALOG_ORIGIN exists so the acceptance suite can point the
// shell at a local origin and no test ever reaches the real one. If a
// configured preference could override it, a developer's saved preference
// would silently redirect that suite. This proves the environment variable
// wins even when a preference names a different origin.
func TestOriginEnvVarWinsOverConfiguration(t *testing.T) {
	root := t.TempDir()
	setCatalogOrigin(t, root, "https://configured.example/catalog")
	t.Setenv(OriginEnvVar, "https://env.example/catalog")

	if got, want := Origin(root), "https://env.example/catalog"; got != want {
		t.Errorf("Origin() = %q, want the env var's %q, not the configured origin", got, want)
	}
}

// TestOriginTrimsATrailingSlashFromEitherSource proves the trailing-slash
// normalisation applies uniformly, whichever layer supplied the value.
func TestOriginTrimsATrailingSlashFromEitherSource(t *testing.T) {
	t.Run("from configuration", func(t *testing.T) {
		root := t.TempDir()
		// Cleared for the same reason as the tests above: this subtest asserts
		// the configured layer, which the environment variable outranks.
		t.Setenv(OriginEnvVar, "")
		setCatalogOrigin(t, root, "https://configured.example/catalog/")
		if got, want := Origin(root), "https://configured.example/catalog"; got != want {
			t.Errorf("Origin() = %q, want %q", got, want)
		}
	})
	t.Run("from the environment", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv(OriginEnvVar, "https://env.example/catalog/")
		if got, want := Origin(root), "https://env.example/catalog"; got != want {
			t.Errorf("Origin() = %q, want %q", got, want)
		}
	})
}

// setCatalogOrigin writes the catalog-origin preference through this
// package's own writer, the way wso2 config set would.
func setCatalogOrigin(t *testing.T, stateRoot, value string) {
	t.Helper()
	err := preferences.Update(stateRoot, func(document preferences.Document) (preferences.Document, error) {
		document.SchemaVersion = preferences.SchemaVersion
		return document.Set(preferences.KeyCatalogOrigin, value)
	})
	if err != nil {
		t.Fatalf("preferences.Update: %v", err)
	}
}
