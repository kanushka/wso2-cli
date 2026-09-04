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
	"io"
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

// fakeAPIM is enough of API Manager for this module: dynamic client
// registration, the publisher, devportal and admin planes, and the key
// manager's token endpoint. It records every request so tests can assert
// bodies, and it can be seeded.
type fakeAPIM struct {
	mu          sync.Mutex
	server      *httptest.Server
	apis        []map[string]any
	deployments map[string][]map[string]any
	apps        []map[string]any
	subs        []map[string]any
	keys        map[string][]map[string]any
	kms         []map[string]any
	requests    []string // "METHOD path body"
	seq         int
	// mapKeysRefusals makes map-keys answer "Key Manager not Registered" this
	// many times before succeeding, as the deployment does briefly.
	mapKeysRefusals int
	// deployPolls counts GET deployments before reporting success.
	deployPolls int
}

func newFakeAPIM(t *testing.T) *fakeAPIM {
	t.Helper()
	fake := &fakeAPIM{deployments: map[string][]map[string]any{}, keys: map[string][]map[string]any{}}
	id := func() string { fake.seq++; return fmt.Sprintf("id-%d", fake.seq) }
	record := func(r *http.Request) map[string]any {
		raw, _ := io.ReadAll(r.Body)
		fake.requests = append(fake.requests, r.Method+" "+r.URL.RequestURI()+" "+string(raw))
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		return body
	}
	list := func(w http.ResponseWriter, items []map[string]any) {
		_ = json.NewEncoder(w).Encode(map[string]any{"count": len(items), "list": items})
	}
	bearer := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+fixtureToken {
				http.Error(w, `{"code":401,"message":"Unauthenticated request"}`, http.StatusUnauthorized)
				return
			}
			fake.mu.Lock()
			defer fake.mu.Unlock()
			next(w, r)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /client-registration/v0.17/register", func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		user, password, _ := r.BasicAuth()
		body := record(r)
		if user != "admin" || password != "admin" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"clientId": "client-1", "clientSecret": "secret-1",
			"clientName": body["clientName"], "tokenType": body["tokenType"]})
	})
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		record(r)
		user, password, _ := r.BasicAuth()
		if user != "ck-1" || password != "cs-1" {
			http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "a.b.c", "expires_in": 3600})
	})
	// publisher
	mux.HandleFunc("GET /api/am/publisher/v4/apis", bearer(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		query := r.URL.Query().Get("query")
		var matching []map[string]any
		for _, api := range fake.apis {
			if query == "" || query == "name:"+api["name"].(string) {
				matching = append(matching, api)
			}
		}
		list(w, matching)
	}))
	mux.HandleFunc("POST /api/am/publisher/v4/apis/import-openapi", bearer(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "no file", http.StatusBadRequest)
			return
		}
		content, _ := io.ReadAll(file)
		fake.requests = append(fake.requests, "POST /api/am/publisher/v4/apis/import-openapi "+
			header.Filename+" "+string(content)+" "+r.FormValue("additionalProperties"))
		var props map[string]any
		_ = json.Unmarshal([]byte(r.FormValue("additionalProperties")), &props)
		props["id"] = id()
		props["lifeCycleStatus"] = "CREATED"
		fake.apis = append(fake.apis, props)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(props)
	}))
	mux.HandleFunc("POST /api/am/publisher/v4/apis/{id}/revisions", bearer(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "rev-" + r.PathValue("id"), "displayName": "Revision 1"})
	}))
	mux.HandleFunc("POST /api/am/publisher/v4/apis/{id}/deploy-revision", bearer(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		fake.deployments[r.PathValue("id")] = []map[string]any{{"name": "Default", "status": "APPROVED"}}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`[{"name":"Default","status":"APPROVED"}]`))
	}))
	mux.HandleFunc("GET /api/am/publisher/v4/apis/{id}/deployments", bearer(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		deployed := fake.deployments[r.PathValue("id")]
		if fake.deployPolls > 0 {
			fake.deployPolls--
			_, _ = w.Write([]byte(`[{"name":"Default","successDeployedTime":0}]`))
			return
		}
		for _, d := range deployed {
			d["successDeployedTime"] = 1788475966000
		}
		_ = json.NewEncoder(w).Encode(deployed)
	}))
	mux.HandleFunc("POST /api/am/publisher/v4/apis/change-lifecycle", bearer(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		for _, api := range fake.apis {
			if api["id"] == r.URL.Query().Get("apiId") {
				api["lifeCycleStatus"] = "PUBLISHED"
			}
		}
		_, _ = w.Write([]byte(`{"lifecycleState":{"state":"Published"}}`))
	}))
	// devportal
	mux.HandleFunc("GET /api/am/devportal/v3/apis", bearer(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		list(w, fake.apis)
	}))
	mux.HandleFunc("GET /api/am/devportal/v3/applications", bearer(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		list(w, fake.apps)
	}))
	mux.HandleFunc("POST /api/am/devportal/v3/applications", bearer(func(w http.ResponseWriter, r *http.Request) {
		body := record(r)
		body["applicationId"] = id()
		fake.apps = append(fake.apps, body)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(body)
	}))
	mux.HandleFunc("POST /api/am/devportal/v3/subscriptions", bearer(func(w http.ResponseWriter, r *http.Request) {
		body := record(r)
		for _, sub := range fake.subs {
			if sub["applicationId"] == body["applicationId"] && sub["apiId"] == body["apiId"] {
				http.Error(w, `{"code":409,"description":"Specified subscription already exists"}`, http.StatusConflict)
				return
			}
		}
		body["subscriptionId"] = id()
		body["status"] = "UNBLOCKED"
		fake.subs = append(fake.subs, body)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(body)
	}))
	mux.HandleFunc("POST /api/am/devportal/v3/applications/{id}/generate-keys", bearer(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		key := map[string]any{"keyMappingId": id(), "keyType": "PRODUCTION", "keyState": "APPROVED",
			"consumerKey": "ck-1", "consumerSecret": "cs-1", "keyManager": "Resident Key Manager"}
		fake.keys[r.PathValue("id")] = append(fake.keys[r.PathValue("id")], key)
		_ = json.NewEncoder(w).Encode(key)
	}))
	mux.HandleFunc("POST /api/am/devportal/v3/applications/{id}/map-keys", bearer(func(w http.ResponseWriter, r *http.Request) {
		body := record(r)
		if fake.mapKeysRefusals > 0 {
			fake.mapKeysRefusals--
			http.Error(w, `{"code":500,"description":"Key Manager not Registered"}`, http.StatusInternalServerError)
			return
		}
		body["mode"] = "MAPPED"
		_ = json.NewEncoder(w).Encode(body)
	}))
	// admin
	mux.HandleFunc("GET /api/am/admin/v4/key-managers", bearer(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		list(w, fake.kms)
	}))
	mux.HandleFunc("POST /api/am/admin/v4/key-managers", bearer(func(w http.ResponseWriter, r *http.Request) {
		body := record(r)
		if body["tokenEndpoint"] == nil || body["revokeEndpoint"] == nil {
			http.Error(w, `{"code":901401,"message":"Required Key Manager configuration missing"}`, http.StatusBadRequest)
			return
		}
		body["id"] = id()
		fake.kms = append(fake.kms, body)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(body)
	}))
	// an issuer's discovery document, for key-managers add --well-known
	mux.HandleFunc("GET /issuer/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": fake.server.URL + "/issuer", "jwks_uri": fake.server.URL + "/issuer/oauth2/jwks",
			"token_endpoint":      fake.server.URL + "/issuer/oauth2/token",
			"revocation_endpoint": fake.server.URL + "/issuer/oauth2/revoke",
		})
	})
	// a gateway route
	mux.HandleFunc("GET /mockapi/1.0.0/status", func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		record(r)
		if r.Header.Get("Authorization") != "Bearer "+fixtureToken {
			http.Error(w, `{"code":"900901"}`, http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	fake.server = httptest.NewServer(mux)
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeAPIM) run(t *testing.T, command []string, arguments ...string) testkit.Outcome {
	t.Helper()
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   command,
		Arguments: arguments,
		Context:   module.Context{Name: "apim-admin", Endpoint: f.server.URL},
		Access:    &testkit.Access{Token: fixtureToken, ExpiresAt: time.Now().Add(time.Minute)},
	})
	if outcome.Err != nil {
		t.Fatalf("%v: %v", command, outcome.Err)
	}
	return outcome
}

func (f *fakeAPIM) requestsTo(prefix string) []string {
	var matching []string
	for _, request := range f.requests {
		if strings.HasPrefix(request, prefix) {
			matching = append(matching, request)
		}
	}
	return matching
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

func scopesAsked(outcome testkit.Outcome) string {
	if len(outcome.AccessRequests) == 0 {
		return ""
	}
	return strings.Join(outcome.AccessRequests[0].Scopes, " ")
}
