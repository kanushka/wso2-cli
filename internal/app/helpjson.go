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
	"bytes"
	"encoding/json"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/internal/parsetree"
	"github.com/wso2/wso2-cli/sdk/commandtree"
)

// helpDocument is the machine-readable form of help: the whole command tree
// in one read, so that an agent loads it once instead of walking the pages
// with --help. Every member is read from the same places the pages are: the
// shell's own command tree, and the tree each installed module declared at
// install (ADR 0013). Nothing is launched and nothing reaches the network.
//
// A document for one topic carries only that topic's part: the shell commands
// beneath a shell command, or the one product with the commands beneath the
// command asked about. The shape stays the same so a reader parses it one way.
type helpDocument struct {
	// Name is the shell as it was invoked, the first word of every path.
	Name string `json:"name"`
	// Commands are the shell's own commands, every level flattened, each
	// with its path after the name.
	Commands []helpCommand `json:"commands,omitempty"`
	// Flags are the shell flags every shell command accepts, the persistent
	// ones. A flag a shell command declares for itself, such as --context, is
	// in that command's own flags.
	Flags []helpFlag `json:"flags,omitempty"`
	// Products are the product namespaces: every one this release knows a
	// stable version of, marked installed or not, and every installed one
	// with its declared tree.
	Products []helpProductDocument `json:"products,omitempty"`
	// Warnings say what the document could not read and so does not show,
	// in the words the table form uses. Absent when it is complete.
	Warnings []string `json:"warnings,omitempty"`
	// Next is the commands a product's own page names in its Next line.
	// Absent on every other page, as the line is.
	Next []string `json:"next,omitempty"`
}

// helpProductDocument is one product namespace in the document.
type helpProductDocument struct {
	Namespace string `json:"namespace"`
	// Description is what the root page names the product by: its released
	// title, or the one-line description its declared tree gives.
	Description string `json:"description,omitempty"`
	Installed   bool   `json:"installed"`
	// Commands is the declared tree, flattened, each path starting with the
	// namespace. Absent for a product that is not installed, whose tree this
	// shell does not hold (ADR 0013), and for one that declares none.
	Commands []helpCommand `json:"commands,omitempty"`
	// Flags are the shell's flags accepted on every product command line,
	// which the shell reads wherever they appear and never forwards. They
	// are listed with the product, as the product page lists them, because
	// a reader who finds them beside the module's would look for them in
	// the module's documentation.
	Flags []helpFlag `json:"flags,omitempty"`
}

// helpCommand is one command, shell or product.
type helpCommand struct {
	// Path is the words after the shell's name that reach the command.
	Path []string `json:"path"`
	// Arguments is what the usage line names after the path: positional
	// arguments in angle brackets, and any flag spelled out beside them.
	Arguments   string `json:"arguments,omitempty"`
	Description string `json:"description,omitempty"`
	// Examples are runnable command lines, placeholders in angle brackets.
	Examples []string `json:"examples,omitempty"`
	// Runnable is false for a command that only groups the ones beneath it.
	Runnable bool     `json:"runnable"`
	Aliases  []string `json:"aliases,omitempty"`
	// Commands are the names of the commands directly beneath this one.
	Commands []string   `json:"commands,omitempty"`
	Flags    []helpFlag `json:"flags,omitempty"`
	// ResultFields are the JSON members the command's result carries, where
	// the module declared them.
	ResultFields []string `json:"resultFields,omitempty"`
}

// helpFlag is one flag a command accepts.
type helpFlag struct {
	Name        string `json:"name"`
	Shorthand   string `json:"shorthand,omitempty"`
	Type        string `json:"type"`
	Default     string `json:"default,omitempty"`
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
}

// renderHelpJSON writes a help document as one JSON document on standard
// output, indented, with placeholders left as written rather than escaped.
func (s Shell) renderHelpJSON(document helpDocument) error {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(document); err != nil {
		return err
	}
	_, err := s.Streams.Out.Write(encoded.Bytes())
	return err
}

