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
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/testkit"
)

// helloAPIDocument is the example gateway RestApi document this command must
// accept verbatim, from gateway/hello-api.yaml.
const helloAPIDocument = `apiVersion: gateway.api-platform.wso2.com/v1
kind: RestApi
metadata:
  name: hello-api-v1
spec:
  displayName: Hello API
  version: v1
  context: /hello
  upstream:
    main:
      url: http://mockapi-c1:8080
  policies:
    - name: jwt-auth
      version: v1
      params:
        issuers:
          - thunder
  operations:
    - method: GET
      path: /
    - method: GET
      path: /{any}
`

// writeFixture writes content to a file under the test's temp directory and
// returns its path.
func writeFixture(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	return path
}

func TestApisCreateSendsTheExactRequestBodyTheYamlMapsTo(t *testing.T) {
	var captured string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v0.9/rest-apis" {
			http.Error(w, `{"code":"NOT_FOUND"}`, http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		captured = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"hello-api-v1","displayName":"Hello API","version":"v1",
			"context":"/hello","lifeCycleStatus":"CREATED"}`))
	}))
	t.Cleanup(server.Close)

	file := writeFixture(t, "hello-api.yaml", helloAPIDocument)
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"apis", "create"},
		Arguments: []string{"-f", file, "--project", "hello-project"},
		Context:   module.Context{Name: "c1", Endpoint: server.URL},
		Access:    &testkit.Access{Token: "control-plane-token"},
	})
	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("apis create failed: err=%v problem=%v", outcome.Err, outcome.Problem)
	}

	var sent map[string]any
	if err := json.Unmarshal([]byte(captured), &sent); err != nil {
		t.Fatalf("the request body is not valid JSON: %v\n%s", err, captured)
	}
	want := map[string]any{
		"id":          "hello-api-v1",
		"displayName": "Hello API",
		"version":     "v1",
		"context":     "/hello",
		"projectId":   "hello-project",
		"upstream": map[string]any{
			"main": map[string]any{"url": "http://mockapi-c1:8080"},
		},
		"policies": []any{
			map[string]any{
				"name":    "jwt-auth",
				"version": "v1",
				"params": map[string]any{
					"issuers": []any{"thunder"},
				},
			},
		},
		"operations": []any{
			map[string]any{"request": map[string]any{"method": "GET", "path": "/"}},
			map[string]any{"request": map[string]any{"method": "GET", "path": "/{any}"}},
		},
	}
	gotJSON, _ := json.Marshal(sent)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("the request body was:\n%s\nwant:\n%s", gotJSON, wantJSON)
	}

	if !strings.Contains(rendered(outcome), "hello-api-v1") {
		t.Fatalf("the result does not report the created API's id:\n%s", rendered(outcome))
	}
}

func TestApisCreateRefusesTheWrongApiVersionOrKindWithoutCallingOut(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(server.Close)

	document := `apiVersion: gateway.api-platform.wso2.com/v2
kind: RestApi
metadata: { name: hello }
spec: { displayName: Hello, version: v1, context: /hello }
`
	file := writeFixture(t, "wrong-version.yaml", document)
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"apis", "create"},
		Arguments: []string{"-f", file, "--project", "hello-project"},
		Context:   module.Context{Name: "c1", Endpoint: server.URL},
		Access:    &testkit.Access{Token: "control-plane-token"},
	})
	if outcome.Problem == nil {
		t.Fatalf("a wrong apiVersion was accepted: %+v", outcome.Result)
	}
	if !strings.Contains(outcome.Problem.Message, "apiVersion") {
		t.Fatalf("the refusal does not name apiVersion: %q", outcome.Problem.Message)
	}
	if called {
		t.Fatal("the module called out despite a document it should have refused")
	}
}

func TestApisCreateNeedsTheProjectFlag(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(server.Close)

	file := writeFixture(t, "hello-api.yaml", helloAPIDocument)
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"apis", "create"},
		Arguments: []string{"-f", file},
		Context:   module.Context{Name: "c1", Endpoint: server.URL},
		Access:    &testkit.Access{Token: "control-plane-token"},
	})
	if outcome.Problem == nil {
		t.Fatalf("apis create ran without --project: %+v", outcome.Result)
	}
	if !strings.Contains(outcome.Problem.Recovery, "--project") {
		t.Fatalf("the refusal does not name the flag to pass: %q", outcome.Problem.Recovery)
	}
	if called {
		t.Fatal("the module called out despite missing --project")
	}
}

func TestApisCreateRefusesAFieldItCannotMapNamingIt(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(server.Close)

	document := `apiVersion: gateway.api-platform.wso2.com/v1
kind: RestApi
metadata: { name: hello }
spec:
  displayName: Hello
  version: v1
  context: /hello
  rateLimit: 100
`
	file := writeFixture(t, "unmapped-field.yaml", document)
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"apis", "create"},
		Arguments: []string{"-f", file, "--project", "hello-project"},
		Context:   module.Context{Name: "c1", Endpoint: server.URL},
		Access:    &testkit.Access{Token: "control-plane-token"},
	})
	if outcome.Problem == nil {
		t.Fatalf("an unmapped field was accepted: %+v", outcome.Result)
	}
	if !strings.Contains(outcome.Problem.Message, "spec.rateLimit") {
		t.Fatalf("the refusal does not name the field: %q", outcome.Problem.Message)
	}
	if called {
		t.Fatal("the module called out despite a field it should have refused")
	}
}

func TestApisDeployPostsOnlyTheDeploymentWithTheExactDeployRequestBody(t *testing.T) {
	// platform-api's DeployAPI associates the API with the target gateway
	// itself when it is not already associated, and POST
	// /rest-apis/{id}/gateways (AddGatewaysToAPI) takes an array, not a
	// single object — so this command must post the deployment alone, never
	// the association.
	var calls []string
	var capturedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/api/v0.9/rest-apis/hello-api/deployments" {
			body, _ := io.ReadAll(r.Body)
			capturedBody = string(body)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"deploymentId":"dep-1","name":"hello-api-prod-gateway-01",
				"gatewayId":"prod-gateway-01","status":"DEPLOYING"}`))
			return
		}
		http.Error(w, `{"code":"NOT_FOUND"}`, http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"apis", "deploy"},
		Arguments: []string{"hello-api", "--gateway-id", "prod-gateway-01"},
		Context:   module.Context{Name: "c1", Endpoint: server.URL},
		Access:    &testkit.Access{Token: "control-plane-token"},
	})
	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("apis deploy failed: err=%v problem=%v", outcome.Err, outcome.Problem)
	}
	want := []string{"POST /api/v0.9/rest-apis/hello-api/deployments"}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("the module called:\n%s\nwant:\n%s", strings.Join(calls, "\n"), strings.Join(want, "\n"))
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(capturedBody), &sent); err != nil {
		t.Fatalf("the deployment request body is not valid JSON: %v\n%s", err, capturedBody)
	}
	want2 := map[string]any{"name": "hello-api-prod-gateway-01", "base": "current", "gatewayId": "prod-gateway-01"}
	gotJSON, _ := json.Marshal(sent)
	wantJSON, _ := json.Marshal(want2)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("the deployment request body was:\n%s\nwant:\n%s", gotJSON, wantJSON)
	}
	if !strings.Contains(rendered(outcome), "dep-1") {
		t.Fatalf("the result does not report the deployment id:\n%s", rendered(outcome))
	}
}

