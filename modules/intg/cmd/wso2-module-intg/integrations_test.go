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
	"net/http"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/testkit"
)

const ordersIntegrations = `{"items":[
  {"projectId":"orders","id":"order-api","handler":"order-api","name":"Order API","displayName":"Order API",
   "description":"Takes orders","componentType":"service","buildpackType":"BI","status":"Ready",
   "lastBuildDate":"2026-10-01T10:00:00Z","createdAt":"2026-09-01T10:00:00Z","deleting":false},
  {"projectId":"orders","id":"nightly-sync","handler":"nightly-sync","name":"Nightly Sync",
   "componentType":"automation","buildpackType":"MI","status":"BuildFailed",
   "lastBuildDate":"2026-10-02T10:00:00Z","createdAt":"2026-09-02T10:00:00Z","deleting":true}]}`

const paymentsIntegrations = `{"items":[
  {"projectId":"payments","id":"refunds","handler":"refunds","name":"Refunds","componentType":"eventIntegration",
   "buildpackType":"other","status":"Ready","lastBuildDate":"","createdAt":"2026-09-03T10:00:00Z"}]}`

const twoProjectIDs = `{"items":[{"id":"orders","name":"Orders"},{"id":"payments","name":"Payments"}]}`

const orderAPIRepository = `{"gitProvider":"github","organizationApp":"acme","nameApp":"orders-integrations",
  "branch":"main","appSubPath":"/order-api","serverUrl":"https://github.com/acme/orders-integrations.git"}`

// --- integration list -------------------------------------------------------

func TestIntegrationListReportsAProjectsIntegrationsInCreateTerms(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{"/projects/orders/components": {status: http.StatusOK, body: ordersIntegrations}})

	outcome := run([]string{"integration", "list"}, []string{"--project", "orders"}, stub.endpoint(),
		&testkit.Access{Token: "t"})

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("integration list failed: %v %v", outcome.Err, outcome.Problem)
	}
	if outcome.Result.Schema != IntegrationsSchema {
		t.Errorf("schema = %q, want %q", outcome.Result.Schema, IntegrationsSchema)
	}
	if stub.calls != 1 || stub.path != basePath+"/projects/orders/components" {
		t.Errorf("calls = %v, want one GET of the project's integrations", stub.paths)
	}
	assertColumns(t, outcome, "name", "id", "project", "type", "runtime", "status", "updated", "state")
	if field(outcome, "count") != "2" {
		t.Errorf("count = %q, want 2", field(outcome, "count"))
	}
	// The type and runtime read in the words integration create takes, not the
	// API's own: one word per concept across the command surface.
	assertRow(t, outcome, 0, "Order API|order-api|orders|integration-as-api|wso2-integrator|Ready|2026-10-01T10:00:00Z|active")
	assertRow(t, outcome, 1, "Nightly Sync|nightly-sync|orders|automation|wso2-integrator-mi|BuildFailed|2026-10-02T10:00:00Z|deleting")
	assertEndsWithNext(t, outcome)
}

func TestIntegrationListWithoutAProjectReadsEveryProject(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{
		"/projects":                     {status: http.StatusOK, body: twoProjectIDs},
		"/projects/orders/components":   {status: http.StatusOK, body: ordersIntegrations},
		"/projects/payments/components": {status: http.StatusOK, body: paymentsIntegrations},
	})

	outcome := run([]string{"integration", "list"}, nil, stub.endpoint(), &testkit.Access{Token: "t"})

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("integration list failed: %v %v", outcome.Err, outcome.Problem)
	}
	if field(outcome, "count") != "3" || len(outcome.Result.Rows) != 3 {
		t.Fatalf("count = %q with %d rows, want 3 across both projects", field(outcome, "count"), len(outcome.Result.Rows))
	}
	assertRow(t, outcome, 2, "Refunds|refunds|payments|event-integration|other|Ready|-|active")
	if len(outcome.AccessRequests) != 1 {
		t.Errorf("access was requested %d times, want once for the whole listing", len(outcome.AccessRequests))
	}
}

