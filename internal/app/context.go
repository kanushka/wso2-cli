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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// The way back from each subcommand's usage refusals.
const (
	contextCreateUsage = "Run wso2 context create <name> --login-product <product> --url <url> " +
		"[--client-id <id>] [--provider <name>] [--use], or wso2 context create <name> " +
		"--issuer <url> --client-id <id> [--provider <name>] [--use]."
	contextUseUsage     = "Run wso2 context use <name>."
	contextListUsage    = "Run wso2 context list [--output table|json]."
	contextCurrentUsage = "Run wso2 context current [--output table|json]."
	contextShowUsage    = "Run wso2 context show [--output table|json]."
)

// contextSetupHint is what a machine with no contexts is told to run.
const contextSetupHint = "Run wso2 context apply -f <file> --use <name> with the file your platform " +
	"team shares, or wso2 context create <name> --login-product <product> --url <url> --use."

// contextRecovery is what every refusal from the context command itself,
// rather than one of its subcommands, points a user at.
const contextRecovery = "Run wso2 context list to see the contexts on this machine, " +
	"wso2 context use <name> to select one, wso2 context create <name> or wso2 context apply " +
	"-f <file> to add one, or wso2 context show to see the document whole and where it lives."

// contextCommand builds the wso2 context tree.
//
// Everything that shapes contexts.json is here: creating and applying
// contexts, adding and removing their products, selecting, renaming and
// deleting them, and showing, editing and exporting the document itself. The
// file is what an on-premises setup is (ADR 0016), so the commands that write
// it live beside the ones that show it.
func (s Shell) contextCommand() *cobra.Command {
	command := &cobra.Command{
		Use:                   "context <subcommand>",
		Short:                 "Set up, select, and inspect the contexts commands run against.",
		Long:                  contextRecovery,
		DisableFlagsInUseLine: true,
		// A RunE is declared because Cobra validates a non-leaf command's
		// arguments only when it is Runnable: leave it nil and wso2 context
		// bogus prints help and exits 0, reporting a typo as success to
		// whatever ran it. Never cobra.NoArgs or cobra.ExactArgs for this —
		// both bypass the flag-error hook and exit 70 instead of 64.
		//
		// A bare wso2 context is the other arm, and is deliberately not a
		// refusal. See helpForBareFamily.
		RunE: func(command *cobra.Command, args []string) error {
			if len(args) == 0 {
				return helpForBareFamily(command)
			}
			return problem.New(problem.CategoryUsage, "shell.unknown_command",
				fmt.Sprintf("%q is not a wso2 context subcommand", args[0])).
				WithRecovery(contextRecovery)
		},
	}
	// The family renders a machine-readable result. It takes no family-wide
	// --context: naming a context is what its own arguments do. The product
	// subcommands are the one exception, and declare their own.
	declareOutputFlag(command.PersistentFlags())
	command.AddCommand(s.contextCreateCommand(), s.contextProductCommand(), s.contextApplyCommand(),
		s.contextUseCommand(), s.contextListCommand(), s.contextCurrentCommand(), s.contextShowCommand(),
		s.contextRenameCommand(), s.contextDeleteCommand(), s.contextEditCommand(),
		s.contextExportCommand())
	return command
}

func (s Shell) contextUseCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Select the context commands run against.",
		Args:  exactlyOneArgument("the name of a configured context", contextUseUsage),
		RunE: func(command *cobra.Command, args []string) error {
			return s.contextUse(command, args[0])
		},
	}
}

func (s Shell) contextListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the configured contexts and mark the selected one.",
		Args:  noArguments(contextListUsage),
		RunE: func(command *cobra.Command, args []string) error {
			return s.contextList(command)
		},
	}
}

func (s Shell) contextCurrentCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show the selected context.",
		Args:  noArguments(contextCurrentUsage),
		RunE: func(command *cobra.Command, args []string) error {
			return s.contextCurrent(command)
		},
	}
}

func (s Shell) contextShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show the context document whole: where it lives, and everything it declares.",
		Args:  noArguments(contextShowUsage),
		RunE: func(command *cobra.Command, args []string) error {
			return s.contextShow(command)
		},
	}
}

