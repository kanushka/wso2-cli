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

// roleAssignFlags are the assignees wso2 iam roles assign adds to a role.
type roleAssignFlags struct {
	users, apps []string
}

// roleCommands is the roles family: list, create and assign, with the flags
// the two writing commands act on.
func roleCommands() (*cobra.Command, *cobra.Command, *cobra.Command, *roleCreateFlags, *cobra.Command, *roleAssignFlags) {
	family := &cobra.Command{
		Use:   "roles",
		Short: "Manage ThunderID roles: who may do what on which API.",
		Long: "A role grants permissions on one resource server to the users and apps assigned to it. " +
			"roles create makes the role, with its first assignees; roles assign adds users and apps to " +
			"a role that exists. A session established before a role was granted keeps the permissions " +
			"it was minted with, so log in again afterwards.",
	}
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
	assignFlags := &roleAssignFlags{}
	assign := &cobra.Command{
		Use:   "assign <name> [--user <username>]... [--app <client-id>]...",
		Short: "Assign users and apps to an existing role; already assigned ones are reported, not refused.",
	}
	assign.Flags().StringArrayVar(&assignFlags.users, "user", nil, "A username to assign; repeat for each.")
	assign.Flags().StringArrayVar(&assignFlags.apps, "app", nil, "A client id to assign; repeat for each.")
	family.AddCommand(list, create, assign)
	return family, list, create, flags, assign, assignFlags
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
		assignments, assigned, err := resolveUsers(ctx, client, flags.users)
		if err != nil {
			return result.Result{}, err
		}
		appAssignments, appsAssigned, err := resolveApps(ctx, client, flags.apps)
		if err != nil {
			return result.Result{}, err
		}
		assigned = append(assigned, appsAssigned...)

		role, err := roleNamed(ctx, client, name)
		if err != nil {
			return result.Result{}, err
		}
		created := false
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

// rolesAssign adds users and apps to a role that exists.
//
// It is idempotent the way every create is: what the role already carries is
// reported as such and not sent again, so a script can run it on every pass.
// The deployment would accept the repeat anyway, but a result that says which
// assignments were new is the one a person reading it needs.
func rolesAssign(command *cobra.Command, flags *roleAssignFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		name, err := oneArgument(command, "a role name")
		if err != nil {
			return result.Result{}, err
		}
		if len(flags.users) == 0 && len(flags.apps) == 0 {
			return result.Result{}, problem.New(problem.CategoryUsage, "iam.missing_flag",
				"wso2 iam roles assign needs at least one --user or --app").
				WithRecovery("Name the users by username and the apps by client id to assign the role to.")
		}
		client, err := managementClient(ctx, request)
		if err != nil {
			return result.Result{}, err
		}
		role, err := roleNamed(ctx, client, name)
		if err != nil {
			return result.Result{}, err
		}
		if role.ID == "" {
			return result.Result{}, notFound("role", name, "roles list")
		}
		wanted, labels, err := resolveUsers(ctx, client, flags.users)
		if err != nil {
			return result.Result{}, err
		}
		appAssignments, appLabels, err := resolveApps(ctx, client, flags.apps)
		if err != nil {
			return result.Result{}, err
		}
		wanted = append(wanted, appAssignments...)
		labels = append(labels, appLabels...)
		current, err := roleAssignments(ctx, client, role.ID)
		if err != nil {
			return result.Result{}, err
		}
		pending, added, already := []thunder.Assignment{}, []string{}, []string{}
		for i, assignment := range wanted {
			if current[assignment] {
				already = append(already, labels[i])
				continue
			}
			pending = append(pending, assignment)
			added = append(added, labels[i])
		}
		if len(pending) > 0 {
			if err := client.Post(ctx, "/roles/"+role.ID+"/assignments/add",
				thunder.AssignmentChange{Assignments: pending}, nil); err != nil {
				return result.Result{}, thunder.Problem(err, "the role assignment")
			}
		}
		return result.New(RoleSchema).
			With("id", "ID", role.ID).
			With("name", "Name", name).
			With("assigned", "Assigned", joined(added)).
			With("already", "Already assigned", joined(already)).
			With(NextField, "Next", "A session established before this keeps what it was minted with: run "+
				"wso2 logout --context <caller> and wso2 login --context <caller> as the user, or "+
				"wso2 login --only <product> for a product beside the login one, then call the API."), nil
	}
}

