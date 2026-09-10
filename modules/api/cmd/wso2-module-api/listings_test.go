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

// platformStub answers the control-plane and gateway paths this module calls,
// recording the path and token it was reached with.
type platformStub struct {
	*httptest.Server
	path      string
	query     string
	presented string
}

func newPlatformStub(t *testing.T, routes map[string]string) *platformStub {
	t.Helper()
	stub := &platformStub{}
	stub.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.path, stub.query = r.URL.Path, r.URL.RawQuery
		stub.presented = r.Header.Get("Authorization")
		body, found := routes[r.URL.Path]
		if !found {
			http.Error(w, `{"status":"error","code":"NOT_FOUND"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(stub.Close)
	return stub
}

func rendered(outcome testkit.Outcome) string {
	if outcome.Result == nil {
		return ""
	}
	var b strings.Builder
	for _, field := range outcome.Result.Fields {
		b.WriteString(field.Name + "=" + field.Value + "\n")
	}
	for _, row := range outcome.Result.Rows {
		b.WriteString(strings.Join(row.Values, "\t") + "\n")
	}
	return b.String()
}

func TestProjectsListReportsARowPerProject(t *testing.T) {
	stub := newPlatformStub(t, map[string]string{
		"/api/v0.9/projects": `{"count":2,"list":[
			{"id":"hello-project","displayName":"Hello Project","organizationId":"default"},
			{"id":"other","displayName":"Other","organizationId":"default"}]}`,
	})
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command: []string{"projects", "list"},
		Context: module.Context{Name: "c1", Endpoint: stub.URL},
		Access:  &testkit.Access{Token: "control-plane-token"},
	})
	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("projects list failed: err=%v problem=%v", outcome.Err, outcome.Problem)
	}
	if stub.presented != "Bearer control-plane-token" {
		t.Fatalf("the module presented %q, want the brokered token", stub.presented)
	}
	if len(outcome.Result.Rows) != 2 {
		t.Fatalf("projects list returned %d rows, want 2", len(outcome.Result.Rows))
	}
	if !strings.Contains(rendered(outcome), "hello-project") {
		t.Fatalf("the listing does not report the project id:\n%s", rendered(outcome))
	}
}

func TestApisListNeedsTheProjectItListsWithin(t *testing.T) {
	// Measured against API Platform 0.16.0: the control plane refuses
	// /rest-apis without projectId, and its refusal names a query parameter
	// rather than the flag a user would have to pass. Saying it here means the
	// user is told what to type.
	stub := newPlatformStub(t, map[string]string{})
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command: []string{"apis", "list"},
		Context: module.Context{Name: "c1", Endpoint: stub.URL},
		Access:  &testkit.Access{Token: "control-plane-token"},
	})
	if outcome.Problem == nil {
		t.Fatalf("apis list ran without a project: %+v", outcome.Result)
	}
	if !strings.Contains(outcome.Problem.Recovery, "--project") {
		t.Fatalf("the refusal does not name the flag to pass: %q", outcome.Problem.Recovery)
	}
}

func TestApisListAsksTheControlPlaneForOneProject(t *testing.T) {
	stub := newPlatformStub(t, map[string]string{
		"/api/v0.9/rest-apis": `{"count":1,"list":[
			{"id":"hello-api","displayName":"Hello API","version":"v1","context":"/hello",
			 "lifeCycleStatus":"CREATED"}]}`,
	})
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"apis", "list"},
		Arguments: []string{"--project", "hello-project"},
		Context:   module.Context{Name: "c1", Endpoint: stub.URL},
		Access:    &testkit.Access{Token: "control-plane-token"},
	})
	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("apis list failed: err=%v problem=%v", outcome.Err, outcome.Problem)
	}
	if stub.query != "projectId=hello-project" {
		t.Fatalf("the module asked with query %q, want projectId=hello-project", stub.query)
	}
	if len(outcome.Result.Rows) != 1 {
		t.Fatalf("apis list returned %d rows, want 1", len(outcome.Result.Rows))
	}
}

func TestGatewayApisListReadsTheGatewayRecordNotTheControlPlane(t *testing.T) {
	// The gateway is a second record on the same product, reached at its own
	// endpoint with access brokered for that record. A module that called the
	// control plane here would report what is designed rather than what is
	// actually serving traffic.
	gateway := newPlatformStub(t, map[string]string{
		"/api/management/v1/rest-apis": `{"count":1,"status":"success","apis":[
			{"kind":"RestApi","metadata":{"name":"hello-api-v1"},
			 "spec":{"displayName":"Hello API","version":"v1","context":"/hello"},
			 "status":{"id":"hello-api-v1","state":"deployed"}}]}`,
	})
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command: []string{"gateway", "apis", "list"},
		Context: module.Context{Name: "c1", Endpoint: "http://control-plane.invalid",
			GatewayEndpoint: gateway.URL},
		Access: &testkit.Access{Token: "gateway-token"},
	})
	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("gateway apis list failed: err=%v problem=%v", outcome.Err, outcome.Problem)
	}
	if gateway.path != "/api/management/v1/rest-apis" {
		t.Fatalf("the module called %q on the gateway", gateway.path)
	}
	if !strings.Contains(rendered(outcome), "deployed") {
		t.Fatalf("the listing does not report the deployment state:\n%s", rendered(outcome))
	}
}
