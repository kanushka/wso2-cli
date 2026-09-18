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
	"github.com/wso2/wso2-cli/sdk/testkit"
)

const helloAudience = "http://gateway.example.test/hello"

// fakePlatform is one server standing in for both surfaces an invocation
// touches: platform-api's control plane, and the gateway the API is deployed
// on, whose registered endpoint is this server's own URL.
type fakePlatform struct {
	server *httptest.Server
	// policies is the policies member of the API the control plane records.
	policies string
	// deployments is the list member of the API's deployments.
	deployments string
	// apiStatus and apiBody are what the deployed API itself answers.
	apiStatus int
	apiBody   string
	// calls records method, path and bearer token, in order.
	calls []string
	// apiRequest is what reached the deployed API.
	apiMethod, apiBodySent string
	apiHeaders             http.Header
}

func newFakePlatform(t *testing.T) *fakePlatform {
	t.Helper()
	platform := &fakePlatform{
		policies: `[{"name":"jwt-auth","version":"v1","params":{"issuers":["thunder"],
			"audiences":["` + helloAudience + `"]}}]`,
		deployments: `[{"deploymentId":"old","gatewayId":"c1-gateway","status":"ARCHIVED"},
			{"deploymentId":"new","gatewayId":"c1-gateway","status":"DEPLOYED"}]`,
		apiStatus: http.StatusOK,
		apiBody:   `{"greeting":"hello"}`,
	}
	platform.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		platform.calls = append(platform.calls, r.Method+" "+r.URL.RequestURI()+" "+token)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v0.9/rest-apis/hello-api-v1":
			_, _ = w.Write([]byte(`{"id":"hello-api-v1","context":"/hello","policies":` + platform.policies + `}`))
		case r.URL.Path == "/api/v0.9/rest-apis/hello-api-v1/deployments":
			_, _ = w.Write([]byte(`{"list":` + platform.deployments + `}`))
		case r.URL.Path == "/api/v0.9/gateways":
			_, _ = w.Write([]byte(`{"list":[
				{"id":"c1-gateway","endpoints":["` + platform.server.URL + `/"]},
				{"id":"c2-gateway","endpoints":["http://c2.example.test"]}]}`))
		case strings.HasPrefix(r.URL.Path, "/hello"):
			sent, _ := io.ReadAll(r.Body)
			platform.apiMethod, platform.apiBodySent, platform.apiHeaders = r.Method, string(sent), r.Header
			w.WriteHeader(platform.apiStatus)
			_, _ = w.Write([]byte(platform.apiBody))
		default:
			http.Error(w, `{"code":"NOT_FOUND"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(platform.server.Close)
	return platform
}

func (p *fakePlatform) run(command string, arguments ...string) testkit.Outcome {
	return testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"apis", command},
		Arguments: arguments,
		Context:   module.Context{Name: "c1", Endpoint: p.server.URL},
		Access:    &testkit.Access{Token: "control-plane-token"},
		AccessByRecord: map[string]*testkit.Access{
			module.RecordAPI: {Token: "api-token"},
		},
	})
}

func mustSucceed(t *testing.T, outcome testkit.Outcome) {
	t.Helper()
	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("the command failed: err=%v problem=%v", outcome.Err, outcome.Problem)
	}
}

func TestApisTokenAsksForAccessBoundToTheAudienceTheApiDeclares(t *testing.T) {
	platform := newFakePlatform(t)
	outcome := platform.run("token", "hello-api-v1")
	mustSucceed(t, outcome)
	if len(outcome.AccessRequests) != 2 {
		t.Fatalf("the module asked for access %d times, want 2: %+v", len(outcome.AccessRequests), outcome.AccessRequests)
	}
	want := module.AccessRequest{Audience: InvocationAudience, Record: module.RecordAPI, Resource: helloAudience}
	if got := outcome.AccessRequests[1]; got.Audience != want.Audience || got.Record != want.Record ||
		got.Resource != want.Resource {
		t.Fatalf("the api access request was %+v, want %+v", got, want)
	}
	text := rendered(outcome)
	if !strings.Contains(text, "api-token") || !strings.Contains(text, helloAudience) {
		t.Fatalf("the result does not carry the token and its audience:\n%s", text)
	}
	if strings.Contains(text, "control-plane-token") {
		t.Fatalf("the result carries the control plane's token:\n%s", text)
	}
}

func TestApisTokenRefusesAnApiThatDeclaresNoAudience(t *testing.T) {
	platform := newFakePlatform(t)
	platform.policies = `[]`
	outcome := platform.run("token", "hello-api-v1")
	if outcome.Problem == nil || outcome.Problem.Code != "api.no_audience" {
		t.Fatalf("problem = %+v, want api.no_audience", outcome.Problem)
	}
	if len(outcome.AccessRequests) != 1 {
		t.Fatalf("the module asked for api access with no audience to bind it to: %+v", outcome.AccessRequests)
	}
}

func TestApisInvokeCallsTheDeployedApiWithItsOwnToken(t *testing.T) {
	platform := newFakePlatform(t)
	outcome := platform.run("invoke", "hello-api-v1", "/greet?lang=en")
	mustSucceed(t, outcome)
	want := []string{
		"GET /api/v0.9/rest-apis/hello-api-v1 control-plane-token",
		"GET /api/v0.9/rest-apis/hello-api-v1/deployments control-plane-token",
		"GET /api/v0.9/gateways control-plane-token",
		"GET /hello/greet?lang=en api-token",
	}
	if strings.Join(platform.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("the module called:\n%s\nwant:\n%s", strings.Join(platform.calls, "\n"), strings.Join(want, "\n"))
	}
	text := rendered(outcome)
	for _, carried := range []string{"200", platform.server.URL + "/hello/greet?lang=en", `"greeting": "hello"`} {
		if !strings.Contains(text, carried) {
			t.Errorf("the result does not carry %q:\n%s", carried, text)
		}
	}
	if strings.Contains(text, "api-token") {
		t.Errorf("the result of an invocation carries the token:\n%s", text)
	}
}

func TestApisInvokeIndentsAJSONAnswerAndLeavesAnyOtherAlone(t *testing.T) {
	platform := newFakePlatform(t)
	platform.apiBody = `{"a":[1,2],"b":"x"}`
	outcome := platform.run("invoke", "hello-api-v1")
	mustSucceed(t, outcome)
	if text := rendered(outcome); !strings.Contains(text, "body={\n  \"a\": [\n    1,\n    2\n  ],\n  \"b\": \"x\"\n}\n") {
		t.Fatalf("the JSON answer is not indented:\n%s", text)
	}
	platform.apiBody = `not json {`
	outcome = platform.run("invoke", "hello-api-v1")
	mustSucceed(t, outcome)
	if text := rendered(outcome); !strings.Contains(text, "body=not json {\n") {
		t.Fatalf("a non-JSON answer was altered:\n%s", text)
	}
}

func TestApisInvokeSendsTheMethodHeadersAndBodyItWasGiven(t *testing.T) {
	platform := newFakePlatform(t)
	outcome := platform.run("invoke", "hello-api-v1", "greet",
		"-X", "post", "-H", "X-Trace: abc", "-H", "Content-Type: text/plain", "-d", "ping")
	mustSucceed(t, outcome)
	if platform.apiMethod != http.MethodPost || platform.apiBodySent != "ping" {
		t.Fatalf("the API received %s %q, want POST \"ping\"", platform.apiMethod, platform.apiBodySent)
	}
	if platform.apiHeaders.Get("X-Trace") != "abc" || platform.apiHeaders.Get("Content-Type") != "text/plain" {
		t.Fatalf("the API received headers %v", platform.apiHeaders)
	}
}

func TestApisInvokeRefusesAnAuthorizationHeaderOfTheCallersOwn(t *testing.T) {
	platform := newFakePlatform(t)
	outcome := platform.run("invoke", "hello-api-v1", "-H", "Authorization: Bearer mine")
	if outcome.Problem == nil || outcome.Problem.Code != "api.invalid_argument" {
		t.Fatalf("problem = %+v, want api.invalid_argument", outcome.Problem)
	}
}

func TestApisInvokeReportsARefusalFromTheApiAsItsResult(t *testing.T) {
	// The person is testing the API: a 401 is the answer they came for, not
	// a failure of this command.
	platform := newFakePlatform(t)
	platform.apiStatus, platform.apiBody = http.StatusUnauthorized, `{"error":"invalid token"}`
	outcome := platform.run("invoke", "hello-api-v1")
	mustSucceed(t, outcome)
	if text := rendered(outcome); !strings.Contains(text, "401") || !strings.Contains(text, "invalid token") {
		t.Fatalf("the result does not carry the API's refusal:\n%s", text)
	}
}

func TestApisInvokeWithNoTokenAsksForNoApiAccess(t *testing.T) {
	platform := newFakePlatform(t)
	outcome := platform.run("invoke", "hello-api-v1", "--no-token")
	mustSucceed(t, outcome)
	if len(outcome.AccessRequests) != 1 {
		t.Fatalf("--no-token still asked for api access: %+v", outcome.AccessRequests)
	}
	if got := platform.apiHeaders.Get("Authorization"); got != "" {
		t.Fatalf("--no-token sent Authorization %q", got)
	}
}

func TestApisInvokeCallsAnApiThatDeclaresNoAudienceWithoutAToken(t *testing.T) {
	platform := newFakePlatform(t)
	platform.policies = `[]`
	outcome := platform.run("invoke", "hello-api-v1")
	mustSucceed(t, outcome)
	if got := platform.apiHeaders.Get("Authorization"); got != "" || len(outcome.AccessRequests) != 1 {
		t.Fatalf("an open API was sent Authorization %q after %d access requests", got, len(outcome.AccessRequests))
	}
}

func TestApisInvokeRefusesAnApiThatIsNotDeployed(t *testing.T) {
	platform := newFakePlatform(t)
	platform.deployments = `[{"deploymentId":"old","gatewayId":"c1-gateway","status":"ARCHIVED"}]`
	outcome := platform.run("invoke", "hello-api-v1")
	if outcome.Problem == nil || outcome.Problem.Code != "api.not_deployed" {
		t.Fatalf("problem = %+v, want api.not_deployed", outcome.Problem)
	}
	if !strings.Contains(outcome.Problem.Recovery, "wso2 api apis deploy hello-api-v1") {
		t.Fatalf("the recovery does not name the deploy command: %q", outcome.Problem.Recovery)
	}
}

func TestApisInvokeNeedsTheGatewayNamedWhenTheApiIsOnSeveral(t *testing.T) {
	platform := newFakePlatform(t)
	platform.deployments = `[{"gatewayId":"c1-gateway","status":"DEPLOYED"},
		{"gatewayId":"c2-gateway","status":"DEPLOYED"}]`
	outcome := platform.run("invoke", "hello-api-v1")
	if outcome.Problem == nil || outcome.Problem.Code != "api.invalid_argument" ||
		!strings.Contains(outcome.Problem.Recovery, "--gateway-id") {
		t.Fatalf("problem = %+v, want api.invalid_argument naming --gateway-id", outcome.Problem)
	}
	mustSucceed(t, platform.run("invoke", "hello-api-v1", "--gateway-id", "c1-gateway"))
}
