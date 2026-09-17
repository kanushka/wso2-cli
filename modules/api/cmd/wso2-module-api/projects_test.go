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

func TestProjectsCreateSendsOnlyDisplayNameByDefault(t *testing.T) {
	var captured string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v0.9/projects" {
			http.Error(w, `{"code":"NOT_FOUND"}`, http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		captured = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"hello-project","displayName":"Hello Project","organizationId":"default"}`))
	}))
	t.Cleanup(server.Close)

	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"projects", "create"},
		Arguments: []string{"Hello Project"},
		Context:   module.Context{Name: "c1", Endpoint: server.URL},
		Access:    &testkit.Access{Token: "control-plane-token"},
	})
	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("projects create failed: err=%v problem=%v", outcome.Err, outcome.Problem)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(captured), &sent); err != nil {
		t.Fatalf("body is not JSON: %v\n%s", err, captured)
	}
	want := map[string]any{"displayName": "Hello Project"}
	gotJSON, _ := json.Marshal(sent)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("request body was:\n%s\nwant:\n%s", gotJSON, wantJSON)
	}
	if !strings.Contains(rendered(outcome), "hello-project") {
		t.Fatalf("result does not report the created project's id:\n%s", rendered(outcome))
	}
}

func TestProjectsCreateSendsDescriptionWhenGiven(t *testing.T) {
	var captured string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"p1","displayName":"P1","organizationId":"default"}`))
	}))
	t.Cleanup(server.Close)

	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"projects", "create"},
		Arguments: []string{"P1", "--description", "A project"},
		Context:   module.Context{Name: "c1", Endpoint: server.URL},
		Access:    &testkit.Access{Token: "control-plane-token"},
	})
	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("projects create failed: err=%v problem=%v", outcome.Err, outcome.Problem)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(captured), &sent); err != nil {
		t.Fatalf("body is not JSON: %v\n%s", err, captured)
	}
	if sent["description"] != "A project" {
		t.Fatalf("description was not sent: %s", captured)
	}
}

func TestProjectsCreateNeedsExactlyOneName(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(server.Close)

	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command: []string{"projects", "create"},
		Context: module.Context{Name: "c1", Endpoint: server.URL},
		Access:  &testkit.Access{Token: "control-plane-token"},
	})
	if outcome.Problem == nil {
		t.Fatalf("projects create ran without a name: %+v", outcome.Result)
	}
	if called {
		t.Fatal("the module called out despite a missing name")
	}
}

func TestProjectsCreateRefusesA409NamingTheExistingProject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"PROJECT_EXISTS","message":"a project with this id already exists"}`))
	}))
	t.Cleanup(server.Close)

	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"projects", "create"},
		Arguments: []string{"Hello Project"},
		Context:   module.Context{Name: "c1", Endpoint: server.URL},
		Access:    &testkit.Access{Token: "control-plane-token"},
	})
	if outcome.Problem == nil {
		t.Fatalf("a 409 was not surfaced as a problem: %+v", outcome.Result)
	}
	if outcome.Problem.Code != "api.project_exists" {
		t.Fatalf("problem code = %q, want api.project_exists", outcome.Problem.Code)
	}
	if !strings.Contains(outcome.Problem.Message, "Hello Project") {
		t.Fatalf("the refusal does not name the project: %q", outcome.Problem.Message)
	}
	if !strings.Contains(outcome.Problem.Recovery, "wso2 api projects list") {
		t.Fatalf("the recovery does not point at the listing: %q", outcome.Problem.Recovery)
	}
}
