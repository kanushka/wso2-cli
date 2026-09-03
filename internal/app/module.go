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
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/install"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// catalogTimeout bounds one install. It covers the whole operation rather than
// one request, so a stalled origin cannot hold a command open indefinitely.
const catalogTimeout = 10 * time.Minute

// The way back from each subcommand's usage refusals.
const (
	moduleAvailableUsage = "Run wso2 module available."
	moduleListUsage      = "Run wso2 module list."
	moduleInstallUsage   = "Run wso2 module install <module> [--channel <channel>]."
	moduleRemoveUsage    = "Run wso2 module remove <module> [--yes] [--dry-run] [--no-input]."
	moduleUpdateUsage    = "Run wso2 module update <module> [--yes] [--dry-run] [--no-input], " +
		"or wso2 module update --all [--yes] [--dry-run] [--no-input]."
)

const moduleRecovery = "Run wso2 module available to see what can be installed, " +
	"wso2 module install <module> to install one, wso2 module update --all to update what is " +
	"installed, or wso2 module remove <module> to take one off this machine."

// moduleCommand builds the wso2 module tree.
//
// Every subcommand below declares its own flags directly (#89): available,
// install, list, remove, and update all had DisableFlagParsing and hand-scanned
// their own argument list until now. install's --channel and update's --all
// were the two spellings a user could get wrong with no recovery guidance
// beyond a hand-written switch's own message; the rest (--yes, --dry-run,
// --no-input) scanned the same list but never had a spelling problem, since a
// bare boolean flag has only one to get right. Declaring all of them together
// is what retires the loops rather than leaving a mix of declared and scanned
// flags in the same command bodies.
func (s Shell) moduleCommand() *cobra.Command {
	command := &cobra.Command{
		Use:                   "module <subcommand>",
		Short:                 "Install, list, and update product modules from the module catalog.",
		Long:                  moduleRecovery,
		DisableFlagsInUseLine: true,
		// A RunE is declared for the reason org's and identity's are: Cobra
		// validates a non-leaf command's arguments only when it is Runnable,
		// so leaving this nil would print help and exit 0 for a mistyped
		// subcommand. Never cobra.NoArgs or cobra.ExactArgs here — both bypass
		// the flag-error hook and exit 70 instead of 64.
		//
		// A bare wso2 module is the other arm, and is deliberately not a
		// refusal. See helpForBareFamily.
		RunE: func(command *cobra.Command, args []string) error {
			if len(args) == 0 {
				return helpForBareFamily(command)
			}
			return problem.New(problem.CategoryUsage, "shell.unknown_command",
				fmt.Sprintf("%q is not a wso2 module subcommand", args[0])).
				WithRecovery(moduleRecovery)
		},
	}
	// The family declares neither shell flag, so every module subcommand
	// refuses both. Every module lifecycle command renders fixed, non-JSON text
	// and selects no context: an install or an update names its target as an
	// argument, not by --context, and its report is prose meant to be read, not
	// a schema a script parses. moduleinstall_test.go's
	// TestVerboseInstallKeepsProgressOffStdout confirms by hand that
	// wso2 module install <module> --output json is refused outright, and this
	// absence is where that refusal now comes from.
	command.AddCommand(s.moduleAvailableCommand(), s.moduleInstallCommand(), s.moduleListCommand(),
		s.moduleRemoveCommand(), s.moduleUpdateCommand())
	return command
}

func (s Shell) moduleAvailableCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "available",
		Short: "List the product modules the catalog publishes.",
		Args:  noArguments(moduleAvailableUsage),
		RunE: func(command *cobra.Command, args []string) error {
			return s.moduleAvailable()
		},
	}
	// Every module subcommand recovers with its own usage line, so a mistyped
	// flag here names this command rather than the shell's general help.
	command.SetFlagErrorFunc(func(command *cobra.Command, err error) error {
		return flagProblemWithRecovery(command, err, moduleAvailableUsage)
	})
	return command
}

