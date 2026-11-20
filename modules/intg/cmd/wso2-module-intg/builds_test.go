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
	"time"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/testkit"
)

const fullCommit = "0123456789abcdef0123456789abcdef01234567"

const orderAPIBuilds = `{"items":[
  {"name":"order-api-workflow-b2","status":"Running","trigger":"manual","commit":"` + fullCommit + `",
   "startedAt":"2026-10-27T10:05:00Z"},
  {"name":"order-api-workflow-b1","status":"Failed","trigger":"initial","commit":"fedcba9876543210fedcba9876543210fedcba98",
   "startedAt":"2026-10-27T09:00:00Z","completedAt":"2026-10-27T09:04:00Z"}]}`

const failedBuild = `{"name":"order-api-workflow-b1","status":"Failed","trigger":"initial",
  "componentName":"order-api","projectName":"orders","commit":"fedcba9876543210fedcba9876543210fedcba98",
  "image":"","startedAt":"2026-10-27T09:00:00Z","completedAt":"2026-10-27T09:04:00Z",
  "tasks":[{"name":"clone","phase":"Succeeded"},{"name":"build","phase":"Failed"}]}`

// --- build run ------------------------------------------------------------

func TestBuildRunBuildsTheBranchHeadWhenNoCommitIsGiven(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{
		"POST /components/order-api/builds": {status: http.StatusCreated, body: `{"message":"build triggered","success":true}`},
	})

	outcome := run([]string{"integration", "build", "run"}, []string{"order-api"}, stub.endpoint(), &testkit.Access{Token: "t"})

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("build run failed: %v %v", outcome.Err, outcome.Problem)
	}
	if stub.method != http.MethodPost || stub.path != basePath+"/components/order-api/builds" {
		t.Errorf("the API was called with %s %s", stub.method, stub.path)
	}
	if sent := decodeBody(t, stub.body); len(sent) != 0 {
		t.Errorf("sent %v, want an empty body, which builds the branch head", sent)
	}
	if outcome.Result.Schema != BuildStartSchema || field(outcome, "integration") != "order-api" ||
		field(outcome, "commit") != "branch head" {
		t.Errorf("result = %+v", outcome.Result.Fields)
	}
	if next := field(outcome, NextField); !strings.Contains(next, "wso2 intg integration build list order-api") {
		t.Errorf("next = %q, want it to name wso2 intg integration build list order-api", next)
	}
}

func TestBuildRunPinsAFullCommit(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{
		"POST /components/order-api/builds": {status: http.StatusCreated, body: `{"message":"build triggered","success":true}`},
	})

	outcome := run([]string{"integration", "build", "run"}, []string{"order-api", "--commit", strings.ToUpper(fullCommit)},
		stub.endpoint(), &testkit.Access{Token: "t"})

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("build run failed: %v %v", outcome.Err, outcome.Problem)
	}
	if sent := decodeBody(t, stub.body); sent["sha"] != fullCommit {
		t.Errorf("sent %v, want sha %s in lower case", sent, fullCommit)
	}
	if field(outcome, "commit") != fullCommit {
		t.Errorf("commit = %q", field(outcome, "commit"))
	}
}

// The build fetches the commit as a ref, which git allows only for a full
// SHA, so a short one is refused before a build is started that would fail.
func TestBuildRunRefusesAShortCommit(t *testing.T) {
	stub := newPlatformStub(t, nil)

	outcome := run([]string{"integration", "build", "run"}, []string{"order-api", "--commit", "0123456"}, stub.endpoint(),
		&testkit.Access{Token: "t"})

	assertProblem(t, outcome, "intg.invalid_argument", problem.CategoryUsage)
	if stub.calls != 0 || len(outcome.AccessRequests) != 0 {
		t.Errorf("a refused command line reached the API (%v)", stub.paths)
	}
}

func TestBuildRunMapsItsRefusals(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		code     string
		recovery string
	}{
		{"unknown integration", http.StatusNotFound, `{"error":"Not Found","message":"integration not found"}`,
			"intg.not_found", "wso2 intg integration list"},
		{"git integration down", http.StatusBadGateway,
			`{"error":"Bad Gateway","message":"git integration unavailable; retry the build"}`,
			"intg.call_failed", "wso2 intg integration build run order-api"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stub := newPlatformStub(t, map[string]stubRoute{"POST /components/order-api/builds": {status: c.status, body: c.body}})

			outcome := run([]string{"integration", "build", "run"}, []string{"order-api"}, stub.endpoint(), &testkit.Access{Token: "t"})

			assertProblem(t, outcome, c.code, problem.CategoryProductService)
			if !strings.Contains(outcome.Problem.Recovery, c.recovery) {
				t.Errorf("recovery = %q, want it to name %q", outcome.Problem.Recovery, c.recovery)
			}
		})
	}
}

