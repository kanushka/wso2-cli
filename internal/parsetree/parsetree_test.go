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

package parsetree_test

import (
	"testing"

	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/parsetree"
	"github.com/wso2/wso2-cli/sdk/commandtree"
)

// tree builds a parseable Tree from a receipt the way the shell would, so
// every test here goes through FromReceipt rather than constructing a Tree
// directly, which is impossible from outside the package.
func tree(commands ...commandtree.Command) parsetree.Tree {
	return parsetree.FromReceipt(modules.Receipt{CommandTree: commandtree.New(commands)})
}

func TestZeroTreeDeclaresNothing(t *testing.T) {
	var zero parsetree.Tree
	if zero.Declared() {
		t.Error("the zero Tree reports Declared")
	}
	if zero.RootHasChildren() {
		t.Error("the zero Tree reports RootHasChildren")
	}
	if commands := zero.Commands(); len(commands) != 0 {
		t.Errorf("Commands = %v, want none", commands)
	}
}

func TestFromReceiptDeclaredWhenTheTreeHasCommands(t *testing.T) {
	got := tree(commandtree.Command{Path: nil}, commandtree.Command{Path: []string{"apps"}})
	if !got.Declared() {
		t.Error("Declared = false for a tree with commands")
	}
}

func TestRootHasChildren(t *testing.T) {
	withChildren := tree(commandtree.Command{Path: nil}, commandtree.Command{Path: []string{"apps"}})
	if !withChildren.RootHasChildren() {
		t.Error("RootHasChildren = false, want true")
	}
	rootOnly := tree(commandtree.Command{Path: nil})
	if rootOnly.RootHasChildren() {
		t.Error("RootHasChildren = true, want false")
	}
}

func TestCommandsHidesHiddenCommands(t *testing.T) {
	got := tree(
		commandtree.Command{Path: nil},
		commandtree.Command{Path: []string{"apps"}},
		commandtree.Command{Path: []string{"secret"}, Hidden: true},
	)
	commands := got.Commands()
	if len(commands) != 2 {
		t.Fatalf("Commands = %d entries, want 2: %v", len(commands), commands)
	}
	for _, command := range commands {
		if command.Hidden {
			t.Errorf("Commands returned a hidden command: %v", command)
		}
	}
}

func TestRouteLandsOnTheDeepestDeclaredCommand(t *testing.T) {
	got := tree(
		commandtree.Command{Path: nil},
		commandtree.Command{Path: []string{"apps"}},
		commandtree.Command{Path: []string{"apps", "list"}},
	)
	routed := got.Route([]string{"apps", "list"})
	if want := []string{"apps", "list"}; !equalStrings(routed.Command.Path, want) {
		t.Errorf("Route landed on %v, want %v", routed.Command.Path, want)
	}
	if routed.Unrouted != "" {
		t.Errorf("Unrouted = %q, want empty", routed.Unrouted)
	}
	if !routed.Path[0] || !routed.Path[1] {
		t.Errorf("Path = %v, want both indices marked", routed.Path)
	}
}

func TestRouteStopsAtTheFirstUnrecognisedWord(t *testing.T) {
	got := tree(
		commandtree.Command{Path: nil},
		commandtree.Command{Path: []string{"apps"}},
	)
	routed := got.Route([]string{"apps", "myapp"})
	if want := []string{"apps"}; !equalStrings(routed.Command.Path, want) {
		t.Errorf("Route landed on %v, want %v", routed.Command.Path, want)
	}
	if routed.Unrouted != "myapp" {
		t.Errorf("Unrouted = %q, want %q", routed.Unrouted, "myapp")
	}
}

func TestRouteWithNoCommandDeclaredStillRoutesFromAnEmptyRoot(t *testing.T) {
	var zero parsetree.Tree
	routed := zero.Route([]string{"anything"})
	if len(routed.Command.Path) != 0 {
		t.Errorf("Command.Path = %v, want empty", routed.Command.Path)
	}
	if routed.Unrouted != "anything" {
		t.Errorf("Unrouted = %q, want %q", routed.Unrouted, "anything")
	}
}

