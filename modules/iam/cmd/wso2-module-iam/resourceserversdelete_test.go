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

const twoResourceServers = `{"totalResults":2,"resourceServers":[
	{"id":"rs-1","name":"System","identifier":"https://localhost:8090/mcp"},
	{"id":"rs-2","name":"Hello API","identifier":"http://localhost:8801/hello"}]}`

// deleteResourceServer runs the command with every confirmation answered yes,
// as a person at a terminal would.
func deleteResourceServer(t *testing.T, arguments ...string) (testkit.Outcome, []string) {
	t.Helper()
	return deleteResourceServerAnswering(t, &testkit.Confirmation{}, arguments...)
}

func deleteResourceServerAnswering(t *testing.T, confirm *testkit.Confirmation, arguments ...string) (testkit.Outcome, []string) {
	t.Helper()
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/resource-servers":
			_, _ = w.Write([]byte(twoResourceServers))
		case r.Method == http.MethodGet && r.URL.Path == "/resource-servers/rs-2/resources":
			_, _ = w.Write([]byte(`{"totalResults":1,"resources":[{"id":"p-1","handle":"read"}]}`))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(server.Close)
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"resource-server", "delete"},
		Arguments: arguments,
		Context:   module.Context{Name: "c1", Endpoint: server.URL},
		Access:    &testkit.Access{Token: "brokered-token"},
		Confirm:   confirm,
	})
	return outcome, calls
}

func TestDeletingAResourceServerByIdentifierRemovesItsPermissionsFirst(t *testing.T) {
	// Measured against ThunderID: permissions are child resources, and the
	// deployment refuses to delete a resource server that still holds any.
	outcome, calls := deleteResourceServer(t, "http://localhost:8801/hello", "--yes")
	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("resource-server delete failed: err=%v problem=%v", outcome.Err, outcome.Problem)
	}
	want := "GET /resource-servers,GET /resource-servers/rs-2/resources," +
		"DELETE /resource-servers/rs-2/resources/p-1,DELETE /resource-servers/rs-2"
	if strings.Join(calls, ",") != want {
		t.Fatalf("calls were:\n%s\nwant:\n%s", strings.Join(calls, ","), want)
	}
}

// TestDeletingAResourceServerAsksFirstAndHonoursEveryAnswer covers the four
// ways the deletion is confirmed or not: a yes at the terminal deletes, a no
// deletes nothing, --yes deletes without asking, and a run where nothing may
// be asked is refused with the shell's recovery naming --yes. The question is
// asked before anything is read, so a refused run calls nothing.
func TestDeletingAResourceServerAsksFirstAndHonoursEveryAnswer(t *testing.T) {
	const question = "Delete the resource server rs-2 and its permissions? This cannot be undone."

	t.Run("answered yes", func(t *testing.T) {
		outcome, calls := deleteResourceServerAnswering(t, &testkit.Confirmation{}, "rs-2")
		if outcome.Err != nil || outcome.Problem != nil {
			t.Fatalf("a confirmed deletion failed: err=%v problem=%v", outcome.Err, outcome.Problem)
		}
		if len(outcome.Confirmations) != 1 || outcome.Confirmations[0].Question != question {
			t.Fatalf("the shell was asked %+v, want %q once", outcome.Confirmations, question)
		}
		if last := calls[len(calls)-1]; last != "DELETE /resource-servers/rs-2" {
			t.Fatalf("calls were %v, want them to end with the delete", calls)
		}
	})
	t.Run("answered no", func(t *testing.T) {
		outcome, calls := deleteResourceServerAnswering(t, &testkit.Confirmation{Refuse: &testkit.Declined}, "rs-2")
		if outcome.Problem == nil || *outcome.Problem != testkit.Declined {
			t.Fatalf("a declined deletion was reported as %+v, want the shell's refusal unchanged", outcome.Problem)
		}
		if len(calls) != 0 {
			t.Fatalf("a declined deletion still called out: %v", calls)
		}
	})
	t.Run("with --yes", func(t *testing.T) {
		outcome, calls := deleteResourceServerAnswering(t, nil, "rs-2", "--yes")
		if outcome.Err != nil || outcome.Problem != nil {
			t.Fatalf("--yes did not delete: err=%v problem=%v", outcome.Err, outcome.Problem)
		}
		if len(outcome.Confirmations) != 0 {
			t.Fatalf("--yes still asked %+v", outcome.Confirmations)
		}
		if last := calls[len(calls)-1]; last != "DELETE /resource-servers/rs-2" {
			t.Fatalf("calls were %v, want them to end with the delete", calls)
		}
	})
	t.Run("under --no-input", func(t *testing.T) {
		refusal := testkit.NonInteractive("ws iam resource-server delete rs-2")
		outcome, calls := deleteResourceServerAnswering(t, &testkit.Confirmation{Refuse: refusal}, "rs-2")
		if outcome.Problem == nil || *outcome.Problem != *refusal {
			t.Fatalf("a run that may not ask was reported as %+v, want the shell's refusal unchanged", outcome.Problem)
		}
		if outcome.Problem.Code != "shell.non_interactive" || !strings.Contains(outcome.Problem.Recovery, "--yes") {
			t.Fatalf("the refusal is not shell.non_interactive naming --yes: %+v", outcome.Problem)
		}
		if len(calls) != 0 {
			t.Fatalf("a refused deletion still called out: %v", calls)
		}
	})
}

func TestDeletingAnUnknownResourceServerIsRefusedAfterOneRead(t *testing.T) {
	outcome, calls := deleteResourceServer(t, "no-such", "--yes")
	if outcome.Problem == nil || outcome.Problem.Code != "iam.not_found" {
		t.Fatalf("an unknown resource server was not refused: %+v", outcome.Problem)
	}
	if len(calls) != 1 {
		t.Fatalf("an unknown resource server still led to calls: %v", calls)
	}
}
