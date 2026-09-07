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
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
)

func TestEveryListAsksForNoScopesAndEndsWithNext(t *testing.T) {
	fake := newFakeAPIM(t)
	for _, command := range []string{"apis", "apps", "key-managers"} {
		outcome := fake.run(t, []string{command, "list"})
		if outcome.Problem != nil {
			t.Fatalf("%s list: %+v", command, outcome.Problem)
		}
		// No scopes: the product record names them, and is the ceiling.
		if scopesAsked(outcome) != "" || outcome.AccessRequests[0].Audience != PublisherAudience {
			t.Errorf("%s list asked for %+v", command, outcome.AccessRequests)
		}
		assertEndsWithNext(t, outcome)
	}
}

func TestImportDeployPublishInOrder(t *testing.T) {
	fake := newFakeAPIM(t)
	deployInterval = 10 * time.Millisecond
	fake.deployPolls = 2
	spec := filepath.Join(t.TempDir(), "mockapi.yaml")
	if err := os.WriteFile(spec, []byte("openapi: 3.0.3\ninfo: {title: Mock, version: 1.0.0}\npaths: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	imported := fake.run(t, []string{"apis", "import"}, "--file", spec, "--name", "MockAPI", "--version", "1.0.0",
		"--api-context", "/mockapi", "--backend", "http://host.docker.internal:18080")
	if imported.Problem != nil {
		t.Fatalf("import: %+v", imported.Problem)
	}
	uploads := fake.requestsTo("POST /api/am/publisher/v4/apis/import-openapi")
	if len(uploads) != 1 || !strings.Contains(uploads[0], "mockapi.yaml openapi: 3.0.3") ||
		!strings.Contains(uploads[0], `"production_endpoints":{"url":"http://host.docker.internal:18080"}`) ||
		!strings.Contains(uploads[0], `"policies":["Unlimited"]`) {
		t.Errorf("upload = %v", uploads)
	}
	if fields := fieldsOf(imported); fields["created"] != "true" || scopesAsked(imported) != "" ||
		!strings.Contains(fields["next"], "apis deploy MockAPI/1.0.0") {
		t.Errorf("import fields = %+v scopes %q", fields, scopesAsked(imported))
	}
	if fieldsOf(fake.run(t, []string{"apis", "import"}, "--file", spec, "--name", "MockAPI", "--version", "1.0.0",
		"--api-context", "/mockapi", "--backend", "http://x"))["created"] != "false" {
		t.Error("a second import was not idempotent")
	}

	lookupInterval = 10 * time.Millisecond
	fake.listLag = 2
	deployed := fake.run(t, []string{"apis", "deploy"}, "MockAPI/1.0.0")
	if deployed.Problem != nil {
		t.Fatalf("deploy: %+v", deployed.Problem)
	}
	if len(fake.requestsTo("POST /api/am/publisher/v4/apis/id-1/revisions")) != 1 ||
		!strings.Contains(fake.requestsTo("POST /api/am/publisher/v4/apis/id-1/deploy-revision")[0], `"vhost":"localhost"`) ||
		len(fake.requestsTo("GET /api/am/publisher/v4/apis/id-1/deployments")) != 3 {
		t.Errorf("deploy requests = %v", fake.requests)
	}
	if fields := fieldsOf(deployed); fields["status"] != "live" || fields["revision"] != "rev-id-1" ||
		scopesAsked(deployed) != "" {
		t.Errorf("deploy fields = %+v", fields)
	}

	published := fake.run(t, []string{"apis", "publish"}, "MockAPI/1.0.0")
	if fields := fieldsOf(published); published.Problem != nil || fields["state"] != "PUBLISHED" || fields["changed"] != "true" {
		t.Errorf("publish = %+v %+v", published.Problem, fields)
	}
	if fieldsOf(fake.run(t, []string{"apis", "publish"}, "MockAPI/1.0.0"))["changed"] != "false" {
		t.Error("a second publish was not idempotent")
	}
	if missing := fake.run(t, []string{"apis", "publish"}, "Nope/1"); missing.Problem == nil || missing.Problem.Code != "apim.not_found" {
		t.Errorf("unknown api: %+v", missing.Problem)
	}
	if bad := fake.run(t, []string{"apis", "deploy"}, "MockAPI"); bad.Problem == nil || bad.Problem.Code != "apim.missing_argument" {
		t.Errorf("bad reference: %+v", bad.Problem)
	}
}

func TestAppsCreateSubscribeKeysAndMapKeys(t *testing.T) {
	fake := newFakeAPIM(t)
	mapKeysInterval = 10 * time.Millisecond
	fake.apis = append(fake.apis, map[string]any{"id": "api-1", "name": "MockAPI", "version": "1.0.0",
		"context": "/mockapi", "lifeCycleStatus": "PUBLISHED"})

	created := fake.run(t, []string{"apps", "create"}, "CliApp")
	if fields := fieldsOf(created); created.Problem != nil || fields["created"] != "true" ||
		!strings.Contains(fake.requestsTo("POST /api/am/devportal/v3/applications ")[0], `"tokenType":"JWT"`) {
		t.Errorf("create = %+v %+v", created.Problem, fields)
	}
	if fieldsOf(fake.run(t, []string{"apps", "create"}, "CliApp"))["created"] != "false" {
		t.Error("a second create was not idempotent")
	}

	subscribed := fake.run(t, []string{"apps", "subscribe"}, "CliApp", "MockAPI/1.0.0")
	if fields := fieldsOf(subscribed); subscribed.Problem != nil || fields["status"] != "UNBLOCKED" || fields["created"] != "true" ||
		!strings.Contains(fake.requestsTo("POST /api/am/devportal/v3/subscriptions")[0], `"apiId":"api-1"`) {
		t.Errorf("subscribe = %+v %+v", subscribed.Problem, fields)
	}
	if again := fake.run(t, []string{"apps", "subscribe"}, "CliApp", "MockAPI/1.0.0"); again.Problem != nil ||
		fieldsOf(again)["created"] != "false" {
		t.Errorf("second subscribe = %+v %+v", again.Problem, fieldsOf(again))
	}

	keys := fake.run(t, []string{"apps", "keys"}, "CliApp")
	if fields := fieldsOf(keys); keys.Problem != nil || fields["consumerKey"] != "ck-1" || fields["verified"] != "true" ||
		len(fake.requestsTo("POST /oauth2/token")) != 1 ||
		!strings.Contains(fake.requestsTo("POST /api/am/devportal/v3/applications/id-1/generate-keys")[0], `"keyManager":"Resident Key Manager"`) {
		t.Errorf("keys = %+v %+v", keys.Problem, fields)
	}

	fake.mapKeysRefusals = 2
	mapped := fake.run(t, []string{"apps", "map-keys"}, "CliApp", "--key-manager", "Thunder", "--client-id", "wso2-cli-ci")
	if fields := fieldsOf(mapped); mapped.Problem != nil || fields["mode"] != "MAPPED" ||
		len(fake.requestsTo("POST /api/am/devportal/v3/applications/id-1/map-keys")) != 3 {
		t.Errorf("map-keys = %+v %+v requests %d", mapped.Problem, fields,
			len(fake.requestsTo("POST /api/am/devportal/v3/applications/id-1/map-keys")))
	}
	if missing := fake.run(t, []string{"apps", "map-keys"}, "CliApp"); missing.Problem == nil || missing.Problem.Code != "apim.missing_flag" {
		t.Errorf("map-keys without flags: %+v", missing.Problem)
	}
	if missing := fake.run(t, []string{"apps", "subscribe"}, "Nope", "MockAPI/1.0.0"); missing.Problem == nil ||
		missing.Problem.Code != "apim.not_found" {
		t.Errorf("unknown app: %+v", missing.Problem)
	}
}

func TestKeyManagersAddDiscoversAndOverrides(t *testing.T) {
	fake := newFakeAPIM(t)
	added := fake.run(t, []string{"key-managers", "add"}, "Thunder", "--well-known", fake.server.URL+"/issuer",
		"--jwks", "http://host.docker.internal:8490/oauth2/jwks")
	if added.Problem != nil {
		t.Fatalf("%+v", added.Problem)
	}
	posts := fake.requestsTo("POST /api/am/admin/v4/key-managers")
	if len(posts) != 1 || !strings.Contains(posts[0], `"value":"http://host.docker.internal:8490/oauth2/jwks"`) ||
		!strings.Contains(posts[0], `"tokenEndpoint":"`+fake.server.URL+`/issuer/oauth2/token"`) ||
		!strings.Contains(posts[0], `"revokeEndpoint":"`+fake.server.URL+`/issuer/oauth2/revoke"`) ||
		!strings.Contains(posts[0], `"type":"CustomKeyManager"`) || !strings.Contains(posts[0], `"consumerKeyClaim":"client_id"`) {
		t.Errorf("key manager body = %v", posts)
	}
	if fields := fieldsOf(added); fields["created"] != "true" || scopesAsked(added) != "" ||
		!strings.Contains(fields["next"], "map-keys <app> --key-manager Thunder") {
		t.Errorf("fields = %+v", fields)
	}
	if fieldsOf(fake.run(t, []string{"key-managers", "add"}, "Thunder", "--well-known", "http://unused"))["created"] != "false" {
		t.Error("a second add was not idempotent")
	}
	// The same issuer under another name is refused: the gateway cannot
	// choose between two key managers for one issuer.
	duplicate := fake.run(t, []string{"key-managers", "add"}, "Thunder2", "--well-known", fake.server.URL+"/issuer")
	if duplicate.Problem == nil || duplicate.Problem.Code != "apim.issuer_registered" ||
		!strings.Contains(duplicate.Problem.Message, `"Thunder"`) || len(fake.requestsTo("POST /api/am/admin/v4/key-managers")) != 1 {
		t.Errorf("duplicate issuer: %+v", duplicate.Problem)
	}
}

func TestMapKeysTellsAMappingHeldElsewhereFromOneAlreadyHeld(t *testing.T) {
	fake := newFakeAPIM(t)
	fake.run(t, []string{"apps", "create"}, "DefaultApplication")
	fake.run(t, []string{"apps", "create"}, "HelloApp")
	first := fake.run(t, []string{"apps", "map-keys"}, "DefaultApplication", "--key-manager", "Thunder", "--client-id", "wso2-cli")
	if first.Problem != nil || fieldsOf(first)["mode"] != "MAPPED" {
		t.Fatalf("first map-keys = %+v %+v", first.Problem, fieldsOf(first))
	}
	// The same client on the same application is what it says: already mapped.
	again := fake.run(t, []string{"apps", "map-keys"}, "DefaultApplication", "--key-manager", "Thunder", "--client-id", "wso2-cli")
	if again.Problem != nil || fieldsOf(again)["mode"] != "MAPPED (already)" {
		t.Errorf("second map-keys = %+v %+v", again.Problem, fieldsOf(again))
	}
	// On another application, API Manager refuses with the same "already"
	// text, and the gateway would then fail the subscription check.
	elsewhere := fake.run(t, []string{"apps", "map-keys"}, "HelloApp", "--key-manager", "Thunder", "--client-id", "wso2-cli")
	if elsewhere.Problem == nil || elsewhere.Problem.Code != "apim.client_mapped_elsewhere" ||
		!strings.Contains(elsewhere.Problem.Message, `"DefaultApplication"`) ||
		!strings.Contains(elsewhere.Problem.Message, `"HelloApp"`) ||
		!strings.Contains(elsewhere.Problem.Recovery, "apps subscribe DefaultApplication") ||
		!strings.Contains(elsewhere.Problem.Recovery, "--client-id") {
		t.Errorf("map-keys elsewhere = %+v", elsewhere.Problem)
	}
	if outcome := fake.run(t, []string{"apps", "keys"}, "HelloApp"); outcome.Problem != nil {
		t.Errorf("keys after a refused mapping: %+v", outcome.Problem)
	}
}

func TestGatewayInvokeRefusesAnIdentityWithoutAGatewayRecord(t *testing.T) {
	fake := newFakeAPIM(t)
	// After wso2 apim connect alone the identity records the management
	// origin and no gateway, so the shell hands the module no gateway
	// endpoint.
	outcome := fake.run(t, []string{"gateway", "invoke"}, "/mockapi/1.0.0/status")
	if outcome.Problem == nil || outcome.Problem.Code != "apim.no_gateway" || outcome.Problem.Category != problem.CategoryUsage ||
		!strings.Contains(outcome.Problem.Message, "gateway") ||
		!strings.Contains(outcome.Problem.Recovery, "wso2 apim connect <gateway-url> --gateway --audience <api resource identifier> --scopes <list>") ||
		!strings.Contains(outcome.Problem.Recovery, "https://<host>:8243") ||
		!strings.Contains(outcome.Problem.Recovery, "wso2 login --only apim") ||
		strings.Contains(outcome.Problem.Recovery, "identity create") {
		t.Errorf("no gateway record: %+v", outcome.Problem)
	}
	if len(outcome.AccessRequests) != 0 || len(fake.requests) != 0 {
		t.Errorf("something was asked before the refusal: %v %v", outcome.AccessRequests, fake.requests)
	}
}

func TestGatewayInvokeRefusesANon2xxAnswerWithItsBody(t *testing.T) {
	fake := newFakeAPIM(t)
	fake.gatewayEndpoint = fake.gateway.URL
	fake.gatewayStatus, fake.gatewayBody = http.StatusForbidden,
		`{"code":"900908","message":"Resource forbidden ","description":"User is NOT authorized to access the Resource. API Subscription validation failed."}`
	outcome := fake.run(t, []string{"gateway", "invoke"}, "/mockapi/1.0.0/status")
	if outcome.Problem == nil || outcome.Problem.Code != "apim.refused" || outcome.Problem.Category != problem.CategoryProductService ||
		!strings.Contains(outcome.Problem.Message, "403") || !strings.Contains(outcome.Problem.Message, "900908") ||
		!strings.Contains(outcome.Problem.Recovery, "map-keys") {
		t.Errorf("403: %+v", outcome.Problem)
	}
	fake.gatewayStatus, fake.gatewayBody = http.StatusBadGateway, `{"code":"101503","message":"Runtime Error"}`
	outcome = fake.run(t, []string{"gateway", "invoke"}, "/mockapi/1.0.0/status")
	if outcome.Problem == nil || outcome.Problem.Code != "apim.refused" ||
		!strings.Contains(outcome.Problem.Message, "502") || !strings.Contains(outcome.Problem.Message, "101503") {
		t.Errorf("502: %+v", outcome.Problem)
	}
}

func TestGatewayInvokeCallsTheGatewayWithTheGatewayRecordsToken(t *testing.T) {
	fake := newFakeAPIM(t)
	// The identity records both: the management origin as the product's own
	// endpoint, and the gateway beside it.
	fake.gatewayEndpoint = fake.gateway.URL
	outcome := fake.run(t, []string{"gateway", "invoke"}, "/mockapi/1.0.0/status")
	if outcome.Problem != nil {
		t.Fatalf("%+v", outcome.Problem)
	}
	fields := fieldsOf(outcome)
	if fields["status"] != "200" || fields["body"] != `{"status":"ok"}` || fields["method"] != "GET" ||
		scopesAsked(outcome) != "" || fields["next"] != "(done)" ||
		fields["url"] != fake.gateway.URL+"/mockapi/1.0.0/status" {
		t.Errorf("fields = %+v scopes %q", fields, scopesAsked(outcome))
	}
	// The token is the gateway record's, asked for by name, not the
	// management record's.
	if len(outcome.AccessRequests) != 1 || outcome.AccessRequests[0].Record != module.RecordGateway ||
		outcome.AccessRequests[0].Audience != GatewayAudience {
		t.Errorf("asked for %+v", outcome.AccessRequests)
	}
	if len(fake.requestsTo("GET /api/am")) != 0 {
		t.Errorf("the management origin was called: %v", fake.requests)
	}
	if bad := fake.run(t, []string{"gateway", "invoke"}, "mockapi"); bad.Problem == nil || bad.Problem.Code != "apim.missing_argument" {
		t.Errorf("bad path: %+v", bad.Problem)
	}
}
