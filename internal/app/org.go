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

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// The way back from each subcommand's usage refusals.
const (
	orgUseUsage     = "Run wso2 org use <organization>."
	orgCurrentUsage = "Run wso2 org current [--output table|json]."
)

// orgRecovery is what every refusal from the org command itself, rather than
// one of its subcommands, points a user at.
const orgRecovery = "Run wso2 org current to show the organization the selected context runs " +
	"within, or wso2 org use <organization> to change it."

// noContextRecovery is the route out of a machine with no context to act on.
// Worded exactly as wso2 context current and wso2 whoami word the same state,
// because it is the same fact and the shell must not invent a second sentence
// for it.
const noContextRecovery = "Run wso2 login to create an identity and a context, " +
	"or wso2 context create <name> --identity <identity> if you already have one."

// orgCommand builds the wso2 org tree.
//
// wso2 org list is deferred (#112 R11): no control-plane endpoint for
// enumerating organizations exists anywhere in this repository, and listing
// would need an access token before an organization is chosen, which the auth
// broker is written to refuse (internal/auth/source.go's checkHomeTenant and
// developmentSource both require Context.Organization already). Building it
// would make an architecture decision — the first shell-side consumer of a
// token, which ADR 0004 governs — as a side effect of a command. So this
// family declares only current and use, and anything else typed under it,
// including list, is refused by this family's own RunE as the unknown
// subcommand it is.
func (s Shell) orgCommand() *cobra.Command {
	command := &cobra.Command{
		Use:                   "org <subcommand>",
		Short:                 "Show and change the organization the selected context runs within.",
		Long:                  "Subcommands: current, use.",
		DisableFlagsInUseLine: true,
		// A RunE is declared here, unlike the config family, because Cobra
		// validates a non-leaf command's arguments only when the command is
		// Runnable: leave it nil and a bare wso2 org prints help and exits 0,
		// reporting a usage error as success.
		//
		// The arm below refuses an unrecognised subcommand with this
		// family's own message.
		RunE: func(command *cobra.Command, args []string) error {
			if err := refuseUnusableShellFlags(command); err != nil {
				return err
			}
			if len(args) == 0 {
				return problem.New(problem.CategoryUsage, "shell.missing_argument",
					"wso2 org needs a subcommand").
					WithRecovery(orgRecovery)
			}
			return problem.New(problem.CategoryUsage, "shell.unknown_command",
				fmt.Sprintf("%q is not a wso2 org subcommand", args[0])).
				WithRecovery(orgRecovery)
		},
	}
	command.AddCommand(s.orgCurrentCommand(), s.orgUseCommand())
	return command
}

func (s Shell) orgCurrentCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show the organization the selected context runs within.",
		Args:  noArguments(orgCurrentUsage),
		RunE: func(command *cobra.Command, args []string) error {
			if err := refuseUnusableShellFlags(command); err != nil {
				return err
			}
			return s.orgCurrent(command)
		},
	}
}

func (s Shell) orgUseCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "use <organization>",
		Short: "Set the organization the selected context runs within.",
		Args:  exactlyOneArgument("the name of an organization", orgUseUsage),
		RunE: func(command *cobra.Command, args []string) error {
			if err := refuseUnusableShellFlags(command); err != nil {
				return err
			}
			return s.orgUse(command, args[0])
		},
	}
}

// orgCurrent reports the organization the selected context runs within.
//
// The three states a machine can be in — no context configured at all, a
// context configured with no organization, and a context with one — are kept
// distinct rather than folded into one blank field: a caller reading the JSON
// tells them apart through Configured and Organization, and prose mode says
// each one in its own sentence, because "" and "no context" read the same in
// a field table and are not the same fact.
func (s Shell) orgCurrent(command *cobra.Command) error {
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

	report := orgCurrentReport{}
	if len(document.Contexts) > 0 {
		selected, err := document.Select("")
		if err != nil {
			return err
		}
		report = orgCurrentReport{
			Configured:   true,
			Context:      selected.Context.Name,
			Organization: selected.Context.Organization,
		}
	}

	if mode == output.ModeJSON {
		return renderContext(s.Streams.Out, mode, report)
	}
	switch {
	case !report.Configured:
		// Worded exactly as wso2 context current words the same state
		// (context.go's contextCurrent): a machine nobody has configured yet
		// has done nothing wrong, and the two commands must not invent two
		// sentences for the one fact.
		_, err = fmt.Fprintln(s.Streams.Out,
			"No context is configured, so commands run against nothing.\n\n"+
				"Run wso2 login to create an identity and a context, "+
				"or wso2 context create <name> --identity <identity> if you already have one.")
	case report.Organization == "":
		_, err = fmt.Fprintf(s.Streams.Out,
			"The %q context is selected and names no organization.\n\n"+
				"Run wso2 org use <organization> to set one.\n", report.Context)
	default:
		err = renderContext(s.Streams.Out, mode, report)
	}
	return err
}

