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
	"strings"

	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/parsetree"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// This file answers commands the shell used to own and no longer does, with
// the exact command that replaces each one (ADR 0016).
//
// A redirect is not an alias. It is consulted only after normal dispatch has
// declined the words — no shell command owns them, and no installed module
// declares them — so it never shadows a namespace, and a module that one day
// declares one of these words is reached exactly as it would have been. The
// refusal either would otherwise reach is wrong rather than merely unhelpful:
// "nothing owns that namespace" and "the module has no such command" both tell
// a user the command never existed, at the one moment the shell knows both
// that it did and where it went.
//
// The replacement is built from what the user typed, so a line out of shell
// history or a notebook turns into a line that runs. The exit class is usage,
// the same for a person and a script: the command did nothing, and the
// replacement is in the recovery either way.

// movedCommand answers a removed command with its replacement, and reports
// nil for anything else.
func movedCommand(namespace string, args []string, descriptor *modules.ProductDescriptor) error {
	switch {
	case namespace == "account":
		return movedAccountCommand(args)
	case len(args) > 0 && args[0] == "connect":
		return movedConnect(namespace, args[1:], descriptor)
	case namespace == "identity":
		return movedIdentityVerb(args)
	}
	return nil
}

// movedIdentityVerb answers the three verbs ADR 0015 moved from identity to
// account, which ADR 0016 then moved on to context.
func movedIdentityVerb(args []string) error {
	if len(args) == 0 {
		return nil
	}
	switch args[0] {
	case "create", "add-product", "list":
		return movedAccountCommand(args)
	}
	return nil
}

// movedAccountCommand answers every command of the retired account family.
func movedAccountCommand(args []string) error {
	verb := ""
	if len(args) > 0 {
		verb = args[0]
	}
	positional, flags := splitOldLine(args[min(1, len(args)):])
	arg := func(index int, placeholder string) string {
		if index < len(positional) {
			return positional[index]
		}
		return placeholder
	}
	var replacement string
	switch verb {
	case "", "help", "--help", "-h":
		replacement = "wso2 context --help"
	case "list":
		replacement = "wso2 context show"
	case "create":
		line := []string{"wso2 context create", arg(0, "<name>")}
		if product := flags.value("product"); product != "" {
			line = append(line, "--login-product", product, "--url", flags.valueOr("endpoint", "<url>"))
		} else {
			line = append(line, "--issuer", flags.valueOr("issuer", "<issuer-url>"),
				"--client-id", flags.valueOr("client-id", "<id>"))
		}
		line = append(line, flags.passthrough("provider", "audience", "client-secret-variable")...)
		line = append(line, flags.renamed("scope", "scopes")...)
		replacement = strings.Join(line, " ")
	case "add-product":
		line := []string{"wso2 context product add", arg(1, "<product>"),
			"--url", flags.valueOr("endpoint", "<url>")}
		line = append(line, flags.passthrough("audience", "scopes", "replace")...)
		line = append(line, "--context", arg(0, "<context>"))
		replacement = strings.Join(line, " ")
	case "remove-product":
		replacement = strings.Join([]string{"wso2 context product remove", arg(1, "<product>"),
			"--context", arg(0, "<context>")}, " ")
	case "rename":
		replacement = strings.Join([]string{"wso2 context rename", arg(0, "<name>"), arg(1, "<new-name>")}, " ")
	default:
		return nil
	}
	old := strings.TrimSpace("wso2 account " + verb)
	return problem.New(problem.CategoryUsage, "shell.command_moved",
		fmt.Sprintf("%s was removed: a context now holds its own login and products", old)).
		WithRecovery(fmt.Sprintf("Run %s instead. See wso2 context --help.", replacement))
}

