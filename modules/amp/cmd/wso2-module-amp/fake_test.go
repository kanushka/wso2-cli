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
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/testkit"
)

const fixtureToken = "wso2-development-token.fixture"

// fakeAgentManager is enough of Agent Manager for this module: the project
// and agent listings under /api/v1, behind a bearer check. It records every
// request so tests can assert paths, and it can be seeded.
type fakeAgentManager struct {
	mu       sync.Mutex
	server   *httptest.Server
	projects map[string][]map[string]any            // by organization
	agents   map[string]map[string][]map[string]any // by organization, then project
	requests []string                               // "METHOD request-uri"
}

func newFakeAgentManager(t *testing.T) *fakeAgentManager {
	t.Helper()
	fake := &fakeAgentManager{
		projects: map[string][]map[string]any{},
		agents:   map[string]map[string][]map[string]any{},
	}
	bearer := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			fake.mu.Lock()
			defer fake.mu.Unlock()
			fake.requests = append(fake.requests, r.Method+" "+r.URL.RequestURI())
			if r.Header.Get("Authorization") != "Bearer "+fixtureToken {
				http.Error(w, `{"message":"unauthenticated"}`, http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/orgs/{org}/projects", bearer(func(w http.ResponseWriter, r *http.Request) {
		items, known := fake.projects[r.PathValue("org")]
		if !known {
			http.Error(w, `{"message":"organization not found"}`, http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"total": len(items), "limit": 10, "offset": 0, "projects": items})
	}))
	mux.HandleFunc("GET /api/v1/orgs/{org}/projects/{project}/agents", bearer(func(w http.ResponseWriter, r *http.Request) {
		items, known := fake.agents[r.PathValue("org")][r.PathValue("project")]
		if !known {
			http.Error(w, `{"message":"project not found"}`, http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"total": len(items), "limit": 10, "offset": 0, "agents": items})
	}))
	fake.server = httptest.NewServer(mux)
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeAgentManager) seed(org string, projects ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.projects[org] = []map[string]any{}
	f.agents[org] = map[string][]map[string]any{}
	for _, name := range projects {
		f.projects[org] = append(f.projects[org], map[string]any{
			"uuid": "p-" + name, "name": name, "displayName": strings.ToUpper(name[:1]) + name[1:],
			"createdAt": "2026-09-01T10:00:00Z",
		})
		f.agents[org][name] = []map[string]any{}
	}
}

func (f *fakeAgentManager) addAgent(org, project, name, status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agents[org][project] = append(f.agents[org][project], map[string]any{
		"uuid": "a-" + name, "name": name, "displayName": name, "status": status,
		"projectName": project, "createdAt": "2026-09-02T10:00:00Z",
	})
}

// run drives the module through the contract, as the shell would, with the
// fake as the recorded endpoint and access already granted.
func (f *fakeAgentManager) run(t *testing.T, organization string, command []string, arguments ...string) testkit.Outcome {
	t.Helper()
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   command,
		Arguments: arguments,
		Context:   module.Context{Name: "amp-dev", OrganizationID: organization, Endpoint: f.server.URL},
		Access:    &testkit.Access{Token: fixtureToken, ExpiresAt: time.Now().Add(time.Minute)},
	})
	if outcome.Err != nil {
		t.Fatalf("%v: %v", command, outcome.Err)
	}
	return outcome
}

func (f *fakeAgentManager) requestsTo(prefix string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var matched []string
	for _, r := range f.requests {
		if strings.HasPrefix(r, prefix) {
			matched = append(matched, r)
		}
	}
	return matched
}

func fieldsOf(outcome testkit.Outcome) map[string]string {
	fields := map[string]string{}
	if outcome.Result == nil {
		return fields
	}
	for _, field := range outcome.Result.Fields {
		fields[field.Name] = field.Value
	}
	return fields
}
