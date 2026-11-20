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
	"net/http"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/testkit"
)

// runInProject invokes a command with a context that records project.
func runInProject(command []string, arguments []string, endpoint, project string) testkit.Outcome {
	return testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   command,
		Arguments: arguments,
		Context:   module.Context{Name: "wso2cloud", Endpoint: endpoint, Project: project},
		Access:    &testkit.Access{Token: "t"},
	})
}

func TestIntegrationListDefaultsToTheContextsProject(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{
		"/projects/orders/components":   {status: http.StatusOK, body: ordersIntegrations},
		"/projects/payments/components": {status: http.StatusOK, body: paymentsIntegrations},
	})

	outcome := runInProject([]string{"integration", "list"}, nil, stub.endpoint(), "orders")

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("integration list failed: %v %v", outcome.Err, outcome.Problem)
	}
	if strings.Join(stub.paths, ", ") != "GET "+basePath+"/projects/orders/components" {
		t.Errorf("calls = %v, want only the context's project", stub.paths)
	}
}

func TestAProjectFlagOverridesTheContextsProject(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{
		"/projects/orders/components":   {status: http.StatusOK, body: ordersIntegrations},
		"/projects/payments/components": {status: http.StatusOK, body: paymentsIntegrations},
	})

	outcome := runInProject([]string{"integration", "list"}, []string{"--project", "payments"}, stub.endpoint(), "orders")

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("integration list failed: %v %v", outcome.Err, outcome.Problem)
	}
	if strings.Join(stub.paths, ", ") != "GET "+basePath+"/projects/payments/components" {
		t.Errorf("calls = %v, want the project the flag names", stub.paths)
	}
}

func TestIntegrationCreateTakesItsProjectFromTheContext(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{
		"POST /projects/orders/components": {status: http.StatusCreated, body: `{"id":"order-api","projectId":"orders"}`},
	})

	outcome := runInProject([]string{"integration", "create"}, []string{"order-api", "--type", "integration-as-api",
		"--runtime", "wso2-integrator", "--repo", "https://github.com/acme/orders.git", "--branch", "main"},
		stub.endpoint(), "orders")

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("integration create failed: %v %v", outcome.Err, outcome.Problem)
	}
	owner := decodeBody(t, stub.body)["spec"].(map[string]any)["owner"].(map[string]any)
	if owner["projectName"] != "orders" {
		t.Errorf("the integration was owned by %v, want the context's project", owner["projectName"])
	}
}

// With a project on the context, the commands that find an integration's project
// do not search every project's listing for it.
func TestDeployWithTheContextsProjectSkipsTheSearch(t *testing.T) {
	stub := newPlatformStub(t, deploymentRoutes(map[string]stubRoute{
		"POST /components/order-api/deployments": {status: http.StatusOK, body: `{"message":"order-api-r4"}`},
	}))

	outcome := runInProject([]string{"integration", "deploy"}, []string{"order-api", "--env", "development"},
		stub.endpoint(), "orders")

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("integration deploy failed: %v %v", outcome.Err, outcome.Problem)
	}
	for _, call := range stub.paths {
		if call == "GET "+basePath+"/projects" {
			t.Errorf("the deploy searched every project (%v)", stub.paths)
		}
	}
	if stub.query != "projectName=orders" {
		t.Errorf("the deploy was sent with %q, want the context's project", stub.query)
	}
}

func TestStatusReportsTheContextsProject(t *testing.T) {
	outcome := runInProject([]string{"status"}, nil, "https://api.example"+basePath, "orders")

	if field(outcome, "project") != "orders" {
		t.Errorf("project = %q, want the context's", field(outcome, "project"))
	}
	if outcome.Result.Fields[0].Name != "namespace" {
		t.Errorf("the first field is %q, want namespace", outcome.Result.Fields[0].Name)
	}
	assertEndsWithNext(t, outcome)
}
