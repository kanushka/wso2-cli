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

	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/install"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// catalogTimeout bounds one install. It covers the whole operation rather than
// one request, so a stalled origin cannot hold a command open indefinitely.
const catalogTimeout = 10 * time.Minute

// module runs the module lifecycle commands.
func (s Shell) module(args []string) error {
	if len(args) == 0 {
		return problem.New(problem.CategoryUsage, "shell.missing_argument",
			"wso2 module needs a subcommand").
			WithRecovery(moduleRecovery)
	}
	switch args[0] {
	case "install":
		return s.moduleInstall(args[1:])
	case "available":
		return s.moduleAvailable(args[1:])
	case "list":
		return s.moduleList(args[1:])
	case "update":
		return s.moduleUpdate(args[1:])
	case "remove":
		return s.moduleRemove(args[1:])
	default:
		return problem.New(problem.CategoryUsage, "shell.unknown_command",
			fmt.Sprintf("%q is not a wso2 module subcommand", args[0])).
			WithRecovery(moduleRecovery)
	}
}

const moduleRecovery = "Run wso2 module available to see what can be installed, " +
	"wso2 module install <module> to install one, wso2 module update --all to update what is " +
	"installed, or wso2 module remove <module> to take one off this machine."

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
func (s Shell) moduleRemove(args []string) error {
	if len(args) == 0 {
		return problem.New(problem.CategoryUsage, "shell.missing_argument",
			"wso2 module remove needs the module to remove").
			WithRecovery("Run wso2 module list to see what is installed, " +
				"then wso2 module remove <module>.")
	}
	if len(args) > 1 {
		return problem.New(problem.CategoryUsage, "shell.unexpected_argument",
			fmt.Sprintf("wso2 module remove takes one module, got %q as well", args[1])).
			WithRecovery("Remove one module at a time: wso2 module remove <module>.")
	}

	namespace := args[0]
	store, err := s.store()
	if err != nil {
		return err
	}
	removed, err := store.Remove(namespace)
	if err != nil {
		return err
	}
	if !removed {
		// Reporting success here would tell a user their module is gone when
		// what is installed is something else under the name they meant.
		return problem.New(problem.CategoryUsage, "shell.module_not_installed",
			fmt.Sprintf("no %s module is installed", namespace)).
			WithRecovery("Run wso2 module list to see what is installed.")
	}

	_, err = fmt.Fprintf(s.Streams.Out, "Removed the %s module.\n", namespace)
	return err
}