func TestRouteStepsOverAnUndeclaredLongFlagsValue(t *testing.T) {
	got := tree(
		commandtree.Command{Path: nil},
		commandtree.Command{Path: []string{"status"}},
	)
	// "--since" is not declared on the root, so its value (1h) is stepped
	// over rather than mistaken for a command name, and "status" is still
	// reached.
	routed := got.Route([]string{"--since", "1h", "status"})
	if want := []string{"status"}; !equalStrings(routed.Command.Path, want) {
		t.Errorf("Route landed on %v, want %v", routed.Command.Path, want)
	}
}

func TestRouteDoesNotStepOverADeclaredBooleanFlagsValue(t *testing.T) {
	got := tree(
		commandtree.Command{Path: nil, Flags: []commandtree.Flag{
			{Name: "all", Type: commandtree.TypeBool},
		}},
		commandtree.Command{Path: []string{"list"}},
	)
	// --all is boolean, so it takes no value and "list" right after it is a
	// command, not a value being skipped.
	routed := got.Route([]string{"--all", "list"})
	if want := []string{"list"}; !equalStrings(routed.Command.Path, want) {
		t.Errorf("Route landed on %v, want %v", routed.Command.Path, want)
	}
}

func TestRouteTreatsALongFlagWithAttachedValueAsNotConsumingTheNextWord(t *testing.T) {
	got := tree(
		commandtree.Command{Path: nil},
		commandtree.Command{Path: []string{"status"}},
	)
	routed := got.Route([]string{"--region=us-east", "status"})
	if want := []string{"status"}; !equalStrings(routed.Command.Path, want) {
		t.Errorf("Route landed on %v, want %v", routed.Command.Path, want)
	}
}

func TestRouteStopsAtADoubleDash(t *testing.T) {
	got := tree(
		commandtree.Command{Path: nil},
		commandtree.Command{Path: []string{"apps"}},
	)
	routed := got.Route([]string{"--", "apps"})
	if len(routed.Command.Path) != 0 {
		t.Errorf("Command.Path = %v, want empty (stopped at --)", routed.Command.Path)
	}
	if routed.Unrouted != "" {
		t.Errorf("Unrouted = %q, want empty", routed.Unrouted)
	}
}

func TestRouteStopsAtAnUnknownLoneDashFlag(t *testing.T) {
	got := tree(
		commandtree.Command{Path: nil},
		commandtree.Command{Path: []string{"apps"}},
	)
	// A single "-" is not a shorthand run (len(argument) > 1 required), so
	// it falls to the default case and is treated as a plain word.
	routed := got.Route([]string{"-"})
	if routed.Unrouted != "-" {
		t.Errorf("Unrouted = %q, want %q", routed.Unrouted, "-")
	}
}

func TestRouteShorthandRunEndingInAValueFlagStepsOverTheNextWord(t *testing.T) {
	got := tree(
		commandtree.Command{Path: nil, Flags: []commandtree.Flag{
			{Name: "help", Shorthand: "h", Type: commandtree.TypeBool},
			{Name: "region", Shorthand: "r", Type: "string"},
		}},
		commandtree.Command{Path: []string{"status"}},
	)
	// -hr is help (bool) then region (takes a value): the run ends in a
	// value flag, so the next word is its value, not a command.
	routed := got.Route([]string{"-hr", "us-east", "status"})
	if want := []string{"status"}; !equalStrings(routed.Command.Path, want) {
		t.Errorf("Route landed on %v, want %v", routed.Command.Path, want)
	}
}

func TestRouteShorthandRunNotEndingInAValueFlagDoesNotStepOver(t *testing.T) {
	got := tree(
		commandtree.Command{Path: nil, Flags: []commandtree.Flag{
			{Name: "help", Shorthand: "h", Type: commandtree.TypeBool},
			{Name: "all", Shorthand: "a", Type: commandtree.TypeBool},
		}},
		commandtree.Command{Path: []string{"status"}},
	)
	routed := got.Route([]string{"-ha", "status"})
	if want := []string{"status"}; !equalStrings(routed.Command.Path, want) {
		t.Errorf("Route landed on %v, want %v", routed.Command.Path, want)
	}
}

