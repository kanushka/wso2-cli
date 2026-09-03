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

// Installing a product module from the catalog, driven through the same seam a
// user uses: the built shell, an isolated state root, and an origin serving
// what the real generator produced from a fixture tag set.
//
// The refusals matter as much as the success. Each one is a case where a user
// could otherwise be shown something that looks like a broken download, so each
// is asserted to be distinguishable from the others by problem code and by exit
// class rather than merely to fail.
package acceptance_test

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/state"
)

// hostPlatformOptions publish for the platform the test runs on, which is what
// an install test needs: what it installs is resolved and launched.
func hostPlatformOptions() catalogOptions {
	return catalogOptions{platforms: []modules.Platform{hostPlatform}}
}

// installModuleFrom runs one install against a catalog origin and reports both
// streams and the exit error.
func installModuleFrom(shell, stateRoot, origin string, args ...string) (string, string, error) {
	environment := shellEnvironment(stateRoot, catalog.OriginEnvVar+"="+origin)
	return runShellWith(shell, environment, append([]string{"module", "install"}, args...)...)
}

// installedVersion reports the version of a namespace the store has active.
func installedVersion(t *testing.T, stateRoot, namespace string) string {
	t.Helper()
	store := modules.NewStore(state.ModuleStore(stateRoot))
	active, err := store.ReadActive(namespace)
	if err != nil {
		t.Fatalf("reading the active-version pointer returned %v", err)
	}
	return active.Version
}

// requireCleanStore proves a failed install left nothing behind: no version
// directory, no executable, and no receipt. A non-zero exit says only that the
// command failed, not that it failed without installing anything.
func requireCleanStore(t *testing.T, stateRoot, namespace string) {
	t.Helper()
	store := modules.NewStore(state.ModuleStore(stateRoot))
	entries, err := os.ReadDir(store.NamespaceDir(namespace))
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatalf("reading the namespace directory returned %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("a failed install left %d entries in %s", len(entries), store.NamespaceDir(namespace))
	}
}

// requireProblem proves a run failed with one exit class and named one problem
// code, which is what makes a refusal distinguishable rather than generic.
func requireProblem(t *testing.T, stdout, stderr string, err error, wantExit int, wantCode string) {
	t.Helper()
	var exitError *exec.ExitError
	if err == nil {
		t.Fatalf("the command succeeded, want the %s refusal:\nstdout:\n%s", wantCode, stdout)
	}
	if !errors.As(err, &exitError) || exitError.ExitCode() != wantExit {
		t.Fatalf("exit status = %v, want %d\nstderr:\n%s", err, wantExit, stderr)
	}
	if !strings.Contains(stderr, wantCode) {
		t.Fatalf("the refusal does not name %q:\n%s", wantCode, stderr)
	}
}

