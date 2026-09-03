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

// Package parsetree holds the one command tree the shell is allowed to parse a
// user's command line with.
//
// A module's command tree exists in two places. The receipt holds the copy read
// out of the installed executable at install time, pinned to that executable by
// a digest the shell recomputes before every launch. The catalog holds a copy
// too, so that a command belonging to a module nobody has installed can still be
// recognised and suggested.
//
// Only the first may reach a parser. The catalog is fetched over the network and
// carries no signature, and a command tree decides how the shell interprets what
// a user types; letting a remote file do that would let whoever served it change
// the meaning of a command already typed. The catalog's copy is for telling
// someone a command exists, never for deciding what one means.
//
// That split is a property of this package rather than a note in a comment.
// [Tree] wraps its declaration in an unexported field and [FromReceipt] is the
// only way to obtain one, so a tree that came from the catalog cannot be handed
// to anything that parses: the call does not compile. The import boundary is
// held to as well — nothing here may reach the catalog package — and
// internal/boundaries states that as a test, because a boundary nothing checks
// is a boundary that erodes.
package parsetree

import (
	"strings"

	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/sdk/commandtree"
)

// Tree is a command tree that came from a verified receipt, and is therefore
// allowed to decide how a command line is read.
//
// The zero Tree declares nothing, which is what a module built before
// declarations existed, or one that does not use Cobra, installs with. The shell
// parses for such a module the way it did before declarations, so the zero value
// is a supported state rather than a missing one.
type Tree struct {
	// declared is unexported on purpose. It is what makes FromReceipt the only
	// door: a caller holding a commandtree.Tree from anywhere else cannot
	// build one of these, and the compiler says so at the call site rather
	// than a reviewer saying so in a comment.
	declared commandtree.Tree
}

// FromReceipt is the only way to obtain a parseable tree.
//
// It takes the whole receipt rather than the tree inside it so that the
// requirement is visible in the type: whoever calls this has already resolved
// and verified an installation, which is what earns the tree the right to parse.
func FromReceipt(receipt modules.Receipt) Tree {
	return Tree{declared: receipt.CommandTree}
}

// Declared reports whether the module declared a tree at all. When it did not,
// the caller falls back to passing the module's arguments through unparsed.
func (t Tree) Declared() bool {
	return !t.declared.Empty()
}

// Routed is where a command line landed in a module's declared tree.
type Routed struct {
	// Command is the command the line names, which is the namespace's own
	// command when the line names no subcommand.
	Command commandtree.Command
	// Path is the indices of the arguments that spelled the command's path, so
	// the caller can tell a word that named a command from one that is an
	// argument to it.
	Path map[int]bool
	// Unrouted is the first plain word that named no command, empty when every
	// plain word was either a command or came after one that was not.
	Unrouted string
}

// Route resolves which command a product command line names.
//
// It walks the line the way Cobra locates a subcommand before parsing anything,
// because the module on the other end is Cobra and the shell has to land on the
// same command it would. Three details of that walk are what make the two
// agree, and each of them was checked against pflag and Cobra rather than
// assumed:
//
// A word is a subcommand only where the command in hand declares one by that
// name. Below that, the same word is an argument — "apps myapp" runs apps with
// an argument, because the module's own parser would.
//
// A flag the command in hand does not declare is assumed to take a value, and
// the word after it is skipped. That is how "--since 1h status" still reaches
// status: the namespace's root command has never heard of --since, and guessing
// that it takes a value is what keeps 1h from being mistaken for a command
// name. The flag is not accepted by doing this; it is only stepped over, and
// whether the command that was found declares it is settled afterwards.
//
// A flag that ends the line, or a "--", ends the walk.
func (t Tree) Route(args []string) Routed {
	// A tree that names no command at the empty path still routes: the walk
	// starts from a namespace that declares no flags of its own rather than
	// refusing to start. Declare always emits the root, so this is the shape a
	// hand-built tree can have and not one the SDK produces.
	current, _ := t.declared.Root()
	routed := Routed{Command: current, Path: map[int]bool{}}
	for index := 0; index < len(args); index++ {
		argument := args[index]
		switch {
		case argument == "--":
			return routed
		case strings.HasPrefix(argument, "--"):
			name, _, attached := strings.Cut(argument[2:], "=")
			if attached {
				continue
			}
			if flag, declared := routed.Command.LookupFlag(name); !declared || flag.TakesValue() {
				index++
			}
		case len(argument) > 1 && argument[0] == '-':
			if shorthandRunEndsInAValue(routed.Command, argument) {
				index++
			}
		default:
			child, found := t.declared.Child(routed.Command.Path, argument)
			if !found {
				routed.Unrouted = argument
				return routed
			}
			routed.Command = child
			routed.Path[index] = true
		}
	}
	return routed
}

// shorthandRunEndsInAValue reports whether a run of single-letter flags takes
// the next argument as a value, which it does when the letter that claims a
// value is the last thing in the run.
func shorthandRunEndsInAValue(command commandtree.Command, argument string) bool {
	letters := []rune(argument[1:])
	for index, letter := range letters {
		flag, declared := command.LookupShorthand(letter)
		if !declared {
			// Unknown letters are assumed to claim a value, for the same
			// reason unknown long flags are: over-stepping loses a command
			// name, and under-stepping invents one out of a flag's value.
			return index == len(letters)-1
		}
		if flag.TakesValue() {
			return index == len(letters)-1
		}
	}
	return false
}

// RootHasChildren reports whether the namespace declares any subcommand. A
// namespace that does takes a subcommand name where it is given a plain word,
// and reports an unknown one rather than passing it along, which is what an
// unmodified Cobra root does.
func (t Tree) RootHasChildren() bool {
	return t.declared.HasChildren(nil)
}

// Commands reports every command the module declares that is worth showing a
// user, which is every one that is not hidden. Suggestions and help read this;
// parsing does not.
func (t Tree) Commands() []commandtree.Command {
	visible := make([]commandtree.Command, 0, len(t.declared.Commands))
	for _, command := range t.declared.Commands {
		if !command.Hidden {
			visible = append(visible, command)
		}
	}
	return visible
}
