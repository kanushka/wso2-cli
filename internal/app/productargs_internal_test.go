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

// referenceTree is a module declaring the shapes that matter: a command taking
// a value flag, a boolean, a shorthand pair, a group with a child, and an alias.
func referenceTree() parsetree.Tree {
	return declaring(
		commandtree.Command{Path: nil, Runnable: true},
		commandtree.Command{Path: []string{"status"}, Runnable: true, Flags: []commandtree.Flag{
			{Name: "since", Type: commandtree.TypeString},
			{Name: "all", Shorthand: "a", Type: commandtree.TypeBool},
			{Name: "region", Shorthand: "r", Type: commandtree.TypeString},
		}},
		commandtree.Command{Path: []string{"apps"}},
		commandtree.Command{Path: []string{"apps", "list"}, Runnable: true},
		commandtree.Command{Path: []string{"apps", "ls"}, Runnable: true, Hidden: true},
	)
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
			_, arguments, mode, _, err := parseProductArgs("reference", referenceTree(), args)
			if err != nil {
				t.Fatalf("parsing %q: %v", args, err)
			}
			if mode != output.ModeJSON {
				t.Errorf("parsing %q rendered %s, not json", args, mode)
			}
			if joined := strings.Join(arguments, " "); strings.Contains(joined, "--output") {
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
	command, arguments, mode, _, err := parseProductArgs("reference", referenceTree(),
		[]string{"status", "--all", "--since", "1h", "--output", "json"})
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if strings.Join(command, " ") != "status" {
		t.Errorf("the command is %q", command)
	}
	if joined := strings.Join(arguments, " "); joined != "--all --since 1h" {
		t.Errorf("the module receives %q", joined)
	}
	if mode != output.ModeJSON {
		t.Errorf("the output mode is %s", mode)
	}
}

// TestAFlagTheCommandDoesNotDeclareIsNamed proves the shell now refuses what it
// used to forward. A mistyped product flag reached the module, which reported it
// in its own words at whatever point it noticed; the shell can now say so before
// anything is launched.
func TestAFlagTheCommandDoesNotDeclareIsNamed(t *testing.T) {
	_, _, _, _, err := parseProductArgs("reference", referenceTree(),
		[]string{"status", "--sinces", "1h"})

	var reported problem.Problem
	if !errors.As(err, &reported) {
		t.Fatalf("parsing an undeclared flag returned %v", err)
	}
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
	_, _, _, _, err := parseProductArgs("reference", referenceTree(), []string{"stats"})

	var reported problem.Problem
	if !errors.As(err, &reported) {
		t.Fatalf("parsing a mistyped command returned %v", err)
	}
	if reported.Code != "shell.unknown_product_command" {
		t.Errorf("the refusal is coded %q", reported.Code)
	}
	if reported.Recovery != "Did you mean wso2 reference status?" {
		t.Errorf("the recovery is %q", reported.Recovery)
	}
}

// TestAMistypedSubcommandUnderAGroupIsNamed proves the refusal reaches inside
// the tree rather than only its top level, and that a group's own child is what
// gets suggested.
func TestAMistypedSubcommandUnderAGroupIsNamed(t *testing.T) {
	_, _, _, _, err := parseProductArgs("reference", referenceTree(), []string{"apps", "lst"})

	var reported problem.Problem
	if !errors.As(err, &reported) {
		t.Fatalf("parsing returned %v", err)
	}
	if reported.Recovery != "Did you mean wso2 reference apps list?" {
		t.Errorf("the recovery is %q", reported.Recovery)
	}
}

// TestAnAliasReachesTheModuleUnderTheNameItServes proves the resolved path is
// what travels, not the words that were typed. The module binds its handler to
// one path, so forwarding the alias would be a command it does not serve.
func TestAnAliasReachesTheModuleUnderTheNameItServes(t *testing.T) {
	command, _, _, _, err := parseProductArgs("reference", referenceTree(), []string{"apps", "ls"})
	if err != nil {
		t.Fatalf("parsing an alias: %v", err)
	}
	if strings.Join(command, " ") != "apps ls" {
		t.Errorf("the alias resolved to %q", command)
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
			_, arguments, _, _, err := parseProductArgs("reference", referenceTree(), testCase.args)
			if err != nil {
				t.Fatalf("parsing %q: %v", testCase.args, err)
			}
			if joined := strings.Join(arguments, " "); joined != testCase.arguments {
				t.Errorf("parsing %q forwarded %q, want %q", testCase.args, joined, testCase.arguments)
			}
		})
	}
}

// TestTheSeparatorHandsEverythingAfterItToTheModule proves a command can still
// take an argument that looks like a flag, including one of the shell's own.
func TestTheSeparatorHandsEverythingAfterItToTheModule(t *testing.T) {
	_, arguments, mode, _, err := parseProductArgs("reference", referenceTree(),
		[]string{"status", "--", "--output", "json"})
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if joined := strings.Join(arguments, " "); joined != "-- --output json" {
		t.Errorf("the module receives %q", joined)
	}
	if mode != output.ModeTable {
		t.Errorf("the shell read a flag written after the separator: %s", mode)
	}
}

// TestAValueFlagWithNoValueIsRefusedRatherThanForwarded proves the shell reports
// the truncated line itself, instead of forwarding it for the module to reject
// after the process has been launched and access brokered.
func TestAValueFlagWithNoValueIsRefusedRatherThanForwarded(t *testing.T) {
	_, _, _, _, err := parseProductArgs("reference", referenceTree(), []string{"status", "--since"})

	var reported problem.Problem
	if !errors.As(err, &reported) {
		t.Fatalf("parsing returned %v", err)
	}
	if reported.Code != "shell.missing_flag_value" {
		t.Errorf("the refusal is coded %q", reported.Code)
	}
}

// TestACommandsOwnArgumentsStillReachIt proves declaring flags did not make
// positional arguments disappear.
func TestACommandsOwnArgumentsStillReachIt(t *testing.T) {
	tree := declaring(commandtree.Command{Path: []string{"deploy"}, Runnable: true,
		Flags: []commandtree.Flag{{Name: "force", Type: commandtree.TypeBool}}})

	command, arguments, _, _, err := parseProductArgs("reference", tree,
		[]string{"deploy", "app.zip", "--force"})
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if strings.Join(command, " ") != "deploy" {
		t.Errorf("the command is %q", command)
	}
	if joined := strings.Join(arguments, " "); joined != "app.zip --force" {
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
			_, arguments, mode, _, err := parseProductArgs("reference", referenceTree(), args)
			if err != nil {
				t.Fatalf("parsing %q: %v", args, err)
			}
			if mode != output.ModeTable {
				t.Errorf("the shell claimed a flag standing in a module flag's value: %s", mode)
			}
			if joined := strings.Join(arguments, " "); joined != strings.Join(args[1:], " ") {
				t.Errorf("the module receives %q, want %q", joined, strings.Join(args[1:], " "))
			}
		})
	}
}
