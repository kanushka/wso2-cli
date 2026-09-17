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
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/testkit"
)

func TestResourceServersListReportsWhatEachOneIsIdentifiedBy(t *testing.T) {
	// The identifier is the whole point of the listing: it is the value an
	// operator copies into an account's product record as the audience, and
	// the exchange then asks for it as a resource indicator.
	stub := newThunderStub(t, map[string]string{
		"/resource-servers": `{"totalResults":1,"resourceServers":[
			{"id":"rs-1","name":"System","identifier":"https://localhost:8090/mcp","delimiter":":"}]}`,
	})
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command: []string{"resource-servers", "list"},
		Context: module.Context{Name: "c1", Endpoint: stub.URL},
		Access:  &testkit.Access{Token: "brokered-token"},
	})
	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("resource-servers list failed: err=%v problem=%v", outcome.Err, outcome.Problem)
	}
	if !strings.Contains(renderFields(outcome), "https://localhost:8090/mcp") {
		t.Fatalf("the listing does not report the identifier:\n%s", renderFields(outcome))
	}
}

func TestCreatingAResourceServerRefusesAnIdentifierThatIsNotAnAbsoluteURI(t *testing.T) {
	// Measured against ThunderID 2026-09-09: a bare name comes back as
	// invalid_target only later, when a token exchange asks for it as an RFC
	// 8707 resource indicator — by which time the resource server exists and
	// the error names something else. Refusing here says the true cause once,
	// before anything is created.
	stub := newThunderStub(t, map[string]string{})
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"resource-servers", "create"},
		Arguments: []string{"Hello API", "--identifier", "not-a-uri"},
		Context:   module.Context{Name: "c1", Endpoint: stub.URL},
		Access:    &testkit.Access{Token: "brokered-token"},
	})
	if outcome.Problem == nil {
		t.Fatalf("a non-URI identifier was accepted: %+v", outcome.Result)
	}
	if !strings.Contains(outcome.Problem.Message, "absolute URI") {
		t.Fatalf("the refusal does not name the cause: %q", outcome.Problem.Message)
	}
}

func TestCreatingAResourceServerRefusesAPermissionCarryingTheDelimiter(t *testing.T) {
	// Also measured: ThunderID rejects a handle containing the resource
	// server's delimiter, and the refusal it returns names a delimiter the
	// caller never chose. Saying it here costs one comparison.
	stub := newThunderStub(t, map[string]string{})
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command: []string{"resource-servers", "create"},
		Arguments: []string{"Hello API", "--identifier", "https://hello.example.test",
			"--permission", "hello:read"},
		Context: module.Context{Name: "c1", Endpoint: stub.URL},
		Access:  &testkit.Access{Token: "brokered-token"},
	})
	if outcome.Problem == nil {
		t.Fatalf("a permission carrying the delimiter was accepted: %+v", outcome.Result)
	}
	if !strings.Contains(outcome.Problem.Message, "delimiter") {
		t.Fatalf("the refusal does not name the cause: %q", outcome.Problem.Message)
	}
}

// route is one path's canned answer for the routed stubs below.
type route struct {
	status int
	body   string
}

// newRoutedStub answers by method-and-path rather than by path alone, and
// records the body of the last POST it received, which is what the
// organization-unit tests need: they must prove what create sent, not merely
// that it succeeded.
func newRoutedStub(t *testing.T, routes map[string]route, captured *string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && captured != nil {
			body, _ := io.ReadAll(r.Body)
			*captured = string(body)
		}
		found, ok := routes[r.Method+" "+r.URL.Path]
		if !ok {
			http.Error(w, `{"code":"NOT_FOUND"}`, http.StatusNotFound)
			return
		}
		status := found.status
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(found.body))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestCreatingAResourceServerSendsTheOUIDFromASingleOUListing(t *testing.T) {
	// Thunder now requires ouId on create. When the deployment records exactly
	// one organization unit, that is the only sensible default, so a caller
	// creating a first resource server never has to look it up by hand.
	var captured string
	server := newRoutedStub(t, map[string]route{
		"GET /organization-units": {body: `{"totalResults":1,"organizationUnits":[
			{"id":"ou-default","handle":"default","name":"Default"}]}`},
		"POST /resource-servers": {status: http.StatusCreated,
			body: `{"id":"rs-1","name":"Hello API","identifier":"http://localhost:8801/hello",` +
				`"delimiter":":","ouId":"ou-default"}`},
	}, &captured)

	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"resource-servers", "create"},
		Arguments: []string{"Hello API", "--identifier", "http://localhost:8801/hello"},
		Context:   module.Context{Name: "c1", Endpoint: server.URL},
		Access:    &testkit.Access{Token: "brokered-token"},
	})
	if outcome.Problem != nil {
		t.Fatalf("create was refused: %+v", outcome.Problem)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(captured), &sent); err != nil {
		t.Fatalf("the request body is not valid JSON: %v\n%s", err, captured)
	}
	if sent["ouId"] != "ou-default" {
		t.Fatalf("the request did not carry ouId from the single organization unit: %s", captured)
	}
}

