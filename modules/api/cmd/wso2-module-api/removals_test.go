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

func runRemoval(server *httptest.Server, command []string, arguments ...string) testkit.Outcome {
	return testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   command,
		Arguments: arguments,
		Context:   module.Context{Name: "c1", Endpoint: server.URL},
		Access:    &testkit.Access{Token: "control-plane-token"},
	})
}

func TestRemovalsRefuseWithoutYesAndCallNothing(t *testing.T) {
	for _, command := range [][]string{{"apis", "delete"}, {"projects", "delete"}} {
		server, calls := recordingServer(t, nil)
		outcome := runRemoval(server, command, "hello")
		if outcome.Problem == nil || outcome.Problem.Code != "api.invalid_argument" {
			t.Fatalf("%v ran without --yes: %+v", command, outcome.Problem)
		}
		if !strings.Contains(outcome.Problem.Recovery, "--yes") {
			t.Fatalf("%v: the recovery does not name --yes: %q", command, outcome.Problem.Recovery)
		}
		if len(*calls) != 0 {
			t.Fatalf("%v called out before it was confirmed: %v", command, *calls)
		}
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
	outcome := runRemoval(server, []string{"apis", "undeploy"}, "hello-api-v1", "--gateway-id", "c1-gateway")
	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("apis undeploy failed: err=%v problem=%v", outcome.Err, outcome.Problem)
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
	outcome := runRemoval(server, []string{"apis", "delete"}, "hello-api-v1", "--yes")
	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("apis delete failed: err=%v problem=%v", outcome.Err, outcome.Problem)
	}
	if last := (*calls)[len(*calls)-1]; last != "DELETE /api/v0.9/rest-apis/hello-api-v1" || len(*calls) != 3 {
		t.Fatalf("calls were %v, want the listing, one undeploy, then the delete", *calls)
	}
}

func TestProjectsDeleteDeletesAndReportsA404AsNotFound(t *testing.T) {
	server, calls := recordingServer(t, nil)
	outcome := runRemoval(server, []string{"projects", "delete"}, "hello", "--yes")
	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("projects delete failed: err=%v problem=%v", outcome.Err, outcome.Problem)
	}
	if strings.Join(*calls, ",") != "DELETE /api/v0.9/projects/hello" {
		t.Fatalf("calls were %v", *calls)
	}

	missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":"NOT_FOUND","message":"project not found"}`, http.StatusNotFound)
	}))
	t.Cleanup(missing.Close)
	outcome = runRemoval(missing, []string{"projects", "delete"}, "hello", "--yes")
	if outcome.Problem == nil || outcome.Problem.Code != "api.not_found" {
		t.Fatalf("a 404 was not reported as api.not_found: %+v", outcome.Problem)
	}
}

func TestApisListWithoutAProjectListsEveryProject(t *testing.T) {
	server, _ := recordingServer(t, map[string]string{
		"GET /api/v0.9/projects":                    `{"count":2,"list":[{"id":"hello","displayName":"Hello"},{"id":"sandbox","displayName":"Sandbox"}]}`,
		"GET /api/v0.9/rest-apis?projectId=hello":   `{"count":1,"list":[{"id":"hello-api-v1","displayName":"Hello API","version":"v1","context":"/hello"}]}`,
		"GET /api/v0.9/rest-apis?projectId=sandbox": `{"count":0,"list":[]}`,
	})
	outcome := runRemoval(server, []string{"apis", "list"})
	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("apis list failed: err=%v problem=%v", outcome.Err, outcome.Problem)
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
	outcome := runRemoval(server, []string{"apis", "delete"}, "hello-api-v1", "--yes")
	if outcome.Err != nil || outcome.Problem != nil || !deleted {
		t.Fatalf("apis delete stopped at a deployment that was already inactive: err=%v problem=%v",
			outcome.Err, outcome.Problem)
	}
}
