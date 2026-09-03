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
	"slices"
	"strings"
	"text/tabwriter"

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
func (s Shell) renderProductHelp(namespace string, declared parsetree.Tree,
	found commandtree.Command) error {
	path := strings.TrimSpace("wso2 " + namespace + " " + strings.Join(found.Path, " "))
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

	if len(found.Flags) > 0 {
		fmt.Fprint(&page, "\nFlags:\n")
		rows := make([][2]string, 0, len(found.Flags))
		for _, flag := range found.Flags {
			rows = append(rows, [2]string{flagSpelling(flag), flag.Usage})
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
		{"-o, --output <mode>", "Render results as table or json."},
	}); err != nil {
		return err
	}

	_, err := s.Streams.Out.Write(page.Bytes())
	return err
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
