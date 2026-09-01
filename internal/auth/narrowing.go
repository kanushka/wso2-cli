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
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/wso2/wso2-cli/internal/contexts"
)

// tokenResponse is what a token endpoint answers a refresh grant with.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	// ExpiresIn is the access token's lifetime in seconds. It decodes through
	// optionalSeconds, not a plain int64, because a single member of the wrong
	// shape fails the whole Unmarshal — and an issuer that states this standard
	// field as a string would have had its access token discarded along with
	// it, leaving the caller an errNoAccessToken that names nothing about the
	// real cause. Zero means not stated, and expiry() then falls back to the
	// JWT exp rather than inventing a lifetime. #131.
	ExpiresIn optionalSeconds `json:"expires_in"`
	// RefreshTokenExpiresIn is the rotated refresh token's own lifetime, in
	// seconds, when the issuer states one, decoded through optionalSeconds so
	// a string-shaped value — seen in the wild for this non-standard OAuth
	// extension — narrows to "not stated" instead of failing the surrounding
	// Unmarshal. Zero means the issuer stated none, or stated something this
	// package cannot read as a positive number of seconds; R7 (#112) treats
	// that as the expected case, and source_session.go's rotation path leaves
	// session.Session.SessionExpiresAt at the zero value rather than
	// inventing a substitute.
	RefreshTokenExpiresIn optionalSeconds `json:"refresh_token_expires_in"`
}

// optionalSeconds decodes a token-response member that states a lifetime in
// seconds, when an issuer sends one, as either a JSON number or a JSON
// string, and treats every other shape — absent, a bool, an object, a
// malformed or non-positive value — as not stated. It never returns an error
// from UnmarshalJSON: see LifetimeSeconds for why neither lifetime member,
// standard or not, may be allowed to fail the token exchange it decorates.
type optionalSeconds int64

// UnmarshalJSON implements json.Unmarshaler. It swallows every shape it
// cannot read as a positive number of seconds, including malformed JSON at
// this member's own position, rather than propagating an error: the
// alternative would let a single non-standard field spelling fail a whole
// refresh grant that would otherwise have succeeded, which is the defect this
// type exists to close.
func (o *optionalSeconds) UnmarshalJSON(data []byte) error {
	var raw any
	if err := json.Unmarshal(data, &raw); err == nil {
		seconds, _ := LifetimeSeconds(raw)
		*o = optionalSeconds(seconds)
	} else {
		*o = 0
	}
	return nil
}

// LifetimeSeconds interprets one already-decoded value as a lifetime in
// seconds, accepting either shape an issuer states one as — a JSON number or a
// JSON string — and reporting every other shape (absent, a bool, an object, a
// malformed or non-positive number) as not stated rather than as an error.
//
// It governs both lifetime members of a token response. expires_in is a number
// by RFC 6749 §5.1 and refresh_token_expires_in is a non-standard extension,
// but issuers state either as a string in the wild, and neither is worth
// failing an exchange over: both only supply an expiry, which is a display and
// caching concern, while the same response carries a credential.
//
// It is exported, and takes the value rather than raw JSON bytes, so both
// halves of this shell can apply the identical rule to the identical wire
// shape without drifting from each other: optionalSeconds.UnmarshalJSON above
// calls it after decoding the member into an any, and
// internal/app/login.go's *oauth2.Token.Extra("refresh_token_expires_in")
// already returns the same already-decoded shape — encoding/json.Unmarshal
// into an any yields float64 for a JSON number and string for a JSON string,
// regardless of which of the two call sites did the decoding.
func LifetimeSeconds(value any) (int64, bool) {
	switch v := value.(type) {
	case float64:
		// Compared before the conversion, not after: converting a float64
		// larger than an int64 can hold is implementation-defined in Go, so a
		// bound checked on the result would be reading a value that is already
		// meaningless.
		if v > 0 && v <= float64(maxLifetimeSeconds) {
			return int64(v), true
		}
	case string:
		if seconds, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil &&
			seconds > 0 && seconds <= maxLifetimeSeconds {
			return seconds, true
		}
	}
	return 0, false
}

// maxLifetimeSeconds is the largest lifetime that survives conversion to a
// time.Duration, which counts nanoseconds in an int64. A larger value wraps
// when multiplied by time.Second and yields an expiry in the past, so a caller
// would report a session that has just been issued as long expired. Anything
// above it is treated as unstated, on the same principle as every other shape
// this function refuses: either lifetime member must never produce an answer
// worse than saying nothing.
const maxLifetimeSeconds = int64(math.MaxInt64) / int64(time.Second)

// expiry is when the issued token stops working: what the response said, or
// what the token itself claims when the response said nothing.
func (r tokenResponse) expiry(facts bearerFacts, now time.Time) time.Time {
	if r.ExpiresIn > 0 {
		return now.Add(time.Duration(int64(r.ExpiresIn)) * time.Second).UTC()
	}
	return facts.ExpiresAt
}