// movedConnect answers wso2 <namespace> connect with the context command that
// records the same thing.
func movedConnect(namespace string, args []string, descriptor *modules.ProductDescriptor) error {
	positional, flags := splitOldLine(args)
	url := "<url>"
	if len(positional) > 0 {
		url = positional[0]
	}
	context := flags.value("account")
	if context == "" {
		context = flags.value("context")
	}
	var line []string
	switch {
	case flags.has("gateway"):
		// The old gateway line never named the product's own URL, so the
		// replacement can only hold its place. --replace is there because the
		// product was always recorded first, and adding the gateway replaces
		// its record.
		line = []string{"wso2 context product add", namespace, "--url", "<" + namespace + "-url>",
			"--gateway", url}
		line = append(line, flags.renamed("audience", "gateway-audience")...)
		line = append(line, flags.renamed("scopes", "gateway-scopes")...)
		line = append(line, "--replace")
		if context != "" {
			line = append(line, "--context", context)
		}
	case descriptor != nil && descriptor.LoginProvider():
		name := context
		if name == "" {
			name = "<name>"
		}
		line = []string{"wso2 context create", name, "--login-product", namespace, "--url", url}
		line = append(line, flags.passthrough("client-id", "client-secret-variable", "audience", "scopes")...)
		line = append(line, "--use")
	default:
		line = []string{"wso2 context product add", namespace, "--url", url}
		line = append(line, flags.passthrough("audience", "scopes", "client-id", "client-id-variable",
			"client-secret-variable", "replace")...)
		if context != "" {
			line = append(line, "--context", context)
		}
	}
	return problem.New(problem.CategoryUsage, "shell.command_moved",
		fmt.Sprintf("wso2 %s connect was removed: products are recorded on a context", namespace)).
		WithRecovery(fmt.Sprintf("Run %s instead. See wso2 context --help.", strings.Join(line, " ")))
}

// declaresCommand reports whether an installed module declares the word as a
// command of its own, in which case the word is the module's and no redirect
// answers it.
func declaresCommand(receipt modules.Receipt, word string) bool {
	declared := parsetree.FromReceipt(receipt)
	if !declared.Declared() {
		return false
	}
	routed := declared.Route([]string{word})
	return routed.Unrouted == "" && len(routed.Command.Path) > 0
}

// oldFlags are the flags of a removed command's line, by name.
type oldFlags struct {
	values map[string]string
	order  []string
}

// splitOldLine separates a removed command's arguments into its positional
// words and its flags. It knows no command's flag set, so a flag followed by a
// word that is not a flag is read as taking that word as its value, except for
// the few that never took one.
func splitOldLine(args []string) ([]string, oldFlags) {
	boolean := map[string]bool{"gateway": true, "replace": true, "no-input": true, "verbose": true}
	flags := oldFlags{values: map[string]string{}}
	var positional []string
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if !strings.HasPrefix(arg, "--") {
			positional = append(positional, arg)
			continue
		}
		name, value, attached := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		if !attached && !boolean[name] && index+1 < len(args) && !strings.HasPrefix(args[index+1], "--") {
			value = args[index+1]
			index++
		}
		if _, seen := flags.values[name]; !seen {
			flags.order = append(flags.order, name)
		}
		flags.values[name] = value
	}
	return positional, flags
}

func (f oldFlags) has(name string) bool {
	_, found := f.values[name]
	return found
}

func (f oldFlags) value(name string) string { return f.values[name] }

func (f oldFlags) valueOr(name, fallback string) string {
	if value := f.values[name]; value != "" {
		return value
	}
	return fallback
}

// passthrough renders each named flag that was given, spelled as it was.
func (f oldFlags) passthrough(names ...string) []string {
	var line []string
	for _, name := range names {
		if !f.has(name) {
			continue
		}
		if value := f.values[name]; value != "" {
			line = append(line, "--"+name, value)
		} else {
			line = append(line, "--"+name)
		}
	}
	return line
}

// renamed renders a flag that was given under the name that replaces it.
func (f oldFlags) renamed(from, to string) []string {
	if !f.has(from) {
		return nil
	}
	return []string{"--" + to, f.values[from]}
}
