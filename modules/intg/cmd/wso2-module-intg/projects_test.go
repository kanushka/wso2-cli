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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/testkit"
)

// basePath is the path the Integration Platform API is served under on WSO2
// Cloud. The stub serves it too, so a client that dropped the base path from
// the recorded URL would reach nothing.
const basePath = "/ipaas-service-ipaas-api-endpoint"

// stubRoute is one canned answer: the status and the body. A route with
// then answers its own status and body first, then each of then in turn,
// repeating the last, for a resource that changes while a command polls it.
type stubRoute struct {
	status int
	body   string
	then   []stubRoute
}

// platformStub stands in for the Integration Platform API, recording the
// method, path, body and token each call reached it with.
type platformStub struct {
	*httptest.Server
	prefix    string
	method    string
	path      string
	query     string
	paths     []string
	body      []byte
	presented string
	calls     int
}

// newPlatformStub answers each route under basePath with its canned status and
// body, and anything else with the 404 the API itself would give. A route is
// keyed by its path alone, by "METHOD /path" where one path answers more than
// one method, or by "METHOD /path?query" where one path answers more than one
// query; the most specific key wins.
func newPlatformStub(t *testing.T, routes map[string]stubRoute) *platformStub {
	t.Helper()
	return newStubAt(t, basePath, routes)
}

// newStubAt is newPlatformStub for a service served under prefix, such as the
// observability proxy beside the API.
func newStubAt(t *testing.T, prefix string, routes map[string]stubRoute) *platformStub {
	t.Helper()
	stub := &platformStub{prefix: prefix}
	served := map[string]int{}
	stub.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.calls++
		stub.method, stub.path, stub.query = r.Method, r.URL.EscapedPath(), r.URL.RawQuery
		stub.paths = append(stub.paths, r.Method+" "+r.URL.EscapedPath())
		stub.body, _ = io.ReadAll(r.Body)
		stub.presented = r.Header.Get("Authorization")
		relative := strings.TrimPrefix(r.URL.EscapedPath(), prefix)
		route, found := routes[r.Method+" "+relative+"?"+r.URL.RawQuery]
		if !found {
			route, found = routes[r.Method+" "+relative]
		}
		if !found {
			route, found = routes[relative]
		}
		if !found || !strings.HasPrefix(r.URL.Path, prefix) {
			route = stubRoute{status: http.StatusNotFound, body: `{"error":"Not Found","message":"no such route"}`}
		}
		if len(route.then) > 0 {
			key := r.Method + " " + r.URL.String()
			answers := append([]stubRoute{{status: route.status, body: route.body}}, route.then...)
			route = answers[min(served[key], len(answers)-1)]
			served[key]++
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(route.status)
		_, _ = w.Write([]byte(route.body))
	}))
	t.Cleanup(stub.Close)
	return stub
}

// endpoint is the product URL a context records for the stub: the API's base
// path, the way the WSO2 Cloud URL carries it.
func (s *platformStub) endpoint() string { return s.URL + s.prefix }

// run invokes one command of this module through the contract.
func run(command []string, arguments []string, endpoint string, access *testkit.Access) testkit.Outcome {
	return testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   command,
		Arguments: arguments,
		Context:   module.Context{Name: "wso2cloud", Endpoint: endpoint},
		Access:    access,
	})
}

const twoProjects = `{"items":[
  {"id":"orders","handler":"orders","name":"Orders","defaultDeploymentPipelineId":"default",
   "gitOrganization":"acme","repository":"orders-integrations"},
  {"id":"default-project","handler":"default-project","name":"Default Project",
   "defaultDeploymentPipelineId":"default","gitOrganization":"","repository":"","deleting":true}]}`

