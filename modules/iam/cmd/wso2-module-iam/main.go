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

// Command wso2-module-iam is the WSO2 CLI iam product module.
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
const Namespace = "iam"

// StatusSchema identifies the semantic shape of this module's status result.
// The shell renders it without interpreting it, so a consumer of JSON output
// can rely on the name to know what the fields mean.
const StatusSchema = "iam.status/v1"

// ManagementAudience is the logical name this module's API is known by, the
// same against every deployment it will ever run against. The concrete value a
// deployment stamps into aud — a resource-server URI on ThunderID, an API
// resource identifier on Identity Server — is recorded by the operator on the
// account, and the shell proves the token is bound to that before handing it
// over. See docs/guides/build-module-quickstart.md.
const ManagementAudience = "identity-management"

// ManagementScope is the permission every command here needs: the deployment's
// own management permission, as a self-hosted ThunderID names it.
const ManagementScope = "system"

// CloudManagementScope is the same permission as WSO2 Cloud names it.
const CloudManagementScope = "iam:system"

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
		fmt.Fprintf(os.Stderr, "%s: %v\n", "wso2-module-"+Namespace, err)
		os.Exit(1)
	}
}

// moduleOptions describe this module to the SDK.
//
// AuthAudiences names the management audience every handler requests access
// for, and module.json declares the same values: the shell intersects a
// runtime request with what the module declared at installation, so an
// audience declared in only one place is refused rather than granted.
// AuthScopes names the management permission under both deployments' names.
func moduleOptions() module.Options {
	return module.Options{
		Namespace:     Namespace,
		Version:       moduleVersion,
		AuthAudiences: []string{ManagementAudience},
		AuthScopes:    []string{ManagementScope, CloudManagementScope},
	}
}