func (s Shell) moduleListCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "list",
		Short: "Report the installed modules and which have an update available.",
		Args:  noArguments(moduleListUsage),
		RunE: func(command *cobra.Command, args []string) error {
			return s.moduleList()
		},
	}
	command.SetFlagErrorFunc(func(command *cobra.Command, err error) error {
		return flagProblemWithRecovery(command, err, moduleListUsage)
	})
	return command
}

func (s Shell) moduleInstallCommand() *cobra.Command {
	var channel string
	command := &cobra.Command{
		Use:   "install <module>",
		Short: "Install one product module from the catalog.",
		Args:  exactlyOneArgument("a module to install", moduleInstallUsage),
		RunE: func(command *cobra.Command, args []string) error {
			// The module may be named as "<module>@<version>" to pin an exact
			// version; Cut on an absent "@" leaves version empty, which is
			// catalog.Policy's zero value for "no pin".
			namespace, version, _ := strings.Cut(args[0], "@")
			policy := catalog.Policy{Channel: channel, Version: version}
			if policy.Channel != "" && policy.Version != "" {
				return problem.New(problem.CategoryUsage, "shell.conflicting_arguments",
					"a pinned version and a channel cannot both be given").
					WithRecovery("Pin a version, or choose a channel, but not both.")
			}
			return s.moduleInstall(namespace, policy)
		},
	}
	command.Flags().StringVar(&channel, "channel", "",
		"Install the newest version on this release channel, instead of the default.")
	// A tailored FlagErrorFunc, not the root's inherited one: a missing
	// --channel value or an unknown flag on this command should point back at
	// this command's own usage rather than at the generic "wso2 help".
	command.SetFlagErrorFunc(func(command *cobra.Command, err error) error {
		return flagProblemWithRecovery(command, err, moduleInstallUsage)
	})
	return command
}

func (s Shell) moduleRemoveCommand() *cobra.Command {
	var opts removeOptions
	command := &cobra.Command{
		Use:   "remove <module>",
		Short: "Take one installed module off this machine.",
		Args:  exactlyOneArgument("the module to remove", moduleRemoveUsage),
		RunE: func(command *cobra.Command, args []string) error {
			opts.namespace = args[0]
			return s.moduleRemove(opts)
		},
	}
	command.Flags().BoolVar(&opts.yes, "yes", false, "Remove without asking for confirmation.")
	command.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Show what would be removed without removing it.")
	command.Flags().BoolVar(&opts.noInput, "no-input", false, "Refuse rather than prompt for confirmation.")
	command.SetFlagErrorFunc(func(command *cobra.Command, err error) error {
		return flagProblemWithRecovery(command, err, moduleRemoveUsage)
	})
	return command
}

func (s Shell) moduleUpdateCommand() *cobra.Command {
	var opts updateOptions
	var all bool
	command := &cobra.Command{
		Use:   "update <module...> | update --all",
		Short: "Bring installed modules to the newest version their channel publishes.",
		// Not exactlyOneArgument or noArguments: this command takes zero or
		// more module names, and which count is valid depends on --all, so the
		// combination is checked in RunE once both are parsed, exactly as
		// parseUpdateArguments used to check it by hand.
		Args: cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, args []string) error {
			opts.namespaces = args
			if all && len(opts.namespaces) > 0 {
				// The problem code here is load-bearing: it is the same
				// "naming a module together with --all is ambiguous" refusal
				// this command has always given, now reached by a declared
				// flag instead of a hand-written scan, and an automation
				// contract keyed on this code must not see it change.
				return problem.New(problem.CategoryUsage, "shell.conflicting_arguments",
					"--all updates every installed module, so naming one as well is ambiguous").
					WithRecovery("Run wso2 module update <module>, or wso2 module update --all.")
			}
			if !all && len(opts.namespaces) == 0 {
				return problem.New(problem.CategoryUsage, "shell.missing_argument",
					"wso2 module update needs a module, or --all").
					WithRecovery("Run wso2 module update <module>, or wso2 module update --all.")
			}
			return s.moduleUpdate(opts)
		},
	}
	command.Flags().BoolVar(&all, "all", false, "Update every installed module that is not pinned.")
	command.Flags().BoolVar(&opts.yes, "yes", false, "Update without asking for confirmation.")
	command.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Show what would be updated without updating it.")
	command.Flags().BoolVar(&opts.noInput, "no-input", false, "Refuse rather than prompt for confirmation.")
	command.SetFlagErrorFunc(func(command *cobra.Command, err error) error {
		return flagProblemWithRecovery(command, err, moduleUpdateUsage)
	})
	return command
}

