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
)

// managementClient acquires system access through the shell and returns a
// client on the context's endpoint. Every command but bootstrap and status
// starts here.
func managementClient(ctx context.Context, request module.Request) (*thunder.Client, error) {
	access, err := request.Access.Acquire(ctx, module.AccessRequest{
		Audience: SystemAudience,
		Scopes:   []string{SystemScope},
	})
	if err != nil {
		return nil, err
	}
	if request.Context.Endpoint == "" {
		return nil, problem.New(problem.CategoryUsage, "iam.no_endpoint",
			"the selected context does not name a ThunderID endpoint").
			WithRecovery("Run wso2 iam bootstrap --url <issuer> and the wso2 iam connect line it prints, " +
				"or record the endpoint with wso2 identity add-product <identity> iam --endpoint <url>.")
	}
	return thunder.New(request.Context.Endpoint, access.Token), nil
}

// notFound is the problem for a name the command line gave that the
// deployment does not know.
func notFound(what, name, listing string) problem.Problem {
	return problem.New(problem.CategoryUsage, "iam.not_found",
		fmt.Sprintf("ThunderID has no %s named %q", what, name)).
		WithRecovery(fmt.Sprintf("Run wso2 iam %s to see the names it knows.", listing))
}

// joined renders a list for a table cell.
func joined(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	return strings.Join(items, ", ")
}

func createdWord(created bool) string {
	if created {
		return "true"
	}
	return "false"
}

// title makes a display name from a permission handle: "status" → "Status".
func title(handle string) string {
	if handle == "" {
		return handle
	}
	return strings.ToUpper(handle[:1]) + handle[1:]
}

// oneArgument reads the single positional argument of a command whose flags
// the tree has already parsed. The tree does not run cobra's own argument
// validation, so the count is checked here.
func oneArgument(command *cobra.Command, what string) (string, error) {
	args := command.Flags().Args()
	if len(args) != 1 {
		return "", problem.New(problem.CategoryUsage, "iam.missing_argument",
			fmt.Sprintf("%s needs exactly one argument, %s, got %d", command.CommandPath(), what, len(args))).
			WithRecovery("Run " + command.CommandPath() + " --help for the usage.")
	}
	return args[0], nil
}
