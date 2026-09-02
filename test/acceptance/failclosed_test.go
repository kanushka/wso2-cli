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
//
// "Rejected before launch" and "rejected before invocation" are claims about
// what did not happen, so they are not left to the shell's own account of the
// failure. The installed executable records how far it got, beside itself, and
// the assertions read that.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/modules/fixture"
	"github.com/wso2/wso2-cli/internal/state"
	"github.com/wso2/wso2-cli/internal/statusservice"
)

// The exit classes the shell returns, named at every call site so a failed
// assertion says which contract broke.
const (
	exitModuleTrust   = 69
	exitModuleProcess = 70
)

// The faults the acceptance fixture module can inject, and the markers it and
// the launch canary leave behind. They repeat the values declared by
// test/acceptance/testdata/faultymodule and .../launchcanary, which are main
// packages and so cannot be imported. Every fault is selected by content and
// every marker asserted by name, so a value that drifts fails its test rather
// than passing quietly.
const (
	faultNone                = ""
	faultNamespaceMismatch   = "namespace-mismatch"
	faultVersionMismatch     = "version-mismatch"
	faultRequiredCapability  = "required-capability"
	faultRuntimeProtocol     = "runtime-protocol"
	faultUnknownMessageKind  = "unknown-message-kind"
	faultUnknownField        = "unknown-field"
	faultTruncatedFrame      = "truncated-frame"
	faultPartialLengthPrefix = "partial-length-prefix"
	faultMalformedFrame      = "malformed-frame"
	faultOversizedFrame      = "oversized-frame"
	faultExtraFrame          = "extra-frame"
	faultPanic               = "panic"
	faultFloodDiagnostics    = "flood-diagnostics"
	faultWaitForInputClose   = "wait-for-input-close"
)

const (
	// faultControlFile is the file, written beside the installed executable,
	// whose content selects the fault.
	faultControlFile = "fault"
	// invokedMarker records that a command invocation reached the module.
	invokedMarker = "invocation-arrived"
	// inputClosedMarker records that the module exited because the shell closed
	// its protocol input rather than because it was killed.
	inputClosedMarker = "input-was-closed"
	// canaryMarker records that the shell launched the installed executable.
	canaryMarker = "canary-was-launched"
)

// diagnosticsCeiling is the most standard error the shell may write for one
// invocation. It is far above the shell's own limit and far below what the
// flooding fixture writes, so the assertion is about boundedness rather than
// about the exact limit in force.
const diagnosticsCeiling = 64 << 10

func TestTheFaultFixtureAnswersLikeTheReferenceModuleWhenNoFaultIsSelected(t *testing.T) {
	// The control case. Every assertion below reads a failure as evidence of
	// the injected fault, which only holds if this module succeeds without one,
	// and reads a missing marker as evidence the exchange stopped early, which
	// only holds if a completed exchange leaves one.
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	installFaultyModule(t, stateRoot, faultNone)

	stdout, stderr := runShell(t, shell, stateRoot, "reference", "call")

	if !strings.Contains(stdout, "operational") {
		t.Errorf("the fault fixture did not report a status:\n%s", stdout)
	}
	if stderr != "" {
		t.Errorf("a successful command wrote diagnostics:\n%s", stderr)
	}
	assertMarkerPresent(t, markerPath(stateRoot, invokedMarker),
		"the fixture does not record the invocations it receives")
}

func TestTheLaunchCanaryRecordsAModuleTheShellDoesLaunch(t *testing.T) {
	// The control case for every "before launch" assertion below: an absent
	// canary marker is only evidence when a launched canary leaves one.
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	installLaunchCanary(t, stateRoot, fixture.Module{})

	run := tryFailingShell(t, shell, stateRoot, "reference", "call")

	// The canary says nothing on the wire, so a shell that launched it ends
	// without a result.
	run.expect(t, exitModuleProcess, "rpc.no_terminal_message")
	assertMarkerPresent(t, markerPath(stateRoot, canaryMarker),
		"the canary does not record being launched")
}

func TestAReceiptPathThatEscapesItsVersionDirectoryIsRejectedBeforeLaunch(t *testing.T) {
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	// The executable is installed where it belongs; only the receipt points
	// elsewhere, which is the redirection the shell must refuse.
	installLaunchCanary(t, stateRoot, fixture.Module{
		ExecutablePathOverride: "../../../escape/wso2-module-reference",
	})

	run := tryFailingShell(t, shell, stateRoot, "reference", "call")

	run.expect(t, exitModuleTrust, "modules.receipt_path_escape")
	run.expectNoLaunch(t, stateRoot)
}

