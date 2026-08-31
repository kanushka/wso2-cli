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

package app_test

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/modules/fixture"
	"github.com/wso2/wso2-cli/internal/output"
)

func TestCommandNamesAreDerivedFromTheShellCommandTree(t *testing.T) {
	if got, want := app.CommandNames(), []string{"config", "context", "doctor", "help", "identity", "login", "logout", "module", "org", "version", "whoami"}; !slices.Equal(got, want) {
		t.Errorf("CommandNames() = %v, want %v", got, want)
	}
}

// TestHelpListsEveryShellCommand proves the help text is generated from the
// command tree rather than maintained by hand: a command that exists is listed,
// and the note about product commands survives.
func TestHelpListsEveryShellCommand(t *testing.T) {
	shell, out, errOut := newShell(t)

	if code := shell.Run([]string{"help"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	for _, command := range []string{"config", "context", "doctor", "help", "identity", "login", "logout", "module", "org", "version", "whoami"} {
		if !strings.Contains(out.String(), command) {
			t.Errorf("help does not list the %q command:\n%s", command, out)
		}
	}
	if !strings.Contains(out.String(), "installed modules") {
		t.Errorf("help does not say product commands come from installed modules:\n%s", out)
	}
}

// TestACommandDescribesItsOwnFlags proves per-command help reaches the user, so
// a flag does not have to be discovered from the whole help page.
func TestACommandDescribesItsOwnFlags(t *testing.T) {
	shell, out, errOut := newShell(t)

	if code := shell.Run([]string{"module", "--help"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.Contains(out.String(), "module") {
		t.Fatalf("wso2 module --help does not describe the command:\n%s", out)
	}
}

// TestAMisspelledCommandSuggestsTheClosestOne proves a typo costs a keystroke
// rather than a trip to the documentation.
func TestAMisspelledCommandSuggestsTheClosestOne(t *testing.T) {
	shell, _, errOut := newShell(t)

	if code := shell.Run([]string{"vresion"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want the usage class %d", code, exit.Usage)
	}
	if !strings.Contains(errOut.String(), "Did you mean") {
		t.Fatalf("stderr offers no suggestion for the typo:\n%s", errOut)
	}
	if !strings.Contains(errOut.String(), "wso2 version") {
		t.Fatalf("stderr does not suggest the version command:\n%s", errOut)
	}
}

// TestTheOutputFlagIsAcceptedInEverySpelling proves pflag's POSIX conventions
// reach the user: the spelling a user habitually types is accepted rather than
// mistaken for a command.
//
// It is asserted on the product namespace path because that is what honors the
// flag today. The fixture does not speak the contract, so reaching the contract
// failure is the evidence the flag was accepted and the namespace dispatched.
func TestTheOutputFlagIsAcceptedInEverySpelling(t *testing.T) {
	for _, spelling := range [][]string{
		{"--output", "json"},
		{"--output=json"},
		{"-o", "json"},
		{"-o=json"},
		{"-ojson"},
	} {
		t.Run(strings.Join(spelling, " "), func(t *testing.T) {
			shell, out, errOut := newShell(t)
			installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "1.0.0"})

			args := append(append([]string{}, spelling...), "reference", "status")
			if code := shell.Run(args); code != exit.ModuleProcess {
				t.Fatalf("exit code = %d, want the module process class %d; stdout: %s stderr: %s",
					code, exit.ModuleProcess, out, errOut)
			}
			if strings.Contains(errOut.String(), "shell.unknown_command") {
				t.Fatalf("%v was mistaken for a command:\n%s", spelling, errOut)
			}
		})
	}
}

// TestEveryFlagFailureIsAUsageProblem proves a parse failure reports in the
// shell's voice, with the documented exit class and recovery guidance, rather
// than in the framework's.
func TestEveryFlagFailureIsAUsageProblem(t *testing.T) {
	for name, args := range map[string][]string{
		"unknown flag":         {"version", "--nonexistent"},
		"missing value":        {"version", "--output"},
		"unknown shorthand":    {"version", "-Z"},
		"malformed flag value": {"version", "--output=notamode"},
	} {
		t.Run(name, func(t *testing.T) {
			shell, _, errOut := newShell(t)

			if code := shell.Run(args); code != exit.Usage {
				t.Fatalf("exit code = %d, want the usage class %d; stderr: %s", code, exit.Usage, errOut)
			}
			if !strings.Contains(errOut.String(), "shell.") {
				t.Fatalf("stderr does not report a typed shell problem:\n%s", errOut)
			}
			// A typed problem renders its message and, below it, its
			// recovery guidance. Guidance is what makes the refusal
			// actionable, so its absence is the failure worth catching.
			if len(strings.Split(strings.TrimSpace(errOut.String()), "\n")) < 2 {
				t.Fatalf("stderr carries no recovery guidance:\n%s", errOut)
			}
		})
	}
}

// TestUsageIsNotPrintedAlongsideAProblem proves the shell owns its own
// diagnostics: a usage dump alongside a typed problem would be the framework
// writing user output.
func TestUsageIsNotPrintedAlongsideAProblem(t *testing.T) {
	shell, out, errOut := newShell(t)

	if code := shell.Run([]string{"version", "--nonexistent"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want the usage class %d", code, exit.Usage)
	}
	if out.Len() != 0 {
		t.Fatalf("a failing command wrote to standard output:\n%s", out)
	}
	if strings.Count(errOut.String(), "Usage:") > 0 {
		t.Fatalf("the framework printed usage alongside the typed problem:\n%s", errOut)
	}
}

// TestAProductFlagStillReachesTheModule proves the passthrough rule survived
// the framework adoption. A product namespace is not a Cobra command, so a flag
// the shell does not recognize is never Cobra's to reject.
//
// The fixture executable does not speak the module contract, so reaching the
// contract failure is the evidence that resolution and launch happened: had the
// shell rejected the flag, this would be a usage problem instead.
func TestAProductFlagStillReachesTheModule(t *testing.T) {
	for _, args := range [][]string{
		{"reference", "status", "--env", "prod"},
		{"reference", "status", "--env=prod"},
		{"reference", "--output", "json", "status", "--env", "prod"},
		{"reference", "status", "--env", "prod", "--output", "json"},
		// A shell flag before the namespace must not put the module's own flags
		// in front of the shell's parser.
		{"--output", "json", "reference", "status", "--env", "prod"},
		{"-o", "json", "reference", "status", "--env=prod"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			shell, _, errOut := newShell(t)
			installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "1.0.0"})

			if code := shell.Run(args); code != exit.ModuleProcess {
				t.Fatalf("exit code = %d, want the module process class %d; stderr: %s",
					code, exit.ModuleProcess, errOut)
			}
			if strings.Contains(errOut.String(), "shell.unknown_flag") {
				t.Fatalf("the shell rejected a flag that belongs to the module:\n%s", errOut)
			}
		})
	}
}

// TestCompletionIsNotOffered proves the framework's generated completion command
// is absent. Until a module declares its command tree, completion would know
// every built-in and no product command, which a user reads as absence of the
// command rather than absence of information.
func TestCompletionIsNotOffered(t *testing.T) {
	shell, out, _ := newShell(t)

	if code := shell.Run([]string{"help"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d", code, exit.OK)
	}
	if strings.Contains(out.String(), "completion") {
		t.Fatalf("help offers a completion command:\n%s", out)
	}
}

// TestAShellFlagBeforeAProductNamespaceIsHonored proves a shell flag is accepted
// on either side of a product namespace, not only after it.
//
// The namespace is not a Cobra command, so arguments that survive the root's own
// flag parsing have to be routed to namespace resolution rather than treated as
// no command at all. Printing help here would be a silent wrong answer.
func TestAShellFlagBeforeAProductNamespaceIsHonored(t *testing.T) {
	for _, args := range [][]string{
		{"--output", "json", "reference", "status"},
		{"-o", "json", "reference", "status"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			shell, out, errOut := newShell(t)
			installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "1.0.0"})

			// The fixture does not speak the contract, so reaching the contract
			// failure is the evidence the namespace was dispatched.
			if code := shell.Run(args); code != exit.ModuleProcess {
				t.Fatalf("exit code = %d, want the module process class %d; stdout: %s stderr: %s",
					code, exit.ModuleProcess, out, errOut)
			}
			if strings.Contains(out.String(), "Usage: wso2") {
				t.Fatalf("the shell showed help instead of dispatching the namespace:\n%s", out)
			}
		})
	}
}

