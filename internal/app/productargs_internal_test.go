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

package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/internal/parsetree"
	"github.com/wso2/wso2-cli/sdk/commandtree"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// declaring builds the parseable tree a module with these commands would
// install. It goes through the receipt because that is the only door: a tree
// the shell parses with is one that came off a verified installation.
func declaring(commands ...commandtree.Command) parsetree.Tree {
	return parsetree.FromReceipt(modules.Receipt{CommandTree: commandtree.New(commands)})
}

// referenceTree is a module declaring the shapes that matter: a root with a
// flag of its own, a command taking a value flag, a boolean, a shorthand pair,
// a group with a child, and an alias.
func referenceTree() parsetree.Tree {
	return declaring(
		commandtree.Command{Path: nil, Runnable: true, Flags: []commandtree.Flag{
			{Name: "verbose", Shorthand: "a", Type: commandtree.TypeBool},
		}},
		commandtree.Command{Path: []string{"status"}, Runnable: true, Flags: []commandtree.Flag{
			{Name: "since", Type: "string"},
			{Name: "all", Shorthand: "a", Type: commandtree.TypeBool},
			{Name: "region", Shorthand: "r", Type: "string"},
			{Name: "verbose", Type: commandtree.TypeBool},
			{Name: "help", Shorthand: "h", Type: commandtree.TypeBool},
		}},
		commandtree.Command{Path: []string{"apps"}, Runnable: true, Aliases: []string{"a"}},
		commandtree.Command{Path: []string{"apps", "list"}, Runnable: true, Aliases: []string{"ls"}},
	)
}

// parse reads a product line against the reference tree, failing the test if it
// is refused.
func parse(t *testing.T, tree parsetree.Tree, args ...string) productLine {
	t.Helper()
	line, err := parseProductArgs("reference", tree, args)
	if err != nil {
		t.Fatalf("parsing %q: %v", args, err)
	}
	return line
}

// refusal reads a product line expecting a typed refusal.
func refusal(t *testing.T, tree parsetree.Tree, args ...string) problem.Problem {
	t.Helper()
	_, err := parseProductArgs("reference", tree, args)
	var reported problem.Problem
	if !errors.As(err, &reported) {
		t.Fatalf("parsing %q returned %v, want a typed problem", args, err)
	}
	return reported
}

// TestTheOutputFlagMeansTheSameWhereverItIsWritten is the defect this whole
// mechanism exists to remove, and the case nothing pinned before.
//
// Without a declaration the shell stopped reading at the first flag it did not
// recognise and handed the rest to the module, so "--output json" written after
// a product flag was forwarded rather than read, and the user got a table with
// no indication that the flag they typed had been ignored. Knowing that --since
// takes a value and where it ends, the shell reads the whole line.
func TestTheOutputFlagMeansTheSameWhereverItIsWritten(t *testing.T) {
	lines := map[string][]string{
		"before the product flag":  {"status", "--output", "json", "--since", "1h"},
		"after the product flag":   {"status", "--since", "1h", "--output", "json"},
		"before the command":       {"--output", "json", "status", "--since", "1h"},
		"joined by an equals sign": {"status", "--since", "1h", "--output=json"},
		"after a boolean flag":     {"status", "--all", "--output", "json"},
	}

	for name, args := range lines {
		t.Run(name, func(t *testing.T) {
			line := parse(t, referenceTree(), args...)
			if line.mode != output.ModeJSON {
				t.Errorf("parsing %q rendered %s, not json", args, line.mode)
			}
			if joined := strings.Join(line.arguments, " "); strings.Contains(joined, "--output") {
				t.Errorf("the shell forwarded its own flag to the module as %q", joined)
			}
		})
	}
}

// TestAValueFlagKeepsItsValueAndABooleanDoesNot proves the one thing the parser
// reads a flag's type for. Treating --all as if it took a value would swallow
// whatever followed, and treating --since as if it did not would leave its value
// looking like a command argument.
func TestAValueFlagKeepsItsValueAndABooleanDoesNot(t *testing.T) {
	line := parse(t, referenceTree(), "status", "--all", "--since", "1h", "--output", "json")

	if strings.Join(line.command, " ") != "status" {
		t.Errorf("the command is %q", line.command)
	}
	if joined := strings.Join(line.arguments, " "); joined != "--all --since 1h" {
		t.Errorf("the module receives %q", joined)
	}
	if line.mode != output.ModeJSON {
		t.Errorf("the output mode is %s", line.mode)
	}
}

