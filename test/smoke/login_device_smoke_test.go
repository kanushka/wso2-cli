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

// This file runs only under `go test -tags smoke`. It waits for a real person
// to approve a real login on a second device and writes to the operating
// system's secure store, so it is kept out of the default gate by the tag
// rather than by a skip.

package smoke_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	fixture "github.com/wso2/wso2-cli/internal/contexts/fixture"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/test/smoke"
)

// TestDeviceLoginSmoke drives the device authorization login against a
// deployment that really exists.
//
// It describes the same deployment as the browser run, from the same variables,
// and differs from it in one field of the installed document. That is the claim
// worth making live: no registration value is specific to the device grant, so
// a deployment already set up for `make smoke-login` needs only the grant
// enabled on the same application.
//
// The deterministic suite already proves the flow — the polling, the four
// endings, the narrowing afterwards — against a fake issuer. What this run adds
// is evidence about a *deployment*: that Asgardeo and Identity Server really
// advertise the endpoint the shell looks for, really answer the polling the way
// RFC 8628 describes, and really leave behind a refresh token the broker can
// narrow.
//
// One answer here is not yet known and is the reason to run this at all.
// Whether either product returns an identity token from the device grant is
// unmeasured, and the shell deliberately does not depend on one. This run
// reports which it saw, so the answer can be recorded rather than assumed. See
// issue #42.
func TestDeviceLoginSmoke(t *testing.T) {
	config := requireDeployment(t)
	config.Kind = contexts.KindOAuthDevice

	// A developer's own environment must not decide what this run proves.
	t.Setenv("WSO2_CONTEXT", "")
	t.Setenv("WSO2_NO_INPUT", "")

	stateRoot := filepath.Join(t.TempDir(), "state")
	if err := fixture.WriteV2(stateRoot, config.Document()); err != nil {
		t.Fatalf("cannot install the smoke context document: %v", err)
	}
	forgetSmokeSession(t)

	captured := &bytes.Buffer{}
	shell := app.Shell{
		StateRoot: stateRoot,
		// Both streams reach the terminal as well as the buffer. A human is
		// about to be asked for a code they must type on another device, and
		// they need it now rather than in the test's final report.
		Streams: output.Streams{
			Out: io.MultiWriter(os.Stdout, captured),
			Err: io.MultiWriter(os.Stderr, captured),
		},
	}

	t.Logf("starting a device login against %s as client %s", config.Issuer, config.ClientID)
	t.Log("approve it on any other device — a phone is fine; nothing has to reach this machine")
	if code := shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("wso2 login exited %d\n%s", code, captured)
	}

	stored, err := session.Store{StateRoot: stateRoot}.Load(smoke.CredentialRef)
	if err != nil {
		t.Fatalf("the device login did not leave a readable session: %v", err)
	}
	if stored.RefreshToken == "" {
		t.Fatal("the stored session carries no refresh token, so no session was established")
	}
	if stored.Issuer != config.Issuer {
		t.Fatalf("the stored session names issuer %q, want %q", stored.Issuer, config.Issuer)
	}
	t.Logf("session stored: refresh token of %d characters", len(stored.RefreshToken))

	// The unmeasured behaviour, reported rather than asserted. The shell prints
	// a subject only when it verified an identity token, so the report is the
	// honest witness for whether this deployment's device grant returned one.
	if bytes.Contains(captured.Bytes(), []byte("Subject")) {
		t.Log("DEVICE LOGIN SMOKE: this deployment's device grant returned a verifiable identity token")
	} else {
		t.Log("DEVICE LOGIN SMOKE: this deployment's device grant returned no identity token — " +
			"the session was established anyway, which is the designed behaviour")
	}

	selection, err := config.Document().Select("")
	if err != nil {
		t.Fatalf("the smoke document selects no context: %v", err)
	}
	// The claim the whole slice rests on: after login there is no device path
	// left. This is the same brokered acquisition the browser run makes, over a
	// session no browser established, and it must narrow identically.
	target, err := config.NarrowTarget()
	if err != nil {
		t.Logf("DEVICE LOGIN SMOKE: narrowing not measured — %v", err)
		return
	}
	acquire(t, &auth.Broker{
		Namespace:    smoke.Namespace,
		Capabilities: config.Capabilities(),
		Selection:    selection,
		InvocationID: "smoke-device-narrowed",
		StateRoot:    stateRoot,
	}, auth.Request{Audience: smoke.ModuleAudience, Scopes: []string{target}},
		"narrowed", "one permission out of the several a device-established session holds")
}
