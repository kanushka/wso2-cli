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
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/exit"
)

func TestLoginReportsTheOrganizationTheReadyProductsAndANextStep(t *testing.T) {
	login := newCloudLoginWith(t, fakeissuer.Options{OrganizationUnits: cloudOrgs})
	installCloudAPIM(t, login.shell)
	installCloudIAM(t, login.shell)

	login.run(t, "login")

	out := login.out.String()
	for _, field := range [][2]string{
		{"Context", "cloud-dev"},
		{"Organization", "Acme Corp (acme)"},
		{"Products", "apim, iam"},
		{"Status", "logged in"},
	} {
		if !hasField(out, field[0], field[1]) {
			t.Errorf("the login report has no %s %q:\n%s", field[0], field[1], out)
		}
	}
	if !strings.Contains(out, "Next") || !strings.Contains(out, "wso2 apim status") {
		t.Errorf("the login report names no next step:\n%s", out)
	}
}

func TestLoginMarksAProductWhoseScopesWereWithheld(t *testing.T) {
	login := newCloudLoginWith(t, fakeissuer.Options{WithheldScopes: []string{"ap:api:manage"}})
	installCloudAPIM(t, login.shell)

	login.run(t, "login")

	if !hasField(login.out.String(), "Products", "apim (without ap:api:manage)") {
		t.Errorf("the login report does not mark the withheld scope:\n%s", login.out)
	}
}

func TestLoginWithNoProductNamesHowToAddOne(t *testing.T) {
	keyring.MockInit()
	login := fakeissuer.New(t, fakeissuer.Options{RefreshScopeMode: "honor"})
	document := browserDoc(login.URL)
	document.Contexts[0].Products = nil
	shell, out, errOut := newLoginShell(t)
	installLogin(t, shell, document)
	followBrowser(&shell)
	if code := shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	if !hasField(out.String(), "Products", "none") || !strings.Contains(out.String(), "wso2 context product add") {
		t.Errorf("the login report does not say how to add a product:\n%s", out)
	}
}
