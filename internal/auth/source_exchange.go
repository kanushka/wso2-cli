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
	"slices"
	"time"
)

// exchangeGrantType is RFC 8693's grant, and accessTokenType the only subject
// and issued token type this shell speaks.
const (
	exchangeGrantType = "urn:ietf:params:oauth:grant-type:token-exchange"
	accessTokenType   = "urn:ietf:params:oauth:token-type:access_token"
)

// exchangeSource answers one module from the login session, by exchanging that
// session's own access token at the login issuer for one bound to the
// product's audience.
//
// It exists for a product whose deployment validates the login provider's
// tokens directly — the WSO2 API Platform behind a ThunderID login, where the
// control plane, the gateway controller and the gateway all verify Thunder's
// signature through its JWKS. Such a product authorizes nothing itself, so
// there is nothing to authorize against: no second browser round trip, no
// second client registration, and no session of its own. What the module
// receives is minted for this one command and stored nowhere.
//
// The difference from every other source is what is proved about the answer.
// A narrowed refresh or a bearer assertion is proved to carry exactly the
// permissions the module asked for. An exchange cannot be: measured against
// ThunderID on 2026-09-09, an exchanged token carries the subject's OIDC
// scopes and drops the resource server permissions entirely, and the product
// authorizes the call from the claims instead — the group the token carries,
// mapped to a role at the product. So the binding is what is proved here, and
// it is proved strictly, because it is the only thing standing between a
// module and a token minted for some other product: a deployment that answers
// an exchange with a token bound elsewhere returns 200 while doing it.
type exchangeSource struct {
	// login is the login session this exchanges, and everything it knows
	// about renewing and rotating.
	login sessionSource
	// namespace is the record asking, named in refusals.
	namespace string
	// product is the module's namespace, named in a recovery.
	product string
	// audience is what the identity registers for this product: what the
	// exchange asks for as its resource, and what the issued token is proved
	// to be bound to.
	audience string
}

// mint exchanges the login session's access token, holding that session's
// rotation lock throughout.
//
// The lock covers the renewal for the same reason the assertion source's does:
// the rotated refresh token is stored before anything is exchanged, so a
// refusal from the exchange can never cost the session.
func (s exchangeSource) mint(request Request, now time.Time) (Grant, error) {
	if err := s.login.ensureSession(); err != nil {
		return Grant{}, err
	}
	var granted Grant
	err := s.login.sessions.WithLock(s.login.ref, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), grantDeadline)
		defer cancel()
		issued, err := s.exchange(ctx, request, now)
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

// exchange renews the login session and trades the access token it yields for
// one bound to this product.
//
// The renewal asks for the login session's own scopes rather than the
// module's. That is not a missed narrowing: the subject token is not what the
// module receives, and asking for the module's permissions here would narrow
// the login session itself — on ThunderID and Identity Server permanently, so
// the next product's command would be refused by a session the shell had
// quietly shrunk. This is the failure ADR 0014 recorded against one shared
// narrowed session, reached from a different direction.
func (s exchangeSource) exchange(ctx context.Context, request Request, now time.Time) (Grant, error) {
	renewed, err := s.login.renew(ctx, s.login.scopes, now)
	if err != nil {
		return Grant{}, err
	}
	endpoint, err := tokenEndpoint(ctx, s.login.client, s.login.issuer)
	if err != nil {
		return Grant{}, err
	}
	issued, err := requestToken(ctx, s.login.client, endpoint, url.Values{
		"grant_type":         {exchangeGrantType},
		"subject_token":      {renewed.AccessToken},
		"subject_token_type": {accessTokenType},
		// RFC 8707's resource, and deliberately not RFC 8693's audience.
		// Measured against ThunderID: audience names the client rather than
		// the protected resource, so a request that used it would come back
		// 200 with a token bound to the shell itself.
		"resource": {s.audience},
	}, clientAuth{id: s.login.clientID})
	if err != nil {
		return Grant{}, s.refusedExchange(err)
	}
	// No refresh token is read, and none is expected: an exchange answers with
	// an access token alone. Nothing here could be stored as a session, which
	// is the property the strategy is built on rather than a limitation of it.
	facts, err := s.verifyBinding(issued)
	if err != nil {
		return Grant{}, err
	}
	return Grant{Token: issued.AccessToken, ExpiresAt: issued.expiry(facts, now)}, nil
}

