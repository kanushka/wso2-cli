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
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/internal/parsetree"
	"github.com/wso2/wso2-cli/sdk/commandtree"
)

// renderProductHelp answers a request for help about a product command from the
// module's declaration.
//
// Nothing is launched to do this. The declaration is local, was read out of the
// installed executable, and is pinned to it, so the shell can say what a command
// accepts without starting a process, selecting a context, or holding a
// session — which is what asking a module used to require, and why asking a
// product command for help used to need a login.
//
// The page is built whole and written once, so an unwritable stream is one error
// rather than a half-written page and fifteen unchecked writes.
//
// Beyond names and flags, the page shows what the guideline's table asks of
// its level, all of it read from the declaration: one example per group on the
// namespace's own page, the group's own examples on a group page, the leaf's
// runnable example and its required flags marked on a leaf page. The
// namespace's page ends with the next line, computed from this machine's
// state rather than fixed: "run wso2 login" on every page is noise, and the
// one command that moves the reader forward is navigation.
func (s Shell) renderProductHelp(namespace string, declared parsetree.Tree,
	found commandtree.Command, descriptor *modules.ProductDescriptor) error {
	name := output.NameOf(s.Streams.Out)
	path := strings.TrimSpace(name + " " + namespace + " " + strings.Join(found.Path, " "))
	children := productChildren(declared, found.Path)
	var page bytes.Buffer

	if found.Short != "" {
		fmt.Fprintf(&page, "%s\n\n", found.Short)
	}
	fmt.Fprintf(&page, "Usage:\n  %s", path)
	if len(children) > 0 {
		fmt.Fprint(&page, " <command>")
	}
	if len(found.Flags) > 0 {
		fmt.Fprint(&page, " [flags]")
	}
	fmt.Fprintln(&page)

	if len(children) > 0 {
		fmt.Fprint(&page, "\nCommands:\n")
		rows := make([][2]string, 0, len(children))
		for _, child := range children {
			rows = append(rows, [2]string{child.Path[len(child.Path)-1], child.Short})
		}
		if err := writeTable(&page, rows); err != nil {
			return err
		}
	}

	if examples := pageExamples(declared, found, children); len(examples) > 0 {
		fmt.Fprint(&page, "\nExamples:\n")
		for _, example := range examples {
			fmt.Fprintf(&page, "  %s\n", example)
		}
	}

	if len(found.Flags) > 0 {
		fmt.Fprint(&page, "\nFlags:\n")
		rows := make([][2]string, 0, len(found.Flags))
		for _, flag := range found.Flags {
			usage := flag.Usage
			if flag.Required {
				usage = strings.TrimSpace(usage + " " + requiredMark)
			}
			rows = append(rows, [2]string{flagSpelling(flag), usage})
		}
		if err := writeTable(&page, rows); err != nil {
			return err
		}
	}

	// The shell's own flags are listed apart from the module's, because they
	// are the shell's on every command and a reader who mistakes one for the
	// other will look for it in the module's documentation.
	fmt.Fprint(&page, "\nShell flags, accepted on every command:\n")
	if err := writeTable(&page, [][2]string{
		{"    --context <name>", "Use the named context instead of the selected one."},
		{"    --no-input", "Refuse rather than prompt, open a browser, or wait for a person."},
		{"-o, --output <mode>", "Render results as table or json."},
	}); err != nil {
		return err
	}

	// The namespace's own page is where a reader decides what to do, so it
	// ends with the one command that moves them on from where this machine
	// is. A command's page is about that command and names nothing else.
	if len(found.Path) == 0 {
		if next, _ := s.productNextStep(namespace, found, descriptor); next != "" {
			fmt.Fprintf(&page, "\nNext  %s\n", output.Hint(s.Streams.Out, next))
		}
	}

	// A module's descriptions are prose it wrote naming the default shell.
	_, err := io.WriteString(s.Streams.Out, output.RenameLines(page.String(), name))
	return err
}

// requiredMark is how a flag the command refuses to run without is marked in
// its usage column. A reader, and an agent, learns it from the page rather
// than from the refusal.
const requiredMark = "(required)"

// groupExampleLimit is how many examples a group page shows when the group
// declares none of its own and they are taken from its children instead: the
// two most common actions, which without a declaration means the first two.
const groupExampleLimit = 2

// pageExamples reports the example lines a page shows.
//
// A leaf shows its own. A group shows its own when it declares any, because
// the module knows which of its actions are the common ones; otherwise the
// first line of its first children stands in. The namespace's own page shows
// one line per command beneath it, so that a reader sees the shape of every
// group without opening each one.
func pageExamples(declared parsetree.Tree, found commandtree.Command, children []commandtree.Command) []string {
	if len(found.Path) > 0 {
		if own := found.Examples(); len(own) > 0 || len(children) == 0 {
			return own
		}
		return childExamples(declared, children, groupExampleLimit)
	}
	return childExamples(declared, children, len(children))
}

