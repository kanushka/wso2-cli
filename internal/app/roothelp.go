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
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/modules"
)

// coreCommands are the shell commands a user starts with: signing in, choosing
// what commands run against, and installing the products they reach. The root
// page lists them first, then the products, then every other shell command, so
// a command added without being named here is still listed, under Other.
var coreCommands = map[string]bool{
	"account": true, "context": true, "login": true, "logout": true,
	"org": true, "product": true, "whoami": true,
}

// minimumNamePadding is the narrowest name column the root page renders, the
// width Cobra pads a command name to and the one the page has always had.
const minimumNamePadding = 11

// helpRow is one line of a root page section: a command or product name and
// what it does. A row with no name is a sentence standing in for an empty
// section, and does not widen the name column.
type helpRow struct {
	name    string
	summary string
}

// helpProduct is one product the root page names.
type helpProduct struct {
	namespace string
	summary   string
	installed bool
}

// rootHelp renders the root page's command sections and its closing lines.
//
// ADR 0015: the page states what a machine can reach before anything is
// installed. Products sit between the shell commands a user starts with and
// the ones they reach for later, and the product section names every product
// this release knows a stable version of, marking those this machine has not
// installed, together with every product that is installed whether the release
// knew of it or not.
//
// Nothing is launched and nothing reaches the network. An installed product is
// read from the same receipts wso2 version reports from, and a store that
// cannot be read costs the page its marks, never the page: a broken state root
// must not take help with it.
func (s Shell) rootHelp(root *cobra.Command) (sections, footer string) {
	products, inventoryRead := s.helpProducts()
	core, other := shellCommandRows(root)
	product, closing := productRows(products, inventoryRead)

	width := minimumNamePadding
	for _, rows := range [][]helpRow{core, product, other} {
		for _, row := range rows {
			width = max(width, len(row.name))
		}
	}
	var page strings.Builder
	for _, section := range []struct {
		title string
		rows  []helpRow
	}{{"Core commands", core}, {"Product commands", product}, {"Other commands", other}} {
		fmt.Fprintf(&page, "\n%s\n", section.title)
		for _, row := range section.rows {
			if row.name == "" {
				fmt.Fprintf(&page, "   %s\n", row.summary)
				continue
			}
			line := fmt.Sprintf("   %-*s   %s", width, row.name, row.summary)
			fmt.Fprintln(&page, strings.TrimRight(line, " "))
		}
	}
	return page.String(), closing
}

// shellCommandRows splits the shell's listed commands into the core ones and
// every other one, in the order Cobra keeps them.
func shellCommandRows(root *cobra.Command) (core, other []helpRow) {
	for _, command := range root.Commands() {
		if !command.IsAvailableCommand() && command.Name() != "help" {
			continue
		}
		row := helpRow{name: command.Name(), summary: command.Short}
		if coreCommands[command.Name()] {
			core = append(core, row)
		} else {
			other = append(other, row)
		}
	}
	return core, other
}

// productRows renders the product section and the lines that close the page,
// which say how to reach what the section lists.
func productRows(products []helpProduct, inventoryRead bool) ([]helpRow, string) {
	if !inventoryRead {
		if len(products) == 0 {
			return []helpRow{{summary: "Product commands are provided by installed products."}}, ""
		}
		rows := make([]helpRow, 0, len(products))
		for _, entry := range products {
			rows = append(rows, helpRow{name: entry.namespace, summary: entry.summary})
		}
		return rows, "The installed products could not be read, so none is marked.\n"
	}
	if len(products) == 0 {
		return []helpRow{{summary: "No products are installed. Run wso2 product list to see what can be."}}, ""
	}

	rows := make([]helpRow, 0, len(products))
	anyInstalled, anyMissing := false, false
	for _, entry := range products {
		summary := entry.summary
		if entry.installed {
			anyInstalled = true
		} else {
			summary = strings.TrimSpace(summary + " (not installed)")
			anyMissing = true
		}
		rows = append(rows, helpRow{name: entry.namespace, summary: summary})
	}
	var closing strings.Builder
	if anyInstalled {
		fmt.Fprintln(&closing, "Run wso2 <product> --help to see an installed product's commands.")
	}
	if anyMissing {
		fmt.Fprintln(&closing, "Run wso2 product install <product> to install one marked not installed.")
	}
	return rows, closing.String()
}

// helpProducts reports the products the root page names, ordered by
// namespace, and whether the installed inventory could be read at all.
//
// A product the release's catalog copy knows is listed only when the copy
// knows a stable version of it, because that is the channel wso2 product
// install selects: help never advertises a product install would then fail to
// find. It is named by its title, and an installed product the copy does not
// know by its declared command tree. Both arrive from outside the shell, so
// both are printed sanitized.
func (s Shell) helpProducts() ([]helpProduct, bool) {
	released := catalog.ReleasedIndex()
	if s.ReleasedIndex != nil {
		released = *s.ReleasedIndex
	}
	entries := map[string]helpProduct{}
	for _, module := range released.Modules {
		if !module.Publishes(catalog.ChannelStable) {
			continue
		}
		entries[module.Namespace] = helpProduct{
			namespace: module.Namespace,
			summary:   catalog.SanitizedTitle(module.Title),
		}
	}

	inventoryRead := s.markInstalled(entries)

	ordered := make([]helpProduct, 0, len(entries))
	for _, entry := range entries {
		ordered = append(ordered, entry)
	}
	sort.Slice(ordered, func(left, right int) bool {
		return ordered[left].namespace < ordered[right].namespace
	})
	return ordered, inventoryRead
}

// markInstalled marks every installed product in entries, adding the ones the
// release did not know of, and reports whether the inventory could be read. A
// namespace whose receipt cannot be verified is still installed: help is not
// where that is diagnosed, and calling it not installed would send a user to
// install something that is already there.
func (s Shell) markInstalled(entries map[string]helpProduct) bool {
	store, err := s.store()
	if err != nil {
		return false
	}
	installed, problems, err := store.Inventory()
	if err != nil {
		return false
	}
	mark := func(namespace, declaredSummary string) {
		entry := entries[namespace]
		entry.namespace, entry.installed = namespace, true
		if entry.summary == "" {
			entry.summary = catalog.Printable(declaredSummary)
		}
		entries[namespace] = entry
	}
	for _, module := range installed {
		mark(module.Namespace, declaredSummary(module.Receipt))
	}
	for _, broken := range problems {
		mark(broken.Namespace, "")
	}
	return true
}

// declaredSummary is the one-line description an installed module's declared
// command tree gives its namespace, or nothing when it declares no tree.
func declaredSummary(receipt modules.Receipt) string {
	root, declared := receipt.CommandTree.Root()
	if !declared {
		return ""
	}
	return root.Short
}
