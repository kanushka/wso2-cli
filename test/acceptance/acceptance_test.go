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

// Package acceptance_test runs the built shell and the built reference module
// from an isolated state directory, through the same external seam a user does.
//
// This increment covers build boundaries and receipt-backed inventory only.
// Product-command invocation arrives with the module contract.
package acceptance_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/modules/fixture"
	"github.com/wso2/wso2-cli/internal/state"
)

// Versions injected into the test builds. They differ from each other and from
// every default so the assertions cannot pass by coincidence.
const (
	testShellVersion = "0.4.2"
	// The protocol version differs from the shell and SDK defaults of "1", so
	// an ineffective ldflag cannot pass by coincidence.
	testProtocolVersion = "3"
	testModuleVersion   = "9.8.7"
	testSDKVersion      = "2.3.4"
)

// testProtocolVersionNumber is testProtocolVersion as the receipt records it.
const testProtocolVersionNumber = 3

func TestVersionReportsIndependentlyInjectedShellAndModuleVersions(t *testing.T) {
	shell := buildShell(t)
	module := buildReferenceModule(t)
	stateRoot := isolatedStateRoot(t)
	installReferenceModule(t, stateRoot, module)

	stdout, stderr := runShell(t, shell, stateRoot, "version")

	for _, want := range []string{
		"v" + testShellVersion,
		"v" + testProtocolVersion,
		runtime.GOOS + "/" + runtime.GOARCH,
		"reference",
		"v" + testModuleVersion,
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("wso2 version stdout does not contain %q:\n%s", want, stdout)
		}
	}
	if stderr != "" {
		t.Errorf("wso2 version wrote diagnostics:\n%s", stderr)
	}

	// The module reports its own version and the SDK it was built against,
	// neither of which is the shell version.
	descriptor := referenceModuleIdentity(t, module)
	if descriptor.Version != testModuleVersion {
		t.Errorf("module version = %q, want %q", descriptor.Version, testModuleVersion)
	}
	if descriptor.SDKVersion != testSDKVersion {
		t.Errorf("module SDK version = %q, want %q", descriptor.SDKVersion, testSDKVersion)
	}
	if !reflect.DeepEqual(descriptor.ProtocolVersions, []int{testProtocolVersionNumber}) {
		t.Errorf("module protocol versions = %v, want [%d]", descriptor.ProtocolVersions, testProtocolVersionNumber)
	}
	if descriptor.Version == testShellVersion || descriptor.SDKVersion == testShellVersion {
		t.Error("module and shell versions are coupled; they must vary independently")
	}
}

func TestVersionDoesNotLaunchTheInstalledModule(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the launch canary is a POSIX shell script")
	}

	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)

	// The installed executable is a canary: running it would create a marker
	// file. Version reporting must read the receipt only.
	marker := filepath.Join(t.TempDir(), "module-was-launched")
	canary := "#!/bin/sh\ntouch " + marker + "\n"
	if _, err := fixture.Install(state.ModuleStore(stateRoot), fixture.Module{
		Namespace:        "reference",
		Version:          testModuleVersion,
		ShellRange:       ">=0.1.0 <1.0.0",
		ProtocolVersions: []int{testProtocolVersionNumber},
		Contents:         []byte(canary),
	}); err != nil {
		t.Fatalf("fixture.Install returned %v", err)
	}

	stdout, stderr := runShell(t, shell, stateRoot, "version")

	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("wso2 version launched the installed module: marker %s exists (stat error %v)", marker, err)
	}
	if !strings.Contains(stdout, "v"+testModuleVersion) {
		t.Fatalf("wso2 version did not report the module from its receipt:\n%s", stdout)
	}
	if stderr != "" {
		t.Errorf("wso2 version wrote diagnostics:\n%s", stderr)
	}
}

func TestACopiedAndModifiedExecutableIsRejectedBeforeLaunch(t *testing.T) {
	shell := buildShell(t)
	module := buildReferenceModule(t)
	stateRoot := isolatedStateRoot(t)
	installReferenceModule(t, stateRoot, module)

	modified, err := os.ReadFile(module)
	if err != nil {
		t.Fatalf("cannot read the built module: %v", err)
	}
	if err := fixture.TamperExecutable(state.ModuleStore(stateRoot), "reference", testModuleVersion,
		"wso2-module-reference", append(modified, 0x00)); err != nil {
		t.Fatalf("TamperExecutable returned %v", err)
	}

	command := exec.Command(shell, "reference", "status")
	command.Env = shellEnvironment(stateRoot)
	output, err := command.CombinedOutput()

	var exitError *exec.ExitError
	if err == nil {
		t.Fatalf("the shell accepted a modified executable:\n%s", output)
	}
	if !errors.As(err, &exitError) || exitError.ExitCode() != 69 {
		t.Fatalf("exit status = %v, want the module trust class 69\n%s", err, output)
	}
	if !strings.Contains(string(output), "modules.executable_digest_mismatch") {
		t.Fatalf("output does not report the integrity failure:\n%s", output)
	}
	// The module writes this diagnostic whenever it starts, so its absence is
	// evidence the shell rejected the executable before launching it.
	if strings.Contains(string(output), "the module contract is not implemented yet") {
		t.Fatalf("the shell launched the modified module:\n%s", output)
	}
}

