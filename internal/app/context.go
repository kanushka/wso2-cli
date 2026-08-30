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

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/result"
)

// The result shapes the context family reports.
const (
	contextCreateSchema  = "shell.context-create/v1"
	contextUseSchema     = "shell.context-use/v1"
	contextCurrentSchema = "shell.context-current/v1"
	contextListSchema    = "shell.context-list/v1"
)

// contextCreateUsage is the way back from a wso2 context create usage refusal.
const contextCreateUsage = "Run wso2 context create <name> --identity <identity> " +
	"[--organization <name>] [--project <name>]."

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
		Long:                  "Subcommands: create, current, list, use.",
		DisableFlagsInUseLine: true,
	}
	command.AddCommand(s.contextCreateCommand(), s.contextUseCommand(),
		s.contextListCommand(), s.contextCurrentCommand())
	return command
}

func (s Shell) contextCreateCommand() *cobra.Command {
	var identity, organization, project string
	command := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a context. Writes no credential and makes no network call.",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if err := refuseUnusableShellFlags(command); err != nil {
				return err
			}
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
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if err := refuseUnusableShellFlags(command); err != nil {
				return err
			}
			return s.contextUse(command, args[0])
		},
	}
}

func (s Shell) contextListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the configured contexts and mark the selected one.",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			if err := refuseUnusableShellFlags(command); err != nil {
				return err
			}
			return s.contextList(command)
		},
	}
}

func (s Shell) contextCurrentCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show the selected context.",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			if err := refuseUnusableShellFlags(command); err != nil {
				return err
			}
			return s.contextCurrent(command)
		},
	}
}

// refuseUnusableShellFlags refuses a shell-owned flag this family cannot act on.
//
// The family declares its own flags, so nothing is forwarded anywhere and the
// returned argument list is discarded: what is wanted is the refusal.
// forwardShellFlags keys off the command's name, so it is asked about the
// parent — "context" is the name shellFlagsFor knows and the name a refusal
// should print, not "list".
func refuseUnusableShellFlags(command *cobra.Command) error {
	_, err := forwardShellFlags(command.Parent(), nil)
	return err
}

