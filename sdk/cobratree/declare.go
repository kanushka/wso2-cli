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
	"context"
	"encoding/json"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/wso2/wso2-cli/sdk/commandtree"
	"github.com/wso2/wso2-cli/sdk/module"
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
	*into = append(*into, commandtree.Command{
		Path:         path,
		Short:        command.Short,
		Example:      command.Example,
		Runnable:     runnable,
		Hidden:       command.Hidden,
		Aliases:      append([]string(nil), command.Aliases...),
		Flags:        declareFlags(command),
		ResultFields: declaredResultFields(command),

		TimeoutSeconds: declaredTimeout(command),
	})

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
			Default:   flagDefault(flag),
			Required:  requiredFlag(flag),
			// Carried verbatim because it is what decides whether the next
			// argument belongs to this flag. Reading the type instead would
			// be right for booleans and wrong for counters.
			NoOptDefault: flag.NoOptDefVal,
		})
	}
	// LocalFlags and InheritedFlags both merge the persistent sets before
	// reporting, so between them they name every flag the command would parse.
	command.LocalFlags().VisitAll(appendFlag)
	command.InheritedFlags().VisitAll(appendFlag)

	// Cobra adds a help flag to every command as it executes, not as it is
	// built, so a tree inspected beforehand does not carry one. Declaring it
	// here is what makes the declaration describe what the command accepts
	// rather than what it happens to have been given yet — without it the
	// shell would report --help as a flag the command does not take, which is
	// both a refusal the module would not make and untrue. Like Cobra, a
	// command that declares its own is left alone.
	if !seen[commandtree.HelpFlagName] {
		shorthand := commandtree.HelpFlagShorthand
		for _, flag := range flags {
			if flag.Shorthand == shorthand {
				shorthand = ""
				break
			}
		}
		flags = append(flags, commandtree.Flag{
			Name:         commandtree.HelpFlagName,
			Shorthand:    shorthand,
			Usage:        "Show help for this command.",
			Type:         commandtree.TypeBool,
			NoOptDefault: "true",
		})
	}
	return flags
}

// flagDefault reports the value a flag has when it is not written. A boolean's
// "false" and an empty string are no default worth declaring, and a slice's
// "[]" is pflag's spelling of none.
func flagDefault(flag *pflag.Flag) string {
	switch flag.DefValue {
	case "", "false", "[]", "0":
		return ""
	}
	return flag.DefValue
}

// ResultFieldsAnnotation is the Cobra annotation ResultFields writes, and
// Declare reads, naming the JSON members a command's result carries.
const ResultFieldsAnnotation = "wso2.resultFields"

// ResultFields declares the JSON members a command's result carries, in order,
// so that the shell's machine-readable help can say what the command answers.
// It records them as an annotation on the command, which is the one place
// Cobra keeps data beside a command, and Declare reads them from there. The
// annotation is a JSON array, so a name is read back exactly as it was given,
// whatever characters it holds.
//
// It describes, and never enforces: the result the handler returns is what is
// rendered, whatever was declared here.
func ResultFields(command *cobra.Command, names ...string) {
	if command.Annotations == nil {
		command.Annotations = map[string]string{}
	}
	encoded, err := json.Marshal(names)
	if err != nil {
		// A []string always marshals; this is unreachable, and declaring
		// nothing is the honest answer if it ever were reached.
		return
	}
	command.Annotations[ResultFieldsAnnotation] = string(encoded)
}

// declaredResultFields reads back what ResultFields recorded.
func declaredResultFields(command *cobra.Command) []string {
	recorded, declared := command.Annotations[ResultFieldsAnnotation]
	if !declared || recorded == "" {
		return nil
	}
	var names []string
	if err := json.Unmarshal([]byte(recorded), &names); err != nil || len(names) == 0 {
		return nil
	}
	return names
}

// requiredFlag reports whether the module marked a flag required with
// cobra.MarkFlagRequired, which is the one way Cobra lets a tree say so.
func requiredFlag(flag *pflag.Flag) bool {
	annotation, marked := flag.Annotations[cobra.BashCompOneRequiredFlag]
	return marked && len(annotation) > 0 && annotation[0] == "true"
}

// Serve declares this tree and then serves it, which is the whole of what a
// Cobra-based module has to do.
//
// Declaring and serving from one call is what keeps a module's commands in one
// place. Calling [Tree.Declare] and [Tree.Commands] separately and handing both
// to [module.Serve] does the same thing, and is the same two chances to update
// one and forget the other.
func (t *Tree) Serve(ctx context.Context, options module.Options) error {
	options.CommandTree = t.Declare()
	return module.Serve(ctx, options, t.Commands()...)
}
