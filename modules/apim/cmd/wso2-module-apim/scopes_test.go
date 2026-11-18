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
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/testkit"
)

// commandScopes is what each command's control-plane calls need on WSO2
// Cloud: the permissions guarding its routes, and none for a route the
// platform guards by none. Commands send none of them: a product session is
// one scope set, renewed as a whole, so each asks for the record's full set.
// The table is what a refusal names.
var commandScopes = []struct {
	command   []string
	arguments []string
	scopes    []string
}{
	{[]string{"status"}, nil, []string{"ap:project:read"}},
	{[]string{"project", "list"}, nil, []string{"ap:project:read"}},
	{[]string{"project", "create"}, []string{"shop"}, []string{"ap:project:create"}},
	{[]string{"project", "delete"}, []string{"shop", "--yes"}, []string{"ap:project:delete"}},
	{[]string{"api", "list"}, nil, []string{"ap:project:read", "ap:rest_api:read"}},
	{[]string{"api", "deploy"}, []string{"hello-api-v1", "--gateway-id", "g"}, nil},
	{[]string{"api", "undeploy"}, []string{"hello-api-v1"}, []string{"ap:rest_api:deployment:read"}},
	{[]string{"api", "delete"}, []string{"hello-api-v1", "--yes"},
		[]string{"ap:rest_api:delete", "ap:rest_api:deployment:read"}},
	{[]string{"api", "invoke"}, []string{"hello-api-v1"},
		[]string{"ap:gateway:read", "ap:rest_api:deployment:read", "ap:rest_api:read"}},
	{[]string{"api", "get-token"}, []string{"hello-api-v1"}, []string{"ap:rest_api:read"}},
	{[]string{"gateway", "register"}, []string{"g", "--display-name", "G", "--endpoint", "https://g.example"}, nil},
	{[]string{"gateway", "token", "create"}, []string{"g"}, nil},
}

func TestEachCommandAsksForTheRecordedPermissionsAndNamesItsOwnWhenRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"code":"FORBIDDEN"}`, http.StatusForbidden)
	}))
	t.Cleanup(server.Close)
	run := func(command, arguments []string) testkit.Outcome {
		return testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
			Command:   command,
			Arguments: arguments,
			Context:   module.Context{Name: "c1", Endpoint: server.URL},
			Access:    &testkit.Access{Token: scopedToken("openid")},
		})
	}
	check := func(t *testing.T, outcome testkit.Outcome, scopes []string) {
		t.Helper()
		if len(outcome.AccessRequests) == 0 {
			t.Fatalf("the command asked for no access: %+v", outcome)
		}
		if got := outcome.AccessRequests[0].Scopes; len(got) != 0 {
			t.Errorf("scopes sent = %v, want none", got)
		}
		if outcome.Problem == nil {
			t.Fatalf("a 403 was not refused: %+v", outcome)
		}
		for _, scope := range scopes {
			if !strings.Contains(outcome.Problem.Message, scope) {
				t.Errorf("refusal does not name %s: %s", scope, outcome.Problem.Message)
			}
		}
	}
	for _, testCase := range commandScopes {
		t.Run(strings.Join(testCase.command, " "), func(t *testing.T) {
			check(t, run(testCase.command, testCase.arguments), testCase.scopes)
		})
	}
	t.Run("api create", func(t *testing.T) {
		file := writeFixture(t, "hello-api.yaml", helloAPIDocument)
		check(t, run([]string{"api", "create"}, []string{"-f", file, "--project", "shop"}),
			[]string{"ap:rest_api:create"})
	})
}

func TestTheManifestDeclaresEveryPermissionACommandAsksFor(t *testing.T) {
	var wanted []string
	for _, testCase := range commandScopes {
		wanted = append(wanted, testCase.scopes...)
	}
	wanted = append(wanted, "ap:rest_api:create")
	slices.Sort(wanted)
	wanted = slices.Compact(wanted)

	if got := slices.Sorted(slices.Values(moduleOptions().AuthScopes)); !slices.Equal(got, wanted) {
		t.Errorf("moduleOptions().AuthScopes = %v, want %v", got, wanted)
	}
	data, err := os.ReadFile("../../module.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Capabilities struct {
			AuthScopes []string `json:"authScopes"`
			Product    struct {
				Scopes []string `json:"scopes"`
			} `json:"product"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	for name, scopes := range map[string][]string{
		"authScopes":     manifest.Capabilities.AuthScopes,
		"product.scopes": manifest.Capabilities.Product.Scopes,
	} {
		if got := slices.Sorted(slices.Values(scopes)); !slices.Equal(got, wanted) {
			t.Errorf("module.json %s = %v, want %v", name, got, wanted)
		}
	}
}

// scopedToken is a token shaped as WSO2 Cloud issues one, carrying scopes.
func scopedToken(scopes string) string {
	encode := base64.RawURLEncoding.EncodeToString
	return encode([]byte(`{"alg":"none"}`)) + "." + encode([]byte(`{"scope":"`+scopes+`"}`)) + ".sig"
}

func TestARefusalSaysWhetherSigningInAgainHelps(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"code":"FORBIDDEN"}`, http.StatusForbidden)
	}))
	t.Cleanup(server.Close)
	for _, testCase := range []struct {
		name, token string
		want        []string
		notWant     string
	}{
		{"granted, still refused", scopedToken("openid ap:project:read"),
			[]string{"ap:project:read", "Signing in again will not help", "administrator"}, "group"},
		{"permission left out", scopedToken("openid ap:project:create"),
			[]string{"ap:project:read", "wso2 login", "administrator"}, "group"},
		{"exchanged, no scopes", "control-plane-token",
			[]string{"map this user's group to a role"}, ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
				Command: []string{"project", "list"},
				Context: module.Context{Name: "c1", Endpoint: server.URL},
				Access:  &testkit.Access{Token: testCase.token},
			})
			if outcome.Problem == nil || outcome.Problem.Code != "apim.not_authorized" {
				t.Fatalf("problem = %+v, want apim.not_authorized", outcome.Problem)
			}
			said := outcome.Problem.Message + "\n" + outcome.Problem.Recovery
			for _, want := range testCase.want {
				if !strings.Contains(said, want) {
					t.Errorf("refusal lacks %q:\n%s", want, said)
				}
			}
			if testCase.notWant != "" && strings.Contains(said, testCase.notWant) {
				t.Errorf("refusal mentions %q:\n%s", testCase.notWant, said)
			}
		})
	}
}