// moduleRemove takes one installed module off this machine.
//
// Exactly one namespace is named. Removing several at once would have to decide
// what a run that failed halfway had done, and a user removing one module at a
// time never has to ask.
//
// What is removed is the module: its versions, its receipts, its active-version
// pointer, and its version policy. What is not removed is everything else. A
// user who removes a module has said nothing about their identity, so the
// credential store and the configuration are left exactly as they were — this
// is not a logout.
//
// #112 §7: this used to remove on sight, with no confirmation, no --yes, and
// no --dry-run. The store is asked whether the namespace is installed before
// anything else runs, deliberately in that order: confirming the removal of
// something that turns out not to be there would ask the wrong question, and
// a typo would look, for one prompt, like a real module about to be deleted.
func (s Shell) moduleRemove(opts removeOptions) error {
	if opts.yes && opts.dryRun {
		return conflictingConfirmationFlags("wso2 module remove")
	}

	store, err := s.store()
	if err != nil {
		return err
	}
	installed, err := store.Installed(opts.namespace)
	if err != nil {
		return err
	}
	if !installed {
		return notInstalledProblem(opts.namespace)
	}

	if opts.dryRun {
		_, err := fmt.Fprintf(s.Streams.Out,
			"Would remove the %s module. Nothing was removed.\n", opts.namespace)
		return err
	}

	if !opts.yes {
		if may, reason := s.mayPrompt(opts.noInput); !may {
			return nonInteractiveConfirmation(
				fmt.Sprintf("removing the %s module", opts.namespace), reason)
		}
		confirmed, err := s.confirm(fmt.Sprintf(
			"Remove the %s module? This deletes it from this machine and cannot be undone. [y/N]: ",
			opts.namespace))
		if err != nil {
			return err
		}
		if !confirmed {
			_, err := fmt.Fprintf(s.Streams.Out,
				"Removal cancelled; the %s module is unchanged.\n", opts.namespace)
			return err
		}
	}

	removed, err := store.Remove(opts.namespace)
	if err != nil {
		return err
	}
	if !removed {
		// The check above found it installed; this is the race of another
		// process (or another run of this one) removing it in between, not a
		// second, different kind of failure. Reported the same way as the
		// check, because from the user's side it is the same fact.
		return notInstalledProblem(opts.namespace)
	}

	_, err = fmt.Fprintf(s.Streams.Out, "Removed the %s module.\n", opts.namespace)
	return err
}

// notInstalledProblem reports that a named namespace has nothing installed.
// Reporting success here would tell a user their module is gone when what is
// installed is something else under the name they meant.
func notInstalledProblem(namespace string) problem.Problem {
	return problem.New(problem.CategoryUsage, "shell.module_not_installed",
		fmt.Sprintf("no %s module is installed", namespace)).
		WithRecovery("Run wso2 module list to see what is installed.")
}

// removeOptions is wso2 module remove's parsed arguments.
type removeOptions struct {
	namespace string
	yes       bool
	dryRun    bool
	noInput   bool
}

// conflictingConfirmationFlags refuses --yes and --dry-run together: one
// skips the confirmation and acts, the other skips acting entirely, and a
// command asked to do both at once has been given no coherent instruction.
func conflictingConfirmationFlags(command string) problem.Problem {
	return problem.New(problem.CategoryUsage, "shell.conflicting_arguments",
		fmt.Sprintf("%s cannot take both --yes and --dry-run", command)).
		WithRecovery("Pass --yes to act without a prompt, or --dry-run to preview without acting, but not both.")
}

