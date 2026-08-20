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

// Keeping installed modules current, driven through the same seam a user uses:
// the built shell, an isolated state root, and an origin serving what the real
// generator produced from a fixture tag set.
//
// Release history is extended by regenerating the catalog over more tags on the
// same origin, which is how a shell meets a product that has released since it
// last looked. What each run cost the origin is counted there rather than
// asserted about the shell's internals.
package acceptance_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/state"
)

// moduleCommandFrom runs one wso2 module subcommand against a catalog origin.
func moduleCommandFrom(shell, stateRoot, origin string, args ...string) (string, string, error) {
	environment := shellEnvironment(stateRoot, catalog.OriginEnvVar+"="+origin)
	return runShellWith(shell, environment, append([]string{"module"}, args...)...)
}

// requireLaunchable proves an installed module is not merely present but works:
// the shell resolves it, launches it, and the module itself answers.
//
// A refusal from the module is what proves the module ran. A missing or broken
// installation fails in the shell instead, and says so with a shell problem
// code, so the two cannot be confused.
func requireLaunchable(t *testing.T, shell, stateRoot, namespace string) {
	t.Helper()
	stdout, stderr, err := runShellWith(shell, shellEnvironment(stateRoot), namespace, "nosuchcommand")
	if err == nil {
		t.Fatalf("an unknown %s command succeeded:\n%s", namespace, stdout)
	}
	if !strings.Contains(stderr, namespace+".unknown_command") {
		t.Fatalf("the installed %s module did not answer:\n%s", namespace, stderr)
	}
}

