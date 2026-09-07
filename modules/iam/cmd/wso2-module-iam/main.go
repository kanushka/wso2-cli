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

// Command wso2-module-iam is the ThunderID product module for the WSO2 CLI.
//
// It registers this CLI with a ThunderID deployment once (bootstrap, with an
// administrator password the shell hands it under the WSO2_IAM_ prefix) and
// then manages resource servers, users, applications and roles through
// access the shell brokers under the system scope.
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

// Namespace is the top-level command this module owns.
const Namespace = "iam"

// The logical audience and scope this module asks the shell for. The shell
// maps the audience to the deployment's system resource server.
const (
	SystemAudience = "thunder-system"
	SystemScope    = "system"
)

// StatusSchema identifies the shape of the status result.
const StatusSchema = "iam.status/v1"

// NextField is the field name the shell renders as a trailing next-step line.
const NextField = "next"

// moduleVersion is injected at build time by the release tooling.
var moduleVersion = "0.0.0-dev"

func main() {
	// Standard output carries protocol frames only; diagnostics go to stderr.
	if err := commands().Serve(context.Background(), moduleOptions()); err != nil {
		fmt.Fprintf(os.Stderr, "wso2-module-iam: %v\n", err)
		os.Exit(1)
	}
}

func moduleOptions() module.Options {
	return module.Options{
		Namespace:     Namespace,
		Version:       moduleVersion,
		AuthAudiences: []string{SystemAudience},
		AuthScopes:    []string{SystemScope},
	}
}

func commands() *cobratree.Tree {
	root := &cobra.Command{
		Use:   Namespace,
		Short: "ThunderID commands for the WSO2 CLI.",
	}
	statusCommand := &cobra.Command{
		Use:   "status",
		Short: "Report this module's own status and what to run first.",
	}
	bootstrapCommand, bootstrapFlags := bootstrapCommand()
	resourceServers, resourceServersListCommand, resourceServersCreateCommand, resourceServerFlags := resourceServerCommands()
	users, usersListCommand, usersCreateCommand, userFlags := userCommands()
	apps, appsListCommand, appsCreateCommand, appFlags := appCommands()
	roles, rolesListCommand, rolesCreateCommand, roleFlags, rolesAssignCommand, roleAssignFlags := roleCommands()
	root.AddCommand(statusCommand, bootstrapCommand, resourceServers, users, apps, roles)
	return cobratree.New(root).
		Handle(statusCommand, status).
		Handle(bootstrapCommand, bootstrap(bootstrapFlags)).
		Handle(resourceServersListCommand, resourceServersList).
		Handle(resourceServersCreateCommand, resourceServersCreate(resourceServersCreateCommand, resourceServerFlags)).
		Handle(usersListCommand, usersList).
		Handle(usersCreateCommand, usersCreate(usersCreateCommand, userFlags)).
		Handle(appsListCommand, appsList).
		Handle(appsCreateCommand, appsCreate(appsCreateCommand, appFlags)).
		Handle(rolesListCommand, rolesList).
		Handle(rolesCreateCommand, rolesCreate(rolesCreateCommand, roleFlags)).
		Handle(rolesAssignCommand, rolesAssign(rolesAssignCommand, roleAssignFlags))
}

func status(ctx context.Context, request module.Request) (result.Result, error) {
	next := "Run wso2 iam bootstrap --url <issuer> once, then the wso2 iam connect line it prints, then wso2 login."
	if request.Context.Endpoint != "" {
		next = "Run wso2 iam resource-servers list to see what this deployment serves."
	}
	return result.New(StatusSchema).
		With("namespace", "Namespace", Namespace).
		With("version", "Version", moduleVersion).
		With("endpoint", "Endpoint", request.Context.Endpoint).
		With(NextField, "Next", next), nil
}
