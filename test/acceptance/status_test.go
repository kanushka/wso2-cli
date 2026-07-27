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

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/modules/fixture"
	"github.com/wso2/wso2-cli/internal/state"
)

// statusFields are the semantic fields the reference status result carries, in
// the order both renderings must follow.
var statusFields = []string{"organization", "service", "status", "checkedAt"}

func TestReferenceStatusRendersAReadableTable(t *testing.T) {
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	installReferenceModule(t, stateRoot, buildReferenceModule(t))

	stdout, stderr := runShell(t, shell, stateRoot, "reference", "status")

	for _, want := range []string{"ORGANIZATION", "SERVICE", "STATUS", "CHECKED AT", "operational"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the table does not contain %q:\n%s", want, stdout)
		}
	}
	if stderr != "" {
		t.Errorf("a successful command wrote diagnostics:\n%s", stderr)
	}
}

func TestReferenceStatusRendersDeterministicJSON(t *testing.T) {
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	installReferenceModule(t, stateRoot, buildReferenceModule(t))

	stdout, stderr := runShell(t, shell, stateRoot, "reference", "status", "--output", "json")

	decoded := decodeStatusJSON(t, stdout)
	for _, field := range statusFields {
		if decoded[field] == "" {
			t.Errorf("the JSON result has no %q field:\n%s", field, stdout)
		}
	}
	if got := keyOrder(t, stdout); !equalStrings(got, statusFields) {
		t.Errorf("JSON keys are in order %v, want the module's declared order %v", got, statusFields)
	}
	if _, err := time.Parse(time.RFC3339, decoded["checkedAt"]); err != nil {
		t.Errorf("checkedAt %q is not an RFC 3339 time: %v", decoded["checkedAt"], err)
	}
	if stderr != "" {
		t.Errorf("a successful command wrote diagnostics:\n%s", stderr)
	}
}

func TestTableAndJSONReportTheSameFields(t *testing.T) {
	// Both renderings are driven by one set of semantic fields, so a value
	// that appears in one must appear in the other.
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	installReferenceModule(t, stateRoot, buildReferenceModule(t))

	table, _ := runShell(t, shell, stateRoot, "reference", "status")
	asJSON, _ := runShell(t, shell, stateRoot, "reference", "status", "--output", "json")

	decoded := decodeStatusJSON(t, asJSON)
	for _, field := range statusFields {
		// checkedAt is read at the moment of each run, so only the fields
		// that are stable between runs can be compared by value.
		if field == "checkedAt" {
			continue
		}
		if !strings.Contains(table, decoded[field]) {
			t.Errorf("the table does not report the %s value %q:\n%s", field, decoded[field], table)
		}
	}
}

func TestJSONOutputStaysValidWhileTheModuleWritesDiagnostics(t *testing.T) {
	// Diagnostics travel on standard error whatever the output mode, so a
	// noisy module cannot corrupt the document a script parses.
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	installNoisyModule(t, stateRoot)

	stdout, stderr := runShell(t, shell, stateRoot, "reference", "status", "--output", "json")

	decoded := decodeStatusJSON(t, stdout)
	if decoded["status"] != "operational" {
		t.Errorf("the JSON result reports status %q, want %q", decoded["status"], "operational")
	}
	if !strings.Contains(stderr, "a diagnostic from the module") {
		t.Errorf("the module's diagnostics were not reported:\n%s", stderr)
	}
	if strings.Contains(stdout, "a diagnostic from the module") {
		t.Errorf("module diagnostics reached standard output:\n%s", stdout)
	}
}