// commands builds this module's command tree and binds each command to its
// handler.
//
// The tree is an ordinary Cobra tree: commands, flags, and help are declared
// here exactly as they would be in a standalone CLI. What differs is the
// ending, because a handler returns fields instead of printing them.
func commands() *cobratree.Tree {
	// Every command declares an Example: the one line that runs it, with
	// placeholders for what the reader supplies. The shell shows a leaf's on
	// its page, a group's on the group's, and the root's as the next step
	// once the product is set up, all from the declared tree and with no
	// module launched. A flag a command refuses to run without is marked
	// required, which the shell shows and the SDK enforces.
	root := &cobra.Command{
		Use:     Namespace,
		Short:   "Work with WSO2 Identity Platform: users, apps, and resource servers.",
		Example: "  ws iam user list",
	}
	statusCommand := &cobra.Command{
		Use:     "status",
		Short:   "Report this module's own status and what to run first.",
		Example: "  ws iam status",
	}
	usersCommand := &cobra.Command{
		Use:     "user",
		Short:   "Read the people this deployment knows.",
		Example: "  ws iam user list",
	}
	usersListCommand := &cobra.Command{
		Use:     "list",
		Short:   "List the users the deployment records.",
		Example: "  ws iam user list",
	}
	usersCommand.AddCommand(usersListCommand)

	resourceServersCommand := &cobra.Command{
		Use:   "resource-server",
		Short: "Read, create and delete the resource servers products are known by.",
		Example: "  ws iam resource-server list\n" +
			"  ws iam resource-server create <name> --identifier <uri>",
	}
	resourceServersListCommand := &cobra.Command{
		Use:     "list",
		Short:   "List the resource servers the deployment records.",
		Example: "  ws iam resource-server list",
	}
	resourceServersCreateCommand := &cobra.Command{
		Use:     "create <name> --identifier <uri>",
		Short:   "Create a resource server a product's access is bound to.",
		Example: "  ws iam resource-server create <name> --identifier <uri>",
	}
	// Declared on the command rather than parsed by hand: the SDK parses a
	// module's own arguments with this flag set before the handler runs, so a
	// hand parser would never see a flag Cobra had already refused.
	var createFlags resourceServerFlags
	resourceServersCreateCommand.Flags().StringVar(&createFlags.identifier, "identifier", "",
		"The absolute URI the deployment binds this resource server's tokens to.")
	resourceServersCreateCommand.Flags().StringVar(&createFlags.description, "description", "",
		"What this resource server is for.")
	resourceServersCreateCommand.Flags().StringArrayVar(&createFlags.permissions, "permission", nil,
		"A permission handle to create on it; repeat for each.")
	resourceServersCreateCommand.Flags().StringVar(&createFlags.ou, "ou", "",
		"The organization unit that owns it, by id or handle. Defaults to the "+
			"deployment's only organization unit; required when it records more than one.")
	if err := resourceServersCreateCommand.MarkFlagRequired("identifier"); err != nil {
		// Only a flag the command never declared fails here, which is a
		// programming error in this file and not a runtime condition.
		panic(err)
	}
	resourceServersDeleteCommand := &cobra.Command{
		Use:     "delete <id-or-identifier> [--yes]",
		Short:   "Delete a resource server and its permissions, asking first; ws iam resource-server list shows the ids.",
		Example: "  ws iam resource-server delete <id-or-identifier>",
	}
	deleteFlags := resourceServerDeleteFlags{}
	resourceServersDeleteCommand.Flags().BoolVar(&deleteFlags.yes, "yes", false,
		"Delete without asking for confirmation.")
	resourceServersCommand.AddCommand(resourceServersListCommand, resourceServersCreateCommand,
		resourceServersDeleteCommand)

	appsCommand := &cobra.Command{
		Use:     "app",
		Short:   "Read the applications this deployment registers.",
		Example: "  ws iam app list",
	}
	appsListCommand := &cobra.Command{
		Use:     "list",
		Short:   "List the applications the deployment records.",
		Example: "  ws iam app list",
	}
	appsCommand.AddCommand(appsListCommand)

	root.AddCommand(statusCommand, usersCommand, appsCommand, resourceServersCommand)

	// The JSON members each result carries, declared so that ws help
	// --output json can say what a command answers before it is run. A
	// listing's rows are one member, "rows", holding its columns.
	cobratree.ResultFields(statusCommand, "namespace", "version", "endpoint", NextField)
	cobratree.ResultFields(usersListCommand, "count", "rows", NextField)
	cobratree.ResultFields(appsListCommand, "count", "rows", NextField)
	cobratree.ResultFields(resourceServersListCommand, "count", "rows", NextField)
	cobratree.ResultFields(resourceServersCreateCommand, "id", "name", "identifier", "permissions", NextField)

	return cobratree.New(root).
		Handle(statusCommand, status).
		Handle(usersListCommand, usersList).
		Handle(appsListCommand, appsList).
		Handle(resourceServersListCommand, resourceServersList).
		Handle(resourceServersCreateCommand, resourceServersCreate(resourceServersCreateCommand, &createFlags)).
		Handle(resourceServersDeleteCommand, resourceServersDelete(resourceServersDeleteCommand, &deleteFlags))
}

// status answers "ws iam status".
//
// It reports what it can know without asking anything of the shell, so a freshly
// generated module answers before it has been given an identity to act as. Call
// your product from here: the invocation carries the selected context, and
// request.Access.Acquire is how a handler obtains short-lived access to it.
func status(ctx context.Context, request module.Request) (result.Result, error) {
	next := "Create a context that logs in through this product with ws context create <name> " +
		"--login-product iam --url <url> --use, then run ws login."
	if request.Context.Endpoint != "" {
		next = "Run ws iam --help to see what this module can do at " + request.Context.Endpoint + "."
	}
	return result.New(StatusSchema).
		With("namespace", "Namespace", Namespace).
		With("version", "Version", moduleVersion).
		With("endpoint", "Endpoint", request.Context.Endpoint).
		With(NextField, "Next", next), nil
}
