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

const runtimeLogEntries = `{"logs":[
  {"timestamp":"2026-10-29T10:00:05Z","log":"GET /greeting 200","level":"INFO"},
  {"timestamp":"2026-10-29T10:00:01Z","log":"service started on 9090","level":"INFO"}],"total":2,"tookMs":8}`

// runtimeLogs is the API, answering the environments, the project orders and
// its integrations, beside the observability proxy answering the log query.
type runtimeLogs struct {
	api, obs *platformStub
}

func newRuntimeLogs(t *testing.T, entries string) runtimeLogs {
	return runtimeLogs{
		api: newPlatformStub(t, deploymentRoutes(map[string]stubRoute{
			"/projects":                   {status: http.StatusOK, body: `{"items":[{"id":"orders"}]}`},
			"/projects/orders/components": {status: http.StatusOK, body: ordersIntegrations},
		})),
		obs: newStubAt(t, obsPrefix, map[string]stubRoute{"POST " + logsQueryPath: {status: http.StatusOK, body: entries}}),
	}
}

func (r runtimeLogs) logs(arguments ...string) testkit.Outcome {
	return testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"integration", "logs"},
		Arguments: arguments,
		Context:   module.Context{Name: "wso2cloud", Endpoint: r.api.endpoint(), GatewayEndpoint: r.obs.endpoint()},
		Access:    &testkit.Access{Token: "exchanged-token"},
	})
}

func TestIntegrationLogsQueriesTheProxyForTheIntegrationInOneEnvironment(t *testing.T) {
	r := newRuntimeLogs(t, runtimeLogEntries)

	outcome := r.logs("order-api", "--env", "Development")

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("integration logs failed: %v %v", outcome.Err, outcome.Problem)
	}
	if r.obs.path != obsPrefix+logsQueryPath || r.obs.presented != "Bearer exchanged-token" {
		t.Errorf("the proxy was called at %s with %q", r.obs.path, r.obs.presented)
	}
	sent := decodeBody(t, r.obs.body)
	scope := sent["searchScope"].(map[string]any)
	want := map[string]any{"project": "orders", "component": "order-api", "environment": "development"}
	for key, value := range want {
		if scope[key] != value {
			t.Errorf("searchScope.%s = %v, want %v", key, scope[key], value)
		}
	}
	if _, named := scope["namespace"]; !named {
		t.Errorf("searchScope = %v, want a namespace member for the proxy to fill in", scope)
	}
	// By default the newest 100 lines of the last hour.
	if sent["sortOrder"] != "desc" || sent["limit"] != float64(100) {
		t.Errorf("sortOrder %v limit %v, want desc and 100", sent["sortOrder"], sent["limit"])
	}
	start, _ := time.Parse(time.RFC3339, sent["startTime"].(string))
	end, _ := time.Parse(time.RFC3339, sent["endTime"].(string))
	if time.Since(end) > time.Minute || end.Sub(start) != time.Hour {
		t.Errorf("window = %v to %v, want the last hour", start, end)
	}
	if outcome.Result.Schema != IntegrationLogsSchema || field(outcome, "environment") != "development" ||
		field(outcome, "since") != "1h0m0s" {
		t.Errorf("result = %+v", outcome.Result.Fields)
	}
	// The newest-first answer is put back in time order.
	if log := field(outcome, "log"); log != "2026-10-29T10:00:01Z service started on 9090\n2026-10-29T10:00:05Z GET /greeting 200" {
		t.Errorf("log = %q, want oldest first", log)
	}
	assertEndsWithNext(t, outcome)
}

func TestIntegrationLogsWithAProjectDoesNotReadTheListing(t *testing.T) {
	r := newRuntimeLogs(t, runtimeLogEntries)

	outcome := r.logs("order-api", "--env", "development", "--project", "orders")

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("integration logs failed: %v %v", outcome.Err, outcome.Problem)
	}
	if strings.Join(r.api.paths, ", ") != "GET "+basePath+"/environments" {
		t.Errorf("API calls = %v, want only the environments", r.api.paths)
	}
}

func TestIntegrationLogsSinceAndAWholeWindow(t *testing.T) {
	r := newRuntimeLogs(t, runtimeLogEntries)

	outcome := r.logs("order-api", "--env", "development", "--project", "orders", "--since", "30m", "--tail", "0")

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("integration logs failed: %v %v", outcome.Err, outcome.Problem)
	}
	sent := decodeBody(t, r.obs.body)
	start, _ := time.Parse(time.RFC3339, sent["startTime"].(string))
	end, _ := time.Parse(time.RFC3339, sent["endTime"].(string))
	if end.Sub(start) != 30*time.Minute {
		t.Errorf("window = %v, want 30 minutes", end.Sub(start))
	}
	if sent["sortOrder"] != "asc" || sent["limit"] != float64(1000) {
		t.Errorf("sortOrder %v limit %v, want asc and 1000 for the whole window", sent["sortOrder"], sent["limit"])
	}
}

func TestIntegrationLogsWithNoOutputPointsAtTheDeployment(t *testing.T) {
	r := newRuntimeLogs(t, `{"logs":[],"total":0}`)

	outcome := r.logs("order-api", "--env", "development", "--project", "orders")

	if field(outcome, "lines") != "0" || field(outcome, "log") != "-" {
		t.Errorf("result = %+v, want no lines", outcome.Result.Fields)
	}
	if next := field(outcome, NextField); !strings.Contains(next, "wso2 intg integration deployment show order-api --env development") {
		t.Errorf("next = %q, want it to name deployment show", next)
	}
}

func TestIntegrationLogsRefusesABadCommandLineBeforeAnyCall(t *testing.T) {
	cases := map[string][]string{
		"no environment":   {"order-api"},
		"bad since":        {"order-api", "--env", "development", "--since", "yesterday"},
		"negative since":   {"order-api", "--env", "development", "--since", "-1h"},
		"negative tail":    {"order-api", "--env", "development", "--tail", "-1"},
		"no integration":   {"--env", "development"},
		"two integrations": {"a", "b", "--env", "development"},
	}
	for name, arguments := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRuntimeLogs(t, runtimeLogEntries)

			outcome := r.logs(arguments...)

			code := "intg.invalid_argument"
			if name == "no environment" {
				code = "module.flag_required"
			}
			assertProblem(t, outcome, code, problem.CategoryUsage)
			if r.api.calls != 0 || r.obs.calls != 0 {
				t.Errorf("a refused command line reached a service (%v %v)", r.api.paths, r.obs.paths)
			}
		})
	}
}

func TestIntegrationLogsWithoutAGatewayNamesHowToRecordOne(t *testing.T) {
	stub := newPlatformStub(t, nil)

	outcome := run([]string{"integration", "logs"}, []string{"order-api", "--env", "development"}, stub.endpoint(),
		&testkit.Access{Token: "t"})

	assertProblem(t, outcome, "intg.observability_not_recorded", problem.CategoryUsage)
	if stub.calls != 0 || len(outcome.AccessRequests) != 0 {
		t.Errorf("a command with nowhere to read logs from reached the API (%v)", stub.paths)
	}
}