// An update check makes one request whose response does not grow when release
// history is extended. That is the whole reason index.json exists: it carries
// the latest version per channel per namespace, so its size is bounded by
// namespaces times channels rather than by how long a product has been
// releasing.
func TestAnUpdateCheckCostsOneRequestThatDoesNotGrowWithHistory(t *testing.T) {
	shell := buildShell(t)
	origin := newCatalogOrigin(t, hostPlatformOptions(), catalogStable)
	stateRoot := isolatedStateRoot(t)

	if _, stderr, err := installModuleFrom(shell, stateRoot, origin.server.URL, catalogNamespace); err != nil {
		t.Fatalf("installing returned %v\n%s", err, stderr)
	}

	origin.forget()
	if stdout, stderr, err := moduleCommandFrom(shell, stateRoot, origin.server.URL, "list"); err != nil {
		t.Fatalf("listing returned %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if got := origin.totalRequests(); got != 1 {
		t.Errorf("an update check made %d requests, want one", got)
	}
	if got := origin.requestCount(catalog.IndexPath); got != 1 {
		t.Errorf("an update check fetched the index %d times, want once", got)
	}
	before := origin.fetch(t, catalog.IndexPath)
	historyBefore := origin.fetch(t, catalog.NamespacePath(catalogNamespace))

	// Two more releases exist. They are older than the one installed, so what
	// the index says is unchanged and only the history it points at grows.
	origin.generate(catalogAncientStable, catalogOlderStable, catalogStable)
	origin.forget()
	if stdout, stderr, err := moduleCommandFrom(shell, stateRoot, origin.server.URL, "list"); err != nil {
		t.Fatalf("listing after the history grew returned %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	if got := origin.totalRequests(); got != 1 {
		t.Errorf("an update check over a longer history made %d requests, want one", got)
	}
	after := origin.fetch(t, catalog.IndexPath)
	if len(after) > len(before) {
		t.Errorf("the index grew from %d to %d bytes when release history was extended:\n%s",
			len(before), len(after), after)
	}
	// The history must actually have grown, or the index staying the size it
	// was proves nothing at all.
	historyAfter := origin.fetch(t, catalog.NamespacePath(catalogNamespace))
	if len(historyAfter) <= len(historyBefore) {
		t.Fatalf("the version history did not grow: %d bytes then %d", len(historyBefore), len(historyAfter))
	}
}

// The shell lists which installed modules have updates available, and which do
// not, so a user can decide what to update.
func TestTheShellListsWhichInstalledModulesHaveUpdates(t *testing.T) {
	shell := buildShell(t)
	origin := newCatalogOrigin(t, hostPlatformOptions(), catalogOlderStable, catalogOtherStable)
	stateRoot := isolatedStateRoot(t)

	for _, namespace := range []string{catalogNamespace, catalogOtherNamespace} {
		if _, stderr, err := installModuleFrom(shell, stateRoot, origin.server.URL, namespace); err != nil {
			t.Fatalf("installing %s returned %v\n%s", namespace, err, stderr)
		}
	}

	// Only the reference module has released since. The other is current, and
	// saying so is as much a part of the report as naming the one that is not.
	origin.generate(catalogOlderStable, catalogStable, catalogOtherStable)
	stdout, stderr, err := moduleCommandFrom(shell, stateRoot, origin.server.URL, "list")
	if err != nil {
		t.Fatalf("listing returned %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	for _, want := range []string{catalogNamespace, "v4.4.0", "v4.5.0 available", catalogOtherNamespace, "current"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the update report does not mention %q:\n%s", want, stdout)
		}
	}
	// A report that called the current module outdated too would pass every
	// assertion above.
	if strings.Contains(stdout, "v1.0.0 available") {
		t.Errorf("the current module is reported as having an update:\n%s", stdout)
	}
}

// Updating installs the newest compatible version on the module's channel, and
// what it activates launches.
func TestUpdatingInstallsTheNewestCompatibleVersionOnTheChannel(t *testing.T) {
	shell := buildShell(t)
	origin := newCatalogOrigin(t, hostPlatformOptions(), catalogOlderStable)
	stateRoot := isolatedStateRoot(t)

	if _, stderr, err := installModuleFrom(shell, stateRoot, origin.server.URL, catalogNamespace); err != nil {
		t.Fatalf("installing returned %v\n%s", err, stderr)
	}
	if got := installedVersion(t, stateRoot, catalogNamespace); got != "4.4.0" {
		t.Fatalf("the active version before updating is %s, want 4.4.0", got)
	}

	// 4.6.0-rc.1 is newer than 4.7.0 is not the question: it is a prerelease,
	// and this module follows the stable channel.
	origin.generate(catalogOlderStable, catalogStable, catalogPrerelease, catalogAddedStable)
	stdout, stderr, err := moduleCommandFrom(shell, stateRoot, origin.server.URL, "update", catalogNamespace)
	if err != nil {
		t.Fatalf("updating returned %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	if got := installedVersion(t, stateRoot, catalogNamespace); got != "4.7.0" {
		t.Errorf("the active version is %s, want the newest stable 4.7.0", got)
	}
	if !strings.Contains(stdout, "4.4.0") || !strings.Contains(stdout, "4.7.0") {
		t.Errorf("the update does not report what it moved:\n%s", stdout)
	}
	requireLaunchable(t, shell, stateRoot, catalogNamespace)

	// Running again moves nothing and says so, rather than reinstalling.
	stdout, stderr, err = moduleCommandFrom(shell, stateRoot, origin.server.URL, "update", catalogNamespace)
	if err != nil {
		t.Fatalf("updating a current module returned %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "current") {
		t.Errorf("updating a current module does not say so:\n%s", stdout)
	}
}

// An update takes the newest version this shell can launch rather than the
// newest that exists, so updating never leaves a module that will not run.
func TestUpdatingDeclinesAVersionThisShellCannotLaunch(t *testing.T) {
	shell := buildShell(t)
	options := hostPlatformOptions()
	options.protocols = map[string][]int{catalogAddedStable: {testProtocolVersionNumber + 1}}
	origin := newCatalogOrigin(t, options, catalogOlderStable)
	stateRoot := isolatedStateRoot(t)

	if _, stderr, err := installModuleFrom(shell, stateRoot, origin.server.URL, catalogNamespace); err != nil {
		t.Fatalf("installing returned %v\n%s", err, stderr)
	}

	origin.generate(catalogOlderStable, catalogStable, catalogAddedStable)
	stdout, stderr, err := moduleCommandFrom(shell, stateRoot, origin.server.URL, "update", catalogNamespace)
	if err != nil {
		t.Fatalf("updating returned %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	if got := installedVersion(t, stateRoot, catalogNamespace); got != "4.5.0" {
		t.Errorf("the active version is %s, want the newest launchable 4.5.0", got)
	}
	requireLaunchable(t, shell, stateRoot, catalogNamespace)
	// The version it declined is published, so the assertion above is not
	// passing because the catalog never offered it.
	history := origin.namespaceFile(t, catalog.NamespacePath(catalogNamespace))
	if history.Versions[0].Version != "4.7.0" {
		t.Fatalf("the catalog's newest version is %s, want the unlaunchable 4.7.0", history.Versions[0].Version)
	}
}

// Channel is settable per module: a user takes a prerelease of one product
// without taking prereleases of all of them, and an update run honours each
// module's own choice.
func TestChannelIsChosenPerModule(t *testing.T) {
	shell := buildShell(t)
	origin := newCatalogOrigin(t, hostPlatformOptions(),
		catalogStable, catalogPrerelease, catalogOtherStable, catalogOtherPrerelease)
	stateRoot := isolatedStateRoot(t)

	if _, stderr, err := installModuleFrom(shell, stateRoot, origin.server.URL,
		catalogNamespace, "--channel", "prerelease"); err != nil {
		t.Fatalf("installing the prerelease returned %v\n%s", err, stderr)
	}
	if _, stderr, err := installModuleFrom(shell, stateRoot, origin.server.URL,
		catalogOtherNamespace); err != nil {
		t.Fatalf("installing the stable module returned %v\n%s", err, stderr)
	}
	if got := installedVersion(t, stateRoot, catalogNamespace); got != "4.6.0-rc.1" {
		t.Fatalf("the prerelease module is at %s, want 4.6.0-rc.1", got)
	}
	// The other module has a newer prerelease published and did not take it,
	// which is the half of the claim that a shell-wide channel would break.
	if got := installedVersion(t, stateRoot, catalogOtherNamespace); got != "1.0.0" {
		t.Fatalf("the stable module is at %s, want the newest stable 1.0.0", got)
	}

	// Both modules have released since: a newer stable for each, and no newer
	// prerelease for either.
	origin.generate(catalogStable, catalogPrerelease, catalogAddedStable,
		catalogOtherStable, catalogOtherPrerelease, catalogOtherNewer)
	stdout, stderr, err := moduleCommandFrom(shell, stateRoot, origin.server.URL, "update", "--all")
	if err != nil {
		t.Fatalf("updating returned %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	// The prerelease module stays on its channel: 4.7.0 is newer than the
	// prerelease it holds, and taking it would be a channel change nobody asked
	// for.
	if got := installedVersion(t, stateRoot, catalogNamespace); got != "4.6.0-rc.1" {
		t.Errorf("the prerelease module moved to %s, want to stay on 4.6.0-rc.1", got)
	}
	if got := installedVersion(t, stateRoot, catalogOtherNamespace); got != "1.1.0" {
		t.Errorf("the stable module is at %s, want the newer stable 1.1.0", got)
	}
}

// A pinned module installs the pinned version and stays pinned across an update
// run that moves everything else.
func TestAPinnedModuleStaysPinnedAcrossAnUpdateRun(t *testing.T) {
	shell := buildShell(t)
	origin := newCatalogOrigin(t, hostPlatformOptions(),
		catalogOlderStable, catalogStable, catalogOtherStable)
	stateRoot := isolatedStateRoot(t)

	if _, stderr, err := installModuleFrom(shell, stateRoot, origin.server.URL,
		catalogNamespace+"@4.4.0"); err != nil {
		t.Fatalf("installing the pinned version returned %v\n%s", err, stderr)
	}
	if _, stderr, err := installModuleFrom(shell, stateRoot, origin.server.URL,
		catalogOtherNamespace); err != nil {
		t.Fatalf("installing the unpinned module returned %v\n%s", err, stderr)
	}
	if got := installedVersion(t, stateRoot, catalogNamespace); got != "4.4.0" {
		t.Fatalf("the pinned module is at %s, want the pinned 4.4.0", got)
	}

	origin.generate(catalogOlderStable, catalogStable, catalogAddedStable,
		catalogOtherStable, catalogOtherNewer)

	// Two runs, because a pin that survived only the run that set it would pass
	// a single-run test while still being a one-shot argument rather than a
	// property of the installation.
	for run := 1; run <= 2; run++ {
		stdout, stderr, err := moduleCommandFrom(shell, stateRoot, origin.server.URL, "update", "--all")
		if err != nil {
			t.Fatalf("update run %d returned %v\nstdout:\n%s\nstderr:\n%s", run, err, stdout, stderr)
		}
		if !strings.Contains(stdout, "pinned") {
			t.Errorf("update run %d does not report the pin:\n%s", run, stdout)
		}
		if got := installedVersion(t, stateRoot, catalogNamespace); got != "4.4.0" {
			t.Fatalf("update run %d moved the pinned module to %s", run, got)
		}
	}
	// The run had something to do, so the pin was not honoured merely because
	// nothing was published.
	if got := installedVersion(t, stateRoot, catalogOtherNamespace); got != "1.1.0" {
		t.Errorf("the unpinned module is at %s, want the newer stable 1.1.0", got)
	}
	// And the pin is visible where a user would look for it.
	stdout, _, err := moduleCommandFrom(shell, stateRoot, origin.server.URL, "list")
	if err != nil {
		t.Fatalf("listing returned %v", err)
	}
	if !strings.Contains(stdout, "pinned to v4.4.0") {
		t.Errorf("the report does not show the pin:\n%s", stdout)
	}
}

// An update that fails partway leaves the previous version active and usable.
// Updating must never be able to take away a module that worked.
//
// The failure is arranged to happen after the download has been accepted and
// staged, which is the point at which a half-installed module could otherwise
// be left behind.
func TestAFailedUpdateLeavesThePreviousVersionActiveAndUsable(t *testing.T) {
	shell := buildShell(t)
	origin := newCatalogOrigin(t, hostPlatformOptions(), catalogOlderStable)
	stateRoot := isolatedStateRoot(t)

	if _, stderr, err := installModuleFrom(shell, stateRoot, origin.server.URL, catalogNamespace); err != nil {
		t.Fatalf("installing returned %v\n%s", err, stderr)
	}
	requireLaunchable(t, shell, stateRoot, catalogNamespace)

	// The newer release publishes an archive that is the one the catalog
	// describes, digest and size included, and is not a module archive.
	origin.options.carriesNoModule = map[string]bool{catalogStable: true}
	origin.generate(catalogOlderStable, catalogStable)

	stdout, stderr, err := moduleCommandFrom(shell, stateRoot, origin.server.URL, "update", catalogNamespace)

	requireProblem(t, stdout, stderr, err, 69, "modules.artifact_malformed")
	if got := installedVersion(t, stateRoot, catalogNamespace); got != "4.4.0" {
		t.Fatalf("the active version after a failed update is %s, want the previous 4.4.0", got)
	}
	// Present is not the claim. The previous version still launches and the
	// module still answers, which is the only thing a user cares about after an
	// update went wrong.
	requireLaunchable(t, shell, stateRoot, catalogNamespace)
	if !strings.Contains(stdout, "4.4.0") {
		t.Errorf("the failed update does not say what is still active:\n%s", stdout)
	}

	// Nothing of the version that failed survives anywhere in the store.
	store := modules.NewStore(state.ModuleStore(stateRoot))
	var survivors []string
	_ = filepath.Walk(store.NamespaceDir(catalogNamespace),
		func(path string, info os.FileInfo, walkErr error) error {
			if walkErr == nil && info != nil && strings.Contains(path, "4.5.0") {
				survivors = append(survivors, path)
			}
			return nil
		})
	if len(survivors) != 0 {
		t.Errorf("a failed update left the version it was installing behind: %v", survivors)
	}
}

// A pinned version installs non-interactively, so a pipeline reproduces the
// same version without depending on what is newest that day and without a
// terminal to answer a prompt.
func TestAPinnedVersionInstallsNonInteractively(t *testing.T) {
	shell := buildShell(t)
	origin := newCatalogOrigin(t, hostPlatformOptions(),
		catalogOlderStable, catalogStable, catalogAddedStable)

	// Two runs from separate state roots with nothing on standard input. A
	// prompt would read end of file and could not be answered, so a run that
	// asked anything could not succeed twice with the same result.
	var installed []string
	for run := 1; run <= 2; run++ {
		stateRoot := isolatedStateRoot(t)
		command := exec.Command(shell, "module", "install", catalogNamespace+"@4.5.0")
		command.Env = shellEnvironment(stateRoot, catalog.OriginEnvVar+"="+origin.server.URL)
		command.Stdin = strings.NewReader("")
		var stdout, stderr strings.Builder
		command.Stdout = &stdout
		command.Stderr = &stderr
		if err := command.Run(); err != nil {
			t.Fatalf("run %d returned %v\nstdout:\n%s\nstderr:\n%s", run, err, &stdout, &stderr)
		}
		installed = append(installed, installedVersion(t, stateRoot, catalogNamespace))

		// A newer version is published, so an install that ignored the pin
		// would land somewhere else.
		if got := installed[run-1]; got != "4.5.0" {
			t.Fatalf("run %d installed %s, want the pinned 4.5.0", run, got)
		}
	}
	if installed[0] != installed[1] {
		t.Errorf("two pinned runs installed %s and %s", installed[0], installed[1])
	}
}

// The modules available to install can be listed from the shell, so what exists
// is discoverable without reading documentation. It costs one request, because
// the index alone answers the question.
func TestTheAvailableModulesCanBeListedFromTheShell(t *testing.T) {
	shell := buildShell(t)
	origin := newCatalogOrigin(t, hostPlatformOptions(),
		catalogStable, catalogPrerelease, catalogOtherStable)
	stateRoot := isolatedStateRoot(t)
	origin.forget()

	stdout, stderr, err := moduleCommandFrom(shell, stateRoot, origin.server.URL, "available")
	if err != nil {
		t.Fatalf("listing the catalog returned %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	for _, want := range []string{
		catalogNamespace, "v4.5.0", "prerelease", "v4.6.0-rc.1",
		catalogOtherNamespace, "v1.0.0",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the catalog listing does not mention %q:\n%s", want, stdout)
		}
	}
	if got := origin.totalRequests(); got != 1 {
		t.Errorf("listing the catalog made %d requests, want one", got)
	}
	// Nothing was installed, so the listing is the catalog's contents rather
	// than a report of local state.
	if _, err := os.Stat(state.ModuleStore(stateRoot)); err == nil {
		t.Error("listing the catalog created a module store")
	}
}

// Naming a module that is not installed is a mistake rather than a silent
// no-op, because an update run over nothing looks exactly like one that worked.
func TestUpdatingAModuleThatIsNotInstalledIsRefused(t *testing.T) {
	shell := buildShell(t)
	origin := newCatalogOrigin(t, hostPlatformOptions(), catalogStable)
	stateRoot := isolatedStateRoot(t)

	stdout, stderr, err := moduleCommandFrom(shell, stateRoot, origin.server.URL, "update", catalogNamespace)

	requireProblem(t, stdout, stderr, err, 64, "modules.not_installed")
}

// An update run over several modules is partway when one of them fails: the
// module that failed keeps the version that worked, and the modules that did
// not fail still move. A run that stopped at the first refusal would leave the
// rest of a user's modules behind for a reason that has nothing to do with
// them.
//
// The whole run costs one index request however many modules it moves, which is
// the same property a check has and the reason the index is read once.
func TestAPartlyFailedUpdateRunMovesTheModulesThatDidNotFail(t *testing.T) {
	shell := buildShell(t)
	origin := newCatalogOrigin(t, hostPlatformOptions(), catalogOlderStable, catalogOtherStable)
	stateRoot := isolatedStateRoot(t)

	for _, namespace := range []string{catalogNamespace, catalogOtherNamespace} {
		if _, stderr, err := installModuleFrom(shell, stateRoot, origin.server.URL, namespace); err != nil {
			t.Fatalf("installing %s returned %v\n%s", namespace, err, stderr)
		}
	}
	requireLaunchable(t, shell, stateRoot, catalogNamespace)

	// The reference module's newer release publishes an archive that is not a
	// module archive, so its update fails after being downloaded and staged.
	origin.options.carriesNoModule = map[string]bool{catalogStable: true}
	origin.generate(catalogOlderStable, catalogStable, catalogOtherStable, catalogOtherNewer)
	origin.forget()

	stdout, stderr, err := moduleCommandFrom(shell, stateRoot, origin.server.URL, "update", "--all")

	requireProblem(t, stdout, stderr, err, 69, "modules.artifact_malformed")
	if got := installedVersion(t, stateRoot, catalogNamespace); got != "4.4.0" {
		t.Errorf("the module whose update failed is at %s, want the previous 4.4.0", got)
	}
	requireLaunchable(t, shell, stateRoot, catalogNamespace)
	if got := installedVersion(t, stateRoot, catalogOtherNamespace); got != "1.1.0" {
		t.Errorf("the module that did not fail is at %s, want the newer 1.1.0", got)
	}
	if got := origin.requestCount(catalog.IndexPath); got != 1 {
		t.Errorf("an update run fetched the index %d times, want once", got)
	}
}

// Every refusal in a run is reported, not just the one the run exits on.
func TestAnUpdateRunReportsEveryRefusal(t *testing.T) {
	shell := buildShell(t)
	origin := newCatalogOrigin(t, hostPlatformOptions(), catalogOlderStable, catalogOtherStable)
	stateRoot := isolatedStateRoot(t)

	for _, namespace := range []string{catalogNamespace, catalogOtherNamespace} {
		if _, stderr, err := installModuleFrom(shell, stateRoot, origin.server.URL, namespace); err != nil {
			t.Fatalf("installing %s returned %v\n%s", namespace, err, stderr)
		}
	}

	origin.options.carriesNoModule = map[string]bool{catalogStable: true, catalogOtherNewer: true}
	origin.generate(catalogOlderStable, catalogStable, catalogOtherStable, catalogOtherNewer)

	stdout, stderr, err := moduleCommandFrom(shell, stateRoot, origin.server.URL, "update", "--all")

	requireProblem(t, stdout, stderr, err, 69, "modules.artifact_malformed")
	// The refusal the run did not exit on names its own module, so a user is
	// not left to infer that a second module failed too.
	if !strings.Contains(stderr, "wso2-module-"+catalogOtherNamespace) {
		t.Errorf("the second refusal is not reported:\n%s", stderr)
	}
	for _, namespace := range []string{catalogNamespace, catalogOtherNamespace} {
		if got := installedVersion(t, stateRoot, namespace); got == "" {
			t.Errorf("%s has no active version after a failed run", namespace)
		}
	}
	if !strings.Contains(stdout, catalogNamespace) || !strings.Contains(stdout, catalogOtherNamespace) {
		t.Errorf("the run does not report both modules:\n%s", stdout)
	}
}
