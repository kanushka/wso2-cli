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

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// The way back from each subcommand's usage refusals.
const (
	contextCreateUsage = "Run wso2 context create <name> --identity <identity> " +
		"[--organization <name>] [--project <name>]."
	contextUseUsage     = "Run wso2 context use <name>."
	contextListUsage    = "Run wso2 context list [--output table|json]."
	contextCurrentUsage = "Run wso2 context current [--output table|json]."
)

// contextRecovery is what every refusal from the context command itself,
// rather than one of its subcommands, points a user at.
const contextRecovery = "Run wso2 context list to see the contexts on this machine, " +
	"wso2 context current to show the selected one, wso2 context use <name> to select " +
	"another, or wso2 context create <name> to add one."

// contextCommand builds the wso2 context tree.
//
// It is the first shell command family whose flags are declared to Cobra rather
// than scanned out of an argument list by hand. login, logout, and module still
// hand-parse and are converted separately (#89); this family is new code, so
// there is no migration to sequence and it is the shape the rest move toward.
func (s Shell) contextCommand() *cobra.Command {
	command := &cobra.Command{
		Use:                   "context <subcommand>",
		Short:                 "Create, select, and list the targets commands run against.",
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
	// The family renders a machine-readable result, and takes no --context:
	// naming a context is what its own arguments do, and a selection flag
	// alongside "wso2 context use beta" would be two answers to one question.
	declareOutputFlag(command.PersistentFlags())
	command.AddCommand(s.contextCreateCommand(), s.contextUseCommand(),
		s.contextListCommand(), s.contextCurrentCommand())
	return command
}

func (s Shell) contextCreateCommand() *cobra.Command {
	var identity, organization, project string
	command := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a context. Writes no credential and makes no network call.",
		Args:  exactlyOneArgument("a name for the context", contextCreateUsage),
		RunE: func(command *cobra.Command, args []string) error {
			return s.contextCreate(command, args[0], identity, organization, project)
		},
	}
	command.Flags().StringVar(&identity, "identity", "",
		"Authenticate this context as the named identity.")
	command.Flags().StringVar(&organization, "organization", "",
		"Run commands within this organization.")
	// Accepted and left unvalidated on purpose: the field is already in schema
	// version 2 and already hand-authorable, and project discovery has no flow
	// (#112 D10). Refusing it would make this command weaker than the editor it
	// replaces. Whether the project exists is answered by the product command
	// that needs it, which is the only thing that can answer it.
	command.Flags().StringVar(&project, "project", "",
		"Narrow the target to this project inside the organization.")
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
// implemented and works. Reporting it as "error: wso2 config needs a
// subcommand" at exit 64 said the opposite loudly enough that a reader of
// docs/examples/user-flow-review.md concluded config and org were
// unimplemented stubs and proposed hiding both from the command tree until
// their subcommands were written (F8). The guidance that used to be the
// refusal's recovery line is now each family's Long, so it is still the first
// thing printed and nothing is lost.
//
// This is only the no-arguments arm. A family whose RunE calls this still
// refuses an unknown subcommand with its own message and the usage exit class,
// which is the whole reason the RunE exists: Cobra validates a non-leaf
// command's arguments only when the command is Runnable, so a nil RunE would
// report "wso2 config bogus" to a script as everything having worked (#133).
//
// The five families share this so that they cannot drift apart. They answered
// a bare name identically before and must go on doing so.
func helpForBareFamily(command *cobra.Command) error {
	return command.Help()
}

// contextCreate writes one context and nothing else.
//
// It performs no network call, by design and not by omission: an issuer typo
// has to surface at wso2 login, where a user is already waiting on the identity
// provider, rather than here, where it would make creating a context depend on
// a deployment being reachable. That is what makes ADR 0011's claim checkable
// by reading this function. See #112 D8.
func (s Shell) contextCreate(command *cobra.Command, name, identity, organization, project string) error {
	mode, err := s.shellOutputMode(command)
	if err != nil {
		return err
	}
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	if identity == "" {
		// Checked here rather than with Cobra's MarkFlagRequired, whose error
		// takes the route exactlyOneArgument's comment describes and would exit
		// outside the documented classes.
		return problem.New(problem.CategoryUsage, "shell.missing_required_flag",
			"wso2 context create needs an identity to authenticate the context as").
			WithRecovery(contextCreateUsage + " Run wso2 login to create an identity.")
	}
	// Checked before the document is opened, so that a name the user mistyped
	// is refused as the argument it is. Left to the document, the same mistake
	// arrives as contexts.document_malformed, which tells a user their file is
	// wrong and offers to remove it — advice that would destroy the contexts
	// they already have, over a name that never reached the file.
	if !contexts.ValidName(name) {
		return problem.New(problem.CategoryUsage, "shell.invalid_argument",
			fmt.Sprintf("%q cannot be used as a context name", name)).
			WithRecovery(fmt.Sprintf("A context name is %s. %s", contexts.NameRule, contextCreateUsage))
	}

	// Every value here came from a flag or an argument the user typed, so none
	// of it is credential material: it is already in their shell history, and
	// the document about to be written names credential sources and holds no
	// credential.
	s.log.Debug("creating a context",
		"context", name, "identity", identity,
		"organization", organization, "project", project,
		"document", contexts.Path(root))

	created := contextCreated{
		Context:      name,
		Identity:     identity,
		Organization: organization,
		Project:      project,
	}
	err = contexts.Update(root, func(document contexts.Document) (contexts.Document, error) {
		if declaresContext(document, name) {
			return document, contextExists(name)
		}
		if !declaresIdentity(document, identity) {
			return document, unknownIdentity(identity, len(document.Identities) > 0)
		}
		// A fresh machine yields the zero document, whose schema version is
		// zero rather than the one the shell writes.
		document.SchemaVersion = contexts.SchemaVersion
		document.Contexts = append(document.Contexts, contexts.Context{
			Name:         name,
			Identity:     identity,
			Organization: organization,
			Project:      project,
		})
		if document.DefaultContext == "" {
			document.DefaultContext = name
			created.Selected = true
		}
		return document, nil
	})
	if err != nil {
		return s.explainWriteRefusal(root, err)
	}

	if mode == output.ModeJSON {
		return renderContext(s.Streams.Out, mode, created)
	}
	if _, err := fmt.Fprintf(s.Streams.Out, "\nCreated the %q context.\n", name); err != nil {
		return err
	}
	if err := renderContext(s.Streams.Out, mode, created); err != nil {
		return err
	}
	// Said only when it happened, and said because nothing else says it: a user
	// whose first context was also selected for them would otherwise have to
	// run wso2 context current to find out.
	if created.Selected {
		_, err = fmt.Fprint(s.Streams.Out,
			"\nIt is the first context, so it is now the selected one. "+
				"Run wso2 context use <name> to select another.\n")
		return err
	}
	_, err = fmt.Fprintf(s.Streams.Out,
		"\nRun wso2 context use %s to run commands against it.\n", name)
	return err
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

	// Encoded directly rather than through renderContext: this site has already
	// branched on the mode, and the table branch below is prose rather than a
	// field table, so contextSelection has no rows to render and no fields()
	// method to leave unused.
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
			Identity:     configured.Identity,
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
		_, err := fmt.Fprintln(s.Streams.Out, "No contexts are configured.\n\n"+contextCreateUsage)
		return err
	}
	table := output.NewTable("current", "context", "identity", "organization", "project")
	for _, entry := range listing.Contexts {
		table.Append(selectionMark(entry.Selected), entry.Name, entry.Identity,
			entry.Organization, entry.Project)
	}
	return table.Render(s.Streams.Out)
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
	if len(document.Contexts) > 0 {
		selected, err := document.Select("")
		if err != nil {
			return err
		}
		current = contextCurrent{
			Configured:   true,
			Context:      selected.Context.Name,
			Identity:     selected.Context.Identity,
			Organization: selected.Context.Organization,
			Project:      selected.Context.Project,
		}
	}
	if mode == output.ModeJSON || current.Configured {
		return renderContext(s.Streams.Out, mode, current)
	}
	// Reported as a state rather than refused: a machine nobody has configured
	// yet has done nothing wrong, and this is among the first commands a
	// first-run user runs. The sentence carries the same fact the JSON carries
	// in Configured; four blank rows above a "Configured: no" row would carry
	// it worse.
	_, err = fmt.Fprintln(s.Streams.Out,
		"No context is configured, so commands run against nothing.\n\n"+
			"Run wso2 login to create an identity and a context, "+
			"or wso2 context create <name> --identity <identity> if you already have one.")
	return err
}