// nonInteractiveConfirmation reports that a destructive action needed
// confirmation and mayPrompt refused it, naming which control fired: subject
// is what needed confirming, and reason is mayPrompt's own wording for why it
// would not ask.
//
// The recovery is chosen from that same reason rather than fixed, because
// only one of the three ways out is true at a time. mayPrompt consults
// --no-input and WSO2_NO_INPUT before it looks at the terminal, so in CI —
// where a non-interactive control is usually set deliberately — a recovery
// saying "run this where standard input is a terminal" advises a change that
// would not alter the outcome. A refusal that already knows which control
// fired has no excuse for naming a different one.
//
// This carries CategoryUsage and shell.non_interactive where login.go's gate
// on the same shared predicate carries CategoryAuthPolicy and
// auth.non_interactive. The predicate is shared so the two cannot disagree
// about whether anything may be asked; the reports differ because the
// refusals are not the same refusal. Login's says access could not be
// acquired and its documented exit class 77 tells a script to go and
// authenticate; this one says the invocation was incomplete, and its exit
// class 64 tells the same script to add --yes. One code for both would have
// to lie about one of them.
func nonInteractiveConfirmation(subject, reason string) problem.Problem {
	recovery := "Pass --yes to proceed without a prompt, or run this where standard input is a terminal."
	switch reason {
	case reasonNoInputFlag:
		recovery = "Pass --yes to proceed without a prompt, or drop --no-input to be asked."
	case reasonNoInputEnv:
		recovery = "Pass --yes to proceed without a prompt, or unset " + NoInputEnvVar + " to be asked."
	}
	return problem.New(problem.CategoryUsage, "shell.non_interactive",
		fmt.Sprintf("%s needs to be confirmed, and %s", subject, reason)).
		WithRecovery(recovery)
}

// moduleInstall installs one product module from the catalog.
//
// The module may be named as "<module>@<version>" to install an exact version,
// which is what a pipeline pins so its behaviour does not depend on what is
// newest that day. Without a pin, the newest version on the chosen channel that
// this shell can launch on this platform is installed. moduleInstallCommand's
// RunE has already split the "@" and refused a pin given together with
// --channel, so policy arrives here as exactly what this install acts under.
func (s Shell) moduleInstall(namespace string, policy catalog.Policy) error {
	installer, err := s.installer()
	if err != nil {
		return err
	}
	// A failed install turns on which catalog was asked, and the origin is read
	// from an environment variable, so a user pointed at the wrong one has no
	// other way to see it. The policy is logged beside it because "no version
	// matched" means nothing without the constraint that failed to match.
	s.log.Debug("installing a module from the catalog",
		"namespace", namespace,
		"catalog_origin", installer.Client.Origin,
		"channel", policy.Channel,
		"pinned_version", policy.Version)

	ctx, cancel := context.WithTimeout(context.Background(), catalogTimeout)
	defer cancel()
	installed, err := installer.Run(ctx, install.Request{Namespace: namespace, Policy: policy})
	if err != nil {
		return err
	}
	s.log.Debug("the module was installed",
		"namespace", installed.Namespace,
		"selected_version", installed.Version,
		"platform", installed.Platform.String())

	_, err = fmt.Fprintf(s.Streams.Out,
		"Installed %s v%s for %s.\nThe artifact was checked against the digest the catalog publishes. "+
			"Artifacts are integrity-checked, not signed.\n",
		installed.Namespace, installed.Version, installed.Platform)
	return err
}

// installer builds the installer this invocation uses: one store, one catalog
// origin, and this shell's own identity.
func (s Shell) installer() (install.Installer, error) {
	store, err := s.store()
	if err != nil {
		return install.Installer{}, err
	}
	identity, err := s.identity()
	if err != nil {
		return install.Installer{}, err
	}
	root, err := s.stateRoot()
	if err != nil {
		return install.Installer{}, err
	}
	return install.Installer{
		Store: store,
		Client: catalog.Client{
			Origin: catalog.Origin(root),
			// The client is told where its origin came from and given the
			// diagnostic log so a failure to reach the origin can name the
			// actual fix (wso2 config, when a preference chose it) and keep
			// the raw transport error under --verbose. See
			// catalog.Client.originUnreachable.
			OriginConfigured: catalog.OriginConfigured(root),
			HTTP:             &http.Client{},
			Log:              s.log,
		},
		Shell: identity,
		// The archive's size is known before Download starts (it comes from
		// the catalog entry, not a response header), so the factory only
		// needs the namespace being downloaded (for the rendered label, so
		// an update moving several modules draws which one) and that size;
		// which of the three renderings comes back is decided by
		// output.NewProgress from s.Streams.Err and whether --verbose has
		// turned s.log on.
		Progress: func(namespace string, total int64) output.Progress {
			return output.NewProgress(s.Streams.Err, s.log, namespace, total, output.SystemClock{})
		},
	}, nil
}