// shellHelpDocument is the whole tree: the shell's commands and flags, and
// every product this release or this machine knows of.
func (s Shell) shellHelpDocument(root *cobra.Command) helpDocument {
	name := output.NameOf(s.Streams.Out)
	document := helpDocument{Name: name, Flags: shellFlagsOf(root, name)}
	for _, command := range listedCommands(root) {
		document.Commands = append(document.Commands, shellCommandSubtree(command, name)...)
	}
	products, inventoryRead := s.helpProducts()
	if !inventoryRead {
		document.Warnings = append(document.Warnings, inventoryUnreadWarning)
	}
	trees := s.installedTrees()
	for _, product := range products {
		entry := helpProductDocument{Namespace: product.namespace, Description: product.summary, Installed: product.installed}
		if tree, declared := trees[product.namespace]; declared {
			entry.Commands = productCommandSubtree(product.namespace, tree, tree.Route(nil).Command, name)
			entry.Flags = productShellFlags()
		}
		document.Products = append(document.Products, entry)
	}
	return document
}

// shellCommandHelpDocument is one shell command and everything beneath it.
func (s Shell) shellCommandHelpDocument(command *cobra.Command) helpDocument {
	name := output.NameOf(s.Streams.Out)
	return helpDocument{Name: name, Commands: shellCommandSubtree(command, name)}
}

// productHelpDocument is one product, with the commands beneath the one asked
// about: the whole declared tree when that is the namespace itself.
func (s Shell) productHelpDocument(namespace string, declared parsetree.Tree, found commandtree.Command,
	descriptor *modules.ProductDescriptor) helpDocument {
	name := output.NameOf(s.Streams.Out)
	entry := helpProductDocument{Namespace: namespace, Installed: true,
		Commands: productCommandSubtree(namespace, declared, found, name),
		Flags:    productShellFlags()}
	products, _ := s.helpProducts()
	for _, product := range products {
		if product.namespace == namespace {
			entry.Description = product.summary
		}
	}
	document := helpDocument{Name: name, Products: []helpProductDocument{entry}}
	if len(found.Path) == 0 {
		// An example is a command as declared, which prose extraction could
		// cut short at a value that reads as a word.
		switch next, example := s.productNextStep(namespace, found, descriptor); {
		case example != "":
			document.Next = []string{output.Rename(example, name)}
		case next != "":
			document.Next = output.NextCommands(s.Streams.Out, next)
		}
	}
	return document
}

// installedTrees reports the declared tree of every installed product whose
// receipt can be read, by namespace. A product whose receipt cannot be read
// is listed without one: help is not where that is diagnosed.
func (s Shell) installedTrees() map[string]parsetree.Tree {
	trees := map[string]parsetree.Tree{}
	store, err := s.store()
	if err != nil {
		return trees
	}
	installed, _, err := store.Inventory()
	if err != nil {
		return trees
	}
	for _, module := range installed {
		if tree := parsetree.FromReceipt(module.Receipt); tree.Declared() {
			trees[module.Namespace] = tree
		}
	}
	return trees
}

// productCommandSubtree flattens a declared command and every visible command
// beneath it, in the tree's order.
func productCommandSubtree(namespace string, declared parsetree.Tree, found commandtree.Command, name string) []helpCommand {
	var commands []helpCommand
	for _, command := range declared.Commands() {
		if len(command.Path) < len(found.Path) || !slices.Equal(command.Path[:len(found.Path)], found.Path) {
			continue
		}
		entry := helpCommand{
			Path:         append([]string{namespace}, command.Path...),
			Arguments:    command.Arguments,
			Description:  output.Rename(command.Short, name),
			Runnable:     command.Runnable,
			Aliases:      command.Aliases,
			ResultFields: command.ResultFields,
		}
		for _, example := range command.Examples() {
			entry.Examples = append(entry.Examples, output.Rename(example, name))
		}
		for _, child := range productChildren(declared, command.Path) {
			entry.Commands = append(entry.Commands, child.Path[len(child.Path)-1])
		}
		for _, flag := range command.Flags {
			entry.Flags = append(entry.Flags, helpFlag{
				Name: flag.Name, Shorthand: flag.Shorthand, Type: flag.Type, Default: flag.Default,
				Required: flag.Required, Description: output.Rename(flag.Usage, name),
			})
		}
		commands = append(commands, entry)
	}
	return commands
}

