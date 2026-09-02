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

// Lookup resolves the command path at the front of words, reporting the command
// and the arguments left after it.
//
// It reports false for a path the module does not serve, which is what lets the
// shell name a mistyped product command instead of forwarding it and letting the
// module fail further along.
func (t Tree) Lookup(words []string) (commandtree.Command, []string, bool) {
	return t.declared.Find(words)
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
