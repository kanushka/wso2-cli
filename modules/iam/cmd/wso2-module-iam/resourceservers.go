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
	"strings"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/modules/iam/internal/thunder"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/result"
)

const (
	ResourceServersSchema = "iam.resource-servers/v1"
	ResourceServerSchema  = "iam.resource-server/v1"
)

type resourceServerCreateFlags struct {
	identifier, ou string
	permissions    []string
}

func resourceServerCommands() (*cobra.Command, *cobra.Command, *cobra.Command, *resourceServerCreateFlags) {
	family := &cobra.Command{Use: "resource-servers", Short: "Manage the APIs ThunderID issues tokens for."}
	list := &cobra.Command{Use: "list", Short: "List the resource servers."}
	flags := &resourceServerCreateFlags{}
	create := &cobra.Command{
		Use:   "create <name> --identifier <uri> [--permission a:b:c]...",
		Short: "Register an API as a resource server, with its permission tree.",
	}
	create.Flags().StringVar(&flags.identifier, "identifier", "",
		"The resource server's identifier: the audience its tokens carry, usually the API's URL.")
	create.Flags().StringArrayVar(&flags.permissions, "permission", nil,
		"A permission as a colon-separated handle path, such as orders:read; repeat for each.")
	create.Flags().StringVar(&flags.ou, "ou", DefaultOU, "The organization unit that owns it.")
	family.AddCommand(list, create)
	return family, list, create, flags
}

func resourceServersList(ctx context.Context, request module.Request) (result.Result, error) {
	client, err := managementClient(ctx, request)
	if err != nil {
		return result.Result{}, err
	}
	var listed thunder.ResourceServerList
	if err := client.Get(ctx, "/resource-servers", &listed); err != nil {
		return result.Result{}, thunder.Problem(err, "the resource server listing")
	}
	names := make([]string, 0, len(listed.ResourceServers))
	for _, server := range listed.ResourceServers {
		names = append(names, server.Name+" ("+server.Identifier+")")
	}
	return result.New(ResourceServersSchema).
		With("total", "Total", fmt.Sprintf("%d", listed.TotalResults)).
		With("resourceServers", "Resource servers", joined(names)).
		With(NextField, "Next", "Run wso2 iam resource-servers create <name> --identifier <uri> "+
			"--permission <a:b:c> to register an API."), nil
}

func resourceServersCreate(command *cobra.Command, flags *resourceServerCreateFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		name, err := oneArgument(command, "a resource server name")
		if err != nil {
			return result.Result{}, err
		}
		if flags.identifier == "" {
			return result.Result{}, problem.New(problem.CategoryUsage, "iam.missing_flag",
				"wso2 iam resource-servers create needs --identifier").
				WithRecovery("Pass --identifier <uri>, the audience the API's tokens must carry.")
		}
		client, err := managementClient(ctx, request)
		if err != nil {
			return result.Result{}, err
		}
		server, created, err := ensureResourceServer(ctx, client, name, flags)
		if err != nil {
			return result.Result{}, err
		}
		for _, permission := range flags.permissions {
			if err := ensurePermission(ctx, client, server.ID, permission); err != nil {
				return result.Result{}, err
			}
		}
		first := "<permission>"
		if len(flags.permissions) > 0 {
			first = flags.permissions[0]
		}
		return result.New(ResourceServerSchema).
			With("id", "ID", server.ID).
			With("name", "Name", server.Name).
			With("identifier", "Identifier", server.Identifier).
			With("created", "Created", createdWord(created)).
			With("permissions", "Permissions", joined(flags.permissions)).
			With(NextField, "Next", fmt.Sprintf("Run wso2 iam roles create <role> --resource-server %q "+
				"--permission %s --assign-user <username> to grant it.", name, first)), nil
	}
}

func ensureResourceServer(ctx context.Context, client *thunder.Client, name string,
	flags *resourceServerCreateFlags) (thunder.ResourceServer, bool, error) {
	var listed thunder.ResourceServerList
	if err := client.Get(ctx, "/resource-servers", &listed); err != nil {
		return thunder.ResourceServer{}, false, thunder.Problem(err, "the resource server listing")
	}
	for _, server := range listed.ResourceServers {
		if server.Name == name {
			return server, false, nil
		}
	}
	var created thunder.ResourceServer
	body := thunder.ResourceServer{Name: name, Identifier: flags.identifier, OUID: flags.ou,
		Description: "Registered by wso2 iam resource-servers create"}
	if err := client.Post(ctx, "/resource-servers", body, &created); err != nil {
		return thunder.ResourceServer{}, false, thunder.Problem(err, "the resource server creation")
	}
	return created, true, nil
}

// ensurePermission walks a:b:c down the resource tree, creating what is
// missing and reusing what exists. ThunderID lists one level at a time: the
// top level without a parent, children by parentId.
func ensurePermission(ctx context.Context, client *thunder.Client, serverID, permission string) error {
	parent := ""
	for _, handle := range strings.Split(permission, ":") {
		path := "/resource-servers/" + serverID + "/resources?limit=100"
		if parent != "" {
			path += "&parentId=" + parent
		}
		var listed thunder.ResourceList
		if err := client.Get(ctx, path, &listed); err != nil {
			return thunder.Problem(err, "the resource listing")
		}
		found := ""
		for _, resource := range listed.Resources {
			if resource.Handle == handle {
				found = resource.ID
				break
			}
		}
		if found == "" {
			var created thunder.Resource
			body := thunder.Resource{Name: title(handle), Handle: handle, Parent: parent}
			if err := client.Post(ctx, "/resource-servers/"+serverID+"/resources", body, &created); err != nil {
				return thunder.Problem(err, fmt.Sprintf("creating the resource %q of %q", handle, permission))
			}
			found = created.ID
		}
		parent = found
	}
	return nil
}
