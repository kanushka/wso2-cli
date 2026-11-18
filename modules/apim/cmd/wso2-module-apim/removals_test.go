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
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/testkit"
)

// recordingServer answers the calls named in answers, 204 to any other, and
// records each as "METHOD path?query".
func recordingServer(t *testing.T, answers map[string]string) (*httptest.Server, *[]string) {
	t.Helper()
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := r.Method + " " + r.URL.RequestURI()
		calls = append(calls, call)
		w.Header().Set("Content-Type", "application/json")
		if body, known := answers[call]; known {
			_, _ = w.Write([]byte(body))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

// runRemoval runs a command with every confirmation answered yes, which is
// what a person at a terminal does; the four-path tests below script the
// other answers themselves.
func runRemoval(server *httptest.Server, command []string, arguments ...string) testkit.Outcome {
	return runRemovalAnswering(server, &testkit.Confirmation{}, command, arguments...)
}

func runRemovalAnswering(server *httptest.Server, confirm *testkit.Confirmation, command []string, arguments ...string) testkit.Outcome {
	return testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   command,
		Arguments: arguments,
		Context:   module.Context{Name: "c1", Endpoint: server.URL},
		Access:    &testkit.Access{Token: "control-plane-token"},
		Confirm:   confirm,
	})
}

// TestRemovalsAskBeforeDeletingAndHonourEveryAnswer covers the four ways a
// deletion is confirmed or not, for both delete commands: a yes at the
// terminal deletes, a no deletes nothing, --yes deletes without asking, and a
// run where nothing may be asked is refused with the shell's recovery naming
// --yes. The question is asked before anything is called, so a refused run
// calls nothing.
func TestRemovalsAskBeforeDeletingAndHonourEveryAnswer(t *testing.T) {
	for _, removal := range []struct {
		command  []string
		question string
		deletes  string
		answers  map[string]string
	}{
		{
			command:  []string{"api", "delete"},
			question: "Delete the API hello and every deployment of it? This cannot be undone.",
			deletes:  "DELETE /api/v0.9/rest-apis/hello",
			answers:  map[string]string{"GET /api/v0.9/rest-apis/hello/deployments": `{"count":0,"list":[]}`},
		},
		{
			command:  []string{"project", "delete"},
			question: "Delete the project hello? This cannot be undone.",
			deletes:  "DELETE /api/v0.9/projects/hello",
		},
	} {
		name := strings.Join(removal.command, " ")
		t.Run(name+" answered yes", func(t *testing.T) {
			server, calls := recordingServer(t, removal.answers)
			outcome := runRemovalAnswering(server, &testkit.Confirmation{}, removal.command, "hello")
			if outcome.Err != nil || outcome.Problem != nil {
				t.Fatalf("a confirmed deletion failed: err=%v problem=%v", outcome.Err, outcome.Problem)
			}
			if len(outcome.Confirmations) != 1 || outcome.Confirmations[0].Question != removal.question {
				t.Fatalf("the shell was asked %+v, want %q once", outcome.Confirmations, removal.question)
			}
			if last := (*calls)[len(*calls)-1]; last != removal.deletes {
				t.Fatalf("calls were %v, want them to end with %s", *calls, removal.deletes)
			}
		})
		t.Run(name+" answered no", func(t *testing.T) {
			server, calls := recordingServer(t, nil)
			outcome := runRemovalAnswering(server, &testkit.Confirmation{Refuse: &testkit.Declined}, removal.command, "hello")
			if outcome.Problem == nil || *outcome.Problem != testkit.Declined {
				t.Fatalf("a declined deletion was reported as %+v, want the shell's refusal unchanged", outcome.Problem)
			}
			if len(*calls) != 0 {
				t.Fatalf("a declined deletion still called out: %v", *calls)
			}
		})
		t.Run(name+" with --yes", func(t *testing.T) {
			server, calls := recordingServer(t, removal.answers)
			outcome := runRemovalAnswering(server, nil, removal.command, "hello", "--yes")
			if outcome.Err != nil || outcome.Problem != nil {
				t.Fatalf("--yes did not delete: err=%v problem=%v", outcome.Err, outcome.Problem)
			}
			if len(outcome.Confirmations) != 0 {
				t.Fatalf("--yes still asked %+v", outcome.Confirmations)
			}
			if last := (*calls)[len(*calls)-1]; last != removal.deletes {
				t.Fatalf("calls were %v, want them to end with %s", *calls, removal.deletes)
			}
		})
		t.Run(name+" under --no-input", func(t *testing.T) {
			server, calls := recordingServer(t, nil)
			refusal := testkit.NonInteractive("ws apim " + name + " hello")
			outcome := runRemovalAnswering(server, &testkit.Confirmation{Refuse: refusal}, removal.command, "hello")
			if outcome.Problem == nil || *outcome.Problem != *refusal {
				t.Fatalf("a run that may not ask was reported as %+v, want the shell's refusal unchanged", outcome.Problem)
			}
			if outcome.Problem.Code != "shell.non_interactive" || !strings.Contains(outcome.Problem.Recovery, "--yes") {
				t.Fatalf("the refusal is not shell.non_interactive naming --yes: %+v", outcome.Problem)
			}
			if len(*calls) != 0 {
				t.Fatalf("a refused deletion still called out: %v", *calls)
			}
		})
	}
}

