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

package cobratree_test

import (
	"context"
	"slices"
	"testing"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/sdk/cobratree"
	"github.com/wso2/wso2-cli/sdk/commandtree"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/result"
)

// TestDeclareIncludesACommandThatOnlyGroupsOthers proves a command with no
// handler still reaches the declaration. Commands() drops it, because a group
// cannot be served; the declaration must keep it, because the shell has to parse
// the path a user types on the way to a command that can, and has to reach the
// group's own help.
func TestDeclareIncludesACommandThatOnlyGroupsOthers(t *testing.T) {
	root := &cobra.Command{Use: "reference"}
	apps := &cobra.Command{Use: "apps", Short: "Work with applications."}
	list := &cobra.Command{Use: "list"}
	root.AddCommand(apps)
	apps.AddCommand(list)

	declared := cobratree.New(root).Handle(list, noop).Declare()

	group, ok := findDeclared(declared, "apps")
	if !ok {
		t.Fatalf("the group command is absent from %+v", declared.Commands)
	}
	if group.Runnable {
		t.Error("a command with no handler is declared runnable")
	}
	if group.Short != "Work with applications." {
		t.Errorf("the group's description is %q", group.Short)
	}
	leaf, ok := findDeclared(declared, "apps", "list")
	if !ok {
		t.Fatal("the handled command is absent")
	}
	if !leaf.Runnable {
		t.Error("a command with a handler is not declared runnable")
	}
}

// TestDeclareFlattensInheritedFlagsOntoEveryCommand proves a persistent flag
// declared on a parent is answerable from the child alone. The shell asks one
// question — does this command take this flag — and walking back up the tree to
// answer it is how a parser ends up disagreeing with the module about a flag it
// inherited.
func TestDeclareFlattensInheritedFlagsOntoEveryCommand(t *testing.T) {
	root := &cobra.Command{Use: "reference"}
	root.PersistentFlags().String("region", "", "The region to act in.")
	apps := &cobra.Command{Use: "apps"}
	list := &cobra.Command{Use: "list"}
	list.Flags().BoolP("all", "a", false, "Include every application.")
	root.AddCommand(apps)
	apps.AddCommand(list)

	declared := cobratree.New(root).Handle(list, noop).Declare()

	leaf, ok := findDeclared(declared, "apps", "list")
	if !ok {
		t.Fatal("the handled command is absent")
	}
	region, ok := leaf.LookupFlag("region")
	if !ok {
		t.Fatalf("the inherited flag is absent from %+v", leaf.Flags)
	}
	if !region.TakesValue() || region.Type != "string" {
		t.Errorf("the inherited flag is declared as %+v", region)
	}
	all, ok := leaf.LookupFlag("all")
	if !ok {
		t.Fatal("the command's own flag is absent")
	}
	if all.TakesValue() {
		t.Error("a boolean flag is declared as taking a value")
	}
	if all.Shorthand != "a" {
		t.Errorf("the shorthand is %q, want \"a\"", all.Shorthand)
	}
	if all.Usage != "Include every application." {
		t.Errorf("the usage is %q", all.Usage)
	}
}

// TestDeclareGivesAnAliasItsOwnPath proves a command reachable under a second
// name parses under it too. A product CLI being migrated brings its aliases
// with it, and a declaration that omitted them would have the shell refuse a
// spelling the module itself accepts — a regression caused by declaring.
func TestDeclareGivesAnAliasItsOwnPath(t *testing.T) {
	root := &cobra.Command{Use: "reference"}
	list := &cobra.Command{Use: "list", Aliases: []string{"ls"}}
	root.AddCommand(list)

	declared := cobratree.New(root).Handle(list, noop).Declare()

	alias, ok := findDeclared(declared, "ls")
	if !ok {
		t.Fatalf("the alias has no path in %+v", declared.Commands)
	}
	if !alias.Runnable {
		t.Error("the alias is not runnable while the command it names is")
	}
	if !alias.Hidden {
		t.Error("the alias is not hidden, so it would be offered as a suggestion")
	}
	canonical, _ := findDeclared(declared, "list")
	if canonical.Hidden {
		t.Error("the command's own name is hidden")
	}
}