// --- build list -----------------------------------------------------------

func TestBuildListReportsTheBuildsNewestFirst(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{"/components/order-api/builds": {status: http.StatusOK, body: orderAPIBuilds}})

	outcome := run([]string{"integration", "build", "list"}, []string{"order-api"}, stub.endpoint(), &testkit.Access{Token: "t"})

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("build list failed: %v %v", outcome.Err, outcome.Problem)
	}
	if outcome.Result.Schema != BuildsSchema || field(outcome, "count") != "2" ||
		field(outcome, "integration") != "order-api" {
		t.Errorf("result = %+v", outcome.Result.Fields)
	}
	assertColumns(t, outcome, "name", "status", "trigger", "commit", "started", "completed")
	assertRow(t, outcome, 0, "order-api-workflow-b2|Running|manual|0123456789ab|2026-10-27T10:05:00Z|-")
	assertRow(t, outcome, 1, "order-api-workflow-b1|Failed|initial|fedcba987654|2026-10-27T09:00:00Z|2026-10-27T09:04:00Z")
	assertEndsWithNext(t, outcome)
}

func TestAnEmptyBuildListNamesBuildRun(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{"/components/order-api/builds": {status: http.StatusOK, body: `{"items":[]}`}})

	outcome := run([]string{"integration", "build", "list"}, []string{"order-api"}, stub.endpoint(), &testkit.Access{Token: "t"})

	if next := field(outcome, NextField); !strings.Contains(next, "wso2 intg integration build run order-api") {
		t.Errorf("next = %q, want it to name wso2 intg integration build run order-api", next)
	}
}

// --- build show -----------------------------------------------------------

func TestBuildShowReportsAFailedBuildAndPointsAtItsLogs(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{
		"/components/order-api/builds/order-api-workflow-b1": {status: http.StatusOK, body: failedBuild},
	})

	outcome := run([]string{"integration", "build", "show"}, []string{"order-api", "order-api-workflow-b1"}, stub.endpoint(),
		&testkit.Access{Token: "t"})

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("build show failed: %v %v", outcome.Err, outcome.Problem)
	}
	if outcome.Result.Schema != BuildSchema {
		t.Errorf("schema = %q, want %q", outcome.Result.Schema, BuildSchema)
	}
	want := []struct{ name, value string }{
		{"name", "order-api-workflow-b1"}, {"integration", "order-api"}, {"project", "orders"}, {"status", "Failed"},
		{"trigger", "initial"}, {"commit", "fedcba9876543210fedcba9876543210fedcba98"}, {"image", "-"},
		{"started", "2026-10-27T09:00:00Z"}, {"completed", "2026-10-27T09:04:00Z"},
		{"steps", "clone: Succeeded\nbuild: Failed"},
	}
	for i, w := range want {
		got := outcome.Result.Fields[i]
		if got.Name != w.name || got.Value != w.value {
			t.Errorf("field %d = %s=%q, want %s=%q", i, got.Name, got.Value, w.name, w.value)
		}
	}
	if next := field(outcome, NextField); !strings.Contains(next, "wso2 intg integration build logs order-api order-api-workflow-b1") {
		t.Errorf("next = %q, want it to name the logs of this build", next)
	}
}

func TestBuildShowOfARunningBuildSaysToFollowIt(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{
		"/components/order-api/builds/b2": {status: http.StatusOK, body: `{"name":"b2","status":"Running","trigger":"manual"}`},
	})

	outcome := run([]string{"integration", "build", "show"}, []string{"order-api", "b2"}, stub.endpoint(), &testkit.Access{Token: "t"})

	if next := field(outcome, NextField); !strings.Contains(next, "wso2 intg integration build show order-api b2") {
		t.Errorf("next = %q, want it to name build show again", next)
	}
}

// The API answers an unknown build with a 500 rather than a 404, so that
// refusal also points at the listing.
func TestBuildShowOfAnUnknownBuildNamesTheListing(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{
		"/components/order-api/builds/nope": {status: http.StatusInternalServerError, body: `{"error":"Internal Server Error","message":"failed to get build status"}`},
	})

	outcome := run([]string{"integration", "build", "show"}, []string{"order-api", "nope"}, stub.endpoint(), &testkit.Access{Token: "t"})

	assertProblem(t, outcome, "intg.call_failed", problem.CategoryProductService)
	if !strings.Contains(outcome.Problem.Recovery, "wso2 intg integration build list order-api") {
		t.Errorf("recovery = %q, want it to name wso2 intg integration build list order-api", outcome.Problem.Recovery)
	}
}