func TestApisUndeployUndeploysOnlyActiveDeploymentsOfTheGateway(t *testing.T) {
	server, calls := recordingServer(t, map[string]string{
		"GET /api/v0.9/rest-apis/hello-api-v1/deployments": `{"count":4,"list":[
			{"deploymentId":"d1","gatewayId":"c1-gateway","status":"DEPLOYED"},
			{"deploymentId":"d2","gatewayId":"c1-gateway","status":"UNDEPLOYED"},
			{"deploymentId":"d4","gatewayId":"c1-gateway","status":"UNDEPLOYING"},
			{"deploymentId":"d3","gatewayId":"other","status":"DEPLOYED"}]}`,
		"POST /api/v0.9/rest-apis/hello-api-v1/deployments/d1/undeploy?gatewayId=c1-gateway": `{"deploymentId":"d1","status":"UNDEPLOYED"}`,
	})
	outcome := runRemoval(server, []string{"api", "undeploy"}, "hello-api-v1", "--gateway-id", "c1-gateway")
	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("api undeploy failed: err=%v problem=%v", outcome.Err, outcome.Problem)
	}
	want := []string{
		"GET /api/v0.9/rest-apis/hello-api-v1/deployments",
		"POST /api/v0.9/rest-apis/hello-api-v1/deployments/d1/undeploy?gatewayId=c1-gateway",
	}
	if strings.Join(*calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls were:\n%s\nwant:\n%s", strings.Join(*calls, "\n"), strings.Join(want, "\n"))
	}
	if !strings.Contains(rendered(outcome), "d1") {
		t.Fatalf("the result does not name the undeployed deployment:\n%s", rendered(outcome))
	}
}

func TestApisDeleteUndeploysEverywhereThenDeletes(t *testing.T) {
	server, calls := recordingServer(t, map[string]string{
		"GET /api/v0.9/rest-apis/hello-api-v1/deployments": `{"count":1,"list":[
			{"deploymentId":"d1","gatewayId":"c1-gateway","status":"DEPLOYED"}]}`,
		"POST /api/v0.9/rest-apis/hello-api-v1/deployments/d1/undeploy?gatewayId=c1-gateway": `{"deploymentId":"d1","status":"UNDEPLOYED"}`,
	})
	outcome := runRemoval(server, []string{"api", "delete"}, "hello-api-v1", "--yes")
	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("api delete failed: err=%v problem=%v", outcome.Err, outcome.Problem)
	}
	if last := (*calls)[len(*calls)-1]; last != "DELETE /api/v0.9/rest-apis/hello-api-v1" || len(*calls) != 3 {
		t.Fatalf("calls were %v, want the listing, one undeploy, then the delete", *calls)
	}
}

func TestProjectsDeleteDeletesAndReportsA404AsNotFound(t *testing.T) {
	server, calls := recordingServer(t, nil)
	outcome := runRemoval(server, []string{"project", "delete"}, "hello", "--yes")
	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("project delete failed: err=%v problem=%v", outcome.Err, outcome.Problem)
	}
	if strings.Join(*calls, ",") != "DELETE /api/v0.9/projects/hello" {
		t.Fatalf("calls were %v", *calls)
	}

	missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":"NOT_FOUND","message":"project not found"}`, http.StatusNotFound)
	}))
	t.Cleanup(missing.Close)
	outcome = runRemoval(missing, []string{"project", "delete"}, "hello", "--yes")
	if outcome.Problem == nil || outcome.Problem.Code != "apim.not_found" {
		t.Fatalf("a 404 was not reported as apim.not_found: %+v", outcome.Problem)
	}
}

func TestApisListWithoutAProjectListsEveryProject(t *testing.T) {
	server, _ := recordingServer(t, map[string]string{
		"GET /api/v0.9/projects":                    `{"count":2,"list":[{"id":"hello","displayName":"Hello"},{"id":"sandbox","displayName":"Sandbox"}]}`,
		"GET /api/v0.9/rest-apis?projectId=hello":   `{"count":1,"list":[{"id":"hello-api-v1","displayName":"Hello API","version":"v1","context":"/hello"}]}`,
		"GET /api/v0.9/rest-apis?projectId=sandbox": `{"count":0,"list":[]}`,
	})
	outcome := runRemoval(server, []string{"api", "list"})
	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("api list failed: err=%v problem=%v", outcome.Err, outcome.Problem)
	}
	page := rendered(outcome)
	if !strings.Contains(page, "hello-api-v1") || !strings.Contains(page, "hello") {
		t.Fatalf("the listing does not name the API beside its project:\n%s", page)
	}
}

func TestApisDeleteTreatsADeploymentThatIsNoLongerActiveAsUndeployed(t *testing.T) {
	// Measured against API Platform 0.16.0: a deployment the listing still
	// calls DEPLOYED a moment after an undeploy answers 409
	// DEPLOYMENT_NOT_ACTIVE, which is the outcome this step wanted.
	var deleted bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"count":1,"list":[{"deploymentId":"d1","gatewayId":"g","status":"DEPLOYED"}]}`))
		case r.Method == http.MethodPost:
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"code":"DEPLOYMENT_NOT_ACTIVE","message":"No active deployment found"}`))
		default:
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(server.Close)
	outcome := runRemoval(server, []string{"api", "delete"}, "hello-api-v1", "--yes")
	if outcome.Err != nil || outcome.Problem != nil || !deleted {
		t.Fatalf("api delete stopped at a deployment that was already inactive: err=%v problem=%v",
			outcome.Err, outcome.Problem)
	}
}
