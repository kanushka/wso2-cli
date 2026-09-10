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
	"testing"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/testkit"
)

// thunderStub answers the management API paths this module calls, and records
// the bearer token it was presented with so a test can prove the module sent
// the access the shell brokered rather than something of its own.
type thunderStub struct {
	*httptest.Server
	presented string
}

func newThunderStub(t *testing.T, routes map[string]string) *thunderStub {
	t.Helper()
	stub := &thunderStub{}
	stub.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.presented = r.Header.Get("Authorization")
		body, found := routes[r.URL.Path]
		if !found {
			http.Error(w, `{"code":"NOT_FOUND"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(stub.Close)
	return stub
}

func TestUsersListReportsEveryUserTheDeploymentReturns(t *testing.T) {
	stub := newThunderStub(t, map[string]string{
		"/users": `{"totalResults":2,"count":2,"users":[
			{"id":"u-1","type":"Person","attributes":{"username":"admin","email":"admin@example.test"}},
			{"id":"u-2","type":"Person","attributes":{"username":"hello","email":"hello@example.test"}}]}`,
	})
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command: []string{"users", "list"},
		Context: module.Context{Name: "c1", Endpoint: stub.URL},
		Access:  &testkit.Access{Token: "brokered-token"},
	})
	if outcome.Err != nil {
		t.Fatalf("the invocation failed: %v", outcome.Err)
	}
	if outcome.Problem != nil {
		t.Fatalf("users list returned the problem %v", outcome.Problem)
	}
	if outcome.Result == nil {
		t.Fatal("users list returned no result")
	}
	// The module must present exactly what the broker gave it. A module that
	// invented a credential of its own would defeat every check the shell made
	// before handing one over.
	if stub.presented != "Bearer brokered-token" {
		t.Fatalf("the module presented %q, want the brokered token", stub.presented)
	}
	if outcome.Result.Schema != UsersSchema {
		t.Errorf("schema = %q, want %q", outcome.Result.Schema, UsersSchema)
	}
}