func TestASymbolicLinkThatLeavesTheVersionDirectoryIsRejectedBeforeLaunch(t *testing.T) {
	// The receipt's path stays inside the version directory, and only the file
	// system takes it out again. Containment proved lexically alone would
	// accept this.
	if runtime.GOOS == "windows" {
		t.Skip("the escape is a symbolic link")
	}
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	receipt := installLaunchCanary(t, stateRoot, fixture.Module{})

	outside := filepath.Join(t.TempDir(), "outside-the-store")
	if err := os.WriteFile(outside, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing the executable outside the store: %v", err)
	}
	const linkName = "escape-link"
	if err := os.Symlink(outside, markerPath(stateRoot, linkName)); err != nil {
		t.Fatalf("linking out of the version directory: %v", err)
	}
	receipt.Executable = linkName
	storeRoot := state.ModuleStore(stateRoot)
	if err := fixture.WriteReceipt(storeRoot, receipt); err != nil {
		t.Fatalf("fixture.WriteReceipt returned %v", err)
	}
	if err := fixture.Activate(storeRoot, "reference", testModuleVersion); err != nil {
		t.Fatalf("fixture.Activate returned %v", err)
	}

	run := tryFailingShell(t, shell, stateRoot, "reference", "call")

	run.expect(t, exitModuleTrust, "modules.receipt_path_escape")
	run.expectNoLaunch(t, stateRoot)
}

func TestIncompatibleReceiptMetadataIsRejectedBeforeLaunch(t *testing.T) {
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
			name:    "protocol version",
			module:  fixture.Module{ProtocolVersions: []int{testProtocolVersionNumber + 1}},
			problem: "modules.incompatible_protocol",
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
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			stateRoot := isolatedStateRoot(t)
			installLaunchCanary(t, stateRoot, testCase.module)

			run := tryFailingShell(t, shell, stateRoot, "reference", "call")

			run.expect(t, exitModuleTrust, testCase.problem)
			run.expectNoLaunch(t, stateRoot)
		})
	}
}

func TestASameNamedExecutableOnPathOrInTheWorkingDirectoryIsIgnored(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the shadowing executable is a POSIX shell script")
	}
	shell := buildShell(t)
	// The installed module has to answer for this test to prove anything, and
	// answering now means brokered access to the local status service.
	stateRoot := deploy(t, statusservice.Options{}).stateRoot

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
	assertMarkerAbsent(t, marker, "the shell launched a same-named executable outside its module store")

	// With the installed executable gone the shell has nothing to launch. An
	// implementation that fell back to PATH or the working directory would
	// succeed here, which is why the missing installation is the sharper proof.
	if err := os.Remove(markerPath(stateRoot, "wso2-module-reference")); err != nil {
		t.Fatalf("removing the installed executable: %v", err)
	}

	stdout, stderr, err = runShadowed(shell, stateRoot, pathDir, workingDir)
	run := failedRun{stdout: stdout, stderr: stderr, err: err}
	run.expect(t, exitModuleTrust, "modules.executable_missing")
	assertMarkerAbsent(t, marker, "the shell fell back to a same-named executable outside its module store")
}

func TestARuntimeIdentityThatContradictsTheReceiptIsRejectedBeforeInvocation(t *testing.T) {
	cases := []struct {
		name    string
		fault   string
		problem string
	}{
		{name: "another namespace", fault: faultNamespaceMismatch, problem: "rpc.identity_mismatch"},
		{name: "another version", fault: faultVersionMismatch, problem: "rpc.identity_mismatch"},
		{name: "an unsupported capability", fault: faultRequiredCapability, problem: "rpc.unsupported_capability"},
		// The receipt promised the protocol the shell selected. An executable
		// that offers only another one at runtime is refused rather than
		// negotiated with, so it cannot widen what its installation declared.
		{name: "another protocol", fault: faultRuntimeProtocol, problem: "rpc.protocol_mismatch"},
	}

	shell := buildShell(t)
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			stateRoot := isolatedStateRoot(t)
			installFaultyModule(t, stateRoot, testCase.fault)

			run := tryFailingShell(t, shell, stateRoot, "reference", "call")

			run.expect(t, exitModuleTrust, testCase.problem)
			assertMarkerAbsent(t, markerPath(stateRoot, invokedMarker),
				"the shell invoked a command in a module it had already refused")
		})
	}
}

func TestAnUnknownEnvelopeMessageKindFailsClosed(t *testing.T) {
	// A message kind from a later protocol release decodes without error and
	// still leaves the shell with nothing it can act on. Ignoring it would let
	// a module's terminal message go unnoticed.
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	installFaultyModule(t, stateRoot, faultUnknownMessageKind)

	run := tryFailingShell(t, shell, stateRoot, "reference", "call")

	run.expect(t, exitModuleProcess, "rpc.unexpected_message")
}

