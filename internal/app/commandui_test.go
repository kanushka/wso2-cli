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
	"github.com/wso2/wso2-cli/internal/state"
	"github.com/wso2/wso2-cli/sdk/commandtree"
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

// TestEveryCommandFamilyAnswersABareNameWithHelp pins that a bare family name
// prints that family's help and succeeds, in all five families.
//
// A bare family name is an incomplete command, not a failed one. It used to be
// refused as shell.missing_argument at exit 64, which said "this command is
// broken" about five families whose every subcommand works — clearly enough
// that a reader of docs/examples/user-flow-review.md proposed hiding config and
// org from the tree until their subcommands were "implemented" (F8).
//
// The refusal that matters is pinned by
// TestEveryCommandFamilyRefusesAnUnknownSubcommand below, and the two must not
// be collapsed into one: the whole point is that the shell tells an incomplete
// command and a mistyped one apart.
func TestEveryCommandFamilyAnswersABareNameWithHelp(t *testing.T) {
	// Each family and one subcommand its help has to name.
	families := map[string]string{
		"context":  "create",
		"identity": "add-product",
		"module":   "available",
		"org":      "current",
		"config":   "set",
	}
	for family, subcommand := range families {
		t.Run(family, func(t *testing.T) {
			shell, out, errOut := newShell(t)

			if code := shell.Run([]string{family}); code != 0 {
				t.Fatalf("wso2 %s exited %d, want 0: naming a family without a "+
					"subcommand is an incomplete command, not a failed one; stderr: %s",
					family, code, errOut)
			}
			// Help is a result, so it belongs on stdout. Writing it to stderr
			// would keep it out of a pipe and read as diagnostic output.
			if out.Len() == 0 {
				t.Fatalf("wso2 %s printed nothing to stdout; stderr: %s", family, errOut)
			}
			if strings.Contains(out.String(), "error:") {
				t.Errorf("wso2 %s calls itself an error:\n%s", family, out)
			}
			// The guidance that used to be the refusal's recovery line is the
			// family's Long, so the help still points at the family's own
			// subcommands. A help body that names none leaves the user exactly
			// where they started.
			if !strings.Contains(out.String(), "wso2 "+family+" ") {
				t.Errorf("wso2 %s help does not point at one of its own subcommands:\n%s", family, out)
			}
			if !strings.Contains(out.String(), subcommand) {
				t.Errorf("wso2 %s help does not name its %q subcommand:\n%s", family, subcommand, out)
			}
		})
	}
}

// TestEveryCommandFamilyRefusesAnUnknownSubcommand pins the half of the
// families' RunE that must stay a refusal.
//
// Cobra validates a non-leaf command's arguments only when it is Runnable, so
// a parent with a nil RunE prints help and exits 0 — which reports a typo to
// whatever script ran it as everything having worked. #133 made all five
// families refuse instead, and giving the bare form back to help must not take
// that with it.
func TestEveryCommandFamilyRefusesAnUnknownSubcommand(t *testing.T) {
	for _, family := range []string{"context", "identity", "module", "org", "config"} {
		t.Run(family, func(t *testing.T) {
			shell, _, errOut := newShell(t)

			if code := shell.Run([]string{family, "nosuchsubcommand"}); code != exit.Usage {
				t.Fatalf("wso2 %s nosuchsubcommand exited %d, want the usage class %d: "+
					"exit 0 tells a script the typo worked; stderr: %s",
					family, code, exit.Usage, errOut)
			}
			if !strings.Contains(errOut.String(), "shell.unknown_command") {
				t.Errorf("wso2 %s nosuchsubcommand does not report shell.unknown_command:\n%s", family, errOut)
			}
			// writeProblem (internal/output/output.go) renders the message on
			// its own first line as "error: ... (code)" and, only when one is
			// set, the recovery on the line below as "  <recovery>". Split on
			// the first newline so the recovery assertion below cannot be
			// satisfied by the message line, which already contains
			// "wso2 <family> " as part of its own text.
			lines := strings.SplitN(errOut.String(), "\n", 2)
			if len(lines) < 2 || strings.TrimSpace(lines[1]) == "" {
				t.Fatalf("wso2 %s did not print recovery guidance below its message:\n%s", family, errOut)
			}
			// A refusal that does not name one of the family's own
			// subcommands leaves the user exactly where they started; a stub
			// like "Sorry." would satisfy a bare line-count check, so pin the
			// documented promise instead (docs/reference/commands.md).
			if !strings.Contains(lines[1], "wso2 "+family+" ") {
				t.Errorf("wso2 %s recovery does not point at one of its own subcommands:\n%s", family, errOut)
			}
		})
	}
}

