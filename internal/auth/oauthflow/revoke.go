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
	"strings"

	oidc "github.com/coreos/go-oidc/v3/oidc"
)

// Revocation is what the shell established about the issuer's own copy of a
// session, and is the only thing logout may claim about it.
//
// It is three values rather than a boolean because "the deployment was told"
// and "the deployment publishes no way to be told" are different facts with the
// same effect on the local session, and collapsing them would let the shell
// report a guarantee it did not obtain. See
// docs/adr/0010-best-effort-revocation-on-session-end.md.
type Revocation string

const (
	// RevocationConfirmed means the issuer accepted the request. It says the
	// deployment was told, not that anything was found to retract: RFC 7009
	// requires a server to answer an unknown token exactly as it answers a live
	// one, so that revocation cannot be used to probe for valid tokens.
	RevocationConfirmed Revocation = "confirmed"
	// RevocationNotAttempted means the issuer was never asked: it advertises no
	// revocation endpoint, or there was no refresh token to name in a request.
	// Nothing was refused, and nothing was proven.
	RevocationNotAttempted Revocation = "not-attempted"
	// RevocationFailed means the issuer was asked and did not accept, or could
	// not be reached at all. The two are one value because the shell cannot
	// tell them apart from the outside in a way a user could act on
	// differently, and because neither changes what happens next.
	RevocationFailed Revocation = "failed"
)

// Revoke asks an issuer to retract one session's refresh token, per RFC 7009.
type Revoke struct {
	// Issuer is the OpenID provider whose discovery document names the
	// revocation endpoint. The endpoint is discovered rather than derived, for
	// the same reason the token endpoint is: the shell reads the deployment's
	// own configuration instead of assuming a URL shape.
	Issuer string
	// ClientID is the public OAuth client this shell presents itself as. There
	// is deliberately no client secret, which is also the most likely reason a
	// deployment refuses: RFC 7009 leaves client authentication to the
	// deployment, and one that requires a confidential client cannot be
	// revoked at by this shell at all.
	ClientID string
	// RefreshToken is the credential to retract. It is the refresh token and
	// not the access token: an access token expires in minutes on every
	// deployment the shell supports, and a second round trip to retract
	// something already expiring buys no guarantee.
	RefreshToken string
	// HTTPClient serves discovery and the revocation request. It defaults to
	// http.DefaultClient.
	HTTPClient *http.Client
}

// Run performs the revocation and reports what it established.
//
// It returns no error, and that is the design rather than an omission. Logout
// removes the shell-owned session under every outcome, so a caller has nothing
// to decide here and nothing to abort: what varies is only what may be claimed
// afterwards. Making the outcome a value keeps that fact in the type instead of
// in a convention about which errors to ignore.
func (r Revoke) Run(ctx context.Context) Revocation {
	if r.RefreshToken == "" {
		// Nothing the shell holds names this session to the issuer, so there is
		// no request to make. It was not asked; it did not refuse.
		return RevocationNotAttempted
	}
	client := r.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, client), r.Issuer)
	if err != nil {
		// Discovery is how the endpoint is found, so an issuer that cannot be
		// read is an issuer that cannot be told. This is a failure and not an
		// unsupported deployment: the shell learned nothing either way, and
		// reporting "publishes no revocation endpoint" would state a fact about
		// a document it never read.
		return RevocationFailed
	}
	var advertised struct {
		RevocationEndpoint string `json:"revocation_endpoint"`
	}
	if provider.Claims(&advertised) != nil || advertised.RevocationEndpoint == "" {
		return RevocationNotAttempted
	}

	form := url.Values{
		"token":           {r.RefreshToken},
		"token_type_hint": {"refresh_token"},
		"client_id":       {r.ClientID},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		advertised.RevocationEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return RevocationFailed
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.Do(request)
	if err != nil {
		return RevocationFailed
	}
	// The body is closed and never read. RFC 7009 defines the success response
	// as having no content, and a deployment's error body may quote the request
	// that produced it — which is token material, on this endpoint, in this
	// shell that renders problems verbatim.
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode/100 != 2 {
		return RevocationFailed
	}
	return RevocationConfirmed
}