func TestAnUnknownFieldOnAKnownMessageIsStillAccepted(t *testing.T) {
	// The other half of the same wire fact. Failing closed on an unrecognized
	// message kind must not become failing closed on every unrecognized byte,
	// or the protocol could never be extended additively.
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	installFaultyModule(t, stateRoot, faultUnknownField)

	stdout, stderr := runShell(t, shell, stateRoot, "reference", "call")

	if !strings.Contains(stdout, "operational") {
		t.Errorf("an additive unknown field cost the module its result:\n%s", stdout)
	}
	if stderr != "" {
		t.Errorf("a successful command wrote diagnostics:\n%s", stderr)
	}
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

			run := tryFailingShell(t, shell, stateRoot, "reference", "call")

			run.expect(t, exitModuleProcess, testCase.problem)
		})
	}
}

func TestAModuleThatPanicsFailsWithAStableProblemWithoutCrashingTheShell(t *testing.T) {
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	installFaultyModule(t, stateRoot, faultPanic)

	run := tryFailingShell(t, shell, stateRoot, "reference", "call")

	run.expect(t, exitModuleProcess, "rpc.no_terminal_message")
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

	run := tryFailingShell(t, shell, stateRoot, "reference", "call")

	run.expect(t, exitModuleProcess, "rpc.extra_message")
	if strings.Contains(run.stdout, "operational") {
		t.Errorf("a refused exchange still reported its result:\n%s", run.stdout)
	}
}

func TestAHangingModuleIsGivenAGracePeriodToExitBeforeItIsKilled(t *testing.T) {
	// The deadline is not simply a kill. The shell first closes the module's
	// protocol input, which tells a module that is merely slow that the
	// exchange is over, and only kills one that does not take the chance.
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	installFaultyModule(t, stateRoot, faultWaitForInputClose)

	stdout, stderr, err := runShellWithCeiling(t, shell, stateRoot, "reference", "call")

	run := failedRun{stdout: stdout, stderr: stderr, err: err}
	run.expect(t, exitModuleProcess, "rpc.timed_out")
	assertMarkerPresent(t, markerPath(stateRoot, inputClosedMarker),
		"the shell killed the module instead of letting it exit on a closed input")
}

func TestModuleDiagnosticsAreBoundedAndCannotContaminateJSONOutput(t *testing.T) {
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	installFaultyModule(t, stateRoot, faultFloodDiagnostics)

	stdout, stderr := runShell(t, shell, stateRoot, "reference", "call", "--output", "json")

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

// expectNoLaunch asserts the installed executable never ran.
//
// The installed executable is a launch canary, so the evidence is the marker it
// would have left. The shell's own account is checked too: every failure
// reachable after launch is reported with an "rpc." code.
func (r failedRun) expectNoLaunch(t *testing.T, stateRoot string) {
	t.Helper()
	assertMarkerAbsent(t, markerPath(stateRoot, canaryMarker),
		"the shell launched a module it should have refused")
	if strings.Contains(r.stderr, "rpc.") {
		t.Errorf("the shell reported a failure only a launched module can cause:\n%s", r.stderr)
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

// installLaunchCanary installs an executable that records being run, under the
// reference module's namespace and version, so a rejection can be proved to
// have happened before launch. The caller supplies whatever receipt facts its
// case is about; the rest default to a compatible installation.
func installLaunchCanary(t *testing.T, stateRoot string, install fixture.Module) modules.Receipt {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "launchcanary"+executableSuffix())
	build(t, repoRoot(t), binary, "", "./test/acceptance/testdata/launchcanary")

	install.Namespace = "reference"
	install.Version = testModuleVersion
	install.SourcePath = binary
	if install.ShellRange == "" {
		install.ShellRange = ">=0.1.0 <1.0.0"
	}
	if len(install.ProtocolVersions) == 0 {
		install.ProtocolVersions = []int{testProtocolVersionNumber}
	}
	receipt, err := fixture.Install(state.ModuleStore(stateRoot), install)
	if err != nil {
		t.Fatalf("fixture.Install returned %v", err)
	}
	return receipt
}

// markerPath is the path of a file beside the installed reference executable.
func markerPath(stateRoot, name string) string {
	store := modules.NewStore(state.ModuleStore(stateRoot))
	return filepath.Join(store.VersionDir("reference", testModuleVersion), name)
}

func assertMarkerPresent(t *testing.T, marker, message string) {
	t.Helper()
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("%s: marker %s is absent (stat error %v)", message, marker, err)
	}
}

func assertMarkerAbsent(t *testing.T, marker, message string) {
	t.Helper()
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s: marker %s exists (stat error %v)", message, marker, err)
	}
}

// runShadowed runs a status command with an impostor first on PATH and another
// in the working directory.
func runShadowed(shell, stateRoot, pathDir, workingDir string) (string, string, error) {
	command := exec.Command(shell, "reference", "call")
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