// TestBothSpellingsOfARefusedFlagAgree pins #154: a shell flag a command does
// not declare is refused the same way whether it is written long or short.
//
// pflag words the two failures differently — "unknown flag: --output" against
// "unknown shorthand flag: 'o' in -o" — and unknownFlagName read only the
// first. So the long spelling reached the shell's own refusal and the short one
// fell through to the verbatim parser message, giving one request two problem
// codes and leaking pflag's vocabulary into user-facing text. The recovery
// differed too: the long form named the command's own help, the short form
// named the whole shell tree.
func TestBothSpellingsOfARefusedFlagAgree(t *testing.T) {
	for _, command := range [][]string{
		{"version"},
		{"login"},
		{"module", "list"},
		{"module", "available"},
	} {
		t.Run(strings.Join(command, " "), func(t *testing.T) {
			long := runForStderr(t, append(append([]string{}, command...), "--output", "json"))
			short := runForStderr(t, append(append([]string{}, command...), "-o", "json"))

			if long != short {
				t.Errorf("the two spellings of --output are refused differently:\n"+
					"  --output: %s\n  -o      : %s", long, short)
			}
			if !strings.Contains(short, "shell.unsupported_flag") {
				t.Errorf("-o is not reported as an unsupported shell flag:\n%s", short)
			}
			// pflag's own wording must not survive into what the user reads.
			if strings.Contains(short, "unknown shorthand flag") {
				t.Errorf("the refusal leaks pflag's wording:\n%s", short)
			}
		})
	}
}

// TestAShorthandThatIsNotAShellFlagKeepsTheOrdinaryRefusal pins the other half
// of #154's fix. Reading the shorthand must not turn every unknown letter into
// "does not take the flag --something": a letter the root does not declare is a
// typo, not a shell flag named on the wrong command, and the distinction
// ownsShellFlag draws is the reason shell.unsupported_flag exists at all.
func TestAShorthandThatIsNotAShellFlagKeepsTheOrdinaryRefusal(t *testing.T) {
	stderr := runForStderr(t, []string{"module", "list", "-z"})

	if strings.Contains(stderr, "shell.unsupported_flag") {
		t.Errorf("an unknown shorthand is reported as a shell flag the command refuses:\n%s", stderr)
	}
	if !strings.Contains(stderr, "shell.unknown_flag") {
		t.Errorf("an unknown shorthand is not reported as an unknown flag:\n%s", stderr)
	}
}

// runForStderr runs the shell and returns what it wrote to standard error,
// failing the test when the command did not report a usage problem.
func runForStderr(t *testing.T, args []string) string {
	t.Helper()
	shell, _, errOut := newShell(t)
	if code := shell.Run(args); code != exit.Usage {
		t.Fatalf("wso2 %s exited %d, want the usage class %d; stderr: %s",
			strings.Join(args, " "), code, exit.Usage, errOut)
	}
	return errOut.String()
}

// referenceTree is the declared command tree the help tests install: a
// namespace that groups one runnable subcommand, which is the smallest shape
// the reference module itself has.
func referenceTree() commandtree.Tree {
	return commandtree.New([]commandtree.Command{
		{Path: nil, Short: "Explore the reference product."},
		{Path: []string{"status"}, Runnable: true, Short: "Report the product status."},
	})
}