func TestBuildShowNeedsAIntegrationAndABuild(t *testing.T) {
	for _, arguments := range [][]string{nil, {"order-api"}, {"a", "b", "c"}} {
		outcome := run([]string{"integration", "build", "show"}, arguments, "https://unused.example", &testkit.Access{Token: "t"})

		assertProblem(t, outcome, "intg.invalid_argument", problem.CategoryUsage)
	}
}

// --- build logs -----------------------------------------------------------

// obsPrefix is the path WSO2 Cloud's observability proxy serves its API
// under, part of the gateway URL a context records for the product.
const obsPrefix = "/wso2cloud-obs"

const buildLogEntries = `{"logs":[
  {"timestamp":"2026-10-27T09:00:01Z","log":"cloning","level":"INFO"},
  {"timestamp":"2026-10-27T09:01:00Z","log":"compiling","level":"INFO"},
  {"timestamp":"2026-10-27T09:04:00Z","log":"error: missing module","level":"ERROR"}],"total":3,"tookMs":12}`

// logsDeployment is the API, answering the builds b1 (09:00 to 09:04) and b2
// (still running) of order-api, beside the observability proxy, answering
// the log query with entries.
type logsDeployment struct {
	api, obs *platformStub
}

func newLogsDeployment(t *testing.T, status int, entries string) logsDeployment {
	return logsDeployment{
		api: newPlatformStub(t, map[string]stubRoute{
			"/components/order-api/builds/b1": {status: http.StatusOK, body: `{"name":"b1","status":"Failed","startedAt":"2026-10-27T09:00:00Z","completedAt":"2026-10-27T09:04:00Z"}`},
			"/components/order-api/builds/b2": {status: http.StatusOK, body: `{"name":"b2","status":"Running","startedAt":"2026-10-27T10:00:00Z"}`},
		}),
		obs: newStubAt(t, obsPrefix, map[string]stubRoute{"POST " + logsQueryPath: {status: status, body: entries}}),
	}
}

// logs runs build logs with the API as the product and the proxy as its
// gateway.
func (d logsDeployment) logs(arguments ...string) testkit.Outcome {
	return testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"integration", "build", "logs"},
		Arguments: arguments,
		Context:   module.Context{Name: "wso2cloud", Endpoint: d.api.endpoint(), GatewayEndpoint: d.obs.endpoint()},
		Access:    &testkit.Access{Token: "exchanged-token"},
	})
}

// The log is read from the observability proxy directly, the way the console
// reads it: a log query for the build's workflow run, at the gateway URL the
// context records, with the token the API is called with.
func TestBuildLogsQueriesTheProxyForTheBuildsWorkflowRun(t *testing.T) {
	d := newLogsDeployment(t, http.StatusOK, buildLogEntries)

	outcome := d.logs("order-api", "b1")

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("build logs failed: %v %v", outcome.Err, outcome.Problem)
	}
	if d.api.calls != 1 || d.api.path != basePath+"/components/order-api/builds/b1" {
		t.Errorf("API calls = %v, want only the build", d.api.paths)
	}
	if d.obs.method != http.MethodPost || d.obs.path != obsPrefix+logsQueryPath {
		t.Errorf("the proxy was called with %s %s, want POST %s", d.obs.method, d.obs.path, obsPrefix+logsQueryPath)
	}
	if d.obs.presented != "Bearer exchanged-token" {
		t.Errorf("the proxy was presented %q, want the brokered token", d.obs.presented)
	}
	if len(outcome.AccessRequests) != 1 || outcome.AccessRequests[0].Record != "" {
		t.Errorf("access requests = %+v, want one, for the product's own record", outcome.AccessRequests)
	}
	sent := decodeBody(t, d.obs.body)
	scope := sent["searchScope"].(map[string]any)
	if scope["workflowRunName"] != "b1" {
		t.Errorf("searchScope = %v, want the workflow run b1", scope)
	}
	if _, named := scope["namespace"]; !named {
		t.Errorf("searchScope = %v, want a namespace member for the proxy to fill in", scope)
	}
	if sent["startTime"] != "2026-10-27T08:55:00Z" || sent["endTime"] != "2026-10-27T09:09:00Z" {
		t.Errorf("window = %v to %v, want the build's run with five minutes either side", sent["startTime"], sent["endTime"])
	}
	if sent["sortOrder"] != "asc" || sent["limit"] != float64(1000) {
		t.Errorf("sortOrder %v limit %v, want asc and 1000", sent["sortOrder"], sent["limit"])
	}
}

