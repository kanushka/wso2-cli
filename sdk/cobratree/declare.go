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

package cobratree

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/wso2/wso2-cli/sdk/commandtree"
)

// Declare reports the tree as the command declaration the shell parses from.
//
// The declaration is generated from the same Cobra tree that is served, which is
// the point: a module's help and its behaviour cannot disagree, and adding a
// command or a flag means editing the tree and nothing else. Nothing here is
// hand-written, and there is no second schema to keep in step.
//
// It differs from [Tree.Commands] in what it keeps and what it carries. Commands
// emits only what can run, because a command with no handler cannot be served.
// Declare emits every command in the tree, because the shell has to parse the
// path a user types on the way to one that can, and because a group's own help
// is something a user can ask for. And it carries each command's flags, which
// the served form has no use for and the parser cannot work without.
//
// Declaring is a read. Unlike Commands, it does not silence the tree, so
// inspecting a module's declaration never changes how the module would behave.
func (t *Tree) Declare() commandtree.Tree {
	var declared []commandtree.Command
	t.declare(t.root, nil, &declared)
	return commandtree.New(declared)
}

// declare walks the tree depth first, emitting one entry per path a command
// answers to.
func (t *Tree) declare(command *cobra.Command, path []string, into *[]commandtree.Command) {
	_, runnable := t.handlers[command]
	entry := commandtree.Command{
		Path:     path,
		Short:    command.Short,
		Runnable: runnable,
		Hidden:   command.Hidden,
		Flags:    declareFlags(command),
	}
	*into = append(*into, entry)

	// An alias is a second path to the same command, so it is declared as its
	// own entry rather than as a field the parser would have to interpret.
	// Hidden keeps it out of suggestions, where it would appear as a
	// near-duplicate of the name it stands in for.
	//
	// Only the command's own name is aliased here, not its parents': a nested
	// alias path is spelled out by whichever level declares it, and expanding
	// every combination would multiply the declaration without naming a path
	// Cobra resolves differently.
	if len(path) > 0 {
		for _, alias := range command.Aliases {
			aliased := entry
			aliased.Path = append(append([]string(nil), path[:len(path)-1]...), alias)
			aliased.Hidden = true
			*into = append(*into, aliased)
		}
	}

	for _, child := range command.Commands() {
		t.declare(child, append(append([]string(nil), path...), child.Name()), into)
	}
}

// declareFlags reports every flag a command accepts, its own and the ones it
// inherits, flattened into one list.
//
// Flattening happens here so that the parser answers "does this command take
// this flag" from the command alone. A parser that walked back up the tree to
// answer it would be a second implementation of Cobra's inheritance, and the
// place where the shell and the module start disagreeing about a flag.
func declareFlags(command *cobra.Command) []commandtree.Flag {
	var flags []commandtree.Flag
	seen := map[string]bool{}
	appendFlag := func(flag *pflag.Flag) {
		if seen[flag.Name] {
			return
		}
		seen[flag.Name] = true
		flags = append(flags, commandtree.Flag{
			Name:      flag.Name,
			Shorthand: flag.Shorthand,
			Usage:     flag.Usage,
			Type:      flag.Value.Type(),
		})
	}
	// LocalFlags and InheritedFlags both merge the persistent sets before
	// reporting, so between them they name every flag the command would parse.
	command.LocalFlags().VisitAll(appendFlag)
	command.InheritedFlags().VisitAll(appendFlag)
	return flags
}