func TestAnEmptyIntegrationListNamesIntegrationCreate(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{"/projects/orders/components": {status: http.StatusOK, body: `{"items":[]}`}})

	outcome := run([]string{"integration", "list"}, []string{"--project", "orders"}, stub.endpoint(),
		&testkit.Access{Token: "t"})

	if next := field(outcome, NextField); !strings.Contains(next, "wso2 intg integration create") {
		t.Errorf("next = %q, want it to name wso2 intg integration create", next)
	}
}

// --- integration show -------------------------------------------------------

func TestIntegrationShowFindsTheIntegrationInItsProjectAndReadsItsRepository(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{
		"/projects/orders/components":      {status: http.StatusOK, body: ordersIntegrations},
		"/components/order-api/repository": {status: http.StatusOK, body: orderAPIRepository},
	})

	outcome := run([]string{"integration", "show"}, []string{"order-api", "--project", "orders"}, stub.endpoint(),
		&testkit.Access{Token: "t"})

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("integration show failed: %v %v", outcome.Err, outcome.Problem)
	}
	if outcome.Result.Schema != IntegrationSchema {
		t.Errorf("schema = %q, want %q", outcome.Result.Schema, IntegrationSchema)
	}
	want := []struct{ name, value string }{
		{"id", "order-api"}, {"name", "Order API"}, {"project", "orders"}, {"type", "integration-as-api"},
		{"runtime", "wso2-integrator"}, {"description", "Takes orders"}, {"status", "Ready"},
		{"repository", "github: acme/orders-integrations"}, {"branch", "main"}, {"path", "order-api"},
		{"updated", "2026-10-01T10:00:00Z"}, {"created", "2026-09-01T10:00:00Z"}, {"state", "active"},
	}
	for i, w := range want {
		got := outcome.Result.Fields[i]
		if got.Name != w.name || got.Value != w.value {
			t.Errorf("field %d = %s=%q, want %s=%q", i, got.Name, got.Value, w.name, w.value)
		}
	}
	if stub.query != "projectName=orders" {
		t.Errorf("the repository was read with query %q, want projectName=orders", stub.query)
	}
	assertEndsWithNext(t, outcome)
}

func TestIntegrationShowMatchesADisplayNameWhenNoIDMatches(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{
		"/projects/orders/components":         {status: http.StatusOK, body: ordersIntegrations},
		"/components/nightly-sync/repository": {status: http.StatusOK, body: `{}`},
	})

	outcome := run([]string{"integration", "show"}, []string{"Nightly Sync", "--project", "orders"}, stub.endpoint(),
		&testkit.Access{Token: "t"})

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("integration show failed: %v %v", outcome.Err, outcome.Problem)
	}
	if field(outcome, "id") != "nightly-sync" || field(outcome, "repository") != "-" {
		t.Errorf("result = %+v, want nightly-sync with no repository", outcome.Result.Fields)
	}
}

func TestIntegrationShowWithoutAProjectSearchesEveryProject(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{
		"/projects":                      {status: http.StatusOK, body: twoProjectIDs},
		"/projects/orders/components":    {status: http.StatusOK, body: ordersIntegrations},
		"/projects/payments/components":  {status: http.StatusOK, body: paymentsIntegrations},
		"/components/refunds/repository": {status: http.StatusOK, body: `{}`},
	})

	outcome := run([]string{"integration", "show"}, []string{"refunds"}, stub.endpoint(), &testkit.Access{Token: "t"})

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("integration show failed: %v %v", outcome.Err, outcome.Problem)
	}
	if field(outcome, "project") != "payments" {
		t.Errorf("project = %q, want payments", field(outcome, "project"))
	}
}

func TestIntegrationShowOfAnUnknownIntegrationNamesTheListing(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{"/projects/orders/components": {status: http.StatusOK, body: ordersIntegrations}})

	outcome := run([]string{"integration", "show"}, []string{"nope", "--project", "orders"}, stub.endpoint(),
		&testkit.Access{Token: "t"})

	assertProblem(t, outcome, "intg.not_found", problem.CategoryProductService)
	if !strings.Contains(outcome.Problem.Recovery, "wso2 intg integration list --project orders") {
		t.Errorf("recovery = %q, want it to name wso2 intg integration list --project orders", outcome.Problem.Recovery)
	}
}