// exactlyOneArgument refuses a wrong argument count as the usage failure it is.
//
// cobra.ExactArgs refuses it too, but its error takes the same route
// ValidateRequiredFlags takes: it never reaches the flag-error hook, so it
// arrives at the shell's classifier untyped and exits in the module-process
// class, which the command reference documents as a module that crashed. An
// argument count a user got wrong is theirs to fix, and the class a script
// branches on has to say so.
func exactlyOneArgument(what, usage string) cobra.PositionalArgs {
	return func(command *cobra.Command, args []string) error {
		switch {
		case len(args) == 0:
			return problem.New(problem.CategoryUsage, "shell.missing_argument",
				fmt.Sprintf("%s needs %s", command.CommandPath(), what)).
				WithRecovery(usage)
		case len(args) > 1:
			return problem.New(problem.CategoryUsage, "shell.unexpected_argument",
				fmt.Sprintf("%s takes one argument, got %d", command.CommandPath(), len(args))).
				WithRecovery(usage)
		}
		return nil
	}
}

// exactlyTwoArguments is exactlyOneArgument for a command that takes two.
func exactlyTwoArguments(what, usage string) cobra.PositionalArgs {
	return func(command *cobra.Command, args []string) error {
		switch {
		case len(args) < 2:
			return problem.New(problem.CategoryUsage, "shell.missing_argument",
				fmt.Sprintf("%s needs %s", command.CommandPath(), what)).
				WithRecovery(usage)
		case len(args) > 2:
			return problem.New(problem.CategoryUsage, "shell.unexpected_argument",
				fmt.Sprintf("%s takes two arguments, got %d", command.CommandPath(), len(args))).
				WithRecovery(usage)
		}
		return nil
	}
}

// atMostOneArgument refuses more than one argument.
func atMostOneArgument(usage string) cobra.PositionalArgs {
	return func(command *cobra.Command, args []string) error {
		if len(args) > 1 {
			return problem.New(problem.CategoryUsage, "shell.unexpected_argument",
				fmt.Sprintf("%s takes at most one argument, got %d", command.CommandPath(), len(args))).
				WithRecovery(usage)
		}
		return nil
	}
}

// noArguments refuses a stray argument, for the same reason exactlyOneArgument
// exists: cobra.NoArgs would report it outside the shell's exit classes.
func noArguments(usage string) cobra.PositionalArgs {
	return func(command *cobra.Command, args []string) error {
		if len(args) > 0 {
			return problem.New(problem.CategoryUsage, "shell.unexpected_argument",
				fmt.Sprintf("%s takes no arguments, got %q", command.CommandPath(), args[0])).
				WithRecovery(usage)
		}
		return nil
	}
}

// helpForBareFamily answers a family name typed with no subcommand at all.
//
// It prints the family's help and succeeds. A bare family name is an
// incomplete command, not a failed one: every subcommand it names is
// implemented and works. This is only the no-arguments arm; a family whose
// RunE calls this still refuses an unknown subcommand with its own message and
// the usage exit class (#133).
func helpForBareFamily(command *cobra.Command) error {
	return command.Help()
}

// contextUse writes the selection and nothing else.
func (s Shell) contextUse(command *cobra.Command, name string) error {
	mode, err := s.shellOutputMode(command)
	if err != nil {
		return err
	}
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	s.log.Debug("selecting a context", "context", name, "document", contexts.Path(root))

	err = contexts.Update(root, func(document contexts.Document) (contexts.Document, error) {
		// Select is what refuses an unknown name, rather than a second lookup
		// written here: it is what every other command resolves a context
		// through, so a name this accepts is a name they can all use.
		if _, err := document.Select(name); err != nil {
			return document, err
		}
		document.DefaultContext = name
		return document, nil
	})
	if err != nil {
		return s.explainWriteRefusal(root, err)
	}
	if mode == output.ModeJSON {
		return encodeContextJSON(s.Streams.Out, contextSelection{Context: name})
	}
	_, err = fmt.Fprintf(s.Streams.Out, "\nCommands now run against the %q context.\n", name)
	return err
}

