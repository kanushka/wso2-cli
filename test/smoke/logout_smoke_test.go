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

//go:build smoke

// This file runs only under `go test -tags smoke`. It opens a real browser,
// signs a real person in, and writes to the operating system's secure store, so
// it is kept out of the default gate by the tag rather than by a skip.

package smoke_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	oidc "github.com/coreos/go-oidc/v3/oidc"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/auth/session"
	fixture "github.com/wso2/wso2-cli/internal/contexts/fixture"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/test/smoke"
)

// TestLogoutSmoke ends a real session against a deployment that really exists,
// and measures what that achieved at the issuer.
//
// It exists because the design of wso2 logout was settled without any of these
// facts. Nothing in docs/research/ records whether Asgardeo, Identity Server or
// ThunderID advertises a revocation endpoint, whether one accepts a public
// client at it, or whether revoking a refresh token there actually stops the
// session renewing. ADR 0010 chose a shape that survives not knowing — each
// outcome discovered at runtime and reported for what it is — and said the
// first live run against each deployment should be recorded. This is that run.
//
// **Every outcome below is a pass.** A deployment that advertises no revocation
// endpoint, or refuses this shell at it, is not a broken deployment and not a
// broken shell: it is one of the three outcomes the command is built to report,
// and the point of this test is to find out which one a real product produces.
// The failures here are the shell failing to do its own half — leaving the
// session in the secure store, or reporting an outcome that contradicts what
// the issuer then does.
//
// The verdict lines go to standard output so they survive without -v and can be
// pasted into the research record.
func TestLogoutSmoke(t *testing.T) {
	config := requireDeployment(t)

	// A developer's own environment must not decide what this run proves.
	t.Setenv("WSO2_CONTEXT", "")
	t.Setenv("WSO2_NON_INTERACTIVE", "")

	stateRoot := filepath.Join(t.TempDir(), "state")
	if err := fixture.WriteV2(stateRoot, config.Document()); err != nil {
		t.Fatalf("cannot install the smoke context document: %v", err)
	}
	forgetSmokeSession(t)

	captured := &bytes.Buffer{}
	shell := app.Shell{
		StateRoot: stateRoot,
		// Both streams reach the terminal as well as the buffer: a human is
		// waiting at a browser and needs the authorization URL now, not in the
		// test's final report.
		Streams: output.Streams{
			Out: io.MultiWriter(os.Stdout, captured),
			Err: io.MultiWriter(os.Stderr, captured),
		},
	}

	t.Logf("signing in against %s as client %s", config.Issuer, config.ClientID)
	if code := shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("wso2 login exited %d\n%s", code, captured)
	}

	store := session.Store{StateRoot: stateRoot}
	stored, err := store.Load(smoke.CredentialRef)
	if err != nil {
		t.Fatalf("the login did not leave a readable session: %v", err)
	}
	if stored.RefreshToken == "" {
		t.Fatal("the stored session carries no refresh token, so there is nothing to revoke")
	}
	// Held because logout is about to remove the only copy the shell keeps, and
	// the whole measurement below is what the issuer does with it afterwards.
	refreshToken := stored.RefreshToken
	t.Logf("session stored: refresh token of %d characters", len(refreshToken))

	// Asked before logout, because logout removes the session either way and a
	// deployment answering this question afterwards would be answering it about
	// a session that no longer exists.
	advertised := advertisesRevocation(t, config.Issuer)
	t.Logf("the deployment advertises a revocation endpoint: %t", advertised)

	// The renewal is proven to work before it is proven to stop. Without this,
	// a refresh token that never renewed on this deployment would read as a
	// successful revocation and the run would report a guarantee it never had.
	if !renews(t, config, refreshToken) {
		t.Fatal("the refresh token does not renew before logout, so this run can prove " +
			"nothing about what revoking it achieved")
	}
	t.Log("the refresh token renews before logout, as it must for the measurement below to mean anything")

	logoutOutput := &bytes.Buffer{}
	shell.Streams = output.Streams{Out: logoutOutput, Err: io.MultiWriter(os.Stderr, captured)}
	if code := shell.Run([]string{"logout", "--output", "json"}); code != exit.OK {
		t.Fatalf("wso2 logout exited %d\nstdout:\n%s\nstderr:\n%s", code, logoutOutput, captured)
	}

	var reported struct {
		Session    string `json:"session"`
		Revocation string `json:"revocation"`
	}
	if err := json.Unmarshal(logoutOutput.Bytes(), &reported); err != nil {
		t.Fatalf("wso2 logout did not render a JSON document: %v\n%s", err, logoutOutput)
	}
	if strings.Contains(logoutOutput.String(), refreshToken) {
		t.Error("the refresh token reached standard output")
	}

	// The shell's own half of the job, and the only part of this run that can
	// fail for a reason worth fixing here.
	if reported.Session != "ended" {
		t.Errorf("logout reported session %q, want ended", reported.Session)
	}
	if _, err := store.Load(smoke.CredentialRef); err == nil {
		t.Error("the session is still in the operating system's secure store after logout")
	}

	// What the deployment did, cross-checked against what the shell claimed.
	revocationVerdict := smoke.VerdictRevocationUnadvertised
	switch reported.Revocation {
	case "confirmed":
		revocationVerdict = smoke.VerdictRevocationAccepted
		if !advertised {
			t.Error("logout reported a confirmed revocation against a deployment whose " +
				"discovery document advertises no revocation endpoint")
		}
	case "failed":
		revocationVerdict = smoke.VerdictRevocationRefused
	case "not-attempted":
		if advertised {
			t.Error("logout did not attempt revocation although the deployment advertises " +
				"a revocation endpoint")
		}
	default:
		t.Fatalf("logout reported an unrecognized revocation outcome %q", reported.Revocation)
	}

	// The independent check, and the reason this test is worth a human's time.
	// An accepted revocation proves the deployment was told; only presenting the
	// token afterwards shows whether being told changed anything.
	refreshVerdict := smoke.VerdictRefreshDead
	if renews(t, config, refreshToken) {
		refreshVerdict = smoke.VerdictRefreshAlive
	}
	if reported.Revocation == "confirmed" && refreshVerdict == smoke.VerdictRefreshAlive {
		t.Log("NOTE: the deployment accepted the revocation and the refresh token still " +
			"renews. That is a finding about the deployment, not about this shell, and it " +
			"belongs in the research record and plausibly upstream.")
	}

	reportRevocation(t, "Does the deployment let this public client revoke a session?",
		revocationVerdict, config)
	reportRevocation(t, "Did revoking the refresh token end the session?", refreshVerdict, config)
}

