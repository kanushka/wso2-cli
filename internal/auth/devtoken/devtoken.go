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

// Package devtoken is the architecture proof's development token issuer.
//
// It exists to prove one boundary: a module can be given access material that
// is narrower than the credential it was derived from. It is not an
// authentication contract, and nothing about this format is public. A
// production shell replaces this package with a real token exchange, and the
// only thing that survives the replacement is the shape of the broker seam:
// mint a short-lived token bound to an audience, scopes, an organization, and
// one invocation.
//
// It is deliberately non-production and says so on the wire: every token it
// mints begins with Prefix. It is an internal package, so no product code can
// reach it.
//
// The token is a claims document and a keyed digest of that document. The key
// is the shell-owned source credential, which the shell and the audience both
// hold and a module never sees. That is enough to prove the boundary and is not
// a key-management design.
package devtoken

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Prefix opens every fixture token, so a token that escapes into a log or a
// bug report is recognizable as development material rather than mistaken for
// a production credential.
const Prefix = "wso2-development-token."

// Lifetime is how long a fixture token is accepted. It is near-term by design:
// the proof shows a module holding access it cannot renew and cannot hoard.
const Lifetime = 2 * time.Minute

// Errors a caller distinguishes. A verifier reports why a token was refused
// without saying anything about the credential behind it.
var (
	// ErrMalformed reports a token that is not in the fixture format.
	ErrMalformed = errors.New("devtoken: the token is malformed")
	// ErrSignature reports a token this issuer did not mint, or one whose
	// claims were edited after it was minted.
	ErrSignature = errors.New("devtoken: the token signature does not match its claims")
	// ErrExpired reports a token that is past its expiry.
	ErrExpired = errors.New("devtoken: the token has expired")
)

// Claims are everything a fixture token asserts.
//
// A token carries no more authority than these values: an audience decides
// whether they match what it serves, and refuses the token when they do not.
type Claims struct {
	// Audience is the single protected audience the token is for.
	Audience string `json:"audience"`
	// Scopes are the permissions the token conveys.
	Scopes []string `json:"scopes"`
	// Organization is the organization the token acts within.
	Organization string `json:"organization"`
	// Invocation is the shell invocation the token was minted for. It stops a
	// token from being replayed under a later invocation.
	Invocation string `json:"invocation"`
	// IssuedAt and ExpiresAt bound the token's life. Mint sets both.
	IssuedAt  time.Time `json:"-"`
	ExpiresAt time.Time `json:"-"`
}

// wireClaims is the encoded form. Times travel as Unix seconds so the document
// has one unambiguous representation to sign.
type wireClaims struct {
	Audience     string   `json:"audience"`
	Scopes       []string `json:"scopes"`
	Organization string   `json:"organization"`
	Invocation   string   `json:"invocation"`
	IssuedAt     int64    `json:"issuedAt"`
	ExpiresAt    int64    `json:"expiresAt"`
}

// Mint issues a fixture token for the given claims, valid for Lifetime from
// issuedAt.
//
// The source credential is the signing key and is never part of the result, so
// what the shell hands a module cannot be turned back into what the shell
// holds.
func Mint(sourceCredential string, claims Claims, issuedAt time.Time) (string, error) {
	if sourceCredential == "" {
		return "", errors.New("devtoken: a source credential is required to mint a token")
	}
	if claims.Audience == "" {
		return "", errors.New("devtoken: a token must name an audience")
	}
	if claims.Invocation == "" {
		return "", errors.New("devtoken: a token must name an invocation")
	}

	issuedAt = issuedAt.UTC()
	document, err := json.Marshal(wireClaims{
		Audience:     claims.Audience,
		Scopes:       claims.Scopes,
		Organization: claims.Organization,
		Invocation:   claims.Invocation,
		IssuedAt:     issuedAt.Unix(),
		ExpiresAt:    issuedAt.Add(Lifetime).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("devtoken: cannot encode the token claims: %w", err)
	}

	encoded := encoding.EncodeToString(document)
	return Prefix + encoded + "." + encoding.EncodeToString(sign(sourceCredential, encoded)), nil
}

// Verify proves a token was minted by this issuer from the given source
// credential and is still within its life, and returns what it claims.
//
// It answers for the token itself only. Whether those claims are the ones an
// audience serves is the audience's decision, not the format's.
func Verify(sourceCredential, token string, now time.Time) (Claims, error) {
	if sourceCredential == "" {
		return Claims{}, errors.New("devtoken: a source credential is required to verify a token")
	}
	body, found := strings.CutPrefix(token, Prefix)
	if !found {
		return Claims{}, fmt.Errorf("%w: it does not begin with %q", ErrMalformed, Prefix)
	}
	encoded, signature, found := strings.Cut(body, ".")
	if !found || encoded == "" || signature == "" || strings.Contains(signature, ".") {
		return Claims{}, fmt.Errorf("%w: it is not a claims document and a signature", ErrMalformed)
	}

	// The signature is checked before the claims are decoded, so nothing an
	// unauthenticated document says can influence what happens next.
	presented, err := encoding.DecodeString(signature)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: its signature is not base64url", ErrMalformed)
	}
	if !hmac.Equal(presented, sign(sourceCredential, encoded)) {
		return Claims{}, ErrSignature
	}

	document, err := encoding.DecodeString(encoded)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: its claims are not base64url", ErrMalformed)
	}
	var decoded wireClaims
	if err := json.Unmarshal(document, &decoded); err != nil {
		return Claims{}, fmt.Errorf("%w: its claims are not a JSON document", ErrMalformed)
	}

	claims := Claims{
		Audience:     decoded.Audience,
		Scopes:       decoded.Scopes,
		Organization: decoded.Organization,
		Invocation:   decoded.Invocation,
		IssuedAt:     time.Unix(decoded.IssuedAt, 0).UTC(),
		ExpiresAt:    time.Unix(decoded.ExpiresAt, 0).UTC(),
	}
	if !now.Before(claims.ExpiresAt) {
		return Claims{}, ErrExpired
	}
	return claims, nil
}

// Allows reports whether the token's scopes include the given scope.
func (c Claims) Allows(scope string) bool {
	for _, granted := range c.Scopes {
		if granted == scope {
			return true
		}
	}
	return false
}

// encoding is unpadded base64url, so a token is one URL- and header-safe word.
var encoding = base64.RawURLEncoding

// sign is the keyed digest binding a claims document to the source credential.
func sign(sourceCredential, encodedClaims string) []byte {
	mac := hmac.New(sha256.New, []byte(sourceCredential))
	mac.Write([]byte(encodedClaims))
	return mac.Sum(nil)
}