func TestIntegrationShowRefusesADisplayNameThatMatchesTwoIntegrations(t *testing.T) {
	twins := `{"items":[{"projectId":"orders","id":"sync-a","name":"Sync"},{"projectId":"orders","id":"sync-b","name":"Sync"}]}`
	stub := newPlatformStub(t, map[string]stubRoute{"/projects/orders/components": {status: http.StatusOK, body: twins}})

	outcome := run([]string{"integration", "show"}, []string{"Sync", "--project", "orders"}, stub.endpoint(),
		&testkit.Access{Token: "t"})

	assertProblem(t, outcome, "intg.invalid_argument", problem.CategoryUsage)
	for _, id := range []string{"sync-a", "sync-b"} {
		if !strings.Contains(outcome.Problem.Message, id) {
			t.Errorf("message = %q, want it to name %s", outcome.Problem.Message, id)
		}
	}
}

// --- integration create -----------------------------------------------------

func createArguments(extra ...string) []string {
	return append([]string{"order-api", "--project", "orders", "--type", "integration-as-api",
		"--runtime", "wso2-integrator", "--repo", "https://github.com/acme/orders-integrations.git",
		"--branch", "main"}, extra...)
}

func TestIntegrationCreateSendsTheBodyTheMCPToolSends(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{"POST /projects/orders/components": {status: http.StatusCreated, body: `{"projectId":"orders","id":"order-api","name":"Order API","componentType":"service","buildpackType":"BI"}`}})

	outcome := run([]string{"integration", "create"},
		createArguments("--path", "/order-api", "--display-name", "Order API", "--description", "Takes orders",
			"--auto-deploy"), stub.endpoint(), &testkit.Access{Token: "t"})

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("integration create failed: %v %v", outcome.Err, outcome.Problem)
	}
	if stub.method != http.MethodPost || stub.path != basePath+"/projects/orders/components" {
		t.Errorf("the API was called with %s %s", stub.method, stub.path)
	}
	sent := decodeBody(t, stub.body)
	metadata := sent["metadata"].(map[string]any)
	annotations := metadata["annotations"].(map[string]any)
	spec := sent["spec"].(map[string]any)
	workflow := spec["workflow"].(map[string]any)
	repository := workflow["parameters"].(map[string]any)["repository"].(map[string]any)
	checks := map[string][2]any{
		"metadata.name":           {metadata["name"], "order-api"},
		"display-name annotation": {annotations["openchoreo.dev/display-name"], "Order API"},
		"description annotation":  {annotations["openchoreo.dev/description"], "Takes orders"},
		"spec.owner.projectName":  {spec["owner"].(map[string]any)["projectName"], "orders"},
		"spec.componentType.name": {spec["componentType"].(map[string]any)["name"], "deployment/integration-as-api"},
		"spec.autoBuild":          {spec["autoBuild"], true},
		"spec.autoDeploy":         {spec["autoDeploy"], true},
		"workflow.kind":           {workflow["kind"], "ClusterWorkflow"},
		"workflow.name":           {workflow["name"], "ballerina-buildpack-builder"},
		"repository.url":          {repository["url"], "https://github.com/acme/orders-integrations.git"},
		"repository.revision":     {repository["revision"].(map[string]any)["branch"], "main"},
		"repository.appPath":      {repository["appPath"], "/order-api"},
	}
	for name, pair := range checks {
		if pair[0] != pair[1] {
			t.Errorf("%s = %v, want %v", name, pair[0], pair[1])
		}
	}
	if outcome.Result.Schema != CreatedIntegrationSchema || field(outcome, "id") != "order-api" {
		t.Errorf("result = %+v, want the created integration", outcome.Result)
	}
	assertEndsWithNext(t, outcome)
}