// Installing a module resolves the newest compatible version, verifies its
// digest, activates it, and writes a receipt.
func TestInstallingAModuleActivatesTheNewestCompatibleVersion(t *testing.T) {
	shell := buildShell(t)
	origin := newCatalogOrigin(t, hostPlatformOptions(),
		catalogOlderStable, catalogStable, catalogPrerelease)
	stateRoot := isolatedStateRoot(t)

	stdout, stderr, err := installModuleFrom(shell, stateRoot, origin.server.URL, catalogNamespace)
	if err != nil {
		t.Fatalf("installing returned %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	// 4.6.0-rc.1 is the newest release that exists, and it is a prerelease, so
	// selecting it would mean the channel had been ignored.
	if got := installedVersion(t, stateRoot, catalogNamespace); got != "4.5.0" {
		t.Errorf("the active version is %s, want the newest stable 4.5.0", got)
	}
	if !strings.Contains(stdout, "4.5.0") {
		t.Errorf("the install does not report what it installed:\n%s", stdout)
	}

	// A receipt the shell wrote is one the shell can resolve: reporting the
	// installed version reads the receipt, checks it against the active-version
	// pointer, and recomputes the executable digest.
	versionOutput, _ := runShell(t, shell, stateRoot, "version")
	if !strings.Contains(versionOutput, "v4.5.0") {
		t.Errorf("wso2 version does not report the installed module:\n%s", versionOutput)
	}

	// The installed module is the one the archive carried, and it launches.
	requireLaunchable(t, shell, stateRoot, catalogNamespace)
}

// A shell speaking only the older protocol installs the newest version it can
// rather than the newest that exists.
func TestAnOlderShellInstallsTheNewestVersionItCanLaunch(t *testing.T) {
	shell := buildShell(t)
	options := hostPlatformOptions()
	// The newest release requires a protocol this shell does not speak, so a
	// selection that took the highest version would install something the shell
	// could never launch.
	options.protocols = map[string][]int{catalogAddedStable: {testProtocolVersionNumber + 1}}
	origin := newCatalogOrigin(t, options, catalogOlderStable, catalogStable, catalogAddedStable)
	stateRoot := isolatedStateRoot(t)

	stdout, stderr, err := installModuleFrom(shell, stateRoot, origin.server.URL, catalogNamespace)
	if err != nil {
		t.Fatalf("installing returned %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	if got := installedVersion(t, stateRoot, catalogNamespace); got != "4.5.0" {
		t.Errorf("the active version is %s, want the newest launchable 4.5.0", got)
	}
	// The version it declined is published, so the test would pass vacuously if
	// the catalog did not carry it.
	history := origin.namespaceFile(t, catalog.NamespacePath(catalogNamespace))
	if history.Versions[0].Version != "4.7.0" {
		t.Fatalf("the catalog's newest version is %s, want the unlaunchable 4.7.0",
			history.Versions[0].Version)
	}
}

// A shell for which no compatible version exists is refused with a
// compatibility problem naming the protocol versions involved, rather than with
// something that reads as a broken download.
func TestAShellNoVersionSupportsIsRefusedNamingTheProtocols(t *testing.T) {
	// This shell speaks one protocol version no published release supports.
	shell := buildShellSpeaking(t, "9")
	origin := newCatalogOrigin(t, hostPlatformOptions(), catalogStable, catalogOlderStable)
	stateRoot := isolatedStateRoot(t)

	stdout, stderr, err := installModuleFrom(shell, stateRoot, origin.server.URL, catalogNamespace)

	requireProblem(t, stdout, stderr, err, 69, "modules.incompatible_protocol")
	for _, want := range []string{"v9", "v" + testProtocolVersion} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the refusal does not name the protocol %s:\n%s", want, stderr)
		}
	}
	requireCleanStore(t, stateRoot, catalogNamespace)
}

// A module whose version number is far ahead of or behind the shell's installs
// normally. The shell never compares a module's version against its own: the
// gate is the protocol versions intersected with the platform, and nothing
// else, so a product free to use its own version scheme cannot produce a
// spurious incompatibility.
func TestAModuleVersionFarFromTheShellsInstallsNormally(t *testing.T) {
	shell := buildShell(t)
	origin := newCatalogOrigin(t, hostPlatformOptions(), catalogAncientStable, catalogAddedStable)

	for _, tc := range []struct {
		name      string
		requested string
		want      string
	}{
		// The shell is 0.4.2. One of these is above it by four major versions
		// and the other below it, and both must install.
		{name: "far ahead", requested: catalogNamespace, want: "4.7.0"},
		{name: "far behind", requested: catalogNamespace + "@0.1.0", want: "0.1.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stateRoot := isolatedStateRoot(t)

			stdout, stderr, err := installModuleFrom(shell, stateRoot, origin.server.URL, tc.requested)
			if err != nil {
				t.Fatalf("installing %s returned %v\nstdout:\n%s\nstderr:\n%s",
					tc.requested, err, stdout, stderr)
			}

			if got := installedVersion(t, stateRoot, catalogNamespace); got != tc.want {
				t.Errorf("the active version is %s, want %s", got, tc.want)
			}
			if tc.want == testShellVersion {
				t.Error("the fixture module version equals the shell version, so it proves nothing")
			}
		})
	}
}

// A platform with no published artifact is told that module publishes nothing
// for it, so it does not read as a transient failure.
func TestAPlatformWithNoPublishedArtifactIsRefusedNamingThePlatform(t *testing.T) {
	shell := buildShell(t)
	// A platform no test runner is ever on, so every runner's own platform is
	// absent from this catalog.
	options := catalogOptions{platforms: []modules.Platform{{OS: "aix", Arch: "ppc64"}}}
	origin := newCatalogOrigin(t, options, catalogStable, catalogOlderStable)
	stateRoot := isolatedStateRoot(t)

	stdout, stderr, err := installModuleFrom(shell, stateRoot, origin.server.URL, catalogNamespace)

	requireProblem(t, stdout, stderr, err, 69, "modules.unsupported_platform")
	if !strings.Contains(stderr, hostPlatform.String()) {
		t.Errorf("the refusal does not name the platform %s:\n%s", hostPlatform, stderr)
	}
	requireCleanStore(t, stateRoot, catalogNamespace)
}

