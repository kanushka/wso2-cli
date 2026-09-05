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
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/wso2/wso2-cli/internal/contexts"
)

// assertionSource derives one module's access at the product's own issuer,
// from an identity token the login session yields.
//
// It exists for a product whose issuer is not the identity's: API Manager
// behind a ThunderID login, say. The login session is still the one credential
// — the shell refreshes it for an identity token and presents that token to
// the product's token endpoint under RFC 7523's JWT bearer grant, as the public
// client the grant names. What comes back is verified exactly as a narrowed
// refresh is: the scopes must be the ones asked for and the token must be
// bound to the audience the product registers. A deployment that maps the
// user to no role and answers with some default scope is therefore refused
// before the module runs, rather than handed a token that looks like access.
//
// The identity token is verified nowhere here. It is an opaque assertion the
// product's issuer verifies against the trust an administrator configured, and
// it lives for this one derivation.
type assertionSource struct {
	// session is the login session this derives from, and everything it
	// knows about renewing and rotating.
	session sessionSource
	// grant names the issuer that takes the assertion and the client that
	// presents it.
	grant contexts.Grant
}

// mint derives access, holding the session's rotation lock throughout.
//
// The lock covers the renewal, so the rotated refresh token is stored before
// the assertion is presented anywhere: a refusal from the product's issuer must
// never cost the session. One deadline bounds both round trips, because the
// lock's own deadline is what other invocations wait against and it has to
// outlast the whole of this.
func (s assertionSource) mint(request Request, now time.Time) (Grant, error) {
	var granted Grant
	err := s.session.sessions.WithLock(s.session.identity.Auth.CredentialRef, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), grantDeadline)
		defer cancel()
		issued, err := s.derive(ctx, request, now)
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

func (s assertionSource) derive(ctx context.Context, request Request, now time.Time) (Grant, error) {
	renewed, err := s.session.renew(ctx, s.grant.AssertionScopes(), now)
	if err != nil {
		return Grant{}, err
	}
	if renewed.IDToken == "" {
		return Grant{}, denial("auth.narrowing_unavailable",
			fmt.Sprintf("the identity provider renewed the session without an identity token, and the "+
				"%q product is derived by presenting one", s.session.namespace),
			"Grant the registered OAuth application the openid scope so a renewal carries an "+
				"identity token, log in again, then retry the command.")
	}

	endpoint, err := tokenEndpoint(ctx, s.session.client, s.grant.Issuer)
	if err != nil {
		return Grant{}, err
	}
	issued, err := requestToken(ctx, s.session.client, endpoint, url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {renewed.IDToken},
		"scope":      {strings.Join(request.Scopes, " ")},
	}, clientAuth{id: s.grant.ClientID})
	if err != nil {
		return Grant{}, s.refusedGrant(err)
	}
	// The product's refresh token, if it issued one, is not read: the session
	// is the one credential, and a second one stored per product would be a
	// second thing to rotate, revoke and lose.
	facts, err := issued.verify(request, s.session.namespace, s.session.audience)
	if err != nil {
		return Grant{}, err
	}
	return Grant{Token: issued.AccessToken, ExpiresAt: issued.expiry(facts, now)}, nil
}

// refusedGrant reads why the product's issuer did not take the assertion, in
// the shell's own terms.
//
// A refusal to narrow means the same thing it means for a session: the issuer
// will not scope down. Every other refusal is about trust — the issuer does not
// know the login provider, the assertion carries an audience it did not
// register, or the client presenting it is not the one it expects — and that is
// configuration on the product, not a login the user can redo.
func (s assertionSource) refusedGrant(err error) error {
	var refusal issuerRefusal
	switch {
	case errors.As(err, &refusal) && refusal.refusedToNarrow():
		return denial("auth.narrowing_unavailable",
			fmt.Sprintf("the %q product's issuer refused to issue the permissions the module asked for",
				s.session.namespace),
			narrowingRecovery)
	case errors.As(err, &refusal), errors.Is(err, errNoAccessToken):
		return denial("auth.trust_not_configured",
			fmt.Sprintf("the %q product's issuer did not accept the identity provider's assertion for "+
				"this login", s.session.namespace),
			fmt.Sprintf("Check that %s trusts %s as an identity provider, registers %q as the audience "+
				"it accepts assertions for, and maps this user's groups to a role that carries the "+
				"permissions asked for; then retry the command.",
				s.grant.Issuer, s.session.identity.Auth.Issuer, s.session.identity.Auth.ClientID))
	default:
		return issuerUnreachable()
	}
}
