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
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/sdk/testkit"
)

// fakeThunder is enough of ThunderID for bootstrap: the console login flow
// and the applications API. It records what it was asked so the test can
// assert the bodies, and it can start with an application already present.
type fakeThunder struct {
	server     *httptest.Server
	challenge  string
	verifier   string
	flowBodies []map[string]any
	created    []map[string]any
	existing   []map[string]any
}

func newFakeThunder(t *testing.T, existing ...map[string]any) *fakeThunder {
	t.Helper()
	fake := &fakeThunder{existing: existing}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /oauth2/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("client_id") != "CONSOLE" || q.Get("resource") != "https://localhost:8090/mcp" ||
			q.Get("code_challenge_method") != "S256" || !strings.Contains(q.Get("scope"), "system") {
			http.Error(w, "bad authorize "+r.URL.RawQuery, http.StatusBadRequest)
			return
		}
		fake.challenge = q.Get("code_challenge")
		http.Redirect(w, r, fake.server.URL+"/gate/signin?applicationId=app&authId=auth-1&executionId=exec-1",
			http.StatusFound)
	})
	mux.HandleFunc("POST /flow/execute", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		fake.flowBodies = append(fake.flowBodies, body)
		inputs, _ := body["inputs"].(map[string]any)
		if inputs["password"] != "Admin@123" {
			_, _ = w.Write([]byte(`{"flowStatus":"ERROR","failureReason":"Invalid credentials"}`))
			return
		}
		if body["challengeToken"] == nil {
			_, _ = w.Write([]byte(`{"flowStatus":"INCOMPLETE","challengeToken":"ct-1"}`))
			return
		}
		_, _ = w.Write([]byte(`{"flowStatus":"COMPLETE","assertion":"assertion-1"}`))
	})
	mux.HandleFunc("POST /oauth2/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["authId"] != "auth-1" || body["assertion"] != "assertion-1" {
			http.Error(w, "bad callback", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"redirect_uri":"` + fake.server.URL + `/console?code=code-1&state=s"}`))
	})
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(sum[:]) != fake.challenge || r.PostForm.Get("code") != "code-1" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"system-token","token_type":"Bearer","expires_in":3600}`))
	})
	mux.HandleFunc("GET /applications", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer system-token" {
			http.Error(w, "no token", http.StatusUnauthorized)
			return
		}
		all := append(append([]map[string]any{}, fake.existing...), fake.created...)
		_ = json.NewEncoder(w).Encode(map[string]any{"totalResults": len(all), "applications": all})
	})
	mux.HandleFunc("POST /applications", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		body["id"] = "app-new"
		fake.created = append(fake.created, body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(raw)
	})
	fake.server = httptest.NewServer(mux)
	t.Cleanup(fake.server.Close)
	return fake
}

func runBootstrap(t *testing.T, url string) testkit.Outcome {
	t.Helper()
	return testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"bootstrap"},
		Arguments: []string{"--url", url},
	})
}

func TestBootstrapLogsInAsTheAdministratorAndRegistersTheCLI(t *testing.T) {
	fake := newFakeThunder(t)
	t.Setenv("WSO2_IAM_ADMIN_PASSWORD", "Admin@123")

	outcome := runBootstrap(t, fake.server.URL)

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("bootstrap failed: err %v problem %+v", outcome.Err, outcome.Problem)
	}
	if len(fake.flowBodies) != 2 || fake.flowBodies[1]["challengeToken"] != "ct-1" ||
		fake.flowBodies[1]["action"] != "action_001" {
		t.Errorf("flow bodies = %+v", fake.flowBodies)
	}
	if len(fake.created) != 1 {
		t.Fatalf("created = %+v", fake.created)
	}
	app := fake.created[0]
	inbound := app["inboundAuthConfig"].([]any)[0].(map[string]any)
	config := inbound["config"].(map[string]any)
	if app["type"] != "custom" || app["authFlowId"] != DefaultAuthFlow || app["ouId"] != DefaultOU ||
		app["registrationFlowId"] != DefaultRegistrationFlow || app["recoveryFlowId"] != DefaultRecoveryFlow ||
		config["clientId"] != "wso2-cli" || config["publicClient"] != true || config["pkceRequired"] != true ||
		config["tokenEndpointAuthMethod"] != "none" || len(config["redirectUris"].([]any)) != 4 {
		t.Errorf("application body = %+v", app)
	}
	fields := map[string]string{}
	for _, field := range outcome.Result.Fields {
		fields[field.Name] = field.Value
	}
	if fields["created"] != "true" || fields["clientId"] != "wso2-cli" ||
		!strings.Contains(fields["next"], "wso2 iam connect "+fake.server.URL+", then wso2 login") ||
		strings.Contains(fields["next"], "--audience") {
		t.Errorf("fields = %+v", fields)
	}
	assertEndsWithNext(t, outcome)
}

func TestBootstrapFindsAnExistingClientAndCreatesNothing(t *testing.T) {
	fake := newFakeThunder(t, map[string]any{"id": "app-old", "name": "WSO2 CLI",
		"inboundAuthConfig": []map[string]any{{"type": "oauth2", "config": map[string]any{"clientId": "wso2-cli"}}}})
	t.Setenv("WSO2_IAM_ADMIN_PASSWORD", "Admin@123")

	outcome := runBootstrap(t, fake.server.URL)

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("bootstrap failed: err %v problem %+v", outcome.Err, outcome.Problem)
	}
	if len(fake.created) != 0 {
		t.Errorf("an application was created: %+v", fake.created)
	}
	for _, field := range outcome.Result.Fields {
		if field.Name == "created" && field.Value != "false" {
			t.Errorf("created = %s", field.Value)
		}
		if field.Name == "applicationId" && field.Value != "app-old" {
			t.Errorf("applicationId = %s", field.Value)
		}
	}
}

func TestBootstrapRefusesWithoutThePasswordAndOnAWrongOne(t *testing.T) {
	fake := newFakeThunder(t)
	t.Setenv("WSO2_IAM_ADMIN_PASSWORD", "")
	outcome := runBootstrap(t, fake.server.URL)
	if outcome.Problem == nil || outcome.Problem.Code != "iam.missing_secret" ||
		!strings.Contains(outcome.Problem.Message, "WSO2_IAM_ADMIN_PASSWORD") {
		t.Errorf("unset password: %+v", outcome.Problem)
	}
	if len(fake.flowBodies) != 0 {
		t.Error("the deployment was contacted without a password")
	}

	t.Setenv("WSO2_IAM_ADMIN_PASSWORD", "wrong")
	outcome = runBootstrap(t, fake.server.URL)
	if outcome.Problem == nil || outcome.Problem.Code != "iam.refused" ||
		!strings.Contains(outcome.Problem.Message, "username or password") {
		t.Errorf("wrong password: %+v", outcome.Problem)
	}
}
