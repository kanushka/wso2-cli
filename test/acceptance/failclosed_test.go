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

package acceptance_test

// This file proves the shell fails closed around a module that is unsafe,
// incompatible, corrupt, or faulty, through the same external seam a user runs.
//
// Every case asserts the same three things: the shell ends in one stable
// problem code, in the exit class automation can act on, with nothing on
// standard output. The last of those is what stops a half-finished exchange
// from reaching a script as though it had succeeded.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/modules/fixture"
	"github.com/wso2/wso2-cli/internal/state"
)

// The faults the acceptance fixture module can inject. They repeat the values
// declared by test/acceptance/testdata/faultymodule, which is a main package and
// so cannot be imported. Its control file is read by content, so a value that
// drifts selects no fault and the case fails rather than passing quietly.
const (
	faultNone                = ""
	faultNamespaceMismatch   = "namespace-mismatch"
	faultVersionMismatch     = "version-mismatch"
	faultRequiredCapability  = "required-capability"
	faultRuntimeProtocol     = "runtime-protocol"
	faultUnknownMessageKind  = "unknown-message-kind"
	faultTruncatedFrame      = "truncated-frame"
	faultPartialLengthPrefix = "partial-length-prefix"
	faultMalformedFrame      = "malformed-frame"
	faultOversizedFrame      = "oversized-frame"
	faultExtraFrame          = "extra-frame"
	faultPanic               = "panic"
	faultFloodDiagnostics    = "flood-diagnostics"
)

// faultControlFile is the file, written beside the installed executable, whose
// content selects the fault.
const faultControlFile = "fault"

// diagnosticsCeiling is the most standard error the shell may write for one
// invocation. It is far above the shell's own limit and far below what the
// flooding fixture writes, so the assertion is about boundedness rather than
// about the exact limit in force.
const diagnosticsCeiling = 64 << 10

func TestTheFaultFixtureAnswersLikeTheReferenceModuleWhenNoFaultIsSelected(t *testing.T) {
	// The control case. Every assertion below reads a failure as evidence of
	// the injected fault, which only holds if this module succeeds without one.
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	installFaultyModule(t, stateRoot, faultNone)

	stdout, stderr := runShell(t, shell, stateRoot, "reference", "status")

	if !strings.Contains(stdout, "operational") {
		t.Errorf("the fault fixture did not report a status:\n%s", stdout)
	}
	if stderr != "" {
		t.Errorf("a successful command wrote diagnostics:\n%s", stderr)
	}
}

func TestAReceiptPathThatEscapesItsVersionDirectoryIsRejectedBeforeLaunch(t *testing.T) {
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	if _, err := fixture.Install(state.ModuleStore(stateRoot), fixture.Module{
		Namespace:        "reference",
		Version:          testModuleVersion,
		ShellRange:       ">=0.1.0 <1.0.0",
		ProtocolVersions: []int{testProtocolVersionNumber},
		SourcePath:       buildReferenceModule(t),
		// The executable is installed where it belongs; only the receipt
		// points elsewhere, which is the redirection the shell must refuse.
		ExecutablePathOverride: "../../../escape/wso2-module-reference",
	}); err != nil {
		t.Fatalf("fixture.Install returned %v", err)
	}

	run := tryFailingShell(t, shell, stateRoot, "reference", "status")

	run.expect(t, 69, "modules.receipt_path_escape")
	run.expectNoLaunch(t)
}