func TestProjectListReportsARowPerProject(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{"/projects": {status: http.StatusOK, body: twoProjects}})

	outcome := run([]string{"project", "list"}, nil, stub.endpoint(), &testkit.Access{Token: "exchanged-token"})

	if outcome.Err != nil {
		t.Fatalf("the invocation failed: %v", outcome.Err)
	}
	if outcome.Problem != nil {
		t.Fatalf("project list returned the problem %v", outcome.Problem)
	}
	if outcome.Result.Schema != ProjectsSchema {
		t.Errorf("schema = %q, want %q", outcome.Result.Schema, ProjectsSchema)
	}
	if stub.method != http.MethodGet || stub.path != basePath+"/projects" {
		t.Errorf("the API was called with %s %s, want GET %s/projects", stub.method, stub.path, basePath)
	}
	if stub.presented != "Bearer exchanged-token" {
		t.Errorf("the API was presented %q, want the brokered token", stub.presented)
	}
	if got := field(outcome, "count"); got != "2" {
		t.Errorf("count = %q, want 2", got)
	}
	wantColumns := []string{"name", "id", "pipeline", "repository", "state"}
	if len(outcome.Result.Columns) != len(wantColumns) {
		t.Fatalf("columns = %+v, want %v", outcome.Result.Columns, wantColumns)
	}
	for i, name := range wantColumns {
		if outcome.Result.Columns[i].Name != name {
			t.Errorf("column %d = %q, want %q", i, outcome.Result.Columns[i].Name, name)
		}
	}
	if len(outcome.Result.Rows) != 2 {
		t.Fatalf("rows = %+v, want two", outcome.Result.Rows)
	}
	if got := strings.Join(outcome.Result.Rows[0].Values, "|"); got != "Orders|orders|default|acme/orders-integrations|active" {
		t.Errorf("first row = %q", got)
	}
	// A project with no linked repository says so rather than showing a lone slash.
	if got := outcome.Result.Rows[1].Values[3]; got != "-" {
		t.Errorf("an unlinked project's repository = %q, want -", got)
	}
	// A project whose delete was accepted keeps appearing until it completes.
	if got := outcome.Result.Rows[1].Values[4]; got != "deleting" {
		t.Errorf("a project being deleted has state %q, want deleting", got)
	}
	assertEndsWithNext(t, outcome)
}

func TestProjectListAsksForTheIntegrationAudienceAndNoScopes(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{"/projects": {status: http.StatusOK, body: `{"items":[]}`}})

	outcome := run([]string{"project", "list"}, nil, stub.endpoint(), &testkit.Access{Token: "t"})

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("project list failed: %v %v", outcome.Err, outcome.Problem)
	}
	if len(outcome.AccessRequests) != 1 {
		t.Fatalf("access requests = %+v, want exactly one", outcome.AccessRequests)
	}
	asked := outcome.AccessRequests[0]
	if asked.Audience != IntegrationAudience || len(asked.Scopes) != 0 || asked.Record != "" {
		t.Errorf("asked for %+v, want the %q audience with no scopes on the product's own record",
			asked, IntegrationAudience)
	}
}

func TestAnEmptyProjectListSaysSo(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{"/projects": {status: http.StatusOK, body: `{"items":[]}`}})

	outcome := run([]string{"project", "list"}, nil, stub.endpoint(), &testkit.Access{Token: "t"})

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("project list failed: %v %v", outcome.Err, outcome.Problem)
	}
	if got := field(outcome, "count"); got != "0" {
		t.Errorf("count = %q, want 0", got)
	}
	if next := field(outcome, NextField); !strings.Contains(next, "no projects") {
		t.Errorf("an empty listing's next step = %q, want it to say there are no projects", next)
	}
}

func TestProjectListWithoutARecordedEndpointAsksForNoAccess(t *testing.T) {
	outcome := run([]string{"project", "list"}, nil, "", &testkit.Access{Token: "t"})

	assertProblem(t, outcome, "intg.product_not_recorded", problem.CategoryUsage)
	if !strings.Contains(outcome.Problem.Recovery, "wso2 context product add intg --url") {
		t.Errorf("recovery = %q, want it to name wso2 context product add intg", outcome.Problem.Recovery)
	}
	if len(outcome.AccessRequests) != 0 {
		t.Errorf("access was requested with nowhere to call: %+v", outcome.AccessRequests)
	}
}

