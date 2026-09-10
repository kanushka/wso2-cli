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
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// sessionSource derives one module's access from the stored login session.
//
// The strategy is a scoped refresh: the session was granted the union of the
// identity's product permissions at login, and each module's request is a
// narrowing of it. What makes this safe is that the narrowing is verified
// rather than requested — the shell proves the token it received carries
// exactly the permissions asked for and is bound to the audience asked for, and
// refuses when it cannot. A deployment that ignores the narrowing would
// otherwise hand every module the whole session's authority.
//
// It is named for what it derives from and not for how the session was
// established, because that is the whole of the difference between the
// interactive kinds: a browser login and a device login both end at a refresh
// token in the secure store, and from there every step below is the same one.
// A second source per login mode would duplicate the rotation lock and the
// narrowing proof for no behaviour.
type sessionSource struct {
	// namespace is the record asking, named in refusals: the module's
	// namespace, or the product's gateway key.
	namespace string
	// product is the module's namespace, the one wso2 login --only is
	// narrowed to in a recovery: it establishes every record of the product.
	product string
	// ref is the secure-store entry this product's session lives under: the
	// identity's own for a direct product, the product's for every other.
	ref string
	// issuer and clientID are where and as whom the session is renewed.
	issuer, clientID string
	// audience is the concrete audience the identity registers for this
	// namespace, and the one an issued token is proved to be bound to.
	audience string
	// scopes is the scope set this access was established for: what a
	// direct or sibling session was authorized with, or the assertion
	// scopes a derived one was. renew compares it against what the stored
	// session actually recorded, to catch a login-product change from
	// under it (see renew).
	scopes []string
	// sessions reads and rotates the stored session.
	sessions session.Store
	// client serves the issuer traffic.
	client *http.Client
	// establish obtains the session when none is stored, or again when the
	// issuer will not renew the stored one to what a request asks for. nil for
	// the login session, which only wso2 login establishes.
	establish func() error
	// contextName is the context this access is brokered for, named in the
	// resource-bound recovery as the one to log out of and back into.
	contextName string
	// resourceBound is true when the deployment binds this session to one
	// resource server and mints its permissions from the roles held on it
	// (see narrowing.resourceBound). Every refusal to narrow is then given the
	// resource-bound way back instead of the general one.
	resourceBound bool
	// strategy is how this session was obtained (a contexts.Strategy* value).
	// A federated session's stored access token is served while it is valid:
	// the product's own issuer minted it for exactly the product's scopes at
	// the code exchange, and some such issuers (API Manager, measured) refuse
	// to renew a management scope on a refresh at all.
	strategy string
}

// reauthorize carries a refusal to renew out of the lock, so mint can
// authorize the product again and retry once. The refusal it wraps is what a
// caller is told when that retry cannot serve the request either.
type reauthorize struct{ refusal error }

func (r reauthorize) Error() string { return r.refusal.Error() }

// mint derives access, holding the session's rotation lock throughout.
//
// The lock covers the read, the grant, and the write of any rotated refresh
// token, because a rotating issuer invalidates what it was presented: two
// invocations refreshing the same session concurrently would leave one of them
// holding a token the issuer has already replaced.
func (s sessionSource) mint(request Request, now time.Time) (Grant, error) {
	if err := s.ensureSession(); err != nil {
		return Grant{}, err
	}
	granted, err := s.mintUnderLock(request, now)
	var again reauthorize
	if !errors.As(err, &again) {
		return granted, err
	}
	// The issuer will not renew the stored session to this request. The
	// product is authorized afresh, through the same hook that established
	// it, outside the lock; what that stores is then served exactly once. A
	// second refusal is reported as the refusal it is: an authorization the
	// deployment answers the same way twice is a registration problem.
	if err := s.establish(); err != nil {
		return Grant{}, s.orRenewalNeedsBrowser(request, err)
	}
	granted, err = s.mintUnderLock(request, now)
	if errors.As(err, &again) {
		return Grant{}, s.notAuthorizedForProduct(request)
	}
	return granted, err
}

