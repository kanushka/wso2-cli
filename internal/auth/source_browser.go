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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
)

const (
	// refreshDeadline bounds one derivation: discovery plus the refresh grant.
	// It exists because the derivation holds the session's rotation lock, and a
	// hung issuer must not keep every other invocation of this shell waiting.
	refreshDeadline = 30 * time.Second
	// refreshResponseLimit bounds what the shell reads from a token endpoint.
	// A token response is a few hundred bytes; anything approaching this is not
	// one, and reading it into memory would be the endpoint's decision to make.
	refreshResponseLimit = 1 << 20
)

// browserSource derives one module's access from the stored login session.
//
// The strategy is a scoped refresh: the session was granted the union of the
// identity's product permissions at login, and each module's request is a
// narrowing of it. What makes this safe is that the narrowing is verified
// rather than requested — the shell proves the token it received carries
// exactly the permissions asked for and is bound to the audience asked for, and
// refuses when it cannot. A deployment that ignores the narrowing would
// otherwise hand every module the whole session's authority.
type browserSource struct {
	// namespace is the module asking, named in refusals.
	namespace string
	// identity is the logged-in identity: issuer, client, and the secure-store
	// reference its session lives under. It holds no credential.
	identity contexts.Identity
	// sessions reads and rotates the stored session.
	sessions session.Store
	// client serves the issuer traffic.
	client *http.Client
}

// mint derives access, holding the session's rotation lock throughout.
//
// The lock covers the read, the grant, and the write of any rotated refresh
// token, because a rotating issuer invalidates what it was presented: two
// invocations refreshing the same session concurrently would leave one of them
// holding a token the issuer has already replaced.
func (s browserSource) mint(request Request, now time.Time) (Grant, error) {
	var granted Grant
	err := s.sessions.WithLock(s.identity.Auth.CredentialRef, func() error {
		issued, err := s.derive(request, now)
		if err != nil {
			return err
		}
		granted = issued
		return nil
	})
	if err != nil {
		return Grant{}, err
	}
	return granted, nil
}

// derive runs one scoped refresh under the lock mint holds.
func (s browserSource) derive(request Request, now time.Time) (Grant, error) {
	stored, err := s.sessions.Load(s.identity.Auth.CredentialRef)
	if err != nil {
		return Grant{}, err
	}
	if stored.Issuer != s.identity.Auth.Issuer {
		// The context now names a different issuer than the one this session
		// was minted by. Refreshing against it would present one deployment's
		// token to another, so the session is treated as belonging to nobody.
		return Grant{}, denial("auth.session_issuer_mismatch",
			fmt.Sprintf("the stored session for the %q identity was established against a different "+
				"identity provider than the context now names", s.identity.Name),
			"Run wso2 login to establish a session against the issuer this context names.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), refreshDeadline)
	defer cancel()
	endpoint, err := tokenEndpoint(ctx, s.client, s.identity.Auth.Issuer)
	if err != nil {
		return Grant{}, err
	}
	// The scope carried is the module's own request, not the identity's product
	// union: the narrowing is the point, and sending the union would ask the
	// issuer for exactly the authority the shell is trying not to hand over.
	issued, err := s.refresh(ctx, endpoint, stored.RefreshToken, request.Scopes)
	if err != nil {
		return Grant{}, err
	}
	facts, err := issued.verify(request, s.namespace)
	if err != nil {
		return Grant{}, err
	}

	// The replacement is stored before the grant is returned, and while the
	// lock is still held. A crash after this point costs an access token; a
	// crash before it would have cost the session.
	// Only the refresh token is replaced. The access token the session carries
	// is the one the login obtained, for the whole product scope union; what
	// this derivation just minted is narrower and belongs to one module for one
	// command, so it is handed over and never stored.
	if issued.RefreshToken != "" && issued.RefreshToken != stored.RefreshToken {
		stored.RefreshToken = issued.RefreshToken
		if err := s.sessions.Save(s.identity.Auth.CredentialRef, stored); err != nil {
			return Grant{}, err
		}
	}
	return Grant{Token: issued.AccessToken, ExpiresAt: issued.expiry(facts, now)}, nil
}

// refresh exchanges the stored refresh token for access narrowed to scopes.
func (s browserSource) refresh(
	ctx context.Context, endpoint, refreshToken string, scopes []string,
) (tokenResponse, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		// A public client has no secret to present, so it identifies itself in
		// the request body, as RFC 6749 requires of one.
		"client_id": {s.identity.Auth.ClientID},
		"scope":     {strings.Join(scopes, " ")},
	}
	post, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, renewalUnreachable()
	}
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("Accept", "application/json")
	answer, err := s.client.Do(post)
	if err != nil {
		return tokenResponse{}, renewalUnreachable()
	}
	defer func() { _ = answer.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(answer.Body, refreshResponseLimit))
	if err != nil {
		return tokenResponse{}, renewalUnreachable()
	}

	if answer.StatusCode != http.StatusOK {
		return tokenResponse{}, s.refusedGrant(answer.StatusCode, body)
	}
	var issued tokenResponse
	if json.Unmarshal(body, &issued) != nil || issued.AccessToken == "" {
		return tokenResponse{}, denial("auth.login_required",
			"the identity provider's answer to this session renewal carried no access token",
			"Run wso2 login to establish a fresh session for this context.")
	}
	return issued, nil
}

// refusedGrant reads why the issuer refused, and says so in the shell's own
// terms. A refusal to narrow is the one answer the shell treats differently:
// it means the session is fine and the deployment will not scope it down, which
// is a registration problem, not a login problem.
//
// That reading is confined to the status RFC 6749 defines it on. A failing or
// unauthorized endpoint may mention invalid_scope for reasons of its own, and
// reporting a broken deployment as a registration a user should go and change
// would send them to edit something that was never wrong.
func (s browserSource) refusedGrant(status int, body []byte) error {
	var refusal struct {
		Error string `json:"error"`
	}
	if status == http.StatusBadRequest &&
		json.Unmarshal(body, &refusal) == nil && refusal.Error == "invalid_scope" {
		return denial("auth.narrowing_unavailable",
			fmt.Sprintf("the deployment refused to narrow this session to the permissions the %q "+
				"module asked for", s.namespace),
			narrowingRecovery)
	}
	// Everything else — a revoked, rotated-away, or expired refresh token —
	// leaves the caller in one place: holding a session the issuer will not
	// honor. The issuer's own words are not repeated; they describe a request
	// the user did not make.
	return denial("auth.login_required",
		"the stored session was not accepted by the identity provider",
		"Run wso2 login to establish a fresh session for this context.")
}

// renewalUnreachable reports a token endpoint the shell could not complete a
// renewal against.
//
// It is deliberately distinct from the discovery failure above: by this point
// the issuer's configuration has already been read, so telling the user the
// shell could not read it would send them to look at something that worked.
func renewalUnreachable() error {
	return denial("auth.discovery_failed",
		"the shell could not reach the identity provider to renew this session",
		"Check that this machine can reach the issuer of the selected context, then retry.")
}
