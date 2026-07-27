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

// Package state locates the shell-owned local state tree.
//
// Every path the shell reads or writes descends from one root, so a test or an
// acceptance harness can redirect all local state by setting a single
// environment variable and can never touch a developer's real configuration.
package state

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RootEnvVar names the environment variable that overrides the state root.
// The acceptance harness sets it to a temporary directory.
const RootEnvVar = "WSO2_HOME"

// Root reports the shell-owned state root: the override when set, otherwise
// ~/.wso2. An override that is not absolute is rejected, because resolving it
// against the working directory would make local state depend on where the
// command was run.
func Root() (string, error) {
	if override := os.Getenv(RootEnvVar); override != "" {
		if !filepath.IsAbs(override) {
			return "", fmt.Errorf("state: %s must be an absolute path, got %q", RootEnvVar, override)
		}
		return filepath.Clean(override), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("state: cannot locate the user home directory: %w", err)
	}
	return filepath.Join(home, ".wso2"), nil
}

// ModuleStore reports the managed module store inside the given state root.
// Modules are resolved only from this directory; the shell never searches PATH
// or the working directory.
func ModuleStore(root string) string {
	return filepath.Join(root, "cli", "modules")
}

// GuardIsolated proves a path is somewhere a test may write.
//
// Every fixture that writes shell state calls it first, so a mistaken test can
// never modify a developer's configuration. It checks both the default state
// root and any root the environment currently selects, and it fails closed when
// it cannot tell where real state lives.
//
// The action names what the caller was about to do, so the refusal reads in the
// caller's own terms. Its errors carry no package prefix, because a fixture
// wraps them in its own.
func GuardIsolated(action, path string) error {
	if path == "" {
		return fmt.Errorf("an isolated path is required to %s", action)
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("the path to %s must be absolute, got %q", action, path)
	}
	cleaned := filepath.Clean(path)

	protected := make([]string, 0, 2)
	if configured := os.Getenv(RootEnvVar); configured != "" && filepath.IsAbs(configured) {
		protected = append(protected, filepath.Clean(configured))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		if len(protected) == 0 {
			return fmt.Errorf("cannot locate real WSO2 state to protect it: %w", err)
		}
	} else {
		protected = append(protected, filepath.Join(home, ".wso2"))
	}

	for _, root := range protected {
		if cleaned == root || within(root, cleaned) {
			return fmt.Errorf(
				"refusing to %s into WSO2 state at %s; pass an isolated directory such as one from t.TempDir()",
				action, root)
		}
	}
	return nil
}

// within reports whether candidate is a path below dir.
func within(dir, candidate string) bool {
	relative, err := filepath.Rel(dir, candidate)
	if err != nil {
		return false
	}
	if relative == "." || relative == ".." {
		return false
	}
	return !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
