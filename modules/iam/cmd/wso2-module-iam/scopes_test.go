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
	"os"
	"slices"
	"testing"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/testkit"
)

// A local ThunderID grants the management permission as "system" and WSO2
// Cloud as "iam:system". The shell proves a token carries exactly what was
// asked, so the module names neither and takes what the context's product
// record consents to.
func TestCommandsTakeTheRecordedPermission(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command: []string{"resource-server", "list"},
		Context: module.Context{Name: "c1", Endpoint: server.URL},
		Access:  &testkit.Access{Token: "brokered-token"},
	})
	if len(outcome.AccessRequests) != 1 {
		t.Fatalf("access requests = %+v, want one", outcome.AccessRequests)
	}
	if got := outcome.AccessRequests[0].Scopes; len(got) != 0 {
		t.Errorf("scopes = %v, want none named", got)
	}
}

func TestTheManifestDeclaresBothSpellingsOfTheManagementPermission(t *testing.T) {
	want := []string{CloudManagementScope, ManagementScope}
	if got := slices.Sorted(slices.Values(moduleOptions().AuthScopes)); !slices.Equal(got, want) {
		t.Errorf("moduleOptions().AuthScopes = %v, want %v", got, want)
	}
	data, err := os.ReadFile("../../module.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Capabilities struct {
			AuthScopes []string `json:"authScopes"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if got := slices.Sorted(slices.Values(manifest.Capabilities.AuthScopes)); !slices.Equal(got, want) {
		t.Errorf("module.json authScopes = %v, want %v", got, want)
	}
}