// moduleInstall installs one product module from the catalog.
//
// The module may be named as "<module>@<version>" to install an exact version,
// which is what a pipeline pins so its behaviour does not depend on what is
// newest that day. Without a pin, the newest version on the chosen channel that
// this shell can launch on this platform is installed.
func (s Shell) moduleInstall(args []string) error {
	namespace, policy, err := parseInstallArguments(args)
	if err != nil {
		return err
	}

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

// parseInstallArguments reads the module to install and the policy to select
// its version under.
func parseInstallArguments(args []string) (string, catalog.Policy, error) {
	var namespace string
	var policy catalog.Policy
	for index := 0; index < len(args); index++ {
		argument := args[index]
		switch {
		case argument == "--channel":
			if index+1 >= len(args) {
				return "", catalog.Policy{}, problem.New(problem.CategoryUsage, "shell.missing_argument",
					"--channel needs a channel name").
					WithRecovery("Run wso2 module install <module> --channel stable.")
			}
			index++
			policy.Channel = args[index]
		case strings.HasPrefix(argument, "-"):
			return "", catalog.Policy{}, problem.New(problem.CategoryUsage, "shell.unknown_flag",
				fmt.Sprintf("%q is not a wso2 module install flag", argument)).
				WithRecovery("Run wso2 module install <module> [--channel <channel>].")
		case namespace != "":
			return "", catalog.Policy{}, problem.New(problem.CategoryUsage, "shell.unexpected_argument",
				fmt.Sprintf("wso2 module install takes one module, got %q as well", argument)).
				WithRecovery("Run wso2 module install <module>.")
		default:
			namespace, policy.Version, _ = strings.Cut(argument, "@")
		}
	}
	if namespace == "" {
		return "", catalog.Policy{}, problem.New(problem.CategoryUsage, "shell.missing_argument",
			"wso2 module install needs a module to install").
			WithRecovery("Run wso2 module install <module>.")
	}
	if policy.Channel != "" && policy.Version != "" {
		return "", catalog.Policy{}, problem.New(problem.CategoryUsage, "shell.conflicting_arguments",
			"a pinned version and a channel cannot both be given").
			WithRecovery("Pin a version, or choose a channel, but not both.")
	}
	return namespace, policy, nil
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
	return install.Installer{
		Store:  store,
		Client: catalog.Client{Origin: catalog.Origin(), HTTP: &http.Client{}},
		Shell:  identity,
	}, nil
}

// moduleAvailable lists the product modules the catalog publishes, so what can
// be installed is discoverable from the shell rather than from documentation.
//
// It costs one request: the index carries the latest version on each channel
// for every namespace, and nothing here selects a specific version.
func (s Shell) moduleAvailable(args []string) error {
	if len(args) > 0 {
		return problem.New(problem.CategoryUsage, "shell.unexpected_argument",
			fmt.Sprintf("wso2 module available takes no arguments, got %q", args[0])).
			WithRecovery("Run wso2 module available.")
	}
	installer, err := s.installer()
	if err != nil {
		return err
	}
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
func (s Shell) moduleList(args []string) error {
	if len(args) > 0 {
		return problem.New(problem.CategoryUsage, "shell.unexpected_argument",
			fmt.Sprintf("wso2 module list takes no arguments, got %q", args[0])).
			WithRecovery("Run wso2 module list.")
	}
	installer, err := s.installer()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), catalogTimeout)
	defer cancel()
	statuses, err := installer.Check(ctx)
	if err != nil {
		return err
	}

	if len(statuses) == 0 {
		_, err := fmt.Fprintln(s.Streams.Out, "No modules are installed.")
		return err
	}
	updates := 0
	table := output.NewTable("module", "installed", "channel", "update")
	for _, status := range statuses {
		table.Append(status.Namespace, "v"+status.Installed, status.Channel, updateColumn(status))
		if status.Update {
			updates++
		}
	}
	if err := table.Render(s.Streams.Out); err != nil {
		return err
	}
	if updates == 0 {
		_, err := fmt.Fprintln(s.Streams.Out, "\nEvery installed module is current.")
		return err
	}
	_, err = fmt.Fprintf(s.Streams.Out,
		"\n%d module(s) have an update available. Run wso2 module update --all to take them.\n", updates)
	return err
}

// updateColumn says what is available for one module in the terms that decide
// it: an update to take, a pin holding it where it is, or a channel the catalog
// publishes nothing on.
func updateColumn(status install.Status) string {
	switch {
	case status.Pinned:
		return "pinned to v" + status.PinnedVersion
	case status.Update:
		return "v" + status.Available + " available"
	case status.Available == "":
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
func (s Shell) moduleUpdate(args []string) error {
	namespaces, err := parseUpdateArguments(args)
	if err != nil {
		return err
	}
	installer, err := s.installer()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), catalogTimeout)
	defer cancel()
	outcomes, err := installer.Update(ctx, namespaces)
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
	default:
		return fmt.Sprintf("%s is current at v%s.", outcome.Namespace, outcome.From), nil
	}
}

// parseUpdateArguments reads the modules an update run covers. Updating
// everything is asked for explicitly, so a mistyped module name cannot become a
// run over every installed module.
func parseUpdateArguments(args []string) ([]string, error) {
	all := false
	var namespaces []string
	for _, argument := range args {
		switch {
		case argument == "--all":
			all = true
		case strings.HasPrefix(argument, "-"):
			return nil, problem.New(problem.CategoryUsage, "shell.unknown_flag",
				fmt.Sprintf("%q is not a wso2 module update flag", argument)).
				WithRecovery("Run wso2 module update <module>, or wso2 module update --all.")
		default:
			namespaces = append(namespaces, argument)
		}
	}
	if all && len(namespaces) > 0 {
		return nil, problem.New(problem.CategoryUsage, "shell.conflicting_arguments",
			"--all updates every installed module, so naming one as well is ambiguous").
			WithRecovery("Run wso2 module update <module>, or wso2 module update --all.")
	}
	if !all && len(namespaces) == 0 {
		return nil, problem.New(problem.CategoryUsage, "shell.missing_argument",
			"wso2 module update needs a module, or --all").
			WithRecovery("Run wso2 module update <module>, or wso2 module update --all.")
	}
	return namespaces, nil
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