// notAuthorizedForProduct is the refusal for a product whose issuer signed
// the user in again and still issued none of the permissions asked for.
//
// It replaces the narrowing refusal at this point because the narrowing is
// no longer the news: the issuer was given every chance to grant the
// permissions and declined, which on a federated product means the user's
// groups map to no role that carries them. The recovery names the issuer and
// the permissions, which is what an administrator needs; nothing about the
// token reaches it.
func (s sessionSource) notAuthorizedForProduct(request Request) error {
	recovery := fmt.Sprintf("Ask an administrator of %s to map this user's group to a role that carries %s, "+
		"then run wso2 login --only %s.", s.issuer, scopeList(request.Scopes), s.product)
	if s.resourceBound {
		recovery = s.narrowing(request).recovery
	}
	return denial("auth.narrowing_unavailable",
		fmt.Sprintf("the %q product's identity provider signed this user in again but issued none of "+
			"the permissions the module asked for (%s), so the user is not authorized for the product",
			s.namespace, scopeList(request.Scopes)),
		recovery)
}

// narrowing is what this source's refusals to narrow are stated in terms of:
// the general way back, or the resource-bound one on a deployment that mints a
// session's permissions from the roles held on its resource server.
func (s sessionSource) narrowing(request Request) narrowing {
	n := narrowing{namespace: s.namespace, audience: s.audience, recovery: narrowingRecovery}
	if s.resourceBound {
		n.resourceBound = true
		n.recovery = resourceBoundRecovery(request.Scopes, s.audience, s.contextName, s.product,
			s.strategy == contexts.StrategyDirect)
	}
	return n
}

// orRenewalNeedsBrowser restates the shell's "nothing may open a browser"
// marker as the refusal that belongs to this point in the flow, and passes
// every other failure to authorize through unchanged.
//
// The distinction it draws is the whole of why BrowserUnavailable is a marker.
// A session for this product is stored — mint reached here by failing to renew
// one, not by finding none — so the refusal for an absent session is false
// here, and worse, its recovery is another login, which is exactly what the
// invocation just declined to run. What the reader needs instead is that a
// session exists, that renewing it is the step that wanted a browser, and what
// to do about it.
//
// The recovery names two causes because the shell cannot tell them apart from
// here, the same ambiguity notAuthorizedForProduct is written around: another
// login, run where a browser can open, may well fix this — the issuer would be
// asked afresh and might grant the permissions this refresh would not. If it
// does not, the deployment maps this user to no role carrying them, and no
// number of logins will change that; an administrator has to. Promising only
// the first would send the reader in a circle.
func (s sessionSource) orRenewalNeedsBrowser(request Request, err error) error {
	var unavailable BrowserUnavailable
	if !errors.As(err, &unavailable) {
		return err
	}
	recovery := fmt.Sprintf("Run wso2 login --only %s where a browser can open. If that login still "+
		"cannot serve this command, this user's groups map to no role at %s that carries %s and an "+
		"administrator has to grant one; logging in again will not change that.",
		s.product, s.issuer, scopeList(request.Scopes))
	refusal := denial("auth.reauthorization_required",
		fmt.Sprintf("the %q product has a session under this account, but the identity provider "+
			"would not renew it to the permissions the module asked for (%s), and authorizing the "+
			"product again is what needed a browser",
			s.namespace, scopeList(request.Scopes)),
		recovery)
	// The control is the shell's own flag, which no module knows about, so it
	// reaches the user through the guidance and never through the problem the
	// module receives.
	refusal.Guidance = fmt.Sprintf("%s asked that no browser open. %s", unavailable.Control, recovery)
	return refusal
}