// TestDeclareGivesTheRootAnEmptyPath proves the namespace's own command is the
// empty path rather than its Use string, matching the path the shell sends and
// the path Commands() emits.
func TestDeclareGivesTheRootAnEmptyPath(t *testing.T) {
	root := &cobra.Command{Use: "reference", Short: "The reference module."}

	declared := cobratree.New(root).Handle(root, noop).Declare()

	if len(declared.Commands) != 1 {
		t.Fatalf("declared %d commands, want 1: %+v", len(declared.Commands), declared.Commands)
	}
	if len(declared.Commands[0].Path) != 0 {
		t.Errorf("the root's path is %q, want empty", declared.Commands[0].Path)
	}
}

// TestDeclareMatchesTheCommandsServed proves the declaration and what Serve
// answers cannot disagree about which paths run. A path that runs but is not
// declared would be refused by the shell before the module saw it; a path
// declared runnable that does not run would be forwarded and then rejected.
func TestDeclareMatchesTheCommandsServed(t *testing.T) {
	root := &cobra.Command{Use: "reference"}
	apps := &cobra.Command{Use: "apps"}
	list := &cobra.Command{Use: "list"}
	root.AddCommand(apps)
	apps.AddCommand(list)
	tree := cobratree.New(root).Handle(list, noop)

	declared := tree.Declare()
	served := tree.Commands()

	var runnable [][]string
	for _, command := range declared.Commands {
		if command.Runnable && !command.Hidden {
			runnable = append(runnable, command.Path)
		}
	}
	if len(runnable) != len(served) {
		t.Fatalf("declared %d runnable commands but serves %d", len(runnable), len(served))
	}
	for _, command := range served {
		if !slices.ContainsFunc(runnable, func(path []string) bool {
			return slices.Equal(path, command.Path)
		}) {
			t.Errorf("the served command %q is not declared runnable", command.Path)
		}
	}
}

// TestDeclareDoesNotDependOnTheOrderCommandsWereAdded proves the declaration is
// canonical. It is written into a receipt and published in a catalog, and the
// two are compared, so the same tree has to declare identically twice.
func TestDeclareDoesNotDependOnTheOrderCommandsWereAdded(t *testing.T) {
	build := func(reversed bool) commandtree.Tree {
		root := &cobra.Command{Use: "reference"}
		first := &cobra.Command{Use: "alpha"}
		second := &cobra.Command{Use: "beta"}
		if reversed {
			root.AddCommand(second, first)
		} else {
			root.AddCommand(first, second)
		}
		return cobratree.New(root).Handle(first, noop).Handle(second, noop).Declare()
	}

	forward, backward := build(false), build(true)
	if !slices.EqualFunc(forward.Commands, backward.Commands, sameCommand) {
		t.Errorf("declaration depends on insertion order:\n %+v\n %+v",
			forward.Commands, backward.Commands)
	}
}

// TestDeclareLeavesTheTreeUnsilenced proves declaring is a read. Commands()
// documents that it silences the tree at the moment it is served; Declare must
// not bring that forward, or inspecting a tree would change it.
func TestDeclareLeavesTheTreeUnsilenced(t *testing.T) {
	root := &cobra.Command{Use: "reference"}

	cobratree.New(root).Declare()

	if root.SilenceErrors || root.SilenceUsage {
		t.Error("declaring silenced the command tree")
	}
}

func findDeclared(tree commandtree.Tree, path ...string) (commandtree.Command, bool) {
	for _, command := range tree.Commands {
		if slices.Equal(command.Path, path) {
			return command, true
		}
	}
	return commandtree.Command{}, false
}

func sameCommand(a, b commandtree.Command) bool {
	return slices.Equal(a.Path, b.Path) && a.Runnable == b.Runnable &&
		a.Short == b.Short && slices.Equal(a.Flags, b.Flags)
}

func noop(context.Context, module.Request) (result.Result, error) {
	return result.Result{}, nil
}