func TestASymbolicLinkThatLeavesTheVersionDirectoryIsRejectedBeforeLaunch(t *testing.T) {
	// The receipt's path stays inside the version directory, and only the file
	// system takes it out again. Containment that was proved lexically alone
	// would accept this.
	if runtime.GOOS == "windows" {
		t.Skip("the escape is a symbolic link")
	}
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	storeRoot := state.ModuleStore(stateRoot)
	receipt, err := fixture.Install(storeRoot, fixture.Module{
		Namespace:        "reference",
		Version:          testModuleVersion,
		ShellRange:       ">=0.1.0 <1.0.0",
		ProtocolVersions: []int{testProtocolVersionNumber},
		SourcePath:       buildReferenceModule(t),
	})
	if err != nil {
		t.Fatalf("fixture.Install returned %v", err)
	}

	outside := filepath.Join(t.TempDir(), "outside-the-store")
	if err := os.WriteFile(outside, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing the executable outside the store: %v", err)
	}
	store := modules.NewStore(storeRoot)
	const linkName = "escape-link"
	if err := os.Symlink(outside, filepath.Join(store.VersionDir("reference", testModuleVersion), linkName)); err != nil {
		t.Fatalf("linking out of the version directory: %v", err)
	}
	receipt.Executable = linkName
	if err := fixture.WriteReceipt(storeRoot, receipt); err != nil {
		t.Fatalf("fixture.WriteReceipt returned %v", err)
	}
	if err := fixture.Activate(storeRoot, "reference", testModuleVersion); err != nil {
		t.Fatalf("fixture.Activate returned %v", err)
	}

	run := tryFailingShell(t, shell, stateRoot, "reference", "status")

	run.expect(t, 69, "modules.receipt_path_escape")
	run.expectNoLaunch(t)
}

func TestIncompatibleReceiptMetadataIsRejectedBeforeLaunch(t *testing.T) {
	// Protocol incompatibility is proved alongside the successful status
	// command; this covers the rest of the compatibility facts a receipt
	// carries.
	cases := []struct {
		name    string
		module  fixture.Module
		problem string
	}{
		{
			name:    "shell version",
			module:  fixture.Module{ShellRange: ">=99.0.0 <100.0.0"},
			problem: "modules.incompatible_shell",
		},
		{
			name:    "operating system",
			module:  fixture.Module{OS: "plan9"},
			problem: "modules.incompatible_platform",
		},
		{
			name:    "architecture",
			module:  fixture.Module{Arch: "mips64p32"},
			problem: "modules.incompatible_platform",
		},
	}

	shell := buildShell(t)
	module := buildReferenceModule(t)
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			stateRoot := isolatedStateRoot(t)
			install := testCase.module
			install.Namespace = "reference"
			install.Version = testModuleVersion
			install.ProtocolVersions = []int{testProtocolVersionNumber}
			install.SourcePath = module
			if install.ShellRange == "" {
				install.ShellRange = ">=0.1.0 <1.0.0"
			}
			if _, err := fixture.Install(state.ModuleStore(stateRoot), install); err != nil {
				t.Fatalf("fixture.Install returned %v", err)
			}

			run := tryFailingShell(t, shell, stateRoot, "reference", "status")

			run.expect(t, 69, testCase.problem)
			run.expectNoLaunch(t)
		})
	}
}

func TestASameNamedExecutableOnPathOrInTheWorkingDirectoryIsIgnored(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the shadowing executable is a POSIX shell script")
	}
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	installReferenceModule(t, stateRoot, buildReferenceModule(t))

	// Two impostors under the module's own executable name: one first on PATH,
	// one in the working directory the shell is run from. Either would leave a
	// marker behind if the shell ever reached for it.
	marker := filepath.Join(t.TempDir(), "impostor-was-launched")
	pathDir := t.TempDir()
	workingDir := t.TempDir()
	impostor := []byte("#!/bin/sh\ntouch '" + marker + "'\nexit 0\n")
	for _, directory := range []string{pathDir, workingDir} {
		if err := os.WriteFile(filepath.Join(directory, "wso2-module-reference"), impostor, 0o755); err != nil {
			t.Fatalf("writing the shadowing executable: %v", err)
		}
	}

	stdout, stderr, err := runShadowed(shell, stateRoot, pathDir, workingDir)
	if err != nil {
		t.Fatalf("wso2 reference status failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "operational") {
		t.Errorf("the installed module did not answer:\n%s", stdout)
	}
	assertImpostorNotLaunched(t, marker)

	// With the installed executable gone the shell has nothing to launch. An
	// implementation that fell back to PATH or the working directory would
	// succeed here, which is why the missing installation is the sharper proof.
	store := modules.NewStore(state.ModuleStore(stateRoot))
	if err := os.Remove(filepath.Join(store.VersionDir("reference", testModuleVersion),
		"wso2-module-reference")); err != nil {
		t.Fatalf("removing the installed executable: %v", err)
	}

	stdout, stderr, err = runShadowed(shell, stateRoot, pathDir, workingDir)
	run := failedRun{stdout: stdout, stderr: stderr, err: err}
	run.expect(t, 69, "modules.executable_missing")
	run.expectNoLaunch(t)
	assertImpostorNotLaunched(t, marker)
}

