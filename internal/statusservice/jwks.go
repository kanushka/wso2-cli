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

package statusservice

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	oidc "github.com/coreos/go-oidc/v3/oidc"
)

// jwksVerifier accepts an access token an OpenID issuer signed, checked against
// the keys that issuer publishes.
//
// It is deliberately not the check the shell makes. internal/auth/claims.go
// reads a token's claims without verifying them, and says why it may: that
// token arrived over the issuer's own connection in answer to the shell's own
// request. A service receiving a bearer token from a caller it knows nothing
// about has no such standing, so nothing the token says about itself is trusted
// until the signature is.
type jwksVerifier struct {
	tokens *oidc.IDTokenVerifier
}

// newJWKSVerifier reads the issuer's OpenID configuration and builds a verifier
// against the keys it publishes.
//
// Discovery happens here rather than per request, so a service that cannot read
// its issuer's configuration refuses to start. That is New's existing rule: a
// service that cannot say what it accepts would accept anything.
func newJWKSVerifier(issuer string, client *http.Client, now func() time.Time) (jwksVerifier, error) {
	ctx := context.Background()
	if client != nil {
		ctx = oidc.ClientContext(ctx, client)
	}
	// NewProvider reads the document and refuses one whose own issuer member
	// disagrees with the URL it came from, so a redirected host cannot
	// substitute its own keys.
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return jwksVerifier{}, fmt.Errorf(
			"statusservice: cannot read the issuer's OpenID configuration: %w", err)
	}
	return jwksVerifier{tokens: provider.VerifierContext(ctx, &oidc.Config{
		// The audience check belongs to authorize, not here. An Asgardeo token
		// names the client identifier alone and an Identity Server 7.3.0 token
		// names the client identifier and the API resource, so membership is
		// the only check both deployments can satisfy — and this library's own
		// check is equality against a single configured value.
		SkipClientIDCheck: true,
		// One algorithm, stated rather than negotiated.
		//
		// This is not what refuses "none" or an HMAC forgery. go-oidc filters
		// an issuer's advertised algorithms through its own allowlist of
		// asymmetric ones before any caller configuration is consulted, so a
		// symmetric algorithm never reaches this verifier however loudly the
		// issuer advertises it.
		//
		// What this line does is the remaining narrowing, from the ten
		// algorithms that allowlist permits to the one this service accepts.
		// Without it the set is whatever the issuer advertises, so an issuer
		// offering ES256 would have its ES256 tokens accepted here — the
		// issuer, rather than this service, choosing how its own tokens are
		// checked.
		SupportedSigningAlgs: []string{oidc.RS256},
		// The service's clock, so a test that moves it moves expiry with it.
		Now: now,
	})}, nil
}

// verify proves the issuer minted this token and reads what it asserts.
//
// Expiry is not checked here: the clock this verifier was built with was handed
// to the library, which applies it as part of verification.
func (v jwksVerifier) verify(presented string) (access, error) {
	token, err := v.tokens.Verify(context.Background(), presented)
	var expired *oidc.TokenExpiredError
	switch {
	case errors.As(err, &expired):
		// A token stating no lifetime at all also arrives here, because an
		// absent exp reads as the zero time and every moment is after it.
		// Refusing it as expired rather than as malformed is the right answer
		// to the only question that matters: it is not access this service
		// will act on.
		return access{}, errAccessExpired
	case err != nil:
		return access{}, errAccessRejected
	}

	var claims struct {
		// Scope is the space-delimited permission list RFC 9068 section 2.2.3
		// describes, which is what both measured deployments emit.
		Scope string `json:"scope"`
		// Organization is the claim Asgardeo mints for a sub-organization and
		// the API Portal verifies on every request.
		Organization string `json:"org_id"`
	}
	if err := token.Claims(&claims); err != nil {
		return access{}, errAccessRejected
	}
	scopes := strings.Fields(claims.Scope)
	// A token whose permissions cannot be read is refused rather than read as
	// carrying none. Both would fail the coverage check today, but a service
	// that treats "I cannot tell" as "it has nothing" is one broken claim away
	// from treating it as "it has everything".
	if len(scopes) == 0 {
		return access{}, errAccessRejected
	}
	return access{
		Audiences:    token.Audience,
		Scopes:       scopes,
		Organization: claims.Organization,
	}, nil
}
