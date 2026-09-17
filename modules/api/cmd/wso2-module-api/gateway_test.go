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
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/testkit"
)

// captureStderr redirects the process's real standard error for the duration
// of fn and returns what was written to it. gatewayRegister writes its
// "shown once" note straight to os.Stderr, per the module contract's rule
// that standard error carries a module's own diagnostics (main.go), so a test
// proving that note landed there — and not on the result the shell renders —
// has to capture the real stream rather than anything testkit exposes.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("opening a pipe to capture stderr: %v", err)
	}
	original := os.Stderr
	os.Stderr = write
	defer func() { os.Stderr = original }()

	fn()

	_ = write.Close()
	captured, err := io.ReadAll(read)
	if err != nil {
		t.Fatalf("reading captured stderr: %v", err)
	}
	return string(captured)
}

func TestGatewayRegisterPrintsTheTokenOnceOnStdoutAndTheWarningOnStderr(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v0.9/gateways":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"prod-gateway-01","displayName":"Production Gateway 01"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v0.9/gateways/prod-gateway-01/tokens":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"tok-1","token":"the-gateway-registration-token"}`))
		default:
			http.Error(w, `{"code":"NOT_FOUND"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	var outcome testkit.Outcome
	stderr := captureStderr(t, func() {
		outcome = testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
			Command: []string{"gateway", "register"},
			Arguments: []string{"prod-gateway-01", "--display-name", "Production Gateway 01",
				"--endpoint", "https://api.example.com:8443/api/v1"},
			Context: module.Context{Name: "c1", Endpoint: server.URL},
			Access:  &testkit.Access{Token: "control-plane-token"},
		})
	})
	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("gateway register failed: err=%v problem=%v", outcome.Err, outcome.Problem)
	}

	rendering := rendered(outcome)
	if strings.Count(rendering, "the-gateway-registration-token") != 1 {
		t.Fatalf("the token does not appear exactly once in the result:\n%s", rendering)
	}
	if strings.Contains(stderr, "the-gateway-registration-token") {
		t.Fatalf("the token leaked into stderr: %q", stderr)
	}
	if !strings.Contains(stderr, "will not be shown again") {
		t.Fatalf("stderr does not carry the one-time warning: %q", stderr)
	}

	var foundField bool
	for _, field := range outcome.Result.Fields {
		if field.Name == "token" && field.Value == "the-gateway-registration-token" {
			foundField = true
		}
	}
	if !foundField {
		t.Fatalf("the result carries no token field: %+v", outcome.Result.Fields)
	}
}

