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

//go:build windows

// Uninstalling on Windows, driven against a real install for the same reason the
// Unix runs are: what has to be removed is what the installer wrote, not what a
// test could arrange to look like it.
//
// What differs from Unix is the target. There is no profile block; the per-user
// PATH entry and the per-user WSO2_HOME variable are what must go, and every
// other entry in that PATH must survive untouched.
package acceptance_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const uninstallScriptRelPath = "scripts/uninstall.ps1"

func TestUninstallRemovesEverythingTheInstallerAdded(t *testing.T) {
	install := newInstallHarness(t)
	defer install.restoreUserEnvironment(t)
	if _, stderr, err := install.run(); err != nil {
		t.Fatalf("install.ps1 failed: %v\nstderr:\n%s", err, stderr)
	}
	binDir := filepath.Join(install.stateRoot, "bin")

	stdout, stderr, err := install.runUninstall()
	if err != nil {
		t.Fatalf("uninstall.ps1 failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	if _, statErr := os.Stat(install.installedBinary()); !os.IsNotExist(statErr) {
		t.Errorf("the binary is still installed at %s", install.installedBinary())
	}
	if userPath := install.userEnvironment(t, "Path"); strings.Contains(
		strings.ToLower(userPath), strings.ToLower(binDir)) {
		t.Errorf("user PATH still contains %s:\n%s", binDir, userPath)
	}
	if got := install.userEnvironment(t, "WSO2_HOME"); got != "" {
		t.Errorf("user WSO2_HOME is still set to %q", got)
	}
	if !strings.Contains(stdout, "Removed") {
		t.Errorf("output does not report what it removed:\n%s", stdout)
	}
}

func TestUninstallKeepsEveryOtherPathEntry(t *testing.T) {
	install := newInstallHarness(t)
	defer install.restoreUserEnvironment(t)
	if _, stderr, err := install.run(); err != nil {
		t.Fatalf("install.ps1 failed: %v\nstderr:\n%s", err, stderr)
	}
	// Whatever the user had on their PATH before is not this script's to touch,
	// and rewriting the variable is exactly where that damage would happen.
	before := install.savedEntries(t)

	if _, stderr, err := install.runUninstall(); err != nil {
		t.Fatalf("uninstall.ps1 failed: %v\nstderr:\n%s", err, stderr)
	}

	after := install.currentEntries(t)
	for _, entry := range before {
		if !containsEntry(after, entry) {
			t.Errorf("uninstalling dropped the unrelated PATH entry %q\nbefore: %v\nafter:  %v",
				entry, before, after)
		}
	}
}

func TestUninstallLeavesConfigurationAndCredentialsAlone(t *testing.T) {
	install := newInstallHarness(t)
	defer install.restoreUserEnvironment(t)
	if _, stderr, err := install.run(); err != nil {
		t.Fatalf("install.ps1 failed: %v\nstderr:\n%s", err, stderr)
	}

	contexts := filepath.Join(install.stateRoot, "cli", "contexts.json")
	if err := os.MkdirAll(filepath.Dir(contexts), 0o755); err != nil {
		t.Fatalf("preparing the state root returned %v", err)
	}
	if err := os.WriteFile(contexts, []byte(`{"version":2}`), 0o600); err != nil {
		t.Fatalf("writing the fixture contexts returned %v", err)
	}

	stdout, stderr, err := install.runUninstall()
	if err != nil {
		t.Fatalf("uninstall.ps1 failed: %v\nstderr:\n%s", err, stderr)
	}

	if _, statErr := os.Stat(contexts); statErr != nil {
		t.Errorf("uninstalling removed the user's contexts: %v", statErr)
	}
	if !strings.Contains(stdout, install.stateRoot) {
		t.Errorf("output does not say what it left in place:\n%s", stdout)
	}
}

func TestUninstallWithPurgeRemovesTheStateRoot(t *testing.T) {
	install := newInstallHarness(t)
	defer install.restoreUserEnvironment(t)
	if _, stderr, err := install.run(); err != nil {
		t.Fatalf("install.ps1 failed: %v\nstderr:\n%s", err, stderr)
	}

	if _, stderr, err := install.runUninstall("-Purge"); err != nil {
		t.Fatalf("uninstall.ps1 -Purge failed: %v\nstderr:\n%s", err, stderr)
	}

	if _, statErr := os.Stat(install.stateRoot); !os.IsNotExist(statErr) {
		t.Errorf("-Purge left the state root at %s", install.stateRoot)
	}
}

func TestUninstallSucceedsWhenNothingIsInstalled(t *testing.T) {
	install := newInstallHarness(t)
	defer install.restoreUserEnvironment(t)

	stdout, stderr, err := install.runUninstall()
	if err != nil {
		t.Fatalf("uninstall.ps1 failed with nothing installed: %v\nstdout:\n%s\nstderr:\n%s",
			err, stdout, stderr)
	}
	if !strings.Contains(stdout, "Nothing to remove") {
		t.Errorf("output does not say there was nothing to do:\n%s", stdout)
	}
}

// runUninstall invokes the uninstall script against the same isolated home and
// state root the install ran in.
func (i *installHarness) runUninstall(args ...string) (string, string, error) {
	i.t.Helper()

	script := filepath.Join(repoRoot(i.t), uninstallScriptRelPath)
	arguments := []string{
		"-NoLogo", "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-File", script,
	}
	command := exec.Command(powerShell(), append(arguments, args...)...)
	command.Env = i.scriptEnvironment()

	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout.String(), stderr.String(), err
}

// savedEntries reports the PATH entries as they were before the first run, and
// currentEntries what they are now. Comparing the two is how an unrelated entry
// lost to a rewrite is caught.
func (i *installHarness) savedEntries(t *testing.T) []string {
	t.Helper()
	if i.platform.savedUserEnvironment == nil {
		t.Fatal("no user environment was captured, so nothing can be compared")
	}
	return splitPathEntries(i.platform.savedUserEnvironment["Path"])
}

func (i *installHarness) currentEntries(t *testing.T) []string {
	t.Helper()
	return splitPathEntries(i.userEnvironment(t, "Path"))
}

func splitPathEntries(value string) []string {
	entries := []string{}
	for _, entry := range strings.Split(value, ";") {
		if trimmed := strings.TrimSpace(entry); trimmed != "" {
			entries = append(entries, trimmed)
		}
	}
	return entries
}

func containsEntry(entries []string, want string) bool {
	for _, entry := range entries {
		if strings.EqualFold(strings.TrimRight(entry, `\`), strings.TrimRight(want, `\`)) {
			return true
		}
	}
	return false
}