func TestApisDeploySurfacesAPlatformApiErrorAsTheRepoProblemShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"DEPLOYMENT_CONFLICT","message":"a deployment already exists"}`))
	}))
	t.Cleanup(server.Close)

	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"apis", "deploy"},
		Arguments: []string{"hello-api", "--gateway-id", "prod-gateway-01"},
		Context:   module.Context{Name: "c1", Endpoint: server.URL},
		Access:    &testkit.Access{Token: "control-plane-token"},
	})
	if outcome.Problem == nil {
		t.Fatalf("a 409 from the deployment was not surfaced as a problem: %+v", outcome.Result)
	}
	if outcome.Problem.Code != "api.call_failed" {
		t.Fatalf("problem code = %q, want api.call_failed", outcome.Problem.Code)
	}
	if !strings.Contains(outcome.Problem.Message, "a deployment already exists") {
		t.Fatalf("the problem does not carry the deployment's own message: %q", outcome.Problem.Message)
	}
}

func TestApisDeployEscapesTheApiIdInThePath(t *testing.T) {
	var capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// r.URL.Path is already decoded; the escaped form on the wire is what
		// proves the id was escaped, so this reads EscapedPath() instead.
		capturedPath = r.URL.EscapedPath()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"deploymentId":"dep-1","name":"n","gatewayId":"gw","status":"DEPLOYING"}`))
	}))
	t.Cleanup(server.Close)

	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"apis", "deploy"},
		Arguments: []string{"hello/api", "--gateway-id", "gw"},
		Context:   module.Context{Name: "c1", Endpoint: server.URL},
		Access:    &testkit.Access{Token: "control-plane-token"},
	})
	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("apis deploy failed: err=%v problem=%v", outcome.Err, outcome.Problem)
	}
	if capturedPath != "/api/v0.9/rest-apis/hello%2Fapi/deployments" {
		t.Fatalf("the api id was not escaped in the path: %q", capturedPath)
	}
}