func TestVersionRunsWithoutNetworkAccessInTheShellDependencyGraph(t *testing.T) {
	// "Works offline" is a property of the code, not of the test machine: the
	// shell binary must not carry an HTTP client at all in this slice.
	root := repoRoot(t)
	command := exec.Command("go", "list", "-deps", "./cmd/wso2")
	command.Dir = root
	command.Env = os.Environ()
	output, err := command.Output()
	if err != nil {
		t.Fatalf("go list -deps failed: %v", err)
	}

	for _, dependency := range strings.Fields(string(output)) {
		if dependency == "net/http" {
			t.Fatal("the shell depends on net/http; wso2 version must resolve inventory from local state only")
		}
	}
}

func TestAnUnknownCommandFailsWithTheUsageExitClass(t *testing.T) {
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)

	command := exec.Command(shell, "nonexistent")
	command.Env = shellEnvironment(stateRoot)
	output, err := command.CombinedOutput()

	var exitError *exec.ExitError
	if err == nil {
		t.Fatalf("an unknown command succeeded:\n%s", output)
	}
	if !errors.As(err, &exitError) || exitError.ExitCode() != 64 {
		t.Fatalf("unknown command exit status = %v, want 64\n%s", err, output)
	}
	if !strings.Contains(string(output), "shell.unknown_command") {
		t.Fatalf("output does not name the usage problem:\n%s", output)
	}
}

func buildShell(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "wso2"+executableSuffix())
	ldflags := strings.Join([]string{
		"-X github.com/wso2/wso2-cli/internal/version.shellVersion=" + testShellVersion,
		"-X github.com/wso2/wso2-cli/internal/version.protocolVersion=" + testProtocolVersion,
	}, " ")
	build(t, repoRoot(t), binary, ldflags, "./cmd/wso2")
	return binary
}

func buildReferenceModule(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "wso2-module-reference"+executableSuffix())
	ldflags := strings.Join([]string{
		"-X main.moduleVersion=" + testModuleVersion,
		"-X github.com/wso2/wso2-cli/sdk/module.SDKVersion=" + testSDKVersion,
		"-X github.com/wso2/wso2-cli/sdk/protocol.Version=" + testProtocolVersion,
	}, " ")
	build(t, filepath.Join(repoRoot(t), "examples", "reference-module"), binary, ldflags,
		"./cmd/wso2-module-reference")
	return binary
}

func build(t *testing.T, directory, output, ldflags, packagePath string) {
	t.Helper()
	command := exec.Command("go", "build", "-ldflags", ldflags, "-o", output, packagePath)
	command.Dir = directory
	command.Env = os.Environ()
	if combined, err := command.CombinedOutput(); err != nil {
		t.Fatalf("building %s failed: %v\n%s", packagePath, err, combined)
	}
}

// isolatedStateRoot returns a temporary shell state root. No test ever reads or
// writes the developer's real WSO2 state.
func isolatedStateRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "state")
}

func installReferenceModule(t *testing.T, stateRoot, modulePath string) {
	t.Helper()
	if _, err := fixture.Install(state.ModuleStore(stateRoot), fixture.Module{
		Namespace:        "reference",
		Version:          testModuleVersion,
		ShellRange:       ">=0.1.0 <1.0.0",
		ProtocolVersions: []int{testProtocolVersionNumber},
		SourcePath:       modulePath,
		AuthAudiences:    []string{"reference-status"},
		AuthScopes:       []string{"reference:status:read"},
	}); err != nil {
		t.Fatalf("fixture.Install returned %v", err)
	}
}

func runShell(t *testing.T, shell, stateRoot string, args ...string) (string, string) {
	t.Helper()
	command := exec.Command(shell, args...)
	command.Env = shellEnvironment(stateRoot)
	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("wso2 %s failed: %v\nstdout:\n%s\nstderr:\n%s",
			strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String()
}

// shellEnvironment builds a minimal environment pointing the shell at the
// isolated state root, so the run cannot depend on developer configuration.
func shellEnvironment(stateRoot string) []string {
	environment := []string{state.RootEnvVar + "=" + stateRoot}
	for _, name := range []string{"PATH", "SystemRoot", "TMP", "TEMP", "HOME", "USERPROFILE"} {
		if value, present := os.LookupEnv(name); present {
			environment = append(environment, name+"="+value)
		}
	}
	return environment
}

type moduleIdentity struct {
	Namespace        string `json:"namespace"`
	Version          string `json:"version"`
	SDKVersion       string `json:"sdkVersion"`
	ProtocolVersions []int  `json:"protocolVersions"`
}

func referenceModuleIdentity(t *testing.T, modulePath string) moduleIdentity {
	t.Helper()
	command := exec.Command(modulePath, "--module-info")
	var identityJSON strings.Builder
	command.Stderr = &identityJSON
	if err := command.Run(); err != nil {
		t.Fatalf("reading the module identity failed: %v", err)
	}
	output := []byte(identityJSON.String())
	var identity moduleIdentity
	if err := json.Unmarshal(output, &identity); err != nil {
		t.Fatalf("the module identity is not valid JSON: %v\n%s", err, output)
	}
	return identity
}

func repoRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot determine the working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.work")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("cannot locate the repository root: no go.work found in any parent directory")
		}
		directory = parent
	}
}

func executableSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
