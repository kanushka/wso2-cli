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
	"context"
	"fmt"

	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/auth/oauthflow"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/sdk/problem"
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

// revokeUnused revokes the refresh token of an authorization that is not
// stored, best effort as at logout (ADR 0010), so nothing outlives it at the
// provider.
func (s Shell) revokeUnused(access contexts.ProductAccess, result oauthflow.Result) {
	if result.Token == nil || result.Token.RefreshToken == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), revokeDeadline)
	defer cancel()
	oauthflow.Revoke{Issuer: access.Issuer, IssuerID: access.IssuerID, ClientID: access.ClientID,
		RefreshToken: result.Token.RefreshToken}.Run(ctx)
}

// cloudOrganization is the ThunderID organization unit a WSO2 Cloud context
// signs in to, by id, or empty when it leaves the choice to the browser.
func cloudOrganization(c contexts.Context) string {
	if !c.WSO2Cloud() {
		return ""
	}
	return c.Organization
}

// issuedIn reports the organization a token response was issued in.
func issuedIn(result oauthflow.Result) auth.Organization {
	accessToken := ""
	if result.Token != nil {
		accessToken = result.Token.AccessToken
	}
	return auth.TokenOrganization(result.IDToken, accessToken)
}

// checkIssuedIn refuses a session issued in another organization than the
// one a WSO2 Cloud context names, before it is stored: commands would
// otherwise act in an organization nobody chose. A token naming none is let
// through, since nothing in it says otherwise.
func checkIssuedIn(c contexts.Context, result oauthflow.Result) error {
	want := cloudOrganization(c)
	got := issuedIn(result)
	if want == "" || got.ID == "" || got.ID == want {
		return nil
	}
	return problem.New(problem.CategoryAuthPolicy, "auth.organization_mismatch",
		fmt.Sprintf("the identity provider issued this login in the %q organization, not in %q, which "+
			"the %q context names", got.Label(), want, c.Name)).
		WithRecovery("Run wso2 org use <organization-id> with an organization this user belongs " +
			"to, or wso2 org use \"\" and wso2 login to choose one in the browser.")
}
