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
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/testkit"
)

const (
	fixtureToken = "wso2-development-token.fixture"
	invocationID = "invocation-7f2a"
)

// serviceCall is what the module sent to the status service.
type serviceCall struct {
	method        string
	path          string
	authorization string
	invocation    string
}

// statusService answers as the local status service would, recording the call.
func statusService(t *testing.T, status int, body string) (*httptest.Server, *serviceCall) {
	t.Helper()
	seen := &serviceCall{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		seen.method = request.Method
		seen.path = request.URL.Path
		seen.authorization = request.Header.Get("Authorization")
		seen.invocation = request.Header.Get(invocationHeader)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		_, _ = writer.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server, seen
}

const operational = `{"organization":"reference-org","service":"reference",` +
	`"status":"operational","checkedAt":"2026-07-27T10:00:00Z"}`

// runStatus invokes the module's status command against an endpoint.
func runStatus(t *testing.T, endpoint string, access *testkit.Access) testkit.Outcome {
	t.Helper()
	// The whole tree is served, as the shell serves it, so the test exercises
	// the routing the module actually ships rather than one handler in
	// isolation.
	return testkit.Run(t.Context(), moduleOptions(), commands(),
		testkit.Invocation{
			Command:      []string{"status"},
			InvocationID: invocationID,
			Context: module.Context{
				Name:           "reference-local",
				OrganizationID: "reference-org",
				Endpoint:       endpoint,
			},
			Access: access,
		})
}

func granted() *testkit.Access {
	return &testkit.Access{Token: fixtureToken, ExpiresAt: time.Now().Add(2 * time.Minute)}
}

func TestStatusCallsTheServiceWithTheBrokeredAccessOnly(t *testing.T) {
	service, seen := statusService(t, http.StatusOK, operational)

	outcome := runStatus(t, service.URL, granted())

	if outcome.Err != nil {
		t.Fatalf("the invocation failed: %v", outcome.Err)
	}
	if outcome.Problem != nil {
		t.Fatalf("the module returned a problem: %+v", *outcome.Problem)
	}
	if len(outcome.AccessRequests) != 1 {
		t.Fatalf("the module made %d access requests, want 1", len(outcome.AccessRequests))
	}
	asked := outcome.AccessRequests[0]
	if asked.Audience != StatusAudience {
		t.Errorf("the module asked for audience %q, want %q", asked.Audience, StatusAudience)
	}
	if len(asked.Scopes) != 1 || asked.Scopes[0] != StatusScope {
		t.Errorf("the module asked for scopes %v, want [%s]", asked.Scopes, StatusScope)
	}
	if seen.method != http.MethodGet {
		t.Errorf("the module called the service with %s, want a read", seen.method)
	}
	if seen.authorization != "Bearer "+fixtureToken {
		t.Errorf("the module presented %q, want the brokered token", seen.authorization)
	}
	if seen.invocation != invocationID {
		t.Errorf("the module named invocation %q, want %q", seen.invocation, invocationID)
	}
}

func TestStatusReturnsTheServicesAnswerAsSemanticFields(t *testing.T) {
	service, _ := statusService(t, http.StatusOK, operational)

	outcome := runStatus(t, service.URL, granted())

	if outcome.Err != nil {
		t.Fatalf("the invocation failed: %v", outcome.Err)
	}
	if outcome.Result == nil {
		t.Fatalf("the module returned no result: %+v", outcome.Problem)
	}
	if outcome.Result.Schema != StatusSchema {
		t.Errorf("result schema is %q, want %q", outcome.Result.Schema, StatusSchema)
	}
	want := []struct{ name, value string }{
		{"organization", "reference-org"},
		{"service", "reference"},
		{"status", "operational"},
		{"checkedAt", "2026-07-27T10:00:00Z"},
	}
	if len(outcome.Result.Fields) != len(want) {
		t.Fatalf("the result carries %d fields, want %d", len(outcome.Result.Fields), len(want))
	}
	for index, field := range want {
		got := outcome.Result.Fields[index]
		if got.Name != field.name || got.Value != field.value {
			t.Errorf("field %d is %s=%q, want %s=%q", index, got.Name, got.Value, field.name, field.value)
		}
	}
}

func TestADeniedRequestEndsTheCommandWithTheShellsDenial(t *testing.T) {
	// The module adds nothing to a refusal that is the shell's to make.
	service, seen := statusService(t, http.StatusOK, operational)
	denial := problem.New(problem.CategoryAuthPolicy, "auth.credential_unavailable",
		"the credential source the \"reference-local\" context names is not set").
		WithRecovery("Set WSO2_REFERENCE_DEV_CREDENTIAL to the credential for this context.")

	outcome := runStatus(t, service.URL, &testkit.Access{Deny: &denial})

	if outcome.Err != nil {
		t.Fatalf("the invocation failed: %v", outcome.Err)
	}
	if outcome.Problem == nil {
		t.Fatal("a denied invocation returned no problem")
	}
	if *outcome.Problem != denial {
		t.Errorf("the module returned %+v, want the shell's denial %+v", *outcome.Problem, denial)
	}
	if seen.method != "" {
		t.Error("the module called the status service without access")
	}
}

func TestAFailingServiceBecomesAProductServiceProblem(t *testing.T) {
	service, _ := statusService(t, http.StatusInternalServerError,
		`{"code":"status_service.unavailable","message":"the service cannot read its status"}`)

	outcome := runStatus(t, service.URL, granted())

	failure := terminalProblem(t, outcome)
	if failure.Category != problem.CategoryProductService {
		t.Errorf("category is %q, want %q", failure.Category, problem.CategoryProductService)
	}
	if failure.Code != "reference.status_unavailable" {
		t.Errorf("code is %q, want reference.status_unavailable", failure.Code)
	}
}

func TestAServiceThatRefusesTheAccessBecomesADistinctProblem(t *testing.T) {
	// The shell granted access and the service would not take it. That is the
	// service's answer, not shell policy, so it is reported apart from a
	// broker denial.
	service, _ := statusService(t, http.StatusForbidden,
		`{"code":"status_service.access_not_accepted","message":"not for this organization"}`)

	outcome := runStatus(t, service.URL, granted())

	failure := terminalProblem(t, outcome)
	if failure.Category != problem.CategoryProductService {
		t.Errorf("category is %q, want %q", failure.Category, problem.CategoryProductService)
	}
	if failure.Code != "reference.status_access_rejected" {
		t.Errorf("code is %q, want reference.status_access_rejected", failure.Code)
	}
}

func TestAnUnreachableServiceBecomesAProductServiceProblem(t *testing.T) {
	service, _ := statusService(t, http.StatusOK, operational)
	// The service is stopped before the call, so the endpoint is well formed
	// and nothing is listening on it.
	endpoint := service.URL
	service.Close()

	outcome := runStatus(t, endpoint, granted())

	failure := terminalProblem(t, outcome)
	if failure.Category != problem.CategoryProductService {
		t.Errorf("category is %q, want %q", failure.Category, problem.CategoryProductService)
	}
	if failure.Recovery == "" {
		t.Error("an unreachable service offers no recovery guidance")
	}
}

func TestAContextWithNoEndpointCannotBeCalled(t *testing.T) {
	outcome := runStatus(t, "", granted())

	failure := terminalProblem(t, outcome)
	if failure.Category != problem.CategoryUsage {
		t.Errorf("category is %q, want %q", failure.Category, problem.CategoryUsage)
	}
	if failure.Recovery == "" {
		t.Error("a context with no endpoint offers no recovery guidance")
	}
}

func TestNoFailureRepeatsTheAccessMaterial(t *testing.T) {
	// A problem is rendered verbatim by the shell, so nothing that could be
	// mistaken for access material may reach one.
	service, _ := statusService(t, http.StatusForbidden, `{"code":"status_service.access_not_accepted"}`)

	outcome := runStatus(t, service.URL, granted())

	failure := terminalProblem(t, outcome)
	if strings.Contains(failure.Message+failure.Recovery, fixtureToken) {
		t.Fatalf("a problem repeats the access material: %+v", failure)
	}
}

func terminalProblem(t *testing.T, outcome testkit.Outcome) problem.Problem {
	t.Helper()
	if outcome.Err != nil {
		t.Fatalf("the invocation failed: %v", outcome.Err)
	}
	if outcome.Problem == nil {
		t.Fatalf("the module returned no problem: %+v", outcome.Result)
	}
	if outcome.Problem.Message == "" || outcome.Problem.Code == "" {
		t.Errorf("the problem %+v is not reportable", *outcome.Problem)
	}
	return *outcome.Problem
}
