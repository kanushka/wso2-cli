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

package rpc

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/protocol/contractv1"
)

// fakeModuleOnce builds the scriptable stand-in module once for the package.
var (
	fakeModuleOnce sync.Once
	fakeModulePath string
	fakeModuleErr  error
)

// buildFakeModule compiles internal/rpc/testdata/fakemodule.
func buildFakeModule(t *testing.T) string {
	t.Helper()
	fakeModuleOnce.Do(func() {
		directory, err := os.MkdirTemp("", "wso2-fake-module-build")
		if err != nil {
			fakeModuleErr = err
			return
		}
		binary := filepath.Join(directory, "fakemodule")
		if runtime.GOOS == "windows" {
			binary += ".exe"
		}
		command := exec.Command("go", "build", "-o", binary, "./testdata/fakemodule")
		if output, err := command.CombinedOutput(); err != nil {
			fakeModuleErr = err
			t.Logf("building the fake module: %s", output)
			return
		}
		fakeModulePath = binary
	})
	if fakeModuleErr != nil {
		t.Fatalf("cannot build the fake module: %v", fakeModuleErr)
	}
	return fakeModulePath
}

// script is one fake module installation: the executable plus the control files
// that decide how it behaves.
type script struct {
	// stdout is the byte stream the module writes as protocol frames.
	stdout []byte
	// stderr is what the module writes as diagnostics.
	stderr []byte
	// dumpEnv makes the module report its own environment as diagnostics.
	dumpEnv bool
	// delay is how long the module waits before writing its frames.
	delay time.Duration
	// linger is how long the module stays alive after writing. The value
	// "forever" never exits.
	linger string
	// exitCode is the module's exit status.
	exitCode int
}