func TestARuntimeIdentityThatContradictsTheReceiptIsRejectedBeforeInvocation(t *testing.T) {
	cases := []struct {
		name  string
		fault string
	}{
		{name: "another namespace", fault: faultNamespaceMismatch},
		{name: "another version", fault: faultVersionMismatch},
	}

	shell := buildShell(t)
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			stateRoot := isolatedStateRoot(t)
			installFaultyModule(t, stateRoot, testCase.fault)

			run := tryFailingShell(t, shell, stateRoot, "reference", "status")

			run.expect(t, 69, "rpc.identity_mismatch")
		})
	}
}

func TestAModuleThatRequiresACapabilityTheShellDoesNotProvideFailsClosed(t *testing.T) {
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	installFaultyModule(t, stateRoot, faultRequiredCapability)

	run := tryFailingShell(t, shell, stateRoot, "reference", "status")

	run.expect(t, 69, "rpc.unsupported_capability")
}

func TestAModuleThatOffersAnotherProtocolAtRuntimeIsRefused(t *testing.T) {
	// The receipt promised the protocol the shell selected. An executable that
	// offers only another one at runtime is refused rather than negotiated
	// with, so it cannot widen what its installation declared.
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	installFaultyModule(t, stateRoot, faultRuntimeProtocol)

	run := tryFailingShell(t, shell, stateRoot, "reference", "status")

	run.expect(t, 69, "rpc.protocol_mismatch")
}

func TestAnUnknownEnvelopeMessageKindFailsClosed(t *testing.T) {
	// A message kind from a later protocol release decodes without error and
	// still leaves the shell with nothing it can act on. Ignoring it would let
	// a module's terminal message go unnoticed.
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	installFaultyModule(t, stateRoot, faultUnknownMessageKind)

	run := tryFailingShell(t, shell, stateRoot, "reference", "status")

	run.expect(t, 70, "rpc.unexpected_message")
}

func TestDamagedFramesBecomeStableProtocolProblems(t *testing.T) {
	cases := []struct {
		name    string
		fault   string
		problem string
	}{
		{name: "truncated payload", fault: faultTruncatedFrame, problem: "rpc.truncated_message"},
		{name: "partial length prefix", fault: faultPartialLengthPrefix, problem: "rpc.truncated_message"},
		{name: "malformed payload", fault: faultMalformedFrame, problem: "rpc.malformed_message"},
		{name: "oversized frame", fault: faultOversizedFrame, problem: "rpc.oversized_message"},
	}

	shell := buildShell(t)
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			stateRoot := isolatedStateRoot(t)
			installFaultyModule(t, stateRoot, testCase.fault)

			run := tryFailingShell(t, shell, stateRoot, "reference", "status")

			run.expect(t, 70, testCase.problem)
		})
	}
}

func TestAModuleThatPanicsFailsWithAStableProblemWithoutCrashingTheShell(t *testing.T) {
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	installFaultyModule(t, stateRoot, faultPanic)

	run := tryFailingShell(t, shell, stateRoot, "reference", "status")

	run.expect(t, 70, "rpc.no_terminal_message")
	// The shell reports the module's own crash text rather than crashing with
	// it: the exit status above is the shell's, not a propagated panic.
	if !strings.Contains(run.stderr, "panicked") {
		t.Errorf("stderr does not carry the module's panic:\n%s", run.stderr)
	}
}

func TestAModuleThatKeepsWritingAfterItsResultProducesNoOutput(t *testing.T) {
	// The result itself was well formed, so this is the sharpest case for the
	// rule that no failure path prints a partial exchange as a success.
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	installFaultyModule(t, stateRoot, faultExtraFrame)

	run := tryFailingShell(t, shell, stateRoot, "reference", "status")

	run.expect(t, 70, "rpc.extra_message")
	if strings.Contains(run.stdout, "operational") {
		t.Errorf("a refused exchange still reported its result:\n%s", run.stdout)
	}
}