// reportRevocation prints one verdict line.
//
// It goes to standard output rather than through t.Logf so that the line
// survives without -v and can be piped straight into a grep, which is how the
// make target and RUNNING.md tell a human to collect it.
func reportRevocation(t *testing.T, question, verdict string, config smoke.Config) {
	t.Helper()
	_, _ = fmt.Fprintf(os.Stdout, "\n%s: %s\n  deployment: %s\n  recorded in: %s\n\n",
		question, verdict, config.Issuer,
		"docs/research/wso2-authentication-landscape.md, per product")
	t.Logf("%s: %s", question, verdict)
}

// advertisesRevocation reports whether the issuer's OpenID configuration names
// a revocation endpoint, read the same way the shell reads it.
func advertisesRevocation(t *testing.T, issuer string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		t.Fatalf("the deployment's OpenID configuration cannot be read: %v", err)
	}
	var document struct {
		RevocationEndpoint string `json:"revocation_endpoint"`
	}
	if err := provider.Claims(&document); err != nil {
		t.Fatalf("the OpenID configuration cannot be decoded: %v", err)
	}
	return document.RevocationEndpoint != ""
}

// renews reports whether the deployment still exchanges this refresh token for
// access.
//
// It asks the token endpoint directly rather than going through the broker,
// because the broker rotates and re-stores what it receives, and a measurement
// that wrote to the secure store would change the thing it is measuring. A
// deployment that rotates refresh tokens hands back a replacement here that this
// test deliberately drops on the floor: the question is whether the presented
// token was still honored, not what came back.
func renews(t *testing.T, config smoke.Config, refreshToken string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	provider, err := oidc.NewProvider(ctx, config.Issuer)
	if err != nil {
		t.Fatalf("the deployment's OpenID configuration cannot be read: %v", err)
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {config.ClientID},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		provider.Endpoint().TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("the renewal request cannot be built: %v", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("the deployment could not be reached to test the refresh token: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	// The body is never printed. On success it carries a live access token, and
	// on failure a deployment may quote the request that produced it.
	switch {
	case response.StatusCode/100 == 2:
		return true
	case response.StatusCode == http.StatusBadRequest || response.StatusCode == http.StatusUnauthorized:
		return false
	default:
		t.Fatalf("the token endpoint answered %d, which this test cannot read as either a "+
			"renewal or a refusal: %s", response.StatusCode, smoke.VerdictRefreshInconclusive)
		return false
	}
}
