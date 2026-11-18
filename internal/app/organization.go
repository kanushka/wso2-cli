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
	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
)

// reportedOrganization is the organization a context acts in, as whoami and
// org current report it. A token that names its organization is the truth of
// where a session acts, so it wins; otherwise the organization the context
// records stands, as an id with nothing else known about it.
type reportedOrganization struct {
	// Organization is the handle, else the id: what a person or a script
	// names the organization by.
	Organization string `json:"organization"`
	// OrganizationID and OrganizationName are the token's ouId and ouName.
	OrganizationID   string `json:"organizationId,omitempty"`
	OrganizationName string `json:"organizationName,omitempty"`
	label            string
}

// organizationOf reads the organization from a stored login session, falling
// back to what the context records.
func organizationOf(stored *session.Session, recorded string) reportedOrganization {
	if stored != nil {
		if token := auth.TokenOrganization(stored.IDToken, stored.AccessToken); !token.IsZero() {
			name := token.Handle
			if name == "" {
				name = token.ID
			}
			return reportedOrganization{Organization: name, OrganizationID: token.ID,
				OrganizationName: token.Name, label: token.Label()}
		}
	}
	return reportedOrganization{Organization: recorded, label: recorded}
}

// loginSessionOf loads the selected context's login session when one is
// stored for the issuer it names, and nil otherwise.
func loginSessionOf(root string, selected contexts.Selection) *session.Session {
	if selected.Identity.Auth.Kind == contexts.KindClientCredentials {
		return nil
	}
	stored, err := (session.Store{StateRoot: root}).Load(selected.Identity.Auth.CredentialRef)
	if err != nil || !sessionServes(stored, selected.Identity.Auth.Issuer) {
		return nil
	}
	return &stored
}
