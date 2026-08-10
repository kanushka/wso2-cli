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

// The Windows installer is driven the same way the Unix one is, against the same
// fixture release, so both are proven against one published contract rather than
// two descriptions of it.
//
// What differs is the environment it touches. There is no shell profile to edit:
// PATH and the state root are per-user environment variables, which is why these
// runs assert on what the script wrote to the user's environment rather than on
// what it wrote to a file. Those writes are real and outlive the process, so each
// run is given its own registry-backed user environment to write into and the
// values are put back afterwards.
package acceptance_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const installScriptRelPath = "scripts/install.ps1"

// platformFields is what only the Windows runs need.
type platformFields struct {
	// processorArchitecture is what PROCESSOR_ARCHITECTURE reports, for reaching
	// the unsupported-hardware path without unsupported hardware.
	processorArchitecture string
	// savedUserEnvironment holds the per-user environment variables as they were
	// before the first run, so what the script really wrote can be put back. These
	// writes outlive the process, unlike everything else a run here touches.
	savedUserEnvironment map[string]string
}

func TestInstallScriptInstallsTheShellAndWiresThePath(t *testing.T) {
	install := newInstallHarness(t)
	defer install.restoreUserEnvironment(t)

	stdout, stderr, err := install.run()
	if err != nil {
		t.Fatalf("install.ps1 failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	if _, statErr := os.Stat(install.installedBinary()); statErr != nil {
		t.Fatalf("expected an installed binary at %s: %v", install.installedBinary(), statErr)
	}
	// Running it is the point: an unextractable or truncated binary of the right
	// name would satisfy an existence check and nothing a user cares about.
	if reported := install.reportedVersion(t); reported != fixtureStableTag {
		t.Errorf("installed binary reported %s, want %s", reported, fixtureStableTag)
	}

	binDir := filepath.Join(install.stateRoot, "bin")
	userPath := install.userEnvironment(t, "Path")
	if !strings.Contains(strings.ToLower(userPath), strings.ToLower(binDir)) {
		t.Errorf("user PATH does not contain %s:\n%s", binDir, userPath)
	}
	if got := install.userEnvironment(t, "WSO2_HOME"); got != install.stateRoot {
		t.Errorf("user WSO2_HOME is %q, want %q", got, install.stateRoot)
	}
	if !strings.Contains(stdout, binDir) {
		t.Errorf("output does not name the directory it added to PATH (%s):\n%s", binDir, stdout)
	}
}

func TestInstallScriptDoesNotDuplicateThePathEntry(t *testing.T) {
	install := newInstallHarness(t)
	defer install.restoreUserEnvironment(t)

	if _, stderr, err := install.run(); err != nil {
		t.Fatalf("first install failed: %v\nstderr:\n%s", err, stderr)
	}
	if _, stderr, err := install.run(); err != nil {
		t.Fatalf("second install failed: %v\nstderr:\n%s", err, stderr)
	}

	binDir := strings.ToLower(filepath.Join(install.stateRoot, "bin"))
	entries := 0
	for _, entry := range strings.Split(install.userEnvironment(t, "Path"), ";") {
		if strings.TrimRight(strings.ToLower(strings.TrimSpace(entry)), `\`) ==
			strings.TrimRight(binDir, `\`) {
			entries++
		}
	}
	if entries != 1 {
		t.Errorf("user PATH carries %d entries for %s, want exactly 1", entries, binDir)
	}
}

func TestInstallScriptRefusesAnArchiveThatFailsItsChecksum(t *testing.T) {
	install := newInstallHarness(t)
	defer install.restoreUserEnvironment(t)
	install.corruptArchive = true

	stdout, stderr, err := install.run()
	if err == nil {
		t.Fatalf("install.ps1 succeeded on a corrupted archive\nstdout:\n%s", stdout)
	}
	if !strings.Contains(strings.ToLower(stdout+stderr), "checksum") {
		t.Errorf("refusal does not mention the checksum:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	// Nothing may survive a failed verification, on any platform.
	if _, statErr := os.Stat(install.installedBinary()); !os.IsNotExist(statErr) {
		t.Error("a binary was installed from an archive that failed verification")
	}
}

func TestInstallScriptRefusesAnArchiveWithNoPublishedChecksum(t *testing.T) {
	install := newInstallHarness(t)
	defer install.restoreUserEnvironment(t)
	install.omitChecksumLine = true

	stdout, stderr, err := install.run()
	if err == nil {
		t.Fatalf("install.ps1 succeeded with no checksum published\nstdout:\n%s", stdout)
	}
	if !strings.Contains(stdout+stderr, "checksums.txt") {
		t.Errorf("refusal does not say the checksum file lacked the archive:\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
}

func TestInstallScriptReadsTheChecksumLineForTheArchiveItDownloaded(t *testing.T) {
	install := newInstallHarness(t)
	defer install.restoreUserEnvironment(t)
	install.precedingSiblingChecksum = true

	stdout, stderr, err := install.run()
	if err != nil {
		t.Fatalf("install.ps1 refused a valid archive because another line was listed first: %v\nstdout:\n%s\nstderr:\n%s",
			err, stdout, stderr)
	}
	if _, statErr := os.Stat(install.installedBinary()); statErr != nil {
		t.Errorf("no binary was installed: %v", statErr)
	}
}

func TestInstallScriptInstallsAPinnedVersion(t *testing.T) {
	install := newInstallHarness(t)
	defer install.restoreUserEnvironment(t)

	if _, stderr, err := install.run(fixtureOlderTag); err != nil {
		t.Fatalf("install.ps1 failed for a pinned version: %v\nstderr:\n%s", err, stderr)
	}

	if reported := install.reportedVersion(t); reported != fixtureOlderTag {
		t.Errorf("installed binary reported %s, want the pinned %s", reported, fixtureOlderTag)
	}
}

func TestInstallScriptResolvesAPrereleaseOnlyWhenAsked(t *testing.T) {
	t.Run("the default resolves the newest stable release", func(t *testing.T) {
		install := newInstallHarness(t)
		defer install.restoreUserEnvironment(t)

		if _, stderr, err := install.run(); err != nil {
			t.Fatalf("install.ps1 failed: %v\nstderr:\n%s", err, stderr)
		}
		if reported := install.reportedVersion(t); reported != fixtureStableTag {
			t.Errorf("the default install resolved %s, want the newest stable %s",
				reported, fixtureStableTag)
		}
	})

	t.Run("the opt-in resolves the newest prerelease", func(t *testing.T) {
		install := newInstallHarness(t)
		defer install.restoreUserEnvironment(t)
		install.environment = append(install.environment, "WSO2_CLI_PRERELEASE=true")

		if _, stderr, err := install.run(); err != nil {
			t.Fatalf("install.ps1 failed: %v\nstderr:\n%s", err, stderr)
		}
		if reported := install.reportedVersion(t); reported != fixturePreleaseTag {
			t.Errorf("the prerelease opt-in installed %s, want %s", reported, fixturePreleaseTag)
		}
	})
}

func TestInstallScriptRefusesAnUnsupportedArchitecture(t *testing.T) {
	install := newInstallHarness(t)
	defer install.restoreUserEnvironment(t)
	// The real detection path is exercised by answering with hardware no release
	// is built for, rather than by giving the script a test-only override.
	install.platform.processorArchitecture = "IA64"

	stdout, stderr, err := install.run()
	if err == nil {
		t.Fatalf("install.ps1 succeeded on an unsupported architecture\nstdout:\n%s", stdout)
	}
	if !strings.Contains(stdout+stderr, "IA64") {
		t.Errorf("refusal does not name the architecture it detected:\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
}

func TestInstallScriptLeavesTheEnvironmentAloneWhenAsked(t *testing.T) {
	install := newInstallHarness(t)
	defer install.restoreUserEnvironment(t)
	before := install.userEnvironment(t, "Path")
	install.environment = append(install.environment, "WSO2_CLI_NO_PROFILE=1")

	stdout, stderr, err := install.run()
	if err != nil {
		t.Fatalf("install.ps1 failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	// The opt-out suppresses the environment change, not the install.
	if _, statErr := os.Stat(install.installedBinary()); statErr != nil {
		t.Errorf("the opt-out suppressed the install itself: %v", statErr)
	}
	if after := install.userEnvironment(t, "Path"); after != before {
		t.Errorf("user PATH changed despite the opt-out:\nbefore: %s\nafter:  %s", before, after)
	}
	if binDir := filepath.Join(install.stateRoot, "bin"); !strings.Contains(stdout, binDir) {
		t.Errorf("output does not tell the user how to reach %s:\n%s", binDir, stdout)
	}
}

func TestInstallScriptHonoursTheStateRootVariable(t *testing.T) {
	install := newInstallHarness(t)
	defer install.restoreUserEnvironment(t)
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	install.stateRoot = elsewhere
	install.environment = append(install.environment, "WSO2_HOME="+elsewhere)

	if _, stderr, err := install.run(); err != nil {
		t.Fatalf("install.ps1 failed: %v\nstderr:\n%s", err, stderr)
	}

	if _, statErr := os.Stat(install.installedBinary()); statErr != nil {
		t.Errorf("the binary was not installed under WSO2_HOME: %v", statErr)
	}
}

// run invokes the real script the way a user does, with everything it could
// reach outside the test redirected: the home directory, the state root, and the
// release origin.
func (i *installHarness) run(args ...string) (string, string, error) {
	i.t.Helper()

	// Captured before the first run rather than in the constructor, so a test that
	// reads these values first still records what was there originally.
	if i.platform.savedUserEnvironment == nil {
		i.platform.savedUserEnvironment = map[string]string{
			"Path":      i.userEnvironment(i.t, "Path"),
			"WSO2_HOME": i.userEnvironment(i.t, "WSO2_HOME"),
		}
	}

	script := filepath.Join(repoRoot(i.t), installScriptRelPath)
	arguments := []string{
		"-NoLogo", "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-File", script,
	}
	command := exec.Command(powerShell(), append(arguments, args...)...)

	// Built from scratch rather than inherited, so an ambient WSO2_ variable
	// cannot reach the run. The Windows API needs SystemRoot and a temp directory
	// to function at all, and PowerShell itself is found on PATH.
	environment := []string{
		"USERPROFILE=" + i.home,
		"HOME=" + i.home,
		"WSO2_CLI_RELEASE_BASE_URL=" + i.server.URL + "/releases",
		"WSO2_CLI_RELEASE_API_URL=" + i.server.URL + "/releases",
	}
	for _, name := range []string{"PATH", "SystemRoot", "windir", "TMP", "TEMP", "ProgramFiles",
		"ProgramData", "LOCALAPPDATA", "APPDATA", "COMSPEC", "PATHEXT", "PSModulePath"} {
		if value, present := os.LookupEnv(name); present {
			environment = append(environment, name+"="+value)
		}
	}
	if i.platform.processorArchitecture != "" {
		// Both are set to the same value. The script reads PROCESSOR_ARCHITEW6432
		// first, so leaving whatever the runner has there would mask the
		// architecture under test — and a runner that has it set is exactly the
		// case an empty override would not cover.
		environment = append(environment,
			"PROCESSOR_ARCHITECTURE="+i.platform.processorArchitecture,
			"PROCESSOR_ARCHITEW6432="+i.platform.processorArchitecture)
	} else {
		for _, name := range []string{"PROCESSOR_ARCHITECTURE", "PROCESSOR_ARCHITEW6432"} {
			if value, present := os.LookupEnv(name); present {
				environment = append(environment, name+"="+value)
			}
		}
	}
	command.Env = append(environment, i.environment...)

	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout.String(), stderr.String(), err
}

// powerShell reports the interpreter to drive the script with, preferring
// PowerShell 7 where the runner has it and falling back to the Windows
// PowerShell every Windows machine ships. The script supports both, so whichever
// is present is the one worth proving against.
func powerShell() string {
	if path, err := exec.LookPath("pwsh"); err == nil {
		return path
	}
	return "powershell"
}

// userEnvironment reads a per-user environment variable as the script writes it:
// out of the user's own environment, not out of this process.
func (i *installHarness) userEnvironment(t *testing.T, name string) string {
	t.Helper()
	command := exec.Command(powerShell(), "-NoLogo", "-NoProfile", "-NonInteractive", "-Command",
		"[Environment]::GetEnvironmentVariable('"+name+"', 'User')")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("reading the user environment variable %s returned %v\n%s", name, err, output)
	}
	return strings.TrimSpace(string(output))
}

// restoreUserEnvironment puts back what the run changed. These writes are real
// and persist beyond the process, so a test that did not undo them would leave
// the machine it ran on carrying a temporary directory on its PATH.
func (i *installHarness) restoreUserEnvironment(t *testing.T) {
	t.Helper()
	if i.platform.savedUserEnvironment == nil {
		return
	}
	for name, value := range i.platform.savedUserEnvironment {
		setting := "$null"
		if value != "" {
			setting = "'" + strings.ReplaceAll(value, "'", "''") + "'"
		}
		command := exec.Command(powerShell(), "-NoLogo", "-NoProfile", "-NonInteractive", "-Command",
			"[Environment]::SetEnvironmentVariable('"+name+"', "+setting+", 'User')")
		if output, err := command.CombinedOutput(); err != nil {
			t.Errorf("restoring the user environment variable %s returned %v\n%s", name, err, output)
		}
	}
}
