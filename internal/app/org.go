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
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// The way back from each subcommand's usage refusals.
const (
	orgListUsage    = "Run wso2 org list."
	orgUseUsage     = "Run wso2 org use <organization>, or on WSO2 Cloud wso2 org use with no argument to choose one in the browser."
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
const noContextRecovery = contextSetupHint

// organizationUnitID is the shape of a ThunderID organization unit id.
var organizationUnitID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// orgCommand builds the wso2 org tree.
//
// wso2 org list is refused by name (ADR 0024): nothing lists the organizations
// a user belongs to, so the command exists only to say so and to point at
// the sign-in, where an organization is chosen.
func (s Shell) orgCommand() *cobra.Command {
	command := &cobra.Command{
		Use:                   "org <subcommand>",
		Short:                 "Show and change the organization the selected context runs within.",
		Long:                  orgRecovery,
		DisableFlagsInUseLine: true,
		// A RunE is declared here because Cobra validates a non-leaf command's
		// arguments only when the command is Runnable: leave it nil and
		// wso2 org bogus prints help and exits 0, reporting a typo as success.
		//
		// The arm below refuses an unrecognised subcommand with this family's
		// own message. A bare wso2 org is the other arm, and is deliberately
		// not a refusal. See helpForBareFamily.
		RunE: func(command *cobra.Command, args []string) error {
			if len(args) == 0 {
				return helpForBareFamily(command)
			}
			return problem.New(problem.CategoryUsage, "shell.unknown_command",
				fmt.Sprintf("%q is not a wso2 org subcommand", args[0])).
				WithRecovery(orgRecovery)
		},
	}
	// The family always acts on the selected context, exactly as wso2 context
	// does: naming one with --context would be a second answer to a question the
	// family does not ask, since org use writes the selected context's
	// Organization field, not some other one's.
	command.AddCommand(s.orgListCommand(), s.orgCurrentCommand(), s.orgUseCommand())
	return command
}

func (s Shell) orgListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the organizations this user belongs to.",
		// Kept out of help until something lists organizations; typing it
		// still gets the refusal that says how to choose one.
		Hidden: true,
		Args:   noArguments(orgListUsage),
		RunE: func(*cobra.Command, []string) error {
			return problem.New(problem.CategoryUsage, "shell.organization_list_unavailable",
				"the organizations this user belongs to cannot be listed: no endpoint lists them yet").
				WithRecovery("Run wso2 org use with no argument to choose an organization in the browser, " +
					"wso2 org current to show the one in use, or wso2 org use <organization-id> to switch " +
					"to one by id.")
		},
	}
}

func (s Shell) orgCurrentCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show the organization the selected context runs within.",
		Args:  noArguments(orgCurrentUsage),
		RunE: func(command *cobra.Command, args []string) error {
			return s.orgCurrent(command)
		},
	}
}

func (s Shell) orgUseCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "use [<organization>]",
		Short: "Set the organization the selected context runs within.",
		Long: "Set the organization the selected context runs within.\n\n" +
			"On WSO2 Cloud, switching signs in again in the browser. With no argument the sign-in shows " +
			"the organization picker and the context records the organization chosen there; an " +
			"organization id signs in to that organization directly.",
		Args: atMostOneArgument(orgUseUsage),
		RunE: func(command *cobra.Command, args []string) error {
			if len(args) == 0 {
				return s.orgChoose(command, args)
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
			Configured: true,
			Context:    selected.Context.Name,
			reportedOrganization: organizationOf(loginSessionOf(root, selected),
				selected.Context.Organization),
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
			output.Hint(s.Streams.Out, "No context is configured, so commands run against nothing.\n\n"+
				contextSetupHint))
	case report.Organization == "":
		_, err = fmt.Fprintf(s.Streams.Out,
			output.Hint(s.Streams.Out, "The %q context is selected and names no organization.\n\n"+
				"Run wso2 org use <organization> to set one.\n"), report.Context)
	default:
		err = renderContext(s.Streams.Out, mode, report)
	}
	return err
}

