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
	"github.com/wso2/wso2-cli/sdk/result"
)

// The semantic shapes this module's listings take.
const (
	ProjectsSchema    = "api.projects/v1"
	ApisSchema        = "api.restApis/v1"
	GatewayApisSchema = "api.gatewayRestApis/v1"
)

// controlPlanePath is the base path every control-plane operation hangs off,
// and gatewayPath the gateway's own. They differ because the two surfaces are
// separate products of the same platform rather than one API behind two hosts.
const (
	controlPlanePath = "/api/v0.9"
	gatewayPath      = "/api/management/v1"
)

type project struct {
	ID             string `json:"id"`
	DisplayName    string `json:"displayName"`
	OrganizationID string `json:"organizationId"`
}

type restAPI struct {
	ID              string `json:"id"`
	DisplayName     string `json:"displayName"`
	Version         string `json:"version"`
	Context         string `json:"context"`
	LifeCycleStatus string `json:"lifeCycleStatus"`
}

// page is the control plane's listing envelope.
type page[T any] struct {
	Count int `json:"count"`
	List  []T `json:"list"`
}

// projectsList answers "wso2 api projects list".
func projectsList(ctx context.Context, request module.Request) (result.Result, error) {
	client, err := controlPlane(ctx, request)
	if err != nil {
		return result.Result{}, err
	}
	var listing page[project]
	if err := client.Get(ctx, controlPlanePath+"/projects", &listing); err != nil {
		return result.Result{}, callFailed(err, "read the projects", request.Context.Endpoint)
	}
	report := result.New(ProjectsSchema).
		With("count", "Projects", strconv.Itoa(listing.Count)).
		WithColumn("displayName", "Name").
		WithColumn("id", "ID").
		WithColumn("organizationId", "Organization")
	for _, p := range listing.List {
		report = report.WithRow(p.DisplayName, p.ID, p.OrganizationID)
	}
	next := "Run wso2 api apis list --project <id> to see what one holds."
	if listing.Count == 0 {
		next = "This organization holds no projects yet."
	}
	return report.With(NextField, "Next", next), nil
}

// apisList answers "wso2 api apis list [--project <id>]".
//
// The control plane requires a project: measured against API Platform 0.16.0,
// /rest-apis without projectId is refused. So with no --project this reads the
// projects first and lists each one's APIs beside the project that holds them,
// rather than making a user run one listing per project to find an API.
func apisList(command *cobra.Command, projectID *string) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		_ = command
		client, err := controlPlane(ctx, request)
		if err != nil {
			return result.Result{}, err
		}
		projects := []string{*projectID}
		if *projectID == "" {
			var listing page[project]
			if err := client.Get(ctx, controlPlanePath+"/projects", &listing); err != nil {
				return result.Result{}, callFailed(err, "read the projects", request.Context.Endpoint)
			}
			projects = projects[:0]
			for _, p := range listing.List {
				projects = append(projects, p.ID)
			}
		}

		report := result.New(ApisSchema)
		if *projectID != "" {
			report = report.With("project", "Project", *projectID)
		}
		count := 0
		rows := [][]string{}
		for _, id := range projects {
			query := url.Values{"projectId": {id}}
			var listing page[restAPI]
			if err := client.Get(ctx, controlPlanePath+"/rest-apis?"+query.Encode(), &listing); err != nil {
				return result.Result{}, callFailed(err, "read the APIs", request.Context.Endpoint)
			}
			count += listing.Count
			for _, api := range listing.List {
				rows = append(rows, []string{api.DisplayName, api.Version, api.Context,
					api.LifeCycleStatus, api.ID, id})
			}
		}
		report = report.With("count", "APIs", strconv.Itoa(count)).
			WithColumn("displayName", "Name").
			WithColumn("version", "Version").
			WithColumn("context", "Context").
			WithColumn("lifeCycleStatus", "Lifecycle").
			WithColumn("id", "ID")
		if *projectID == "" {
			report = report.WithColumn("projectId", "Project")
		}
		for _, row := range rows {
			if *projectID != "" {
				row = row[:5]
			}
			report = report.WithRow(row...)
		}
		return report.With(NextField, "Next",
			"Run wso2 api gateway apis list to see which of these a gateway is serving."), nil
	}
}

// gatewayRestAPI is one entry of a gateway's own listing. The gateway answers
// in the deployment artifact's shape — a CR with a spec and a status — rather
// than in the control plane's, so it is read separately.
type gatewayRestAPI struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		DisplayName string `json:"displayName"`
		Version     string `json:"version"`
		Context     string `json:"context"`
	} `json:"spec"`
	Status struct {
		ID    string `json:"id"`
		State string `json:"state"`
	} `json:"status"`
}

type gatewayListing struct {
	Count int              `json:"count"`
	APIs  []gatewayRestAPI `json:"apis"`
}

// gatewayApisList answers "wso2 api gateway apis list": what the gateway is
// actually serving, which is not always what the control plane designed.
func gatewayApisList(ctx context.Context, request module.Request) (result.Result, error) {
	client, err := gateway(ctx, request)
	if err != nil {
		return result.Result{}, err
	}
	var listing gatewayListing
	if err := client.Get(ctx, gatewayPath+"/rest-apis", &listing); err != nil {
		return result.Result{}, callFailed(err, "read the deployed APIs",
			request.Context.GatewayEndpoint)
	}
	report := result.New(GatewayApisSchema).
		With("count", "Deployed APIs", strconv.Itoa(listing.Count)).
		WithColumn("displayName", "Name").
		WithColumn("version", "Version").
		WithColumn("context", "Context").
		WithColumn("state", "State").
		WithColumn("id", "ID")
	for _, api := range listing.APIs {
		name := api.Spec.DisplayName
		if name == "" {
			name = api.Metadata.Name
		}
		report = report.WithRow(name, api.Spec.Version, api.Spec.Context,
			api.Status.State, api.Status.ID)
	}
	return report.With(NextField, "Next",
		"A deployed API answers at the gateway's own endpoint."), nil
}
