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

// Command wso2-module-api is the WSO2 CLI api product module.
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

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/sdk/cobratree"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/result"
)

// Namespace is the product namespace this module owns. It is the first word of
// every command the module answers.
const Namespace = "api"

// StatusSchema identifies the semantic shape of this module's status result.
// The shell renders it without interpreting it, so a consumer of JSON output
// can rely on the name to know what the fields mean.
const StatusSchema = "api.status/v1"

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
		fmt.Fprintf(os.Stderr, "wso2-module-api: %v\n", err)
		os.Exit(1)
	}
}

// moduleOptions describe this module to the SDK.
//
// AuthAudiences names the control-plane and gateway audiences the handlers
// request access for, and module.json declares the same values: the shell
// intersects a runtime request with what the module declared at installation,
// so an audience declared in only one place is refused rather than granted.
// No scope is declared, because every request names an audience alone.
func moduleOptions() module.Options {
	return module.Options{
		Namespace:     Namespace,
		Version:       moduleVersion,
		AuthAudiences: []string{ManagementAudience, GatewayAudience},
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
	root := &cobra.Command{
		Use:   Namespace,
		Short: "Work with WSO2 API Platform: projects, APIs, and gateways.",
	}
	statusCommand := &cobra.Command{
		Use:   "status",
		Short: "Report this module's own status and what to run first.",
	}
	projectsCommand := &cobra.Command{
		Use:   "projects",
		Short: "Read and create the projects this organization holds.",
	}
	projectsListCommand := &cobra.Command{
		Use:   "list",
		Short: "List the projects the control plane records.",
	}
	projectsCommand.AddCommand(projectsListCommand)

	projectsCreateCommand := &cobra.Command{
		Use:   "create <name> [--description <text>]",
		Short: "Create a project.",
	}
	projectsCreateFlagsValue := &projectsCreateFlags{}
	projectsCreateCommand.Flags().StringVar(&projectsCreateFlagsValue.description, "description", "",
		"A description of the project.")
	projectsCommand.AddCommand(projectsCreateCommand)

	apisCommand := &cobra.Command{
		Use:   "apis",
		Short: "Read and design the APIs a project holds.",
	}
	apisListCommand := &cobra.Command{
		Use:   "list --project <id>",
		Short: "List the APIs a project holds.",
	}
	var project string
	apisListCommand.Flags().StringVar(&project, "project", "",
		"The project whose APIs to list; wso2 api projects list shows the ids.")
	apisCommand.AddCommand(apisListCommand)

	apisCreateCommand := &cobra.Command{
		Use:   "create -f <file> --project <id>",
		Short: "Create an API in a project from a gateway RestApi file.",
	}
	createFlags := &apisCreateFlags{}
	apisCreateCommand.Flags().StringVarP(&createFlags.file, "file", "f", "",
		"The gateway RestApi document to create the API from.")
	apisCreateCommand.Flags().StringVar(&createFlags.project, "project", "",
		"The project to create the API in; wso2 api projects list shows the ids.")
	apisCommand.AddCommand(apisCreateCommand)

	apisDeployCommand := &cobra.Command{
		Use:   "deploy <api-id> --gateway-id <gateway-id>",
		Short: "Deploy an API to a registered gateway.",
	}
	deployFlags := &apisDeployFlags{}
	// Named --gateway-id, not --gateway: "wso2 context product add api
	// --gateway <url>" already gives --gateway one meaning, the gateway's
	// URL, and ADR 0015 keeps one word to one meaning across the surface.
	// This flag names a gateway's handle instead.
	apisDeployCommand.Flags().StringVar(&deployFlags.gatewayID, "gateway-id", "",
		"The handle of the gateway to deploy the API to.")
	apisCommand.AddCommand(apisDeployCommand)

	gatewayCommand := &cobra.Command{
		Use:   "gateway",
		Short: "Register a gateway and read what it is actually serving.",
	}
	gatewayRegisterCommand := &cobra.Command{
		Use: "register <handle> --display-name <name> --endpoint <url> " +
			"[--endpoint <url> ...] [--type regular|ai|event]",
		Short: "Register a gateway with the control plane and mint its registration token.",
	}
	registerFlags := &gatewayRegisterFlags{}
	gatewayRegisterCommand.Flags().StringVar(&registerFlags.displayName, "display-name", "",
		"The gateway's human-readable name.")
	gatewayRegisterCommand.Flags().StringArrayVar(&registerFlags.endpoints, "endpoint", nil,
		"A network endpoint the gateway exposes; repeat for more than one.")
	gatewayRegisterCommand.Flags().StringVar(&registerFlags.gwType, "type", "",
		"The gateway's functionality type: regular, ai, or event. Defaults to regular.")
	gatewayCommand.AddCommand(gatewayRegisterCommand)

	gatewayTokenCommand := &cobra.Command{
		Use:   "token",
		Short: "Mint a gateway's registration token.",
	}
	gatewayTokenCreateCommand := &cobra.Command{
		Use:   "create <gateway-id>",
		Short: "Mint a new registration token for a gateway that is already registered.",
	}
	gatewayTokenCommand.AddCommand(gatewayTokenCreateCommand)
	gatewayCommand.AddCommand(gatewayTokenCommand)

	gatewayApisCommand := &cobra.Command{
		Use:   "apis",
		Short: "Read the APIs deployed on the gateway.",
	}
	gatewayApisListCommand := &cobra.Command{
		Use:   "list",
		Short: "List the APIs the gateway is serving.",
	}
	gatewayApisCommand.AddCommand(gatewayApisListCommand)
	gatewayCommand.AddCommand(gatewayApisCommand)

	root.AddCommand(statusCommand, projectsCommand, apisCommand, gatewayCommand)

	return cobratree.New(root).
		Handle(statusCommand, status).
		Handle(projectsListCommand, projectsList).
		Handle(projectsCreateCommand, projectsCreate(projectsCreateCommand, projectsCreateFlagsValue)).
		Handle(apisListCommand, apisList(apisListCommand, &project)).
		Handle(apisCreateCommand, apisCreate(apisCreateCommand, createFlags)).
		Handle(apisDeployCommand, apisDeploy(apisDeployCommand, deployFlags)).
		Handle(gatewayRegisterCommand, gatewayRegister(gatewayRegisterCommand, registerFlags)).
		Handle(gatewayTokenCreateCommand, gatewayTokenCreate(gatewayTokenCreateCommand)).
		Handle(gatewayApisListCommand, gatewayApisList)
}

// status answers "wso2 api status".
//
// It reports what it can know without asking anything of the shell, so a freshly
// generated module answers before it has been given an identity to act as. Call
// your product from here: the invocation carries the selected context, and
// request.Access.Acquire is how a handler obtains short-lived access to it.
func status(ctx context.Context, request module.Request) (result.Result, error) {
	next := "Record where this product runs on the selected context: wso2 context product add api --url <url>."
	if request.Context.Endpoint != "" {
		next = "Run wso2 api --help to see what this module can do at " + request.Context.Endpoint + "."
	}
	return result.New(StatusSchema).
		With("namespace", "Namespace", Namespace).
		With("version", "Version", moduleVersion).
		With("endpoint", "Endpoint", request.Context.Endpoint).
		With(NextField, "Next", next), nil
}