func (s Shell) contextList(command *cobra.Command) error {
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

	listing := contextListing{Contexts: make([]contextEntry, 0, len(document.Contexts))}
	for _, configured := range document.Contexts {
		listing.Contexts = append(listing.Contexts, contextEntry{
			Name:         configured.Name,
			Type:         configured.Type,
			Issuer:       configured.Login.Issuer,
			Products:     productList(configured),
			Organization: configured.Organization,
			Project:      configured.Project,
			Selected:     configured.Name == document.DefaultContext,
		})
	}
	if mode == output.ModeJSON {
		return encodeContextJSON(s.Streams.Out, listing)
	}
	// An unconfigured machine is a state, not a breakage, so it reports what to
	// run rather than that nothing is there.
	if len(listing.Contexts) == 0 {
		_, err := fmt.Fprintln(s.Streams.Out, output.Hint(s.Streams.Out, "No contexts are configured.\n\n"+
			contextSetupHint))
		return err
	}
	table := output.NewTable("current", "context", "type", "issuer", "products", "organization", "project")
	for _, entry := range listing.Contexts {
		table.Append(selectionMark(entry.Selected), entry.Name, entry.Type, entry.Issuer,
			strings.Join(entry.Products, ","), entry.Organization, entry.Project)
	}
	if err := table.Render(s.Streams.Out); err != nil {
		return err
	}
	if document.DefaultContext == "" {
		_, err := fmt.Fprintln(s.Streams.Out, output.Hint(s.Streams.Out,
			"\nNo context is selected. Run wso2 context use <name> to select one."))
		return err
	}
	return nil
}

// contextCurrent reports the context commands run against.
func (s Shell) contextCurrent(command *cobra.Command) error {
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

	current := contextCurrent{}
	if len(document.Contexts) > 0 && document.DefaultContext != "" {
		selected, err := document.Select("")
		if err != nil {
			return err
		}
		current = contextCurrent{
			Configured:   true,
			Context:      selected.Context.Name,
			Type:         selected.Context.Type,
			Issuer:       selected.Context.Login.Issuer,
			Products:     productList(selected.Context),
			Organization: selected.Context.Organization,
			Project:      selected.Context.Project,
		}
	}
	if mode == output.ModeJSON || current.Configured {
		return renderContext(s.Streams.Out, mode, current)
	}
	message := "No context is configured, so commands run against nothing.\n\n" + contextSetupHint
	if len(document.Contexts) > 0 {
		message = "No context is selected, so commands run against nothing.\n\n" +
			"Run wso2 context list to see the configured contexts, then wso2 context use <name>."
	}
	_, err = fmt.Fprintln(s.Streams.Out, output.Hint(s.Streams.Out, message))
	return err
}

// contextShow reports where the context document lives and shows it whole:
// every context it declares, with its login and its products, alongside the
// schema version and the selection.
//
// The path is reported even when nothing has been written there yet: a user
// who has never set anything up still needs to know where a document would go,
// and contexts.Load answers a missing file with the zero Document rather than
// an error, so Written is the only thing that tells the two states apart.
//
// Nothing here needs redacting. A context is named and located values only — a
// credential reference, an environment variable name, an issuer, never a token
// or a secret — by the guarantee internal/contexts documents at its own package
// level. Showing the document whole is what proves that guarantee to a reader.
func (s Shell) contextShow(command *cobra.Command) error {
	mode, err := s.shellOutputMode(command)
	if err != nil {
		return err
	}
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	path := contexts.Path(root)
	_, statErr := os.Stat(path)
	written := statErr == nil

	document, err := contexts.Load(root)
	if err != nil {
		return err
	}

	report := contextDocumentReport{
		Path:           path,
		Written:        written,
		SchemaVersion:  document.SchemaVersion,
		DefaultContext: document.DefaultContext,
		Contexts:       make([]shownContext, 0, len(document.Contexts)),
		Notes:          []string{},
	}
	for _, configured := range document.Contexts {
		report.Contexts = append(report.Contexts, shownContext{
			Context:          configured,
			CredentialSource: credentialSource(configured),
		})
	}
	if migration, migrated := document.Migrated(); migrated {
		report.Notes = append(report.Notes, fmt.Sprintf("The document on disk is schema version %d; the "+
			"next write stores it as version %d.", migration.From, contexts.SchemaVersion))
		report.Notes = append(report.Notes, migration.Notes()...)
	}
	report.Notes = append(report.Notes, driftNotes(document, s.installedDescriptors())...)

	if mode == output.ModeJSON {
		return encodeContextJSON(s.Streams.Out, report)
	}
	return report.renderTable(s.Streams.Out)
}