// moduleAvailable lists the product modules the catalog publishes, so what can
// be installed is discoverable from the shell rather than from documentation.
//
// It costs one request: the index carries the latest version on each channel
// for every namespace, and nothing here selects a specific version.
func (s Shell) moduleAvailable() error {
	installer, err := s.installer()
	if err != nil {
		return err
	}
	// Same reason the install log names it: the origin is read from an
	// environment variable, so a listing that comes back short or empty is
	// unreadable without the catalog it came from.
	s.log.Debug("listing the catalog",
		"catalog_origin", installer.Client.Origin)
	ctx, cancel := context.WithTimeout(context.Background(), catalogTimeout)
	defer cancel()
	available, err := installer.Available(ctx)
	if err != nil {
		return err
	}

	if len(available) == 0 {
		_, err := fmt.Fprintln(s.Streams.Out, "The module catalog publishes no modules.")
		return err
	}
	table := output.NewTable("module", "channel", "version")
	for _, module := range available {
		for _, channel := range module.Channels {
			table.Append(module.Namespace, channel.Channel, "v"+channel.Version)
		}
	}
	if err := table.Render(s.Streams.Out); err != nil {
		return err
	}
	_, err = fmt.Fprintln(s.Streams.Out,
		"\nRun wso2 module install <module> to install one.")
	return err
}

// moduleList reports the installed modules and which of them have an update
// available on the channel each one follows.
//
// The whole report costs one request whatever is installed, because the index
// carries the latest version per channel and no version history is fetched: a
// check selects nothing, and selecting is what a history is for.
func (s Shell) moduleList() error {
	installer, err := s.installer()
	if err != nil {
		return err
	}
	// The update column is an answer about a catalog, so it is unreadable
	// without the one that answered — and this command reaches the network for
	// the same origin an install would.
	s.log.Debug("checking installed modules against the catalog",
		"catalog_origin", installer.Client.Origin)
	ctx, cancel := context.WithTimeout(context.Background(), catalogTimeout)
	defer cancel()
	statuses, err := installer.Check(ctx)
	if err != nil {
		var unreachable problem.Problem
		if !errors.As(err, &unreachable) || unreachable.Code != "catalog.origin_unreachable" {
			return err
		}
		// An unreachable catalog fails only half of this command's question.
		// What is installed is purely local — wso2 version already answers it
		// offline — so the installed half is still reported, the update column
		// says "unknown" rather than guessing, and the catalog failure is
		// downgraded to a diagnostic (fix round 2, F4). The run exits 0: the
		// stderr warning, not the exit code, is where the degraded half is
		// reported, the same contract a corrupt preferences document already
		// has. wso2 module available, whose whole question is the catalog,
		// still fails outright.
		return s.moduleListOffline(installer, unreachable)
	}

	if len(statuses) == 0 {
		_, err := fmt.Fprintln(s.Streams.Out, "No modules are installed.")
		return err
	}
	table := output.NewTable("module", "installed", "channel", "update")
	for _, status := range statuses {
		table.Append(status.Namespace, "v"+status.Installed, channelColumn(status), updateColumn(status))
	}
	if err := table.Render(s.Streams.Out); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(s.Streams.Out); err != nil {
		return err
	}
	for _, line := range listSummary(statuses) {
		if _, err := fmt.Fprintln(s.Streams.Out, line); err != nil {
			return err
		}
	}
	return nil
}

