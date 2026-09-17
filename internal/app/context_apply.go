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
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// The way back from apply's and export's usage refusals.
const (
	contextApplyUsage = "Run wso2 context apply -f <file> [--use <name>] [--dry-run] [--no-install] " +
		"[--update-products]."
	contextExportUsage = "Run wso2 context export [<name>] > <file>."
)

// inputFile is the shareable file wso2 context apply reads: contexts only, each
// as short as the installed descriptors allow. It never selects a context and
// never names a credential reference; those belong to one machine.
type inputFile struct {
	// SchemaVersion is optional, and when given must be the current one.
	SchemaVersion int            `json:"schemaVersion,omitempty"`
	Contexts      []inputContext `json:"contexts"`
}

// inputContext is one context as an input file states it.
type inputContext struct {
	Name         string                  `json:"name"`
	Type         string                  `json:"type,omitempty"`
	Login        contexts.Login          `json:"login"`
	Organization string                  `json:"organization,omitempty"`
	Project      string                  `json:"project,omitempty"`
	Products     map[string]inputProduct `json:"products,omitempty"`
}

// inputProduct is one product as an input file states it: the record's own
// members, every one but url optional, and the version an install pins.
type inputProduct struct {
	URL                  string            `json:"url"`
	Audience             string            `json:"audience,omitempty"`
	Scopes               []string          `json:"scopes,omitempty"`
	Grant                *contexts.Grant   `json:"grant,omitempty"`
	ClientIDVariable     string            `json:"clientIdVariable,omitempty"`
	ClientSecretVariable string            `json:"clientSecretVariable,omitempty"`
	Gateway              *contexts.Gateway `json:"gateway,omitempty"`
	Version              string            `json:"version,omitempty"`
}

// applyFlags are what wso2 context apply takes.
type applyFlags struct {
	file, use                         string
	dryRun, noInstall, updateProducts bool
}

func (s Shell) contextApplyCommand() *cobra.Command {
	var flags applyFlags
	command := &cobra.Command{
		Use:   "apply -f <file>",
		Short: "Create or replace contexts from a shared file, installing the products they need.",
		Long: "Reads a context file your platform team shares and writes each context in it to this " +
			"machine's context document. A context is matched by name and replaced whole; contexts the " +
			"file does not name are kept. Missing products are installed first, their defaults are " +
			"resolved and written in full, and sessions a replaced context no longer matches are ended.\n\n" +
			"The file never selects a context: pass --use <name> for that. Nothing is written until " +
			"every product is installed and every record resolves; --dry-run shows the plan and stops.",
		Args: noArguments(contextApplyUsage),
		RunE: func(command *cobra.Command, args []string) error {
			return s.contextApply(command, flags)
		},
	}
	f := command.Flags()
	f.StringVarP(&flags.file, "file", "f", "", "The context file to apply, or - for standard input.")
	f.StringVar(&flags.use, "use", "", "Select this context from the file once it is applied.")
	f.BoolVar(&flags.dryRun, "dry-run", false, "Show what would be installed, written and ended, and change nothing.")
	f.BoolVar(&flags.noInstall, "no-install", false,
		"Install nothing: every product must be installed already, or stated in full in the file.")
	f.BoolVar(&flags.updateProducts, "update-products", false,
		"Install the version the file pins for a product installed at another version.")
	return command
}

func (s Shell) contextExportCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "export [<name>]",
		Short: "Print contexts as a shareable file: complete records, with nothing machine-specific.",
		Args:  atMostOneArgument(contextExportUsage),
		RunE: func(command *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			return s.contextExport(name)
		},
	}
}

// readInputFile reads and validates an input file without side effects: its
// shape, its names, its URLs, and the references between its members.
func (s Shell) readInputFile(path string) (inputFile, error) {
	if path == "" {
		return inputFile{}, problem.New(problem.CategoryUsage, "shell.missing_required_flag",
			"wso2 context apply needs -f <file>").WithRecovery(contextApplyUsage)
	}
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(s.reader())
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return inputFile{}, problem.New(problem.CategoryUsage, "shell.input_unreadable",
			fmt.Sprintf("the context file %s cannot be read", path)).
			WithRecovery("Check the path and that the file is readable. " + contextApplyUsage)
	}
	return decodeInputFile(data)
}