// The results this family reports.
//
// They are rendered here rather than through output.Report, which the rest of
// the shell uses, because result.Result carries string values only: a listing
// is n rows of fields rather than one, and "selected" is a boolean that a JSON
// caller would otherwise have to read back out of the word "yes". Both
// renderings are still driven by one value, which is what ADR 0003 asks for.
// None of them publishes a discriminator, because the shell's own renderer
// suppresses result.Result's and one command inventing a second convention is
// worse than the convention being absent. #85 is where that is settled.
type (
	// contextCreated is what wso2 context create reports.
	contextCreated struct {
		Context      string `json:"context"`
		Identity     string `json:"identity"`
		Organization string `json:"organization"`
		Project      string `json:"project"`
		// Selected reports whether this context is now the one commands run
		// against, which is true for the first context created and no other.
		Selected bool `json:"selected"`
	}

	// contextSelection is what wso2 context use reports.
	contextSelection struct {
		Context string `json:"context"`
	}

	// contextCurrent is what wso2 context current reports.
	contextCurrent struct {
		// Configured says whether any context exists to be current. It is a
		// field rather than an absence, because a caller cannot read the
		// difference between "nothing is configured" and "the context has an
		// empty name" out of empty strings.
		Configured   bool   `json:"configured"`
		Context      string `json:"context"`
		Identity     string `json:"identity"`
		Organization string `json:"organization"`
		Project      string `json:"project"`
	}

	// contextEntry is one row of the listing. Nothing is omitted when empty:
	// a caller iterating the rows must not have to tell an absent key from an
	// unset value, and the other results in this family omit nothing either.
	contextEntry struct {
		Name         string `json:"name"`
		Identity     string `json:"identity"`
		Organization string `json:"organization"`
		Project      string `json:"project"`
		Selected     bool   `json:"selected"`
	}

	// contextListing is what wso2 context list reports.
	contextListing struct {
		Contexts []contextEntry `json:"contexts"`
	}
)

