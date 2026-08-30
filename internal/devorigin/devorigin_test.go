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

// What a local install has to be is not "some files in the store": it is an
// installation a later shell run cannot tell apart from a published one. So the
// central test here installs the reference module and then asks the store the
// same questions the shell asks it, from outside this package, rather than
// asserting on what Install returned about itself.
//
// The install builds the module executable, which costs the better part of a
// compile and makes this the slowest test in the package. It is not avoidable:
// an archive with no real executable in it would prove nothing about the digest
// the receipt records or about the shell being able to resolve it, which is
// what is being asked. internal/scaffold's tests pay the same cost for the same
// reason.
//
// Every root is a temporary directory. Nothing here may reach the developer's
// own WSO2 state: this package is not the test fixture installer, so
// state.GuardIsolated never runs over it, and the isolation is this test's own
// responsibility.
package devorigin_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/devorigin"
	"github.com/wso2/wso2-cli/internal/install"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/semver"
	"github.com/wso2/wso2-cli/internal/state"
)

// referenceNamespace is the module every test here installs. It is the one
// module this repository is guaranteed to carry.
const referenceNamespace = "reference"

func TestAnInstalledModuleIsIndistinguishableFromAPublishedOne(t *testing.T) {
	stateRoot := t.TempDir()
	shell := compatibleShell(t)

	result, err := devorigin.Install(context.Background(), devorigin.Request{
		RepositoryRoot: repositoryRoot(t),
		Namespace:      referenceNamespace,
		StateRoot:      stateRoot,
		Shell:          shell,
	})
	if err != nil {
		t.Fatalf("installing the reference module failed: %v", err)
	}
	if result.Version != devorigin.DefaultVersion {
		t.Errorf("installed version is %q, want the default %q", result.Version, devorigin.DefaultVersion)
	}

	store := modules.NewStore(state.ModuleStore(stateRoot))

	// The receipt is what the shell gates a launch on, so a local install that
	// wrote one the shell would refuse has installed nothing usable.
	receipt, err := modules.ReadReceipt(store.ReceiptPath(referenceNamespace, result.Version))
	if err != nil {
		t.Fatalf("reading the module receipt failed: %v", err)
	}
	if err := receipt.Validate(); err != nil {
		t.Errorf("the module receipt is not valid: %v", err)
	}
	if receipt.ModuleVersion != result.Version {
		t.Errorf("the receipt records version %q, want %q", receipt.ModuleVersion, result.Version)
	}

	// The pin is what stops a published release from replacing the developer's
	// own build the next time an update runs.
	policy, err := store.ReadPolicy(referenceNamespace)
	if err != nil {
		t.Fatalf("reading the version policy failed: %v", err)
	}
	if !policy.Pinned() {
		t.Error("the version policy records no pin, so an update run could replace this build")
	}
	if policy.PinnedVersion != result.Version {
		t.Errorf("the version policy pins %q, want %q", policy.PinnedVersion, result.Version)
	}

	// The digest is recomputed rather than read back from the receipt: a
	// receipt agreeing with itself says nothing about the file on disk.
	executable := filepath.Join(store.VersionDir(referenceNamespace, result.Version),
		install.ExecutableName(referenceNamespace, shell.Platform))
	digest, err := modules.FileDigest(executable)
	if err != nil {
		t.Fatalf("reading the installed executable failed: %v", err)
	}
	if digest != receipt.ExecutableSHA256 {
		t.Errorf("the installed executable digests to %s, and the receipt records %s",
			digest, receipt.ExecutableSHA256)
	}

	// The whole point of going through the real installer: the shell resolves
	// what it left behind.
	resolved, err := store.Resolve(referenceNamespace, shell)
	if err != nil {
		t.Fatalf("the shell cannot resolve the installed module: %v", err)
	}
	// Both sides go through the symlink resolution the store does, because a
	// temporary directory on macOS sits under a symlinked /tmp and the two
	// spellings name one file.
	if resolvedPath(t, resolved.ExecutablePath) != resolvedPath(t, executable) {
		t.Errorf("the shell resolved %q, want %q", resolved.ExecutablePath, executable)
	}
}