// decodeInputFile is the strict decode of an input file. Unknown members are
// refused: the file is written by hand, and a misspelled member silently
// ignored would be a default nobody asked for.
func decodeInputFile(data []byte) (inputFile, error) {
	var probe struct {
		DefaultContext *string `json:"defaultContext"`
		Contexts       []struct {
			CredentialRef *string `json:"credentialRef"`
		} `json:"contexts"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return inputFile{}, inputProblem("is not valid JSON: " + err.Error())
	}
	if probe.DefaultContext != nil {
		return inputFile{}, problem.New(problem.CategoryUsage, "shell.input_malformed",
			"the context file selects a context (defaultContext), and a shared file never does").
			WithRecovery("Remove defaultContext from the file and pass --use <name> to select one.")
	}
	for _, context := range probe.Contexts {
		if context.CredentialRef != nil {
			return inputFile{}, problem.New(problem.CategoryUsage, "shell.input_malformed",
				"the context file names a credentialRef, which belongs to one machine's secure store").
				WithRecovery("Remove credentialRef from the file; apply keeps a replaced context's own and " +
					"assigns one to a new context. wso2 context export writes files without it.")
		}
	}
	var file inputFile
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return inputFile{}, inputProblem(err.Error())
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return inputFile{}, inputProblem("contains more than one JSON document")
	}
	if file.SchemaVersion != 0 && file.SchemaVersion != contexts.SchemaVersion {
		return inputFile{}, inputProblem(fmt.Sprintf("declares schema version %d, and this shell reads %d",
			file.SchemaVersion, contexts.SchemaVersion))
	}
	if len(file.Contexts) == 0 {
		return inputFile{}, inputProblem("declares no contexts")
	}
	seen := map[string]bool{}
	for _, context := range file.Contexts {
		if !contexts.ValidName(context.Name) {
			return inputFile{}, inputProblem(fmt.Sprintf("names a context %q, and a context name is %s",
				context.Name, contexts.NameRule))
		}
		if seen[context.Name] {
			return inputFile{}, inputProblem(fmt.Sprintf("declares the context %q more than once", context.Name))
		}
		seen[context.Name] = true
		if err := context.validate(); err != nil {
			return inputFile{}, err
		}
	}
	return file, nil
}

// validate checks what one input context can be checked for without a
// descriptor.
func (c inputContext) validate() error {
	for _, namespace := range slices.Sorted(maps.Keys(c.Products)) {
		if !contexts.ValidName(namespace) {
			return inputProblem(fmt.Sprintf("names a product %q on the context %q, which is not a namespace",
				namespace, c.Name))
		}
		product := c.Products[namespace]
		if product.URL == "" {
			return inputProblem(fmt.Sprintf("gives the %q product on the context %q no url", namespace, c.Name))
		}
		if _, err := productURL(fmt.Sprintf("%s url on %s", namespace, c.Name), product.URL); err != nil {
			return err
		}
		if product.Gateway != nil {
			if _, err := productURL(fmt.Sprintf("%s gateway url on %s", namespace, c.Name),
				product.Gateway.Endpoint); err != nil {
				return err
			}
		}
	}
	switch {
	case c.Login.Product != "":
		if _, recorded := c.Products[c.Login.Product]; !recorded {
			return inputProblem(fmt.Sprintf("logs the context %q in through the %q product, which it does "+
				"not list under products", c.Name, c.Login.Product))
		}
	case c.Login.Issuer == "" || c.Login.ClientID == "":
		return inputProblem(fmt.Sprintf("gives the context %q neither a login product nor an issuer and "+
			"client id to log in through", c.Name))
	}
	if c.Login.Issuer != "" {
		if err := refuseNonIssuerURL(c.Login.Issuer); err != nil {
			return err
		}
	}
	return nil
}

func inputProblem(detail string) problem.Problem {
	return problem.New(problem.CategoryUsage, "shell.input_malformed", "the context file "+detail).
		WithRecovery("Correct the file and run the command again. Nothing was installed or written. " +
			"docs/reference/context-file.md states every member the file may carry.")
}

// wanted is every product the input needs installed, with its pin.
func (f inputFile) wanted() []installNeed {
	var needs []installNeed
	for _, context := range f.Contexts {
		for _, namespace := range slices.Sorted(maps.Keys(context.Products)) {
			needs = append(needs, installNeed{Namespace: namespace, Version: context.Products[namespace].Version})
		}
	}
	return needs
}

// resolve builds the complete context an input context describes, from the
// installed descriptors. The credential reference is left for the caller.
func (c inputContext) resolve(lookup descriptorLookup) (contexts.Context, error) {
	login := c.Login
	if login.Kind == "" {
		login.Kind = contexts.KindOAuthBrowser
		if login.ClientSecretVariable != "" {
			login.Kind = contexts.KindClientCredentials
		}
	}
	products := map[string]contexts.Product{}
	if login.Product != "" {
		input := c.Products[login.Product]
		descriptor := lookup(login.Product)
		spec := input.spec()
		if descriptor == nil {
			// Stated in full, as --no-install requires: taken as written.
			product, err := resolveProduct(login.Product, nil, spec, contexts.Context{})
			if err != nil {
				return contexts.Context{}, err
			}
			if login.Issuer == "" || login.ClientID == "" {
				return contexts.Context{}, inputProblem(fmt.Sprintf("logs the context %q in through the "+
					"%q product, which is not installed, and states no issuer and client id",
					c.Name, login.Product))
			}
			products[login.Product] = product
		} else {
			resolvedLogin, product, err := resolveLoginProduct(login.Product, descriptor, spec, login)
			if err != nil {
				return contexts.Context{}, err
			}
			if err := resolveGateway(login.Product, descriptor, spec, contexts.Context{Name: c.Name,
				Login: resolvedLogin}, &product); err != nil {
				return contexts.Context{}, err
			}
			login = resolvedLogin
			products[login.Product] = product
		}
	}
	if login.Tenant == "" {
		login.Tenant = contexts.TenantForIssuer(login.Issuer)
	}
	resolved := contexts.Context{
		Name: c.Name, Type: c.Type, Login: login, Organization: c.Organization, Project: c.Project,
	}
	if resolved.Type == "" {
		resolved.Type = contexts.IdentityTypeForIssuer(login.Issuer)
	}
	for _, namespace := range slices.Sorted(maps.Keys(c.Products)) {
		if namespace == login.Product {
			continue
		}
		product, err := resolveProduct(namespace, lookup(namespace), c.Products[namespace].spec(), resolved)
		if err != nil {
			return contexts.Context{}, err
		}
		products[namespace] = product
	}
	if len(products) > 0 {
		resolved.Products = products
	}
	return resolved.Freeze(), nil
}

// spec is the input product as the resolver reads it.
func (p inputProduct) spec() productSpec {
	spec := productSpec{URL: strings.TrimRight(p.URL, "/"), Audience: p.Audience, Scopes: p.Scopes,
		Grant: p.Grant, ClientIDVariable: p.ClientIDVariable, ClientSecretVariable: p.ClientSecretVariable,
		Version: p.Version}
	if p.Gateway != nil {
		spec.Gateway = &gatewaySpec{URL: strings.TrimRight(p.Gateway.Endpoint, "/"), Audience: p.Gateway.Audience,
			Scopes: p.Gateway.Scopes}
	}
	return spec
}

// contextApply writes each context in the input file, in the order of
// operations ADR 0016 fixes: validate the whole input, plan, stop for
// --dry-run, install, resolve, end the sessions whose bindings change, and
// write the document under its lock. Nothing is written until everything
// before the write has succeeded.
func (s Shell) contextApply(command *cobra.Command, flags applyFlags) error {
	mode, err := s.shellOutputMode(command)
	if err != nil {
		return err
	}
	file, err := s.readInputFile(flags.file)
	if err != nil {
		return err
	}
	if flags.use != "" && !slices.ContainsFunc(file.Contexts, func(c inputContext) bool { return c.Name == flags.use }) {
		return problem.New(problem.CategoryUsage, "shell.invalid_argument",
			fmt.Sprintf("--use %s names a context the file does not declare", flags.use)).
			WithRecovery("Pass --use with one of the file's contexts, or select another later with wso2 " +
				"context use <name>.")
	}
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	if err := contexts.Writable(root); err != nil {
		return s.explainWriteRefusal(root, err)
	}

	installs, err := s.planInstalls(file.wanted())
	if err != nil {
		return err
	}
	report := applyReport{DryRun: flags.dryRun, Install: []string{}, Mismatched: []string{},
		Contexts: []applyEntry{}, Ending: []string{}, Sessions: []endedSession{}}
	for _, need := range installs.Missing {
		report.Install = append(report.Install, need.String())
	}
	for _, need := range installs.Mismatched {
		report.Mismatched = append(report.Mismatched, fmt.Sprintf("%s (installed v%s, file pins v%s)",
			need.Namespace, need.Installed, need.Version))
	}
	if flags.noInstall {
		report.Install = []string{}
	}

	// A dry run with products still to install cannot resolve their
	// defaults, so it reports the contexts by name and stops.
	if flags.dryRun && len(installs.Missing) > 0 && !flags.noInstall {
		document, err := contexts.Load(root)
		if err != nil {
			return err
		}
		for _, context := range file.Contexts {
			action := "create"
			if declaresContext(document, context.Name) {
				action = "replace"
			}
			report.Contexts = append(report.Contexts, applyEntry{Name: context.Name, Action: action,
				Changes: []string{"resolved after install"}})
		}
		return s.reportApply(mode, report, flags)
	}

	if !flags.dryRun {
		toInstall := installs.Missing
		if flags.noInstall {
			toInstall = nil
		}
		if flags.updateProducts {
			toInstall = append(toInstall, installs.Mismatched...)
		}
		if err := s.install(toInstall); err != nil {
			return err
		}
	}

	lookup := s.installedDescriptors()
	resolved := make([]contexts.Context, 0, len(file.Contexts))
	for _, context := range file.Contexts {
		built, err := context.resolve(lookup)
		if err != nil {
			return err
		}
		resolved = append(resolved, built)
	}

	plan, err := s.planChange(root, func(document contexts.Document) (contexts.Document, error) {
		for _, context := range resolved {
			existing, found := document.Find(context.Name)
			switch {
			case found:
				context.CredentialRef = existing.CredentialRef
			default:
				context.CredentialRef = document.NewCredentialRef(context.Name)
			}
			if context.Login.Kind == contexts.KindClientCredentials {
				context.CredentialRef = ""
			} else if context.CredentialRef == "" {
				context.CredentialRef = document.NewCredentialRef(context.Name)
			}
			entry := applyEntry{Name: context.Name, Action: "create", Changes: []string{}}
			if found {
				entry.Action = "replace"
				entry.Changes = contextChanges(existing.Freeze(), context)
				if len(entry.Changes) == 0 {
					entry.Action = "unchanged"
				}
			}
			report.Contexts = append(report.Contexts, entry)
			document = document.Put(context)
		}
		if flags.use != "" {
			document.DefaultContext = flags.use
		}
		return document, nil
	})
	if err != nil {
		return err
	}
	report.Ending = endingLines(plan.before, plan.ending)
	report.Selected = plan.after.DefaultContext
	if !flags.dryRun {
		ended, err := s.writeChange(root, plan)
		if err != nil {
			return err
		}
		if ended != nil {
			report.Sessions = ended
		}
	}
	return s.reportApply(mode, report, flags)
}

// contextChanges lists, one line per member, how a replaced context differs
// from the one it replaces. The credential reference never changes and is not
// compared.
func contextChanges(before, after contexts.Context) []string {
	before.CredentialRef, after.CredentialRef = "", ""
	left, right := flatten(before), flatten(after)
	keys := map[string]bool{}
	for key := range left {
		keys[key] = true
	}
	for key := range right {
		keys[key] = true
	}
	var changes []string
	for _, key := range slices.Sorted(maps.Keys(keys)) {
		was, is := left[key], right[key]
		if reflect.DeepEqual(was, is) {
			continue
		}
		switch {
		case was == "":
			changes = append(changes, fmt.Sprintf("%s: set to %s", key, is))
		case is == "":
			changes = append(changes, fmt.Sprintf("%s: removed (was %s)", key, was))
		default:
			changes = append(changes, fmt.Sprintf("%s: %s -> %s", key, was, is))
		}
	}
	return changes
}

// flatten renders a context as dotted member paths and their values.
func flatten(context contexts.Context) map[string]string {
	data, _ := json.Marshal(context)
	var tree any
	_ = json.Unmarshal(data, &tree)
	flat := map[string]string{}
	var walk func(prefix string, value any)
	walk = func(prefix string, value any) {
		switch typed := value.(type) {
		case map[string]any:
			for key, child := range typed {
				path := key
				if prefix != "" {
					path = prefix + "." + key
				}
				walk(path, child)
			}
		case []any:
			parts := make([]string, 0, len(typed))
			for _, item := range typed {
				parts = append(parts, fmt.Sprint(item))
			}
			flat[prefix] = strings.Join(parts, ",")
		default:
			flat[prefix] = fmt.Sprint(typed)
		}
	}
	walk("", tree)
	delete(flat, "name")
	return flat
}

// applyReport is what wso2 context apply reports.
type applyReport struct {
	DryRun     bool           `json:"dryRun"`
	Install    []string       `json:"install"`
	Mismatched []string       `json:"mismatched"`
	Contexts   []applyEntry   `json:"contexts"`
	Ending     []string       `json:"ending"`
	Sessions   []endedSession `json:"sessions"`
	Selected   string         `json:"selected"`
}

// applyEntry is one context the file names and what applying does to it.
type applyEntry struct {
	Name    string   `json:"name"`
	Action  string   `json:"action"`
	Changes []string `json:"changes"`
}

// reportApply renders the plan, or what was done.
func (s Shell) reportApply(mode output.Mode, report applyReport, flags applyFlags) error {
	if mode == output.ModeJSON {
		return encodeContextJSON(s.Streams.Out, report)
	}
	w := s.Streams.Out
	heading := "Applied the context file."
	if report.DryRun {
		heading = "Plan (dry run: nothing installed, written or ended)."
	}
	if _, err := fmt.Fprintf(w, "\n%s\n", heading); err != nil {
		return err
	}
	if len(report.Install) > 0 {
		verb := "Installed"
		if report.DryRun {
			verb = "Would install"
		}
		if _, err := fmt.Fprintf(w, "\n%s: %s\n", verb, strings.Join(report.Install, ", ")); err != nil {
			return err
		}
	}
	if len(report.Mismatched) > 0 {
		note := "left as installed; pass --update-products to install the pinned version"
		if flags.updateProducts {
			note = "updated to the pinned version"
		}
		if _, err := fmt.Fprintf(w, "\nVersion differs from the file: %s (%s)\n",
			strings.Join(report.Mismatched, ", "), note); err != nil {
			return err
		}
	}
	table := output.NewTable("context", "action", "changes")
	for _, entry := range report.Contexts {
		changes := "-"
		if len(entry.Changes) > 0 {
			changes = strings.Join(entry.Changes, "; ")
		}
		table.Append(entry.Name, entry.Action, changes)
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if err := table.Render(w); err != nil {
		return err
	}
	ending := sessionsCell(report.Ending, report.Sessions, report.DryRun)
	if _, err := fmt.Fprintf(w, "\nSessions ending: %s\n", ending); err != nil {
		return err
	}
	for _, note := range endedNotes(report.Sessions) {
		if _, err := fmt.Fprintf(w, "\n%s\n", note); err != nil {
			return err
		}
	}
	var next string
	switch {
	case report.DryRun:
		next = "Run the command without --dry-run to apply it."
	case report.Selected == "":
		next = "No context is selected. Run wso2 context use <name>, then wso2 login."
	case flags.use != "":
		next = "Run wso2 login."
	default:
		next = fmt.Sprintf("Run wso2 login --context <name> for a context the file set up; %q stays "+
			"selected.", report.Selected)
	}
	return output.NextStep(w, next)
}

// contextExport prints contexts in the input-file form: complete records, so
// the file applies the same on any machine whatever its installed versions,
// with the credential references and the selection removed.
func (s Shell) contextExport(name string) error {
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	document, err := contexts.Load(root)
	if err != nil {
		return err
	}
	file := inputFile{Contexts: []inputContext{}}
	for _, context := range document.Freeze().Contexts {
		if name != "" && context.Name != name {
			continue
		}
		if context.Synthetic() {
			continue
		}
		exported := inputContext{Name: context.Name, Type: context.Type, Login: context.Login,
			Organization: context.Organization, Project: context.Project}
		if len(context.Products) > 0 {
			exported.Products = map[string]inputProduct{}
		}
		for namespace, product := range context.Products {
			exported.Products[namespace] = inputProduct{URL: product.Endpoint, Audience: product.Audience,
				Scopes: product.Scopes, Grant: product.Grant, ClientIDVariable: product.ClientIDVariable,
				ClientSecretVariable: product.ClientSecretVariable, Gateway: product.Gateway}
		}
		file.Contexts = append(file.Contexts, exported)
	}
	if name != "" && len(file.Contexts) == 0 {
		return contextNotFound(name)
	}
	return encodeContextJSON(s.Streams.Out, file)
}

// driftNotes names every record whose frozen values differ from what its
// installed product's descriptor would produce now, and the command that
// adopts the new ones. A product update never edits a context by itself
// (ADR 0016), so this is how a user learns that re-applying would change
// something.
func driftNotes(document contexts.Document, lookup descriptorLookup) []string {
	var notes []string
	for _, context := range document.Contexts {
		for _, namespace := range slices.Sorted(maps.Keys(context.Products)) {
			descriptor := lookup(namespace)
			if descriptor == nil {
				continue
			}
			for _, drift := range productDrift(namespace, *descriptor, context.Products[namespace],
				namespace == context.Login.Product) {
				notes = append(notes, fmt.Sprintf("The %q product on the %q context records %s. If that was "+
					"not set deliberately, adopt the new value with wso2 context product add %s --url %s "+
					"--replace --context %s, or by applying the context file again.",
					namespace, context.Name, drift, namespace, context.Products[namespace].Endpoint, context.Name))
			}
		}
	}
	return notes
}

// productDrift describes how a record differs from its descriptor's defaults,
// in the members the descriptor alone decides.
func productDrift(namespace string, descriptor modules.ProductDescriptor, product contexts.Product,
	login bool) []string {
	var drifts []string
	if !login && descriptor.Grant != "" {
		recorded := ""
		if product.Grant != nil {
			recorded = product.Grant.Kind
		}
		if recorded != descriptor.Grant {
			drifts = append(drifts, fmt.Sprintf("the grant %q where the installed %s product now uses %q",
				recorded, namespace, descriptor.Grant))
		}
	}
	if len(descriptor.Scopes) > 0 && !sameScopes(product.Scopes, descriptor.Scopes) {
		drifts = append(drifts, fmt.Sprintf("the scopes %q where the installed %s product now asks for %q",
			strings.Join(product.Scopes, ","), namespace, strings.Join(descriptor.Scopes, ",")))
	}
	return drifts
}

// sameScopes compares two scope lists as sets.
func sameScopes(a, b []string) bool {
	left, right := slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b))
	return slices.Equal(slices.Compact(left), slices.Compact(right))
}
