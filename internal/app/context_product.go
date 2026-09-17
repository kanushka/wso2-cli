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
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// The way back from the product subcommands' usage refusals.
const (
	contextProductAddUsage = "Run wso2 context product add <product> --url <url> [--gateway <url>] " +
		"[--audience <uri>] [--scopes <list>] [--gateway-audience <uri>] [--gateway-scopes <list>] " +
		"[--replace] [--dry-run] [--context <name>]."
	contextProductRemoveUsage = "Run wso2 context product remove <product> [--dry-run] [--context <name>], " +
		"where <product> is a product or <product>/gateway for its gateway record alone."
)

// contextProductFlags are what wso2 context product add takes beyond the
// namespace. None of it is a credential: the variables are names.
type contextProductFlags struct {
	url, gateway, audience, gatewayAudience string
	scopes, gatewayScopes                   []string
	scopesSet, gatewayScopesSet             bool
	clientID                                string
	clientIDVariable, clientSecretVariable  string
	replace, dryRun, noInstall              bool
}

func (s Shell) contextProductCommand() *cobra.Command {
	command := &cobra.Command{
		Use:                   "product <subcommand>",
		Short:                 "Add or remove the products a context reaches.",
		Long:                  "Run wso2 context product add <product> --url <url> to record where a product runs, or wso2 context product remove <product> to stop reaching it.",
		DisableFlagsInUseLine: true,
		RunE: func(command *cobra.Command, args []string) error {
			if len(args) == 0 {
				return helpForBareFamily(command)
			}
			return problem.New(problem.CategoryUsage, "shell.unknown_command",
				fmt.Sprintf("%q is not a wso2 context product subcommand", args[0])).
				WithRecovery(contextProductAddUsage)
		},
	}
	command.AddCommand(s.contextProductAddCommand(), s.contextProductRemoveCommand())
	return command
}

func (s Shell) contextProductAddCommand() *cobra.Command {
	var flags contextProductFlags
	command := &cobra.Command{
		Use:   "add <product> --url <url>",
		Short: "Record where a product runs on the selected context, with its defaults filled in.",
		Long: "Records the product on the selected context (or the one --context names). The installed " +
			"product's descriptor supplies the audience, scopes and grant, and the complete record is " +
			"written to the context file, so a later product update changes nothing until the context " +
			"is applied again. A product that is not installed is installed first.",
		Args: exactlyOneArgument("the product to add", contextProductAddUsage),
		RunE: func(command *cobra.Command, args []string) error {
			flags.scopesSet = command.Flags().Changed("scopes")
			flags.gatewayScopesSet = command.Flags().Changed("gateway-scopes")
			return s.contextProductAdd(command, args[0], flags)
		},
	}
	f := command.Flags()
	f.StringVar(&flags.url, "url", "", "The product's URL.")
	f.StringVar(&flags.gateway, "gateway", "", "The product's gateway URL, when it has one.")
	f.StringVar(&flags.audience, "audience", "", "The token audience, when not the descriptor's default.")
	f.StringSliceVar(&flags.scopes, "scopes", nil, "The permissions to record, comma-separated, when not the descriptor's.")
	f.StringVar(&flags.gatewayAudience, "gateway-audience", "",
		"The gateway API's own resource identifier, when not the default.")
	f.StringSliceVar(&flags.gatewayScopes, "gateway-scopes", nil, "The gateway API's own permissions.")
	f.StringVar(&flags.clientID, "client-id", "",
		"The product's own public client, for a product reached through its own issuer.")
	f.StringVar(&flags.clientIDVariable, "client-id-variable", "",
		"The environment variable holding the product credential's client id (client-credentials contexts).")
	f.StringVar(&flags.clientSecretVariable, "client-secret-variable", "",
		"The environment variable holding the product's own client secret (client-credentials contexts).")
	f.BoolVar(&flags.replace, "replace", false,
		"Replace the product's existing record, ending the sessions it no longer matches.")
	f.BoolVar(&flags.dryRun, "dry-run", false, "Show the record and what would change, and write nothing.")
	f.BoolVar(&flags.noInstall, "no-install", false, "Refuse rather than install a product that is not installed.")
	declareContextFlag(f)
	return command
}