// verifyBinding proves an exchanged token is one this module may be handed:
// readable, bound to the audience the identity registers for this product, and
// carrying a lifetime somebody stated.
//
// Scopes are not checked, and their absence is not a refusal. That is the one
// place this differs from the narrowing every other source proves, and it is
// a deliberate consequence of the grant rather than an omission: an exchange
// carries no resource server permissions across, so there would be nothing to
// compare the request against and a check would refuse every honest answer.
// The product authorizes the call itself, from the claims the token carries.
func (s exchangeSource) verifyBinding(issued tokenResponse) (bearerFacts, error) {
	facts, err := bearerClaims(issued.AccessToken)
	if err != nil {
		return bearerFacts{}, denial("auth.exchange_unusable",
			fmt.Sprintf("the identity provider exchanged the session for access to the %q product in a "+
				"form the shell cannot check the audience binding of", s.namespace),
			s.registrationRecovery())
	}
	if !slices.Contains(facts.Audiences, s.audience) {
		return bearerFacts{}, denial("auth.exchange_unusable",
			fmt.Sprintf("the identity provider exchanged the session for access that is not bound to "+
				"the %q audience this account registers for the %q product",
				s.audience, s.namespace),
			s.registrationRecovery())
	}
	if issued.ExpiresIn <= 0 && facts.ExpiresAt.IsZero() {
		return bearerFacts{}, denial("auth.exchange_unusable",
			fmt.Sprintf("the identity provider stated no lifetime for the access it exchanged for the "+
				"%q product, and the token claims none either", s.namespace),
			s.registrationRecovery())
	}
	return facts, nil
}

// registrationRecovery is the way back from an exchange that answered but
// answered unusably. Every cause is a registration at the login provider, and
// naming the resource server is what an administrator acts on.
func (s exchangeSource) registrationRecovery() string {
	return fmt.Sprintf("Register %q at %s as the resource server identifier for the %q product, so an "+
		"exchange for it mints a token bound to that audience, then retry the command.",
		s.audience, s.login.issuer, s.product)
}

// refusedExchange states a refused exchange in terms of the registration
// behind it.
//
// The grant being unregistered is called out on its own because it is the
// commonest way this fails and the protocol error alone leads nowhere: a
// client that has not been given the exchange grant answers unauthorized_client,
// which reads as a credential problem and is not one.
func (s exchangeSource) refusedExchange(err error) error {
	var refusal issuerRefusal
	if !errors.As(err, &refusal) {
		if errors.Is(err, errNoAccessToken) {
			return denial("auth.exchange_unusable",
				fmt.Sprintf("the identity provider accepted the exchange for the %q product and "+
					"returned no access token", s.namespace),
				s.registrationRecovery())
		}
		return issuerUnreachable(err, s.login.issuer)
	}
	if refusal.unauthorizedGrant() || refusal.rejectedClient() {
		return denial("auth.exchange_unavailable",
			fmt.Sprintf("the identity provider will not exchange this session for access to the %q "+
				"product, because the OAuth application this shell logs in as is not registered for "+
				"the token exchange grant", s.namespace),
			fmt.Sprintf("Add the urn:ietf:params:oauth:grant-type:token-exchange grant to the %q "+
				"application at %s, then retry the command.", s.login.clientID, s.login.issuer))
	}
	if refusal.rejectedTarget() {
		return denial("auth.exchange_unavailable",
			fmt.Sprintf("the identity provider does not recognize the %q audience this account "+
				"registers for the %q product", s.audience, s.namespace),
			s.registrationRecovery())
	}
	return denial("auth.exchange_unavailable",
		fmt.Sprintf("the identity provider refused to exchange this session for access to the %q product",
			s.namespace),
		fmt.Sprintf("Run wso2 login to establish the session again. If the refusal persists, the %q "+
			"application at %s is not registered to exchange for %q.",
			s.login.clientID, s.login.issuer, s.audience))
}