// narrowingRecovery is the way back from every refusal to hand a module a grant
// the shell could not prove was narrowed to its request.
const narrowingRecovery = "Check the deployment's API resource registration and the permissions " +
	"granted to the registered OAuth application, then retry. The shell does not hand a module " +
	"broader access than it asked for."

// indicatorRecovery is the way back from a deployment that will not issue
// access without being told which protected resource it is for.
//
// It is a different instruction from every other narrowing refusal, because
// nothing about the deployment is wrong: the context document did not say what
// kind of deployment this is, so the shell asked in a shape this one does not
// accept.
const indicatorRecovery = "This deployment binds access to one named resource and will not issue " +
	"any without being told which. Name the deployment's identity provider on this identity in " +
	"the context document, or set its derivation to " + contexts.DerivationTokenResource +
	" explicitly, then retry."

// unknownResourceRecovery is the way back from a deployment that was told which
// protected resource, and does not know the one it was told.
//
// It is the opposite failure to indicatorRecovery arriving as the same OAuth
// error. The identity already says how this deployment derives access; what is
// wrong is the name it derives against, which is a registration on the
// deployment or a value in the document, and never the derivation itself.
const unknownResourceRecovery = "Register that resource server on the deployment, or correct the " +
	"audience on this identity's product entry to one it knows, then retry."

// verify proves an issued token is exactly what the module asked for.
//
// It is the check the whole derivation exists to make. A deployment may answer
// a narrowed request with the session's full authority, or with a token bound
// to some other audience, and both look like success at the protocol level. The
// shell refuses rather than degrades: a module that receives more than it asked
// for has been handed authority nobody decided to give it, and a module that
// receives a token its audience will reject fails later for a reason no one can
// diagnose from where it fails.
func (r tokenResponse) verify(request Request, namespace, audience string) (bearerFacts, error) {
	facts, err := bearerClaims(r.AccessToken)
	if err != nil {
		// Without readable claims the shell cannot prove the audience binding,
		// and an unprovable grant is not one this broker issues.
		return bearerFacts{}, denial("auth.narrowing_unavailable",
			fmt.Sprintf("the deployment issued access for the %q module in a form the shell cannot "+
				"check against what the module asked for", namespace),
			narrowingRecovery)
	}
	// The response's own statement wins, because it is the deployment speaking
	// about what it issued. The token's claim answers for issuers that state
	// nothing.
	effective := strings.Fields(r.Scope)
	if len(effective) == 0 {
		effective = facts.Scopes
	}
	if len(effective) == 0 {
		return bearerFacts{}, denial("auth.narrowing_unavailable",
			fmt.Sprintf("the deployment did not state which permissions it issued for the %q module, "+
				"so the shell cannot prove they are the ones it asked for", namespace),
			narrowingRecovery)
	}
	if !sameScopeSet(effective, request.Scopes) {
		// Scope names are not secrets, and naming both sides is the difference
		// between a refusal and a registration a user can go and fix.
		return bearerFacts{}, denial("auth.narrowing_unavailable",
			fmt.Sprintf("the %q module asked for the permissions %s and the deployment issued %s",
				namespace, scopeList(request.Scopes), scopeList(effective)),
			narrowingRecovery)
	}
	// The binding is proved against the audience the identity registers for
	// this product, not against the logical name the module asked by. The
	// module's name is a constant compiled into it and says nothing about any
	// deployment; the registered value is what this deployment stamps into aud,
	// and it is what a person authorized against a real tenant.
	if !slices.Contains(facts.Audiences, audience) {
		return bearerFacts{}, denial("auth.narrowing_unavailable",
			fmt.Sprintf("the deployment issued access for the %q module that is not bound to the %q "+
				"audience this identity registers for it", namespace, audience),
			narrowingRecovery)
	}
	// Both sources of a lifetime silent at once leaves nothing to expire, and
	// expiry() then returns the zero time — which reaches a module as an epoch
	// expiry, reading as access that died in 1970. Refuse instead of handing
	// over a grant whose lifetime nobody stated. The response's own expires_in
	// is only RECOMMENDED by RFC 6749 section 5.1, so this is reachable; when
	// it is present it wins, exactly as expiry() has it.
	if r.ExpiresIn <= 0 && facts.ExpiresAt.IsZero() {
		return bearerFacts{}, denial("auth.narrowing_unavailable",
			fmt.Sprintf("the deployment stated no lifetime for the access it issued for the %q "+
				"module, and the token claims none either", namespace),
			narrowingRecovery)
	}
	return facts, nil
}

// sameScopeSet reports whether two permission lists carry the same members,
// whatever their order or repetition.
func sameScopeSet(issued, requested []string) bool {
	for _, scope := range issued {
		if !slices.Contains(requested, scope) {
			return false
		}
	}
	for _, scope := range requested {
		if !slices.Contains(issued, scope) {
			return false
		}
	}
	return true
}

// scopeList renders permissions for a refusal in a stable order.
func scopeList(scopes []string) string {
	sorted := slices.Sorted(slices.Values(scopes))
	if len(sorted) == 0 {
		return "none"
	}
	return strings.Join(sorted, ", ")
}