// TestHelpListsTheInstalledModuleNamespaces pins the fix for the highest-rated
// usability finding: a user who has just installed a module looks at help
// first, and a footer that never mentioned the installation read as the
// installation having failed.
func TestHelpListsTheInstalledModuleNamespaces(t *testing.T) {
	shell, out, errOut := newShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})

	if code := shell.Run([]string{"help"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.Contains(out.String(), "Installed: reference") {
		t.Errorf("help does not name the installed namespace:\n%s", out)
	}
	if !strings.Contains(out.String(), "wso2 <namespace> --help") {
		t.Errorf("help does not say how to see a module's commands:\n%s", out)
	}
}

// TestHelpSaysWhenNoModuleIsInstalled proves the footer stays truthful in the
// empty state and points at the way out of it.
func TestHelpSaysWhenNoModuleIsInstalled(t *testing.T) {
	shell, out, errOut := newShell(t)

	if code := shell.Run([]string{"help"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.Contains(out.String(), "None are installed") {
		t.Errorf("help does not say no modules are installed:\n%s", out)
	}
	if !strings.Contains(out.String(), "wso2 module available") {
		t.Errorf("help does not point at wso2 module available:\n%s", out)
	}
}

// TestHelpStillRendersWhenTheStateRootIsUnusable proves the footer degrades
// rather than taking the help page with it: a broken environment must never
// make help itself fail.
func TestHelpStillRendersWhenTheStateRootIsUnusable(t *testing.T) {
	t.Setenv(state.RootEnvVar, "relative/not-absolute")
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	shell := app.Shell{Streams: output.Streams{Out: out, Err: errOut}}

	if code := shell.Run([]string{"help"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.Contains(out.String(), "Product commands are provided by installed modules.") {
		t.Errorf("help lost its product-commands footer:\n%s", out)
	}
}

// TestHelpForAnInstalledNamespaceRendersTheModulesDeclaredHelp proves
// wso2 help <namespace> answers the same page wso2 <namespace> --help does,
// instead of the root page it used to print.
func TestHelpForAnInstalledNamespaceRendersTheModulesDeclaredHelp(t *testing.T) {
	shell, out, errOut := newShell(t)
	installFixture(t, shell, fixture.Module{
		Namespace: "reference", Version: "0.1.0", CommandTree: referenceTree(),
	})

	if code := shell.Run([]string{"help", "reference"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.Contains(out.String(), "wso2 reference") {
		t.Errorf("the page is not about the reference namespace:\n%s", out)
	}
	if !strings.Contains(out.String(), "status") {
		t.Errorf("the page does not list the declared subcommand:\n%s", out)
	}
	if strings.Contains(out.String(), "Shell commands") {
		t.Errorf("the root page was printed instead of the module's:\n%s", out)
	}
}

// TestHelpForANamespaceSubcommandRendersThatCommandsPage proves the topic walk
// goes below the namespace, so wso2 help reference status is the status page.
func TestHelpForANamespaceSubcommandRendersThatCommandsPage(t *testing.T) {
	shell, out, errOut := newShell(t)
	installFixture(t, shell, fixture.Module{
		Namespace: "reference", Version: "0.1.0", CommandTree: referenceTree(),
	})

	if code := shell.Run([]string{"help", "reference", "status"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.Contains(out.String(), "wso2 reference status") {
		t.Errorf("the page is not about the status command:\n%s", out)
	}
	if !strings.Contains(out.String(), "Report the product status.") {
		t.Errorf("the page does not carry the command's summary:\n%s", out)
	}
}

// TestHelpForAShellCommandStillRendersItsOwnPage pins that routing product
// namespaces through the help command took nothing from the built-ins.
func TestHelpForAShellCommandStillRendersItsOwnPage(t *testing.T) {
	shell, out, errOut := newShell(t)

	if code := shell.Run([]string{"help", "module"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.Contains(out.String(), "install") {
		t.Errorf("wso2 help module does not describe the module family:\n%s", out)
	}
}

// TestHelpForAnUnknownTopicIsRefusedLikeAnyUnknownCommand pins that the help
// command stopped printing the root page for a topic it does not recognise.
// Exit 0 with the wrong page told a script the typo had worked, which is the
// same silent wrong answer the families refuse in
// TestEveryCommandFamilyRefusesAnUnknownSubcommand.
func TestHelpForAnUnknownTopicIsRefusedLikeAnyUnknownCommand(t *testing.T) {
	stderr := runForStderr(t, []string{"help", "bogus"})

	if !strings.Contains(stderr, "shell.unknown_command") {
		t.Errorf("an unknown help topic is not refused as an unknown command:\n%s", stderr)
	}
}

// TestHelpForAShellCommandRefusesALeftoverWord proves a word Find could not
// place below a shell command is refused rather than shrugged into the
// resolved command's page — wso2 help config bogus must not render config's
// help and report success, for the same reason wso2 help bogus must not.
func TestHelpForAShellCommandRefusesALeftoverWord(t *testing.T) {
	stderr := runForStderr(t, []string{"help", "config", "bogus"})

	if !strings.Contains(stderr, "shell.unknown_command") ||
		!strings.Contains(stderr, `"bogus" is not a wso2 config subcommand`) {
		t.Errorf("a leftover help word is not refused as an unknown subcommand:\n%s", stderr)
	}
}

// TestHelpForAnUnknownProductSubcommandIsRefused proves the walk below a
// namespace refuses a word the module does not serve, exactly as invoking it
// would, rather than shrugging it into the namespace's own page.
func TestHelpForAnUnknownProductSubcommandIsRefused(t *testing.T) {
	shell, _, errOut := newShell(t)
	installFixture(t, shell, fixture.Module{
		Namespace: "reference", Version: "0.1.0", CommandTree: referenceTree(),
	})

	if code := shell.Run([]string{"help", "reference", "bogus"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want the usage class %d; stderr: %s", code, exit.Usage, errOut)
	}
	if !strings.Contains(errOut.String(), "shell.unknown_product_command") {
		t.Errorf("the unknown subcommand is not named:\n%s", errOut)
	}
}

// TestHelpAboutAnUndeclaredModuleIsRefusedTruthfully pins F6's help-command
// half: a module installed from a build that predates command-tree declaration
// has no declaration to answer from, and the shell says that instead of
// launching a process that will call the command unknown.
func TestHelpAboutAnUndeclaredModuleIsRefusedTruthfully(t *testing.T) {
	shell, _, errOut := newShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})

	if code := shell.Run([]string{"help", "reference"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want the usage class %d; stderr: %s", code, exit.Usage, errOut)
	}
	if !strings.Contains(errOut.String(), "shell.module_help_undeclared") {
		t.Errorf("the missing declaration is not named:\n%s", errOut)
	}
	if !strings.Contains(errOut.String(), "wso2 module install reference --channel stable") {
		t.Errorf("the recovery does not point at a build that declares its commands:\n%s", errOut)
	}
}

// TestAHelpFlagOnAnUndeclaredModuleDoesNotLaunchIt pins F6 itself: the
// installed fixture cannot speak the contract, so the usage class — rather
// than the module-process class every launched invocation of it ends in — is
// the proof nothing was launched.
func TestAHelpFlagOnAnUndeclaredModuleDoesNotLaunchIt(t *testing.T) {
	for _, args := range [][]string{
		{"reference", "--help"},
		{"reference", "-h"},
		{"reference", "status", "--help"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			shell, _, errOut := newShell(t)
			installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})

			if code := shell.Run(args); code != exit.Usage {
				t.Fatalf("exit code = %d, want the usage class %d; stderr: %s",
					code, exit.Usage, errOut)
			}
			if !strings.Contains(errOut.String(), "shell.module_help_undeclared") {
				t.Errorf("the refusal does not name the missing declaration:\n%s", errOut)
			}
		})
	}
}

// TestABareUndeclaredNamespaceStillForwardsToTheModule pins the boundary of
// F6's fix: under the old protocol a module may serve its namespace's own bare
// invocation, so only an explicit help flag is caught. The fixture does not
// speak the contract, so reaching the contract failure is the evidence the
// invocation was still forwarded.
func TestABareUndeclaredNamespaceStillForwardsToTheModule(t *testing.T) {
	shell, _, errOut := newShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})

	if code := shell.Run([]string{"reference"}); code != exit.ModuleProcess {
		t.Fatalf("exit code = %d, want the module process class %d; stderr: %s",
			code, exit.ModuleProcess, errOut)
	}
}
