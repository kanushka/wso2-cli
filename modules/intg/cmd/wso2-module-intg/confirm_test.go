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

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/testkit"
)

// deletion is one destructive command and the question it asks.
type deletion struct {
	command  []string
	target   string
	route    string
	question string
}

var deletions = []deletion{
	{[]string{"project", "delete"}, "orders", "DELETE /projects/orders",
		"Delete the project orders and every integration in it? This cannot be undone."},
	{[]string{"integration", "delete"}, "order-api", "DELETE /components/order-api",
		"Delete the integration order-api, its builds and its deployments? This cannot be undone."},
}

// runAnswering runs a deletion with the shell answering its question as
// confirmation does.
func runAnswering(d deletion, endpoint string, confirmation *testkit.Confirmation, arguments ...string) testkit.Outcome {
	return testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   d.command,
		Arguments: append([]string{d.target}, arguments...),
		Context:   module.Context{Name: "wso2cloud", Endpoint: endpoint},
		Access:    &testkit.Access{Token: "t"},
		Confirm:   confirmation,
	})
}

func TestADeletionAsksFirstAndDeletesWhenConfirmed(t *testing.T) {
	for _, d := range deletions {
		stub := newPlatformStub(t, map[string]stubRoute{d.route: {status: http.StatusNoContent, body: ``}})

		outcome := runAnswering(d, stub.endpoint(), &testkit.Confirmation{})

		if outcome.Err != nil || outcome.Problem != nil {
			t.Fatalf("%v failed: %v %v", d.command, outcome.Err, outcome.Problem)
		}
		if len(outcome.Confirmations) != 1 || outcome.Confirmations[0].Question != d.question {
			t.Errorf("%v asked %+v, want %q", d.command, outcome.Confirmations, d.question)
		}
		if stub.method != http.MethodDelete || stub.calls != 1 {
			t.Errorf("%v made %d calls, the last %s, want one DELETE", d.command, stub.calls, stub.method)
		}
	}
}

func TestADeletionThatIsNotConfirmedCallsNothing(t *testing.T) {
	for _, d := range deletions {
		for _, refusal := range []*problem.Problem{&testkit.Declined,
			testkit.NonInteractive("wso2 intg " + strings.Join(d.command, " ") + " " + d.target)} {
			stub := newPlatformStub(t, nil)

			outcome := runAnswering(d, stub.endpoint(), &testkit.Confirmation{Refuse: refusal})

			// The shell's refusal is its policy, so it comes back unchanged.
			assertProblem(t, outcome, refusal.Code, problem.CategoryUsage)
			if outcome.Problem.Recovery != refusal.Recovery {
				t.Errorf("%v recovery = %q, want the shell's %q", d.command, outcome.Problem.Recovery, refusal.Recovery)
			}
			if stub.calls != 0 || len(outcome.AccessRequests) != 0 {
				t.Errorf("%v reached the API without confirmation (%v)", d.command, stub.paths)
			}
		}
	}
}

func TestADeletionGivenYesAsksNothing(t *testing.T) {
	for _, d := range deletions {
		stub := newPlatformStub(t, map[string]stubRoute{d.route: {status: http.StatusNoContent, body: ``}})

		// A nil Confirm refuses, so a question asked here would fail the run.
		outcome := runAnswering(d, stub.endpoint(), nil, "--yes")

		if outcome.Err != nil || outcome.Problem != nil {
			t.Fatalf("%v --yes failed: %v %v", d.command, outcome.Err, outcome.Problem)
		}
		if len(outcome.Confirmations) != 0 {
			t.Errorf("%v --yes asked %+v", d.command, outcome.Confirmations)
		}
	}
}
