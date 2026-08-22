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

// Removing an installed module.
//
// Install, update, and list all have somewhere to put a module and no way to
// take one back out, which leaves a user editing the managed module store by
// hand. These tests drive the built shell the way a user does, and they ask
// what a user would ask afterwards: is it gone, is everything else still
// there, and can I tell a typo from a no-op.
package acceptance_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/state"
)

// listModules runs one inventory listing against a catalog origin.
//
// The origin is supplied rather than left to the shell's default because
// listing a non-empty inventory asks the catalog what is newest: a run without
// it reaches the published catalog, which would make an acceptance test depend
// on the network and on what is published that day.
func listModules(shell, stateRoot, origin string) (string, string, error) {
	environment := shellEnvironment(stateRoot, catalog.OriginEnvVar+"="+origin)
	return runShellWith(shell, environment, "module", "list")
}

// removeModule runs one removal and reports both streams and the exit error.
func removeModule(shell, stateRoot string, args ...string) (string, string, error) {
	return runShellWith(shell, shellEnvironment(stateRoot),
		append([]string{"module", "remove"}, args...)...)
}

// TestRemovingAModuleTakesItOffTheMachine is the whole of what removal means
// from outside: the module stops being installed, and nothing of it is left in
// the store for the next resolve to trip over.
func TestRemovingAModuleTakesItOffTheMachine(t *testing.T) {
	shell := buildShell(t)
	origin := newCatalogOrigin(t, hostPlatformOptions(), catalogStable)
	stateRoot := isolatedStateRoot(t)

	if _, stderr, err := installModuleFrom(shell, stateRoot, origin.server.URL, catalogNamespace); err != nil {
		t.Fatalf("installing returned %v\nstderr:\n%s", err, stderr)
	}

	stdout, stderr, err := removeModule(shell, stateRoot, catalogNamespace)
	if err != nil {
		t.Fatalf("removing returned %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	// A removal that does not say what it removed leaves the user guessing
	// whether the namespace they typed was the one that went.
	if !strings.Contains(stdout, catalogNamespace) {
		t.Errorf("the removal does not name what it removed:\n%s", stdout)
	}

	// Removal is not a matter of the inventory forgetting: the bytes go too. A
	// version directory left behind is an executable still on disk, and a
	// receipt left behind is something a later resolve would trust.
	store := modules.NewStore(state.ModuleStore(stateRoot))
	if _, err := os.Stat(store.NamespaceDir(catalogNamespace)); !os.IsNotExist(err) {
		t.Errorf("the namespace directory survives removal: %v", err)
	}
	if _, err := os.Stat(store.PolicyPath(catalogNamespace)); !os.IsNotExist(err) {
		t.Errorf("the module policy survives removal: %v", err)
	}

	inventory, _, err := listModules(shell, stateRoot, origin.server.URL)
	if err != nil {
		t.Fatalf("listing returned %v", err)
	}
	if strings.Contains(inventory, catalogNamespace) {
		t.Errorf("a removed module is still listed as installed:\n%s", inventory)
	}
}

// TestARemovedModulesCommandsStopResolving asks the question a user actually
// asks after removing something: is the command gone. The answer has to be the
// same one an unknown namespace has always produced, because from the user's
// side that is now exactly what it is.
func TestARemovedModulesCommandsStopResolving(t *testing.T) {
	shell := buildShell(t)
	origin := newCatalogOrigin(t, hostPlatformOptions(), catalogStable)
	stateRoot := isolatedStateRoot(t)

	if _, stderr, err := installModuleFrom(shell, stateRoot, origin.server.URL, catalogNamespace); err != nil {
		t.Fatalf("installing returned %v\nstderr:\n%s", err, stderr)
	}
	if _, stderr, err := removeModule(shell, stateRoot, catalogNamespace); err != nil {
		t.Fatalf("removing returned %v\nstderr:\n%s", err, stderr)
	}

	stdout, stderr, err := runShellWith(shell, shellEnvironment(stateRoot), catalogNamespace, "status")
	requireProblem(t, stdout, stderr, err, int(exit.Usage), "shell.unknown_command")
}

// TestRemovingOneModuleLeavesTheOthersInstalled states the boundary of a
// removal. One namespace is named, one namespace goes, and the store is not
// otherwise disturbed.
func TestRemovingOneModuleLeavesTheOthersInstalled(t *testing.T) {
	shell := buildShell(t)
	origin := newCatalogOrigin(t, hostPlatformOptions(),
		catalogStable, catalogOtherStable)
	stateRoot := isolatedStateRoot(t)

	for _, namespace := range []string{catalogNamespace, catalogOtherNamespace} {
		if _, stderr, err := installModuleFrom(shell, stateRoot, origin.server.URL, namespace); err != nil {
			t.Fatalf("installing %s returned %v\nstderr:\n%s", namespace, err, stderr)
		}
	}

	if _, stderr, err := removeModule(shell, stateRoot, catalogNamespace); err != nil {
		t.Fatalf("removing returned %v\nstderr:\n%s", err, stderr)
	}

	if got := installedVersion(t, stateRoot, catalogOtherNamespace); got == "" {
		t.Fatal("the module that was not named lost its active version")
	}
	// Listing an inventory that is not empty asks the catalog what is newest,
	// so this run has to be pointed at the fixture origin. Without it the shell
	// would fall back to the published one and this test would depend on the
	// network and on what is published today.
	inventory, _, err := listModules(shell, stateRoot, origin.server.URL)
	if err != nil {
		t.Fatalf("listing returned %v", err)
	}
	if !strings.Contains(inventory, catalogOtherNamespace) {
		t.Errorf("the module that was not named is no longer listed:\n%s", inventory)
	}
}

// TestRemovingAModuleLeavesConfigurationAndCredentialsAlone separates removal
// from logging out. A user who removes a module has said nothing about their
// identity, and taking their session with it would be a surprise they cannot
// undo without authenticating again.
func TestRemovingAModuleLeavesConfigurationAndCredentialsAlone(t *testing.T) {
	shell := buildShell(t)
	origin := newCatalogOrigin(t, hostPlatformOptions(), catalogStable)
	stateRoot := isolatedStateRoot(t)

	if _, stderr, err := installModuleFrom(shell, stateRoot, origin.server.URL, catalogNamespace); err != nil {
		t.Fatalf("installing returned %v\nstderr:\n%s", err, stderr)
	}

	// Everything in the state root that is not the module store, recorded
	// before and after. Naming the files individually would let a removal that
	// deleted something this test had not thought of pass.
	before := stateRootEntriesOutsideTheStore(t, stateRoot)
	if _, stderr, err := removeModule(shell, stateRoot, catalogNamespace); err != nil {
		t.Fatalf("removing returned %v\nstderr:\n%s", err, stderr)
	}
	after := stateRootEntriesOutsideTheStore(t, stateRoot)

	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Errorf("removal changed the state root outside the module store:\nbefore:\n%v\nafter:\n%v",
			before, after)
	}
}

// TestRemovingAModuleThatIsNotInstalledIsRefused keeps a typo distinguishable
// from a no-op. Reporting success for a namespace that was never there would
// tell a user their module is gone when something else is still installed
// under the name they meant.
func TestRemovingAModuleThatIsNotInstalledIsRefused(t *testing.T) {
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)

	stdout, stderr, err := removeModule(shell, stateRoot, catalogNamespace)
	requireProblem(t, stdout, stderr, err, int(exit.Usage), "shell.module_not_installed")

	// The refusal has to leave the user somewhere to go next, and what is
	// installed is the thing they need to see.
	if !strings.Contains(stderr, "wso2 module list") {
		t.Errorf("the refusal does not name how to see what is installed:\n%s", stderr)
	}
}

