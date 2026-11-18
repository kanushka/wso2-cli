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

// Command wso2-module-apim is the WSO2 CLI api product module.
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
	"strconv"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/sdk/cobratree"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/result"
)

// Namespace is the product namespace this module owns. It is the first word of
// every command the module answers.
const Namespace = "apim"

// StatusSchema identifies the semantic shape of this module's status result.
// The shell renders it without interpreting it, so a consumer of JSON output
// can rely on the name to know what the fields mean.
const StatusSchema = "apim.status/v1"

// ManagementAudience is the logical name the API Platform's control plane is
// known by, the same against every deployment. The concrete value a deployment
// binds tokens to is recorded by the operator on the account, and the shell
// proves the token is bound to it before handing anything over.
const ManagementAudience = "api-management"

// GatewayAudience is the logical name of the product's gateway, held as a
// second record on the same product because the gateway validates the same
// login provider's tokens under its own audience.
const GatewayAudience = "api-gateway"

// NextField is the field name the shell renders as a trailing next-step line.
// Every result this module returns ends with it, so a user is never left
// wondering what to run.
const NextField = "next"

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
		fmt.Fprintf(os.Stderr, "wso2-module-apim: %v\n", err)
		os.Exit(1)
	}
}

// moduleOptions describe this module to the SDK.
//
// AuthAudiences names the control-plane and gateway audiences the handlers
// request access for, and module.json declares the same values: the shell
// intersects a runtime request with what the module declared at installation,
// so an audience declared in only one place is refused rather than granted.
// AuthScopes are the WSO2 Cloud permissions the commands ask for, which
// module.json also states as the product's scopes.
func moduleOptions() module.Options {
	return module.Options{
		Namespace:     Namespace,
		Version:       moduleVersion,
		AuthAudiences: []string{ManagementAudience, GatewayAudience, InvocationAudience},
		AuthScopes:    declaredScopes,
	}
}