// TestAModuleFlagBeginningWithTheOutputShorthandIsNotClaimed proves the shell
// does not widen its own flag into a module's namespace.
//
// The shell accepts an attached shorthand value, -ojson, so a prefix match would
// also claim -optimize and any other module flag starting with the same letter.
// A module owns its flags, so only an actual mode is claimed.
func TestAModuleFlagBeginningWithTheOutputShorthandIsNotClaimed(t *testing.T) {
	shell, _, errOut := newShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "1.0.0"})

	// The fixture does not speak the contract, so reaching the contract failure
	// is the evidence the flag was forwarded rather than rejected here.
	if code := shell.Run([]string{"reference", "status", "-optimize"}); code != exit.ModuleProcess {
		t.Fatalf("exit code = %d, want the module process class %d; stderr: %s",
			code, exit.ModuleProcess, errOut)
	}
	if strings.Contains(errOut.String(), "output mode") {
		t.Fatalf("the shell claimed a module flag as its own:\n%s", errOut)
	}
}

// TestAShellFlagACommandCannotActOnIsRefused proves a flag is never silently
// ignored.
//
// The shell declares its common flags once on the root, but not every built-in
// can act on all of them: version renders fixed output and the module lifecycle
// commands select no context. A value accepted and then dropped is worse than a
// refusal, because the user believes it took effect.
func TestAShellFlagACommandCannotActOnIsRefused(t *testing.T) {
	for _, args := range [][]string{
		{"--output", "json", "version"},
		{"version", "--output", "json"},
		{"--output", "json", "module", "list"},
		{"--context", "prod", "module", "list"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			shell, _, errOut := newShell(t)

			if code := shell.Run(args); code != exit.Usage {
				t.Fatalf("exit code = %d, want the usage class %d; stderr: %s", code, exit.Usage, errOut)
			}
			if !strings.Contains(errOut.String(), "shell.unsupported_flag") {
				t.Fatalf("stderr does not report the flag as unsupported:\n%s", errOut)
			}
		})
	}
}

