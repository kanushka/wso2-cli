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

func deleteResourceServer(t *testing.T, arguments ...string) (testkit.Outcome, []string) {
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
		Command:   []string{"resource-servers", "delete"},
		Arguments: arguments,
		Context:   module.Context{Name: "c1", Endpoint: server.URL},
		Access:    &testkit.Access{Token: "brokered-token"},
	})
	return outcome, calls
}

func TestDeletingAResourceServerByIdentifierRemovesItsPermissionsFirst(t *testing.T) {
	// Measured against ThunderID: permissions are child resources, and the
	// deployment refuses to delete a resource server that still holds any.
	outcome, calls := deleteResourceServer(t, "http://localhost:8801/hello", "--yes")
	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("resource-servers delete failed: err=%v problem=%v", outcome.Err, outcome.Problem)
	}
	want := "GET /resource-servers,GET /resource-servers/rs-2/resources," +
		"DELETE /resource-servers/rs-2/resources/p-1,DELETE /resource-servers/rs-2"
	if strings.Join(calls, ",") != want {
		t.Fatalf("calls were:\n%s\nwant:\n%s", strings.Join(calls, ","), want)
	}
}

func TestDeletingAResourceServerRefusesWithoutYesAndAnUnknownOne(t *testing.T) {
	outcome, calls := deleteResourceServer(t, "rs-2")
	if outcome.Problem == nil || !strings.Contains(outcome.Problem.Recovery, "--yes") {
		t.Fatalf("the deletion ran unconfirmed: %+v", outcome.Problem)
	}
	if len(calls) != 0 {
		t.Fatalf("the module called out before the deletion was confirmed: %v", calls)
	}

	outcome, calls = deleteResourceServer(t, "no-such", "--yes")
	if outcome.Problem == nil || outcome.Problem.Code != "identity.not_found" {
		t.Fatalf("an unknown resource server was not refused: %+v", outcome.Problem)
	}
	if len(calls) != 1 {
		t.Fatalf("an unknown resource server still led to calls: %v", calls)
	}
}
