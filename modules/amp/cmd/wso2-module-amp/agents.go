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
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/result"
)

// AgentsSchema identifies the shape of the agent listing.
const AgentsSchema = "amp.agents/v1"

// agent is the part of Agent Manager's agent record this module reads.
type agent struct {
	Name        string    `json:"name"`
	DisplayName string    `json:"displayName"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
}

type agentList struct {
	Total  int     `json:"total"`
	Agents []agent `json:"agents"`
}

func agentCommands() (family, list *cobra.Command) {
	family = &cobra.Command{Use: "agents", Short: "Read the agents a project deploys."}
	list = &cobra.Command{
		Use:   "list --project <name> [--org <organization>] [--limit <n>] [--offset <n>]",
		Short: "List the agents in a project.",
	}
	list.Flags().String("project", "", "The project whose agents to list.")
	addPaginationFlags(list)
	family.AddCommand(list)
	return family, list
}

// agentsList answers "wso2 amp agents list".
func agentsList(command *cobra.Command) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		project, _ := command.Flags().GetString("project")
		if project == "" {
			return result.Result{}, problem.New(problem.CategoryUsage, "amp.missing_flag",
				"wso2 amp agents list needs --project <name>").
				WithRecovery("Run wso2 amp projects list to see the names it knows.")
		}
		org, err := organization(command, request)
		if err != nil {
			return result.Result{}, err
		}
		client, err := managementClient(ctx, request)
		if err != nil {
			return result.Result{}, err
		}
		var listed agentList
		path := "/orgs/" + org + "/projects/" + project + "/agents" + pagination(command)
		if err := client.Get(ctx, path, &listed); err != nil {
			return result.Result{}, amp.Problem(err, "the agent listing")
		}
		names := make([]string, 0, len(listed.Agents))
		for _, a := range listed.Agents {
			status := a.Status
			if status == "" {
				status = "unknown"
			}
			names = append(names, fmt.Sprintf("%s (%s, %s)", a.Name, a.DisplayName, status))
		}
		next := "Run wso2 amp agents list --project " + project + " --output json for the same fields as JSON."
		if len(listed.Agents) == 0 {
			next = "Create an agent with amctl agent create; this module only reads."
		}
		return result.New(AgentsSchema).
			With("organization", "Organization", org).
			With("project", "Project", project).
			With("count", "Count", fmt.Sprintf("%d", listed.Total)).
			With("agents", "Agents", joined(names)).
			With(NextField, "Next", next), nil
	}
}
