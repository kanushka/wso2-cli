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

// Command wso2-module-intg is the WSO2 CLI product module for the WSO2 Cloud
// Integration Platform.
//
// It is built against the public SDK alone and imports no shell package, which
// is what lets it be released, installed, and updated on its own schedule.
//
// The shell owns rendering. A handler returns semantic fields in presentation
// order and never prints: the same handler answers a table run and a JSON run,
// and the field order here is the order both follow. See
// docs/adr/0003-shell-owned-output.md.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/sdk/cobratree"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/result"
)

// Namespace is the product namespace this module owns. It is the first word of
// every command the module answers.
const Namespace = "intg"

// StatusSchema identifies the semantic shape of this module's status result.
// The shell renders it without interpreting it, so a consumer of JSON output
// can rely on the name to know what the fields mean.
const StatusSchema = "intg.status/v1"

// NextField is the field name the shell renders as a trailing next-step line.
// Every result this module returns ends with it, so a user is never left
// wondering what to run.
const NextField = "next"

// IntegrationAudience is the logical name of the Integration Platform API this
// module asks the shell for access to. It is the same against every
// deployment: a context maps it to whatever the deployment binds tokens to,
// which on WSO2 Cloud is the API's own URL, registered as a ThunderID resource
// server.
const IntegrationAudience = "integration-management"

// moduleVersion is this module's own release version. A release injects it:
//
//	go build -ldflags "-X main.moduleVersion=0.1.0"
//
// It moves independently of the shell, protocol, and SDK versions.
var moduleVersion = "0.0.0-dev"

func main() {
	// Standard output carries protocol frames only. Anything this process wants
	// to say goes to standard error, where the shell captures it as bounded
	// diagnostics. See docs/adr/0002-module-transport.md.
	//
	// The tree is served, not its commands: Serve declares the tree to the
	// shell as well, which is what lets the shell answer --help, name a
	// mistyped command, and parse this module's flags before it is launched.
	if err := commands().Serve(context.Background(), moduleOptions()); err != nil {
		fmt.Fprintf(os.Stderr, "wso2-module-intg: %v\n", err)
		os.Exit(1)
	}
}

// moduleOptions describe this module to the SDK.
//
// The audience here and in module.json must stay equal: the shell intersects a
// runtime request with what the module declared at installation. No scopes are
// declared, because the product is reached by token exchange and authorized by
// the organization claims of the token rather than by scopes.
func moduleOptions() module.Options {
	return module.Options{
		Namespace:     Namespace,
		Version:       moduleVersion,
		AuthAudiences: []string{IntegrationAudience},
	}
}

