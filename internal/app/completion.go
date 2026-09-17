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
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/internal/parsetree"
)

// isCompletionRequest reports whether a first argument is the hidden command a
// shell's completion script runs on every Tab. Cobra adds that command only
// while it executes, so dispatch cannot find it among the root's commands.
func isCompletionRequest(name string) bool {
	return name == cobra.ShellCompRequestCmd || name == cobra.ShellCompNoDescRequestCmd
}

// prepareCompletion wires the completion of everything the shell knows without
// launching anything: its own commands and flag values, the installed product
// namespaces, and each product's commands and flags from the tree its receipt
// declares.
//
// words are the words the completion script sent, the last of which is the one
// being completed. A product command never enters the command tree, so Cobra
// hands its words to the root and drops a shell flag's name from them when it
// thinks a value is being completed. The product side reads the words as they
// were written instead.
//
// The words are copied because Cobra appends to the argument slice it is given
// while it looks for flags, which would overwrite the word being completed.
func (s Shell) prepareCompletion(root *cobra.Command, words []string) {
	words = slices.Clone(words)
	registerFlagValues(root, map[string]cobra.CompletionFunc{
		contextFlag: s.completeContextNames,
		outputFlag:  completeOutputModes,
	})
	root.ValidArgsFunction = func(*cobra.Command, []string, string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(words) == 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		typed, before := words[len(words)-1], words[:len(words)-1]
		namespace, rest, found := productWords(root, before)
		if !found {
			return s.completeNamespaces(root, typed), cobra.ShellCompDirectiveNoFileComp
		}
		return s.completeProduct(namespace, rest, typed)
	}
}

// registerFlagValues registers a value completion for every flag in the tree
// with one of the given names. A persistent flag is reached from each command
// beneath the one that declares it, and registering it again is refused, so
// that refusal is expected and ignored.
func registerFlagValues(command *cobra.Command, completions map[string]cobra.CompletionFunc) {
	for _, flags := range []*pflag.FlagSet{command.LocalNonPersistentFlags(), command.PersistentFlags()} {
		flags.VisitAll(func(flag *pflag.Flag) {
			if complete, ok := completions[flag.Name]; ok {
				_ = command.RegisterFlagCompletionFunc(flag.Name, complete)
			}
		})
	}
	for _, child := range command.Commands() {
		registerFlagValues(child, completions)
	}
}

// productWords finds the product namespace among the words written before the
// one being completed, stepping over the root's own flags and their values the
// way dispatch does. It reports the namespace and the words after it.
func productWords(root *cobra.Command, words []string) (string, []string, bool) {
	for index := 0; index < len(words); index++ {
		word := words[index]
		switch {
		case word == "--":
			return "", nil, false
		case strings.HasPrefix(word, "--"):
			name, _, attached := strings.Cut(word[2:], "=")
			if !attached && rootFlagTakesValue(root.Flags().Lookup(name), root.PersistentFlags().Lookup(name)) {
				index++
			}
		case len(word) == 2 && word[0] == '-':
			letter := word[1:]
			if rootFlagTakesValue(root.Flags().ShorthandLookup(letter), root.PersistentFlags().ShorthandLookup(letter)) {
				index++
			}
		case strings.HasPrefix(word, "-"):
		default:
			return word, words[index+1:], true
		}
	}
	return "", nil, false
}

// rootFlagTakesValue reports whether the first flag found consumes the word
// after it. An unknown flag is not stepped over: dispatch would refuse it, so
// nothing after it is a namespace worth completing.
func rootFlagTakesValue(candidates ...*pflag.Flag) bool {
	for _, flag := range candidates {
		if flag != nil {
			return flag.NoOptDefVal == ""
		}
	}
	return false
}

// completeNamespaces offers the installed product namespaces. Cobra offers the
// shell's own commands beside them, and a namespace a built-in shadows is left
// out because dispatch would never reach it.
func (s Shell) completeNamespaces(root *cobra.Command, typed string) []cobra.Completion {
	store, err := s.store()
	if err != nil {
		return nil
	}
	installed, _, err := store.Inventory()
	if err != nil {
		return nil
	}
	var completions []cobra.Completion
	for _, module := range installed {
		if !strings.HasPrefix(module.Namespace, typed) || isShellCommand(root, module.Namespace) {
			continue
		}
		completions = append(completions,
			cobra.CompletionWithDesc(module.Namespace, catalog.Printable(declaredSummary(module.Receipt))))
	}
	return completions
}

