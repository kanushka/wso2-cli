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

package oauthflow_test

import (
	"context"
	"testing"

	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/oauthflow"
)

// A deployment that advertises the endpoint and serves it is told, and the
// refresh token stops renewing. The outcome the shell reports and the state at
// the issuer have to agree; asserting only the outcome would pass against a
// revocation that returned 200 and did nothing.
func TestRevokeConfirmed(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	refreshToken := issuer.SeedSession([]string{"openid"})

	outcome := oauthflow.Revoke{
		Issuer:       issuer.URL,
		ClientID:     "cli",
		RefreshToken: refreshToken,
		HTTPClient:   issuer.HTTPClient(),
	}.Run(context.Background())

	if outcome != oauthflow.RevocationConfirmed {
		t.Fatalf("outcome = %q, want %q", outcome, oauthflow.RevocationConfirmed)
	}
	if issuer.RefreshTokenLive(refreshToken) {
		t.Fatal("the issuer would still renew a session with the revoked refresh token")
	}
}

// A deployment that publishes no revocation endpoint is never asked, and the
// shell says so rather than reporting a failure it did not have.
func TestRevokeNotAttemptedWhenNotAdvertised(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{OmitRevocationEndpoint: true})
	refreshToken := issuer.SeedSession([]string{"openid"})

	outcome := oauthflow.Revoke{
		Issuer:       issuer.URL,
		ClientID:     "cli",
		RefreshToken: refreshToken,
		HTTPClient:   issuer.HTTPClient(),
	}.Run(context.Background())

	if outcome != oauthflow.RevocationNotAttempted {
		t.Fatalf("outcome = %q, want %q", outcome, oauthflow.RevocationNotAttempted)
	}
	if !issuer.RefreshTokenLive(refreshToken) {
		t.Fatal("nothing was revoked, so the refresh token should still renew at the issuer")
	}
}

// A deployment that advertises the endpoint and then declines the request is
// reported as a refusal, distinct from never having been asked. The two differ
// in what the shell may claim, which is the whole point of ADR 0010.
func TestRevokeFailedWhenRefused(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{RefuseRevocation: true})
	refreshToken := issuer.SeedSession([]string{"openid"})

	outcome := oauthflow.Revoke{
		Issuer:       issuer.URL,
		ClientID:     "cli",
		RefreshToken: refreshToken,
		HTTPClient:   issuer.HTTPClient(),
	}.Run(context.Background())

	if outcome != oauthflow.RevocationFailed {
		t.Fatalf("outcome = %q, want %q", outcome, oauthflow.RevocationFailed)
	}
}

// An unreachable issuer is a failure and not a refusal to proceed: logout goes
// on to remove the local session either way, so this must return a value rather
// than an error.
func TestRevokeFailedWhenIssuerUnreachable(t *testing.T) {
	outcome := oauthflow.Revoke{
		// A port nothing listens on, on an address that needs no name service.
		Issuer:       "http://127.0.0.1:1",
		ClientID:     "cli",
		RefreshToken: "rt-whatever",
	}.Run(context.Background())

	if outcome != oauthflow.RevocationFailed {
		t.Fatalf("outcome = %q, want %q", outcome, oauthflow.RevocationFailed)
	}
}

// A cancelled context is a failure, not a hang. Logout bounds the call so a
// slow issuer cannot hold the session lock past its own deadline.
func TestRevokeFailedWhenContextCancelled(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	refreshToken := issuer.SeedSession([]string{"openid"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	outcome := oauthflow.Revoke{
		Issuer:       issuer.URL,
		ClientID:     "cli",
		RefreshToken: refreshToken,
		HTTPClient:   issuer.HTTPClient(),
	}.Run(ctx)

	if outcome != oauthflow.RevocationFailed {
		t.Fatalf("outcome = %q, want %q", outcome, oauthflow.RevocationFailed)
	}
}

// Revocation without a refresh token is not a request worth making. Nothing
// the shell holds identifies the session to the issuer, so it was never asked.
func TestRevokeNotAttemptedWithoutRefreshToken(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})

	outcome := oauthflow.Revoke{
		Issuer:     issuer.URL,
		ClientID:   "cli",
		HTTPClient: issuer.HTTPClient(),
	}.Run(context.Background())

	if outcome != oauthflow.RevocationNotAttempted {
		t.Fatalf("outcome = %q, want %q", outcome, oauthflow.RevocationNotAttempted)
	}
}