// The results this family reports.
//
// They are rendered here rather than through output.Report, which the rest of
// the shell uses, because result.Result carries string values only: a listing
// is n rows of fields rather than one, and "selected" is a boolean that a JSON
// caller would otherwise have to read back out of the word "yes".
type (
	// contextSelection is what wso2 context use reports.
	contextSelection struct {
		Context string `json:"context"`
	}

	// contextCurrent is what wso2 context current reports.
	contextCurrent struct {
		// Configured says whether a context is selected to be current.
		Configured   bool     `json:"configured"`
		Context      string   `json:"context"`
		Type         string   `json:"type"`
		Issuer       string   `json:"issuer"`
		Products     []string `json:"products"`
		Organization string   `json:"organization"`
		Project      string   `json:"project"`
	}

	// contextEntry is one row of the listing. Nothing is omitted when empty:
	// a caller iterating the rows must not have to tell an absent key from an
	// unset value.
	contextEntry struct {
		Name         string   `json:"name"`
		Type         string   `json:"type"`
		Issuer       string   `json:"issuer"`
		Products     []string `json:"products"`
		Organization string   `json:"organization"`
		Project      string   `json:"project"`
		Selected     bool     `json:"selected"`
	}

	// contextListing is what wso2 context list reports.
	contextListing struct {
		Contexts []contextEntry `json:"contexts"`
	}

	// contextDocumentReport is what wso2 context show reports: the context
	// document, plus where it lives, whether it has been written at all, and
	// anything a reader should know about it.
	contextDocumentReport struct {
		Path           string         `json:"path"`
		Written        bool           `json:"written"`
		SchemaVersion  int            `json:"schemaVersion"`
		DefaultContext string         `json:"defaultContext"`
		Contexts       []shownContext `json:"contexts"`
		// Notes are the migration's report and any record whose values
		// differ from what its installed product would produce now.
		Notes []string `json:"notes"`
	}

	// shownContext is one context as wso2 context show reports it: the
	// context as the document records it, plus where its credential comes
	// from. The source is derived because no single member holds it for
	// every kind; it names a source and never a credential.
	shownContext struct {
		contexts.Context
		CredentialSource string `json:"credentialSource"`
	}
)

func (c contextCurrent) fields() [][2]string {
	return [][2]string{
		{"Context", c.Context},
		{"Type", c.Type},
		{"Issuer", c.Issuer},
		{"Products", strings.Join(c.Products, ", ")},
		{"Organization", c.Organization},
		{"Project", c.Project},
	}
}

// productList names a context's products in namespace order, never nil.
func productList(context contexts.Context) []string {
	products := slices.Sorted(maps.Keys(context.Products))
	if products == nil {
		return []string{}
	}
	return products
}

// renderTable writes the whole document as a person reads it: the path and
// whether it exists, the contexts, every product record, then the notes.
func (c contextDocumentReport) renderTable(w io.Writer) error {
	if err := output.Fields(w, [][2]string{
		{"Path", c.Path},
		{"Written", yesNo(c.Written)},
	}); err != nil {
		return err
	}
	if !c.Written {
		_, err := fmt.Fprintln(w, "\nNo context document has been written yet.\n\n"+contextSetupHint)
		return err
	}
	selected := c.DefaultContext
	if selected == "" {
		selected = "(none)"
	}
	if _, err := fmt.Fprintf(w, "\nSchema version: %d\nSelected context: %s\n",
		c.SchemaVersion, selected); err != nil {
		return err
	}
	if len(c.Contexts) == 0 {
		_, err := fmt.Fprintln(w, "\nNo contexts are declared.")
		return err
	}
	if _, err := fmt.Fprintln(w, "\nContexts"); err != nil {
		return err
	}
	table := output.NewTable("current", "name", "type", "kind", "issuer", "login product",
		"credential source", "organization", "project")
	for _, entry := range c.Contexts {
		table.Append(selectionMark(entry.Name == c.DefaultContext), entry.Name, entry.Type,
			entry.Login.Kind, entry.Login.Issuer, entry.Login.Product, entry.CredentialSource,
			entry.Organization, entry.Project)
	}
	if err := table.Render(w); err != nil {
		return err
	}
	shown := make([]contexts.Context, 0, len(c.Contexts))
	for _, entry := range c.Contexts {
		shown = append(shown, entry.Context)
	}
	if err := renderProductRecords(w, shown); err != nil {
		return err
	}
	for _, note := range c.Notes {
		if _, err := fmt.Fprintf(w, "\n%s\n", note); err != nil {
			return err
		}
	}
	return nil
}