func TestModuleDiagnosticsAreBoundedAndCannotContaminateJSONOutput(t *testing.T) {
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	installFaultyModule(t, stateRoot, faultFloodDiagnostics)

	stdout, stderr := runShell(t, shell, stateRoot, "reference", "status", "--output", "json")

	decoded := decodeStatusJSON(t, stdout)
	if decoded["status"] != "operational" {
		t.Errorf("the JSON result reports status %q, want %q", decoded["status"], "operational")
	}
	if strings.Contains(stdout, "dddd") {
		t.Errorf("module diagnostics reached standard output:\n%s", stdout)
	}
	if length := len(stderr); length > diagnosticsCeiling {
		t.Errorf("the shell wrote %d bytes of diagnostics, want at most %d", length, diagnosticsCeiling)
	}
	if !strings.Contains(stderr, "discarded") {
		t.Errorf("stderr does not report that diagnostics were discarded:\n%s", stderr)
	}
}

// failedRun is one shell run that was expected to fail.
type failedRun struct {
	stdout string
	stderr string
	err    error
}

// tryFailingShell runs the shell and returns the run for assertion.
func tryFailingShell(t *testing.T, shell, stateRoot string, args ...string) failedRun {
	t.Helper()
	stdout, stderr, err := tryShell(shell, stateRoot, args...)
	return failedRun{stdout: stdout, stderr: stderr, err: err}
}

// expect asserts the run ended in one named problem and exit class, with
// nothing on standard output.
func (r failedRun) expect(t *testing.T, wantExit int, wantProblem string) {
	t.Helper()
	if got := exitCode(t, r.err); got != wantExit {
		t.Fatalf("exit status = %d, want %d\nstderr:\n%s", got, wantExit, r.stderr)
	}
	if !strings.Contains(r.stderr, wantProblem) {
		t.Errorf("stderr does not report %s:\n%s", wantProblem, r.stderr)
	}
	if r.stdout != "" {
		t.Errorf("a failed command still wrote to standard output:\n%s", r.stdout)
	}
}

// expectNoLaunch asserts the module was refused before it ran.
//
// Every failure reachable after launch is reported with an "rpc." code, so
// their absence is evidence the shell never started the executable.
func (r failedRun) expectNoLaunch(t *testing.T) {
	t.Helper()
	if strings.Contains(r.stderr, "rpc.") {
		t.Errorf("the shell launched a module it should have refused:\n%s", r.stderr)
	}
}

// installFaultyModule installs the fault-injecting fixture under the reference
// module's namespace and version, and selects one fault.
func installFaultyModule(t *testing.T, stateRoot, fault string) {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "faultymodule"+executableSuffix())
	build(t, repoRoot(t), binary,
		"-X main.moduleVersion="+testModuleVersion+
			" -X github.com/wso2/wso2-cli/sdk/protocol.Version="+testProtocolVersion,
		"./test/acceptance/testdata/faultymodule")
	installReferenceModule(t, stateRoot, binary)
	writeControlFile(t, stateRoot, faultControlFile, fault)
}

// runShadowed runs a status command with an impostor first on PATH and another
// in the working directory.
func runShadowed(shell, stateRoot, pathDir, workingDir string) (string, string, error) {
	command := exec.Command(shell, "reference", "status")
	command.Dir = workingDir
	command.Env = withPathPrefix(shellEnvironment(stateRoot), pathDir)
	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout.String(), stderr.String(), err
}

// withPathPrefix puts a directory first on the environment's PATH.
func withPathPrefix(environment []string, directory string) []string {
	prefixed := make([]string, 0, len(environment)+1)
	replaced := false
	for _, entry := range environment {
		if value, found := strings.CutPrefix(entry, "PATH="); found {
			entry = "PATH=" + directory + string(os.PathListSeparator) + value
			replaced = true
		}
		prefixed = append(prefixed, entry)
	}
	if !replaced {
		prefixed = append(prefixed, "PATH="+directory)
	}
	return prefixed
}

func assertImpostorNotLaunched(t *testing.T, marker string) {
	t.Helper()
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the shell launched a same-named executable outside its module store: marker %s (stat error %v)",
			marker, err)
	}
}

// processIsRunning reports whether a process id still names a live process.
func processIsRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal zero performs the permission and existence checks without
	// delivering anything.
	return process.Signal(syscall.Signal(0)) == nil
}
