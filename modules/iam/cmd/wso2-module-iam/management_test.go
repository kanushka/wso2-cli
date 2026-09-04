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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/testkit"
)

// fakeManagement is enough of ThunderID's management API for the four
// command families: in-memory lists, name lookups, and a record of every
// POST body so a test can assert what was sent.
type fakeManagement struct {
	mu      sync.Mutex
	server  *httptest.Server
	servers []map[string]any
	res     map[string][]map[string]any // by resource server id
	apps    []map[string]any
	users   []map[string]any
	roles   []map[string]any
	posts   []string // "path body"
	seq     int
}

func newFakeManagement(t *testing.T) *fakeManagement {
	t.Helper()
	fake := &fakeManagement{res: map[string][]map[string]any{}}
	id := func() string { fake.seq++; return fmt.Sprintf("id-%d", fake.seq) }
	list := func(w http.ResponseWriter, key string, items []map[string]any) {
		_ = json.NewEncoder(w).Encode(map[string]any{"totalResults": len(items), key: items})
	}
	read := func(r *http.Request) map[string]any {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		raw, _ := json.Marshal(body)
		fake.posts = append(fake.posts, r.URL.Path+" "+string(raw))
		return body
	}
	mux := http.NewServeMux()
	guard := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+fixtureToken {
				http.Error(w, "unauthenticated", http.StatusUnauthorized)
				return
			}
			fake.mu.Lock()
			defer fake.mu.Unlock()
			next(w, r)
		}
	}
	mux.HandleFunc("GET /resource-servers", guard(func(w http.ResponseWriter, r *http.Request) { list(w, "resourceServers", fake.servers) }))
	mux.HandleFunc("POST /resource-servers", guard(func(w http.ResponseWriter, r *http.Request) {
		body := read(r)
		body["id"] = id()
		fake.servers = append(fake.servers, body)
		_ = json.NewEncoder(w).Encode(body)
	}))
	mux.HandleFunc("GET /resource-servers/{rs}/resources", guard(func(w http.ResponseWriter, r *http.Request) {
		// One level at a time, as the deployment does: top level without a
		// parent, children by parentId. A limit above 100 is refused.
		if limit := r.URL.Query().Get("limit"); limit == "" || limit > "100" && len(limit) > 2 {
			http.Error(w, `{"code":"RES-1011"}`, http.StatusBadRequest)
			return
		}
		parent := r.URL.Query().Get("parentId")
		var level []map[string]any
		for _, resource := range fake.res[r.PathValue("rs")] {
			p, _ := resource["parent"].(string)
			if p == parent {
				level = append(level, resource)
			}
		}
		list(w, "resources", level)
	}))
	mux.HandleFunc("POST /resource-servers/{rs}/resources", guard(func(w http.ResponseWriter, r *http.Request) {
		body := read(r)
		body["id"] = id()
		fake.res[r.PathValue("rs")] = append(fake.res[r.PathValue("rs")], body)
		_ = json.NewEncoder(w).Encode(body)
	}))
	mux.HandleFunc("GET /applications", guard(func(w http.ResponseWriter, r *http.Request) { list(w, "applications", fake.apps) }))
	mux.HandleFunc("POST /applications", guard(func(w http.ResponseWriter, r *http.Request) {
		body := read(r)
		body["id"] = id()
		fake.apps = append(fake.apps, body)
		_ = json.NewEncoder(w).Encode(body)
	}))
	mux.HandleFunc("GET /users", guard(func(w http.ResponseWriter, r *http.Request) { list(w, "users", fake.users) }))
	mux.HandleFunc("POST /users", guard(func(w http.ResponseWriter, r *http.Request) {
		body := read(r)
		body["id"] = id()
		fake.users = append(fake.users, body)
		_ = json.NewEncoder(w).Encode(body)
	}))
	mux.HandleFunc("GET /roles", guard(func(w http.ResponseWriter, r *http.Request) { list(w, "roles", fake.roles) }))
	mux.HandleFunc("POST /roles", guard(func(w http.ResponseWriter, r *http.Request) {
		body := read(r)
		body["id"] = id()
		fake.roles = append(fake.roles, body)
		_ = json.NewEncoder(w).Encode(body)
	}))
	mux.HandleFunc("POST /roles/{id}/assignments/add", guard(func(w http.ResponseWriter, r *http.Request) {
		read(r)
		w.WriteHeader(http.StatusOK)
	}))
	fake.server = httptest.NewServer(mux)
	t.Cleanup(fake.server.Close)
	return fake
}

const fixtureToken = "wso2-development-token.fixture"

