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

// Package cobratree serves a Cobra command tree as a product module.
//
// A product CLI being migrated already has a Cobra command tree: its commands,
// its flags, and its help. This package keeps that tree and changes only what
// each command does at the end — instead of printing, a handler returns a typed
// result the shell renders. The tree is translated into the commands
// [module.Serve] already accepts, so nothing here is a second way to speak the
// module contract.
//
// It is a package of its own so that a module which does not use Cobra does not
// link it.
//
// Two guarantees hold without the module author arranging them. Every writer in
// the tree points at standard error, and Cobra prints neither errors nor usage
// itself, so the tree's own output cannot reach standard output — which carries
// protocol frames only, and which a stray write would corrupt. And a flag
// failure arrives at the shell as a typed problem rather than as Cobra's own
// error text.
//
// The limit is worth stating: a handler that calls fmt.Println writes to
// standard output and corrupts the stream, and no adapter can prevent that. What
// is prevented is the tree doing it on the author's behalf.
package cobratree

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/result"
)

// Tree binds handlers to the commands of one Cobra command tree.
type Tree struct {
	root     *cobra.Command
	handlers map[*cobra.Command]module.Handler
}

// New prepares a command tree to be served.
//
// Every command in the tree, including ones added later, is silenced: its output
// is redirected to standard error, and Cobra is prevented from printing errors
// and usage itself. Standard error is where a module's diagnostics belong, and
// standard output is left to the protocol.
func New(root *cobra.Command) *Tree {
	return &Tree{root: root, handlers: map[*cobra.Command]module.Handler{}}
}

// Handle binds a handler to one command in the tree.
//
// A command with no handler is not served, so the shell reports it as an unknown
// command rather than as a command that succeeded silently.
func (t *Tree) Handle(command *cobra.Command, run module.Handler) *Tree {
	t.handlers[command] = run
	return t
}

// Commands reports the tree as the commands [module.Serve] accepts, one per
// command a handler was bound to.
func (t *Tree) Commands() []module.Command {
	silence(t.root)

	commands := make([]module.Command, 0, len(t.handlers))
	for command, run := range t.handlers {
		commands = append(commands, module.Command{
			Path: path(t.root, command),
			Run:  t.invoke(command, run),
		})
	}
	return commands
}

// invoke parses the module's own arguments with the command's flag set and then
// runs the handler.
//
// The handler reads its flags from the command it was written beside, which is
// why the flags are parsed before it is called and why nothing about the flag
// set has to travel through the request.
func (t *Tree) invoke(command *cobra.Command, run module.Handler) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		if err := command.ParseFlags(request.Arguments); err != nil {
			return result.Result{}, flagProblem(command, err)
		}
		return run(ctx, request)
	}
}

// silence redirects a command tree's output to standard error and stops Cobra
// printing errors and usage itself.
func silence(command *cobra.Command) {
	command.SetOut(os.Stderr)
	command.SetErr(os.Stderr)
	command.SilenceErrors = true
	command.SilenceUsage = true
	for _, child := range command.Commands() {
		silence(child)
	}
}

// path reports a command's path within the namespace, which is its position in
// the tree without the root's own name. The shell sends the same shape in the
// command path it invokes.
func path(root, command *cobra.Command) []string {
	var reversed []string
	for current := command; current != nil && current != root; current = current.Parent() {
		reversed = append(reversed, current.Name())
	}
	names := make([]string, 0, len(reversed))
	for index := len(reversed) - 1; index >= 0; index-- {
		names = append(names, reversed[index])
	}
	return names
}

// flagProblem reports a flag failure as a typed problem.
//
// Cobra and pflag report one as a plain error, which the shell would classify as
// a module process failure — a crash, rather than the user's mistake it is.
func flagProblem(command *cobra.Command, err error) problem.Problem {
	return problem.New(problem.CategoryUsage, "module.flag_invalid", err.Error()).
		WithRecovery(fmt.Sprintf("Run wso2 %s --help to see the flags this command accepts.",
			strings.Join(strings.Fields(command.CommandPath()), " ")))
}