// install writes a scripted fake module into a private directory and returns a
// launcher pointed at it.
func install(t *testing.T, scripted script) Launcher {
	t.Helper()
	directory := t.TempDir()
	executable := filepath.Join(directory, "wso2-module-reference")
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}

	source, err := os.ReadFile(buildFakeModule(t))
	if err != nil {
		t.Fatalf("reading the fake module: %v", err)
	}
	if err := os.WriteFile(executable, source, 0o755); err != nil {
		t.Fatalf("installing the fake module: %v", err)
	}

	write := func(name string, contents []byte) {
		if err := os.WriteFile(filepath.Join(directory, name), contents, 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	if scripted.stdout != nil {
		write("stdout.bin", scripted.stdout)
	}
	if scripted.stderr != nil {
		write("stderr.bin", scripted.stderr)
	}
	if scripted.dumpEnv {
		write("dump-env", nil)
	}
	if scripted.delay > 0 {
		write("delay-ms", []byte(strconv.Itoa(int(scripted.delay.Milliseconds()))))
	}
	if scripted.linger != "" {
		write("linger-ms", []byte(scripted.linger))
	}
	if scripted.exitCode != 0 {
		write("exit-code", []byte(strconv.Itoa(scripted.exitCode)))
	}

	return Launcher{
		Resolved: modules.Resolved{
			Receipt: modules.Receipt{
				SchemaVersion: modules.ReceiptSchemaVersion,
				Namespace:     testNamespace,
				ModuleVersion: testVersion,
			},
			ExecutablePath:  executable,
			ProtocolVersion: 1,
		},
		Shell:        ShellIdentity{Version: "0.0.0-dev", Platform: "test/arch"},
		InvocationID: testInvocationID,
	}
}

// conformingExchange is the byte stream of a module that handshakes and returns
// a status result.
func conformingExchange(t *testing.T) []byte {
	t.Helper()
	return moduleStream(t, conformingHello(), statusResult()).Bytes()
}

func TestALaunchedModuleReturnsItsResult(t *testing.T) {
	launcher := install(t, script{stdout: conformingExchange(t)})

	outcome, err := launcher.Invoke(t.Context(), statusInvocation())
	if err != nil {
		t.Fatalf("invoking a conforming module failed: %v", err)
	}
	if outcome.Result.Schema != "reference.status/v1" {
		t.Errorf("result schema is %q, want %q", outcome.Result.Schema, "reference.status/v1")
	}
	if outcome.InvocationID != testInvocationID {
		t.Errorf("outcome reports invocation %q, want %q", outcome.InvocationID, testInvocationID)
	}
}

func TestAModuleInheritsNoneOfTheShellsEnvironment(t *testing.T) {
	// The shell decides what a module may see. Inheriting the environment
	// would hand every module every secret the user's shell happens to hold.
	t.Setenv("WSO2_TEST_CANARY", "canary-value")
	launcher := install(t, script{stdout: conformingExchange(t), dumpEnv: true})

	outcome, err := launcher.Invoke(t.Context(), statusInvocation())
	if err != nil {
		t.Fatalf("invoking the module failed: %v", err)
	}
	if strings.Contains(outcome.Diagnostics.Text, "canary-value") {
		t.Error("the module saw a variable from the shell's environment")
	}
	for _, line := range outcome.Diagnostics.Lines() {
		name, _, _ := strings.Cut(line, "=")
		if !allowedChildEnvironment[strings.ToUpper(name)] {
			t.Errorf("the module inherited the environment entry %q", name)
		}
	}
}

// allowedChildEnvironment are the only entries a launched module may see: the
// ones an operating system needs to start a process at all.
var allowedChildEnvironment = map[string]bool{"SYSTEMROOT": true, "SYSTEMDRIVE": true, "WINDIR": true}

func TestModuleDiagnosticsAreCapturedAndBounded(t *testing.T) {
	// A looping module must not be able to exhaust shell memory, and its
	// output must never reach the stream carrying the command's result.
	flood := strings.Repeat("noisy diagnostic line\n", 5000)
	launcher := install(t, script{stdout: conformingExchange(t), stderr: []byte(flood)})
	launcher.DiagnosticLimit = 256

	outcome, err := launcher.Invoke(t.Context(), statusInvocation())
	if err != nil {
		t.Fatalf("invoking the module failed: %v", err)
	}
	if len(outcome.Diagnostics.Text) > 256 {
		t.Errorf("captured %d diagnostic bytes, want at most 256", len(outcome.Diagnostics.Text))
	}
	if !outcome.Diagnostics.Truncated {
		t.Error("diagnostics were cut short but not reported as truncated")
	}
	if outcome.Result.Schema != "reference.status/v1" {
		t.Error("a module's diagnostics prevented its result from arriving")
	}
}

func TestAModuleThatNeverAnswersIsTerminated(t *testing.T) {
	launcher := install(t, script{linger: "forever"})

	started := time.Now()
	outcome, err := launcher.Invoke(t.Context(), Invocation{
		Namespace: testNamespace, Command: []string{"status"}, Timeout: 200 * time.Millisecond,
	})
	elapsed := time.Since(started)

	if code := problemCode(t, err); code != "rpc.timed_out" {
		t.Errorf("problem code is %q, want %q", code, "rpc.timed_out")
	}
	if outcome.Result.Schema != "" {
		t.Error("a module that never answered still produced a result")
	}
	// The deadline plus the termination grace bounds how long a hanging
	// module can hold the shell.
	if limit := 200*time.Millisecond + TerminationGrace + 5*time.Second; elapsed > limit {
		t.Errorf("the shell was held for %s, want at most %s", elapsed, limit)
	}
}

func TestAModuleThatCrashesBeforeAnsweringIsAProcessProblem(t *testing.T) {
	launcher := install(t, script{exitCode: 3, stderr: []byte("the module crashed\n")})

	_, err := launcher.Invoke(t.Context(), statusInvocation())
	if code := problemCode(t, err); code != "rpc.no_terminal_message" {
		t.Errorf("problem code is %q, want %q", code, "rpc.no_terminal_message")
	}
}

func TestACrashingModulesDiagnosticsSurviveTheFailure(t *testing.T) {
	// The reason a module failed is usually the only thing it wrote, so the
	// diagnostics must outlive the problem that ended the invocation.
	launcher := install(t, script{exitCode: 3, stderr: []byte("cannot open the local socket\n")})

	outcome, err := launcher.Invoke(t.Context(), statusInvocation())
	if err == nil {
		t.Fatal("a crashing module reported no failure")
	}
	if !strings.Contains(outcome.Diagnostics.Text, "cannot open the local socket") {
		t.Errorf("diagnostics are %q, want the module's standard error", outcome.Diagnostics.Text)
	}
}

func TestAModuleThatAnswersThenExitsUncleanlyIsAProcessProblem(t *testing.T) {
	// A conforming module exits successfully once it has answered, so an
	// unclean exit means the two sides disagree about whether the command
	// finished.
	launcher := install(t, script{stdout: conformingExchange(t), exitCode: 7})

	_, err := launcher.Invoke(t.Context(), statusInvocation())
	if code := problemCode(t, err); code != "rpc.module_exited" {
		t.Errorf("problem code is %q, want %q", code, "rpc.module_exited")
	}
}

func TestPlainTextOnModuleStandardOutputIsRefused(t *testing.T) {
	// Standard output carries protocol frames only. A module that prints for a
	// user instead of framing a message is refused, rather than having its
	// output guessed at or passed through to the user's terminal.
	//
	// Which framing problem it becomes depends on the bytes — the first one is
	// read as a length — so the assertion is that it is refused as a module
	// process failure and that nothing of it reaches the user, not which of
	// the framing codes applies.
	for name, text := range map[string]string{
		"a short line":  "Status: everything is fine\n",
		"a long report": strings.Repeat("Status: everything is fine\n", 64),
	} {
		t.Run(name, func(t *testing.T) {
			launcher := install(t, script{stdout: []byte(text)})

			outcome, err := launcher.Invoke(t.Context(), statusInvocation())
			if problemCategory(t, err) != problem.CategoryModuleProcess {
				t.Errorf("problem category is %q, want %q", problemCategory(t, err), problem.CategoryModuleProcess)
			}
			if code := problemCode(t, err); !strings.HasPrefix(code, "rpc.") {
				t.Errorf("problem code is %q, want a protocol failure", code)
			}
			if outcome.Result.Schema != "" {
				t.Error("plain text on standard output produced a result")
			}
			if strings.Contains(outcome.Diagnostics.Text, "everything is fine") {
				t.Error("text a module wrote to standard output was reported as a diagnostic")
			}
		})
	}
}

func TestAModuleThatWritesNothingIsAProcessProblem(t *testing.T) {
	launcher := install(t, script{})

	_, err := launcher.Invoke(t.Context(), statusInvocation())
	if code := problemCode(t, err); code != "rpc.no_terminal_message" {
		t.Errorf("problem code is %q, want %q", code, "rpc.no_terminal_message")
	}
}

func TestAModuleStillWritingAfterARefusedHandshakeCannotBlockTheShell(t *testing.T) {
	// The shell stops reading the moment it refuses a module, which leaves a
	// module that is still writing blocked in the kernel. Reaping it has to be
	// bounded, or the refusal would hang instead of failing.
	mismatched := &contractv1.Envelope{Message: &contractv1.Envelope_Hello{Hello: &contractv1.Hello{
		Module:           &contractv1.ModuleIdentity{Namespace: "billing", Version: testVersion},
		ProtocolVersions: []uint32{1},
	}}}
	// Comfortably more than a pipe buffer, so the module blocks rather than
	// finishing its write and exiting.
	flood := append(moduleStream(t, mismatched).Bytes(), make([]byte, 1<<20)...)
	launcher := install(t, script{stdout: flood})

	completed := make(chan error, 1)
	go func() {
		_, err := launcher.Invoke(context.Background(), statusInvocation())
		completed <- err
	}()

	select {
	case err := <-completed:
		if code := problemCode(t, err); code != "rpc.identity_mismatch" {
			t.Errorf("problem code is %q, want %q", code, "rpc.identity_mismatch")
		}
	case <-time.After(TerminationGrace + 30*time.Second):
		t.Fatal("the shell was still waiting for a module it had already refused")
	}
}

func TestAMissingExecutableIsReportedRatherThanPanicking(t *testing.T) {
	launcher := install(t, script{})
	launcher.Resolved.ExecutablePath = filepath.Join(t.TempDir(), "absent")

	_, err := launcher.Invoke(t.Context(), statusInvocation())
	if code := problemCode(t, err); code != "rpc.module_not_launched" {
		t.Errorf("problem code is %q, want %q", code, "rpc.module_not_launched")
	}
}

func TestAGeneratedInvocationIdentifierIsUsedWhenNoneIsSupplied(t *testing.T) {
	// The identifier is the shell's, not the module's, and a later increment
	// binds issued access tokens to it, so it must be generated per
	// invocation rather than reused.
	first := install(t, script{})
	first.InvocationID = ""
	second := install(t, script{})
	second.InvocationID = ""

	firstOutcome, _ := first.Invoke(t.Context(), statusInvocation())
	secondOutcome, _ := second.Invoke(t.Context(), statusInvocation())

	if firstOutcome.InvocationID == "" {
		t.Fatal("no invocation identifier was generated")
	}
	if firstOutcome.InvocationID == secondOutcome.InvocationID {
		t.Error("two invocations were given the same identifier")
	}
}

func TestASlowButConformingModuleStillSucceeds(t *testing.T) {
	launcher := install(t, script{stdout: conformingExchange(t), delay: 150 * time.Millisecond})

	outcome, err := launcher.Invoke(t.Context(), Invocation{
		Namespace: testNamespace, Command: []string{"status"}, Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("a slow but conforming module failed: %v", err)
	}
	if outcome.Result.Schema != "reference.status/v1" {
		t.Errorf("result schema is %q, want %q", outcome.Result.Schema, "reference.status/v1")
	}
}