func TestGatewayRegisterDefaultsTypeToRegular(t *testing.T) {
	var capturedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v0.9/gateways":
			body, _ := io.ReadAll(r.Body)
			capturedBody = string(body)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"gw-1","displayName":"Gateway"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v0.9/gateways/gw-1/tokens":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"tok-1","token":"tkn"}`))
		default:
			http.Error(w, `{"code":"NOT_FOUND"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	captureStderr(t, func() {
		outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
			Command: []string{"gateway", "register"},
			Arguments: []string{"gw-1", "--display-name", "Gateway",
				"--endpoint", "https://gw.example.test"},
			Context: module.Context{Name: "c1", Endpoint: server.URL},
			Access:  &testkit.Access{Token: "control-plane-token"},
		})
		if outcome.Err != nil || outcome.Problem != nil {
			t.Fatalf("gateway register failed: err=%v problem=%v", outcome.Err, outcome.Problem)
		}
	})
	if !strings.Contains(capturedBody, `"functionalityType":"regular"`) {
		t.Fatalf("the request body does not default functionalityType to regular: %s", capturedBody)
	}
}

func TestGatewayRegisterRefusesAnUnknownType(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(server.Close)

	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command: []string{"gateway", "register"},
		Arguments: []string{"gw-1", "--display-name", "Gateway",
			"--endpoint", "https://gw.example.test", "--type", "batch"},
		Context: module.Context{Name: "c1", Endpoint: server.URL},
		Access:  &testkit.Access{Token: "control-plane-token"},
	})
	if outcome.Problem == nil {
		t.Fatalf("an unknown --type was accepted: %+v", outcome.Result)
	}
	if called {
		t.Fatal("the module called out despite an unknown --type")
	}
}

func TestGatewayRegisterLeavesTheGatewayRegisteredAndNamesTokenCreateWhenTheTokenMintFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v0.9/gateways":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"gw-1","displayName":"Gateway"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v0.9/gateways/gw-1/tokens":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"code":"INTERNAL","message":"boom"}`))
		default:
			http.Error(w, `{"code":"NOT_FOUND"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	var outcome testkit.Outcome
	captureStderr(t, func() {
		outcome = testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
			Command: []string{"gateway", "register"},
			Arguments: []string{"gw-1", "--display-name", "Gateway",
				"--endpoint", "https://gw.example.test"},
			Context: module.Context{Name: "c1", Endpoint: server.URL},
			Access:  &testkit.Access{Token: "control-plane-token"},
		})
	})
	if outcome.Problem == nil {
		t.Fatalf("a failed token mint after a successful register was not reported: %+v", outcome.Result)
	}
	if !strings.Contains(outcome.Problem.Message, "gw-1") || !strings.Contains(outcome.Problem.Message, "no token was minted") {
		t.Fatalf("the problem does not say the gateway is registered without a token: %q",
			outcome.Problem.Message)
	}
	if !strings.Contains(outcome.Problem.Recovery, "wso2 api gateway token create gw-1") {
		t.Fatalf("the recovery does not name gateway token create: %q", outcome.Problem.Recovery)
	}
}

func TestGatewayRegisterRefusesAnEmptyToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v0.9/gateways":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"gw-1","displayName":"Gateway"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v0.9/gateways/gw-1/tokens":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"tok-1","token":""}`))
		default:
			http.Error(w, `{"code":"NOT_FOUND"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	var outcome testkit.Outcome
	captureStderr(t, func() {
		outcome = testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
			Command: []string{"gateway", "register"},
			Arguments: []string{"gw-1", "--display-name", "Gateway",
				"--endpoint", "https://gw.example.test"},
			Context: module.Context{Name: "c1", Endpoint: server.URL},
			Access:  &testkit.Access{Token: "control-plane-token"},
		})
	})
	if outcome.Problem == nil {
		t.Fatalf("an empty token was accepted: %+v", outcome.Result)
	}
	if !strings.Contains(outcome.Problem.Recovery, "wso2 api gateway token create gw-1") {
		t.Fatalf("the recovery does not name gateway token create: %q", outcome.Problem.Recovery)
	}
}

func TestGatewayTokenCreateMintsATokenForAnExistingGatewayAndPrintsItOnce(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/api/v0.9/gateways/gw-1/tokens" {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"tok-2","token":"rotated-token"}`))
			return
		}
		http.Error(w, `{"code":"NOT_FOUND"}`, http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	var outcome testkit.Outcome
	stderr := captureStderr(t, func() {
		outcome = testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
			Command:   []string{"gateway", "token", "create"},
			Arguments: []string{"gw-1"},
			Context:   module.Context{Name: "c1", Endpoint: server.URL},
			Access:    &testkit.Access{Token: "control-plane-token"},
		})
	})
	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("gateway token create failed: err=%v problem=%v", outcome.Err, outcome.Problem)
	}
	if strings.Count(rendered(outcome), "rotated-token") != 1 {
		t.Fatalf("the token does not appear exactly once in the result:\n%s", rendered(outcome))
	}
	if strings.Contains(stderr, "rotated-token") {
		t.Fatalf("the token leaked into stderr: %q", stderr)
	}
	if !strings.Contains(stderr, "will not be shown again") {
		t.Fatalf("stderr does not carry the one-time warning: %q", stderr)
	}
}

