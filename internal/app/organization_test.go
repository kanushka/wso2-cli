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
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
)

var (
	acmeUnit  = fakeissuer.OrganizationUnit{ID: "0d5e8b1c-3f4a-4c2e-9a1b-ac0e00000001", Name: "Acme Corp", Handle: "acme"}
	betaUnit  = fakeissuer.OrganizationUnit{ID: "7f21c4aa-9b2d-4e8f-8c3a-be7a00000002", Name: "Beta Labs", Handle: "beta"}
	cloudOrgs = []fakeissuer.OrganizationUnit{acmeUnit, betaUnit}
)

func TestWhoamiReadsTheOrganizationFromTheSignedInToken(t *testing.T) {
	login := newCloudLoginWith(t, fakeissuer.Options{OrganizationUnits: cloudOrgs})
	installCloudAPIM(t, login.shell)
	login.run(t, "login")

	login.out.Reset()
	login.run(t, "whoami", "--output", "json")
	var report struct {
		Organization     string `json:"organization"`
		OrganizationID   string `json:"organizationId"`
		OrganizationName string `json:"organizationName"`
	}
	if err := json.Unmarshal(login.out.Bytes(), &report); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, login.out)
	}
	if report.Organization != "acme" || report.OrganizationID != acmeUnit.ID || report.OrganizationName != "Acme Corp" {
		t.Errorf("whoami JSON organization = %+v, want the token's handle, id and name", report)
	}

	login.out.Reset()
	login.run(t, "whoami")
	if !strings.Contains(login.out.String(), "Acme Corp (acme)") {
		t.Errorf("whoami does not name the organization:\n%s", login.out)
	}
}

func TestOrgCurrentReadsTheOrganizationFromTheSignedInToken(t *testing.T) {
	login := newCloudLoginWith(t, fakeissuer.Options{OrganizationUnits: cloudOrgs})
	installCloudAPIM(t, login.shell)
	login.run(t, "login")

	login.out.Reset()
	login.run(t, "org", "current")
	if !strings.Contains(login.out.String(), "Acme Corp (acme)") || strings.Contains(login.out.String(), "no organization") {
		t.Errorf("org current does not report the token's organization:\n%s", login.out)
	}

	login.out.Reset()
	login.run(t, "org", "current", "--output", "json")
	var report struct {
		Organization   string `json:"organization"`
		OrganizationID string `json:"organizationId"`
	}
	if err := json.Unmarshal(login.out.Bytes(), &report); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, login.out)
	}
	if report.Organization != "acme" || report.OrganizationID != acmeUnit.ID {
		t.Errorf("org current JSON = %+v", report)
	}
}

func TestWhoamiWithoutAnOrganizationClaimReportsNone(t *testing.T) {
	login := newCloudLogin(t)
	installCloudAPIM(t, login.shell)
	login.run(t, "login")
	login.out.Reset()
	login.run(t, "whoami")
	if strings.Contains(login.out.String(), "Organization") {
		t.Errorf("whoami names an organization no token carries:\n%s", login.out)
	}
}