// TestAFlagTheCommandDoesNotDeclareIsNamed proves the shell now refuses what it
// used to forward. A mistyped product flag reached the module, which reported it
// in its own words at whatever point it noticed; the shell can now say so before
// anything is launched.
func TestAFlagTheCommandDoesNotDeclareIsNamed(t *testing.T) {
	reported := refusal(t, referenceTree(), "status", "--sinces", "1h")

	if reported.Code != "shell.unknown_product_flag" {
		t.Errorf("the refusal is coded %q", reported.Code)
	}
	if !strings.Contains(reported.Message, "--sinces") {
		t.Errorf("the refusal does not name the flag: %q", reported.Message)
	}
	if !strings.Contains(reported.Recovery, "wso2 reference status --help") {
		t.Errorf("the recovery is %q", reported.Recovery)
	}
}

// TestAMistypedProductCommandIsNamedAndASuggestionOffered proves the shell can
// now answer for a module's commands. Cobra's suggestions never covered a
// product namespace, because the shell did not know what was in one.
func TestAMistypedProductCommandIsNamedAndASuggestionOffered(t *testing.T) {
	reported := refusal(t, referenceTree(), "stats")

	if reported.Code != "shell.unknown_product_command" {
		t.Errorf("the refusal is coded %q", reported.Code)
	}
	if reported.Recovery != "Did you mean wso2 reference status?" {
		t.Errorf("the recovery is %q", reported.Recovery)
	}
}

// TestAWordUnderAGroupIsThatGroupsArgument pins fidelity to Cobra at a place
// where being helpful would be wrong.
//
// Cobra refuses a plain word that names no subcommand only at a command's root;
// anywhere below it the word is that command's own argument, and its Find and
// ValidateArgs were run to confirm it. So the shell reports "stats" at the
// namespace root and stays out of the way under "apps", because refusing there
// would refuse a line the module itself would have accepted — the same class of
// disagreement, pointing the other way, that declaring a tree exists to end.
func TestAWordUnderAGroupIsThatGroupsArgument(t *testing.T) {
	line := parse(t, referenceTree(), "apps", "myapp")

	if strings.Join(line.command, " ") != "apps" {
		t.Errorf("the command is %q", line.command)
	}
	if strings.Join(line.arguments, " ") != "myapp" {
		t.Errorf("the module receives %q", line.arguments)
	}
}

// TestACommandIsFoundPastFlagsWrittenBeforeIt is the routing half of the defect.
//
// Cobra locates a subcommand before it parses anything, stepping over flags and
// over the values it assumes unknown flags carry. A shell that stopped at the
// first flag would land on the namespace root instead: "--verbose status" would
// run the root with "status" as an argument, silently, which is the same
// "position changes meaning" failure the output flag had. Verified against
// Cobra's own Find for each of these lines.
func TestACommandIsFoundPastFlagsWrittenBeforeIt(t *testing.T) {
	cases := map[string]struct {
		args      []string
		command   string
		arguments string
	}{
		"past a flag the root declares": {
			args: []string{"--verbose", "status"}, command: "status", arguments: "--verbose",
		},
		"past a flag only the subcommand declares": {
			args: []string{"--since", "1h", "status"}, command: "status", arguments: "--since 1h",
		},
		"past a shorthand the root declares": {
			args: []string{"-a", "status"}, command: "status", arguments: "-a",
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			line := parse(t, referenceTree(), testCase.args...)
			if got := strings.Join(line.command, " "); got != testCase.command {
				t.Errorf("parsing %q reached %q, want %q", testCase.args, got, testCase.command)
			}
			if joined := strings.Join(line.arguments, " "); joined != testCase.arguments {
				t.Errorf("parsing %q forwarded %q, want %q", testCase.args, joined, testCase.arguments)
			}
		})
	}
}

// TestAnAliasReachesTheModuleUnderTheNameItServes proves the canonical path is
// what travels, at every level.
//
// The module binds its handler to the name, not the alias: cobratree builds
// each served path from the command's own Name, and the SDK matches an incoming
// path exactly. Forwarding "apps ls" would reach a module that serves
// "apps list" and be reported as an unknown command.
func TestAnAliasReachesTheModuleUnderTheNameItServes(t *testing.T) {
	for _, args := range [][]string{{"apps", "ls"}, {"a", "list"}, {"a", "ls"}} {
		line := parse(t, referenceTree(), args...)
		if strings.Join(line.command, " ") != "apps list" {
			t.Errorf("%q resolved to %q, want [apps list]", args, line.command)
		}
	}
}