func (s Shell) contextProductRemoveCommand() *cobra.Command {
	var dryRun bool
	command := &cobra.Command{
		Use:   "remove <product>",
		Short: "Stop the selected context reaching a product, ending the product's own sessions first.",
		Args:  exactlyOneArgument("the product to remove", contextProductRemoveUsage),
		RunE: func(command *cobra.Command, args []string) error {
			return s.contextProductRemove(command, args[0], dryRun)
		},
	}
	command.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be removed and ended, and change nothing.")
	declareContextFlag(command.Flags())
	return command
}

// targetContextName is the context a product subcommand acts on: --context,
// else WSO2_CONTEXT, else the selected one. The document is what refuses a
// name it does not declare.
func targetContextName(command *cobra.Command, document contexts.Document) (string, error) {
	name := ""
	if flag := shellFlag(command, contextFlag); flag != nil {
		name = flag.Value.String()
	}
	if name == "" {
		name = os.Getenv("WSO2_CONTEXT")
	}
	selected, err := document.Select(name)
	if err != nil {
		return "", err
	}
	if selected.Context.Name == "" {
		return "", problem.New(problem.CategoryUsage, "contexts.no_context_selected",
			"no context exists to add the product to").
			WithRecovery(contextSetupHint)
	}
	return selected.Context.Name, nil
}

// contextProductAdd records one product on one context.
func (s Shell) contextProductAdd(command *cobra.Command, namespace string, flags contextProductFlags) error {
	mode, err := s.shellOutputMode(command)
	if err != nil {
		return err
	}
	if !contexts.ValidName(namespace) {
		return problem.New(problem.CategoryUsage, "shell.invalid_argument",
			fmt.Sprintf("%q cannot be a product namespace", namespace)).WithRecovery(contextProductAddUsage)
	}
	if flags.url == "" {
		return problem.New(problem.CategoryUsage, "shell.missing_required_flag",
			fmt.Sprintf("wso2 context product add %s needs --url with the product's URL", namespace)).
			WithRecovery(contextProductAddUsage)
	}
	spec, err := productSpecFromFlags(flags)
	if err != nil {
		return err
	}
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	document, err := contexts.Load(root)
	if err != nil {
		return err
	}
	name, err := targetContextName(command, document)
	if err != nil {
		return err
	}

	installed := ""
	pending := false
	if flags.dryRun {
		product, err := s.installedProduct(namespace)
		if err != nil {
			return err
		}
		pending = product == nil
	} else {
		installed, err = s.ensureInstalled(namespace, flags.noInstall)
		if err != nil {
			return err
		}
	}
	lookup := s.installedDescriptors()
	var added contexts.Product
	replaced := false
	change := func(document contexts.Document) (contexts.Document, error) {
		target, found := document.Find(name)
		if !found {
			return document, problem.New(problem.CategoryUsage, "contexts.unknown_context",
				fmt.Sprintf("no context named %q is configured", name)).WithRecovery(contextListUsage)
		}
		_, replaced = target.Products[namespace]
		if replaced && !flags.replace {
			return document, productExists(name, namespace)
		}
		// Frozen before the product goes in: the login keeps running for the
		// product it ran for, whatever this one's namespace sorts as.
		target = target.Freeze()
		product, err := resolveProduct(namespace, lookup(namespace), spec, target)
		if err != nil {
			return document, err
		}
		added = product
		products := make(map[string]contexts.Product, len(target.Products)+1)
		for key, value := range target.Products {
			products[key] = value
		}
		products[namespace] = product
		target.Products = products
		return document.Put(target), nil
	}
	if pending {
		return s.reportPendingProduct(mode, name, namespace, spec)
	}
	plan, err := s.planChange(root, change)
	if err != nil {
		return err
	}
	report := productAdded{
		Context: name, Product: namespace, URL: added.Endpoint, Audience: added.Audience,
		Scopes: added.Scopes, Replaced: replaced, Installed: installed, DryRun: flags.dryRun,
		Ending: endingLines(plan.before, plan.ending), Sessions: []endedSession{},
	}
	if report.Scopes == nil {
		report.Scopes = []string{}
	}
	if added.Grant != nil {
		report.Grant = added.Grant.Kind
	}
	if added.Gateway != nil {
		report.GatewayURL, report.GatewayAudience = added.Gateway.Endpoint, added.Gateway.Audience
	}
	context, _ := plan.after.Find(name)
	access, _ := context.Account().Access(namespace)
	report.Strategy = access.Strategy
	if !flags.dryRun {
		ended, err := s.writeChange(root, plan)
		if err != nil {
			return err
		}
		report.Sessions = ended
		if report.Sessions == nil {
			report.Sessions = []endedSession{}
		}
	}
	report.nextLine = s.productNext(root, context, namespace, plan.after.DefaultContext == name)
	if mode == output.ModeJSON {
		return renderContext(s.Streams.Out, mode, report)
	}
	verb := "Added"
	switch {
	case flags.dryRun:
		verb = "Would add"
		if replaced {
			verb = "Would replace"
		}
	case replaced:
		verb = "Replaced"
	}
	if _, err := fmt.Fprintf(s.Streams.Out, "\n%s the %q product on the %q context.\n\n", verb, namespace,
		name); err != nil {
		return err
	}
	if err := renderContext(s.Streams.Out, mode, report); err != nil {
		return err
	}
	for _, note := range endedNotes(report.Sessions) {
		if _, err := fmt.Fprintf(s.Streams.Out, "\n%s\n", note); err != nil {
			return err
		}
	}
	return nil
}