// childExamples reports the first example line of each child, at most limit of
// them, reaching one level further down for a child that declares none.
func childExamples(declared parsetree.Tree, children []commandtree.Command, limit int) []string {
	var examples []string
	for _, child := range children {
		if len(examples) == limit {
			break
		}
		if example, ok := firstExample(declared, child); ok {
			examples = append(examples, example)
		}
	}
	return examples
}

// firstExample reports a command's first example line, or its first child's
// when it declares none: a group's example is what one does beneath it.
func firstExample(declared parsetree.Tree, command commandtree.Command) (string, bool) {
	if own := command.Examples(); len(own) > 0 {
		return own[0], true
	}
	for _, child := range productChildren(declared, command.Path) {
		if own := child.Examples(); len(own) > 0 {
			return own[0], true
		}
	}
	return "", false
}

// productNextStep computes the command the namespace's page names as the next
// one to run, from the state this machine records and nothing else: no module
// is launched and nothing is asked of the network, so the page stays a help
// page. Each step is the one that moves a reader to the next state, and the
// last is the module's own first command — the root's declared example, also
// returned whole as example — or its status command when it declares none.
//
// A state that cannot be read costs the page its next line, never the page.
func (s Shell) productNextStep(namespace string, root commandtree.Command,
	descriptor *modules.ProductDescriptor) (step, example string) {
	loginProvider := descriptor != nil && descriptor.Provider != ""
	setup := "Run wso2 context product add " + namespace + " --url <url> to record where the product runs."
	if loginProvider {
		setup = "Run wso2 context create <name> --login-product " + namespace +
			" --url <url> --use, then wso2 login."
	}
	selected, err := s.selection("")
	if err != nil || selected.Context.Name == "" {
		return setup, ""
	}
	if _, recorded := selected.Identity.Products[namespace]; !recorded {
		return setup, ""
	}
	ready := "Run wso2 " + namespace + " status to see what to run first."
	if examples := root.Examples(); len(examples) > 0 {
		example = examples[0]
		ready = "Run " + example + "."
	}
	if selected.Identity.Auth.Kind == contexts.KindClientCredentials {
		return ready, example
	}
	stateRoot, err := s.stateRoot()
	if err != nil {
		return ready, example
	}
	stored, err := (session.Store{StateRoot: stateRoot}).Load(selected.Identity.Auth.CredentialRef)
	switch {
	case err != nil && isNoSession(err):
		return "Run wso2 login to log in to the " + selected.Context.Name + " context.", ""
	case err != nil:
		// A secure store that cannot be asked is not a help page's to
		// diagnose; wso2 doctor is.
		return ready, example
	case !sessionServes(stored, selected.Identity.Auth.Issuer):
		return "Run wso2 login to log in to the " + selected.Context.Name + " context.", ""
	}
	if state, _, _ := sessionExpiryState(stored, time.Now()); state == whoamiSessionExpired {
		return "Run wso2 login to log in to the " + selected.Context.Name + " context again.", ""
	}
	return ready, example
}

// writeTable renders two aligned columns into the page being built.
func writeTable(page *bytes.Buffer, rows [][2]string) error {
	table := tabwriter.NewWriter(page, 0, 0, 2, ' ', 0)
	for _, row := range rows {
		// A tabwriter buffers until it is flushed, so a write to one reports
		// nothing a caller can act on. Flush is where an error appears, and it
		// is returned.
		_, _ = fmt.Fprintf(table, "  %s\t%s\n", row[0], row[1])
	}
	return table.Flush()
}

// flagSpelling renders a flag the way a user writes it.
func flagSpelling(flag commandtree.Flag) string {
	spelled := "    --" + flag.Name
	if flag.Shorthand != "" {
		spelled = "-" + flag.Shorthand + ", --" + flag.Name
	}
	if flag.TakesValue() {
		spelled += " " + flag.Type
	}
	return spelled
}

// productChildren reports the commands declared directly beneath a path, in the
// order they are declared, skipping the hidden ones an alias creates.
func productChildren(declared parsetree.Tree, path []string) []commandtree.Command {
	var children []commandtree.Command
	for _, command := range declared.Commands() {
		if len(command.Path) == len(path)+1 && slices.Equal(command.Path[:len(path)], path) {
			children = append(children, command)
		}
	}
	return children
}
