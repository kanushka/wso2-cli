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
// AuthAudiences and AuthScopes are empty because this module asks the shell for
// nothing yet. Declare an audience and a scope here, and the same values in
// module.json, before a handler requests access: the shell intersects a runtime
// request with what the module declared at installation, so an undeclared
// audience is refused rather than granted. modules/reference is the worked example.
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
func commands() *cobratree.Tree {
	root := &cobra.Command{
		Use:   Namespace,
		Short: "Api commands for the WSO2 CLI.",
	}
	statusCommand := &cobra.Command{
		Use:   "status",
		Short: "Report this module's own status and what to run first.",
	}
	projectsCommand := &cobra.Command{
		Use:   "projects",
		Short: "Read the projects this organization holds.",
	}
	projectsListCommand := &cobra.Command{
		Use:   "list",
		Short: "List the projects the control plane records.",
	}
	projectsCommand.AddCommand(projectsListCommand)

	apisCommand := &cobra.Command{
		Use:   "apis",
		Short: "Read the APIs a project designs.",
	}
	apisListCommand := &cobra.Command{
		Use:   "list --project <id>",
		Short: "List the APIs a project holds.",
	}
	var project string
	apisListCommand.Flags().StringVar(&project, "project", "",
		"The project whose APIs to list; wso2 api projects list shows the ids.")
	apisCommand.AddCommand(apisListCommand)

	gatewayCommand := &cobra.Command{
		Use:   "gateway",
		Short: "Read what a gateway is actually serving.",
	}
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
		Handle(apisListCommand, apisList(apisListCommand, &project)).
		Handle(gatewayApisListCommand, gatewayApisList)
}

// status answers "wso2 api status".
//
// It reports what it can know without asking anything of the shell, so a freshly
// generated module answers before it has been given an identity to act as. Call
// your product from here: the invocation carries the selected context, and
// request.Access.Acquire is how a handler obtains short-lived access to it.
func status(ctx context.Context, request module.Request) (result.Result, error) {
	next := "Record where this product runs on the identity you log in with: " +
		"wso2 account add-product <account> api --endpoint <url>, or " +
		"wso2 api connect <url> once module.json declares a product descriptor."
	if request.Context.Endpoint != "" {
		next = "Run wso2 api --help to see what this module can do at " + request.Context.Endpoint + "."
	}
	return result.New(StatusSchema).
		With("namespace", "Namespace", Namespace).
		With("version", "Version", moduleVersion).
		With("endpoint", "Endpoint", request.Context.Endpoint).
		With(NextField, "Next", next), nil
}