// commands builds this module's command tree and binds each command to its
// handler.
//
// The tree is an ordinary Cobra tree: commands, flags, and help are declared
// here exactly as they would be in a standalone CLI. What differs is the
// ending, because a handler returns fields instead of printing them.
//
// No command here sets cobra.Command.Args: cobratree.Tree.invoke only calls
// command.ParseFlags, never command.ValidateArgs, so a Cobra positional-arg
// validator would never run and would be dead configuration that looks live.
// Positional-argument counts are enforced in each handler instead, with the
// shared exactlyOneArgument helper in access.go.
func commands() *cobratree.Tree {
	// Every command declares an Example: the one line that runs it, with
	// placeholders for what the reader supplies. The shell shows a leaf's on
	// its page, a group's on the group's, and the root's as the next step
	// once the product is set up, all from the declared tree and with no
	// module launched. A flag a command refuses to run without is marked
	// required, which the shell shows and the SDK enforces.
	root := &cobra.Command{
		Use:     Namespace,
		Short:   "Work with WSO2 API Platform: projects, APIs, and gateways.",
		Example: "  wso2 apim project list",
	}
	statusCommand := &cobra.Command{
		Use:     "status",
		Short:   "Report this module's own status and what to run first.",
		Example: "  wso2 apim status",
	}
	projectsCommand := &cobra.Command{
		Use:   "project",
		Short: "Read, create and delete the projects this organization holds.",
		Example: "  wso2 apim project list\n" +
			"  wso2 apim project create <name>",
	}
	projectsListCommand := &cobra.Command{
		Use:     "list",
		Short:   "List the projects the control plane records.",
		Example: "  wso2 apim project list",
	}
	projectsCommand.AddCommand(projectsListCommand)

	projectsCreateCommand := &cobra.Command{
		Use:     "create <name> [--description <text>]",
		Short:   "Create a project.",
		Example: "  wso2 apim project create <name> --description <text>",
	}
	projectsCreateFlagsValue := &projectsCreateFlags{}
	projectsCreateCommand.Flags().StringVar(&projectsCreateFlagsValue.description, "description", "",
		"A description of the project.")
	projectsCommand.AddCommand(projectsCreateCommand)

	projectsDeleteCommand := &cobra.Command{
		Use:     "delete <project-id> [--yes]",
		Short:   "Delete a project, asking first; wso2 apim project list shows the ids.",
		Example: "  wso2 apim project delete <project-id>",
	}
	projectsDeleteFlags := &removalFlags{}
	projectsDeleteCommand.Flags().BoolVar(&projectsDeleteFlags.yes, "yes", false,
		"Delete without asking for confirmation.")
	projectsCommand.AddCommand(projectsDeleteCommand)

	apisCommand := &cobra.Command{
		Use:   "api",
		Short: "Read and design the APIs a project holds.",
		Example: "  wso2 apim api list --project <id>\n" +
			"  wso2 apim api create -f <file> --project <id>",
	}
	apisListCommand := &cobra.Command{
		Use:     "list [--project <id>]",
		Short:   "List the APIs of one project, or of every project.",
		Example: "  wso2 apim api list --project <id>",
	}
	var project string
	apisListCommand.Flags().StringVar(&project, "project", "",
		"The one project whose APIs to list; every project's by default. wso2 apim project list shows the ids.")
	apisCommand.AddCommand(apisListCommand)

	apisCreateCommand := &cobra.Command{
		Use:     "create -f <file> --project <id>",
		Short:   "Create an API in a project from a gateway RestApi file.",
		Example: "  wso2 apim api create -f <file> --project <id>",
	}
	createFlags := &apisCreateFlags{}
	apisCreateCommand.Flags().StringVarP(&createFlags.file, "file", "f", "",
		"The gateway RestApi document to create the API from.")
	apisCreateCommand.Flags().StringVar(&createFlags.project, "project", "",
		"The project to create the API in; wso2 apim project list shows the ids.")
	markRequired(apisCreateCommand, "file", "project")
	apisCommand.AddCommand(apisCreateCommand)

	apisDeployCommand := &cobra.Command{
		Use:     "deploy <api-id> --gateway-id <gateway-id>",
		Short:   "Deploy an API to a registered gateway; wso2 apim api list shows the ids.",
		Example: "  wso2 apim api deploy <api-id> --gateway-id <gateway-id>",
	}
	deployFlags := &apisDeployFlags{}
	// Named --gateway-id, not --gateway: "wso2 context product add apim
	// --gateway <url>" already gives --gateway one meaning, the gateway's
	// URL, and ADR 0015 keeps one word to one meaning across the surface.
	// This flag names a gateway's handle instead.
	apisDeployCommand.Flags().StringVar(&deployFlags.gatewayID, "gateway-id", "",
		"The handle of the gateway to deploy the API to, as wso2 apim gateway register named it.")
	markRequired(apisDeployCommand, "gateway-id")
	apisCommand.AddCommand(apisDeployCommand)

	apisUndeployCommand := &cobra.Command{
		Use:     "undeploy <api-id> [--gateway-id <gateway-id>]",
		Short:   "Undeploy an API from one gateway, or from every gateway.",
		Example: "  wso2 apim api undeploy <api-id>",
	}
	undeployFlags := &apisUndeployFlags{}
	apisUndeployCommand.Flags().StringVar(&undeployFlags.gatewayID, "gateway-id", "",
		"The handle of the one gateway to undeploy from; every gateway by default.")
	apisCommand.AddCommand(apisUndeployCommand)

	apisDeleteCommand := &cobra.Command{
		Use:     "delete <api-id> [--yes]",
		Short:   "Undeploy an API everywhere and delete it, asking first; wso2 apim api list shows the ids.",
		Example: "  wso2 apim api delete <api-id>",
	}
	apisDeleteFlags := &removalFlags{}
	apisDeleteCommand.Flags().BoolVar(&apisDeleteFlags.yes, "yes", false,
		"Delete without asking for confirmation.")
	apisCommand.AddCommand(apisDeleteCommand)

	apisInvokeCommand := &cobra.Command{
		Use:     "invoke <api-id> [path] [-X <method>] [-H <name: value>]... [-d <body>] [--gateway-id <id>] [--no-token]",
		Short:   "Call a deployed API through its gateway, with a token minted for it.",
		Example: "  wso2 apim api invoke <api-id> /path -X GET",
	}
	invokeFlags := &apisInvokeFlags{}
	apisInvokeCommand.Flags().StringVarP(&invokeFlags.method, "method", "X", "", "The HTTP method; GET by default.")
	apisInvokeCommand.Flags().StringArrayVarP(&invokeFlags.headers, "header", "H", nil,
		"A header to send, as name: value; repeat for more than one.")
	apisInvokeCommand.Flags().StringVarP(&invokeFlags.data, "data", "d", "", "The request body to send.")
	apisInvokeCommand.Flags().StringVar(&invokeFlags.gatewayID, "gateway-id", "",
		"The handle of the gateway to call, when the API is deployed on more than one.")
	apisInvokeCommand.Flags().BoolVar(&invokeFlags.noToken, "no-token", false,
		"Send no token, to see what the gateway answers an anonymous caller.")
	apisCommand.AddCommand(apisInvokeCommand)

	apisTokenCommand := &cobra.Command{
		Use:     "get-token <api-id>",
		Short:   "Mint a short-lived token bound to a deployed API, for calling it by hand.",
		Example: "  wso2 apim api get-token <api-id>",
	}
	tokenFlags := &apisTokenFlags{}
	apisCommand.AddCommand(apisTokenCommand)

	gatewayCommand := &cobra.Command{
		Use:   "gateway",
		Short: "Register a gateway and read what it is actually serving.",
		Example: "  wso2 apim gateway register <handle> --display-name <name> --endpoint <url>\n" +
			"  wso2 apim gateway api list",
	}
	gatewayRegisterCommand := &cobra.Command{
		Use: "register <handle> --display-name <name> --endpoint <url> " +
			"[--endpoint <url> ...] [--type regular|ai|event]",
		Short:   "Register a gateway with the control plane and mint its registration token.",
		Example: "  wso2 apim gateway register <handle> --display-name <name> --endpoint <url>",
	}
	registerFlags := &gatewayRegisterFlags{}
	gatewayRegisterCommand.Flags().StringVar(&registerFlags.displayName, "display-name", "",
		"The gateway's human-readable name.")
	gatewayRegisterCommand.Flags().StringArrayVar(&registerFlags.endpoints, "endpoint", nil,
		"A network endpoint the gateway exposes; repeat for more than one.")
	gatewayRegisterCommand.Flags().StringVar(&registerFlags.gwType, "type", "",
		"The gateway's functionality type: regular, ai, or event. Defaults to regular.")
	markRequired(gatewayRegisterCommand, "display-name", "endpoint")
	gatewayCommand.AddCommand(gatewayRegisterCommand)

	gatewayTokenCommand := &cobra.Command{
		Use:     "token",
		Short:   "Mint a gateway's registration token.",
		Example: "  wso2 apim gateway token create <gateway-id>",
	}
	gatewayTokenCreateCommand := &cobra.Command{
		Use:     "create <gateway-id>",
		Short:   "Mint a new registration token for a gateway that is already registered.",
		Example: "  wso2 apim gateway token create <gateway-id>",
	}
	gatewayTokenCommand.AddCommand(gatewayTokenCreateCommand)
	gatewayCommand.AddCommand(gatewayTokenCommand)

	gatewayApisCommand := &cobra.Command{
		Use:     "api",
		Short:   "Read the APIs deployed on the gateway.",
		Example: "  wso2 apim gateway api list",
	}
	gatewayApisListCommand := &cobra.Command{
		Use:     "list",
		Short:   "List the APIs the gateway is serving.",
		Example: "  wso2 apim gateway api list",
	}
	gatewayApisCommand.AddCommand(gatewayApisListCommand)
	gatewayCommand.AddCommand(gatewayApisCommand)

	root.AddCommand(statusCommand, projectsCommand, apisCommand, gatewayCommand)

	// The JSON members each result carries, declared so that wso2 help
	// --output json can say what a command answers before it is run. A
	// listing's rows are one member, "rows", holding its columns.
	cobratree.ResultFields(statusCommand, "namespace", "version", "endpoint", "reachable", "projects", NextField)
	cobratree.ResultFields(projectsListCommand, "count", "rows", NextField)
	cobratree.ResultFields(projectsCreateCommand, "id", "displayName", "organizationId", NextField)
	cobratree.ResultFields(apisListCommand, "count", "rows", NextField)
	cobratree.ResultFields(apisCreateCommand, "id", "displayName", "version", "context", NextField)
	cobratree.ResultFields(apisDeployCommand, "deploymentId", "apiId", "gatewayId", "status", NextField)
	cobratree.ResultFields(gatewayRegisterCommand, "id", "displayName", "functionalityType", "endpoints", "token", NextField)
	cobratree.ResultFields(gatewayTokenCreateCommand, "gatewayId", "token", NextField)
	cobratree.ResultFields(gatewayApisListCommand, "count", "rows", NextField)

	return cobratree.New(root).
		Handle(statusCommand, status).
		Handle(projectsListCommand, projectsList).
		Handle(projectsCreateCommand, projectsCreate(projectsCreateCommand, projectsCreateFlagsValue)).
		Handle(apisListCommand, apisList(apisListCommand, &project)).
		Handle(apisCreateCommand, apisCreate(apisCreateCommand, createFlags)).
		Handle(apisDeployCommand, apisDeploy(apisDeployCommand, deployFlags)).
		Handle(apisUndeployCommand, apisUndeploy(apisUndeployCommand, undeployFlags)).
		Handle(apisDeleteCommand, apisDelete(apisDeleteCommand, apisDeleteFlags)).
		Handle(projectsDeleteCommand, projectsDelete(projectsDeleteCommand, projectsDeleteFlags)).
		Handle(apisInvokeCommand, apisInvoke(apisInvokeCommand, invokeFlags)).
		Handle(apisTokenCommand, apisToken(apisTokenCommand, tokenFlags)).
		Handle(gatewayRegisterCommand, gatewayRegister(gatewayRegisterCommand, registerFlags)).
		Handle(gatewayTokenCreateCommand, gatewayTokenCreate(gatewayTokenCreateCommand)).
		Handle(gatewayApisListCommand, gatewayApisList)
}