// TestRemovalTakesExactlyOneNamespace pins the argument shape, so that a run
// naming two modules refuses rather than removing the first and ignoring the
// second.
func TestRemovalTakesExactlyOneNamespace(t *testing.T) {
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)

	stdout, stderr, err := removeModule(shell, stateRoot)
	requireProblem(t, stdout, stderr, err, int(exit.Usage), "shell.missing_argument")

	stdout, stderr, err = removeModule(shell, stateRoot, catalogNamespace, catalogOtherNamespace)
	requireProblem(t, stdout, stderr, err, int(exit.Usage), "shell.unexpected_argument")
}

// TestRemovingThenReinstallingWorks is the loop a module developer lives in
// while iterating on their own build, and the one reason removal cannot leave
// anything behind: a stale receipt or version directory would make the next
// install resolve against something the developer already discarded.
func TestRemovingThenReinstallingWorks(t *testing.T) {
	shell := buildShell(t)
	origin := newCatalogOrigin(t, hostPlatformOptions(), catalogStable)
	stateRoot := isolatedStateRoot(t)

	if _, stderr, err := installModuleFrom(shell, stateRoot, origin.server.URL, catalogNamespace); err != nil {
		t.Fatalf("installing returned %v\nstderr:\n%s", err, stderr)
	}
	if _, stderr, err := removeModule(shell, stateRoot, catalogNamespace); err != nil {
		t.Fatalf("removing returned %v\nstderr:\n%s", err, stderr)
	}
	if _, stderr, err := installModuleFrom(shell, stateRoot, origin.server.URL, catalogNamespace); err != nil {
		t.Fatalf("reinstalling returned %v\nstderr:\n%s", err, stderr)
	}

	if got := installedVersion(t, stateRoot, catalogNamespace); got != "4.5.0" {
		t.Errorf("the reinstalled active version is %s, want 4.5.0", got)
	}
	versionOutput, _ := runShell(t, shell, stateRoot, "version")
	if !strings.Contains(versionOutput, "v4.5.0") {
		t.Errorf("wso2 version does not report the reinstalled module:\n%s", versionOutput)
	}
}

// stateRootEntriesOutsideTheStore lists every path under a state root that is
// not part of the managed module store, so a test can compare the whole of the
// rest of a user's state before and after a command.
func stateRootEntriesOutsideTheStore(t *testing.T, stateRoot string) []string {
	t.Helper()
	store := state.ModuleStore(stateRoot)

	var paths []string
	err := filepath.WalkDir(stateRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == store {
			return filepath.SkipDir
		}
		relative, relativeErr := filepath.Rel(stateRoot, path)
		if relativeErr != nil {
			return relativeErr
		}
		paths = append(paths, relative)
		return nil
	})
	if err != nil {
		t.Fatalf("walking the state root returned %v", err)
	}
	return paths
}