func TestIntegrationCreateMapsEveryTypeAndRuntime(t *testing.T) {
	types := map[string]string{
		"integration-as-api": "deployment/integration-as-api", "automation": "cronjob/scheduled-task",
		"event-integration": "deployment/event-integration", "file-integration": "deployment/file-integration",
		"mcp-server": "deployment/mcp-server",
	}
	runtimes := map[string]string{
		"wso2-integrator": "ballerina-buildpack-builder", "wso2-integrator-mi": "mi-buildpack-builder",
		"dockerfile": "dockerfile-builder",
	}
	for kind, integrationType := range types {
		for runtime, workflow := range runtimes {
			stub := newPlatformStub(t, map[string]stubRoute{"POST /projects/orders/components": {status: http.StatusCreated, body: `{"id":"x"}`}})
			run([]string{"integration", "create"}, []string{"x", "--project", "orders", "--type", kind, "--runtime", runtime,
				"--repo", "https://github.com/acme/x", "--branch", "main"}, stub.endpoint(), &testkit.Access{Token: "t"})
			spec := decodeBody(t, stub.body)["spec"].(map[string]any)
			if got := spec["componentType"].(map[string]any)["name"]; got != integrationType {
				t.Errorf("--type %s sent %v, want %s", kind, got, integrationType)
			}
			if got := spec["workflow"].(map[string]any)["name"]; got != workflow {
				t.Errorf("--runtime %s sent %v, want %s", runtime, got, workflow)
			}
		}
	}
}

// The platform suffixes a taken name rather than refusing it, so the result
// reports the ID it actually created and says why it differs.
func TestIntegrationCreateReportsASuffixedName(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{"POST /projects/orders/components": {status: http.StatusCreated, body: `{"projectId":"orders","id":"order-api-x7k2","name":"order-api","componentType":"service","buildpackType":"BI",
		  "warning":"integration created, but the first build could not be started"}`}})

	outcome := run([]string{"integration", "create"}, createArguments(), stub.endpoint(), &testkit.Access{Token: "t"})

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("integration create failed: %v %v", outcome.Err, outcome.Problem)
	}
	if field(outcome, "id") != "order-api-x7k2" {
		t.Errorf("id = %q, want the suffixed one", field(outcome, "id"))
	}
	if got := field(outcome, "warning"); !strings.Contains(got, "first build could not be started") {
		t.Errorf("warning = %q, want the platform's warning", got)
	}
	next := field(outcome, NextField)
	for _, want := range []string{"order-api was taken", "order-api-x7k2"} {
		if !strings.Contains(next, want) {
			t.Errorf("next = %q, want it to mention %q", next, want)
		}
	}
}

func TestIntegrationCreateRefusesAnIncompleteCommandLine(t *testing.T) {
	base := []string{"--project", "orders", "--type", "automation", "--runtime", "dockerfile",
		"--repo", "https://github.com/acme/x", "--branch", "main"}
	without := func(flag string) []string {
		out := []string{"x"}
		for i := 0; i < len(base); i += 2 {
			if base[i] != flag {
				out = append(out, base[i], base[i+1])
			}
		}
		return out
	}
	cases := map[string][]string{
		"no name":         base,
		"no project":      without("--project"),
		"no type":         without("--type"),
		"no runtime":      without("--runtime"),
		"no repo":         without("--repo"),
		"no branch":       without("--branch"),
		"unknown type":    append([]string{"x"}, append(without("--type")[1:], "--type", "service")...),
		"unknown runtime": append([]string{"x"}, append(without("--runtime")[1:], "--runtime", "java")...),
		"plain http repo": append([]string{"x"}, append(without("--repo")[1:], "--repo", "http://github.com/acme/x")...),
		"uppercase name":  append([]string{"Order_API"}, base...),
	}
	for name, arguments := range cases {
		t.Run(name, func(t *testing.T) {
			stub := newPlatformStub(t, nil)

			outcome := run([]string{"integration", "create"}, arguments, stub.endpoint(), &testkit.Access{Token: "t"})

			code := "intg.invalid_argument"
			if strings.HasPrefix(name, "no ") && name != "no name" && name != "no project" {
				code = "module.flag_required"
			}
			assertProblem(t, outcome, code, problem.CategoryUsage)
			if stub.calls != 0 || len(outcome.AccessRequests) != 0 {
				t.Errorf("a refused command line reached the API (%v)", stub.paths)
			}
		})
	}
}