// orSessionRequired restates the same marker for the other case: no session is
// stored for this product at all, so the absent-session refusal is the true
// one and keeps the message and recovery it has always had. Only the guidance
// changes, to name the control that refused rather than to leave the reader
// wondering why a browser did not open.
func (s sessionSource) orSessionRequired(err error) error {
	var unavailable BrowserUnavailable
	if !errors.As(err, &unavailable) {
		return err
	}
	refusal := SessionRequired(s.namespace, s.product)
	refusal.Guidance = fmt.Sprintf("Run wso2 login --only %s before this command; %s asked that no "+
		"browser open.", s.product, unavailable.Control)
	return refusal
}

// mintUnderLock derives access under the session's rotation lock.
func (s sessionSource) mintUnderLock(request Request, now time.Time) (Grant, error) {
	var granted Grant
	err := s.sessions.WithLock(s.ref, func() error {
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

// ensureSession establishes the product's own session when it is absent,
// before the rotation lock is taken: a browser round trip must not hold the
// lock other invocations wait on.
func (s sessionSource) ensureSession() error {
	if s.establish == nil {
		return nil
	}
	_, err := s.sessions.Load(s.ref)
	if err == nil || !isLoginRequired(err) {
		return err
	}
	return s.orSessionRequired(s.establish())
}

// isLoginRequired reports whether err is the session store's own refusal for
// no stored session, the one case ensureSession fills in rather than passes on.
func isLoginRequired(err error) bool {
	var typed problem.Problem
	return errors.As(err, &typed) && typed.Code == "auth.login_required"
}

// derive answers one request under the lock mint holds: from the stored
// access token when the strategy allows and it covers the request, otherwise
// by one scoped refresh.
func (s sessionSource) derive(request Request, now time.Time) (Grant, error) {
	if s.strategy == contexts.StrategyFederated {
		if granted, served := s.storedGrant(request, now); served {
			return granted, nil
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), grantDeadline)
	defer cancel()
	issued, err := s.renew(ctx, request.Scopes, now)
	if err != nil {
		return Grant{}, s.orReauthorize(err)
	}
	facts, err := issued.verifyNarrowing(request, s.narrowing(request))
	if err != nil {
		return Grant{}, s.orReauthorize(err)
	}
	return Grant{Token: issued.AccessToken, ExpiresAt: issued.expiry(facts, now)}, nil
}

// orReauthorize turns a refusal to narrow into a request to authorize the
// product again, when this source has a way to. Every other refusal, and a
// narrowing refusal for the login session, passes through unchanged.
func (s sessionSource) orReauthorize(err error) error {
	var refused Denial
	if s.establish != nil && errors.As(err, &refused) && refused.Problem.Code == "auth.narrowing_unavailable" {
		return reauthorize{refusal: err}
	}
	return err
}

// protocolScopes are the scopes the shell itself adds to every authorization
// and that no product permission is spelled as. A stored access token carries
// them beside the product's scopes, and they are not what a module asked for.
//
// They must be exactly the scopes oauthflow's Login asks for beyond the
// product's own. profile and email joined that list so a login learns who
// signed in; left out of this one, every token a login stores would read as
// minted for a different request, so a federated product would never be served
// its own valid token and would fall through to a refresh the issuer refuses.
var protocolScopes = map[string]bool{
	"openid": true, "offline_access": true, "profile": true, "email": true,
}

// storedGrant serves the access token the session stored at authorization,
// when it is still valid and carries exactly the request: the product's
// audience, and the request's scopes beside nothing but the protocol's own.
// Anything else is not served, and the caller refreshes instead.
func (s sessionSource) storedGrant(request Request, now time.Time) (Grant, bool) {
	stored, err := s.sessions.Load(s.ref)
	if err != nil || stored.AccessToken == "" || !stored.ExpiresAt.After(now.Add(storedTokenMargin)) {
		return Grant{}, false
	}
	facts, err := bearerClaims(stored.AccessToken)
	if err != nil || !slices.Contains(facts.Audiences, s.audience) {
		return Grant{}, false
	}
	var carried []string
	for _, scope := range facts.Scopes {
		if !protocolScopes[scope] {
			carried = append(carried, scope)
		}
	}
	if !scopeSetsEqual(carried, request.Scopes) {
		return Grant{}, false
	}
	expiresAt := stored.ExpiresAt
	if !facts.ExpiresAt.IsZero() && facts.ExpiresAt.Before(expiresAt) {
		expiresAt = facts.ExpiresAt
	}
	return Grant{Token: stored.AccessToken, ExpiresAt: expiresAt.UTC()}, true
}

// storedTokenMargin is how much life a stored access token must have left to
// be served: enough for the module to use it, so a token that expires
// mid-command is refreshed now rather than failed later.
const storedTokenMargin = 30 * time.Second

// renew refreshes the stored session for scopes and persists any rotation,
// under the lock the caller holds.
//
// It is the whole of what the two derivations share: the narrowed refresh
// hands what comes back to the module, and the assertion derivation hands its
// identity token to another issuer. Both need the rotated refresh token
// stored before anything else happens with the answer, and this is the one
// place that stores it.
func (s sessionSource) renew(ctx context.Context, scopes []string, now time.Time) (tokenResponse, error) {
	stored, err := s.sessions.Load(s.ref)
	if err != nil {
		return tokenResponse{}, err
	}
	if stored.Issuer != s.issuer {
		// The context now names a different issuer than the one this session
		// was minted by. Refreshing against it would present one deployment's
		// token to another, so the session is treated as belonging to nobody.
		return tokenResponse{}, denial("auth.session_issuer_mismatch",
			fmt.Sprintf("the stored session for the %q product was established against a different "+
				"identity provider than the context now names", s.namespace),
			fmt.Sprintf("Run wso2 login --only %s to authorize this product against the issuer this "+
				"context names, or wso2 login to authorize every product.", s.product))
	}
	if driftedProduct := s.productDrifted(stored); driftedProduct {
		// A product whose namespace sorts before the one this session was
		// established for can become the login product without anyone
		// touching the session: LoginAccess picks the first direct product by
		// sorted namespace. The stored session then answers for scopes and a
		// client it was never authorized for, and presenting it here would
		// hand this product another product's authority.
		return tokenResponse{}, denial("auth.login_required",
			fmt.Sprintf("the stored session for the %q product was established for a different "+
				"product", s.namespace),
			fmt.Sprintf("Run wso2 login --only %s to authorize this product, or wso2 login to "+
				"authorize every product.", s.product))
	}

	endpoint, err := tokenEndpoint(ctx, s.client, s.issuer)
	if err != nil {
		return tokenResponse{}, err
	}
	// The scope carried is the caller's own request, not the identity's product
	// union: the narrowing is the point, and sending the union would ask the
	// issuer for exactly the authority the shell is trying not to hand over.
	issued, err := s.refresh(ctx, endpoint, stored.RefreshToken, scopes)
	if err != nil {
		return tokenResponse{}, s.refusedGrant(err, scopes)
	}

	// The replacement is stored before the answer is returned, and while the
	// lock is still held. A crash after this point costs an access token; a
	// crash before it would have cost the session.
	// Only the refresh token is replaced. The access token the session carries
	// is the one the login obtained, for the whole product scope union; what
	// this renewal just minted is narrower and belongs to one module for one
	// command, so it is handed over and never stored.
	if issued.RefreshToken != "" && issued.RefreshToken != stored.RefreshToken {
		stored.RefreshToken = issued.RefreshToken
		// The rotated token's own lifetime, when this rotation discloses one;
		// the zero value otherwise (R7, #112). It is reassigned rather than
		// left as whatever an earlier login or rotation happened to record,
		// because that value described a refresh token this rotation has just
		// replaced, and carrying it forward would misstate the new one's
		// lifetime as known when it is not.
		//
		// One consequence of this, worth knowing rather than fixing: an
		// issuer that discloses a lifetime at login but falls silent about it
		// on every later rotation will make wso2 whoami's Session expiry
		// silently downgrade from a timestamp to "not stated by the issuer"
		// the first time this session rotates, with nothing actually wrong.
		// That is the honest answer under R7 — this package cannot tell "the
		// issuer forgot to say" apart from "the issuer changed its mind" —
		// but it is a visible behavior change with no visible cause.
		stored.SessionExpiresAt = time.Time{}
		if issued.RefreshTokenExpiresIn > 0 {
			stored.SessionExpiresAt = now.Add(time.Duration(issued.RefreshTokenExpiresIn) * time.Second).UTC()
		}
		if err := s.sessions.Save(s.ref, stored); err != nil {
			return tokenResponse{}, err
		}
	}
	return issued, nil
}

// productDrifted reports whether the stored session was established for a
// different product than this access, judging by client ID and scope set.
//
// A legacy entry, written before session.Session recorded either field,
// carries both empty; there is nothing to compare it against, so it is
// trusted exactly as it was before this check existed. Scopes are compared
// as sets, sorted, because narrowing never depends on the order they were
// requested in.
func (s sessionSource) productDrifted(stored session.Session) bool {
	if stored.ClientID != "" && stored.ClientID != s.clientID {
		return true
	}
	if len(stored.Scopes) == 0 {
		return false
	}
	return !scopeSetsEqual(stored.Scopes, s.scopes)
}

// scopeSetsEqual compares two scope lists as sets, ignoring order.
func scopeSetsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sortedA := slices.Clone(a)
	sortedB := slices.Clone(b)
	slices.Sort(sortedA)
	slices.Sort(sortedB)
	return slices.Equal(sortedA, sortedB)
}

// refresh exchanges the stored refresh token for access narrowed to scopes.
func (s sessionSource) refresh(
	ctx context.Context, endpoint, refreshToken string, scopes []string,
) (tokenResponse, error) {
	issued, err := requestToken(ctx, s.client, endpoint, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"scope":         {strings.Join(scopes, " ")},
	}, clientAuth{id: s.clientID})
	if err != nil {
		return tokenResponse{}, err
	}
	return issued, nil
}

// refusedGrant reads why the renewal did not produce a token, and says so in
// the shell's own terms.
//
// A refusal to narrow is the one answer treated differently: it means the
// session is fine and the deployment will not scope it down, which is a
// registration problem, not a login problem. On a resource-bound deployment it
// is the answer for a session minted without the permissions, and the refusal
// names them and the resource server: the opening phrase stays the one
// test/smoke reads a live run's outcome off.
func (s sessionSource) refusedGrant(err error, scopes []string) error {
	var refusal issuerRefusal
	switch {
	case errors.As(err, &refusal) && refusal.refusedToNarrow():
		message := fmt.Sprintf("the deployment refused to narrow this session to the permissions the %q "+
			"module asked for", s.namespace)
		if s.resourceBound {
			message += fmt.Sprintf(" (%s) on the %q resource server it was minted for",
				scopeList(scopes), s.audience)
		}
		return denial("auth.narrowing_unavailable", message, s.narrowing(Request{Scopes: scopes}).recovery)
	case errors.As(err, &refusal):
		// Every other refusal — a revoked, rotated-away, or expired refresh
		// token — leaves the caller in one place: holding a session the issuer
		// will not honor. The issuer's own words are not repeated; they
		// describe a request the user did not make.
		return denial("auth.login_required",
			"the stored session was not accepted by the identity provider",
			"Run wso2 login to establish a fresh session for this context.")
	case errors.Is(err, errNoAccessToken):
		return denial("auth.login_required",
			"the identity provider's answer to this session renewal carried no access token",
			"Run wso2 login to establish a fresh session for this context.")
	default:
		return issuerUnreachable(err, s.issuer)
	}
}
