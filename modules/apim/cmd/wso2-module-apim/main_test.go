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

// The module is driven through the module contract rather than by calling its
// handler, because the contract is what the shell speaks: a handler that returns
// the right fields inside a module that answers the wrong message is not a
// working module.
func TestStatusAnswersThroughTheContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"count":0,"list":[]}`)
	}))
	t.Cleanup(server.Close)
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command: []string{"status"},
		Context: module.Context{Name: "c1", Endpoint: server.URL},
		Access:  &testkit.Access{Token: "control-plane-token"},
	})

	// The exchange is checked before the answer. Result and Problem are both
	// nil when the handshake or the serve loop failed, so reading the result
	// first would panic instead of reporting what went wrong.
	if outcome.Err != nil {
		t.Fatalf("the invocation failed: %v", outcome.Err)
	}
	if outcome.Problem != nil {
		t.Fatalf("status returned the problem %v", outcome.Problem)
	}
	if outcome.Result == nil {
		t.Fatal("status returned no result")
	}
	if outcome.Result.Schema != StatusSchema {
		t.Errorf("schema = %q, want %q", outcome.Result.Schema, StatusSchema)
	}
	if len(outcome.Result.Fields) == 0 {
		t.Fatal("status returned no fields")
	}
	// The first field is what a table shows first, so the order is part of the
	// answer rather than an accident of how the result was built.
	if got := outcome.Result.Fields[0].Name; got != "namespace" {
		t.Errorf("the first field is %q, want %q", got, "namespace")
	}
	assertEndsWithNext(t, outcome)
}

// An unknown command has to be refused by the module rather than answered, so
// the shell can tell a typo from a command that failed.
func TestAnUnknownCommandIsRefused(t *testing.T) {
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command: []string{"nosuchcommand"},
	})

	if outcome.Err != nil {
		t.Fatalf("the invocation failed: %v", outcome.Err)
	}
	if outcome.Problem == nil {
		t.Fatalf("an unknown command was answered with %v", outcome.Result)
	}
}

// The tree this module serves is what the shell parses a command line against
// before the module is launched. A module whose declaration is empty still
// works, but the shell then cannot answer --help or name a mistyped command,
// and that is a regression this test catches.
func TestTheCommandTreeIsDeclared(t *testing.T) {
	declared := commands().Declare()
	if len(declared.Commands) == 0 {
		t.Fatal("the module declares no commands")
	}
}

// assertEndsWithNext pins the rule every result of this module follows: the
// last field says what a user most likely runs next.
func assertEndsWithNext(t *testing.T, outcome testkit.Outcome) {
	t.Helper()
	if outcome.Result == nil || len(outcome.Result.Fields) == 0 {
		t.Fatal("no result to check")
	}
	last := outcome.Result.Fields[len(outcome.Result.Fields)-1]
	if last.Name != NextField || last.Value == "" {
		t.Errorf("the result does not end with a next step: %+v", outcome.Result.Fields)
	}
}

// TestStatusWorksOutTheProjectStateAndNamesTheNextCommand proves status does
// what help reuses it for: with a reachable control plane it names the first
// project's listing, with none it names project create, and it exits 0 in
// either state because both are states to describe rather than failures.
func TestStatusWorksOutTheProjectStateAndNamesTheNextCommand(t *testing.T) {
	projects := `{"count":1,"list":[{"id":"proj-1","displayName":"Payments","organizationId":"org-1"}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v0.9/projects" {
			http.Error(w, `{"code":"NOT_FOUND"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, projects)
	}))
	t.Cleanup(server.Close)
	run := func() testkit.Outcome {
		return testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
			Command: []string{"status"},
			Context: module.Context{Name: "c1", Endpoint: server.URL},
			Access:  &testkit.Access{Token: "control-plane-token"},
		})
	}

	outcome := run()
	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("status failed: %v %v", outcome.Err, outcome.Problem)
	}
	if got := fieldValue(outcome, "projects"); got != "1" {
		t.Errorf("projects = %q, want 1", got)
	}
	if got := fieldValue(outcome, NextField); !strings.Contains(got, "wso2 apim api list --project proj-1") {
		t.Errorf("next = %q, want the first project's listing", got)
	}
	assertEndsWithNext(t, outcome)

	projects = `{"count":0,"list":[]}`
	outcome = run()
	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("status failed with no projects: %v %v", outcome.Err, outcome.Problem)
	}
	if got := fieldValue(outcome, NextField); !strings.Contains(got, "wso2 apim project create <name>") {
		t.Errorf("next = %q, want project create", got)
	}
}

// TestStatusFailsWhenTheProductCannotBeReached proves status exits non-zero,
// with the failure's own code and recovery, for every way the product can
// fail to answer: nothing recorded, access denied, and a platform that does
// not respond.
func TestStatusFailsWhenTheProductCannotBeReached(t *testing.T) {
	denial := problem.New(problem.CategoryAuthPolicy, "auth.login_required", "no session").
		WithRecovery("Run wso2 login to establish a session for this context.")
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	for _, testCase := range []struct {
		name, endpoint string
		access         *testkit.Access
		code           string
	}{
		{"nothing recorded", "", &testkit.Access{Token: "t"}, "apim.product_not_recorded"},
		{"access denied", "https://apim.example.test", &testkit.Access{Deny: &denial}, "auth.login_required"},
		{"no answer", closed.URL, &testkit.Access{Token: "t"}, "apim.deployment_unreachable"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
				Command: []string{"status"},
				Context: module.Context{Name: "c1", Endpoint: testCase.endpoint},
				Access:  testCase.access,
			})
			if outcome.Problem == nil || outcome.Problem.Code != testCase.code {
				t.Fatalf("problem = %+v, err = %v, want %s", outcome.Problem, outcome.Err, testCase.code)
			}
		})
	}
}

// TestEveryVisibleCommandDeclaresAnExampleAndCreateMarksItsRequiredFlags pins
// what the shell's help pages read from this tree: an example on every command
// a user can reach, and the required marks on wso2 apim api create's --file
// and --project, which the shell shows and the SDK enforces.
func TestEveryVisibleCommandDeclaresAnExampleAndCreateMarksItsRequiredFlags(t *testing.T) {
	declared := commands().Declare()
	for _, command := range declared.Commands {
		if command.Hidden {
			continue
		}
		if len(command.Examples()) == 0 {
			t.Errorf("wso2 apim %s declares no example", strings.Join(command.Path, " "))
		}
	}
	for _, command := range declared.Commands {
		if strings.Join(command.Path, " ") != "api create" {
			continue
		}
		var required []string
		for _, flag := range command.RequiredFlags() {
			required = append(required, flag.Name)
		}
		if strings.Join(required, ",") != "file,project" {
			t.Errorf("api create marks %v required, want file and project", required)
		}
	}
}

// fieldValue reads one field of an outcome's result by name.
func fieldValue(outcome testkit.Outcome, name string) string {
	if outcome.Result == nil {
		return ""
	}
	for _, field := range outcome.Result.Fields {
		if field.Name == name {
			return field.Value
		}
	}
	return ""
}
