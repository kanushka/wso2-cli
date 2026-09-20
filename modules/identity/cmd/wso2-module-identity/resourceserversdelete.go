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
	"net/url"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/result"
)

// ResourceServerRemovalSchema identifies the semantic shape of a deleted
// resource server.
const ResourceServerRemovalSchema = "identity.resourceServerRemoval/v1"

// resourceServerDeleteFlags are what delete was asked for. A module never
// prompts, so --yes on the command line is where a deletion is confirmed.
type resourceServerDeleteFlags struct {
	yes bool
}

type resourceServerResources struct {
	Resources []struct {
		ID string `json:"id"`
	} `json:"resources"`
}

// resourceServersDelete answers "wso2 identity resource-servers delete
// <id-or-identifier> --yes".
//
// It takes the identifier as well as the id because the identifier is what
// the person who created one typed and what every other command shows them as
// the audience; the id is ThunderID's own. Permissions are child resources
// the deployment will not delete a resource server over, so they go first.
func resourceServersDelete(command *cobra.Command, flags *resourceServerDeleteFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		const usage = "Run wso2 identity resource-servers delete <id-or-identifier> --yes. " +
			"wso2 identity resource-servers list shows both."
		positional := command.Flags().Args()
		if len(positional) != 1 {
			return result.Result{}, deleteUsageProblem(
				"wso2 identity resource-servers delete needs the one resource server to delete", usage)
		}
		if !flags.yes {
			return result.Result{}, deleteUsageProblem(
				"deleting the resource server "+positional[0]+" cannot be undone, and --yes did not confirm it",
				"Run wso2 identity resource-servers delete "+positional[0]+" --yes to delete it.")
		}
		client, err := clientFor(ctx, request)
		if err != nil {
			return result.Result{}, err
		}
		var listing resourceServerListing
		if err := client.Get(ctx, "/resource-servers", &listing); err != nil {
			return result.Result{}, callFailed(err, "read the resource servers", request.Context.Endpoint)
		}
		var found *resourceServer
		for i, rs := range listing.ResourceServers {
			if rs.ID == positional[0] || rs.Identifier == positional[0] {
				found = &listing.ResourceServers[i]
				break
			}
		}
		if found == nil {
			return result.Result{}, moduleProblem("identity.not_found",
				"the deployment records no resource server with the id or identifier "+positional[0],
				"Run wso2 identity resource-servers list to see what it records.")
		}

		base := "/resource-servers/" + url.PathEscape(found.ID)
		var resources resourceServerResources
		if err := client.Get(ctx, base+"/resources", &resources); err != nil {
			return result.Result{}, callFailed(err, "read the resource server's permissions", request.Context.Endpoint)
		}
		for _, permission := range resources.Resources {
			if err := client.Delete(ctx, base+"/resources/"+url.PathEscape(permission.ID)); err != nil {
				return result.Result{}, callFailed(err, "delete the resource server's permissions", request.Context.Endpoint)
			}
		}
		if err := client.Delete(ctx, base); err != nil {
			return result.Result{}, callFailed(err, "delete the resource server", request.Context.Endpoint)
		}
		return result.New(ResourceServerRemovalSchema).
			With("id", "Deleted", found.ID).
			With("name", "Name", found.Name).
			With("identifier", "Identifier", found.Identifier).
			With("permissions", "Permissions deleted", strconv.Itoa(len(resources.Resources))).
			With(NextField, "Next",
				"Tokens are no longer minted for "+found.Identifier+"; an API naming it as its "+
					"jwt-auth audience stops accepting new ones."), nil
	}
}

// deleteUsageProblem refuses a delete command line. usageProblem's recovery
// is create's own usage, which would send someone deleting to the wrong
// command.
func deleteUsageProblem(message, recovery string) error {
	return problem.New(problem.CategoryUsage, "identity.invalid_argument", message).WithRecovery(recovery)
}
