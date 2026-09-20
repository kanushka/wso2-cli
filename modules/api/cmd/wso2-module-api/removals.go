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
	"net/url"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/modules/api/internal/platform"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/result"
)

// The semantic shapes this module's removals take.
const (
	UndeploymentSchema = "api.undeployment/v1"
	RemovalSchema      = "api.removal/v1"
)

// removalFlags are what a delete command was asked for.
//
// A module never prompts — the protocol gives it no terminal to ask on — so a
// deletion is confirmed on the command line instead: without --yes the command
// says what it would delete and calls nothing.
type removalFlags struct {
	yes bool
}

// apisUndeployFlags are what "wso2 api apis undeploy" was asked for.
type apisUndeployFlags struct {
	gatewayID string
}

// confirmed refuses a deletion that --yes did not confirm, naming what would
// have been deleted and the command line that deletes it.
func (f removalFlags) confirmed(what, commandLine string) error {
	if f.yes {
		return nil
	}
	return usageProblem("deleting "+what+" cannot be undone, and --yes did not confirm it",
		"Run "+commandLine+" --yes to delete it.")
}

// undeployActive undeploys every deployment of apiID that is still active, on
// gatewayID alone when one is named, and reports the deployments it undeployed.
//
// Undeploy is addressed to one deployment and carries the gateway it is bound
// to as a query parameter, so the deployments are read first rather than asking
// a user for an id no other command of this module ever showed them.
func undeployActive(ctx context.Context, client platform.Client, endpoint, apiID, gatewayID string) ([]deployment, error) {
	base := controlPlanePath + "/rest-apis/" + url.PathEscape(apiID) + "/deployments"
	var listing page[deployment]
	if err := client.Get(ctx, base, &listing); err != nil {
		return nil, removalCallFailed(err, "read the API's deployments", endpoint, "API "+apiID)
	}
	var undeployed []deployment
	for _, d := range listing.List {
		// The same two statuses apis invoke calls deployed: every other one
		// (UNDEPLOYED, ARCHIVED, a transition out) has nothing left to undeploy.
		if d.Status != "DEPLOYED" && d.Status != "DEPLOYING" {
			continue
		}
		if gatewayID != "" && d.GatewayID != gatewayID {
			continue
		}
		query := url.Values{"gatewayId": {d.GatewayID}}
		path := base + "/" + url.PathEscape(d.DeploymentID) + "/undeploy?" + query.Encode()
		if err := client.Post(ctx, path, nil, nil); err != nil {
			// The listing lags an undeploy by a moment, so a deployment it
			// still calls DEPLOYED can already be inactive. That is the
			// outcome asked for, not a refusal.
			var refusal platform.Failure
			if errors.As(err, &refusal) && refusal.Code == "DEPLOYMENT_NOT_ACTIVE" {
				continue
			}
			return undeployed, removalCallFailed(err, "undeploy the API", endpoint, "API "+apiID)
		}
		undeployed = append(undeployed, d)
	}
	return undeployed, nil
}

// apisUndeploy answers "wso2 api apis undeploy <api-id> [--gateway-id <id>]".
// It asks for no --yes: an undeployed API is deployed again with one command.
func apisUndeploy(command *cobra.Command, flags *apisUndeployFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		apiID, err := exactlyOneArgument(command.Flags().Args(),
			"wso2 api apis undeploy needs the id of the API to undeploy",
			"Run wso2 api apis undeploy <api-id>. wso2 api apis list shows the ids.")
		if err != nil {
			return result.Result{}, err
		}
		client, err := controlPlane(ctx, request)
		if err != nil {
			return result.Result{}, err
		}
		undeployed, err := undeployActive(ctx, client, request.Context.Endpoint, apiID, flags.gatewayID)
		if err != nil {
			return result.Result{}, err
		}
		report := result.New(UndeploymentSchema).
			With("apiId", "API", apiID).
			With("count", "Undeployed", strconv.Itoa(len(undeployed))).
			WithColumn("deploymentId", "Deployment ID").
			WithColumn("gatewayId", "Gateway")
		for _, d := range undeployed {
			report = report.WithRow(d.DeploymentID, d.GatewayID)
		}
		next := "The gateway drops the route a moment later; wso2 api gateway apis list shows when."
		if len(undeployed) == 0 {
			next = "The API had no active deployment to undeploy."
		}
		return report.With(NextField, "Next", next), nil
	}
}

