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

//go:build !windows

// Uninstalling is driven against a real install rather than a hand-built
// imitation of one: every test here installs first, through the same script a
// user runs, and then removes it. What is asserted is that the machine is left as
// it was — with the deliberate exception of the state a user created, which
// survives unless they ask for it to go.
//
// A hand-placed binary and a hand-written profile block would prove the uninstall
// removes what a test wrote. Installing first is what proves it removes what the
// installer wrote.
package acceptance_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const uninstallScriptRelPath = "scripts/uninstall.sh"

func TestUninstallRemovesEverythingTheInstallerAdded(t *testing.T) {
	install := newInstallHarness(t)
	if _, stderr, err := install.run(); err != nil {
		t.Fatalf("install.sh failed: %v\nstderr:\n%s", err, stderr)
	}
	before := install.readProfile(t)

	stdout, stderr, err := install.runUninstall()
	if err != nil {
		t.Fatalf("uninstall.sh failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	if _, statErr := os.Stat(install.installedBinary()); !os.IsNotExist(statErr) {
		t.Errorf("the binary is still installed at %s", install.installedBinary())
	}
	profile := install.readProfile(t)
	if strings.Contains(profile, installBlockMarker) {
		t.Errorf("the profile still carries the install block:\n%s", profile)
	}
	// The lines the user had before any of this must come back exactly. A rewrite
	// that reformatted or dropped them would be worse than leaving the block.
	if !strings.Contains(profile, "# existing profile") {
		t.Errorf("uninstalling dropped the user's own profile lines:\n%s", profile)
	}
	if profile == before {
		t.Error("the profile is unchanged, so nothing was removed from it")
	}
	if !strings.Contains(stdout, "Removed") {
		t.Errorf("output does not report what it removed:\n%s", stdout)
	}
}

func TestUninstallLeavesConfigurationAndCredentialsAlone(t *testing.T) {
	install := newInstallHarness(t)
	if _, stderr, err := install.run(); err != nil {
		t.Fatalf("install.sh failed: %v\nstderr:\n%s", err, stderr)
	}

	// State a user would have created by using the CLI. Removing a binary is not a
	// request to destroy this, and doing so silently would be the worse default.
	contexts := filepath.Join(install.stateRoot, "cli", "contexts.json")
	if err := os.MkdirAll(filepath.Dir(contexts), 0o755); err != nil {
		t.Fatalf("preparing the state root returned %v", err)
	}
	if err := os.WriteFile(contexts, []byte(`{"version":2}`), 0o600); err != nil {
		t.Fatalf("writing the fixture contexts returned %v", err)
	}

	stdout, stderr, err := install.runUninstall()
	if err != nil {
		t.Fatalf("uninstall.sh failed: %v\nstderr:\n%s", err, stderr)
	}

	if _, statErr := os.Stat(contexts); statErr != nil {
		t.Errorf("uninstalling removed the user's contexts: %v", statErr)
	}
	// Someone who wanted everything gone has to be told that something is left.
	if !strings.Contains(stdout, install.stateRoot) {
		t.Errorf("output does not say what it left in place:\n%s", stdout)
	}
}

func TestUninstallWithPurgeRemovesTheStateRoot(t *testing.T) {
	install := newInstallHarness(t)
	if _, stderr, err := install.run(); err != nil {
		t.Fatalf("install.sh failed: %v\nstderr:\n%s", err, stderr)
	}

	if _, stderr, err := install.runUninstall("--purge"); err != nil {
		t.Fatalf("uninstall.sh --purge failed: %v\nstderr:\n%s", err, stderr)
	}

	if _, statErr := os.Stat(install.stateRoot); !os.IsNotExist(statErr) {
		t.Errorf("--purge left the state root at %s", install.stateRoot)
	}
}

func TestUninstallSucceedsWhenNothingIsInstalled(t *testing.T) {
	install := newInstallHarness(t)
	if install.writeProfile {
		if err := os.WriteFile(install.profilePath, []byte("# existing profile\n"), 0o644); err != nil {
			t.Fatalf("writing the fixture profile returned %v", err)
		}
	}

	stdout, stderr, err := install.runUninstall()
	if err != nil {
		t.Fatalf("uninstall.sh failed with nothing installed: %v\nstdout:\n%s\nstderr:\n%s",
			err, stdout, stderr)
	}

	if !strings.Contains(stdout, "Nothing to remove") {
		t.Errorf("output does not say there was nothing to do:\n%s", stdout)
	}
	// A profile it never wrote to must come back untouched.
	if profile := install.readProfile(t); profile != "# existing profile\n" {
		t.Errorf("the profile was modified with nothing installed:\n%q", profile)
	}
}

func TestUninstallStillRemovesTheBinaryAfterTheBlockWasRemovedByHand(t *testing.T) {
	install := newInstallHarness(t)
	if _, stderr, err := install.run(); err != nil {
		t.Fatalf("install.sh failed: %v\nstderr:\n%s", err, stderr)
	}
	// Someone who tidied their own profile has still got a binary to remove, and a
	// script that gave up at the missing block would leave it there.
	if err := os.WriteFile(install.profilePath, []byte("# existing profile\n"), 0o644); err != nil {
		t.Fatalf("rewriting the profile returned %v", err)
	}

	if _, stderr, err := install.runUninstall(); err != nil {
		t.Fatalf("uninstall.sh failed: %v\nstderr:\n%s", err, stderr)
	}

	if _, statErr := os.Stat(install.installedBinary()); !os.IsNotExist(statErr) {
		t.Errorf("the binary is still installed at %s", install.installedBinary())
	}
}

func TestUninstallLeavesAProfileAloneWhenTheBlockHasNoEnd(t *testing.T) {
	install := newInstallHarness(t)
	// A profile carrying the opening marker and no closing one, with the user's
	// own configuration after it. Treating "no end" as "to the end of the file"
	// would delete all of it — which is why this refuses instead.
	damaged := "# existing profile\n" + installBlockMarker +
		"\nexport PATH=\"/somewhere/bin:$PATH\"\n\nexport EDITOR=vim\nalias gs='git status'\n"
	if err := os.WriteFile(install.profilePath, []byte(damaged), 0o644); err != nil {
		t.Fatalf("writing the damaged profile returned %v", err)
	}

	stdout, stderr, err := install.runUninstall()
	if err != nil {
		t.Fatalf("uninstall.sh failed on a profile with no end marker: %v\nstderr:\n%s", err, stderr)
	}

	profile := install.readProfile(t)
	if profile != damaged {
		t.Errorf("the profile was rewritten:\nwant:\n%s\ngot:\n%s", damaged, profile)
	}
	// Saying nothing would leave the user with a block they do not know is there.
	if !strings.Contains(stdout+stderr, "end marker") {
		t.Errorf("nothing told the user the block was left in place:\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
}

func TestUninstallRemovesABlockFromAProfileTheRunningShellIsNotWiredIn(t *testing.T) {
	install := newInstallHarness(t)
	// Installed under zsh, uninstalled by a bash user: the block is in .zshrc and
	// checking only the profile for the current shell would leave it behind,
	// putting a directory that no longer exists on PATH forever.
	zshrc := filepath.Join(install.home, ".zshrc")
	if err := os.WriteFile(zshrc, []byte("# zsh profile\n"), 0o644); err != nil {
		t.Fatalf("writing the zsh profile returned %v", err)
	}
	install.profilePath = zshrc
	install.environment = append(install.environment, "SHELL=/bin/zsh")
	if _, stderr, err := install.run(); err != nil {
		t.Fatalf("install.sh failed: %v\nstderr:\n%s", err, stderr)
	}
	if profile := install.readProfile(t); !strings.Contains(profile, installBlockMarker) {
		t.Fatalf("the install did not wire the zsh profile, so this proves nothing:\n%s", profile)
	}

	// The uninstall runs as a bash user would.
	install.environment = append(install.environment, "SHELL=/bin/bash")
	if _, stderr, err := install.runUninstall(); err != nil {
		t.Fatalf("uninstall.sh failed: %v\nstderr:\n%s", err, stderr)
	}

	if profile := install.readProfile(t); strings.Contains(profile, installBlockMarker) {
		t.Errorf("the zsh profile still carries the install block:\n%s", profile)
	}
}

// runUninstall invokes the uninstall script against the same isolated home and
// state root the install ran in.
func (i *installHarness) runUninstall(args ...string) (string, string, error) {
	i.t.Helper()

	script := filepath.Join(repoRoot(i.t), uninstallScriptRelPath)
	command := exec.Command("bash", append([]string{script}, args...)...)
	command.Env = append([]string{
		"HOME=" + i.home,
		"PATH=" + os.Getenv("PATH"),
		"SHELL=/bin/bash",
	}, i.environment...)

	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout.String(), stderr.String(), err
}