// TestShorthandFlagsReadTheWayPflagReadsThem proves a run of single letters is
// split as the module would split it. A shell that read "-ar eu" differently
// from the module it forwards to would send a value the module never saw.
func TestShorthandFlagsReadTheWayPflagReadsThem(t *testing.T) {
	cases := map[string]struct {
		args      []string
		arguments string
	}{
		"a boolean alone":              {args: []string{"status", "-a"}, arguments: "-a"},
		"a value flag with its value":  {args: []string{"status", "-r", "eu"}, arguments: "-r eu"},
		"a value attached to the run":  {args: []string{"status", "-areu"}, arguments: "-areu"},
		"a run ending in a value flag": {args: []string{"status", "-ar", "eu"}, arguments: "-ar eu"},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			line := parse(t, referenceTree(), testCase.args...)
			if joined := strings.Join(line.arguments, " "); joined != testCase.arguments {
				t.Errorf("parsing %q forwarded %q, want %q", testCase.args, joined, testCase.arguments)
			}
		})
	}
}

// TestTheSeparatorHandsEverythingAfterItToTheModule proves a command can still
// take an argument that looks like a flag, including one of the shell's own.
func TestTheSeparatorHandsEverythingAfterItToTheModule(t *testing.T) {
	line := parse(t, referenceTree(), "status", "--", "--output", "json")

	if joined := strings.Join(line.arguments, " "); joined != "-- --output json" {
		t.Errorf("the module receives %q", joined)
	}
	if line.mode != output.ModeTable {
		t.Errorf("the shell read a flag written after the separator: %s", line.mode)
	}
}

// TestAValueFlagWithNoValueIsRefusedRatherThanForwarded proves the shell reports
// the truncated line itself, instead of forwarding it for the module to reject
// after the process has been launched and access brokered.
func TestAValueFlagWithNoValueIsRefusedRatherThanForwarded(t *testing.T) {
	if code := refusal(t, referenceTree(), "status", "--since").Code; code != "shell.missing_flag_value" {
		t.Errorf("the refusal is coded %q", code)
	}
}

// TestACommandsOwnArgumentsStillReachIt proves declaring flags did not make
// positional arguments disappear.
func TestACommandsOwnArgumentsStillReachIt(t *testing.T) {
	tree := declaring(
		commandtree.Command{Path: nil},
		commandtree.Command{Path: []string{"deploy"}, Runnable: true,
			Flags: []commandtree.Flag{{Name: "force", Type: commandtree.TypeBool}}})

	line := parse(t, tree, "deploy", "app.zip", "--force")

	if strings.Join(line.command, " ") != "deploy" {
		t.Errorf("the command is %q", line.command)
	}
	if joined := strings.Join(line.arguments, " "); joined != "app.zip --force" {
		t.Errorf("the module receives %q", joined)
	}
}

// TestAShellFlagWhereAModuleFlagsValueBelongsIsThatValue pins the rule at the
// one place the shell's claim on its own flags stops.
//
// A module flag that takes a value takes the next argument as that value, and
// pflag does not exempt something that looks like another flag — verified
// against pflag itself, not assumed. Reading "--output" as the shell's here
// would part company with the module the line is being forwarded to, which is
// the disagreement declaring a tree exists to end. Anyone who meant the shell's
// flag has "--" to say so.
func TestAShellFlagWhereAModuleFlagsValueBelongsIsThatValue(t *testing.T) {
	lines := map[string][]string{
		"as a shorthand run's value": {"status", "-ar", "--output", "json"},
		"as a long flag's value":     {"status", "--region", "--output", "json"},
	}

	for name, args := range lines {
		t.Run(name, func(t *testing.T) {
			line := parse(t, referenceTree(), args...)
			if line.mode != output.ModeTable {
				t.Errorf("the shell claimed a flag standing in a module flag's value: %s", line.mode)
			}
			if joined := strings.Join(line.arguments, " "); joined != strings.Join(args[1:], " ") {
				t.Errorf("the module receives %q, want %q", joined, strings.Join(args[1:], " "))
			}
		})
	}
}

