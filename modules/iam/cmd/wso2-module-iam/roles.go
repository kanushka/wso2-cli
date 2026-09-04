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

package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/modules/iam/internal/thunder"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/result"
)

const (
	RolesSchema = "iam.roles/v1"
	RoleSchema  = "iam.role/v1"
)

type roleCreateFlags struct {
	resourceServer, ou       string
	permissions, users, apps []string
}

func roleCommands() (*cobra.Command, *cobra.Command, *cobra.Command, *roleCreateFlags) {
	family := &cobra.Command{Use: "roles", Short: "Manage ThunderID roles: who may do what on which API."}
	list := &cobra.Command{Use: "list", Short: "List the roles."}
	flags := &roleCreateFlags{}
	create := &cobra.Command{
		Use:   "create <name> --resource-server <name> --permission <a:b:c>... [--assign-user u]... [--assign-app c]...",
		Short: "Create a role granting permissions on a resource server to users and apps.",
	}
	create.Flags().StringVar(&flags.resourceServer, "resource-server", "", "The resource server the permissions belong to.")
	create.Flags().StringArrayVar(&flags.permissions, "permission", nil, "A permission the role grants; repeat for each.")
	create.Flags().StringArrayVar(&flags.users, "assign-user", nil, "A username to assign; repeat for each.")
	create.Flags().StringArrayVar(&flags.apps, "assign-app", nil, "A client id to assign; repeat for each.")
	create.Flags().StringVar(&flags.ou, "ou", DefaultOU, "The organization unit that owns the role.")
	family.AddCommand(list, create)
	return family, list, create, flags
}

func rolesList(ctx context.Context, request module.Request) (result.Result, error) {
	client, err := managementClient(ctx, request)
	if err != nil {
		return result.Result{}, err
	}
	var listed thunder.RoleList
	if err := client.Get(ctx, "/roles", &listed); err != nil {
		return result.Result{}, thunder.Problem(err, "the role listing")
	}
	names := make([]string, 0, len(listed.Roles))
	for _, role := range listed.Roles {
		names = append(names, role.Name)
	}
	return result.New(RolesSchema).
		With("total", "Total", fmt.Sprintf("%d", listed.TotalResults)).
		With("roles", "Roles", joined(names)).
		With(NextField, "Next", "Run wso2 iam roles create <name> --resource-server <name> "+
			"--permission <a:b:c> --assign-user <username> to grant access."), nil
}

func rolesCreate(command *cobra.Command, flags *roleCreateFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		name, err := oneArgument(command, "a role name")
		if err != nil {
			return result.Result{}, err
		}
		if flags.resourceServer == "" || len(flags.permissions) == 0 {
			return result.Result{}, problem.New(problem.CategoryUsage, "iam.missing_flag",
				"wso2 iam roles create needs --resource-server and at least one --permission").
				WithRecovery("Name the resource server the permissions belong to and the permissions to grant.")
		}
		client, err := managementClient(ctx, request)
		if err != nil {
			return result.Result{}, err
		}
		serverID, err := resourceServerID(ctx, client, flags.resourceServer)
		if err != nil {
			return result.Result{}, err
		}
		assignments := []thunder.Assignment{}
		assigned := []string{}
		if len(flags.users) > 0 {
			var listed thunder.UserList
			if err := client.Get(ctx, "/users", &listed); err != nil {
				return result.Result{}, thunder.Problem(err, "the user listing")
			}
			for _, username := range flags.users {
				id := ""
				for _, user := range listed.Users {
					if user.Username() == username {
						id = user.ID
					}
				}
				if id == "" {
					return result.Result{}, notFound("user", username, "users list")
				}
				assignments = append(assignments, thunder.Assignment{Type: "user", ID: id})
				assigned = append(assigned, "user "+username)
			}
		}
		appAssignments := []thunder.Assignment{}
		if len(flags.apps) > 0 {
			var listed thunder.ApplicationList
			if err := client.Get(ctx, "/applications", &listed); err != nil {
				return result.Result{}, thunder.Problem(err, "the application listing")
			}
			for _, clientID := range flags.apps {
				id := ""
				for _, app := range listed.Applications {
					if app.OAuthClientID() == clientID {
						id = app.ID
					}
				}
				if id == "" {
					return result.Result{}, notFound("application", clientID, "apps list")
				}
				appAssignments = append(appAssignments, thunder.Assignment{Type: "app", ID: id})
				assigned = append(assigned, "app "+clientID)
			}
		}

		var roles thunder.RoleList
		if err := client.Get(ctx, "/roles", &roles); err != nil {
			return result.Result{}, thunder.Problem(err, "the role listing")
		}
		role, created := thunder.Role{}, false
		for _, candidate := range roles.Roles {
			if candidate.Name == name {
				role = candidate
			}
		}
		if role.ID == "" {
			body := thunder.Role{Name: name, Description: "Created by wso2 iam roles create", OUID: flags.ou,
				Permissions: []thunder.RolePermission{{ResourceServerID: serverID, Permissions: flags.permissions}},
				Assignments: assignments}
			if err := client.Post(ctx, "/roles", body, &role); err != nil {
				return result.Result{}, thunder.Problem(err, "the role creation")
			}
			created = true
			assignments = nil
		}
		// An existing role gets the users too; a new one carried them already.
		pending := append(assignments, appAssignments...)
		if len(pending) > 0 {
			if err := client.Post(ctx, "/roles/"+role.ID+"/assignments/add",
				thunder.AssignmentChange{Assignments: pending}, nil); err != nil {
				return result.Result{}, thunder.Problem(err, "the role assignment")
			}
		}
		return result.New(RoleSchema).
			With("id", "ID", role.ID).
			With("name", "Name", name).
			With("created", "Created", createdWord(created)).
			With("permissions", "Permissions", joined(flags.permissions)).
			With("assigned", "Assigned", joined(assigned)).
			With(NextField, "Next", "Run wso2 login --context <caller> and call the API, or "+
				"wso2 apim bootstrap --url <base> to register API Manager next."), nil
	}
}

func resourceServerID(ctx context.Context, client *thunder.Client, name string) (string, error) {
	var listed thunder.ResourceServerList
	if err := client.Get(ctx, "/resource-servers", &listed); err != nil {
		return "", thunder.Problem(err, "the resource server listing")
	}
	for _, server := range listed.ResourceServers {
		if server.Name == name || server.Identifier == name {
			return server.ID, nil
		}
	}
	return "", notFound("resource server", name, "resource-servers list")
}