// commands builds this module's command tree and binds each command to its
// handler.
//
// The tree is an ordinary Cobra tree: commands, flags, and help are declared
// here exactly as they would be in a standalone CLI. What differs is the
// ending, because a handler returns fields instead of printing them.
func commands() *cobratree.Tree {
	root := newCommand(Namespace, "Manage integrations on the WSO2 Cloud Integration Platform.",
		"wso2 intg integration list")
	statusCommand := newCommand("status", "Report this module's own status and what to run first.",
		"wso2 intg status")
	cobratree.ResultFields(statusCommand, "namespace", "version", "endpoint", "project", NextField)
	root.AddCommand(statusCommand)

	// Positional arguments are counted in the handler, never through
	// cobra.Command.Args: the SDK parses flags but does not run Args
	// validators, so one set here would never be consulted.
	projectCommand := newCommand("project", "Work with the organization's projects.",
		"wso2 intg project list",
		"wso2 intg project create <name>")
	projectListCommand := newCommand("list", "List the projects in the organization.",
		"wso2 intg project list")
	cobratree.ResultFields(projectListCommand, "count", "rows", NextField)
	projectShowCommand := newCommand("show <project>", "Show one project.",
		"wso2 intg project show <project>")
	cobratree.ResultFields(projectShowCommand, projectFields...)
	projectCreateCommand := newCommand("create <name>", "Create a project, optionally linked to a repository.",
		"wso2 intg project create <name> --repo <url> --branch <branch>")
	cobratree.ResultFields(projectCreateCommand, projectFields...)
	createFlags := &projectCreateFlags{}
	createFlags.bind(projectCreateCommand)
	projectDeleteCommand := newCommand("delete <project>", "Delete a project and everything in it.",
		"wso2 intg project delete <project>")
	cobratree.ResultFields(projectDeleteCommand, "project", "state", NextField)
	deleteFlags := &removalFlags{}
	deleteFlags.bind(projectDeleteCommand)
	projectCommand.AddCommand(projectListCommand, projectShowCommand, projectCreateCommand, projectDeleteCommand)
	root.AddCommand(projectCommand)

	integrationCommand := newCommand("integration", "Work with the integrations in a project.",
		"wso2 intg integration list --project <project>",
		"wso2 intg integration deploy <integration> --env <environment>")
	integrationListCommand := newCommand("list", "List the integrations of a project, or of every project.",
		"wso2 intg integration list --project <project>")
	cobratree.ResultFields(integrationListCommand, "count", "rows", NextField)
	listFlags := &projectFlag{}
	listFlags.bind(integrationListCommand, "The project to list. Defaults to the context's project, or every project.")
	integrationShowCommand := newCommand("show <integration>", "Show one integration and its source repository.",
		"wso2 intg integration show <integration>")
	cobratree.ResultFields(integrationShowCommand, "id", "name", "project", "type", "runtime", "description",
		"status", "repository", "branch", "path", "updated", "created", "state", NextField)
	showFlags := &projectFlag{}
	showFlags.bind(integrationShowCommand, "The project to look in. Defaults to the context's project, or every project.")
	integrationCreateCommand := newCommand("create <name>",
		"Create an integration that builds from a public git repository.",
		"wso2 intg integration create <name> --project <project> --type <type> --runtime <runtime> "+
			"--repo <url> --branch <branch>")
	cobratree.ResultFields(integrationCreateCommand, "id", "name", "project", "type", "runtime", "warning", NextField)
	integrationCreateFlagsValue := &integrationCreateFlags{}
	integrationCreateFlagsValue.bind(integrationCreateCommand)
	markRequired(integrationCreateCommand, "type", "runtime", "repo", "branch")
	integrationDeleteCommand := newCommand("delete <integration>", "Delete an integration.",
		"wso2 intg integration delete <integration>")
	cobratree.ResultFields(integrationDeleteCommand, "integration", "state", NextField)
	integrationDeleteFlagsValue := &integrationDeleteFlags{}
	integrationDeleteFlagsValue.projectFlag.bind(integrationDeleteCommand,
		"The project the integration is in. Defaults to the context's project, or the one the platform records.")
	integrationDeleteFlagsValue.removalFlags.bind(integrationDeleteCommand)
	integrationCommand.AddCommand(integrationListCommand, integrationShowCommand, integrationCreateCommand,
		integrationDeleteCommand)
	root.AddCommand(integrationCommand)

	buildCommand := newCommand("build", "Start builds of an integration and read their status and logs.",
		"wso2 intg integration build run <integration> --wait",
		"wso2 intg integration build logs <integration> <build>")
	buildRunCommand := newCommand("run <integration>", "Start a build of an integration.",
		"wso2 intg integration build run <integration> --wait")
	cobratree.ResultFields(buildRunCommand, "integration", "commit", "message", NextField)
	buildRunFlagsValue := &buildRunFlags{}
	buildRunCommand.Flags().StringVar(&buildRunFlagsValue.commit, "commit", "",
		"The full 40-character commit SHA to build. Defaults to the head of the integration's branch.")
	buildRunFlagsValue.bind(buildRunCommand, "the build finishes")
	cobratree.WithTimeout(buildRunCommand, waitDeadline)
	buildListCommand := newCommand("list <integration>", "List an integration's recent builds, newest first.",
		"wso2 intg integration build list <integration>")
	cobratree.ResultFields(buildListCommand, "integration", "count", "rows", NextField)
	buildShowCommand := newCommand("show <integration> <build>", "Show one build's status, commit, image and steps.",
		"wso2 intg integration build show <integration> <build>")
	cobratree.ResultFields(buildShowCommand, buildFields...)
	buildLogsCommand := newCommand("logs <integration> <build>", "Show one build's log.",
		"wso2 intg integration build logs <integration> <build> --tail 200")
	cobratree.ResultFields(buildLogsCommand, "build", "integration", "lines", "log", NextField)
	buildLogsFlagsValue := &buildLogsFlags{}
	buildLogsCommand.Flags().IntVar(&buildLogsFlagsValue.tail, "tail", 0,
		"Show only the last this many lines. Defaults to the whole log.")
	buildCommand.AddCommand(buildRunCommand, buildListCommand, buildShowCommand, buildLogsCommand)
	integrationCommand.AddCommand(buildCommand)

	integrationDeployCommand := newCommand("deploy <integration>",
		"Deploy an integration where its project's pipeline starts.",
		"wso2 intg integration deploy <integration> --env <environment>")
	cobratree.ResultFields(integrationDeployCommand, "integration", "environment", "build", "release", "status",
		NextField)
	deployFlags := &integrationDeployFlags{}
	integrationDeployCommand.Flags().StringVar(&deployFlags.env, "env", "",
		"The environment to deploy to; wso2 intg environment list shows the ids.")
	integrationDeployCommand.Flags().StringVar(&deployFlags.build, "build", "",
		"A succeeded build whose image to deploy; wso2 intg integration build list shows the names. "+
			"Defaults to the integration's current image.")
	integrationDeployCommand.Flags().StringVar(&deployFlags.project, "project", "",
		"The project the integration is in. Defaults to the context's project, or the one the integration listing reports.")
	markRequired(integrationDeployCommand, "env")
	deployFlags.bind(integrationDeployCommand, "the new release is running")
	cobratree.WithTimeout(integrationDeployCommand, waitDeadline)
	integrationPromoteCommand := newCommand("promote <integration>",
		"Promote what runs in one environment to the next, along the project's pipeline.",
		"wso2 intg integration promote <integration> --from <environment> --to <environment>")
	cobratree.ResultFields(integrationPromoteCommand, "integration", "from", "to", "release", "status", NextField)
	promoteFlags := &integrationPromoteFlags{}
	integrationPromoteCommand.Flags().StringVar(&promoteFlags.from, "from", "",
		"The environment whose running release to promote; wso2 intg environment list shows the ids.")
	integrationPromoteCommand.Flags().StringVar(&promoteFlags.to, "to", "",
		"The environment to promote into; wso2 intg environment list shows the ids.")
	integrationPromoteCommand.Flags().StringVar(&promoteFlags.project, "project", "",
		"The project the integration is in. Defaults to the context's project, or the one the integration listing reports.")
	markRequired(integrationPromoteCommand, "from", "to")
	promoteFlags.bind(integrationPromoteCommand, "the promoted release is running")
	cobratree.WithTimeout(integrationPromoteCommand, waitDeadline)
	integrationStopCommand := newCommand("stop <integration>",
		"Stop an integration in one environment, keeping its deployment.",
		"wso2 intg integration stop <integration> --env <environment>")
	cobratree.ResultFields(integrationStopCommand, "integration", "environment", "state", NextField)
	stopFlags := &environmentFlag{}
	integrationStopCommand.Flags().StringVar(&stopFlags.env, "env", "",
		"The environment to stop it in; wso2 intg environment list shows the ids.")
	markRequired(integrationStopCommand, "env")
	integrationRedeployCommand := newCommand("redeploy <integration>",
		"Restart what an integration runs in one environment, or start it again after a stop.",
		"wso2 intg integration redeploy <integration> --env <environment>")
	cobratree.ResultFields(integrationRedeployCommand, "integration", "environment", "release", "status", "message",
		NextField)
	redeployFlags := &integrationRedeployFlags{}
	integrationRedeployCommand.Flags().StringVar(&redeployFlags.env, "env", "",
		"The environment to redeploy it in; wso2 intg environment list shows the ids.")
	markRequired(integrationRedeployCommand, "env")
	redeployFlags.bind(integrationRedeployCommand, "it is running again")
	cobratree.WithTimeout(integrationRedeployCommand, waitDeadline)
	integrationLogsCommand := newCommand("logs <integration>", "Show what an integration logs in one environment.",
		"wso2 intg integration logs <integration> --env <environment> --since 15m")
	cobratree.ResultFields(integrationLogsCommand, "integration", "environment", "since", "lines", "log", NextField)
	integrationLogsFlagsValue := &integrationLogsFlags{}
	integrationLogsCommand.Flags().StringVar(&integrationLogsFlagsValue.env, "env", "",
		"The environment to read its logs in; wso2 intg environment list shows the ids.")
	integrationLogsCommand.Flags().StringVar(&integrationLogsFlagsValue.since, "since", "1h",
		"How far back to look, such as 15m or 24h.")
	integrationLogsCommand.Flags().IntVar(&integrationLogsFlagsValue.tail, "tail", defaultLogTail,
		"Show only the newest this many lines; 0 shows every line in the window, up to 1000.")
	integrationLogsCommand.Flags().StringVar(&integrationLogsFlagsValue.project, "project", "",
		"The project the integration is in. Defaults to the context's project, or the one the integration listing reports.")
	markRequired(integrationLogsCommand, "env")
	integrationCommand.AddCommand(integrationDeployCommand, integrationPromoteCommand, integrationStopCommand,
		integrationRedeployCommand, integrationLogsCommand)

	environmentCommand := newCommand("environment", "See the organization's deployment environments.",
		"wso2 intg environment list")
	environmentListCommand := newCommand("list", "List the organization's environments.",
		"wso2 intg environment list")
	cobratree.ResultFields(environmentListCommand, "count", "rows", NextField)
	environmentCommand.AddCommand(environmentListCommand)
	root.AddCommand(environmentCommand)

	deploymentCommand := newCommand("deployment", "See what an integration runs in each environment.",
		"wso2 intg integration deployment list <integration>",
		"wso2 intg integration deployment show <integration> --env <environment>")
	deploymentListCommand := newCommand("list <integration>", "List what an integration runs in every environment.",
		"wso2 intg integration deployment list <integration>")
	cobratree.ResultFields(deploymentListCommand, "integration", "rows", NextField)
	deploymentShowCommand := newCommand("show <integration>", "Show what an integration runs in one environment.",
		"wso2 intg integration deployment show <integration> --env <environment>")
	cobratree.ResultFields(deploymentShowCommand, "integration", "environment", "status", "release", "build",
		"commit", "deployed", "schedule", NextField)
	showDeploymentFlags := &environmentFlag{}
	deploymentShowCommand.Flags().StringVar(&showDeploymentFlags.env, "env", "",
		"The environment to show; wso2 intg environment list shows the ids.")
	markRequired(deploymentShowCommand, "env")
	deploymentCommand.AddCommand(deploymentListCommand, deploymentShowCommand)
	integrationCommand.AddCommand(deploymentCommand)

	return cobratree.New(root).
		Handle(statusCommand, status).
		Handle(projectListCommand, projectList(projectListCommand)).
		Handle(projectShowCommand, projectShow(projectShowCommand)).
		Handle(projectCreateCommand, projectCreate(projectCreateCommand, createFlags)).
		Handle(projectDeleteCommand, projectDelete(projectDeleteCommand, deleteFlags)).
		Handle(integrationListCommand, integrationList(integrationListCommand, listFlags)).
		Handle(integrationShowCommand, integrationShow(integrationShowCommand, showFlags)).
		Handle(integrationCreateCommand, integrationCreate(integrationCreateCommand, integrationCreateFlagsValue)).
		Handle(integrationDeleteCommand, integrationDelete(integrationDeleteCommand, integrationDeleteFlagsValue)).
		Handle(buildRunCommand, buildRun(buildRunCommand, buildRunFlagsValue)).
		Handle(buildListCommand, buildList(buildListCommand)).
		Handle(buildShowCommand, buildShow(buildShowCommand)).
		Handle(buildLogsCommand, buildLogs(buildLogsCommand, buildLogsFlagsValue)).
		Handle(integrationDeployCommand, integrationDeploy(integrationDeployCommand, deployFlags)).
		Handle(integrationPromoteCommand, integrationPromote(integrationPromoteCommand, promoteFlags)).
		Handle(integrationStopCommand, integrationStop(integrationStopCommand, stopFlags)).
		Handle(integrationRedeployCommand, integrationRedeploy(integrationRedeployCommand, redeployFlags)).
		Handle(environmentListCommand, environmentList(environmentListCommand)).
		Handle(deploymentListCommand, deploymentList(deploymentListCommand)).
		Handle(deploymentShowCommand, deploymentShow(deploymentShowCommand, showDeploymentFlags)).
		Handle(integrationLogsCommand, integrationLogs(integrationLogsCommand, integrationLogsFlagsValue))
}

