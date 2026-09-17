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
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/testkit"
)

const whoamiAnswer = `{"organization":"reference-org","audiences":"reference-status",` +
	`"scopes":"reference:status:read","invocation":"invocation-7f2a","boundTo":"reference-local"}`

// runWhoami invokes the module's whoami command against an endpoint, the way
// runCall invokes call: the whole tree is served so routing is exercised too.
func runWhoami(t *testing.T, endpoint string, access *testkit.Access) testkit.Outcome {
	t.Helper()
	return testkit.Run(t.Context(), moduleOptions(), commandTree().Commands(),
		testkit.Invocation{
			Command:      []string{"whoami"},
			InvocationID: invocationID,
			Context: module.Context{
				Name:           "reference-local",
				OrganizationID: "reference-org",
				Endpoint:       endpoint,
			},
			Access: access,
		})
}

func TestWhoamiReachesTheServiceWithTheBrokeredAccessOnly(t *testing.T) {
	service, seen := statusService(t, http.StatusOK, whoamiAnswer)

	outcome := runWhoami(t, service.URL, granted())

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
	if seen.path != whoamiPath {
		t.Errorf("the module called %q, want %q", seen.path, whoamiPath)
	}
	if seen.authorization != "Bearer "+fixtureToken {
		t.Errorf("the module presented %q, want the brokered token", seen.authorization)
	}
	if seen.invocation != invocationID {
		t.Errorf("the module named invocation %q, want %q", seen.invocation, invocationID)
	}
}

func TestWhoamiReturnsTheServicesAnswerAsSemanticFields(t *testing.T) {
	service, _ := statusService(t, http.StatusOK, whoamiAnswer)

	outcome := runWhoami(t, service.URL, granted())

	if outcome.Err != nil {
		t.Fatalf("the invocation failed: %v", outcome.Err)
	}
	if outcome.Result == nil {
		t.Fatalf("the module returned no result: %+v", outcome.Problem)
	}
	if outcome.Result.Schema != WhoamiSchema {
		t.Errorf("result schema is %q, want %q", outcome.Result.Schema, WhoamiSchema)
	}
	want := []struct{ name, value string }{
		{"organization", "reference-org"},
		{"audiences", "reference-status"},
		{"scopes", "reference:status:read"},
		{"invocation", "invocation-7f2a"},
		{"boundTo", "reference-local"},
	}
	if len(outcome.Result.Fields) != len(want)+1 {
		t.Fatalf("the result carries %d fields, want %d", len(outcome.Result.Fields), len(want)+1)
	}
	for index, field := range want {
		got := outcome.Result.Fields[index]
		if got.Name != field.name || got.Value != field.value {
			t.Errorf("field %d is %s=%q, want %s=%q", index, got.Name, got.Value, field.name, field.value)
		}
	}
	if last := outcome.Result.Fields[len(outcome.Result.Fields)-1]; last.Name != "expiresAt" || last.Value == "" {
		t.Errorf("the result does not end with expiresAt: %+v", last)
	}
}

func TestWhoamiADeniedRequestEndsTheCommandWithTheShellsDenial(t *testing.T) {
	service, seen := statusService(t, http.StatusOK, whoamiAnswer)
	denial := problem.New(problem.CategoryAuthPolicy, "auth.credential_unavailable",
		"the credential source the \"reference-local\" context names is not set").
		WithRecovery("Set WSO2_REFERENCE_DEV_CREDENTIAL to the credential for this context.")

	outcome := runWhoami(t, service.URL, &testkit.Access{Deny: &denial})

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

func TestWhoamiAContextWithNoEndpointCannotBeCalled(t *testing.T) {
	outcome := runWhoami(t, "", granted())

	failure := terminalProblem(t, outcome)
	if failure.Category != problem.CategoryUsage {
		t.Errorf("category is %q, want %q", failure.Category, problem.CategoryUsage)
	}
	if failure.Recovery == "" {
		t.Error("a context with no endpoint offers no recovery guidance")
	}
}

func TestWhoamiAFailingServiceBecomesAProductServiceProblem(t *testing.T) {
	service, _ := statusService(t, http.StatusInternalServerError,
		`{"code":"status_service.unavailable","message":"the service cannot verify access"}`)

	outcome := runWhoami(t, service.URL, granted())

	failure := terminalProblem(t, outcome)
	if failure.Category != problem.CategoryProductService {
		t.Errorf("category is %q, want %q", failure.Category, problem.CategoryProductService)
	}
	if failure.Code != "reference.status_unavailable" {
		t.Errorf("code is %q, want reference.status_unavailable", failure.Code)
	}
}

func TestWhoamiAServiceThatAnswersWithoutAVerifiedAudienceIsUnreadable(t *testing.T) {
	// Valid JSON, but the field readWhoami actually checks is blank: the
	// service answered, and answered without the claim the command exists to
	// report.
	service, _ := statusService(t, http.StatusOK,
		`{"organization":"reference-org","scopes":"reference:status:read"}`)

	outcome := runWhoami(t, service.URL, granted())

	failure := terminalProblem(t, outcome)
	if failure.Category != problem.CategoryProductService {
		t.Errorf("category is %q, want %q", failure.Category, problem.CategoryProductService)
	}
	if failure.Code != "reference.status_unavailable" {
		t.Errorf("code is %q, want reference.status_unavailable", failure.Code)
	}
	// unreadable's recovery differs from unavailable's: retrying an endpoint
	// that is not a status service cannot change the answer.
	if strings.Contains(failure.Recovery, "Retry the command.") {
		t.Errorf("an unreadable answer offers retry as recovery: %q", failure.Recovery)
	}
}