func TestApisCreateMapsOperationPoliciesOntoTheRequestField(t *testing.T) {
	var captured string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"hello-api","displayName":"Hello","version":"v1","context":"/hello"}`))
	}))
	t.Cleanup(server.Close)

	document := `apiVersion: gateway.api-platform.wso2.com/v1
kind: RestApi
metadata: { name: hello-api }
spec:
  displayName: Hello
  version: v1
  context: /hello
  upstream: { main: { url: http://backend:8080 } }
  operations:
    - method: GET
      path: /
      policies:
        - name: rate-limit
          version: v1
`
	file := writeFixture(t, "op-policies.yaml", document)
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"apis", "create"},
		Arguments: []string{"-f", file, "--project", "p1"},
		Context:   module.Context{Name: "c1", Endpoint: server.URL},
		Access:    &testkit.Access{Token: "control-plane-token"},
	})
	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("apis create failed: err=%v problem=%v", outcome.Err, outcome.Problem)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(captured), &sent); err != nil {
		t.Fatalf("body is not JSON: %v\n%s", err, captured)
	}
	operations, _ := sent["operations"].([]any)
	if len(operations) != 1 {
		t.Fatalf("operations = %v, want 1 entry", sent["operations"])
	}
	op, _ := operations[0].(map[string]any)
	request, _ := op["request"].(map[string]any)
	if request == nil {
		t.Fatalf("operation has no request member: %v", op)
	}
	policies, _ := request["policies"].([]any)
	if len(policies) != 1 {
		t.Fatalf("operation.request.policies not carried through: %v", request)
	}
}

func TestApisCreateRefusesAnUnknownOperationKeyByPath(t *testing.T) {
	document := `apiVersion: gateway.api-platform.wso2.com/v1
kind: RestApi
metadata: { name: hello-api }
spec:
  displayName: Hello
  version: v1
  context: /hello
  upstream: { main: { url: http://backend:8080 } }
  operations:
    - method: GET
      path: /
    - method: GET
      path: /other
      foo: bar
`
	file := writeFixture(t, "op-unknown.yaml", document)
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"apis", "create"},
		Arguments: []string{"-f", file, "--project", "p1"},
		Context:   module.Context{Name: "c1", Endpoint: "http://unused.invalid"},
	})
	if outcome.Problem == nil {
		t.Fatalf("an unknown operation key was accepted: %+v", outcome.Result)
	}
	if !strings.Contains(outcome.Problem.Message, "spec.operations[1].foo") {
		t.Fatalf("the refusal does not name the path: %q", outcome.Problem.Message)
	}
}

func TestApisCreateRefusesAnOperationEntryThatIsNotAnObject(t *testing.T) {
	document := `apiVersion: gateway.api-platform.wso2.com/v1
kind: RestApi
metadata: { name: hello-api }
spec:
  displayName: Hello
  version: v1
  context: /hello
  upstream: { main: { url: http://backend:8080 } }
  operations:
    - "not an object"
`
	file := writeFixture(t, "op-not-object.yaml", document)
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"apis", "create"},
		Arguments: []string{"-f", file, "--project", "p1"},
		Context:   module.Context{Name: "c1", Endpoint: "http://unused.invalid"},
	})
	if outcome.Problem == nil {
		t.Fatalf("a non-object operation entry was accepted: %+v", outcome.Result)
	}
	if !strings.Contains(outcome.Problem.Message, "spec.operations[0]") {
		t.Fatalf("the refusal does not name the entry: %q", outcome.Problem.Message)
	}
}

func TestApisCreateRefusesAnUnknownUpstreamKeyByPath(t *testing.T) {
	document := `apiVersion: gateway.api-platform.wso2.com/v1
kind: RestApi
metadata: { name: hello-api }
spec:
  displayName: Hello
  version: v1
  context: /hello
  upstream:
    main:
      url: http://backend:8080
      hostRewrite: true
`
	file := writeFixture(t, "upstream-unknown.yaml", document)
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"apis", "create"},
		Arguments: []string{"-f", file, "--project", "p1"},
		Context:   module.Context{Name: "c1", Endpoint: "http://unused.invalid"},
	})
	if outcome.Problem == nil {
		t.Fatalf("an unknown upstream key was accepted: %+v", outcome.Result)
	}
	if !strings.Contains(outcome.Problem.Message, "spec.upstream.main.hostRewrite") {
		t.Fatalf("the refusal does not name the path: %q", outcome.Problem.Message)
	}
}

func TestApisCreateRefusesAnUnknownPolicyKeyByPath(t *testing.T) {
	document := `apiVersion: gateway.api-platform.wso2.com/v1
kind: RestApi
metadata: { name: hello-api }
spec:
  displayName: Hello
  version: v1
  context: /hello
  upstream: { main: { url: http://backend:8080 } }
  policies:
    - name: jwt-auth
      version: v1
      scope: read
`
	file := writeFixture(t, "policy-unknown.yaml", document)
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"apis", "create"},
		Arguments: []string{"-f", file, "--project", "p1"},
		Context:   module.Context{Name: "c1", Endpoint: "http://unused.invalid"},
	})
	if outcome.Problem == nil {
		t.Fatalf("an unknown policy key was accepted: %+v", outcome.Result)
	}
	if !strings.Contains(outcome.Problem.Message, "spec.policies[0].scope") {
		t.Fatalf("the refusal does not name the path: %q", outcome.Problem.Message)
	}
}

func TestApisCreateRequiresDisplayNameContextVersionAndUpstream(t *testing.T) {
	cases := []struct {
		name     string
		document string
		want     string
	}{
		{
			name: "missing displayName",
			document: `apiVersion: gateway.api-platform.wso2.com/v1
kind: RestApi
metadata: { name: hello-api }
spec:
  version: v1
  context: /hello
  upstream: { main: { url: http://backend:8080 } }
`,
			want: "spec.displayName",
		},
		{
			name: "missing context",
			document: `apiVersion: gateway.api-platform.wso2.com/v1
kind: RestApi
metadata: { name: hello-api }
spec:
  displayName: Hello
  version: v1
  upstream: { main: { url: http://backend:8080 } }
`,
			want: "spec.context",
		},
		{
			name: "missing version",
			document: `apiVersion: gateway.api-platform.wso2.com/v1
kind: RestApi
metadata: { name: hello-api }
spec:
  displayName: Hello
  context: /hello
  upstream: { main: { url: http://backend:8080 } }
`,
			want: "spec.version",
		},
		{
			name: "missing upstream",
			document: `apiVersion: gateway.api-platform.wso2.com/v1
kind: RestApi
metadata: { name: hello-api }
spec:
  displayName: Hello
  version: v1
  context: /hello
`,
			want: "spec.upstream",
		},
		{
			name: "unquoted numeric version",
			document: `apiVersion: gateway.api-platform.wso2.com/v1
kind: RestApi
metadata: { name: hello-api }
spec:
  displayName: Hello
  version: 1.0
  context: /hello
  upstream: { main: { url: http://backend:8080 } }
`,
			want: "Quote",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			file := writeFixture(t, "required.yaml", testCase.document)
			outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
				Command:   []string{"apis", "create"},
				Arguments: []string{"-f", file, "--project", "p1"},
				Context:   module.Context{Name: "c1", Endpoint: "http://unused.invalid"},
			})
			if outcome.Problem == nil {
				t.Fatalf("%s: was accepted: %+v", testCase.name, outcome.Result)
			}
			if !strings.Contains(outcome.Problem.Message, testCase.want) &&
				!strings.Contains(outcome.Problem.Recovery, testCase.want) {
				t.Fatalf("%s: refusal does not mention %q: message=%q recovery=%q",
					testCase.name, testCase.want, outcome.Problem.Message, outcome.Problem.Recovery)
			}
		})
	}
}

func TestApisCreateRendersFieldErrorsFromA400AndPointsAtTheFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":"error","code":"VALIDATION_FAILED","message":"invalid request",
			"errors":[{"field":"context","message":"must start with /"}]}`))
	}))
	t.Cleanup(server.Close)

	file := writeFixture(t, "hello-api.yaml", helloAPIDocument)
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"apis", "create"},
		Arguments: []string{"-f", file, "--project", "hello-project"},
		Context:   module.Context{Name: "c1", Endpoint: server.URL},
		Access:    &testkit.Access{Token: "control-plane-token"},
	})
	if outcome.Problem == nil {
		t.Fatalf("a 400 was not surfaced as a problem: %+v", outcome.Result)
	}
	if !strings.Contains(outcome.Problem.Message, "context: must start with /") {
		t.Fatalf("the problem does not render the field error: %q", outcome.Problem.Message)
	}
	if !strings.Contains(outcome.Problem.Recovery, file) {
		t.Fatalf("the recovery does not point at the file: %q", outcome.Problem.Recovery)
	}
	if strings.Contains(outcome.Problem.Recovery, "deployment's own logs") {
		t.Fatalf("the recovery still points at the deployment's logs: %q", outcome.Problem.Recovery)
	}
}

func TestApisCreateRefusesNoPositionalArguments(t *testing.T) {
	file := writeFixture(t, "hello-api.yaml", helloAPIDocument)
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"apis", "create"},
		Arguments: []string{"unexpected-arg", "-f", file, "--project", "p1"},
		Context:   module.Context{Name: "c1", Endpoint: "http://unused.invalid"},
	})
	if outcome.Problem == nil {
		t.Fatalf("a positional argument was accepted: %+v", outcome.Result)
	}
}

func TestApisCreateRefusesAnUnknownTopLevelKey(t *testing.T) {
	document := `apiVersion: gateway.api-platform.wso2.com/v1
kind: RestApi
metadata: { name: hello-api }
status: { id: x }
spec:
  displayName: Hello
  version: v1
  context: /hello
  upstream: { main: { url: http://backend:8080 } }
`
	file := writeFixture(t, "top-level-unknown.yaml", document)
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"apis", "create"},
		Arguments: []string{"-f", file, "--project", "p1"},
		Context:   module.Context{Name: "c1", Endpoint: "http://unused.invalid"},
	})
	if outcome.Problem == nil {
		t.Fatalf("an unknown top-level key was accepted: %+v", outcome.Result)
	}
	if !strings.Contains(outcome.Problem.Message, "status") {
		t.Fatalf("the refusal does not name the key: %q", outcome.Problem.Message)
	}
}

func TestApisCreateRefusesAnUnknownMetadataKey(t *testing.T) {
	document := `apiVersion: gateway.api-platform.wso2.com/v1
kind: RestApi
metadata: { name: hello-api, namespace: default }
spec:
  displayName: Hello
  version: v1
  context: /hello
  upstream: { main: { url: http://backend:8080 } }
`
	file := writeFixture(t, "metadata-unknown.yaml", document)
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"apis", "create"},
		Arguments: []string{"-f", file, "--project", "p1"},
		Context:   module.Context{Name: "c1", Endpoint: "http://unused.invalid"},
	})
	if outcome.Problem == nil {
		t.Fatalf("an unknown metadata key was accepted: %+v", outcome.Result)
	}
	if !strings.Contains(outcome.Problem.Message, "metadata.namespace") {
		t.Fatalf("the refusal does not name the key: %q", outcome.Problem.Message)
	}
}

