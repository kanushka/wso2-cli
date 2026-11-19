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

package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/wso2/wso2-cli/internal/contexts"
)

// ClientGrantCheck is what one product's client-credentials grant issued.
type ClientGrantCheck struct {
	Namespace string
	// Granted are the scopes issued, and Withheld the recorded ones that were
	// not. A product granted nothing is one the client holds no role for.
	Granted  []string
	Withheld []string
	// Organization is the token's ouId claim, empty when it names none.
	Organization string
}

// CheckClientGrant presents a client-credentials context's credential for one
// recorded product, as a command would, before the context is written.
//
// Unlike a command's grant it reports a narrower answer rather than refusing
// it: ThunderID leaves out, with a 200, the scopes the client's roles do not
// carry, and the person setting the account up needs to see which. Anything a
// command would refuse otherwise is refused here too, as the shell reports it.
func CheckClientGrant(ctx context.Context, client *http.Client, selection contexts.Selection,
	namespace, secret string) (ClientGrantCheck, error) {
	access, found := selection.Identity.Access(namespace)
	if !found {
		return ClientGrantCheck{}, fmt.Errorf("auth: the %q product is not recorded", namespace)
	}
	if client == nil {
		client = http.DefaultClient
	}
	source := clientCredentialsSource{
		namespace:      namespace,
		contextName:    selection.Context.Name,
		issuer:         access.Issuer,
		issuerID:       access.IssuerID,
		clientID:       access.ClientID,
		resource:       access.Resource,
		audience:       access.Audience,
		secretVariable: selection.Identity.Auth.ClientSecretVariable,
	}
	endpoint, err := tokenEndpoint(ctx, client, source.issuer, source.issuerID)
	if err != nil {
		return ClientGrantCheck{}, reported(err)
	}
	form := url.Values{
		"grant_type": {"client_credentials"},
		"scope":      {strings.Join(access.Scopes, " ")},
	}
	if access.Resource != "" {
		form.Set("resource", access.Resource)
	}
	issued, err := requestToken(ctx, client, endpoint, form, clientAuth{id: source.clientID, secret: secret})
	if err != nil {
		return ClientGrantCheck{}, reported(source.refusedGrant(err, access.Resource))
	}
	check := ClientGrantCheck{Namespace: namespace, Granted: GrantedScopes(issued.Scope, issued.AccessToken)}
	for _, scope := range access.Scopes {
		if !slices.Contains(check.Granted, scope) {
			check.Withheld = append(check.Withheld, scope)
		}
	}
	if len(check.Granted) == 0 {
		return check, nil
	}
	// Proved as a command would prove a grant of exactly what was issued: a
	// scope beyond the request, or a token bound elsewhere, is refused.
	request := Request{Scopes: check.Granted}
	if slices.ContainsFunc(check.Granted, func(scope string) bool { return !slices.Contains(access.Scopes, scope) }) {
		request.Scopes = access.Scopes
	}
	if _, err := issued.verify(request, namespace, access.Audience); err != nil {
		return ClientGrantCheck{}, reported(err)
	}
	check.Organization = organizationUnit(issued.AccessToken)
	return check, nil
}

// organizationUnit is the ouId claim ThunderID stamps on a token for the
// organization unit it was issued in, empty when the token names none.
func organizationUnit(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		OUID string `json:"ouId"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	return claims.OUID
}

func reported(err error) error {
	var refused Denial
	if errors.As(err, &refused) {
		return refused.Reported()
	}
	return err
}
