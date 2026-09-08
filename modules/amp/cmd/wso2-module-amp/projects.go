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
	"time"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/modules/amp/internal/amp"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/result"
)

// ProjectsSchema identifies the shape of the project listing.
const ProjectsSchema = "amp.projects/v1"

// project is the part of Agent Manager's project record this module reads.
// Fields it does not read are not declared, so a deployment adding some does
// not change what this module answers.
type project struct {
	Name        string    `json:"name"`
	DisplayName string    `json:"displayName"`
	CreatedAt   time.Time `json:"createdAt"`
}

type projectList struct {
	Total    int       `json:"total"`
	Projects []project `json:"projects"`
}

func projectCommands() (family, list *cobra.Command) {
	family = &cobra.Command{Use: "projects", Short: "Read the projects an organization holds."}
	list = &cobra.Command{
		Use:   "list [--org <organization>] [--limit <n>] [--offset <n>]",
		Short: "List the projects in an organization.",
	}
	addPaginationFlags(list)
	family.AddCommand(list)
	return family, list
}

// projectsList answers "wso2 amp projects list". The command that was matched
// is closed over so the handler reads the flags the tree parsed for it.
func projectsList(command *cobra.Command) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		org, err := organization(command, request)
		if err != nil {
			return result.Result{}, err
		}
		client, err := managementClient(ctx, request)
		if err != nil {
			return result.Result{}, err
		}
		var listed projectList
		if err := client.Get(ctx, "/orgs/"+org+"/projects"+pagination(command), &listed); err != nil {
			return result.Result{}, amp.Problem(err, "the project listing")
		}
		names := make([]string, 0, len(listed.Projects))
		for _, p := range listed.Projects {
			names = append(names, fmt.Sprintf("%s (%s, %s)", p.Name, p.DisplayName, p.CreatedAt.Format("2006-01-02")))
		}
		next := "Run wso2 amp agents list --project <name> to see what a project deploys."
		if len(listed.Projects) == 0 {
			next = "Create a project in the Agent Manager console or with amctl project create; this module only reads."
		}
		return result.New(ProjectsSchema).
			With("organization", "Organization", org).
			With("count", "Count", fmt.Sprintf("%d", listed.Total)).
			With("projects", "Projects", joined(names)).
			With(NextField, "Next", next), nil
	}
}