// orgUse sets the selected context's organization, through contexts.Update, so
// a concurrent writer cannot lose it (internal/contexts/save.go's Update holds
// the document lock across the whole read-modify-write).
//
// Selecting the context is done inside the Update closure that writes the
// change: document.Select is what every other command resolves the selected
// context through, and resolving it under the same lock is what makes the
// read and the write agree on which context that is. A WSO2 Cloud context is
// recognised by an earlier lookup, since switching it signs in before
// anything is written; its write selects again under the lock
// (recordOrganization).
func (s Shell) orgUse(command *cobra.Command, organization string) error {
	mode, err := s.shellOutputMode(command)
	if err != nil {
		return err
	}
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	if selected, err := s.selection(""); err == nil && selected.Context.WSO2Cloud() {
		return s.orgUseCloud(mode, root, selected, organization)
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
		if err := refuseOrganizationSwitch(selected.Identity, organization); err != nil {
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
		if err := encodeJSON(s.Streams.Out,
			orgSelection{Context: edited, Organization: organization}); err != nil {
			return err
		}
	} else if organization == "" {
		// Worded apart from the sentence below for the reason orgCurrent
		// keeps its own states apart: "" and an organization are not the same
		// fact, and "organization to \"\"" reads as a value rather than as the
		// unset field it is.
		if _, err := fmt.Fprintf(s.Streams.Out,
			"\nCleared the %q context's organization; commands run against the deployment "+
				"the context logs in to.\n", edited); err != nil {
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
		Configured bool   `json:"configured"`
		Context    string `json:"context"`
		reportedOrganization
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
		{"Organization", o.label},
	}
}

// refuseOrganizationSwitch refuses to record an organization on a context
// whose identity provider has no organization switch, before the document is
// written: the auth broker would refuse every later command with the same
// code (internal/auth/source.go's checkHomeTenant), and a value accepted here
// only to break every call is worse than one refused at the flag.
//
// Clearing the field is not a switch and is never refused. A document written
// before this check, or by hand, can carry an organization on such a provider,
// and then every product command is refused by the broker telling the user to
// leave the organization unset — which this command is the only writer of. A
// refusal here would leave that state with no way out of itself.
//
// Asgardeo, and any provider the document does not name, keep the field: a
// tenant switch is what the broker's check exists for on them.
func refuseOrganizationSwitch(identity contexts.Account, organization string) error {
	if organization == "" {
		return nil
	}
	switch identity.Auth.Provider {
	case contexts.ProviderThunder, contexts.ProviderIdentityServer:
	default:
		return nil
	}
	return problem.New(problem.CategoryAuthPolicy, "auth.organization_switch_unsupported",
		fmt.Sprintf("the %q context logs in against %s, which has no organization to switch to",
			identity.Name, identity.Auth.Provider)).
		WithRecovery("Leave the context's organization unset; commands run against the deployment " +
			"the context logs in to. Organization switch is for a multi-tenant provider such as Asgardeo.")
}

// orgChoose is wso2 org use with no argument: on WSO2 Cloud, a sign-in that
// lets the person choose the organization; elsewhere, the refusal for a
// missing organization.
func (s Shell) orgChoose(command *cobra.Command, args []string) error {
	selected, err := s.selection("")
	var reported problem.Problem
	if err != nil && (!errors.As(err, &reported) || reported.Code != "contexts.no_context_selected") {
		return err
	}
	if err != nil || !selected.Context.WSO2Cloud() {
		return exactlyOneArgument("the name of an organization", orgUseUsage)(command, args)
	}
	mode, err := s.shellOutputMode(command)
	if err != nil {
		return err
	}
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	return s.orgUseCloud(mode, root, selected, "")
}

// orgUseCloud switches a WSO2 Cloud context to another organization. A
// ThunderID token is issued in one organization, chosen in the browser at
// sign-in, so switching is signing in again at that organization's own
// endpoints. The sessions of the previous organization are ended, every
// product is authorized in the new one, and only then is the organization's
// id recorded on the context. A sign-in that fails leaves the previous
// organization recorded and the context signed in to none.
//
// The organization is named by id. The handle or name of the organization the
// stored session was issued in resolves to its id; no other can be resolved,
// since nothing lists a user's organizations. Named by none, the sign-in asks
// the person to sign in again, where ThunderID shows its organization picker,
// and the organization it was issued in is the one recorded.
func (s Shell) orgUseCloud(mode output.Mode, root string, selected contexts.Selection, organization string) error {
	current := auth.Organization{}
	if stored := loginSessionOf(root, selected); stored != nil {
		current = auth.TokenOrganization(stored.IDToken, stored.AccessToken)
	}
	id := organization
	if organization != "" && current.ID != "" &&
		(organization == current.ID || organization == current.Handle || organization == current.Name) {
		id = current.ID
	}
	name := selected.Context.Name
	if id != "" && id == current.ID {
		if err := s.recordOrganization(root, name, id); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(s.Streams.Err, "Already signed in to this organization, so no new sign-in was needed."); err != nil {
			return err
		}
		return s.reportOrgUse(mode, name, id)
	}
	if id != "" && !organizationUnitID.MatchString(id) {
		// Refused before anything ends: only the organization logged in to
		// can be named by handle or name, and any other value is sent to the
		// provider as an id.
		return problem.New(problem.CategoryUsage, "shell.invalid_argument",
			fmt.Sprintf("%q is not an organization id, and not the handle or name of the organization the %q "+
				"context is logged in to", organization, name)).
			WithRecovery("Run wso2 org use <organization-id>; wso2 whoami -o json shows the id of the " +
				"organization a login was issued in.")
	}
	if control := s.nonInteractiveControl(false); control != "" {
		again := "wso2 org use"
		if organization != "" {
			again += " " + organization
		}
		return problem.New(problem.CategoryAuthPolicy, "auth.non_interactive",
			fmt.Sprintf("switching the %q context to another organization logs in again in the browser, "+
				"which %s asked not to open", name, control)).
			WithRecovery(fmt.Sprintf("Run %s where a browser can open.", again))
	}
	// The products a login of the context installs and records, as wso2
	// login does, so the switch authorizes the same ones.
	if err := s.prepareCloudLogin(loginFlags{contextName: name}); err != nil {
		return err
	}
	selected, err := s.selection(name)
	if err != nil {
		return err
	}

	store := session.Store{StateRoot: root}
	endAll := func() error {
		for _, access := range selected.Identity.Accesses() {
			if _, err := s.endSession(store, access); err != nil {
				return err
			}
		}
		return nil
	}
	if err := endAll(); err != nil {
		return err
	}
	switching := selected
	switching.Context.Organization = id
	outcome, err := s.establishAndStore(switching, loginFlags{contextName: name, chooseOrganization: id == ""})
	if err == nil && id == "" {
		id = issuedIn(outcome.first).ID
		switching.Context.Organization = id
	}
	if err == nil {
		err = s.recordOrganization(root, name, id)
	}
	if err != nil {
		// Whatever this login established is in an organization the context
		// does not record, so it goes too.
		return orgUseUnfinished(err, name, selected.Context.Organization, endAll())
	}
	if mode == output.ModeJSON {
		return encodeJSONWithNext(s.Streams.Out, orgSelection{Context: name, Organization: id},
			s.loginNext(switching, outcome))
	}
	label := strconv.Quote(id)
	if issued := issuedIn(outcome.first); !issued.IsZero() {
		label = issued.Label()
	}
	if _, err := fmt.Fprintf(s.Streams.Out, "\nLogged the %q context in to the %s organization.\n", name, label); err != nil {
		return err
	}
	if err := s.reportLogin(switching, outcome); err != nil {
		return err
	}
	return output.NextStep(s.Streams.Out, s.loginNext(switching, outcome))
}

// recordOrganization writes a context's organization, selecting the context
// again under the document lock, as orgUse does, so the write lands on the
// context the sessions were established for.
func (s Shell) recordOrganization(root, name, organization string) error {
	err := contexts.Update(root, func(document contexts.Document) (contexts.Document, error) {
		if still, err := document.Select(""); err != nil || still.Context.Name != name {
			return document, problem.New(problem.CategoryUsage, "contexts.document_busy",
				"the selected context changed while the organization was being set").
				WithRecovery("Run the same wso2 org use again.")
		}
		for i := range document.Contexts {
			if document.Contexts[i].Name == name {
				document.Contexts[i].Organization = organization
			}
		}
		return document, nil
	})
	if err != nil {
		return s.explainWriteRefusal(root, err)
	}
	return nil
}

// orgUseUnfinished says, on a switch whose login failed, where the context
// stands: the previous organization still recorded, its sessions ended, and
// the login that restores them.
func orgUseUnfinished(err error, name, previous string, cleanup error) error {
	var reported problem.Problem
	if !errors.As(err, &reported) {
		return errors.Join(err, cleanup)
	}
	recorded := fmt.Sprintf("still names the %q organization", previous)
	if previous == "" {
		recorded = "still names no organization"
	}
	note := fmt.Sprintf("The %q context %s; its login and product sessions have ended, and wso2 login "+
		"restores them.", name, recorded)
	if cleanup != nil {
		note = fmt.Sprintf("The %q context %s; some of its sessions may not have ended, so run wso2 logout "+
			"--context %s, then wso2 login --context %s.", name, recorded, name, name)
	}
	if reported.Recovery != "" {
		note = reported.Recovery + " " + note
	}
	extended := withRecovery(err, reported, note)
	if cleanup != nil {
		return fmt.Errorf("%w: %w", extended, cleanup)
	}
	return extended
}

// reportOrgUse reports the organization a context now names.
func (s Shell) reportOrgUse(mode output.Mode, name, organization string) error {
	if mode == output.ModeJSON {
		return encodeJSON(s.Streams.Out, orgSelection{Context: name, Organization: organization})
	}
	sentence := fmt.Sprintf("\nSet the %q context's organization to %q.\n", name, organization)
	if organization == "" {
		sentence = fmt.Sprintf("\nCleared the %q context's organization; the next login chooses one in "+
			"the browser.\n", name)
	}
	_, err := fmt.Fprint(s.Streams.Out, sentence)
	return err
}