// markRequired marks flags the command refuses to run without. The shell
// shows the mark on the command's help page and the SDK enforces it before the
// handler runs, so the handler's own check is the one a direct caller hits.
func markRequired(command *cobra.Command, names ...string) {
	for _, name := range names {
		if err := command.MarkFlagRequired(name); err != nil {
			// Only a flag the command never declared fails here, which is a
			// programming error in this file and not a runtime condition.
			panic(err)
		}
	}
}

// status answers "wso2 apim status".
//
// It works out where this machine stands with the product and names the one
// command that moves it on: a reachable organization with no project names
// project create, and one with projects names the listing of the first. A
// product that cannot be reached is a failure, with the failure's own
// recovery, so a script can tell a working setup from a broken one by the
// exit code.
func status(ctx context.Context, request module.Request) (result.Result, error) {
	client, err := controlPlane(ctx, request, scopeProjectRead)
	if err != nil {
		return result.Result{}, err
	}
	var listing page[project]
	if err := client.Get(ctx, controlPlanePath+"/projects", &listing); err != nil {
		return result.Result{}, callFailed(err, "read the projects", request.Context.Endpoint)
	}
	report := result.New(StatusSchema).
		With("namespace", "Namespace", Namespace).
		With("version", "Version", moduleVersion).
		With("endpoint", "Endpoint", request.Context.Endpoint).
		With("reachable", "Reachable", "yes").
		With("projects", "Projects", strconv.Itoa(listing.Count))
	if listing.Count == 0 || len(listing.List) == 0 {
		return report.With(NextField, "Next",
			"Run wso2 apim project create <name> to create the first project."), nil
	}
	return report.With(NextField, "Next",
		"Run wso2 apim api list --project "+listing.List[0].ID+" to see what "+
			listing.List[0].DisplayName+" holds."), nil
}