// TestLoginActsOnTheContextFlagFromEitherSide proves the flag a command does
// honor reaches it, written before or after the command name.
func TestLoginActsOnTheContextFlagFromEitherSide(t *testing.T) {
	for _, args := range [][]string{
		{"--context", "nosuchcontext", "login"},
		{"login", "--context", "nosuchcontext"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			shell, _, errOut := newShell(t)

			// No context document exists, so reaching context resolution is the
			// evidence the flag arrived.
			if code := shell.Run(args); code == exit.OK {
				t.Fatalf("exit code = %d, want a failure; stderr: %s", code, errOut)
			}
			if !strings.Contains(errOut.String(), "nosuchcontext") {
				t.Fatalf("the context flag did not reach login:\n%s", errOut)
			}
		})
	}
}

// failingWriter refuses every write, standing in for an unwritable output
// stream such as a closed pipe.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("the stream is closed")
}

// TestAFailureInsideACommandIsNotReportedAsAUsageProblem proves the framework's
// error path did not swallow the shell's classification.
//
// Executing the command tree runs the command bodies as well as parsing flags,
// so treating every error it returns as a parse failure would report an
// unwritable stream as the user's mistake, with the usage exit class and
// recovery guidance about flags. A flag failure is a usage problem; a stream
// that cannot be written is not.
func TestAFailureInsideACommandIsNotReportedAsAUsageProblem(t *testing.T) {
	shell := app.Shell{
		StateRoot: t.TempDir(),
		Streams:   output.Streams{Out: failingWriter{}, Err: &bytes.Buffer{}},
	}

	code := shell.Run([]string{"version"})

	if code == exit.OK {
		t.Fatalf("exit code = %d, want a failure", code)
	}
	if code == exit.Usage {
		t.Fatalf("an unwritable output stream was reported as a usage problem (exit %d)", code)
	}
	if code != exit.ModuleProcess {
		t.Fatalf("exit code = %d, want the module process class %d", code, exit.ModuleProcess)
	}
}