// reportPendingProduct is --dry-run's report for a product that is not
// installed: nothing can be resolved without its descriptor, so the report
// says what would be installed and what the record is known to hold.
func (s Shell) reportPendingProduct(mode output.Mode, context, namespace string, spec productSpec) error {
	report := productAdded{Context: context, Product: namespace, URL: spec.URL, DryRun: true,
		Install: namespace, Scopes: []string{}, Ending: []string{}, Sessions: []endedSession{}}
	if spec.Gateway != nil {
		report.GatewayURL = spec.Gateway.URL
	}
	report.nextLine = fmt.Sprintf("Run the command without --dry-run to install %s and write the record. "+
		"The defaults are resolved after install.", namespace)
	if mode == output.ModeJSON {
		return renderContext(s.Streams.Out, mode, report)
	}
	if _, err := fmt.Fprintf(s.Streams.Out, "\nWould install the %s product, then add it to the %q "+
		"context.\n\n", namespace, context); err != nil {
		return err
	}
	return renderContext(s.Streams.Out, mode, report)
}

// productSpecFromFlags reads what the add line states about the product.
func productSpecFromFlags(flags contextProductFlags) (productSpec, error) {
	url, err := productURL("--url", flags.url)
	if err != nil {
		return productSpec{}, err
	}
	spec := productSpec{URL: url, Audience: flags.audience, ClientID: flags.clientID,
		ClientIDVariable: flags.clientIDVariable, ClientSecretVariable: flags.clientSecretVariable}
	if flags.scopesSet {
		spec.Scopes = flags.scopes
	}
	for _, named := range []struct{ flag, value string }{
		{"--client-secret-variable", flags.clientSecretVariable},
		{"--client-id-variable", flags.clientIDVariable},
	} {
		if named.value != "" && !contexts.ValidVariable(named.value) {
			return productSpec{}, problem.New(problem.CategoryUsage, "shell.invalid_argument",
				fmt.Sprintf("%s does not name an environment variable", named.flag)).
				WithRecovery("Pass the name of the variable holding the credential, not the credential. " +
					"The value is not repeated here, in case it is the secret itself.")
		}
	}
	if flags.clientIDVariable != "" && flags.clientSecretVariable == "" {
		return productSpec{}, problem.New(problem.CategoryUsage, "shell.missing_required_flag",
			"--client-id-variable names half a credential and needs --client-secret-variable").
			WithRecovery(contextProductAddUsage)
	}
	if flags.gateway == "" && (flags.gatewayAudience != "" || flags.gatewayScopesSet) {
		return productSpec{}, problem.New(problem.CategoryUsage, "shell.missing_required_flag",
			"--gateway-audience and --gateway-scopes describe a gateway and need --gateway <url>").
			WithRecovery(contextProductAddUsage)
	}
	if flags.gateway != "" {
		gateway, err := productURL("--gateway", flags.gateway)
		if err != nil {
			return productSpec{}, err
		}
		spec.Gateway = &gatewaySpec{URL: gateway, Audience: flags.gatewayAudience}
		if flags.gatewayScopesSet {
			spec.Gateway.Scopes = flags.gatewayScopes
		}
	}
	return spec, nil
}