// apisDelete answers "wso2 api apis delete <api-id> --yes". It undeploys the
// API from every gateway first, because deleting it is what the user asked for
// and an active deployment is the one thing that would leave a gateway serving
// an API the control plane no longer records.
func apisDelete(command *cobra.Command, flags *removalFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		apiID, err := exactlyOneArgument(command.Flags().Args(),
			"wso2 api apis delete needs the id of the API to delete",
			"Run wso2 api apis delete <api-id> --yes. wso2 api apis list shows the ids.")
		if err != nil {
			return result.Result{}, err
		}
		if err := flags.confirmed("the API "+apiID+" and its deployments",
			"wso2 api apis delete "+apiID); err != nil {
			return result.Result{}, err
		}
		client, err := controlPlane(ctx, request)
		if err != nil {
			return result.Result{}, err
		}
		undeployed, err := undeployActive(ctx, client, request.Context.Endpoint, apiID, "")
		if err != nil {
			return result.Result{}, err
		}
		path := controlPlanePath + "/rest-apis/" + url.PathEscape(apiID)
		if err := client.Delete(ctx, path); err != nil {
			return result.Result{}, removalCallFailed(err, "delete the API", request.Context.Endpoint, "API "+apiID)
		}
		return result.New(RemovalSchema).
			With("kind", "Deleted", "api").
			With("id", "ID", apiID).
			With("undeployed", "Undeployed first", strconv.Itoa(len(undeployed))), nil
	}
}

// projectsDelete answers "wso2 api projects delete <project-id> --yes".
func projectsDelete(command *cobra.Command, flags *removalFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		projectID, err := exactlyOneArgument(command.Flags().Args(),
			"wso2 api projects delete needs the id of the project to delete",
			"Run wso2 api projects delete <project-id> --yes. wso2 api projects list shows the ids.")
		if err != nil {
			return result.Result{}, err
		}
		if err := flags.confirmed("the project "+projectID, "wso2 api projects delete "+projectID); err != nil {
			return result.Result{}, err
		}
		client, err := controlPlane(ctx, request)
		if err != nil {
			return result.Result{}, err
		}
		if err := client.Delete(ctx, controlPlanePath+"/projects/"+url.PathEscape(projectID)); err != nil {
			return result.Result{}, removalCallFailed(err, "delete the project",
				request.Context.Endpoint, "project "+projectID)
		}
		return result.New(RemovalSchema).
			With("kind", "Deleted", "project").
			With("id", "ID", projectID), nil
	}
}

// removalCallFailed refuses a removal's own call. A 404 means the thing named
// on the command line is not there, which the listing answers and the
// deployment's logs do not; a 400 or 409 is the deployment declining — a
// project that still holds APIs, or the organization's last one — and its own
// words say which.
func removalCallFailed(err error, attempted, endpoint, what string) error {
	var refusal platform.Failure
	if errors.As(err, &refusal) {
		switch refusal.Status {
		case 404:
			return moduleProblem("api.not_found", "the deployment records no "+what,
				"Run wso2 api projects list or wso2 api apis list to see what it records.")
		case 400, 409:
			return moduleProblem("api.call_failed",
				"the deployment would not "+attempted+": "+refusalMessage(refusal),
				"Remove what still depends on it, then retry. A project goes once its APIs have, "+
					"and an organization keeps at least one project.")
		}
	}
	return callFailed(err, attempted, endpoint)
}