func (f *fakeManagement) run(t *testing.T, command []string, arguments ...string) testkit.Outcome {
	t.Helper()
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   command,
		Arguments: arguments,
		Context:   module.Context{Name: "thunder-admin", Endpoint: f.server.URL},
		Access:    &testkit.Access{Token: fixtureToken, ExpiresAt: time.Now().Add(time.Minute)},
	})
	if outcome.Err != nil {
		t.Fatalf("%v: %v", command, outcome.Err)
	}
	return outcome
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

func (f *fakeManagement) postsTo(path string) []string {
	var matching []string
	for _, post := range f.posts {
		if strings.HasPrefix(post, path+" ") {
			matching = append(matching, strings.TrimPrefix(post, path+" "))
		}
	}
	return matching
}

func TestEveryManagementCommandAcquiresSystemAccessAndEndsWithNext(t *testing.T) {
	fake := newFakeManagement(t)
	for _, command := range [][]string{
		{"resource-servers", "list"}, {"users", "list"}, {"apps", "list"}, {"roles", "list"},
	} {
		outcome := fake.run(t, command)
		if outcome.Problem != nil {
			t.Fatalf("%v: %+v", command, outcome.Problem)
		}
		if len(outcome.AccessRequests) != 1 || outcome.AccessRequests[0].Audience != SystemAudience ||
			len(outcome.AccessRequests[0].Scopes) != 1 || outcome.AccessRequests[0].Scopes[0] != SystemScope {
			t.Errorf("%v asked for %+v", command, outcome.AccessRequests)
		}
		assertEndsWithNext(t, outcome)
	}
}

func TestResourceServerCreateBuildsThePermissionTreeAndReusesParents(t *testing.T) {
	fake := newFakeManagement(t)
	outcome := fake.run(t, []string{"resource-servers", "create"}, "Mock API",
		"--identifier", "http://localhost:18080/mockapi",
		"--permission", "reference:status:read", "--permission", "reference:status:write", "--permission", "orders:read")
	if outcome.Problem != nil {
		t.Fatalf("%+v", outcome.Problem)
	}
	fields := fieldsOf(outcome)
	if fields["created"] != "true" || fields["identifier"] != "http://localhost:18080/mockapi" ||
		!strings.Contains(fields["next"], `--resource-server "Mock API" --permission reference:status:read`) {
		t.Errorf("fields = %+v", fields)
	}
	created := fake.postsTo("/resource-servers/id-1/resources")
	// reference, status, read, write, orders, read: six nodes, parents reused.
	if len(created) != 6 || !strings.Contains(created[0], `"handle":"reference"`) ||
		!strings.Contains(created[3], `"handle":"write"`) || !strings.Contains(created[3], `"parent":"id-3"`) {
		t.Errorf("resources created = %v", created)
	}
	if server := fake.postsTo("/resource-servers"); len(server) != 1 || !strings.Contains(server[0], `"ouId":"`+DefaultOU+`"`) {
		t.Errorf("server posts = %v", server)
	}

	again := fake.run(t, []string{"resource-servers", "create"}, "Mock API",
		"--identifier", "http://localhost:18080/mockapi", "--permission", "orders:read")
	if fieldsOf(again)["created"] != "false" || len(fake.postsTo("/resource-servers")) != 1 ||
		len(fake.postsTo("/resource-servers/id-1/resources")) != 6 {
		t.Errorf("a second create was not idempotent: %+v %v", fieldsOf(again), fake.posts)
	}
}

func TestUserCreateReadsThePasswordFromTheEnvironmentOnly(t *testing.T) {
	fake := newFakeManagement(t)
	t.Setenv("WSO2_IAM_USER_PASSWORD", "")
	outcome := fake.run(t, []string{"users", "create"}, "cliuser", "--email", "cliuser@example.com")
	if outcome.Problem == nil || outcome.Problem.Code != "iam.missing_secret" {
		t.Fatalf("unset password: %+v", outcome.Problem)
	}
	t.Setenv("WSO2_IAM_USER_PASSWORD", "Cli@12345")
	outcome = fake.run(t, []string{"users", "create"}, "cliuser", "--email", "cliuser@example.com", "--given-name", "CLI")
	if outcome.Problem != nil {
		t.Fatalf("%+v", outcome.Problem)
	}
	posts := fake.postsTo("/users")
	if len(posts) != 1 || !strings.Contains(posts[0], `"password":"Cli@12345"`) ||
		!strings.Contains(posts[0], `"username":"cliuser"`) || !strings.Contains(posts[0], `"type":"Person"`) {
		t.Errorf("user posts = %v", posts)
	}
	for name, value := range fieldsOf(outcome) {
		if strings.Contains(value, "Cli@12345") {
			t.Errorf("the password leaked into the field %s", name)
		}
	}
	if fieldsOf(fake.run(t, []string{"users", "create"}, "cliuser", "--email", "x@y"))["created"] != "false" {
		t.Error("a second create was not idempotent")
	}
}