// A digest mismatch aborts before anything is activated, leaving no executable
// and no receipt.
func TestADigestMismatchAbortsLeavingNoExecutableAndNoReceipt(t *testing.T) {
	shell := buildShell(t)
	origin := newCatalogOrigin(t, hostPlatformOptions(), catalogStable)
	origin.corruptArchive(catalogStable, hostPlatform)
	stateRoot := isolatedStateRoot(t)

	stdout, stderr, err := installModuleFrom(shell, stateRoot, origin.server.URL, catalogNamespace)

	requireProblem(t, stdout, stderr, err, 69, "modules.artifact_digest_mismatch")
	requireCleanStore(t, stateRoot, catalogNamespace)
	// Nothing anywhere under the store may survive a refused download, staged
	// bytes included.
	store := modules.NewStore(state.ModuleStore(stateRoot))
	var survivors []string
	_ = filepath.Walk(store.Root(), func(path string, info os.FileInfo, walkErr error) error {
		if walkErr == nil && info != nil && !info.IsDir() {
			survivors = append(survivors, path)
		}
		return nil
	})
	if len(survivors) != 0 {
		t.Errorf("a refused download left files behind: %v", survivors)
	}
}

// An unreachable catalog origin is reported distinguishably from a module that
// does not exist, so a user can tell an outage from a mistake.
func TestAnUnreachableOriginIsDistinguishableFromAnUnknownModule(t *testing.T) {
	shell := buildShell(t)
	origin := newCatalogOrigin(t, hostPlatformOptions(), catalogStable)
	stateRoot := isolatedStateRoot(t)

	unreachableStdout, unreachableStderr, unreachableErr :=
		installModuleFrom(shell, stateRoot, unreachableOrigin(t), catalogNamespace)
	requireProblem(t, unreachableStdout, unreachableStderr, unreachableErr, 70, "catalog.origin_unreachable")

	unknownStdout, unknownStderr, unknownErr :=
		installModuleFrom(shell, stateRoot, origin.server.URL, "nosuchproduct")
	requireProblem(t, unknownStdout, unknownStderr, unknownErr, 64, "catalog.unknown_module")

	// The two are told apart by both halves of the reporting idiom, so neither
	// automation reading the exit class nor a person reading the code has to
	// guess which failure happened.
	if unreachableStderr == unknownStderr {
		t.Error("an unreachable origin and an unknown module report the same thing")
	}
	requireCleanStore(t, stateRoot, catalogNamespace)
}

// unreachableOrigin reports an origin nothing is listening on. The port was
// bound and released, so it is a refused connection rather than a name that
// might resolve to somebody else's server.
func unreachableOrigin(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a port returned %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("releasing the reserved port returned %v", err)
	}
	return "http://" + address
}

