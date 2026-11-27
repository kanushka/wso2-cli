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

package app_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
)

// fakePlatform is WSO2 Cloud's platform service, holding projects by id in
// the order it lists them.
type fakePlatform struct {
	mutex    sync.Mutex
	ids      []string
	names    map[string]string
	pageSize int
	// refuse answers every request with this status and reason when set.
	refuse       int
	refuseReason string
	requests     []string
	created      []map[string]any
	deleted      []string
	deleting     map[string]bool
	// stuckCursor answers every page with the same next cursor.
	stuckCursor bool
}

func newFakePlatform(t *testing.T, names ...string) (*fakePlatform, string) {
	t.Helper()
	platform := &fakePlatform{names: map[string]string{}, pageSize: 100}
	for i := 0; i+1 < len(names); i += 2 {
		platform.ids = append(platform.ids, names[i])
		platform.names[names[i]] = names[i+1]
	}
	server := httptest.NewServer(http.HandlerFunc(platform.serve))
	t.Cleanup(server.Close)
	return platform, server.URL
}

func (p *fakePlatform) serve(w http.ResponseWriter, r *http.Request) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.requests = append(p.requests, r.Method+" "+r.URL.RequestURI())
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if p.refuse != 0 {
		w.WriteHeader(p.refuse)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": p.refuseReason})
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/admin/projects":
		start := 0
		if cursor := r.URL.Query().Get("cursor"); cursor != "" {
			start = slices.Index(p.ids, cursor)
		}
		end := min(start+p.pageSize, len(p.ids))
		items := []map[string]any{}
		for _, id := range p.ids[start:end] {
			metadata := map[string]any{"name": id,
				"annotations": map[string]string{"openchoreo.dev/display-name": p.names[id]}}
			if p.deleting[id] {
				metadata["deletionTimestamp"] = "2026-11-27T00:00:00Z"
			}
			items = append(items, map[string]any{
				"metadata": metadata,
				"spec":     map[string]any{"deploymentPipelineRef": map[string]string{"name": "default"}},
			})
		}
		next := ""
		if end < len(p.ids) {
			next = p.ids[end]
		}
		if p.stuckCursor {
			next = p.ids[len(p.ids)-1]
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items,
			"pagination": map[string]string{"nextCursor": next}})
	case r.Method == http.MethodPost && r.URL.Path == "/admin/projects":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		p.created = append(p.created, body)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(body)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/admin/projects/"):
		p.deleted = append(p.deleted, strings.TrimPrefix(r.URL.Path, "/admin/projects/"))
		w.WriteHeader(http.StatusOK)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// newSignedInProjects is a signed-in WSO2 Cloud context whose platform
// service is platform.
func newSignedInProjects(t *testing.T, names ...string) (*cloudLogin, *fakePlatform) {
	t.Helper()
	return newSignedInProjectsWith(t, fakeissuer.Options{}, names...)
}

func newSignedInProjectsWith(t *testing.T, options fakeissuer.Options, names ...string) (*cloudLogin, *fakePlatform) {
	t.Helper()
	login := newCloudLoginWith(t, options)
	installCloudAPIM(t, login.shell)
	login.run(t, "login")
	platform, endpoint := newFakePlatform(t, names...)
	login.shell.PlatformURL = endpoint
	login.mutex.Lock()
	login.opened = 0
	login.mutex.Unlock()
	return login, platform
}

func (c *cloudLogin) runFailing(t *testing.T, want exit.Code, args ...string) {
	t.Helper()
	c.out.Reset()
	c.errOut.Reset()
	if code := c.shell.Run(args); code != want {
		t.Fatalf("%v: exit %d, want %d; stdout %s stderr %s", args, code, want, c.out, c.errOut)
	}
}