func TestCreatingAResourceServerUsesAnOUIDGivenDirectly(t *testing.T) {
	// --ou given as an id names the organization unit outright, without
	// falling back to whatever the deployment's listing would default to.
	var captured string
	server := newRoutedStub(t, map[string]route{
		"GET /organization-units": {body: `{"totalResults":2,"organizationUnits":[
			{"id":"ou-1","handle":"default","name":"Default"},
			{"id":"ou-2","handle":"marketing","name":"Marketing"}]}`},
		"POST /resource-servers": {status: http.StatusCreated,
			body: `{"id":"rs-1","name":"Hello API","identifier":"http://localhost:8801/hello",` +
				`"delimiter":":","ouId":"ou-2"}`},
	}, &captured)

	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"resource-servers", "create"},
		Arguments: []string{"Hello API", "--identifier", "http://localhost:8801/hello", "--ou", "ou-2"},
		Context:   module.Context{Name: "c1", Endpoint: server.URL},
		Access:    &testkit.Access{Token: "brokered-token"},
	})
	if outcome.Problem != nil {
		t.Fatalf("create was refused: %+v", outcome.Problem)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(captured), &sent); err != nil {
		t.Fatalf("the request body is not valid JSON: %v\n%s", err, captured)
	}
	if sent["ouId"] != "ou-2" {
		t.Fatalf("the request did not carry the --ou id given: %s", captured)
	}
}

func TestCreatingAResourceServerResolvesAnOUHandle(t *testing.T) {
	// --ou given as a handle is resolved through the same listing to Thunder's
	// own id, because a handle is what an operator recognizes and an id is
	// what Thunder's API requires.
	var captured string
	server := newRoutedStub(t, map[string]route{
		"GET /organization-units": {body: `{"totalResults":2,"organizationUnits":[
			{"id":"ou-1","handle":"default","name":"Default"},
			{"id":"ou-2","handle":"marketing","name":"Marketing"}]}`},
		"POST /resource-servers": {status: http.StatusCreated,
			body: `{"id":"rs-1","name":"Hello API","identifier":"http://localhost:8801/hello",` +
				`"delimiter":":","ouId":"ou-2"}`},
	}, &captured)

	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command: []string{"resource-servers", "create"},
		Arguments: []string{"Hello API", "--identifier", "http://localhost:8801/hello",
			"--ou", "marketing"},
		Context: module.Context{Name: "c1", Endpoint: server.URL},
		Access:  &testkit.Access{Token: "brokered-token"},
	})
	if outcome.Problem != nil {
		t.Fatalf("create was refused: %+v", outcome.Problem)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(captured), &sent); err != nil {
		t.Fatalf("the request body is not valid JSON: %v\n%s", err, captured)
	}
	if sent["ouId"] != "ou-2" {
		t.Fatalf("the handle marketing was not resolved to its id: %s", captured)
	}
}

func TestCreatingAResourceServerWithMultipleOUsAndNoOURefuses(t *testing.T) {
	// A deployment recording more than one organization unit has no honest
	// default; guessing would silently place the resource server somewhere
	// the caller never chose.
	server := newRoutedStub(t, map[string]route{
		"GET /organization-units": {body: `{"totalResults":2,"organizationUnits":[
			{"id":"ou-1","handle":"default","name":"Default"},
			{"id":"ou-2","handle":"marketing","name":"Marketing"}]}`},
	}, nil)

	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"resource-servers", "create"},
		Arguments: []string{"Hello API", "--identifier", "http://localhost:8801/hello"},
		Context:   module.Context{Name: "c1", Endpoint: server.URL},
		Access:    &testkit.Access{Token: "brokered-token"},
	})
	if outcome.Problem == nil {
		t.Fatalf("an ambiguous default organization unit was accepted: %+v", outcome.Result)
	}
	for _, want := range []string{"--ou", "default (ou-1)", "marketing (ou-2)"} {
		if !strings.Contains(outcome.Problem.Message, want) {
			t.Fatalf("the refusal does not mention %q: %q", want, outcome.Problem.Message)
		}
	}
}

func TestCreatingAResourceServerRendersThunderFieldErrorsFromA400(t *testing.T) {
	// Thunder's field errors were dropped before this fix: only the generic
	// "Validation Failed" message reached the user. The description and the
	// per-field errors are what actually explain the refusal.
	server := newRoutedStub(t, map[string]route{
		"GET /organization-units": {body: `{"totalResults":1,"organizationUnits":[
			{"id":"ou-default","handle":"default","name":"Default"}]}`},
		"POST /resource-servers": {status: http.StatusBadRequest,
			body: `{"code":"INVALID_INPUT_METADATA","description":"One or more inbound fields ` +
				`failed structural edge-boundary rules.","errors":{"ouId":"The field 'ouId' is ` +
				`missing but is strictly required."},"message":"Validation Failed"}`},
	}, nil)

	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"resource-servers", "create"},
		Arguments: []string{"Hello API", "--identifier", "http://localhost:8801/hello"},
		Context:   module.Context{Name: "c1", Endpoint: server.URL},
		Access:    &testkit.Access{Token: "brokered-token"},
	})
	if outcome.Problem == nil {
		t.Fatalf("a 400 was not surfaced as a problem: %+v", outcome.Result)
	}
	if !strings.Contains(outcome.Problem.Message, "ouId: The field 'ouId' is missing") {
		t.Fatalf("the problem does not render the field error: %q", outcome.Problem.Message)
	}
	if !strings.Contains(outcome.Problem.Message, "structural edge-boundary rules") {
		t.Fatalf("the problem does not render Thunder's description: %q", outcome.Problem.Message)
	}
	if strings.Contains(outcome.Problem.Recovery, "deployment's own logs") {
		t.Fatalf("a 400 still points at the deployment's logs: %q", outcome.Problem.Recovery)
	}
}

// renderFields flattens a result's values so a test can assert on what a
// rendering would show without depending on field or column order. A listing
// carries its answer in rows, so both halves are flattened.
func renderFields(outcome testkit.Outcome) string {
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
