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
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/testkit"
)

const ordersProject = `{"id":"orders","handler":"orders","name":"Orders","description":"Order flows",
  "defaultDeploymentPipelineId":"default","gitProvider":"github","gitOrganization":"acme",
  "repository":"orders-integrations","branch":"main","directoryPath":"flows",
  "createdDate":"2026-08-01T12:00:00Z","deleting":false}`

// --- project show ---------------------------------------------------------

func TestProjectShowReportsTheProjectInDisplayOrder(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{"/projects/orders": {status: http.StatusOK, body: ordersProject}})

	outcome := run([]string{"project", "show"}, []string{"orders"}, stub.endpoint(), &testkit.Access{Token: "t"})

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("project show failed: %v %v", outcome.Err, outcome.Problem)
	}
	if outcome.Result.Schema != ProjectSchema {
		t.Errorf("schema = %q, want %q", outcome.Result.Schema, ProjectSchema)
	}
	if stub.method != http.MethodGet || stub.path != basePath+"/projects/orders" {
		t.Errorf("the API was called with %s %s", stub.method, stub.path)
	}
	want := []struct{ name, value string }{
		{"id", "orders"}, {"name", "Orders"}, {"description", "Order flows"}, {"pipeline", "default"},
		{"repository", "github: acme/orders-integrations"}, {"branch", "main"}, {"path", "flows"},
		{"created", "2026-08-01T12:00:00Z"}, {"state", "active"},
	}
	for i, w := range want {
		got := outcome.Result.Fields[i]
		if got.Name != w.name || got.Value != w.value {
			t.Errorf("field %d = %s=%q, want %s=%q", i, got.Name, got.Value, w.name, w.value)
		}
	}
	assertEndsWithNext(t, outcome)
}

func TestProjectShowEscapesTheProjectName(t *testing.T) {
	stub := newPlatformStub(t, nil)

	run([]string{"project", "show"}, []string{"a/b"}, stub.endpoint(), &testkit.Access{Token: "t"})

	if stub.path != basePath+"/projects/a%2Fb" {
		t.Errorf("the API was called at %s, want the name escaped as one path segment", stub.path)
	}
}

func TestProjectShowOfAnUnknownProjectNamesTheListing(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{
		"/projects/nope": {status: http.StatusNotFound, body: `{"error":"Not Found","message":"project nope not found"}`},
	})

	outcome := run([]string{"project", "show"}, []string{"nope"}, stub.endpoint(), &testkit.Access{Token: "t"})

	assertProblem(t, outcome, "intg.not_found", problem.CategoryProductService)
	if !strings.Contains(outcome.Problem.Message, "project nope not found") {
		t.Errorf("message = %q, want the API's words", outcome.Problem.Message)
	}
	if !strings.Contains(outcome.Problem.Recovery, "wso2 intg project list") {
		t.Errorf("recovery = %q, want it to name wso2 intg project list", outcome.Problem.Recovery)
	}
}

func TestProjectShowNeedsExactlyOneProject(t *testing.T) {
	for _, arguments := range [][]string{nil, {"a", "b"}} {
		outcome := run([]string{"project", "show"}, arguments, "https://unused.example", &testkit.Access{Token: "t"})

		assertProblem(t, outcome, "intg.invalid_argument", problem.CategoryUsage)
		if len(outcome.AccessRequests) != 0 {
			t.Errorf("access was requested for %v: %+v", arguments, outcome.AccessRequests)
		}
	}
}

// --- project create -------------------------------------------------------

func TestProjectCreateSendsOnlyWhatWasGiven(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{
		"POST /projects": {status: http.StatusCreated, body: `{"id":"orders","name":"orders","defaultDeploymentPipelineId":"default"}`},
	})

	outcome := run([]string{"project", "create"}, []string{"orders"}, stub.endpoint(), &testkit.Access{Token: "t"})

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("project create failed: %v %v", outcome.Err, outcome.Problem)
	}
	if stub.method != http.MethodPost || stub.path != basePath+"/projects" {
		t.Errorf("the API was called with %s %s", stub.method, stub.path)
	}
	sent := decodeBody(t, stub.body)
	if len(sent) != 1 || sent["name"] != "orders" {
		t.Errorf("sent %v, want only the name", sent)
	}
	if outcome.Result.Schema != ProjectSchema || field(outcome, "id") != "orders" {
		t.Errorf("result = %+v, want the created project", outcome.Result)
	}
	if next := field(outcome, NextField); !strings.Contains(next, "wso2 intg project show orders") {
		t.Errorf("next = %q, want it to name wso2 intg project show orders", next)
	}
}

func TestProjectCreateSendsEveryGivenFieldAndTheRepositoryLink(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{"POST /projects": {status: http.StatusCreated, body: ordersProject}})

	outcome := run([]string{"project", "create"}, []string{"orders",
		"--display-name", "Orders", "--description", "Order flows", "--pipeline", "default",
		"--repo", "https://github.com/acme/orders-integrations.git", "--branch", "main", "--path", "/flows/"},
		stub.endpoint(), &testkit.Access{Token: "t"})

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("project create failed: %v %v", outcome.Err, outcome.Problem)
	}
	sent := decodeBody(t, stub.body)
	want := map[string]any{
		"name": "orders", "displayName": "Orders", "description": "Order flows", "deploymentPipeline": "default",
		"gitProvider": "github", "gitOrganization": "acme", "repository": "orders-integrations",
		"branch": "main", "directoryPath": "flows",
	}
	for key, value := range want {
		if sent[key] != value {
			t.Errorf("sent %s = %v, want %v", key, sent[key], value)
		}
	}
	if len(sent) != len(want) {
		t.Errorf("sent %v, want exactly %v", sent, want)
	}
}