func TestRouteUnknownShorthandLetterIsAssumedToTakeAValue(t *testing.T) {
	got := tree(
		commandtree.Command{Path: nil},
		commandtree.Command{Path: []string{"status"}},
	)
	// -z is not declared anywhere, so it is assumed to take a value and its
	// argument (foo) is skipped over rather than mistaken for a command.
	routed := got.Route([]string{"-z", "foo", "status"})
	if want := []string{"status"}; !equalStrings(routed.Command.Path, want) {
		t.Errorf("Route landed on %v, want %v", routed.Command.Path, want)
	}
}

func TestPendingValueEmptyArgs(t *testing.T) {
	got := tree(commandtree.Command{Path: nil})
	if name, pending := got.PendingValue(nil); pending || name != "" {
		t.Errorf("PendingValue(nil) = %q, %v; want \"\", false", name, pending)
	}
}

func TestPendingValueLastWordIsNotAFlag(t *testing.T) {
	got := tree(commandtree.Command{Path: nil})
	cases := [][]string{
		{"status"},
		{"--"},
		{"-"},
		{"--region=us-east"},
	}
	for _, args := range cases {
		if name, pending := got.PendingValue(args); pending {
			t.Errorf("PendingValue(%v) = %q, true; want false", args, name)
		}
	}
}

func TestPendingValueDeclaredLongFlag(t *testing.T) {
	got := tree(commandtree.Command{Path: nil, Flags: []commandtree.Flag{
		{Name: "region", Type: "string"},
		{Name: "all", Type: commandtree.TypeBool},
	}})
	if name, pending := got.PendingValue([]string{"--region"}); !pending || name != "region" {
		t.Errorf("PendingValue(--region) = %q, %v; want region, true", name, pending)
	}
	if name, pending := got.PendingValue([]string{"--all"}); pending {
		t.Errorf("PendingValue(--all) = %q, %v; want false (boolean takes no value)", name, pending)
	}
}

func TestPendingValueUndeclaredLongFlagIsAssumedToTakeAValue(t *testing.T) {
	got := tree(commandtree.Command{Path: nil})
	name, pending := got.PendingValue([]string{"--since"})
	if !pending || name != "since" {
		t.Errorf("PendingValue(--since) = %q, %v; want since, true", name, pending)
	}
}

func TestPendingValueFollowsRouteToTheCommandBeforeTheFlag(t *testing.T) {
	got := tree(
		commandtree.Command{Path: nil},
		commandtree.Command{Path: []string{"apps"}, Flags: []commandtree.Flag{
			{Name: "region", Type: "string"},
		}},
	)
	// region is declared on "apps", not the root, so PendingValue only sees
	// it as taking a value once routed there.
	name, pending := got.PendingValue([]string{"apps", "--region"})
	if !pending || name != "region" {
		t.Errorf("PendingValue(apps --region) = %q, %v; want region, true", name, pending)
	}
}

func TestPendingValueDeclaredShorthand(t *testing.T) {
	got := tree(commandtree.Command{Path: nil, Flags: []commandtree.Flag{
		{Name: "region", Shorthand: "r", Type: "string"},
		{Name: "help", Shorthand: "h", Type: commandtree.TypeBool},
	}})
	name, pending := got.PendingValue([]string{"-hr"})
	if !pending || name != "region" {
		t.Errorf("PendingValue(-hr) = %q, %v; want region, true", name, pending)
	}
	// A run ending in a boolean is not pending a value.
	if name, pending := got.PendingValue([]string{"-h"}); pending {
		t.Errorf("PendingValue(-h) = %q, %v; want false", name, pending)
	}
}

func TestPendingValueUndeclaredShorthandIsAssumedToTakeAValue(t *testing.T) {
	got := tree(commandtree.Command{Path: nil})
	name, pending := got.PendingValue([]string{"-z"})
	if !pending || name != "z" {
		t.Errorf("PendingValue(-z) = %q, %v; want z, true", name, pending)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}