// productNext is the command that authorizes the product just recorded.
func (s Shell) productNext(root string, context contexts.Context, namespace string, selected bool) string {
	flag := " --context " + context.Name
	if selected {
		flag = ""
	}
	if context.Login.Kind == contexts.KindClientCredentials {
		return fmt.Sprintf("Run wso2 %s --help%s. A client-credentials context needs no login.", namespace, flag)
	}
	account := context.Account()
	store := session.Store{StateRoot: root}
	login := account.LoginAccess()
	if _, err := store.Load(login.SessionRef); err != nil {
		return fmt.Sprintf("Run wso2 login%s.", flag)
	}
	access, _ := account.Access(namespace)
	if access.Strategy == contexts.StrategyExchanged || access.Strategy == contexts.StrategyDirect {
		return fmt.Sprintf("Run wso2 %s --help. The context's login session already reaches it.", namespace)
	}
	return fmt.Sprintf("Run wso2 login --only %s%s.", namespace, flag)
}

// productExists refuses a second record of one product without --replace.
func productExists(context, namespace string) problem.Problem {
	return problem.New(problem.CategoryUsage, "contexts.product_exists",
		fmt.Sprintf("the context %q already records the %q product", context, namespace)).
		WithRecovery("Run wso2 context show to see what it records. Pass --replace to overwrite the " +
			"record, which replaces the whole of it and ends the sessions it no longer matches.")
}

// contextProductRemove drops one record from a context, ending every session
// the removal leaves nothing to reach before the record goes.
func (s Shell) contextProductRemove(command *cobra.Command, key string, dryRun bool) error {
	mode, err := s.shellOutputMode(command)
	if err != nil {
		return err
	}
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	document, err := contexts.Load(root)
	if err != nil {
		return err
	}
	name, err := targetContextName(command, document)
	if err != nil {
		return err
	}
	var records []string
	change := func(document contexts.Document) (contexts.Document, error) {
		target, found := document.Find(name)
		if !found {
			return document, problem.New(problem.CategoryUsage, "contexts.unknown_context",
				fmt.Sprintf("no context named %q is configured", name)).WithRecovery(contextListUsage)
		}
		target = target.Freeze()
		if target.Login.Kind != contexts.KindClientCredentials && key == target.Login.Product {
			return document, loginProductRemoval(target, key)
		}
		remaining, removed := target.WithoutRecord(key)
		if !removed {
			return document, recordNotHeld(name, key, target.Account().RecordKeys())
		}
		kept := remaining.Account().RecordKeys()
		records = slices.DeleteFunc(target.Account().RecordKeys(), func(candidate string) bool {
			return slices.Contains(kept, candidate)
		})
		return document.Put(remaining), nil
	}
	plan, err := s.planChange(root, change)
	if err != nil {
		return err
	}
	report := productRemoved{Context: name, Product: key, Records: records, DryRun: dryRun,
		Ending: endingLines(plan.before, plan.ending), Sessions: []endedSession{}}
	if !dryRun {
		ended, err := s.writeChange(root, plan)
		if err != nil {
			return err
		}
		if ended != nil {
			report.Sessions = ended
		}
	}
	if mode == output.ModeJSON {
		return renderContext(s.Streams.Out, mode, report)
	}
	verb := "Removed"
	if dryRun {
		verb = "Would remove"
	}
	line := fmt.Sprintf("%s the %q product from the %q context.", verb, key, name)
	if namespace, gateway := contexts.SplitGatewayKey(key); gateway {
		line = fmt.Sprintf("%s the gateway record of the %q product from the %q context.", verb, namespace, name)
	}
	if _, err := fmt.Fprintf(s.Streams.Out, "\n%s\n\n", line); err != nil {
		return err
	}
	if err := renderContext(s.Streams.Out, mode, report); err != nil {
		return err
	}
	for _, note := range endedNotes(report.Sessions) {
		if _, err := fmt.Fprintf(s.Streams.Out, "\n%s\n", note); err != nil {
			return err
		}
	}
	return nil
}