// contextCreate writes one context and nothing else.
//
// It performs no network call, by design and not by omission: an issuer typo
// has to surface at wso2 login, where a user is already waiting on the identity
// provider, rather than here, where it would make creating a context depend on
// a deployment being reachable. That is what makes ADR 0011's claim checkable
// by reading this function. See #112 D8.
func (s Shell) contextCreate(command *cobra.Command, name, identity, organization, project string) error {
	mode, err := shellOutputMode(command)
	if err != nil {
		return err
	}
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	if identity == "" {
		// Left to this check rather than to Cobra's required-flag machinery,
		// which reports a plain error with no category and would exit outside
		// the documented classes.
		return problem.New(problem.CategoryUsage, "shell.missing_flag",
			"wso2 context create needs an identity to authenticate the context as").
			WithRecovery(contextCreateUsage + " Run wso2 login to create an identity.")
	}

	// Every value here came from a flag the user typed, so none of it is
	// credential material: it is already in their shell history, and the
	// document about to be written names credential sources and holds no
	// credential.
	s.log.Debug("creating a context",
		"context", name, "identity", identity,
		"organization", organization, "project", project,
		"document", contexts.Path(root))

	selected := false
	err = contexts.Update(root, func(document contexts.Document) (contexts.Document, error) {
		if declaresContext(document, name) {
			return document, contextExists(name)
		}
		if !declaresIdentity(document, identity) {
			return document, unknownIdentity(identity)
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
			selected = true
		}
		return document, nil
	})
	if err != nil {
		return s.explainWriteRefusal(root, err)
	}

	reported := result.New(contextCreateSchema).
		With("context", "Context", name).
		With("identity", "Identity", identity).
		With("organization", "Organization", organization).
		With("project", "Project", project).
		With("selected", "Selected", yesNo(selected))
	if mode == output.ModeJSON {
		return output.Report(s.Streams.Out, mode, reported)
	}
	if _, err := fmt.Fprintf(s.Streams.Out, "\nCreated the %q context.\n", name); err != nil {
		return err
	}
	if err := output.Report(s.Streams.Out, mode, reported); err != nil {
		return err
	}
	// Said only when it happened, and said because nothing else says it: a user
	// whose first context was also selected for them would otherwise have to
	// run wso2 context current to find out.
	if selected {
		_, err = fmt.Fprintf(s.Streams.Out,
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
	mode, err := shellOutputMode(command)
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

	reported := result.New(contextUseSchema).With("context", "Context", name)
	if mode == output.ModeJSON {
		return output.Report(s.Streams.Out, mode, reported)
	}
	_, err = fmt.Fprintf(s.Streams.Out, "\nCommands now run against the %q context.\n", name)
	return err
}

// contextEntry is one row of the listing.
type contextEntry struct {
	Name         string `json:"name"`
	Identity     string `json:"identity"`
	Organization string `json:"organization,omitempty"`
	Project      string `json:"project,omitempty"`
	Selected     bool   `json:"selected"`
}

// contextListing is what wso2 context list reports.
//
// It is rendered here rather than through output.Report because a report is a
// flat ordered list of named values and a listing is n rows of them. Flattening
// the contexts into one field would hand a JSON caller a sentence to parse,
// which is the opposite of what --output json is for. The shell gains a list
// result shape with #85; this converges on it then.
type contextListing struct {
	Schema   string         `json:"schema"`
	Selected string         `json:"selected"`
	Contexts []contextEntry `json:"contexts"`
}

func (s Shell) contextList(command *cobra.Command) error {
	mode, err := shellOutputMode(command)
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

	listing := contextListing{
		Schema:   contextListSchema,
		Selected: document.DefaultContext,
		Contexts: make([]contextEntry, 0, len(document.Contexts)),
	}
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
		encoded, err := json.MarshalIndent(listing, "", "  ")
		if err != nil {
			return fmt.Errorf("app: cannot encode the context listing: %w", err)
		}
		_, err = fmt.Fprintf(s.Streams.Out, "%s\n", encoded)
		return err
	}
	// An unconfigured machine is a state, not a breakage, so it reports what to
	// run rather than that nothing is there.
	if len(listing.Contexts) == 0 {
		_, err := fmt.Fprintln(s.Streams.Out,
			"No contexts are configured.\n\n"+contextCreateUsage)
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
	mode, err := shellOutputMode(command)
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

	if len(document.Contexts) == 0 {
		reported := result.New(contextCurrentSchema).
			With("context", "Context", "").
			With("identity", "Identity", "").
			With("organization", "Organization", "").
			With("project", "Project", "")
		if mode == output.ModeJSON {
			return output.Report(s.Streams.Out, mode, reported)
		}
		// Reported as a state rather than refused: a machine nobody has
		// configured yet has done nothing wrong, and this is the first command
		// a first-run user runs.
		_, err := fmt.Fprintln(s.Streams.Out,
			"No context is configured, so commands run against nothing.\n\n"+
				"Run wso2 login to create an identity and a context, "+
				"or wso2 context create <name> --identity <identity> if you already have one.")
		return err
	}
	selected, err := document.Select("")
	if err != nil {
		return err
	}
	reported := result.New(contextCurrentSchema).
		With("context", "Context", selected.Context.Name).
		With("identity", "Identity", selected.Context.Identity).
		With("organization", "Organization", selected.Context.Organization).
		With("project", "Project", selected.Context.Project)
	return output.Report(s.Streams.Out, mode, reported)
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
// Nothing is moved, renamed, backed up or converted. There is no migration
// command, and offering one that does not exist would be worse than the
// refusal.
func (s Shell) explainWriteRefusal(stateRoot string, err error) error {
	var typed problem.Problem
	if !errors.As(err, &typed) || typed.Code != "contexts.document_frozen" {
		return err
	}
	document, loadErr := contexts.Load(stateRoot)
	if loadErr != nil || document.SchemaVersion != contexts.SchemaVersionLegacy {
		return err
	}
	path := contexts.Path(stateRoot)
	return problem.New(problem.CategoryUsage, typed.Code,
		fmt.Sprintf("the WSO2 CLI context document at %s is schema version 1, "+
			"which this shell reads but does not write", path)).
		WithRecovery(fmt.Sprintf("The contexts it declares still work: wso2 context list and "+
			"wso2 context current read it as it is. Writing to it, whether that is creating a "+
			"context or changing the selection, means starting a schema version 2 document, "+
			"so move %s "+
			"aside and run the command again. Nothing converts the old file, and the contexts "+
			"it declares would have to be created again.", path))
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
func unknownIdentity(name string) problem.Problem {
	return problem.New(problem.CategoryUsage, "contexts.unknown_identity",
		fmt.Sprintf("no identity named %q is declared in the context document", name)).
		WithRecovery("Check the name against the document's identities, or run wso2 login to " +
			"create one. Logging in is the only thing that creates an identity.")
}

// selectionMark marks the row a command would run against.
func selectionMark(selected bool) string {
	if selected {
		return "*"
	}
	return ""
}

// yesNo renders a boolean for a reported field, which carries strings only.
func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