func setProject(t *testing.T, shell app.Shell, project string) {
	t.Helper()
	if err := contexts.Update(shell.StateRoot, func(document contexts.Document) (contexts.Document, error) {
		for i := range document.Contexts {
			document.Contexts[i].Project = project
		}
		return document, nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestProjectListPagesThroughEveryProjectWithoutABrowser(t *testing.T) {
	login, platform := newSignedInProjects(t, "retail", "Retail", "payments", "Payments", "hr", "HR")
	platform.pageSize = 2
	setProject(t, login.shell, "payments")
	login.out.Reset()

	login.run(t, "project", "list", "-o", "json")

	var listed struct {
		Count string              `json:"count"`
		Rows  []map[string]string `json:"rows"`
		Next  []string            `json:"next"`
	}
	if err := json.Unmarshal(login.out.Bytes(), &listed); err != nil {
		t.Fatalf("not one JSON document: %v\n%s", err, login.out)
	}
	var ids, marked []string
	for _, row := range listed.Rows {
		ids = append(ids, row["id"])
		if row["selected"] == "*" {
			marked = append(marked, row["id"])
		}
	}
	if listed.Count != "3" || !slices.Equal(ids, []string{"retail", "payments", "hr"}) {
		t.Errorf("listed %s %v, want all three projects across both pages", listed.Count, ids)
	}
	if !slices.Equal(marked, []string{"payments"}) {
		t.Errorf("marked %v, want the selected project alone", marked)
	}
	if len(listed.Next) == 0 || listed.Next[0] != "wso2 project use retail" {
		t.Errorf("next = %v, want the use command", listed.Next)
	}
	if login.opened != 0 {
		t.Errorf("the browser opened %d times; the platform session comes from the device secret", login.opened)
	}
	ref := contexts.ProductSessionRef(cloudContext(t, login.shell).CredentialRef, contexts.PlatformKey)
	stored, err := session.Store{StateRoot: login.shell.StateRoot}.Load(ref)
	if err != nil || stored.Resource != contexts.PlatformAudience ||
		!slices.Equal(stored.Scopes, contexts.PlatformScopes) {
		t.Errorf("platform session = %+v, %v; want one bound to %s with the project scopes", stored, err,
			contexts.PlatformAudience)
	}
	if _, recorded := cloudContext(t, login.shell).Products[contexts.PlatformKey]; recorded {
		t.Error("the platform access was written to the context file")
	}
}

func TestProjectUseTakesANameAndStoresTheID(t *testing.T) {
	login, _ := newSignedInProjects(t, "retail", "Retail Store")

	login.run(t, "project", "use", "retail store")

	if got := cloudContext(t, login.shell).Project; got != "retail" {
		t.Errorf("project = %q, want the id retail", got)
	}
}

func TestProjectUseRefusesAnUnknownProjectAsNotFound(t *testing.T) {
	login, _ := newSignedInProjects(t, "retail", "Retail")

	login.runFailing(t, exit.NotFound, "project", "use", "nope", "-o", "json")

	if !strings.Contains(login.out.String(), `"wso2 project list"`) {
		t.Errorf("the refusal does not name the list in next:\n%s", login.out)
	}
	if got := cloudContext(t, login.shell).Project; got != "" {
		t.Errorf("project = %q, want it left unset", got)
	}
}

func TestProjectCurrentAnswersFromTheContextAlone(t *testing.T) {
	login, platform := newSignedInProjects(t)
	setProject(t, login.shell, "retail")
	login.out.Reset()

	login.run(t, "project", "current", "-o", "json")

	if !strings.Contains(login.out.String(), `"project": "retail"`) {
		t.Errorf("current = %s, want the stored id", login.out)
	}
	if len(platform.requests) != 0 {
		t.Errorf("current called the platform service: %v", platform.requests)
	}
}

func TestProjectCreateDerivesTheIDAndDoesNotSelectIt(t *testing.T) {
	login, platform := newSignedInProjects(t)

	login.run(t, "project", "create", "Customer Care  Portal!", "--description", "Support")

	if len(platform.created) != 1 {
		t.Fatalf("created %v, want one project", platform.created)
	}
	body, _ := json.Marshal(platform.created[0])
	for _, want := range []string{`"name":"customer-care-portal"`, `"openchoreo.dev/display-name":"Customer Care  Portal!"`,
		`"openchoreo.dev/description":"Support"`, `"deploymentPipelineRef":{"kind":"DeploymentPipeline","name":"default"}`,
		`"type":{"kind":"ProjectType","name":"default"}`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("create body %s lacks %s", body, want)
		}
	}
	if got := cloudContext(t, login.shell).Project; got != "" {
		t.Errorf("project = %q; creating does not select", got)
	}
}

func TestProjectCreateRefusesAReservedID(t *testing.T) {
	login, platform := newSignedInProjects(t)

	login.runFailing(t, exit.Usage, "project", "create", "New")

	if len(platform.requests) != 0 {
		t.Errorf("a refused create reached the service: %v", platform.requests)
	}
}

func TestProjectDeleteAsksThenClearsTheSelectedProject(t *testing.T) {
	login, platform := newSignedInProjects(t, "retail", "Retail")
	setProject(t, login.shell, "retail")
	t.Setenv("WSO2_NO_INPUT", "1")

	login.runFailing(t, exit.Usage, "project", "delete", "retail")
	if !strings.Contains(login.errOut.String(), "shell.non_interactive") || len(platform.deleted) != 0 {
		t.Fatalf("an unconfirmed delete went ahead: %v\n%s", platform.deleted, login.errOut)
	}

	login.run(t, "project", "delete", "Retail", "--yes")

	if !slices.Equal(platform.deleted, []string{"retail"}) {
		t.Errorf("deleted %v, want retail", platform.deleted)
	}
	if got := cloudContext(t, login.shell).Project; got != "" {
		t.Errorf("project = %q, want the deleted project's selection cleared", got)
	}
	if !strings.Contains(login.errOut.String(), "no longer selects a project") {
		t.Errorf("clearing the selection was not said:\n%s", login.errOut)
	}
}

func TestProjectServiceRefusalsKeepTheirExitClassAndReason(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   exit.Code
	}{
		{http.StatusConflict, exit.Conflict},
		{http.StatusForbidden, exit.AuthPolicy},
		{http.StatusBadGateway, exit.ProductService},
	} {
		login, platform := newSignedInProjects(t, "retail", "Retail")
		platform.refuse, platform.refuseReason = tc.status, "project retail still has 2 APIs"

		login.runFailing(t, tc.want, "project", "list")

		if !strings.Contains(login.errOut.String(), "project retail still has 2 APIs") {
			t.Errorf("HTTP %d: the service's reason is missing:\n%s", tc.status, login.errOut)
		}
	}
}

func TestProjectCommandsRefuseAContextThatIsNotWSO2Cloud(t *testing.T) {
	shell, _, errOut := newShell(t)
	installLogin(t, shell, oneContextDocument())

	if code := shell.Run([]string{"project", "list"}); code != exit.Usage {
		t.Fatalf("exit %d, want usage; stderr %s", code, errOut)
	}
	if !strings.Contains(errOut.String(), "shell.not_wso2_cloud") {
		t.Errorf("stderr lacks shell.not_wso2_cloud:\n%s", errOut)
	}
}

func TestLogoutEndsThePlatformSession(t *testing.T) {
	login, _ := newSignedInProjects(t, "retail", "Retail")
	login.run(t, "project", "list")
	ref := contexts.ProductSessionRef(cloudContext(t, login.shell).CredentialRef, contexts.PlatformKey)
	store := session.Store{StateRoot: login.shell.StateRoot}
	if stored, _ := store.Stored(ref); !stored {
		t.Fatal("no platform session after project list")
	}

	login.run(t, "logout")

	if stored, _ := store.Stored(ref); stored {
		t.Error("the platform session outlived logout")
	}
}

func TestProjectDeleteWithoutTheRoleIsRefusedBeforeAnyoneIsAsked(t *testing.T) {
	login, platform := newSignedInProjectsWith(t, fakeissuer.Options{WithheldScopes: []string{"cloud:project:delete"}},
		"retail", "Retail")
	login.run(t, "project", "list")

	login.runFailing(t, exit.AuthPolicy, "project", "delete", "retail")

	if !strings.Contains(login.errOut.String(), "auth.platform_forbidden") ||
		!strings.Contains(login.errOut.String(), "delete projects") {
		t.Errorf("the refusal does not say the role does not allow deleting:\n%s", login.errOut)
	}
	if len(platform.deleted) != 0 {
		t.Errorf("deleted %v without the role", platform.deleted)
	}
}

func TestLoginEndsThePlatformSessionSoTheNextProjectCommandUsesTheNewSignIn(t *testing.T) {
	login, _ := newSignedInProjects(t, "retail", "Retail")
	login.run(t, "project", "list")
	ref := contexts.ProductSessionRef(cloudContext(t, login.shell).CredentialRef, contexts.PlatformKey)
	store := session.Store{StateRoot: login.shell.StateRoot}

	login.run(t, "login")

	if stored, _ := store.Stored(ref); stored {
		t.Error("the platform session outlived a new login")
	}
	login.run(t, "project", "list")
	if stored, _ := store.Stored(ref); !stored {
		t.Error("the next project command did not establish the platform session again")
	}
}

func TestProjectUseRefusesAProjectBeingDeleted(t *testing.T) {
	login, platform := newSignedInProjects(t, "retail", "Retail")
	platform.deleting = map[string]bool{"retail": true}

	login.runFailing(t, exit.Conflict, "project", "use", "retail")
}

func TestProjectListRefusesACursorThatNeverAdvances(t *testing.T) {
	login, platform := newSignedInProjects(t, "retail", "Retail", "hr", "HR")
	platform.pageSize, platform.stuckCursor = 1, true

	login.runFailing(t, exit.ProductService, "project", "list")
}
