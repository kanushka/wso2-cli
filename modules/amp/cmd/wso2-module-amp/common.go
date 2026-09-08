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

	"github.com/wso2/wso2-cli/modules/amp/internal/amp"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// managementClient acquires access through the shell and returns a client on
// the context's endpoint. Every command but status starts here.
//
// It names no scopes: the shell answers with the scopes the identity's amp
// product records, and that record is the ceiling for every command here. A
// user who recorded amp:project:read alone can list projects and nothing
// else, without this module having to know.
func managementClient(ctx context.Context, request module.Request) (*amp.Client, error) {
	access, err := request.Access.Acquire(ctx, module.AccessRequest{Audience: APIAudience})
	if err != nil {
		// A shell policy denial. Returned unchanged: the shell renders it and
		// chooses the exit class.
		return nil, err
	}
	if request.Context.Endpoint == "" {
		return nil, problem.New(problem.CategoryUsage, "amp.no_endpoint",
			"the selected context does not name an Agent Manager endpoint").
			WithRecovery("Record it on the identity you log in with: wso2 identity add-product <identity> amp " +
				"--endpoint <url> --audience <resource> --scopes " + strings.Join(recordedScopes(), ","))
	}
	return amp.New(request.Context.Endpoint, access.Token), nil
}

// organization is the organization a command acts in: --org when given,
// otherwise the one the shell's selected context runs within.
func organization(command *cobra.Command, request module.Request) (string, error) {
	if org, _ := command.Flags().GetString("org"); org != "" {
		return org, nil
	}
	if request.Context.OrganizationID != "" {
		return request.Context.OrganizationID, nil
	}
	return "", problem.New(problem.CategoryUsage, "amp.org_required",
		fmt.Sprintf("%s needs an organization, and the selected context runs within none", command.CommandPath())).
		WithRecovery("Run wso2 org use <organization> to set one for every command, or pass --org <organization>.")
}

// pagination renders the optional --limit and --offset flags as a query
// string, empty when neither was given so the deployment applies its own
// defaults.
func pagination(command *cobra.Command) string {
	var parts []string
	if command.Flags().Changed("limit") {
		limit, _ := command.Flags().GetInt("limit")
		parts = append(parts, fmt.Sprintf("limit=%d", limit))
	}
	if command.Flags().Changed("offset") {
		offset, _ := command.Flags().GetInt("offset")
		parts = append(parts, fmt.Sprintf("offset=%d", offset))
	}
	if len(parts) == 0 {
		return ""
	}
	return "?" + strings.Join(parts, "&")
}

func addPaginationFlags(command *cobra.Command) {
	command.Flags().String("org", "", "The organization to act in; defaults to the selected context's.")
	command.Flags().Int("limit", 0, "Maximum number of results to return.")
	command.Flags().Int("offset", 0, "Number of results to skip.")
}

func recordedScopes() []string {
	return []string{ScopeProjectRead, ScopeAgentRead}
}

func joined(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	return strings.Join(items, ", ")
}