// moduleListOffline renders the installed modules from local state alone,
// with the catalog failure that forced it reported as a warning.
//
// The update column says "unknown" for every module the catalog would have
// been asked about: nothing was asked, which is a different fact from "the
// catalog publishes nothing", and reusing the unpublished wording would claim
// the catalog answered. A pin is a local fact, so it is still reported. The
// summary lines are omitted — every one of them is a claim about the catalog's
// answer — and the warning beneath the table is what accounts for the missing
// column, carrying the same recovery the hard failure would have (which names
// wso2 config unset when the "catalog-origin" preference chose the origin).
func (s Shell) moduleListOffline(installer install.Installer, unreachable problem.Problem) error {
	statuses, err := installer.CheckLocal()
	if err != nil {
		return err
	}
	if len(statuses) == 0 {
		_, err := fmt.Fprintln(s.Streams.Out, "No modules are installed.")
		return err
	}
	table := output.NewTable("module", "installed", "channel", "update")
	for _, status := range statuses {
		update := "unknown"
		if status.Pinned {
			update = "pinned to v" + status.PinnedVersion
		}
		table.Append(status.Namespace, "v"+status.Installed, channelColumn(status), update)
	}
	if err := table.Render(s.Streams.Out); err != nil {
		return err
	}
	output.Diagnostic(s.Streams.Err, problem.New(problem.CategoryModuleProcess, unreachable.Code,
		"the module catalog could not be reached, so update availability is unknown").
		WithRecovery(unreachable.Recovery))
	return nil
}

// moduleState is the one classification wso2 module list reports.
//
// The UPDATE column and the summary beneath the table both derive from it, so
// they cannot disagree. They used to disagree: the column distinguished four
// states and the summary was driven by the Update boolean alone, which is false
// for three of them. A pinned module has Update false because a pin holds it
// where the user put it, and an unpublished one has it false because there is
// nothing to compare against; neither is a statement that the installed version
// is current, and the summary called both current. #143, and #135 one layer
// down.
type moduleState int

const (
	// stateUpdatable has a newer version on the channel it follows.
	stateUpdatable moduleState = iota
	// statePinned is held at an exact version. Installer.statuses does resolve
	// Available and compare it for a pinned module — it suppresses Update
	// afterwards rather than skipping the comparison — so what is true of a pin
	// is that the module will not be moved, not that nothing was asked.
	statePinned
	// stateUnpublished follows a channel the catalog publishes no version of
	// this module on, so whether it is current is unknown, not true.
	stateUnpublished
	// stateCurrent is at the newest version its channel publishes.
	stateCurrent
)

// stateOf classifies one module. The order matters: a pin overrides the channel
// (catalog.Policy documents that), so it is asked about first.
func stateOf(status install.Status) moduleState {
	switch {
	case status.Pinned:
		return statePinned
	case status.Update:
		return stateUpdatable
	case status.Available == "":
		return stateUnpublished
	default:
		return stateCurrent
	}
}

// listSummary accounts for every installed module beneath the table.
//
// It returns one line per state that has any modules in it, so nothing is
// folded into a claim that is not true of it, and it names a command wherever
// there is something to run. A pin names none on purpose: a pinned module is
// where the user put it, and there is nothing to do about it.
//
// The short, reassuring line survives for the case it is right for — every
// module genuinely at the newest version its channel publishes — because that
// case is the common one.
func listSummary(statuses []install.Status) []string {
	counts := map[moduleState]int{}
	for _, status := range statuses {
		counts[stateOf(status)]++
	}
	if counts[stateCurrent] == len(statuses) {
		return []string{"Every installed module is current."}
	}

	var lines []string
	if n := counts[stateUpdatable]; n > 0 {
		lines = append(lines, fmt.Sprintf(
			"%d module(s) have an update available. Run wso2 module update --all to take them.", n))
	}
	if n := counts[stateCurrent]; n > 0 {
		lines = append(lines, fmt.Sprintf("%d module(s) are current.", n))
	}
	if n := counts[statePinned]; n > 0 {
		lines = append(lines, fmt.Sprintf(
			"%d module(s) are pinned and will not be updated.", n))
	}
	if n := counts[stateUnpublished]; n > 0 {
		lines = append(lines, fmt.Sprintf(
			"%d module(s) are not published on the channel they follow, so whether they are "+
				"current is unknown. Run wso2 module available to see what the catalog publishes.", n))
	}
	return lines
}