// projectFields and buildFields are the members of the results more than one
// command returns.
var (
	projectFields = []string{"id", "name", "description", "pipeline", "repository", "branch", "path", "created",
		"state", NextField}
	buildFields = []string{"name", "integration", "project", "status", "trigger", "commit", "image", "started",
		"completed", "steps", NextField}
)

// newCommand declares one command with its help examples, one runnable line
// each, which the shell shows on the command's help page and names in the
// recovery when a required flag is missing.
func newCommand(use, short string, examples ...string) *cobra.Command {
	return &cobra.Command{Use: use, Short: short, Example: strings.Join(examples, "\n")}
}

// markRequired marks flags the command cannot run without, so help marks them
// and the SDK refuses a command line that leaves one out before the handler
// runs. The handler still refuses an empty value, which Cobra counts as given.
func markRequired(command *cobra.Command, names ...string) {
	for _, name := range names {
		if err := command.MarkFlagRequired(name); err != nil {
			panic("intg: marking an undeclared flag required: " + name)
		}
	}
}

// status answers "wso2 intg status".
//
// It reports what it can know without asking anything of the shell, so it
// answers before a context records the product or a login exists.
func status(ctx context.Context, request module.Request) (result.Result, error) {
	next := "Record where this product runs on the selected context: " +
		"wso2 context product add intg --url <url>."
	if request.Context.Endpoint != "" {
		next = "Run wso2 intg project list to see the organization's projects at " +
			request.Context.Endpoint + "."
	}
	return result.New(StatusSchema).
		With("namespace", "Namespace", Namespace).
		With("version", "Version", moduleVersion).
		With("endpoint", "Endpoint", request.Context.Endpoint).
		With("project", "Project", orDash(request.Context.Project)).
		With(NextField, "Next", next), nil
}