// renderProductRecords shows every record each context holds, whole: what it
// reaches and how, one row per record. A gateway record is a row of its own
// under its <namespace>/gateway key, because it is reached separately. The
// login product is marked: every other product's session is obtained through
// the one a context logs in through, so it is the record that explains the
// rest.
func renderProductRecords(w io.Writer, configured []contexts.Context) error {
	table := output.NewTable("login", "context", "record", "url", "audience", "scopes", "grant")
	rows := 0
	for _, context := range configured {
		account := context.Account()
		login := ""
		if context.Login.Kind != contexts.KindClientCredentials {
			login = account.LoginAccess().Namespace
		}
		for _, key := range account.RecordKeys() {
			namespace, gateway := contexts.SplitGatewayKey(key)
			product := context.Products[namespace]
			endpoint, audience, scopes, grant := product.Endpoint, product.Audience, product.Scopes, ""
			if gateway {
				endpoint, audience, scopes = product.Gateway.Endpoint, product.Gateway.Audience, product.Gateway.Scopes
			} else if product.Grant != nil {
				grant = product.Grant.Kind
			}
			table.Append(selectionMark(key == login), context.Name, key, endpoint, audience,
				strings.Join(scopes, ","), grant)
			rows++
		}
	}
	if rows == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w, "\nProducts"); err != nil {
		return err
	}
	return table.Render(w)
}

// credentialSource names where a context's credential comes from — never the
// credential itself, which a context has nowhere to hold. A browser context
// names the secure-store entry its session lives under; a client-credentials
// context names the environment variable its secret is read from at use.
func credentialSource(context contexts.Context) string {
	switch {
	case context.CredentialRef != "":
		return "secure store: " + context.CredentialRef
	case context.Login.ClientSecretVariable != "":
		return "env: " + context.Login.ClientSecretVariable
	case context.CredentialVariable() != "":
		// A version 1 document's context: its credential is read from this
		// variable, in a field the shell never encodes because it never
		// writes a version 1 document back.
		return "env: " + context.CredentialVariable()
	default:
		return ""
	}
}

// reportable is a result of this family that renders in either mode.
type reportable interface {
	// fields are the labelled values the table shows, listed in the order the
	// JSON document declares them. The order and membership are kept by hand.
	fields() [][2]string
}

// renderContext writes one result as JSON or as a labelled field table.
func renderContext(w io.Writer, mode output.Mode, value reportable) error {
	if mode == output.ModeJSON {
		return encodeContextJSON(w, value)
	}
	if err := output.Fields(w, value.fields()); err != nil {
		return err
	}
	if stepper, ok := value.(nextStepper); ok {
		return output.NextStep(w, stepper.next())
	}
	return nil
}

// nextStepper is a result that ends its table with what to run next, printed
// apart from the fields so the command in it is not lost among the values.
type nextStepper interface {
	next() string
}

// encodeContextJSON writes one result as an indented JSON document.
func encodeContextJSON(w io.Writer, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("app: cannot encode the context result: %w", err)
	}
	_, err = fmt.Fprintf(w, "%s\n", encoded)
	return err
}

// explainWriteRefusal turns the writer's refusal to overwrite a version 1
// document into advice a user can act on.
//
// The condition is caught by code rather than by matching the message. Only
// the version 1 case is rewritten: a document a newer CLI on this machine
// manages is not this shell's to explain, and the writer's own recovery, which
// names that CLI, is already the right advice.
func (s Shell) explainWriteRefusal(stateRoot string, err error) error {
	var typed problem.Problem
	if !errors.As(err, &typed) || typed.Code != "contexts.document_frozen" {
		return err
	}
	document, loadErr := contexts.Load(stateRoot)
	if loadErr != nil || document.SchemaVersion != contexts.SchemaVersionLegacy {
		return err
	}
	return problem.New(problem.CategoryUsage, typed.Code,
		fmt.Sprintf("the WSO2 CLI context document at %s is schema version 1, "+
			"which this shell reads but does not write", contexts.Path(stateRoot))).
		WithRecovery(fmt.Sprintf("wso2 context list and wso2 context current still read it as it is. "+
			"To write, move the file aside; the shell then starts a fresh schema version %d "+
			"document. Nothing is converted, so declare the contexts again with wso2 context "+
			"create or wso2 context apply -f <file>.", contexts.SchemaVersion))
}

// declaresContext reports whether the document already names this context.
func declaresContext(document contexts.Document, name string) bool {
	_, found := document.Find(name)
	return found
}

// contextExists refuses to replace a context that is already there.
func contextExists(name string) problem.Problem {
	return problem.New(problem.CategoryUsage, "contexts.context_exists",
		fmt.Sprintf("a context named %q is already configured", name)).
		WithRecovery("Choose another name, or run wso2 context list to see what is configured. " +
			"Creating a context never replaces one; wso2 context apply -f <file> replaces a context " +
			"by name.")
}

// selectionMark marks the row a command would run against.
func selectionMark(selected bool) string {
	if selected {
		return "*"
	}
	return ""
}

// yesNo renders a boolean for a table cell, which carries text.
func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
