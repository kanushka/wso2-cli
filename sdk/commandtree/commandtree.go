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

// Package commandtree carries a module's declared command tree: the commands it
// serves, the flags each one accepts, and enough about each flag for a parser to
// know where its value ends.
//
// The shell cannot otherwise tell where its own flags stop and a module's begin.
// Without a declaration it consumes the flags it recognises and hands everything
// from the first unrecognised one onward to the module untouched, so a flag's
// position silently changes its meaning. A declared tree is what lets the shell
// parse a product command line as precisely as the module would.
//
// This package is data and queries only. It builds nothing, reads no file, and
// executes nothing, so both the shell and a module can depend on it without
// either depending on the other. Where a tree may be read from is not this
// package's business to enforce; see internal/parsetree for the boundary that
// keeps a remotely fetched tree away from the parser.
package commandtree

import (
	"slices"
	"strings"
)

// The flag types this package names. A flag's type reaches the parser for one
// reason — whether a value follows the flag — so only the boolean case has to
// be recognised, and every other type is carried through as pflag spells it.
const (
	// TypeBool is the type of a flag that takes no value.
	TypeBool = "bool"
	// TypeString is the type of an ordinary string flag. It is named for
	// tests and callers that build a tree by hand; the parser treats it like
	// every other non-boolean type.
	TypeString = "string"
)

// Tree is the set of commands a module serves.
//
// The zero Tree is a module that declares nothing, which is a supported state
// rather than an error: a module built against an older SDK, or one that does
// not use Cobra, has no tree to declare. The shell falls back to passing its
// arguments through, which is what it did for every module before declarations
// existed.
type Tree struct {
	// Commands are every command in the tree, ordered canonically by path.
	Commands []Command `json:"commands,omitempty"`
}

// Command is one command a module declares.
type Command struct {
	// Path is the command path within the namespace, such as
	// []string{"apps", "list"}. The empty path is the namespace's own root.
	Path []string `json:"path,omitempty"`
	// Short is the one-line description, as the module's own help renders it.
	Short string `json:"short,omitempty"`
	// Runnable reports whether the module serves this command or only groups
	// others beneath it. A group is declared so that its path parses and its
	// help is reachable, and refused as a command to run.
	Runnable bool `json:"runnable,omitempty"`
	// Hidden reports whether this path should be offered to a user. An alias
	// gets its own entry so that it parses, and is hidden so that it is not
	// suggested alongside the name it duplicates.
	Hidden bool `json:"hidden,omitempty"`
	// Flags are every flag this command accepts, including the ones it
	// inherits from its parents. They are flattened at extraction so that
	// answering "does this command take this flag" never walks the tree.
	Flags []Flag `json:"flags,omitempty"`
}

// Flag is one flag a command accepts.
type Flag struct {
	// Name is the flag's long spelling, without leading dashes.
	Name string `json:"name"`
	// Shorthand is the single-letter spelling, empty when the flag has none.
	Shorthand string `json:"shorthand,omitempty"`
	// Usage is the flag's one-line description.
	Usage string `json:"usage,omitempty"`
	// Type is the flag's value type as pflag names it. The parser reads it
	// for one question only, which TakesValue answers.
	Type string `json:"type,omitempty"`
}

// New builds a canonical tree from commands in any order.
//
// A declaration is written into a receipt at install, published in a catalog by
// the release pipeline, and compared between the two. Two extractions of the
// same tree therefore have to produce identical bytes, and the order Cobra hands
// its commands back in is not stable enough to rely on. Ordering here rather
// than at each call site is what makes that a property of the type.
func New(commands []Command) Tree {
	ordered := make([]Command, len(commands))
	for index, command := range commands {
		ordered[index] = command.canonical()
	}
	slices.SortFunc(ordered, func(a, b Command) int {
		return slices.Compare(a.Path, b.Path)
	})
	return Tree{Commands: ordered}
}

// canonical returns the command with its own slices ordered and copied, so that
// a tree does not alias the caller's memory and two equal commands serialize
// alike.
func (c Command) canonical() Command {
	c.Path = slices.Clone(c.Path)
	c.Flags = slices.Clone(c.Flags)
	slices.SortFunc(c.Flags, func(a, b Flag) int {
		return strings.Compare(a.Name, b.Name)
	})
	return c
}

// Empty reports whether the tree declares no commands. A module with an empty
// tree is one the shell parses for as it always has.
func (t Tree) Empty() bool {
	return len(t.Commands) == 0
}

// Find matches the longest declared command path at the front of args and
// reports it with the arguments left over.
//
// Longest wins: a tree declaring both "apps" and "apps list" answers "apps list"
// for "apps list --all", because the shorter match would send the wrong path to
// the module and pass "list" along as an argument.
//
// A leading word that names no command is not a match. The caller reports it as
// an unknown command rather than treating it as an argument to the namespace's
// root, which is the difference between naming a user's typo and running
// something they did not ask for.
func (t Tree) Find(args []string) (Command, []string, bool) {
	var (
		best      Command
		bestDepth = -1
	)
	for _, command := range t.Commands {
		depth := len(command.Path)
		if depth <= bestDepth || depth > len(args) {
			continue
		}
		if slices.Equal(command.Path, args[:depth]) {
			best, bestDepth = command, depth
		}
	}
	if bestDepth < 0 {
		return Command{}, nil, false
	}
	remaining := args[bestDepth:]
	// A plain word left over where the matched command has children is the
	// name of a subcommand the module does not serve, not an argument. Where
	// the match is a leaf the same word is its argument, which is why this
	// asks the tree instead of refusing every unmatched word: a command that
	// takes a file name would otherwise be unreachable.
	if len(remaining) > 0 && !strings.HasPrefix(remaining[0], "-") && t.hasChildren(best.Path) {
		return Command{}, nil, false
	}
	return best, remaining, true
}

// hasChildren reports whether the tree declares any command beneath this path.
func (t Tree) hasChildren(path []string) bool {
	for _, command := range t.Commands {
		if len(command.Path) > len(path) && slices.Equal(command.Path[:len(path)], path) {
			return true
		}
	}
	return false
}

// LookupFlag reports the flag a command accepts under its long spelling.
func (c Command) LookupFlag(name string) (Flag, bool) {
	for _, flag := range c.Flags {
		if flag.Name == name {
			return flag, true
		}
	}
	return Flag{}, false
}

// LookupShorthand reports the flag a command accepts under a single-letter
// spelling.
func (c Command) LookupShorthand(letter rune) (Flag, bool) {
	for _, flag := range c.Flags {
		if flag.Shorthand != "" && []rune(flag.Shorthand)[0] == letter {
			return flag, true
		}
	}
	return Flag{}, false
}

// TakesValue reports whether a value follows this flag on the command line.
//
// Every type but boolean does. Getting this backwards is what makes "--all list"
// swallow a command path as a flag's argument, so the parser asks the
// declaration rather than guessing from the shape of what follows.
func (f Flag) TakesValue() bool {
	return f.Type != TypeBool
}