func TestGatewayRegisterEscapesTheHandleInTheTokenPath(t *testing.T) {
	var tokenPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v0.9/gateways":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"gw/1","displayName":"Gateway"}`))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v0.9/gateways/"):
			tokenPath = r.URL.EscapedPath()
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"tok-1","token":"tkn"}`))
		default:
			http.Error(w, `{"code":"NOT_FOUND"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	captureStderr(t, func() {
		outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
			Command: []string{"gateway", "register"},
			Arguments: []string{"gw-1", "--display-name", "Gateway",
				"--endpoint", "https://gw.example.test"},
			Context: module.Context{Name: "c1", Endpoint: server.URL},
			Access:  &testkit.Access{Token: "control-plane-token"},
		})
		if outcome.Err != nil || outcome.Problem != nil {
			t.Fatalf("gateway register failed: err=%v problem=%v", outcome.Err, outcome.Problem)
		}
	})
	if tokenPath != "/api/v0.9/gateways/gw%2F1/tokens" {
		t.Fatalf("the gateway id was not escaped in the token path: %q", tokenPath)
	}
}

func TestGatewayRegisterCarriesTheMintFailureReasonAndReusesNotAuthorizedOn403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v0.9/gateways":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"gw-1","displayName":"Gateway"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v0.9/gateways/gw-1/tokens":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"code":"FORBIDDEN","message":"missing ap:gateway:token:create"}`))
		default:
			http.Error(w, `{"code":"NOT_FOUND"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	var outcome testkit.Outcome
	captureStderr(t, func() {
		outcome = testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
			Command: []string{"gateway", "register"},
			Arguments: []string{"gw-1", "--display-name", "Gateway",
				"--endpoint", "https://gw.example.test"},
			Context: module.Context{Name: "c1", Endpoint: server.URL},
			Access:  &testkit.Access{Token: "control-plane-token"},
		})
	})
	if outcome.Problem == nil {
		t.Fatalf("a 403 token mint after register was not reported: %+v", outcome.Result)
	}
	if outcome.Problem.Code != "api.not_authorized" {
		t.Fatalf("problem code = %q, want api.not_authorized", outcome.Problem.Code)
	}
	if !strings.Contains(outcome.Problem.Recovery, "wso2 api gateway token create gw-1") {
		t.Fatalf("the recovery does not name gateway token create: %q", outcome.Problem.Recovery)
	}
}

func TestGatewayRegisterFallsBackToTheHandleWhenTheCreatedIdIsEmpty(t *testing.T) {
	var tokenPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v0.9/gateways":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"displayName":"Gateway"}`))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v0.9/gateways/"):
			tokenPath = r.URL.Path
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"tok-1","token":"tkn"}`))
		default:
			http.Error(w, `{"code":"NOT_FOUND"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	var outcome testkit.Outcome
	captureStderr(t, func() {
		outcome = testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
			Command: []string{"gateway", "register"},
			Arguments: []string{"gw-1", "--display-name", "Gateway",
				"--endpoint", "https://gw.example.test"},
			Context: module.Context{Name: "c1", Endpoint: server.URL},
			Access:  &testkit.Access{Token: "control-plane-token"},
		})
	})
	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("gateway register failed: err=%v problem=%v", outcome.Err, outcome.Problem)
	}
	var idField string
	for _, field := range outcome.Result.Fields {
		if field.Name == "id" {
			idField = field.Value
		}
	}
	if idField != "gw-1" {
		t.Fatalf("id field = %q, want the handle gw-1 as a fallback", idField)
	}
	if tokenPath != "/api/v0.9/gateways/gw-1/tokens" {
		t.Fatalf("the token mint did not use the handle as a fallback id: %q", tokenPath)
	}
}

func TestGatewayTokenCreateMentionsTheTwoTokenLimitInRecoveryOnFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"TOKEN_LIMIT","message":"token limit reached"}`))
	}))
	t.Cleanup(server.Close)

	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"gateway", "token", "create"},
		Arguments: []string{"gw-1"},
		Context:   module.Context{Name: "c1", Endpoint: server.URL},
		Access:    &testkit.Access{Token: "control-plane-token"},
	})
	if outcome.Problem == nil {
		t.Fatalf("a 409 from token mint was not surfaced: %+v", outcome.Result)
	}
	if !strings.Contains(outcome.Problem.Recovery, "2 active tokens") {
		t.Fatalf("the recovery does not mention the token limit: %q", outcome.Problem.Recovery)
	}
}
