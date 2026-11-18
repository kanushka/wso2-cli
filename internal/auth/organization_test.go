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

package auth_test

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/wso2/wso2-cli/internal/auth"
)

func unsignedJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func TestTokenOrganizationReadsTheOrganizationUnitClaims(t *testing.T) {
	idToken := unsignedJWT(t, map[string]any{"sub": "u", "ouId": "ou-1", "ouName": "Acme", "ouHandle": "acme"})
	got := auth.TokenOrganization(idToken)
	if got != (auth.Organization{ID: "ou-1", Name: "Acme", Handle: "acme"}) {
		t.Errorf("organization = %+v", got)
	}
}

func TestTokenOrganizationTakesTheFirstTokenThatNamesOne(t *testing.T) {
	bare := unsignedJWT(t, map[string]any{"sub": "u"})
	access := unsignedJWT(t, map[string]any{"ouId": "ou-2", "ouHandle": "beta"})
	got := auth.TokenOrganization("", "not a jwt", bare, access)
	if got.ID != "ou-2" || got.Handle != "beta" || got.Name != "" {
		t.Errorf("organization = %+v, want the access token's", got)
	}
	if !auth.TokenOrganization(bare).IsZero() {
		t.Error("a token with no organization claim named one")
	}
}

func TestOrganizationLabel(t *testing.T) {
	for _, tc := range []struct {
		organization auth.Organization
		want         string
	}{
		{auth.Organization{ID: "ou-1", Name: "Acme", Handle: "acme"}, "Acme (acme)"},
		{auth.Organization{ID: "ou-1", Handle: "acme"}, "acme"},
		{auth.Organization{ID: "ou-1", Name: "Acme"}, "Acme"},
		{auth.Organization{ID: "ou-1"}, "ou-1"},
		{auth.Organization{}, ""},
	} {
		if got := tc.organization.Label(); got != tc.want {
			t.Errorf("Label(%+v) = %q, want %q", tc.organization, got, tc.want)
		}
	}
}