func TestProjectListPassesTheShellsDenialThroughUnchanged(t *testing.T) {
	stub := newPlatformStub(t, map[string]stubRoute{"/projects": {status: http.StatusOK, body: twoProjects}})
	denial := problem.New(problem.CategoryAuthPolicy, "auth.session_required", "no session").
		WithRecovery("Run wso2 login.")

	outcome := run([]string{"project", "list"}, nil, stub.endpoint(), &testkit.Access{Deny: &denial})

	assertProblem(t, outcome, "auth.session_required", problem.CategoryAuthPolicy)
	if stub.calls != 0 {
		t.Errorf("the API was called %d times without access", stub.calls)
	}
}

func TestProjectListMapsEachRefusalToItsOwnProblem(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		code     string
		category problem.Category
		words    string
	}{
		{"unauthorized", http.StatusUnauthorized,
			`{"error":"Unauthorized","message":"authentication failed: 'ouHandle' claim not found in JWT"}`,
			"intg.not_authorized", problem.CategoryAuthPolicy, "'ouHandle' claim not found"},
		{"forbidden", http.StatusForbidden, `{"error":"Forbidden","message":"insufficient permissions"}`,
			"intg.not_authorized", problem.CategoryAuthPolicy, "insufficient permissions"},
		{"payment required", http.StatusPaymentRequired, `{"error":"Payment Required","message":"plan limit reached"}`,
			"intg.plan_limit", problem.CategoryProductService, "plan limit reached"},
		{"not found", http.StatusNotFound, `{"error":"Not Found","message":"no such route"}`,
			"intg.not_found", problem.CategoryProductService, "no such route"},
		{"server error", http.StatusInternalServerError, `{"error":"Internal Server Error","message":"openchoreo unavailable"}`,
			"intg.call_failed", problem.CategoryProductService, "openchoreo unavailable"},
		{"no body", http.StatusBadGateway, ``,
			"intg.call_failed", problem.CategoryProductService, "502"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stub := newPlatformStub(t, map[string]stubRoute{"/projects": {status: c.status, body: c.body}})

			outcome := run([]string{"project", "list"}, nil, stub.endpoint(), &testkit.Access{Token: "t"})

			assertProblem(t, outcome, c.code, c.category)
			if !strings.Contains(outcome.Problem.Message, c.words) {
				t.Errorf("message = %q, want the API's own words %q", outcome.Problem.Message, c.words)
			}
			if outcome.Problem.Recovery == "" {
				t.Error("the problem names no recovery")
			}
		})
	}
}

func TestProjectListReportsAnUnreachableAPI(t *testing.T) {
	stub := newPlatformStub(t, nil)
	endpoint := stub.endpoint()
	stub.Close()

	outcome := run([]string{"project", "list"}, nil, endpoint, &testkit.Access{Token: "t"})

	assertProblem(t, outcome, "intg.deployment_unreachable", problem.CategoryProductService)
	if !strings.Contains(outcome.Problem.Message, endpoint) {
		t.Errorf("message = %q, want it to name %s", outcome.Problem.Message, endpoint)
	}
}

func TestProjectListRefusesAnArgument(t *testing.T) {
	outcome := run([]string{"project", "list"}, []string{"orders"}, "https://unused.example", &testkit.Access{Token: "t"})

	assertProblem(t, outcome, "intg.invalid_argument", problem.CategoryUsage)
	if len(outcome.AccessRequests) != 0 {
		t.Errorf("access was requested for a command line that was refused: %+v", outcome.AccessRequests)
	}
}

// field returns the named field's value, or "" when the result has none.
func field(outcome testkit.Outcome, name string) string {
	if outcome.Result == nil {
		return ""
	}
	for _, f := range outcome.Result.Fields {
		if f.Name == name {
			return f.Value
		}
	}
	return ""
}

// assertProblem checks the invocation ended in the named problem.
func assertProblem(t *testing.T, outcome testkit.Outcome, code string, category problem.Category) {
	t.Helper()
	if outcome.Err != nil {
		t.Fatalf("the invocation failed: %v", outcome.Err)
	}
	if outcome.Problem == nil {
		t.Fatalf("got the result %+v, want the problem %s", outcome.Result, code)
	}
	if outcome.Problem.Code != code || outcome.Problem.Category != category {
		t.Fatalf("got %s (%s), want %s (%s): %s", outcome.Problem.Code, outcome.Problem.Category,
			code, category, outcome.Problem.Message)
	}
}