func TestAppCreateShowsAGeneratedSecretOnceAndAPublicClientNone(t *testing.T) {
	fake := newFakeManagement(t)
	outcome := fake.run(t, []string{"apps", "create"}, "wso2-cli-ci", "--type", "m2m")
	if outcome.Problem != nil {
		t.Fatalf("%+v", outcome.Problem)
	}
	fields := fieldsOf(outcome)
	posts := fake.postsTo("/applications")
	if fields["clientSecret"] == "" || fields["clientSecret"] == "(not shown again)" ||
		!strings.Contains(posts[0], `"clientSecret":"`+fields["clientSecret"]+`"`) ||
		!strings.Contains(posts[0], `"grantTypes":["client_credentials"]`) || !strings.Contains(posts[0], `"type":"m2m"`) ||
		!strings.Contains(fields["next"], "WSO2_THUNDER_CI_SECRET") {
		t.Errorf("m2m: fields %+v posts %v", fields, posts)
	}
	if fieldsOf(fake.run(t, []string{"apps", "create"}, "wso2-cli-ci", "--type", "m2m"))["clientSecret"] != "(not shown again)" {
		t.Error("the secret was shown twice")
	}
	public := fieldsOf(fake.run(t, []string{"apps", "create"}, "wso2-cli", "--type", "public"))
	posts = fake.postsTo("/applications")
	if public["clientSecret"] != "(none: public client)" || len(posts) != 2 ||
		!strings.Contains(posts[1], `"publicClient":true`) || !strings.Contains(posts[1], `10428/callback`) {
		t.Errorf("public: fields %+v posts %v", public, posts)
	}
	if outcome := fake.run(t, []string{"apps", "create"}, "x"); outcome.Problem == nil || outcome.Problem.Code != "iam.missing_flag" {
		t.Errorf("no type: %+v", outcome.Problem)
	}
}

func TestRoleCreateResolvesNamesAndAssignsAppsSeparately(t *testing.T) {
	fake := newFakeManagement(t)
	t.Setenv("WSO2_IAM_USER_PASSWORD", "Cli@12345")
	fake.run(t, []string{"resource-servers", "create"}, "Mock API", "--identifier", "http://x/mock", "--permission", "orders:read")
	fake.run(t, []string{"users", "create"}, "cliuser", "--email", "c@x")
	fake.run(t, []string{"apps", "create"}, "wso2-cli-ci", "--type", "m2m")

	outcome := fake.run(t, []string{"roles", "create"}, "Mock API Caller", "--resource-server", "Mock API",
		"--permission", "orders:read", "--assign-user", "cliuser", "--assign-app", "wso2-cli-ci")
	if outcome.Problem != nil {
		t.Fatalf("%+v", outcome.Problem)
	}
	role := fake.postsTo("/roles")
	if len(role) != 1 || !strings.Contains(role[0], `"resourceServerId":"id-1"`) ||
		!strings.Contains(role[0], `"assignments":[{"id":"id-4","type":"user"}]`) {
		t.Errorf("role posts = %v", role)
	}
	assigned := fake.postsTo("/roles/id-6/assignments/add")
	if len(assigned) != 1 || !strings.Contains(assigned[0], `{"id":"id-5","type":"app"}`) {
		t.Errorf("assignment posts = %v", assigned)
	}
	if fields := fieldsOf(outcome); fields["assigned"] != "user cliuser, app wso2-cli-ci" || fields["created"] != "true" {
		t.Errorf("fields = %+v", fields)
	}
	missing := fake.run(t, []string{"roles", "create"}, "Other", "--resource-server", "Nope", "--permission", "x")
	if missing.Problem == nil || missing.Problem.Code != "iam.not_found" || !strings.Contains(missing.Problem.Message, `"Nope"`) {
		t.Errorf("unknown server: %+v", missing.Problem)
	}
}

func TestACreateWithoutItsArgumentIsAUsageProblem(t *testing.T) {
	fake := newFakeManagement(t)
	outcome := fake.run(t, []string{"users", "create"}, "--email", "x@y")
	if outcome.Problem == nil || outcome.Problem.Code != "iam.missing_argument" {
		t.Errorf("%+v", outcome.Problem)
	}
}