// completeProduct offers what may follow a product command line, read from the
// tree the installed module declared. Nothing is launched and no session is
// needed. A module that declared no tree gets the shell's default, which is
// file completion.
func (s Shell) completeProduct(namespace string, words []string, typed string) ([]cobra.Completion, cobra.ShellCompDirective) {
	store, err := s.store()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	installed, _, err := store.Inventory()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	found := slices.IndexFunc(installed, func(module modules.Installed) bool { return module.Namespace == namespace })
	if found < 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	declared := parsetree.FromReceipt(installed[found].Receipt)
	if !declared.Declared() {
		return nil, cobra.ShellCompDirectiveDefault
	}

	if flag, pending := declared.PendingValue(words); pending {
		return s.completeProductFlagValue(flag, typed)
	}
	if name, value, attached := strings.Cut(typed, "="); attached && strings.HasPrefix(name, "--") {
		return s.completeProductFlagValue(strings.TrimPrefix(name, "--"), value)
	}

	routed := declared.Route(words)
	if strings.HasPrefix(typed, "-") {
		return productFlags(routed, typed), cobra.ShellCompDirectiveNoFileComp
	}
	if routed.Unrouted != "" {
		return nil, cobra.ShellCompDirectiveDefault
	}
	children := productChildren(declared, routed.Command.Path)
	if len(children) == 0 {
		return nil, cobra.ShellCompDirectiveDefault
	}
	var completions []cobra.Completion
	for _, child := range children {
		name := child.Path[len(child.Path)-1]
		if strings.HasPrefix(name, typed) {
			completions = append(completions, cobra.CompletionWithDesc(name, catalog.Printable(child.Short)))
		}
	}
	return completions, cobra.ShellCompDirectiveNoFileComp
}

// completeProductFlagValue offers the values of a product command's flag. The
// shell knows the values of the flags it forwards; any other flag's value is
// left to the shell's default.
func (s Shell) completeProductFlagValue(flag, typed string) ([]cobra.Completion, cobra.ShellCompDirective) {
	switch flag {
	case contextFlag:
		return s.completeContextNames(nil, nil, typed)
	case outputFlag, "o":
		return completeOutputModes(nil, nil, typed)
	}
	return nil, cobra.ShellCompDirectiveDefault
}

// productFlags offers the flags the routed command declares, and the shell
// flags dispatch takes off every product command line.
func productFlags(routed parsetree.Routed, typed string) []cobra.Completion {
	type offered struct{ spelling, usage string }
	var flags []offered
	for _, flag := range routed.Command.Flags {
		flags = append(flags, offered{"--" + flag.Name, flag.Usage})
		if flag.Shorthand != "" && typed == "-" {
			flags = append(flags, offered{"-" + flag.Shorthand, flag.Usage})
		}
	}
	for _, shell := range []offered{
		{"--" + verboseFlag, "Write diagnostics about what the shell attempted to stderr."},
		{"--" + noInputFlag, "Refuse rather than prompt or wait for a human."},
	} {
		if _, declared := routed.Command.LookupFlag(strings.TrimPrefix(shell.spelling, "--")); !declared {
			flags = append(flags, shell)
		}
	}
	var completions []cobra.Completion
	for _, flag := range flags {
		if strings.HasPrefix(flag.spelling, typed) {
			completions = append(completions, cobra.CompletionWithDesc(flag.spelling, catalog.Printable(flag.usage)))
		}
	}
	return completions
}

// completeContextNames offers the names of the configured contexts. A document
// that cannot be read offers nothing: completion is not where that is reported.
func (s Shell) completeContextNames(_ *cobra.Command, _ []string, typed string) ([]cobra.Completion, cobra.ShellCompDirective) {
	root, err := s.stateRoot()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	document, err := contexts.Load(root)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var completions []cobra.Completion
	for _, configured := range document.Contexts {
		if strings.HasPrefix(configured.Name, typed) {
			completions = append(completions, configured.Name)
		}
	}
	return completions, cobra.ShellCompDirectiveNoFileComp
}

// completeFirstContextName offers context names for a command whose first
// argument names a context, and nothing after it.
func (s Shell) completeFirstContextName(command *cobra.Command, args []string, typed string) ([]cobra.Completion, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return s.completeContextNames(command, args, typed)
}

// completeOutputModes offers the renderings --output accepts.
func completeOutputModes(_ *cobra.Command, _ []string, typed string) ([]cobra.Completion, cobra.ShellCompDirective) {
	var completions []cobra.Completion
	for _, mode := range []output.Mode{output.ModeTable, output.ModeJSON} {
		if strings.HasPrefix(string(mode), typed) {
			completions = append(completions, string(mode))
		}
	}
	return completions, cobra.ShellCompDirectiveNoFileComp
}
