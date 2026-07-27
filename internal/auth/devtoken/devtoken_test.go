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

package devtoken_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wso2/wso2-cli/internal/auth/devtoken"
)

// sourceCredential is the shell-owned development credential the fixture issuer
// signs with. It never leaves the shell.
const sourceCredential = "canary-source-credential-2f8c"

var issuedAt = time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC)

func claims() devtoken.Claims {
	return devtoken.Claims{
		Audience:     "reference-status",
		Scopes:       []string{"reference:status:read"},
		Organization: "reference-org",
		Invocation:   "invocation-7f2a",
	}
}

func TestAMintedTokenCarriesEveryBoundClaim(t *testing.T) {
	token, err := devtoken.Mint(sourceCredential, claims(), issuedAt)
	if err != nil {
		t.Fatalf("Mint returned %v", err)
	}

	verified, err := devtoken.Verify(sourceCredential, token, issuedAt.Add(time.Second))
	if err != nil {
		t.Fatalf("Verify returned %v", err)
	}
	if verified.Audience != "reference-status" {
		t.Errorf("audience = %q, want %q", verified.Audience, "reference-status")
	}
	if !reflect.DeepEqual(verified.Scopes, []string{"reference:status:read"}) {
		t.Errorf("scopes = %v, want [reference:status:read]", verified.Scopes)
	}
	if verified.Organization != "reference-org" {
		t.Errorf("organization = %q, want %q", verified.Organization, "reference-org")
	}
	if verified.Invocation != "invocation-7f2a" {
		t.Errorf("invocation = %q, want %q", verified.Invocation, "invocation-7f2a")
	}
}

func TestAMintedTokenExpiresInTheNearTerm(t *testing.T) {
	token, err := devtoken.Mint(sourceCredential, claims(), issuedAt)
	if err != nil {
		t.Fatalf("Mint returned %v", err)
	}

	verified, err := devtoken.Verify(sourceCredential, token, issuedAt)
	if err != nil {
		t.Fatalf("Verify returned %v", err)
	}
	lifetime := verified.ExpiresAt.Sub(verified.IssuedAt)
	if lifetime <= 0 || lifetime > 5*time.Minute {
		t.Errorf("token lifetime = %s, want a positive near-term lifetime of at most 5m", lifetime)
	}
	if !verified.IssuedAt.Equal(issuedAt) {
		t.Errorf("issued at = %s, want %s", verified.IssuedAt, issuedAt)
	}
}

func TestAnExpiredTokenIsRefused(t *testing.T) {
	token, err := devtoken.Mint(sourceCredential, claims(), issuedAt)
	if err != nil {
		t.Fatalf("Mint returned %v", err)
	}

	_, err = devtoken.Verify(sourceCredential, token, issuedAt.Add(devtoken.Lifetime+time.Second))

	if !errors.Is(err, devtoken.ErrExpired) {
		t.Fatalf("Verify returned %v, want ErrExpired", err)
	}
}

func TestATokenSignedByAnotherCredentialIsRefused(t *testing.T) {
	token, err := devtoken.Mint("another-source-credential", claims(), issuedAt)
	if err != nil {
		t.Fatalf("Mint returned %v", err)
	}

	_, err = devtoken.Verify(sourceCredential, token, issuedAt)

	if !errors.Is(err, devtoken.ErrSignature) {
		t.Fatalf("Verify returned %v, want ErrSignature", err)
	}
}

func TestEditedClaimsAreRefused(t *testing.T) {
	// The claims are the whole of the token's authority, so a holder must not
	// be able to widen them by editing what it was given.
	token, err := devtoken.Mint(sourceCredential, claims(), issuedAt)
	if err != nil {
		t.Fatalf("Mint returned %v", err)
	}
	edited, err := devtoken.Mint(sourceCredential, devtoken.Claims{
		Audience:     "another-audience",
		Scopes:       []string{"reference:status:write"},
		Organization: "another-org",
		Invocation:   "another-invocation",
	}, issuedAt)
	if err != nil {
		t.Fatalf("Mint returned %v", err)
	}
	// The edited claims are pasted in front of the original signature.
	forged := devtoken.Prefix + claimsSegment(t, edited) + "." + signatureSegment(t, token)

	_, err = devtoken.Verify(sourceCredential, forged, issuedAt)

	if !errors.Is(err, devtoken.ErrSignature) {
		t.Fatalf("Verify returned %v, want ErrSignature", err)
	}
}

func TestAMalformedTokenIsRefused(t *testing.T) {
	valid, err := devtoken.Mint(sourceCredential, claims(), issuedAt)
	if err != nil {
		t.Fatalf("Mint returned %v", err)
	}

	for name, token := range map[string]string{
		"empty":                "",
		"no segments":          "not-a-token",
		"missing prefix":       strings.TrimPrefix(valid, devtoken.Prefix),
		"foreign prefix":       "bearer." + strings.TrimPrefix(valid, devtoken.Prefix),
		"one segment":          devtoken.Prefix + claimsSegment(t, valid),
		"extra segment":        valid + ".extra",
		"unreadable signature": devtoken.Prefix + claimsSegment(t, valid) + ".not+base64url",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := devtoken.Verify(sourceCredential, token, issuedAt); !errors.Is(err, devtoken.ErrMalformed) {
				t.Fatalf("Verify returned %v, want ErrMalformed", err)
			}
		})
	}
}

func TestATokenNeverCarriesTheSourceCredential(t *testing.T) {
	// The module receives the token and nothing else, so the credential the
	// shell derived it from must not be recoverable from it.
	token, err := devtoken.Mint(sourceCredential, claims(), issuedAt)
	if err != nil {
		t.Fatalf("Mint returned %v", err)
	}

	if strings.Contains(token, sourceCredential) {
		t.Fatalf("the fixture token carries the source credential: %s", token)
	}
}

func TestATokenIsVisiblyNonProduction(t *testing.T) {
	token, err := devtoken.Mint(sourceCredential, claims(), issuedAt)
	if err != nil {
		t.Fatalf("Mint returned %v", err)
	}

	if !strings.HasPrefix(token, devtoken.Prefix) {
		t.Errorf("the fixture token %q does not begin with the non-production prefix %q", token, devtoken.Prefix)
	}
	if !strings.Contains(devtoken.Prefix, "development") {
		t.Errorf("the token prefix %q does not name itself a development fixture", devtoken.Prefix)
	}
}

func TestAnEmptySourceCredentialCannotMintAToken(t *testing.T) {
	if _, err := devtoken.Mint("", claims(), issuedAt); err == nil {
		t.Fatal("Mint accepted an empty source credential")
	}
}

// claimsSegment returns the encoded claims of a minted token.
func claimsSegment(t *testing.T, token string) string {
	t.Helper()
	return splitToken(t, token)[0]
}

// signatureSegment returns the encoded signature of a minted token.
func signatureSegment(t *testing.T, token string) string {
	t.Helper()
	return splitToken(t, token)[1]
}

func splitToken(t *testing.T, token string) []string {
	t.Helper()
	segments := strings.Split(strings.TrimPrefix(token, devtoken.Prefix), ".")
	if len(segments) != 2 {
		t.Fatalf("a minted token has %d segments, want 2: %s", len(segments), token)
	}
	return segments
}