// The namespace file is fetched only when a specific version must be selected,
// and a command using an already-installed module makes no catalog request at
// all, so normal work does not depend on network access.
func TestNormalWorkMakesNoCatalogRequest(t *testing.T) {
	shell := buildShell(t)
	origin := newCatalogOrigin(t, hostPlatformOptions(), catalogStable, catalogOlderStable)
	stateRoot := isolatedStateRoot(t)

	stdout, stderr, err := installModuleFrom(shell, stateRoot, origin.server.URL, catalogNamespace)
	if err != nil {
		t.Fatalf("installing returned %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	// Selecting a version costs the index once, the history once, and the
	// archive once. Fetching the history more than once, or on an operation
	// that selects nothing, is what the index exists to avoid.
	if got := origin.requestCount(catalog.IndexPath); got != 1 {
		t.Errorf("installing fetched the index %d times, want once", got)
	}
	if got := origin.requestCount(catalog.NamespacePath(catalogNamespace)); got != 1 {
		t.Errorf("installing fetched the version history %d times, want once", got)
	}
	if got := origin.totalRequests(); got != 3 {
		t.Errorf("installing made %d requests, want the index, the history, and one archive", got)
	}

	origin.forget()
	// A product command and a version report both use the installed module, and
	// neither selects a version.
	//
	// The command has to be one the module answers. An unknown one is answered
	// by the shell from the declared tree without launching anything (#153), so
	// it would make no catalog request whether or not the module was usable,
	// and this assertion would hold vacuously.
	if _, stderr, err := runShellWith(shell, shellEnvironment(stateRoot, catalog.OriginEnvVar+"="+origin.server.URL),
		catalogNamespace, "status"); err != nil {
		t.Errorf("the installed module did not answer: %v\n%s", err, stderr)
	}
	runShell(t, shell, stateRoot, "version")

	if got := origin.totalRequests(); got != 0 {
		t.Errorf("using an installed module made %d catalog requests, want none", got)
	}
}

// The receipt written at installation carries what the catalog entry declared,
// so what the shell later gates a launch on is what was published rather than
// what the archive happened to contain.
func TestTheWrittenReceiptRecordsWhatTheCatalogPublished(t *testing.T) {
	shell := buildShell(t)
	origin := newCatalogOrigin(t, hostPlatformOptions(), catalogStable)
	stateRoot := isolatedStateRoot(t)

	if _, stderr, err := installModuleFrom(shell, stateRoot, origin.server.URL, catalogNamespace); err != nil {
		t.Fatalf("installing returned %v\n%s", err, stderr)
	}

	store := modules.NewStore(state.ModuleStore(stateRoot))
	data, err := os.ReadFile(store.ReceiptPath(catalogNamespace, "4.5.0"))
	if err != nil {
		t.Fatalf("reading the written receipt returned %v", err)
	}
	var receipt modules.Receipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatalf("the written receipt is not readable: %v", err)
	}

	published := origin.namespaceFile(t, catalog.NamespacePath(catalogNamespace)).Versions[0]
	if receipt.Compatibility.Shell != published.Compatibility.Shell {
		t.Errorf("the receipt declares shell range %q, the catalog published %q",
			receipt.Compatibility.Shell, published.Compatibility.Shell)
	}
	if len(receipt.Compatibility.ProtocolVersions) != 1 ||
		receipt.Compatibility.ProtocolVersions[0] != testProtocolVersionNumber {
		t.Errorf("the receipt declares protocol versions %v, the catalog published %v",
			receipt.Compatibility.ProtocolVersions, published.Compatibility.ProtocolVersions)
	}
	if receipt.Platform != hostPlatform {
		t.Errorf("the receipt records platform %s, want %s", receipt.Platform, hostPlatform)
	}
	// The broker intersects a runtime request with what the receipt records, so
	// a receipt written from a catalog entry that carried no capabilities would
	// deny every brokered request a module makes. What the module declares is
	// published and reaches the receipt.
	if len(published.Capabilities.AuthAudiences) == 0 {
		t.Fatalf("the catalog published no capabilities: %+v", published.Capabilities)
	}
	if !slices.Equal(receipt.Capabilities.AuthAudiences, published.Capabilities.AuthAudiences) ||
		!slices.Equal(receipt.Capabilities.AuthScopes, published.Capabilities.AuthScopes) {
		t.Errorf("the receipt records capabilities %+v, the catalog published %+v",
			receipt.Capabilities, published.Capabilities)
	}
}

// A download that is the archive the catalog describes but is not a module
// archive fails after the bytes have been accepted and staged, which is the
// point at which a half-installed module could be left behind. Nothing may
// survive it either.
func TestAnArchiveCarryingNoModuleLeavesNothingBehind(t *testing.T) {
	shell := buildShell(t)
	options := hostPlatformOptions()
	options.carriesNoModule = map[string]bool{catalogStable: true}
	origin := newCatalogOrigin(t, options, catalogStable)
	stateRoot := isolatedStateRoot(t)

	stdout, stderr, err := installModuleFrom(shell, stateRoot, origin.server.URL, catalogNamespace)

	requireProblem(t, stdout, stderr, err, 69, "modules.artifact_malformed")
	requireCleanStore(t, stateRoot, catalogNamespace)
}

// Selection honours the channel rather than the highest version: choosing the
// prerelease channel takes the newest release on it, and choosing nothing takes
// the newest stable even when a prerelease is newer.
func TestAChannelSelectionTakesTheNewestOnThatChannel(t *testing.T) {
	shell := buildShell(t)
	origin := newCatalogOrigin(t, hostPlatformOptions(),
		catalogStable, catalogPrerelease, catalogAddedStable)

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "prerelease channel", args: []string{catalogNamespace, "--channel", "prerelease"}, want: "4.6.0-rc.1"},
		// 4.6.0-rc.1 is newer than 4.7.0 in neither direction that matters: it
		// is a prerelease, so the stable channel passes over it.
		{name: "default channel", args: []string{catalogNamespace}, want: "4.7.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stateRoot := isolatedStateRoot(t)

			stdout, stderr, err := installModuleFrom(shell, stateRoot, origin.server.URL, tc.args...)
			if err != nil {
				t.Fatalf("installing returned %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
			}

			if got := installedVersion(t, stateRoot, catalogNamespace); got != tc.want {
				t.Errorf("the active version is %s, want %s", got, tc.want)
			}
		})
	}
}