func TestAnUnknownOutputModeFailsWithTheUsageExitClass(t *testing.T) {
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	installReferenceModule(t, stateRoot, buildReferenceModule(t))

	stdout, stderr, err := tryShell(shell, stateRoot, "reference", "status", "--output", "yaml")

	if exitCode(t, err) != 64 {
		t.Fatalf("exit status = %v, want the usage class 64\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stderr, "shell.unknown_output_mode") {
		t.Errorf("stderr does not name the usage problem:\n%s", stderr)
	}
	if stdout != "" {
		t.Errorf("a rejected output mode still wrote to standard output:\n%s", stdout)
	}
}

func TestAModuleThatSpeaksAnotherProtocolFailsBeforeInvocation(t *testing.T) {
	// The receipt promises a protocol the shell does not speak, so the module
	// is never launched.
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	if _, err := fixture.Install(state.ModuleStore(stateRoot), fixture.Module{
		Namespace:        "reference",
		Version:          testModuleVersion,
		ShellRange:       ">=0.1.0 <1.0.0",
		ProtocolVersions: []int{testProtocolVersionNumber + 1},
		SourcePath:       buildReferenceModule(t),
	}); err != nil {
		t.Fatalf("fixture.Install returned %v", err)
	}

	stdout, stderr, err := tryShell(shell, stateRoot, "reference", "status")

	if exitCode(t, err) != 69 {
		t.Fatalf("exit status = %v, want the module trust class 69\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stderr, "modules.incompatible_protocol") {
		t.Errorf("stderr does not report the protocol mismatch:\n%s", stderr)
	}
	if stdout != "" {
		t.Errorf("a refused module still wrote to standard output:\n%s", stdout)
	}
}

func TestAModuleThatNeverAnswersFailsWithAStableProblem(t *testing.T) {
	// A hanging module must not merely stop blocking the shell eventually. It
	// has to end as one named problem in the module process exit class, with
	// nothing on standard output for a script to misread as a result, and the
	// module process itself has to be gone.
	if runtime.GOOS == "windows" {
		t.Skip("the hanging module fixture is a POSIX shell script")
	}
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	// The module records its own process id before hanging, so the test can
	// ask whether the shell terminated it rather than merely stopped waiting.
	pidFile := filepath.Join(t.TempDir(), "module.pid")
	installScriptedModule(t, stateRoot,
		"#!/bin/sh\necho $$ > '"+pidFile+"'\nwhile true; do sleep 1; done\n")

	stdout, stderr, err := runShellWithCeiling(t, shell, stateRoot, "reference", "status")

	if exitCode(t, err) != 70 {
		t.Fatalf("exit status = %v, want the module process class 70\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stderr, "rpc.timed_out") {
		t.Errorf("stderr does not report the deadline:\n%s", stderr)
	}
	if stdout != "" {
		t.Errorf("a module that never answered still wrote to standard output:\n%s", stdout)
	}
	// The module ignores the closed protocol input, so only the kill that ends
	// the termination grace can have stopped it.
	if pid := readPID(t, pidFile); processIsRunning(pid) {
		t.Errorf("the hanging module (process %d) outlived the shell", pid)
	}
}

// readPID reads a process id a fixture module recorded for itself.
func readPID(t *testing.T, path string) int {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the module recorded no process id: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil {
		t.Fatalf("the recorded process id %q is not a number: %v", content, err)
	}
	return pid
}

func TestAModuleThatCrashesBeforeAnsweringFailsWithAStableProblem(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the crashing module fixture is a POSIX shell script")
	}
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	installScriptedModule(t, stateRoot,
		"#!/bin/sh\necho 'the module could not reach its local socket' >&2\nexit 3\n")

	stdout, stderr, err := tryShell(shell, stateRoot, "reference", "status")

	if exitCode(t, err) != 70 {
		t.Fatalf("exit status = %v, want the module process class 70\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stderr, "rpc.no_terminal_message") {
		t.Errorf("stderr does not report the missing result:\n%s", stderr)
	}
	// What the module said before dying is usually the only explanation of
	// why, so it has to survive the failure that ended the invocation.
	if !strings.Contains(stderr, "could not reach its local socket") {
		t.Errorf("stderr does not carry the module's own diagnostics:\n%s", stderr)
	}
	if stdout != "" {
		t.Errorf("a crashing module still wrote to standard output:\n%s", stdout)
	}
}

func TestAModuleThatAnswersThenExitsUncleanlyFailsWithAStableProblem(t *testing.T) {
	// A conforming module exits successfully once it has answered, so an
	// unclean exit means the module and the shell disagree about whether the
	// command finished. The shell reports that rather than printing a result
	// the module may not stand behind.
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	installNoisyModule(t, stateRoot)
	steerInstalledModule(t, stateRoot, "exit-uncleanly")

	stdout, stderr, err := tryShell(shell, stateRoot, "reference", "status")

	if exitCode(t, err) != 70 {
		t.Fatalf("exit status = %v, want the module process class 70\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stderr, "rpc.module_exited") {
		t.Errorf("stderr does not report the unclean exit:\n%s", stderr)
	}
	if stdout != "" {
		t.Errorf("a module that exited uncleanly still wrote a result:\n%s", stdout)
	}
}

// decodeStatusJSON parses the shell's JSON output, failing the test when it is
// not a valid document of string fields.
func decodeStatusJSON(t *testing.T, document string) map[string]string {
	t.Helper()
	decoded := make(map[string]string)
	if err := json.Unmarshal([]byte(document), &decoded); err != nil {
		t.Fatalf("the JSON result is not a valid document: %v\n%s", err, document)
	}
	return decoded
}

// keyOrder reports the object keys in the order they appear in the document.
func keyOrder(t *testing.T, document string) []string {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(document))
	if _, err := decoder.Token(); err != nil {
		t.Fatalf("the JSON result does not open an object: %v\n%s", err, document)
	}
	var keys []string
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			t.Fatalf("reading a JSON key: %v", err)
		}
		keys = append(keys, key.(string))
		if _, err := decoder.Token(); err != nil {
			t.Fatalf("reading a JSON value: %v", err)
		}
	}
	return keys
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

// installNoisyModule installs a conforming module that writes diagnostics while
// it answers, under the reference module's namespace and version.
func installNoisyModule(t *testing.T, stateRoot string) {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "noisymodule"+executableSuffix())
	build(t, repoRoot(t), binary,
		"-X main.moduleVersion="+testModuleVersion+
			" -X github.com/wso2/wso2-cli/sdk/protocol.Version="+testProtocolVersion,
		"./test/acceptance/testdata/noisymodule")
	installReferenceModule(t, stateRoot, binary)
}

// steerInstalledModule writes a control file beside an installed module
// executable.
//
// The acceptance module reads these rather than arguments or environment
// variables, because the shell supplies neither. Writing one leaves the
// executable untouched, so its receipt digest still matches and the shell
// still launches it.
func steerInstalledModule(t *testing.T, stateRoot, name string) {
	t.Helper()
	writeControlFile(t, stateRoot, name, "")
}

// writeControlFile writes a control file, with content, beside the installed
// reference executable.
func writeControlFile(t *testing.T, stateRoot, name, content string) {
	t.Helper()
	store := modules.NewStore(state.ModuleStore(stateRoot))
	path := filepath.Join(store.VersionDir("reference", testModuleVersion), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing the control file %s: %v", name, err)
	}
}

// runShellWithCeiling runs the shell under a wall-clock ceiling, so a test for
// a module that never answers fails rather than hangs when the shell's own
// deadline does not fire.
func runShellWithCeiling(t *testing.T, shell, stateRoot string, args ...string) (string, string, error) {
	t.Helper()
	const ceiling = 2 * time.Minute

	command := exec.Command(shell, args...)
	command.Env = shellEnvironment(stateRoot)
	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatalf("starting the shell failed: %v", err)
	}

	finished := make(chan error, 1)
	go func() { finished <- command.Wait() }()

	select {
	case err := <-finished:
		return stdout.String(), stderr.String(), err
	case <-time.After(ceiling):
		_ = command.Process.Kill()
		<-finished
		t.Fatalf("the shell was still running %s after invoking the module", ceiling)
		return "", "", nil
	}
}

// installScriptedModule installs a POSIX shell script as the reference module.
func installScriptedModule(t *testing.T, stateRoot, script string) {
	t.Helper()
	if _, err := fixture.Install(state.ModuleStore(stateRoot), fixture.Module{
		Namespace:        "reference",
		Version:          testModuleVersion,
		ShellRange:       ">=0.1.0 <1.0.0",
		ProtocolVersions: []int{testProtocolVersionNumber},
		Contents:         []byte(script),
	}); err != nil {
		t.Fatalf("fixture.Install returned %v", err)
	}
}

// tryShell runs the shell and returns its output and exit error rather than
// failing the test, for the runs that are expected to fail.
func tryShell(shell, stateRoot string, args ...string) (string, string, error) {
	command := exec.Command(shell, args...)
	command.Env = shellEnvironment(stateRoot)
	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout.String(), stderr.String(), err
}

// exitCode reports the process status behind a failed run.
func exitCode(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		t.Fatal("the command succeeded, want a failure")
	}
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		t.Fatalf("the command failed without an exit status: %v", err)
	}
	return exitError.ExitCode()
}
