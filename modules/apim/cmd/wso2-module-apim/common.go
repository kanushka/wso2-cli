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
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/modules/apim/internal/apim"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// The three management planes, relative to the context's endpoint.
const (
	publisherPath = "/api/am/publisher/v4"
	devportalPath = "/api/am/devportal/v3"
	adminPath     = "/api/am/admin/v4"
)

// managementClient returns a client on the context's endpoint. It names no
// scopes: the shell answers with the scopes the identity's apim product
// records, and that record is the ceiling for every command here.
func managementClient(ctx context.Context, request module.Request) (*apim.Client, error) {
	access, err := request.Access.Acquire(ctx, module.AccessRequest{Audience: PublisherAudience})
	if err != nil {
		return nil, err
	}
	if request.Context.Endpoint == "" {
		return nil, problem.New(problem.CategoryUsage, "apim.no_endpoint",
			"the selected context does not name an API Manager endpoint").
			WithRecovery("Run wso2 apim bootstrap --url <base> once, then the wso2 apim connect line it prints.")
	}
	return apim.New(request.Context.Endpoint, access.Token), nil
}

func oneArgument(command *cobra.Command, what string) (string, error) {
	args := command.Flags().Args()
	if len(args) != 1 {
		return "", problem.New(problem.CategoryUsage, "apim.missing_argument",
			fmt.Sprintf("%s needs exactly one argument, %s, got %d", command.CommandPath(), what, len(args))).
			WithRecovery("Run " + command.CommandPath() + " --help for the usage.")
	}
	return args[0], nil
}

func twoArguments(command *cobra.Command, what string) (string, string, error) {
	args := command.Flags().Args()
	if len(args) != 2 {
		return "", "", problem.New(problem.CategoryUsage, "apim.missing_argument",
			fmt.Sprintf("%s needs two arguments, %s, got %d", command.CommandPath(), what, len(args))).
			WithRecovery("Run " + command.CommandPath() + " --help for the usage.")
	}
	return args[0], args[1], nil
}

func missingFlag(command, flag, why string) problem.Problem {
	return problem.New(problem.CategoryUsage, "apim.missing_flag",
		fmt.Sprintf("wso2 apim %s needs %s", command, flag)).WithRecovery(why)
}

func notFound(what, name, listing string) problem.Problem {
	return problem.New(problem.CategoryUsage, "apim.not_found",
		fmt.Sprintf("API Manager has no %s named %q", what, name)).
		WithRecovery(fmt.Sprintf("Run wso2 apim %s to see the names it knows.", listing))
}

// secretFromEnvironment reads a secret the shell handed this module under
// its own prefix; the variable's name is all a command line carries.
func secretFromEnvironment(variable, what string) (string, error) {
	value := os.Getenv(variable)
	if value == "" {
		return "", problem.New(problem.CategoryUsage, "apim.missing_secret",
			fmt.Sprintf("the environment variable %s, which should hold %s, is not set", variable, what)).
			WithRecovery(fmt.Sprintf("Export %s before running the command. The shell hands a module "+
				"only the WSO2_APIM_ variables, and never a value given as a flag.", variable))
	}
	return value, nil
}

// nameVersion splits MockAPI/1.0.0 into its two halves.
func nameVersion(reference string) (string, string, error) {
	name, version, found := strings.Cut(reference, "/")
	if !found || name == "" || version == "" {
		return "", "", problem.New(problem.CategoryUsage, "apim.missing_argument",
			fmt.Sprintf("%q is not an API reference", reference)).
			WithRecovery("Name an API as <name>/<version>, such as MockAPI/1.0.0.")
	}
	return name, version, nil
}

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