// TestVerboseInstallKeepsProgressOffStdout proves ADR 0003 at the command
// level for this wave's progress reporting, not only inside
// internal/output's own unit tests: running module install non-interactively
// (this harness always pipes both streams, so IsTerminal is false for
// either) with --verbose must add the download's periodic progress lines to
// stderr and must never change what stdout renders. wso2 module install does
// not support --output at all — it is refused outright, confirmed by hand
// (`wso2 module install <module> --output json` exits 64 with
// shell.unsupported_flag) because the module family in internal/app/module.go
// "module" case returns nil — so this is the applicable analogue of a
// "--output json is unaffected" test for this command family: stdout is a
// fixed, non-JSON message either way, and what matters is that --verbose
// cannot perturb it.
//
// What this test can and cannot catch, checked by hand: verboseProgress
// (internal/output/progress.go) writes through the pre-existing Logger, which
// module.go's factory never touches directly — it only hands NewProgress a
// writer for the IsTerminal check and the (here unreached) terminal renderer.
// So swapping which Streams field that writer argument is does NOT change
// what this test observes, and I confirmed that: making the factory pass
// s.Streams.Out instead of s.Streams.Err left this test green. What the test
// DOES catch is the realistic failure mode — a stray direct write that
// bypasses Streams altogether, such as an errant fmt.Println. I confirmed
// that by adding one inside verboseProgress.Report and watching this test
// fail with the leaked line inline in stdout, then reverting it.
func TestVerboseInstallKeepsProgressOffStdout(t *testing.T) {
	shell := buildShell(t)
	origin := newCatalogOrigin(t, hostPlatformOptions(), catalogStable)

	quietRoot := isolatedStateRoot(t)
	quietStdout, _, err := installModuleFrom(shell, quietRoot, origin.server.URL, catalogNamespace)
	if err != nil {
		t.Fatalf("the quiet install failed: %v", err)
	}

	verboseRoot := isolatedStateRoot(t)
	verboseStdout, verboseStderr, err := installModuleFrom(shell, verboseRoot, origin.server.URL,
		catalogNamespace, "--verbose")
	if err != nil {
		t.Fatalf("the verbose install failed: %v", err)
	}

	if verboseStdout != quietStdout {
		t.Errorf("--verbose changed stdout:\nwithout --verbose:\n%s\nwith --verbose:\n%s",
			quietStdout, verboseStdout)
	}
	for _, leaked := range []string{"download progress", "Downloading"} {
		if strings.Contains(verboseStdout, leaked) {
			t.Errorf("stdout contains %q, which belongs on stderr as a diagnostic:\n%s", leaked, verboseStdout)
		}
	}
	if !strings.Contains(verboseStderr, "download progress") {
		t.Errorf("--verbose did not report download progress on stderr:\n%s", verboseStderr)
	}
}

// A pinned install says a pin was created and how to clear it, and the plain
// install that clears it says so too. The pin is a side effect of how the
// module was named, and a plain install being the way out is written nowhere
// else, so the two runs that change the pin are the two that must name it (F7).
func TestAnInstallReportsThePinItCreatesAndThePinItClears(t *testing.T) {
	shell := buildShell(t)
	origin := newCatalogOrigin(t, hostPlatformOptions(), catalogOlderStable, catalogStable)
	stateRoot := isolatedStateRoot(t)

	stdout, stderr, err := installModuleFrom(shell, stateRoot, origin.server.URL, catalogNamespace+"@4.4.0")
	if err != nil {
		t.Fatalf("the pinning install returned %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "Pinned reference to v4.4.0.") {
		t.Errorf("the pinning install does not say a pin was created:\n%s", stdout)
	}
	if !strings.Contains(stdout, "run wso2 module install reference to clear it") {
		t.Errorf("the pinning install does not name the command that clears the pin:\n%s", stdout)
	}

	stdout, stderr, err = installModuleFrom(shell, stateRoot, origin.server.URL, catalogNamespace)
	if err != nil {
		t.Fatalf("the clearing install returned %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "The pin to v4.4.0 was cleared") {
		t.Errorf("the clearing install does not say the pin was cleared:\n%s", stdout)
	}
	if got := installedVersion(t, stateRoot, catalogNamespace); got != "4.5.0" {
		t.Errorf("the active version is %s, want the newest stable 4.5.0 once the pin is cleared", got)
	}

	// A third, plain install of the now-unpinned module claims nothing about
	// pins either way.
	stdout, stderr, err = installModuleFrom(shell, stateRoot, origin.server.URL, catalogNamespace)
	if err != nil {
		t.Fatalf("the repeated install returned %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if strings.Contains(stdout, "pin") || strings.Contains(stdout, "Pinned") {
		t.Errorf("an install of an unpinned module talks about pins:\n%s", stdout)
	}
}