func TestBuildLogsReportsTheWholeLog(t *testing.T) {
	d := newLogsDeployment(t, http.StatusOK, buildLogEntries)

	outcome := d.logs("order-api", "b1")

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("build logs failed: %v %v", outcome.Err, outcome.Problem)
	}
	if outcome.Result.Schema != BuildLogsSchema || field(outcome, "lines") != "3" {
		t.Errorf("result = %+v", outcome.Result.Fields)
	}
	want := "2026-10-27T09:00:01Z cloning\n2026-10-27T09:01:00Z compiling\n2026-10-27T09:04:00Z error: missing module"
	if log := field(outcome, "log"); log != want {
		t.Errorf("log = %q, want %q", log, want)
	}
	assertEndsWithNext(t, outcome)
}

// A tail asks for the newest lines first and puts them back in order.
func TestBuildLogsTailAsksForTheNewestLines(t *testing.T) {
	d := newLogsDeployment(t, http.StatusOK, `{"logs":[{"timestamp":"2026-10-27T09:04:00Z","log":"error: missing module"},
	  {"timestamp":"2026-10-27T09:01:00Z","log":"compiling"}],"total":3}`)

	outcome := d.logs("order-api", "b1", "--tail", "2")

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("build logs failed: %v %v", outcome.Err, outcome.Problem)
	}
	sent := decodeBody(t, d.obs.body)
	if sent["sortOrder"] != "desc" || sent["limit"] != float64(2) {
		t.Errorf("sortOrder %v limit %v, want desc and 2", sent["sortOrder"], sent["limit"])
	}
	if log := field(outcome, "log"); log != "2026-10-27T09:01:00Z compiling\n2026-10-27T09:04:00Z error: missing module" {
		t.Errorf("log = %q, want the two newest lines in order", log)
	}
	if field(outcome, "lines") != "2 of 3" {
		t.Errorf("lines = %q, want 2 of 3", field(outcome, "lines"))
	}
}

// A running build has no end yet, so the window runs to now.
func TestBuildLogsOfARunningBuildSearchesUpToNow(t *testing.T) {
	d := newLogsDeployment(t, http.StatusOK, `{"logs":[],"total":0}`)

	outcome := d.logs("order-api", "b2")

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("build logs failed: %v %v", outcome.Err, outcome.Problem)
	}
	end, err := time.Parse(time.RFC3339, decodeBody(t, d.obs.body)["endTime"].(string))
	if err != nil || time.Since(end) > time.Minute {
		t.Errorf("endTime = %v, want now", end)
	}
	if field(outcome, "lines") != "0" || field(outcome, "log") != "-" {
		t.Errorf("result = %+v, want no lines", outcome.Result.Fields)
	}
	if next := field(outcome, NextField); !strings.Contains(next, "wso2 intg integration build show order-api b2") {
		t.Errorf("next = %q, want it to name build show", next)
	}
}

func TestBuildLogsReportsARefusedQuery(t *testing.T) {
	d := newLogsDeployment(t, http.StatusBadGateway, `{"error":"Bad Gateway","message":"backend unavailable"}`)

	outcome := d.logs("order-api", "b1")

	assertProblem(t, outcome, "intg.call_failed", problem.CategoryProductService)
	if !strings.Contains(outcome.Problem.Message, "backend unavailable") {
		t.Errorf("message = %q, want the service's words", outcome.Problem.Message)
	}
}

// The proxy's URL is part of the context, as the product's gateway, so a
// context that records none is told how to add it before anything is called.
func TestBuildLogsWithoutAGatewayNamesHowToRecordOne(t *testing.T) {
	stub := newPlatformStub(t, nil)

	outcome := run([]string{"integration", "build", "logs"}, []string{"order-api", "b1"}, stub.endpoint(), &testkit.Access{Token: "t"})

	assertProblem(t, outcome, "intg.observability_not_recorded", problem.CategoryUsage)
	for _, want := range []string{"wso2 context product add intg", "--gateway"} {
		if !strings.Contains(outcome.Problem.Recovery, want) {
			t.Errorf("recovery = %q, want it to name %q", outcome.Problem.Recovery, want)
		}
	}
	if stub.calls != 0 || len(outcome.AccessRequests) != 0 {
		t.Errorf("a command with nowhere to read logs from reached the API (%v)", stub.paths)
	}
}

func TestBuildLogsRefusesANegativeTail(t *testing.T) {
	outcome := run([]string{"integration", "build", "logs"}, []string{"order-api", "b1", "--tail", "-3"}, "https://unused.example",
		&testkit.Access{Token: "t"})

	assertProblem(t, outcome, "intg.invalid_argument", problem.CategoryUsage)
}