func TestApisCreateRefusesANonStringMetadataName(t *testing.T) {
	document := `apiVersion: gateway.api-platform.wso2.com/v1
kind: RestApi
metadata: { name: 123 }
spec:
  displayName: Hello
  version: v1
  context: /hello
  upstream: { main: { url: http://backend:8080 } }
`
	file := writeFixture(t, "metadata-name-not-string.yaml", document)
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"apis", "create"},
		Arguments: []string{"-f", file, "--project", "p1"},
		Context:   module.Context{Name: "c1", Endpoint: "http://unused.invalid"},
	})
	if outcome.Problem == nil {
		t.Fatalf("a non-string metadata.name was accepted: %+v", outcome.Result)
	}
	if !strings.Contains(outcome.Problem.Message, "metadata.name") {
		t.Fatalf("the refusal does not name metadata.name: %q", outcome.Problem.Message)
	}
}

func TestApisCreateRefusesANullUpstreamMainWithAProperMessage(t *testing.T) {
	document := `apiVersion: gateway.api-platform.wso2.com/v1
kind: RestApi
metadata: { name: hello-api }
spec:
  displayName: Hello
  version: v1
  context: /hello
  upstream:
    main: null
`
	file := writeFixture(t, "upstream-main-null.yaml", document)
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"apis", "create"},
		Arguments: []string{"-f", file, "--project", "p1"},
		Context:   module.Context{Name: "c1", Endpoint: "http://unused.invalid"},
	})
	if outcome.Problem == nil {
		t.Fatalf("a null spec.upstream.main was accepted: %+v", outcome.Result)
	}
	if !strings.Contains(outcome.Problem.Message, "spec.upstream.main must be a map") {
		t.Fatalf("the refusal does not give the proper message: %q", outcome.Problem.Message)
	}
}
