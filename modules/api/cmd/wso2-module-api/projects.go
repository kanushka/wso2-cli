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
	"errors"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/modules/api/internal/platform"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/result"
)

// ProjectSchema identifies the semantic shape of a created project.
const ProjectSchema = "api.project/v1"

// projectsCreateFlags are what "wso2 api projects create" was asked for.
type projectsCreateFlags struct {
	description string
}

// projectsCreate answers "wso2 api projects create <name> [--description
// <text>]".
//
// CreateRequestProject.id is optional — platform-api's own schema says it is
// "Auto-generated from displayName if not provided" — so this command sends
// only displayName (and description, when given) and lets the deployment
// derive the handle, rather than reimplementing its slug rules here and
// risking the two disagreeing.
func projectsCreate(command *cobra.Command, flags *projectsCreateFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		name, err := exactlyOneArgument(command.Flags().Args(),
			"wso2 api projects create needs the project's display name",
			"Run wso2 api projects create <name>.")
		if err != nil {
			return result.Result{}, err
		}

		client, err := controlPlane(ctx, request)
		if err != nil {
			return result.Result{}, err
		}

		body := map[string]any{"displayName": name}
		if flags.description != "" {
			body["description"] = flags.description
		}
		var created project
		if err := client.Post(ctx, controlPlanePath+"/projects", body, &created); err != nil {
			return result.Result{}, projectCreateCallFailed(err, request.Context.Endpoint, name)
		}
		return result.New(ProjectSchema).
			With("id", "ID", created.ID).
			With("displayName", "Name", created.DisplayName).
			With("organizationId", "Organization", created.OrganizationID).
			With(NextField, "Next",
				"Run wso2 api apis create -f <file> --project "+created.ID+" to create an API in it."), nil
	}
}

// projectCreateCallFailed refuses "wso2 api projects create"'s own call to
// POST /projects. A 409 means a project by this name (or requested id) exists
// already, so the refusal names it and points at the listing rather than
// callFailed's generic "check the deployment's logs" — there is nothing to
// check there for a conflict this command's own input caused.
func projectCreateCallFailed(err error, endpoint, name string) error {
	var refusal platform.Failure
	if errors.As(err, &refusal) && refusal.Status == 409 {
		return moduleProblem("api.project_exists",
			"a project named "+name+" already exists: "+refusalMessage(refusal),
			"Run wso2 api projects list to see it.")
	}
	return callFailed(err, "create the project", endpoint)
}