// TestAskingForHelpIsNotAskingToRun proves the shell recognises a request for
// help rather than reporting --help as a flag the command does not take, and
// that a flag which takes a value still claims the word after it.
func TestAskingForHelpIsNotAskingToRun(t *testing.T) {
	if !parse(t, referenceTree(), "status", "--help").help {
		t.Error("--help was not read as a request for help")
	}
	if !parse(t, referenceTree(), "status", "-h").help {
		t.Error("-h was not read as a request for help")
	}
	if parse(t, referenceTree(), "status").help {
		t.Error("a line with no help flag asked for help")
	}
	// --since takes a value, so the word after it is that value, which is how
	// pflag reads the same line.
	if parse(t, referenceTree(), "status", "--since", "--help").help {
		t.Error("a value belonging to --since was read as a request for help")
	}
	if parse(t, referenceTree(), "status", "--", "--help").help {
		t.Error("a word after the separator was read as a request for help")
	}
	// pflag reads an explicit false as false, and Cobra shows no help for it.
	if parse(t, referenceTree(), "status", "--help=false").help {
		t.Error("--help=false was read as a request for help")
	}
	if parse(t, referenceTree(), "status", "-h=false").help {
		t.Error("-h=false was read as a request for help")
	}
	if !parse(t, referenceTree(), "status", "--help=true").help {
		t.Error("--help=true was not read as a request for help")
	}
}

// TestAnExplicitBooleanValueIsThatFlagsValue proves an attached value is read as
// the flag's own, in both spellings, rather than as more shorthand letters.
// pflag accepts "-a=false"; a parser that treated "=" as another letter would
// refuse a line the module accepts.
func TestAnExplicitBooleanValueIsThatFlagsValue(t *testing.T) {
	for _, args := range [][]string{
		{"status", "--all=false"}, {"status", "-a=false"}, {"status", "-a=true"},
	} {
		line := parse(t, referenceTree(), args...)
		if joined := strings.Join(line.arguments, " "); joined != args[1] {
			t.Errorf("%q forwarded %q, want %q", args, joined, args[1])
		}
	}
}

// TestACounterTakesNoValueAlthoughItIsNotBoolean proves the parser reads pflag's
// no-value property rather than the flag's type. A counter's type is "count",
// and pflag still leaves the next word alone — verified against pflag, where
// "--verbose status" leaves status unconsumed. Treating anything non-boolean as
// taking a value would swallow the command name.
func TestACounterTakesNoValueAlthoughItIsNotBoolean(t *testing.T) {
	counter := []commandtree.Flag{
		{Name: "loud", Shorthand: "L", Type: "count", NoOptDefault: "+1"},
	}
	tree := declaring(
		commandtree.Command{Path: nil, Flags: counter},
		commandtree.Command{Path: []string{"status"}, Runnable: true, Flags: counter},
	)

	line := parse(t, tree, "--loud", "status")

	if strings.Join(line.command, " ") != "status" {
		t.Errorf("the counter swallowed the command name; reached %q", line.command)
	}
	if strings.Join(line.arguments, " ") != "--loud" {
		t.Errorf("the module receives %q", line.arguments)
	}
}

// TestTheShellsOwnShorthandInsideAModuleRunIsExplained proves the refusal tells
// the truth about whose flag it is.
//
// "-ao json" joins the shell's -o to a module's -a, and a run belongs to one
// flag set. Reporting it as a flag the command does not take would be false —
// "-o json" beside it works — and would send the reader to the module's
// documentation looking for the shell's flag.
func TestTheShellsOwnShorthandInsideAModuleRunIsExplained(t *testing.T) {
	reported := refusal(t, referenceTree(), "status", "-ao", "json")

	if reported.Code != "shell.shell_flag_in_a_product_run" {
		t.Errorf("the refusal is coded %q", reported.Code)
	}
	if !strings.Contains(reported.Message, "the shell's own flag") {
		t.Errorf("the refusal does not say whose flag it is: %q", reported.Message)
	}
	if !strings.Contains(reported.Recovery, "on its own") {
		t.Errorf("the recovery does not say how to write it: %q", reported.Recovery)
	}
	// Written on its own it is the shell's, and reaches the shell.
	if line := parse(t, referenceTree(), "status", "-a", "-o", "json"); line.mode != output.ModeJSON {
		t.Errorf("the same flag written separately rendered %s", line.mode)
	}
}