func (c contextCreated) fields() [][2]string {
	return [][2]string{
		{"Context", c.Context},
		{"Identity", c.Identity},
		{"Organization", c.Organization},
		{"Project", c.Project},
		{"Selected", yesNo(c.Selected)},
	}
}

func (c contextCurrent) fields() [][2]string {
	return [][2]string{
		{"Context", c.Context},
		{"Identity", c.Identity},
		{"Organization", c.Organization},
		{"Project", c.Project},
	}
}

// reportable is a result of this family that renders in either mode.
type reportable interface {
	// fields are the labelled values the table shows, listed in the order the
	// JSON document declares them.
	//
	// That order is kept by hand, and so is the membership: a member added to
	// the struct and not to fields() reaches JSON and is silently missing from
	// the table. The compiler catches the reverse and nothing catches this
	// direction, so the two are written together or not at all. contextCurrent
	// departs from the membership deliberately — see the comment there — which
	// is why this is a convention rather than a guarantee.
	fields() [][2]string
}

// renderContext writes one result as JSON or as a labelled field table.
func renderContext(w io.Writer, mode output.Mode, value reportable) error {
	if mode == output.ModeJSON {
		return encodeContextJSON(w, value)
	}
	return output.Fields(w, value.fields())
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
// The condition is caught by code rather than by matching the message, so the
// wording of either can change without silently disabling this. Which version
// was found is answered by loading the document rather than by reading the
// refusal: a version 1 document is one this shell still reads, and a version
// written by a newer CLI is one it cannot read at all, so whether Load succeeds
// is exactly the distinction — and it is drawn by the package's own reader
// rather than by a second parser here.
//
// Only the version 1 case is rewritten. A document a newer CLI on this machine
// manages is not this shell's to explain, and the writer's own recovery, which
// names that CLI, is already the right advice.
//
// The route out has to name wso2 login, and not just the file. A version 1
// document's identities exist only as the ones the compatibility read
// manufactures, so moving the file aside takes them with it: a user told merely
// to move it and retry meets a second refusal with their old file already
// renamed. Login is the only thing that creates an identity (#112 D3).
//
// Nothing is moved, renamed, backed up or converted here. There is no migration
// command, and offering one that does not exist would be worse than the refusal.
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
		WithRecovery("wso2 context list and wso2 context current still read it as it is. " +
			"To write, move the file aside; the shell then starts a fresh schema version 2 " +
			"document. Nothing is converted, so run wso2 login to create an identity and " +
			"wso2 context create to declare the contexts again.")
}

// declaresContext reports whether the document already names this context.
func declaresContext(document contexts.Document, name string) bool {
	for _, candidate := range document.Contexts {
		if candidate.Name == name {
			return true
		}
	}
	return false
}

// declaresIdentity reports whether the document declares this identity.
func declaresIdentity(document contexts.Document, name string) bool {
	for _, candidate := range document.Identities {
		if candidate.Name == name {
			return true
		}
	}
	return false
}

// contextExists refuses to replace a context that is already there.
//
// Overwriting would be the one thing a user cannot undo: the previous
// organization, project and identity are not recorded anywhere else.
func contextExists(name string) problem.Problem {
	return problem.New(problem.CategoryUsage, "contexts.context_exists",
		fmt.Sprintf("a context named %q is already configured", name)).
		WithRecovery("Choose another name, or run wso2 context list to see what is configured. " +
			"Creating a context never replaces one.")
}

// unknownIdentity refuses a context that would authenticate as nothing.
//
// The recovery names wso2 login because login is the only thing that creates an
// identity: there is no wso2 identity create, by decision (#112 D3), so any
// other advice would send the user looking for a command that does not exist.
// Which recovery depends on whether any identity exists at all: a document with
// identities offers wso2 identity list, because the likeliest fault is a
// mistyped name, and a document with none offers nothing to list, so pointing
// at the list would send a first-run user in a circle back to login.
func unknownIdentity(name string, anyDeclared bool) problem.Problem {
	if !anyDeclared {
		return problem.New(problem.CategoryUsage, "contexts.unknown_identity",
			fmt.Sprintf("no identity named %q is configured, and no identities exist", name)).
			WithRecovery("Run wso2 login --url <issuer> --client-id <id> to log in and create " +
				"one. Logging in is the only thing that creates an identity.")
	}
	return problem.New(problem.CategoryUsage, "contexts.unknown_identity",
		fmt.Sprintf("no identity named %q is configured", name)).
		WithRecovery("Run wso2 identity list to see the identities login created, or wso2 login " +
			"--url <issuer> --client-id <id> to create one. Logging in is the only thing that " +
			"creates an identity.")
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