// shellCommandSubtree flattens a shell command and every listed command
// beneath it, in the order Cobra keeps them.
func shellCommandSubtree(command *cobra.Command, name string) []helpCommand {
	entry := helpCommand{
		Path:        shellCommandPath(command),
		Arguments:   shellCommandArguments(command),
		Description: output.Rename(command.Short, name),
		Runnable:    command.Runnable(),
		Aliases:     command.Aliases,
	}
	for _, example := range (commandtree.Command{Example: command.Example}).Examples() {
		entry.Examples = append(entry.Examples, output.Rename(example, name))
	}
	children := listedCommands(command)
	for _, child := range children {
		entry.Commands = append(entry.Commands, child.Name())
	}
	appendFlag := func(flag *pflag.Flag) {
		if flag.Hidden {
			return
		}
		entry.Flags = append(entry.Flags, describeShellFlag(flag, name))
	}
	command.LocalFlags().VisitAll(appendFlag)
	command.InheritedFlags().VisitAll(appendFlag)
	commands := []helpCommand{entry}
	for _, child := range children {
		commands = append(commands, shellCommandSubtree(child, name)...)
	}
	return commands
}

func shellCommandArguments(command *cobra.Command) string {
	_, arguments, _ := strings.Cut(strings.TrimSpace(command.Use), " ")
	return strings.TrimSpace(arguments)
}

// listedCommands are the commands a help page lists beneath one: the
// available ones, and help itself, which Cobra does not count as available.
func listedCommands(command *cobra.Command) []*cobra.Command {
	var listed []*cobra.Command
	for _, child := range command.Commands() {
		if child.IsAvailableCommand() || child.Name() == "help" {
			listed = append(listed, child)
		}
	}
	return listed
}

// shellCommandPath reports the words after the shell's name that reach a
// shell command.
func shellCommandPath(command *cobra.Command) []string {
	var reversed []string
	for current := command; current != nil && current.HasParent(); current = current.Parent() {
		reversed = append(reversed, current.Name())
	}
	slices.Reverse(reversed)
	return reversed
}

// shellFlagsOf reports the shell flags every shell command accepts: the
// root's persistent ones. Its local ones, --context and --output, are taken
// only by the shell commands that declare them, and those commands list them;
// a document that said otherwise would send a reader into shell.unsupported_flag.
func shellFlagsOf(root *cobra.Command, name string) []helpFlag {
	var flags []helpFlag
	root.PersistentFlags().VisitAll(func(flag *pflag.Flag) {
		if !flag.Hidden {
			flags = append(flags, describeShellFlag(flag, name))
		}
	})
	return flags
}

// productShellFlags reports the shell flags accepted on every product command
// line, the three the product page prints in the same words. The shell reads
// them wherever they appear on the line and never forwards them, so they are
// not in any module's declared tree; --no-input is taken off the line before
// Cobra sees it and is declared nowhere at all.
func productShellFlags() []helpFlag {
	return []helpFlag{
		{Name: contextFlag, Type: "string", Description: "Use the named context instead of the selected one."},
		{Name: noInputFlag, Type: "bool", Description: "Refuse rather than prompt, open a browser, or wait for a person."},
		{Name: outputFlag, Shorthand: "o", Type: "string", Default: string(output.ModeTable), Description: "Render results as table or json."},
	}
}

// describeShellFlag describes one of the shell's own flags the way a product flag is
// described, so a reader handles both alike.
func describeShellFlag(flag *pflag.Flag, name string) helpFlag {
	described := helpFlag{
		Name: flag.Name, Shorthand: flag.Shorthand, Type: flag.Value.Type(),
		Default: flag.DefValue, Description: output.Rename(flag.Usage, name),
	}
	switch described.Default {
	case "", "false", "[]", "0":
		described.Default = ""
	}
	if annotation, marked := flag.Annotations[cobra.BashCompOneRequiredFlag]; marked && len(annotation) > 0 {
		described.Required = annotation[0] == "true"
	}
	return described
}