// channelColumn says which channel a module follows, or says nothing when it
// follows none. A pin overrides the channel — catalog.Policy documents that,
// and it is why a pinned prerelease is installable without putting the module
// on that channel — so a module pinned with no channel recorded has no
// followed channel to name while the pin holds. The UPDATE column already
// declines to derive a verdict from a channel in that case; this is the same
// honesty one column to the left. #128.
func channelColumn(status install.Status) string {
	if status.Pinned && status.PolicyChannel == "" {
		return "—"
	}
	return status.Channel
}

// updateColumn says what is available for one module in the terms that decide
// it: an update to take, a pin holding it where it is, or a channel the catalog
// publishes nothing on.
func updateColumn(status install.Status) string {
	switch stateOf(status) {
	case statePinned:
		return "pinned to v" + status.PinnedVersion
	case stateUpdatable:
		return "v" + status.Available + " available"
	case stateUnpublished:
		return "not published"
	default:
		return "current"
	}
}

// moduleUpdate brings installed modules to the newest version their own channel
// publishes.
//
// A pinned module is passed over rather than moved, so updating everything else
// cannot silently take a module off the version it is held at. A module whose
// update is refused keeps the version that was active before the run, and the
// refusal is reported rather than swallowed.
//
// #112 §7 named wso2 module update --all specifically as acting immediately
// with no confirmation, no --yes, and no --dry-run, so the confirmation guards
// only the unnamed, --all form. The reason is unboundedness, not
// destructiveness: activate (install.go) never deactivates a module until a
// replacement has been downloaded and verified into its own version
// directory, and moving to a new version never touches the directory the
// previous version lives in — each version has its own path under
// versions/, so a named update is recoverable and not especially dangerous
// even without a prompt. --all is what is unbounded — it is the run whose
// scope is "however many modules happen to be installed", decided by the
// state of the machine rather than by anything the caller typed. --dry-run
// and --yes are accepted on either form; --yes is simply a no-op when
// nothing was going to ask.
func (s Shell) moduleUpdate(opts updateOptions) error {
	if opts.yes && opts.dryRun {
		return conflictingConfirmationFlags("wso2 module update")
	}

	installer, err := s.installer()
	if err != nil {
		return err
	}

	if opts.dryRun {
		return s.reportUpdatePlan(installer, opts.namespaces)
	}

	if len(opts.namespaces) == 0 && !opts.yes {
		// NothingWouldMove is a local-only, network-free check: nothing
		// installed, or everything installed pinned. Asking permission to do
		// nothing trains a person to answer without reading, so this run
		// skips straight to reporting that outcome instead of asking first.
		// It is deliberately not a full "would this change anything" answer
		// (that costs the same index request the run itself pays), so this
		// can still ask before a run that turns out to find everything
		// already current.
		skip, err := installer.NothingWouldMove(opts.namespaces)
		if err != nil {
			return err
		}
		if !skip {
			if may, reason := s.mayPrompt(opts.noInput); !may {
				return nonInteractiveConfirmation("updating every installed module", reason)
			}
			confirmed, err := s.confirm(
				"This updates every installed module that has a newer version on its channel. Continue? [y/N]: ")
			if err != nil {
				return err
			}
			if !confirmed {
				_, err := fmt.Fprintln(s.Streams.Out, "Update cancelled; nothing was changed.")
				return err
			}
		}
	}

	// An empty namespace list is wso2 module update --all, so what was asked
	// for is logged as it was parsed rather than as it was typed: an update
	// that moved a module the user did not name is read here.
	s.log.Debug("updating modules from the catalog",
		"namespaces", strings.Join(opts.namespaces, " "),
		"all", len(opts.namespaces) == 0,
		"catalog_origin", installer.Client.Origin)
	ctx, cancel := context.WithTimeout(context.Background(), catalogTimeout)
	defer cancel()
	outcomes, err := installer.Update(ctx, opts.namespaces)
	if err != nil {
		return err
	}
	if len(outcomes) == 0 {
		_, err := fmt.Fprintln(s.Streams.Out, "No modules are installed.")
		return err
	}

	var failures []error
	for _, outcome := range outcomes {
		line, failure := updateLine(outcome)
		if _, err := fmt.Fprintln(s.Streams.Out, line); err != nil {
			return err
		}
		if failure != nil {
			failures = append(failures, failure)
		}
	}
	if len(failures) == 0 {
		return nil
	}
	// Every module is attempted, every refusal is reported, and the first is
	// what the run exits on, so a run that moved some modules and refused
	// others is neither silent about the refusals nor reported as a success.
	for _, failure := range failures[1:] {
		output.Diagnostic(s.Streams.Err, asProblem(failure))
	}
	return failures[0]
}

