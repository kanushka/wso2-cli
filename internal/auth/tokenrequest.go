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
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// grantDeadline bounds one derivation: discovery plus the grant that
	// follows it. Both grants need one, for reasons that meet in the same
	// place — an interactive derivation holds the session's rotation lock
	// while it runs, and an automated one has nobody watching the machine it
	// is holding.
	grantDeadline = 30 * time.Second
	// grantResponseLimit bounds what the shell reads from a token endpoint.
	// A token response is a few hundred bytes; anything approaching this is not
	// one, and reading it into memory would be the endpoint's decision to make.
	grantResponseLimit = 1 << 20
)

// errIssuerSilent reports a token endpoint the shell got no readable answer
// from: a transport failure, a timeout, or a body it could not read.
var errIssuerSilent = errors.New("auth: the identity provider did not answer the token request")

// errNoAccessToken reports an accepted token request the answer to which
// carried no token. It is separate from a refusal because the deployment said
// yes; what came back is simply not usable.
var errNoAccessToken = errors.New("auth: the token response carried no access token")

// issuerRefusal is a token endpoint's own refusal, carried far enough for the
// grant that made the request to read it in its own terms.
//
// It never reaches a user. The two grants the shell runs recover from the same
// refusal differently — a session the issuer will not renew means log in again,
// while a client secret it will not take means correct the secret — so the
// mapping from refusal to denial belongs to each source and not to here.
type issuerRefusal struct {
	// status is the HTTP status the endpoint answered with.
	status int
	// code is the OAuth error member of that answer, empty when it carried
	// none.
	code string
}

func (issuerRefusal) Error() string {
	return "auth: the identity provider refused the token request"
}

// refusedToNarrow reports RFC 6749's answer for a scope the deployment will not
// issue.
//
// The reading is confined to the status the RFC defines it on. A failing or
// unauthorized endpoint may mention invalid_scope for reasons of its own, and
// reporting a broken deployment as a registration a user should go and change
// would send them to edit something that was never wrong.
func (r issuerRefusal) refusedToNarrow() bool {
	return r.status == http.StatusBadRequest && r.code == "invalid_scope"
}

// requiresResourceIndicator reports RFC 8707's answer for a request that named
// no protected resource on a deployment that decides the audience from one.
//
// It is worth telling apart from every other refusal because it is the one
// caused by the context document rather than by the deployment: the identity
// did not say which product this deployment binds access to, and the fix is a
// line in a document rather than a change to a registration.
func (r issuerRefusal) requiresResourceIndicator() bool {
	return r.status == http.StatusBadRequest && r.code == "invalid_target"
}

// rejectedClient reports the deployment declining the credentials the request
// identified its client with.
func (r issuerRefusal) rejectedClient() bool {
	return r.status == http.StatusUnauthorized || r.code == "invalid_client"
}

// clientAuth is how a token request says which OAuth client is making it.
type clientAuth struct {
	// id is the registered client identifier.
	id string
	// secret is the client's secret, empty for a public client.
	secret string
}

// requestToken runs one token request and reads the answer.
//
// Both grants the shell runs — the refresh behind an interactive session and
// the client-credentials grant behind an automated one — speak the same
// protocol, so they are spoken here once. What each makes of a refusal is what
// differs, and a refusal comes back as an issuerRefusal for the caller to read.
//
// form carries the grant's own members; how the client identifies itself is
// decided here, so neither caller has to know the rule.
func requestToken(
	ctx context.Context, client *http.Client, endpoint string,
	form url.Values, presenting clientAuth,
) (tokenResponse, error) {
	body := url.Values{}
	maps.Copy(body, form)
	if presenting.secret == "" {
		// A public client has no secret to present, so it names itself in the
		// request body, as RFC 6749 requires of one.
		body.Set("client_id", presenting.id)
	}

	post, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		strings.NewReader(body.Encode()))
	if err != nil {
		return tokenResponse{}, errIssuerSilent
	}
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("Accept", "application/json")
	if presenting.secret != "" {
		// A confidential client presents its credentials as HTTP Basic
		// authentication, which RFC 6749 requires every authorization server
		// to accept — and which keeps the secret out of the request body,
		// where an endpoint is likelier to echo it into its own logs. The
		// values are form-encoded first, as the same section requires.
		post.SetBasicAuth(url.QueryEscape(presenting.id), url.QueryEscape(presenting.secret))
	}

	answer, err := client.Do(post)
	if err != nil {
		return tokenResponse{}, errIssuerSilent
	}
	defer func() { _ = answer.Body.Close() }()
	read, err := io.ReadAll(io.LimitReader(answer.Body, grantResponseLimit))
	if err != nil {
		return tokenResponse{}, errIssuerSilent
	}

	if answer.StatusCode != http.StatusOK {
		var stated struct {
			Error string `json:"error"`
		}
		// An answer that states no error code is still a refusal; it is simply
		// one the caller will have less to go on about.
		_ = json.Unmarshal(read, &stated)
		return tokenResponse{}, issuerRefusal{status: answer.StatusCode, code: stated.Error}
	}
	var issued tokenResponse
	if json.Unmarshal(read, &issued) != nil || issued.AccessToken == "" {
		return tokenResponse{}, errNoAccessToken
	}
	return issued, nil
}

// issuerUnreachable reports a token endpoint the shell got no usable answer
// from at all.
//
// It shares the discovery failure's code because that list is closed and both
// recover the same way, but it says something different: by this point the
// issuer's configuration has already been read, so telling the user the shell
// could not read it would send them to look at something that worked.
func issuerUnreachable() error {
	return denial("auth.discovery_failed",
		"the shell could not reach the identity provider to obtain access for this command",
		"Check that this machine can reach the issuer of the selected context, then retry.")
}