// assignmentPage is one page of GET /roles/{id}/assignments.
type assignmentPage struct {
	TotalResults int                  `json:"totalResults"`
	Count        int                  `json:"count"`
	Assignments  []thunder.Assignment `json:"assignments"`
}

// assignmentPageSize is how many assignments one page asks for. The deployment
// defaults to 30 and states no maximum for this listing.
const assignmentPageSize = 100

// roleAssignments reads everything the role is assigned to, following the
// deployment's paging by offset until the pages account for the total.
func roleAssignments(ctx context.Context, client *thunder.Client, roleID string) (map[thunder.Assignment]bool, error) {
	current := map[thunder.Assignment]bool{}
	for offset := 0; ; {
		var page assignmentPage
		path := fmt.Sprintf("/roles/%s/assignments?limit=%d&offset=%d", roleID, assignmentPageSize, offset)
		if err := client.Get(ctx, path, &page); err != nil {
			return nil, thunder.Problem(err, "the role assignment listing")
		}
		for _, assignment := range page.Assignments {
			current[assignment] = true
		}
		offset += len(page.Assignments)
		if len(page.Assignments) == 0 || offset >= page.TotalResults {
			return current, nil
		}
	}
}

// roleNamed is the role with this name, or the zero Role when there is none.
func roleNamed(ctx context.Context, client *thunder.Client, name string) (thunder.Role, error) {
	var roles thunder.RoleList
	if err := client.Get(ctx, "/roles", &roles); err != nil {
		return thunder.Role{}, thunder.Problem(err, "the role listing")
	}
	for _, candidate := range roles.Roles {
		if candidate.Name == name {
			return candidate, nil
		}
	}
	return thunder.Role{}, nil
}

// resolveUsers turns usernames into user assignments, with a label for each in
// the order given, refusing the first name the deployment does not know.
func resolveUsers(ctx context.Context, client *thunder.Client, usernames []string) ([]thunder.Assignment, []string, error) {
	assignments, labels := []thunder.Assignment{}, []string{}
	if len(usernames) == 0 {
		return assignments, labels, nil
	}
	var listed thunder.UserList
	if err := client.Get(ctx, "/users", &listed); err != nil {
		return nil, nil, thunder.Problem(err, "the user listing")
	}
	for _, username := range usernames {
		id := ""
		for _, user := range listed.Users {
			if user.Username() == username {
				id = user.ID
			}
		}
		if id == "" {
			return nil, nil, notFound("user", username, "users list")
		}
		assignments = append(assignments, thunder.Assignment{Type: "user", ID: id})
		labels = append(labels, "user "+username)
	}
	return assignments, labels, nil
}

// resolveApps turns client ids into app assignments, as resolveUsers does for
// usernames.
func resolveApps(ctx context.Context, client *thunder.Client, clientIDs []string) ([]thunder.Assignment, []string, error) {
	assignments, labels := []thunder.Assignment{}, []string{}
	if len(clientIDs) == 0 {
		return assignments, labels, nil
	}
	var listed thunder.ApplicationList
	if err := client.Get(ctx, "/applications", &listed); err != nil {
		return nil, nil, thunder.Problem(err, "the application listing")
	}
	for _, clientID := range clientIDs {
		id := ""
		for _, app := range listed.Applications {
			if app.OAuthClientID() == clientID {
				id = app.ID
			}
		}
		if id == "" {
			return nil, nil, notFound("application", clientID, "apps list")
		}
		assignments = append(assignments, thunder.Assignment{Type: "app", ID: id})
		labels = append(labels, "app "+clientID)
	}
	return assignments, labels, nil
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
