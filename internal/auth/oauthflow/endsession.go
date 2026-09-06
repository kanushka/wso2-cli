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

package oauthflow

import (
	"context"
	"net/http"
	"net/url"

	oidc "github.com/coreos/go-oidc/v3/oidc"
)

// EndSession describes the browser session wso2 logout asks a provider to
// end, and where.
type EndSession struct {
	// Issuer is the OpenID provider whose end-session endpoint is read from
	// discovery.
	Issuer string
	// ClientID names the application the session was established for. Every
	// provider measured wants it: ThunderID refuses a request naming neither
	// a client nor a token, and API Manager shows a consent page without a
	// token hint.
	ClientID string
	// IDToken is the identity token the session's login returned, sent as the
	// hint of which session to end. Empty when the session recorded none.
	IDToken string
	// HTTPClient serves discovery. It defaults to http.DefaultClient.
	HTTPClient *http.Client
}

// URL is the page a browser opens to end the session, or empty when the
// provider advertises no end-session endpoint.
//
// No post-logout redirect is asked for: a provider honours one only when it
// is registered on the application, and the shell cannot tell whether it is.
// The provider's own signed-out page is where the tab ends, which is enough
// for a tab the user did not ask to keep.
func (e EndSession) URL(ctx context.Context) (string, error) {
	client := e.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, client), e.Issuer)
	if err != nil {
		return "", discoveryFailed(
			"the shell could not read the identity provider's OpenID configuration",
			"Check the issuer of the selected context and that this machine can reach it, then retry.")
	}
	var advertised struct {
		EndSessionEndpoint string `json:"end_session_endpoint"`
	}
	if err := provider.Claims(&advertised); err != nil || advertised.EndSessionEndpoint == "" {
		return "", nil
	}
	parsed, err := url.Parse(advertised.EndSessionEndpoint)
	if err != nil {
		return "", nil
	}
	query := parsed.Query()
	query.Set("client_id", e.ClientID)
	if e.IDToken != "" {
		query.Set("id_token_hint", e.IDToken)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}
