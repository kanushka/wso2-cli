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

package app

import (
	"testing"

	"github.com/wso2/wso2-cli/internal/contexts"
)

// The module is told the selected context's project beside its organization
// and the product's location, and nothing when the context names none.
func TestTheInvocationCarriesTheSelectedContextsProject(t *testing.T) {
	selection := contexts.Selection{
		Context: contexts.Context{Name: "wso2cloud-dev", Organization: "acme", Project: "orders"},
		Identity: contexts.Account{Products: map[string]contexts.Product{
			"intg": {Endpoint: "https://api.example/ipaas"},
		}},
	}

	got := invocationContext(selection, "intg")

	if got.Name != "wso2cloud-dev" || got.OrganizationID != "acme" || got.Project != "orders" ||
		got.Endpoint != "https://api.example/ipaas" {
		t.Errorf("the invocation context is %+v, want the selected context with its project", got)
	}

	selection.Context.Project = ""
	if got := invocationContext(selection, "intg"); got.Project != "" {
		t.Errorf("a context naming no project sent %q", got.Project)
	}
}