func TestProjectCreateReadsABitbucketRepository(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{"POST /projects": {status: http.StatusCreated, body: ordersProject}})

	run([]string{"project", "create"}, []string{"orders", "--repo", "bitbucket.org/acme/orders", "--branch", "dev"},
		stub.endpoint(), &testkit.Access{Token: "t"})

	sent := decodeBody(t, stub.body)
	if sent["gitProvider"] != "bitbucket" || sent["gitOrganization"] != "acme" || sent["repository"] != "orders" {
		t.Errorf("sent %v, want the bitbucket repository acme/orders", sent)
	}
}

// The API drops an incomplete repository link without saying so, so the
// command refuses one before anything is sent.
func TestProjectCreateRefusesAnIncompleteRepositoryLink(t *testing.T) {
	cases := map[string][]string{
		"repo without branch":   {"orders", "--repo", "https://github.com/acme/orders"},
		"branch without repo":   {"orders", "--branch", "main"},
		"path without repo":     {"orders", "--path", "flows"},
		"repo without a name":   {"orders", "--repo", "https://github.com/acme", "--branch", "main"},
		"unsupported host":      {"orders", "--repo", "https://gitlab.com/acme/orders", "--branch", "main"},
		"no project name":       nil,
		"two project names":     {"orders", "payments"},
		"repo with extra parts": {"orders", "--repo", "https://github.com/acme/orders/tree/main", "--branch", "main"},
	}
	for name, arguments := range cases {
		t.Run(name, func(t *testing.T) {
			stub := newPlatformStub(t, nil)

			outcome := run([]string{"project", "create"}, arguments, stub.endpoint(), &testkit.Access{Token: "t"})

			assertProblem(t, outcome, "intg.invalid_argument", problem.CategoryUsage)
			if stub.calls != 0 || len(outcome.AccessRequests) != 0 {
				t.Errorf("a refused command line reached the API (%d calls, %d access requests)",
					stub.calls, len(outcome.AccessRequests))
			}
		})
	}
}

func TestProjectCreateMapsItsOwnRefusals(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		code     string
		category problem.Category
		recovery string
	}{
		{"name taken by a different project", http.StatusConflict,
			`{"error":"Conflict","message":"project orders already exists"}`,
			"intg.project_exists", problem.CategoryProductService, "wso2 intg project show orders"},
		{"created but incomplete", http.StatusServiceUnavailable,
			`{"error":"Service Unavailable","message":"release bindings incomplete"}`,
			"intg.project_incomplete", problem.CategoryProductService, "wso2 intg project create orders"},
		{"plan limit", http.StatusPaymentRequired, `{"error":"Payment Required","message":"project limit reached"}`,
			"intg.plan_limit", problem.CategoryProductService, "subscription"},
		{"bad request", http.StatusBadRequest, `{"error":"Bad Request","message":"name is required"}`,
			"intg.call_failed", problem.CategoryUsage, "command line"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stub := newPlatformStub(t, map[string]stubRoute{"POST /projects": {status: c.status, body: c.body}})

			outcome := run([]string{"project", "create"}, []string{"orders"}, stub.endpoint(), &testkit.Access{Token: "t"})

			assertProblem(t, outcome, c.code, c.category)
			if !strings.Contains(outcome.Problem.Recovery, c.recovery) {
				t.Errorf("recovery = %q, want it to mention %q", outcome.Problem.Recovery, c.recovery)
			}
		})
	}
}

// --- project delete -------------------------------------------------------

func TestProjectDeleteReportsThatDeletionContinues(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{"DELETE /projects/orders": {status: http.StatusNoContent, body: ``}})

	outcome := run([]string{"project", "delete"}, []string{"orders", "--yes"}, stub.endpoint(), &testkit.Access{Token: "t"})

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("project delete failed: %v %v", outcome.Err, outcome.Problem)
	}
	if stub.method != http.MethodDelete || stub.path != basePath+"/projects/orders" {
		t.Errorf("the API was called with %s %s", stub.method, stub.path)
	}
	if outcome.Result.Schema != ProjectRemovalSchema || field(outcome, "project") != "orders" ||
		field(outcome, "state") != "deleting" {
		t.Errorf("result = %+v, want the project reported as deleting", outcome.Result)
	}
	if next := field(outcome, NextField); !strings.Contains(next, "wso2 intg project list") {
		t.Errorf("next = %q, want it to name wso2 intg project list", next)
	}
}

func TestProjectDeleteOfAnUnknownProjectNamesTheListing(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{
		"DELETE /projects/nope": {status: http.StatusNotFound, body: `{"error":"Not Found","message":"project nope not found"}`},
	})

	outcome := run([]string{"project", "delete"}, []string{"nope", "--yes"}, stub.endpoint(), &testkit.Access{Token: "t"})

	assertProblem(t, outcome, "intg.not_found", problem.CategoryProductService)
	if !strings.Contains(outcome.Problem.Recovery, "wso2 intg project list") {
		t.Errorf("recovery = %q, want it to name wso2 intg project list", outcome.Problem.Recovery)
	}
}

// decodeBody reads a JSON request body the stub recorded.
func decodeBody(t *testing.T, body []byte) map[string]any {
	t.Helper()
	sent := map[string]any{}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("the request body is not JSON: %v: %s", err, body)
	}
	return sent
}