// orgUse sets the selected context's organization, through contexts.Update, so
// a concurrent writer cannot lose it (internal/contexts/save.go's Update holds
// the document lock across the whole read-modify-write).
//
// Selecting the context is done here, not by a second lookup: document.Select
// is what every other command resolves the selected context through, and
// resolving it inside the same Update closure that writes the change is what
// makes the read and the write agree on which context that is, even if the
// document's default changes between an earlier read and this one.
func (s Shell) orgUse(command *cobra.Command, organization string) error {
	mode, err := s.shellOutputMode(command)
	if err != nil {
		return err
	}
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	s.log.Debug("setting the selected context's organization",
		"organization", organization, "document", contexts.Path(root))

	var edited string
	var changed bool
	err = contexts.Update(root, func(document contexts.Document) (contexts.Document, error) {
		if len(document.Contexts) == 0 {
			return document, noContextToEdit()
		}
		selected, err := document.Select("")
		if err != nil {
			return document, err
		}
		edited = selected.Context.Name
		for i := range document.Contexts {
			if document.Contexts[i].Name == edited {
				// Recorded before the write so the warning below can tell a
				// real change from a no-op. Re-running org use with the
				// organization a context already names changes nothing, and a
				// warning that a session no longer matches would be false.
				changed = document.Contexts[i].Organization != organization
				document.Contexts[i].Organization = organization
			}
		}
		return document, nil
	})
	if err != nil {
		return s.explainWriteRefusal(root, err)
	}

	if mode == output.ModeJSON {
		if err := encodeContextJSON(s.Streams.Out,
			orgSelection{Context: edited, Organization: organization}); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintf(s.Streams.Out,
		"\nSet the %q context's organization to %q.\n", edited, organization); err != nil {
		return err
	}

	if !changed {
		return nil
	}

	// This is the fact the whole command exists to surface (#112 task 5): the
	// auth broker narrows a minted token to Context.Organization and refuses
	// outright when it is empty (internal/auth/source.go's checkHomeTenant and
	// developmentSource), so a session signed in while this context named a
	// different organization no longer matches it. The command that changed
	// the field is what has to say so, not just the reference docs — a user
	// who meets an authentication failure on their next command and has to go
	// looking for why was not told by the thing that caused it. Nothing here
	// migrates, invalidates, or re-mints that session; that is beyond this
	// task and beyond #112's settled design.
	//
	// It goes to Err in both renderings, for two reasons. A warning is a
	// diagnostic, not the command's result, so ADR 0003 puts it on Err —
	// prompt.go's confirm states the same rule for the same reason, and on Out
	// this text would corrupt --output json. And a script is the caller most
	// likely to meet the authentication failure it predicts, so it is the last
	// caller that should be the one never told.
	_, err = fmt.Fprintf(s.Streams.Err,
		"\nA session already signed in under the %q context no longer matches: WSO2 CLI binds a "+
			"signed-in session to the organization a context names, and this changed it. If the next "+
			"command fails to authenticate, run wso2 login again.\n", edited)
	return err
}

// noContextToEdit refuses wso2 org use on a machine with no context to select
// an organization on. It is a usage refusal, not a state to report, because
// unlike wso2 org current this command was asked to write something, and there
// is nothing to write it to.
func noContextToEdit() error {
	return problem.New(problem.CategoryUsage, "shell.no_context_configured",
		"no context is configured to set an organization on").
		WithRecovery(noContextRecovery)
}

// The results this family reports. See context.go's comment on the same
// convention for contextCreated and its siblings: rendered here rather than
// through output.Report because a caller must read Configured as a boolean,
// and neither rendering publishes a schema discriminator (#85).
type (
	// orgCurrentReport is what wso2 org current reports.
	orgCurrentReport struct {
		// Configured says whether any context exists to report on. See the
		// identical field on contextCurrent (context.go) for why this is a
		// field rather than an absence.
		Configured   bool   `json:"configured"`
		Context      string `json:"context"`
		Organization string `json:"organization"`
	}

	// orgSelection is what wso2 org use reports.
	orgSelection struct {
		Context      string `json:"context"`
		Organization string `json:"organization"`
	}
)

func (o orgCurrentReport) fields() [][2]string {
	return [][2]string{
		{"Context", o.Context},
		{"Organization", o.Organization},
	}
}