// A prerelease version keeps a developer's own build off the stable channel, so
// nobody following stable can ever be offered it.
func TestTheDefaultVersionLandsOnThePrereleaseChannel(t *testing.T) {
	parsed, err := semver.Parse(devorigin.DefaultVersion)
	if err != nil {
		t.Fatalf("the default version is not a semantic version: %v", err)
	}
	if channel := catalog.Channel(parsed); channel != catalog.ChannelPrerelease {
		t.Errorf("the default version falls on the %s channel, want %s", channel, catalog.ChannelPrerelease)
	}
}

// The trap this refusal exists for: a locally built shell reports 0.0.0-dev,
// which no scaffolded module's declared range contains, and the version is not
// part of what catalog.Select gates on. Without the refusal the install
// succeeds and every later launch fails, which tells the developer their module
// works when it cannot run at all.
func TestAShellTheModuleCannotBeLaunchedByIsRefusedBeforeAnythingIsInstalled(t *testing.T) {
	stateRoot := t.TempDir()
	shell := compatibleShell(t)
	shell.Version = parseVersion(t, "0.0.0-dev")

	_, err := devorigin.Install(context.Background(), devorigin.Request{
		RepositoryRoot: repositoryRoot(t),
		Namespace:      referenceNamespace,
		StateRoot:      stateRoot,
		Shell:          shell,
	})
	if err == nil {
		t.Fatal("installing for a shell the module cannot be launched by succeeded")
	}
	// The message has to name both sides, because the developer's next move
	// depends on which of the two they can change.
	for _, expected := range []string{"0.0.0-dev", declaredShellRange(t)} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("the refusal does not name %q: %v", expected, err)
		}
	}

	// Refused before the build, so there is nothing to clean up and no
	// half-installed module to explain.
	if _, err := os.Stat(state.ModuleStore(stateRoot)); !os.IsNotExist(err) {
		t.Errorf("the refused install left a module store behind: %v", err)
	}
}

func TestAnUnknownNamespaceIsRefused(t *testing.T) {
	_, err := devorigin.Install(context.Background(), devorigin.Request{
		RepositoryRoot: repositoryRoot(t),
		Namespace:      "nosuchproduct",
		StateRoot:      t.TempDir(),
		Shell:          compatibleShell(t),
	})
	if err == nil {
		t.Fatal("installing a module this repository does not declare succeeded")
	}
	if !strings.Contains(err.Error(), "nosuchproduct") {
		t.Errorf("the refusal does not name the namespace asked for: %v", err)
	}
}

// compatibleShell is a shell identity the reference module can be launched by:
// its protocol versions and its shell version are taken from what the module
// itself declares, so the test states one thing about compatibility rather than
// restating the module's declaration and drifting from it.
func compatibleShell(t *testing.T) modules.ShellIdentity {
	t.Helper()
	declaration := referenceDeclaration(t)
	supported, err := semver.ParseRange(declaration.Compatibility.Shell)
	if err != nil {
		t.Fatalf("the reference module declares an unreadable shell range: %v", err)
	}
	shellVersion := parseVersion(t, "1.0.0")
	if !supported.Contains(shellVersion) {
		t.Fatalf("the reference module declares %q, which does not contain the version this test uses",
			supported.String())
	}
	return modules.ShellIdentity{
		Version:          shellVersion,
		ProtocolVersions: declaration.Compatibility.ProtocolVersions,
		Platform:         modules.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH},
	}
}

func declaredShellRange(t *testing.T) string {
	t.Helper()
	return referenceDeclaration(t).Compatibility.Shell
}

func referenceDeclaration(t *testing.T) catalog.Declaration {
	t.Helper()
	declarations, err := catalog.Discover(repositoryRoot(t))
	if err != nil {
		t.Fatalf("discovering the modules in this checkout failed: %v", err)
	}
	for _, declaration := range declarations {
		if declaration.Namespace == referenceNamespace {
			return declaration
		}
	}
	t.Fatalf("this checkout declares no %s module", referenceNamespace)
	return catalog.Declaration{}
}

func resolvedPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolving %q failed: %v", path, err)
	}
	return resolved
}

func parseVersion(t *testing.T, input string) semver.Version {
	t.Helper()
	version, err := semver.Parse(input)
	if err != nil {
		t.Fatalf("%q is not a semantic version: %v", input, err)
	}
	return version
}

// repositoryRoot is this checkout, found from this file rather than from the
// working directory.
func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("the test source location is unknown")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}
