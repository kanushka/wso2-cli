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

// The install script is driven exactly as a user drives it: the real script,
// from this checkout, against a release it downloads over HTTP. Nothing here
// asserts on the script's internals, because a user cannot see them; what is
// asserted is what a user is left with — a binary that runs, a profile that
// gained one block, and an aborted run that left neither behind.
//
// Two isolations make that safe to run anywhere. The home directory and the
// state root are temporary directories, so no run can reach the developer's
// real profile or state, and the release origin points at a local test server,
// so no run reaches GitHub. A test that forgot either would install onto the
// machine running it, so both are supplied by one helper rather than per test.
package acceptance_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The fixture release these run against is in install_fixture_test.go, shared
// with the Windows runs so that both scripts are proven against one contract.
const installScriptRelPath = "scripts/install.sh"

// platformFields is what only the Unix runs need.
type platformFields struct {
	// unameMachine is what `uname -m` answers, for reaching the
	// unsupported-hardware path without unsupported hardware.
	unameMachine string
}

func TestInstallScriptInstallsTheShellAndWiresThePath(t *testing.T) {
	install := newInstallHarness(t)

	stdout, stderr, err := install.run()
	if err != nil {
		t.Fatalf("install.sh failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	binary := filepath.Join(install.stateRoot, "bin", "wso2")
	info, statErr := os.Stat(binary)
	if statErr != nil {
		t.Fatalf("expected an installed binary at %s: %v", binary, statErr)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("installed binary has mode %v, want the execute bit set", info.Mode().Perm())
	}

	// The point of installing a binary is being able to run it. A file with the
	// right name that cannot execute would satisfy every assertion above.
	reported, runErr := exec.Command(binary, "version").CombinedOutput()
	if runErr != nil {
		t.Fatalf("the installed binary did not run: %v\noutput:\n%s", runErr, reported)
	}
	if !strings.Contains(string(reported), fixtureStableTag) {
		t.Errorf("installed binary reported %q, want it to mention %s", reported, fixtureStableTag)
	}

	profile := install.readProfile(t)
	if blocks := strings.Count(profile, installBlockMarker); blocks != 1 {
		t.Errorf("profile carries %d install blocks, want exactly 1:\n%s", blocks, profile)
	}
	if want := filepath.Join(install.stateRoot, "bin"); !strings.Contains(profile, want) {
		t.Errorf("profile does not put %s on PATH:\n%s", want, profile)
	}
	// A user who is not told which file changed cannot undo it, and cannot know
	// why their current shell still fails to find the command.
	if !strings.Contains(stdout, install.profilePath) {
		t.Errorf("output does not name the profile it edited (%s):\n%s", install.profilePath, stdout)
	}
}

func TestInstallScriptIsIdempotent(t *testing.T) {
	install := newInstallHarness(t)

	if _, stderr, err := install.run(); err != nil {
		t.Fatalf("first install failed: %v\nstderr:\n%s", err, stderr)
	}
	binary := filepath.Join(install.stateRoot, "bin", "wso2")
	if err := os.WriteFile(binary, []byte("stale"), 0o755); err != nil {
		t.Fatalf("could not stale the installed binary: %v", err)
	}

	if _, stderr, err := install.run(); err != nil {
		t.Fatalf("second install failed: %v\nstderr:\n%s", err, stderr)
	}

	// Re-running is the documented upgrade path, so the binary must be replaced
	// rather than left as whatever was there.
	replaced, err := os.ReadFile(binary)
	if err != nil {
		t.Fatalf("reading the reinstalled binary returned %v", err)
	}
	if string(replaced) == "stale" {
		t.Error("the second run did not replace the installed binary")
	}
	profile := install.readProfile(t)
	if blocks := strings.Count(profile, installBlockMarker); blocks != 1 {
		t.Errorf("profile carries %d install blocks after two runs, want exactly 1:\n%s", blocks, profile)
	}
}

func TestInstallScriptRefusesAnArchiveThatFailsItsChecksum(t *testing.T) {
	install := newInstallHarness(t)
	install.corruptArchive = true

	stdout, stderr, err := install.run()
	if err == nil {
		t.Fatalf("install.sh succeeded on a corrupted archive\nstdout:\n%s", stdout)
	}

	// The refusal has to be legible as a verification failure. A generic failure
	// would leave a user guessing whether the download merely broke.
	if !strings.Contains(strings.ToLower(stdout+stderr), "checksum") {
		t.Errorf("refusal does not mention the checksum:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	// Nothing may survive a failed verification: an executable installed from an
	// archive that failed its checksum is the whole risk this check exists for.
	if _, statErr := os.Stat(filepath.Join(install.stateRoot, "bin", "wso2")); !os.IsNotExist(statErr) {
		t.Error("a binary was installed from an archive that failed verification")
	}
	if profile := install.readProfile(t); strings.Contains(profile, installBlockMarker) {
		t.Errorf("the profile was edited by a run that failed verification:\n%s", profile)
	}
}

func TestInstallScriptInstallsAPinnedVersion(t *testing.T) {
	install := newInstallHarness(t)

	stdout, stderr, err := install.run(fixtureOlderTag)
	if err != nil {
		t.Fatalf("install.sh failed for a pinned version: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	reported, runErr := exec.Command(filepath.Join(install.stateRoot, "bin", "wso2"), "version").CombinedOutput()
	if runErr != nil {
		t.Fatalf("the installed binary did not run: %v", runErr)
	}
	if !strings.Contains(string(reported), fixtureOlderTag) {
		t.Errorf("installed binary reported %q, want the pinned %s", reported, fixtureOlderTag)
	}
}

func TestInstallScriptResolvesAPrereleaseOnlyWhenAsked(t *testing.T) {
	t.Run("the default resolves the newest stable release", func(t *testing.T) {
		install := newInstallHarness(t)

		if _, stderr, err := install.run(); err != nil {
			t.Fatalf("install.sh failed: %v\nstderr:\n%s", err, stderr)
		}

		// Asserted positively. Checking only that the prerelease tag is absent
		// would pass just as well if nothing had been installed at all.
		if reported := install.reportedVersion(t); reported != fixtureStableTag {
			t.Errorf("the default install resolved %s, want the newest stable %s",
				reported, fixtureStableTag)
		}
	})

	t.Run("the opt-in resolves the newest prerelease", func(t *testing.T) {
		install := newInstallHarness(t)
		install.environment = append(install.environment, "WSO2_CLI_PRERELEASE=true")

		if _, stderr, err := install.run(); err != nil {
			t.Fatalf("install.sh failed: %v\nstderr:\n%s", err, stderr)
		}

		if reported := install.reportedVersion(t); reported != fixturePreleaseTag {
			t.Errorf("the prerelease opt-in installed %s, want %s", reported, fixturePreleaseTag)
		}
	})
}

func TestInstallScriptReadsTheChecksumLineForTheArchiveItDownloaded(t *testing.T) {
	install := newInstallHarness(t)
	// A checksum file that lists a longer name ending in this archive's name
	// first — the shape a future signature or bundle artifact would add. A
	// filename matched as a substring takes the first such line, so it would
	// compare the archive against another artifact's digest and refuse a release
	// that is perfectly good.
	install.precedingSiblingChecksum = true

	stdout, stderr, err := install.run()
	if err != nil {
		t.Fatalf("install.sh refused a valid archive because another line was listed first: %v\nstdout:\n%s\nstderr:\n%s",
			err, stdout, stderr)
	}
	if _, statErr := os.Stat(filepath.Join(install.stateRoot, "bin", "wso2")); statErr != nil {
		t.Errorf("no binary was installed: %v", statErr)
	}
}

func TestInstallScriptRefusesAnArchiveWithNoPublishedChecksum(t *testing.T) {
	install := newInstallHarness(t)
	// The checksum file exists but says nothing about this archive, so there is
	// nothing to verify against. Installing anyway would defeat the check.
	install.omitChecksumLine = true

	stdout, stderr, err := install.run()
	if err == nil {
		t.Fatalf("install.sh succeeded with no checksum published for the archive\nstdout:\n%s", stdout)
	}
	if !strings.Contains(stdout+stderr, "checksums.txt") {
		t.Errorf("refusal does not say the checksum file lacked the archive:\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	if _, statErr := os.Stat(filepath.Join(install.stateRoot, "bin", "wso2")); !os.IsNotExist(statErr) {
		t.Error("a binary was installed without a published checksum to verify it")
	}
}

func TestInstallScriptExportsTheStateRootItInstalledInto(t *testing.T) {
	install := newInstallHarness(t)
	elsewhere := filepath.Join(t.TempDir(), "custom-root")
	install.stateRoot = elsewhere
	install.environment = append(install.environment, "WSO2_HOME="+elsewhere)

	if _, stderr, err := install.run(); err != nil {
		t.Fatalf("install.sh failed: %v\nstderr:\n%s", err, stderr)
	}

	// Without the export, a shell installed under a non-default root would read
	// its state from the default one: the binary and its state would disagree.
	profile := install.readProfile(t)
	if want := "export WSO2_HOME=\"" + elsewhere + "\""; !strings.Contains(profile, want) {
		t.Errorf("profile does not export the state root it installed into (%s):\n%s", want, profile)
	}
}

func TestInstallScriptReplacesABlockPointingSomewhereElse(t *testing.T) {
	install := newInstallHarness(t)
	if _, stderr, err := install.run(); err != nil {
		t.Fatalf("first install failed: %v\nstderr:\n%s", err, stderr)
	}

	// The same user installs again into a different root. Recognising only its own
	// marker and stopping there would leave the first install's directory on PATH,
	// so the shell found by name would still be the old one.
	elsewhere := filepath.Join(t.TempDir(), "second-root")
	first := filepath.Join(install.stateRoot, "bin")
	install.stateRoot = elsewhere
	install.environment = append(install.environment, "WSO2_HOME="+elsewhere)
	if _, stderr, err := install.run(); err != nil {
		t.Fatalf("second install failed: %v\nstderr:\n%s", err, stderr)
	}

	profile := install.readProfile(t)
	if blocks := strings.Count(profile, installBlockMarker); blocks != 1 {
		t.Errorf("profile carries %d install blocks, want exactly 1:\n%s", blocks, profile)
	}
	if strings.Contains(profile, first) {
		t.Errorf("profile still puts the previous install's %s on PATH:\n%s", first, profile)
	}
	if want := filepath.Join(elsewhere, "bin"); !strings.Contains(profile, want) {
		t.Errorf("profile does not put the new %s on PATH:\n%s", want, profile)
	}
	// The lines that were in the profile before any install must survive a rewrite.
	if !strings.Contains(profile, "# existing profile") {
		t.Errorf("rewriting the block dropped the user's own profile lines:\n%s", profile)
	}
}

func TestInstallScriptRefusesAnUnsupportedArchitecture(t *testing.T) {
	install := newInstallHarness(t)
	// The real detection path is exercised by answering `uname -m` with hardware
	// no release is built for, rather than by giving the script a test-only
	// override it would then carry for users.
	install.platform.unameMachine = "sparc64"

	stdout, stderr, err := install.run()
	if err == nil {
		t.Fatalf("install.sh succeeded on an unsupported architecture\nstdout:\n%s", stdout)
	}
	if !strings.Contains(stdout+stderr, "sparc64") {
		t.Errorf("refusal does not name the architecture it detected:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestInstallScriptLeavesTheProfileAloneWhenAsked(t *testing.T) {
	install := newInstallHarness(t)
	install.environment = append(install.environment, "WSO2_CLI_NO_PROFILE=1")

	stdout, stderr, err := install.run()
	if err != nil {
		t.Fatalf("install.sh failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	// The opt-out suppresses the edit, not the install.
	if _, statErr := os.Stat(filepath.Join(install.stateRoot, "bin", "wso2")); statErr != nil {
		t.Errorf("the opt-out suppressed the install itself: %v", statErr)
	}
	if profile := install.readProfile(t); strings.Contains(profile, installBlockMarker) {
		t.Errorf("the profile was edited despite the opt-out:\n%s", profile)
	}
	// Someone who opted out still has to be told what to do to reach the binary.
	if want := filepath.Join(install.stateRoot, "bin"); !strings.Contains(stdout, want) {
		t.Errorf("output does not tell the user to add %s to PATH:\n%s", want, stdout)
	}
}

func TestInstallScriptInstallsWhenTheProfileCannotBeWritten(t *testing.T) {
	install := newInstallHarness(t)

	stdout, stderr, err := install.runWithProfileMode(0o444)
	if err != nil {
		t.Fatalf("install.sh failed on a read-only profile: %v\nstdout:\n%s\nstderr:\n%s",
			err, stdout, stderr)
	}

	// The binary is installed before the profile is touched. Abandoning the run
	// at an unwritable file would throw away a working install, and report it as
	// a bare permission error rather than as something the user can act on.
	if _, statErr := os.Stat(filepath.Join(install.stateRoot, "bin", "wso2")); statErr != nil {
		t.Errorf("a read-only profile prevented the install itself: %v", statErr)
	}
	if !strings.Contains(stdout, "not writable") {
		t.Errorf("output does not explain that the profile was left alone:\n%s", stdout)
	}
	if !strings.Contains(stdout, "export PATH=") {
		t.Errorf("output does not print the lines to add by hand:\n%s", stdout)
	}
	if profile := install.readProfile(t); strings.Contains(profile, installBlockMarker) {
		t.Errorf("a read-only profile was written to anyway:\n%s", profile)
	}
}

func TestInstallScriptRefusesAStateRootThatWouldInjectIntoTheProfile(t *testing.T) {
	// A newline is legal in a path and catastrophic in a profile: everything after
	// it becomes its own line, which is arbitrary shell code in the user's
	// startup file.
	install := newInstallHarness(t)
	injected := filepath.Join(t.TempDir(), "root\nexport EVIL=1")
	install.stateRoot = injected
	install.environment = append(install.environment, "WSO2_HOME="+injected)

	stdout, stderr, err := install.run()
	if err == nil {
		t.Fatalf("install.sh accepted a state root containing a line break\nstdout:\n%s", stdout)
	}
	if !strings.Contains(stdout+stderr, "line break") {
		t.Errorf("refusal does not say why it refused:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if profile := install.readProfile(t); strings.Contains(profile, "EVIL") {
		t.Errorf("the profile was written with injected content:\n%s", profile)
	}
}

func TestInstallScriptSucceedsWhenNoProfileCanBeDetected(t *testing.T) {
	install := newInstallHarness(t)
	install.writeProfile = false

	stdout, stderr, err := install.run()
	if err != nil {
		t.Fatalf("install.sh failed with no profile present: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	if _, statErr := os.Stat(filepath.Join(install.stateRoot, "bin", "wso2")); statErr != nil {
		t.Errorf("no binary was installed: %v", statErr)
	}
	// With nowhere safe to write, the lines the user must add themselves are the
	// only way the install can be completed, so they have to be printed.
	if !strings.Contains(stdout, "export PATH=") {
		t.Errorf("output does not print the lines to add by hand:\n%s", stdout)
	}
}

func TestInstallScriptHonoursTheStateRootVariable(t *testing.T) {
	install := newInstallHarness(t)
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	install.stateRoot = elsewhere
	install.environment = append(install.environment, "WSO2_HOME="+elsewhere)

	if _, stderr, err := install.run(); err != nil {
		t.Fatalf("install.sh failed: %v\nstderr:\n%s", err, stderr)
	}

	// The shell reads WSO2_HOME to find its state. An installer that ignored it
	// would put the binary somewhere the shell it installed does not look.
	if _, statErr := os.Stat(filepath.Join(elsewhere, "bin", "wso2")); statErr != nil {
		t.Errorf("the binary was not installed under WSO2_HOME: %v", statErr)
	}
}

func (i *installHarness) run(args ...string) (string, string, error) {
	i.t.Helper()
	if i.writeProfile {
		if err := os.WriteFile(i.profilePath, []byte("# existing profile\n"), 0o644); err != nil {
			i.t.Fatalf("writing the fixture profile returned %v", err)
		}
		// Written first and then restricted, because the mode under test may not
		// permit writing the contents.
		if err := os.Chmod(i.profilePath, i.profileMode); err != nil {
			i.t.Fatalf("setting the fixture profile mode returned %v", err)
		}
	}

	script := filepath.Join(repoRoot(i.t), installScriptRelPath)
	command := exec.Command("bash", append([]string{script}, args...)...)

	path := os.Getenv("PATH")
	if i.platform.unameMachine != "" {
		path = i.shimmedUname() + string(os.PathListSeparator) + path
	}
	command.Env = append([]string{
		"HOME=" + i.home,
		"PATH=" + path,
		"SHELL=/bin/bash",
		"WSO2_CLI_RELEASE_BASE_URL=" + i.server.URL + "/releases",
		"WSO2_CLI_RELEASE_API_URL=" + i.server.URL + "/releases",
	}, i.environment...)

	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout.String(), stderr.String(), err
}

// shimmedUname returns a directory holding a uname that answers -m with the
// machine under test and defers to the real one for everything else.
func (i *installHarness) shimmedUname() string {
	i.t.Helper()
	directory := i.t.TempDir()
	shim := "#!/bin/sh\nif [ \"$1\" = \"-m\" ]; then echo " + i.platform.unameMachine +
		"; else exec /usr/bin/uname \"$@\"; fi\n"
	if err := os.WriteFile(filepath.Join(directory, "uname"), []byte(shim), 0o755); err != nil {
		i.t.Fatalf("writing the uname shim returned %v", err)
	}
	return directory
}

// readProfile reports the profile's contents. A missing file is only an
// acceptable answer when the test never wrote one: otherwise every assertion
// that the profile does not contain something would hold trivially, including in
// a run that deleted it.
func (i *installHarness) readProfile(t *testing.T) string {
	t.Helper()
	contents, err := os.ReadFile(i.profilePath)
	if os.IsNotExist(err) {
		if i.writeProfile {
			t.Fatalf("the profile at %s is gone; the run removed a file it was given", i.profilePath)
		}
		return ""
	}
	if err != nil {
		t.Fatalf("reading the profile returned %v", err)
	}
	return string(contents)
}

// runWithProfileMode runs the installer against a profile carrying the given
// permission, so the script can be presented with one it cannot write to.
func (i *installHarness) runWithProfileMode(mode os.FileMode, args ...string) (string, string, error) {
	i.t.Helper()
	previous := i.profileMode
	i.profileMode = mode
	defer func() {
		i.profileMode = previous
		// Restored so the test's own cleanup can remove the temporary directory.
		if err := os.Chmod(i.profilePath, 0o644); err != nil && !os.IsNotExist(err) {
			i.t.Errorf("restoring the fixture profile mode returned %v", err)
		}
	}()
	return i.run(args...)
}