// loginProductRemoval refuses to remove the product a context logs in
// through: the login session was authorized for it, and every other product's
// session is obtained through that one.
func loginProductRemoval(context contexts.Context, key string) error {
	others := slices.DeleteFunc(context.Account().RecordKeys(), func(candidate string) bool {
		return candidate == key
	})
	recovery := "To log in through another product, create a context that logs in through it: " +
		"wso2 context create <name> --login-product <product> --url <url>."
	if len(others) > 0 {
		recovery += fmt.Sprintf(" The context's other records can be removed: %s.", strings.Join(others, ", "))
	}
	return problem.New(problem.CategoryUsage, "contexts.login_product",
		fmt.Sprintf("the context %q logs in through %q, so removing it would leave the login session "+
			"authorized for a product the context no longer records", context.Name, key)).
		WithRecovery(recovery)
}

// recordNotHeld refuses to remove a record the context does not hold, naming
// every one it does.
func recordNotHeld(context, key string, recorded []string) problem.Problem {
	holds := "It records no products."
	if len(recorded) > 0 {
		holds = "It records " + strings.Join(recorded, ", ") + "."
	}
	return problem.New(problem.CategoryUsage, "contexts.unknown_product",
		fmt.Sprintf("the context %q records nothing under %q", context, key)).
		WithRecovery(holds + " Run wso2 context show to see what each context reaches.")
}

// The results the product subcommands report.
type (
	productAdded struct {
		Context         string         `json:"context"`
		Product         string         `json:"product"`
		URL             string         `json:"url"`
		Audience        string         `json:"audience"`
		Scopes          []string       `json:"scopes"`
		Grant           string         `json:"grant"`
		Strategy        string         `json:"strategy"`
		GatewayURL      string         `json:"gatewayUrl"`
		GatewayAudience string         `json:"gatewayAudience"`
		Replaced        bool           `json:"replaced"`
		Installed       string         `json:"installed"`
		Install         string         `json:"install,omitempty"`
		DryRun          bool           `json:"dryRun"`
		Ending          []string       `json:"ending"`
		Sessions        []endedSession `json:"sessions"`
		nextLine        string
	}

	productRemoved struct {
		Context  string         `json:"context"`
		Product  string         `json:"product"`
		Records  []string       `json:"records"`
		DryRun   bool           `json:"dryRun"`
		Ending   []string       `json:"ending"`
		Sessions []endedSession `json:"sessions"`
	}
)

func (p productAdded) fields() [][2]string {
	fields := [][2]string{
		{"Context", p.Context},
		{"Product", p.Product},
		{"URL", p.URL},
		{"Audience", p.Audience},
		{"Scopes", strings.Join(p.Scopes, ",")},
		{"Grant", p.Grant},
		{"Strategy", p.Strategy},
	}
	if p.GatewayURL != "" {
		fields = append(fields, [2]string{"Gateway URL", p.GatewayURL},
			[2]string{"Gateway audience", p.GatewayAudience})
	}
	if p.Installed != "" {
		fields = append(fields, [2]string{"Installed", p.Product + " v" + p.Installed})
	}
	fields = append(fields, [2]string{"Sessions ending", sessionsCell(p.Ending, p.Sessions, p.DryRun)})
	return fields
}

func (p productAdded) next() string { return p.nextLine }

func (p productRemoved) fields() [][2]string {
	return [][2]string{
		{"Context", p.Context},
		{"Product", p.Product},
		{"Records removed", strings.Join(p.Records, ", ")},
		{"Sessions ending", sessionsCell(p.Ending, p.Sessions, p.DryRun)},
	}
}

// sessionsCell is the table cell naming what a change ends: the plan under
// --dry-run, what was ended otherwise.
func sessionsCell(ending []string, ended []endedSession, dryRun bool) string {
	if dryRun {
		if len(ending) == 0 {
			return "none"
		}
		return strings.Join(ending, "; ")
	}
	if len(ended) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(ended))
	for _, session := range ended {
		record := session.Record
		if record == "" {
			record = "login"
		}
		if session.Session == "ended" {
			parts = append(parts, fmt.Sprintf("%s (ended, revocation %s)", record, session.Revocation))
		} else {
			parts = append(parts, record+" (nothing stored)")
		}
	}
	return strings.Join(parts, ", ")
}