// reportUpdatePlan renders what wso2 module update would do without doing it.
//
// It reads Installer.Check rather than a second planner of its own: Check
// already computes, in one index request, exactly the []Status Update acts
// on, so a preview built from anything else could disagree with the run it
// is previewing.
func (s Shell) reportUpdatePlan(installer install.Installer, namespaces []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), catalogTimeout)
	defer cancel()
	statuses, err := installer.Check(ctx, namespaces...)
	if err != nil {
		return err
	}
	if len(statuses) == 0 {
		_, err := fmt.Fprintln(s.Streams.Out, "No modules are installed.")
		return err
	}
	for _, status := range statuses {
		if _, err := fmt.Fprintln(s.Streams.Out, dryRunUpdateLine(status)); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(s.Streams.Out, "\nNothing was changed. Run without --dry-run to apply this.")
	return err
}

// dryRunUpdateLine renders what Update would do to one module, from the same
// Status it would act on: the four branches mirror updateOne's exactly.
// module_test.go exercises the first three (TestModuleUpdateAllDryRunReports*)
// and module_internal_test.go the fourth (the unpublished branch), so this
// claim is enforced rather than merely asserted in a comment.
func dryRunUpdateLine(status install.Status) string {
	switch {
	case status.Pinned:
		return fmt.Sprintf("%s is pinned to v%s and would not be updated.", status.Namespace, status.PinnedVersion)
	case status.Available == "":
		return fmt.Sprintf("The catalog publishes no version of %s on the %s channel, "+
			"so whether v%s is up to date is unknown. Run wso2 module available to see what it publishes.",
			status.Namespace, status.Channel, status.Installed)
	case status.Update:
		return fmt.Sprintf("%s would be updated from v%s to v%s.",
			status.Namespace, status.Installed, status.Available)
	default:
		return fmt.Sprintf("%s is already current at v%s.", status.Namespace, status.Installed)
	}
}

// updateLine renders one module's outcome, and reports the refusal to exit on.
func updateLine(outcome install.Outcome) (string, error) {
	switch outcome.Action {
	case install.ActionUpdated:
		return fmt.Sprintf("Updated %s from v%s to v%s.", outcome.Namespace, outcome.From, outcome.To), nil
	case install.ActionPinned:
		return fmt.Sprintf("%s is pinned to v%s and was not updated.", outcome.Namespace, outcome.From), nil
	case install.ActionFailed:
		return fmt.Sprintf("%s could not be updated. v%s is still active.",
			outcome.Namespace, outcome.From), outcome.Err
	case install.ActionNotPublished:
		return fmt.Sprintf("The catalog publishes no version of %s on the %s channel, "+
			"so whether v%s is up to date is unknown. Run wso2 module available to see what it publishes.",
			outcome.Namespace, outcome.Channel, outcome.From), nil
	default:
		return fmt.Sprintf("%s is current at v%s.", outcome.Namespace, outcome.From), nil
	}
}

// updateOptions is wso2 module update's parsed arguments.
type updateOptions struct {
	namespaces []string
	yes        bool
	dryRun     bool
	noInput    bool
}

// asProblem renders a refusal that is not the one this run exits on, so a
// second failure is still reported in the shell's own idiom.
func asProblem(err error) problem.Problem {
	var typed problem.Problem
	if errors.As(err, &typed) {
		return typed
	}
	return problem.New(problem.CategoryModuleProcess, "modules.update_failed", err.Error())
}