func TestIntegrationCreateMapsItsOwnRefusals(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		code     string
		category problem.Category
	}{
		{"every suffixed name taken", http.StatusConflict, "intg.integration_exists", problem.CategoryProductService},
		{"plan limit", http.StatusPaymentRequired, "intg.plan_limit", problem.CategoryProductService},
		{"bad request", http.StatusBadRequest, "intg.call_failed", problem.CategoryUsage},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stub := newPlatformStub(t, map[string]stubRoute{"POST /projects/orders/components": {status: c.status, body: `{"error":"refused","message":"the platform's words"}`}})

			outcome := run([]string{"integration", "create"}, createArguments(), stub.endpoint(), &testkit.Access{Token: "t"})

			assertProblem(t, outcome, c.code, c.category)
			if !strings.Contains(outcome.Problem.Message, "the platform's words") {
				t.Errorf("message = %q, want the platform's words", outcome.Problem.Message)
			}
		})
	}
}

// --- integration delete -----------------------------------------------------

func TestIntegrationDeleteNamesTheProjectOnlyWhenGiven(t *testing.T) {
	for _, c := range []struct {
		arguments []string
		query     string
	}{
		{[]string{"order-api", "--yes", "--project", "orders"}, "projectName=orders"},
		{[]string{"order-api", "--yes"}, ""},
	} {
		stub := newPlatformStub(t, map[string]stubRoute{"DELETE /components/order-api": {status: http.StatusNoContent, body: ``}})

		outcome := run([]string{"integration", "delete"}, c.arguments, stub.endpoint(), &testkit.Access{Token: "t"})

		if outcome.Err != nil || outcome.Problem != nil {
			t.Fatalf("integration delete %v failed: %v %v", c.arguments, outcome.Err, outcome.Problem)
		}
		if stub.method != http.MethodDelete || stub.path != basePath+"/components/order-api" || stub.query != c.query {
			t.Errorf("%v called %s %s?%s, want query %q", c.arguments, stub.method, stub.path, stub.query, c.query)
		}
		if outcome.Result.Schema != IntegrationRemovalSchema || field(outcome, "integration") != "order-api" ||
			field(outcome, "state") != "deleting" {
			t.Errorf("result = %+v, want the integration reported as deleting", outcome.Result)
		}
	}
}

func TestIntegrationDeleteOfAnUnknownIntegrationNamesTheListing(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{
		"DELETE /components/nope": {status: http.StatusNotFound, body: `{"error":"Not Found","message":"integration nope not found"}`},
	})

	outcome := run([]string{"integration", "delete"}, []string{"nope", "--yes"}, stub.endpoint(), &testkit.Access{Token: "t"})

	assertProblem(t, outcome, "intg.not_found", problem.CategoryProductService)
	if !strings.Contains(outcome.Problem.Recovery, "wso2 intg integration list") {
		t.Errorf("recovery = %q, want it to name wso2 intg integration list", outcome.Problem.Recovery)
	}
}

// assertColumns checks a listing's columns, in order.
func assertColumns(t *testing.T, outcome testkit.Outcome, names ...string) {
	t.Helper()
	if len(outcome.Result.Columns) != len(names) {
		t.Fatalf("columns = %+v, want %v", outcome.Result.Columns, names)
	}
	for i, name := range names {
		if outcome.Result.Columns[i].Name != name {
			t.Errorf("column %d = %q, want %q", i, outcome.Result.Columns[i].Name, name)
		}
	}
}

// assertRow checks one listing row, its values joined with "|".
func assertRow(t *testing.T, outcome testkit.Outcome, index int, want string) {
	t.Helper()
	if index >= len(outcome.Result.Rows) {
		t.Fatalf("no row %d in %+v", index, outcome.Result.Rows)
	}
	if got := strings.Join(outcome.Result.Rows[index].Values, "|"); got != want {
		t.Errorf("row %d = %q, want %q", index, got, want)
	}
}
