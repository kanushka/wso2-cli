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

package smoke

import (
	"slices"
	"strings"
)

// The verdicts the refresh-narrowing experiment can reach.
//
// These are the words that get copied by hand into the research document, so
// they are defined once here rather than written out at the point they are
// printed.
const (
	// VerdictHonored: the deployment issued exactly the permissions asked for.
	VerdictHonored = "honored"
	// VerdictHonoredWithProtocolScopes: the deployment narrowed the product
	// permissions to exactly what was asked for, and kept the OpenID protocol
	// scopes the session was established with. The narrowing question is
	// answered yes; the shell still refuses the grant, because a module must
	// not receive permissions it did not ask for.
	VerdictHonoredWithProtocolScopes = "honored (protocol scopes retained)"
	// VerdictIgnored: the deployment issued a materially different permission
	// set than the one requested.
	VerdictIgnored = "ignored"
	// VerdictRejected: the token endpoint answered invalid_scope.
	VerdictRejected = "rejected"

	VerdictInconclusiveOpaque   = "inconclusive (opaque access token)"
	VerdictInconclusiveUnstated = "inconclusive (deployment stated no scope)"
	VerdictInconclusiveAudience = "inconclusive (audience not bound)"
	// VerdictInconclusiveUnreadable: the refusal did not name the permissions
	// the deployment issued, so there is nothing to compare.
	VerdictInconclusiveUnreadable = "inconclusive (unrecognized narrowing refusal)"
)

// The verdicts the session-revocation experiment can reach.
//
// They answer two questions that no document in this repository answers today:
// whether a deployment publishes a way to retract a session, and whether this
// shell — a public client with no secret — is allowed to use it. Like the
// narrowing verdicts above, these are the words copied by hand into the
// research record, so they are defined once here.
const (
	// VerdictRevocationAccepted: the deployment advertises a revocation
	// endpoint and accepted this client's request to it.
	VerdictRevocationAccepted = "advertised and accepted"
	// VerdictRevocationUnadvertised: the deployment publishes no
	// revocation_endpoint in its OpenID configuration, so nothing was asked.
	VerdictRevocationUnadvertised = "not advertised"
	// VerdictRevocationRefused: the deployment advertises the endpoint and did
	// not accept the request. The likeliest cause is that it requires a
	// confidential client there, which this shell is not.
	VerdictRevocationRefused = "advertised and refused"
)

// The verdicts of the independent check on what revocation actually did.
//
// The revocation response does not answer this. RFC 7009 requires a server to
// answer an unknown token exactly as it answers a live one, so an accepted
// request proves the deployment was told and nothing more. The only way to
// learn whether the session really died is to try to use it.
const (
	// VerdictRefreshDead: presenting the refresh token no longer renews.
	VerdictRefreshDead = "refresh token no longer renews"
	// VerdictRefreshAlive: the refresh token still renews after revocation.
	// Against an accepted revocation this is the finding worth reporting
	// upstream: the deployment said yes and kept the session alive.
	VerdictRefreshAlive = "refresh token still renews"
	// VerdictRefreshInconclusive: the token endpoint answered in a way this
	// test cannot read as either.
	VerdictRefreshInconclusive = "inconclusive (the token endpoint answered neither a renewal nor a recognized refusal)"
)

// narrowingCode is the refusal every narrowing outcome arrives as.
const narrowingCode = "auth.narrowing_unavailable"

// The phrases the shell's narrowing refusals are told apart by.
//
// Each is a stable fragment of one format string in internal/auth/narrowing.go
// or internal/auth/source_browser.go. They are matched in an order where no
// earlier phrase appears in a later phrase's message.
const (
	phraseRefusedToNarrow = "refused to narrow this session"
	phraseOpaqueToken     = "in a form the shell cannot check"
	phraseNoScopeStated   = "did not state which permissions"
	phraseAudienceUnbound = "is not bound to the"
	phraseScopeMismatch   = " and the deployment issued "
)

// protocolScopes are the OpenID scopes a session carries because it is a login,
// not because a module asked for them.
//
// oauthflow always requests openid and offline_access — the first identifies
// the flow as OpenID Connect, the second is what produces the refresh token the
// whole session is built on — and a deployment may return them on a narrowed
// refresh alongside the product permissions it correctly narrowed to. The other
// three are standard claim scopes an authorization server may attach for the
// same reason. None of them is a permission a module can ask for, so none of
// them is evidence that a narrowing request was disregarded.
var protocolScopes = []string{"openid", "offline_access", "profile", "email", "address", "phone"}

// NarrowingVerdict reads which of RFC 6749 §6's outcomes a deployment produced.
//
// It takes the typed code and message rather than the error itself so that the
// classification is a pure function of what the shell said, testable without a
// deployment. That matters more here than anywhere else in this package: the
// verdict it returns is copied by hand into a research document as a finding
// about a product, and a misclassification becomes a false claim that outlives
// the run that produced it.
//
// An empty code means the broker issued the grant.
func NarrowingVerdict(code, message string, requested []string) string {
	if code == "" {
		return VerdictHonored
	}
	if code != narrowingCode {
		return "inconclusive (" + code + ")"
	}
	switch {
	case strings.Contains(message, phraseRefusedToNarrow):
		return VerdictRejected
	case strings.Contains(message, phraseOpaqueToken):
		return VerdictInconclusiveOpaque
	case strings.Contains(message, phraseNoScopeStated):
		return VerdictInconclusiveUnstated
	case strings.Contains(message, phraseAudienceUnbound):
		return VerdictInconclusiveAudience
	case strings.Contains(message, phraseScopeMismatch):
		return scopeMismatchVerdict(message, requested)
	default:
		return VerdictInconclusiveUnreadable
	}
}

// scopeMismatchVerdict decides whether a permission set that differs from the
// requested one differs in a way that answers the question.
//
// The only difference that does not is a leftover protocol scope: the session
// was established with openid and offline_access, so a deployment that narrowed
// the product permissions exactly right can still answer with them attached.
// Reading that as a disregarded request would record the opposite of what
// happened.
func scopeMismatchVerdict(message string, requested []string) string {
	_, tail, found := strings.Cut(message, phraseScopeMismatch)
	if !found || strings.TrimSpace(tail) == "" {
		return VerdictInconclusiveUnreadable
	}
	issued := parseScopeList(tail)
	if len(issued) == 0 {
		return VerdictInconclusiveUnreadable
	}
	var product []string
	for _, scope := range issued {
		if !slices.Contains(protocolScopes, scope) {
			product = append(product, scope)
		}
	}
	if sameSet(product, requested) {
		return VerdictHonoredWithProtocolScopes
	}
	return VerdictIgnored
}

// parseScopeList reads the permission list the shell rendered with scopeList,
// which joins a sorted set with ", ".
func parseScopeList(value string) []string {
	if strings.TrimSpace(value) == "none" {
		return nil
	}
	var list []string
	for _, part := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			list = append(list, trimmed)
		}
	}
	return list
}

// sameSet reports whether two permission lists carry the same members.
func sameSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for _, scope := range left {
		if !slices.Contains(right, scope) {
			return false
		}
	}
	return true
}
