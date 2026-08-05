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
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/auth/session"
	fixture "github.com/wso2/wso2-cli/internal/contexts/fixture"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/test/smoke"
)

// TestLoginSmoke drives the whole slice against a deployment that really
// exists: `wso2 login` through a browser a human answers, the refresh token
// into the operating system's secure store, and one brokered acquisition on
// top of the session that login established.
//
// It is written to pass against both a hosted Asgardeo tenant and a local
// Identity Server 7.x from the same code, because the shell makes no
// distinction between them and a test that did would stop proving that.
//
// One outcome deserves its own sentence. If the deployment will not narrow a
// session to what a module asked for, the acquisition step refuses with
// auth.narrowing_unavailable, and this test reports that and passes. The
// refusal is the designed behavior — the shell does not hand a module more
// authority than it requested — so a run that produces it has demonstrated the
// slice working, not failing. Every other refusal is a real failure.
func TestLoginSmoke(t *testing.T) {
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

	// The session is read back through the shell's own store, which reads the
	// operating system's secure store and nothing else. A refresh token here is
	// the persistence claim, proven where it is actually kept.
	stored, err := session.Store{StateRoot: stateRoot}.Load(smoke.CredentialRef)
	if err != nil {
		t.Fatalf("the login did not leave a readable session: %v", err)
	}
	if stored.RefreshToken == "" {
		t.Fatal("the stored session carries no refresh token")
	}
	if stored.Issuer != config.Issuer {
		t.Fatalf("the stored session names issuer %q, want %q", stored.Issuer, config.Issuer)
	}
	t.Logf("session stored: refresh token of %d characters, access token expiring %s",
		len(stored.RefreshToken), stored.ExpiresAt.Format("15:04:05Z07:00"))

	selection, err := config.Document().Select("")
	if err != nil {
		t.Fatalf("the smoke document selects no context: %v", err)
	}
	broker := &auth.Broker{
		Namespace:    smoke.Namespace,
		Capabilities: config.Capabilities(),
		Selection:    selection,
		InvocationID: "smoke-login",
		StateRoot:    stateRoot,
	}

	grant, err := broker.Acquire(auth.Request{Audience: config.Audience, Scopes: config.Scopes})
	switch {
	case err == nil:
		t.Logf("LOGIN SMOKE: granted — access of %d characters bound to %q, expiring %s",
			len(grant.Token), config.Audience, grant.ExpiresAt.Format("15:04:05Z07:00"))
	case refusalCode(err) == codeNarrowingUnavailable:
		// Documented, correct behavior. See this test's own doc comment and
		// docs/guides/login.md's troubleshooting section.
		//
		// auth.narrowing_unavailable covers five distinct causes — see
		// internal/auth/narrowing.go's verify() and the table under
		// auth.narrowing_unavailable in docs/guides/login.md section 8 — so this
		// summary must not name one of them (a "narrowed grant" specifically).
		// The interpolated error text below is what actually says which of the
		// five happened; this sentence only states what is true regardless: the
		// shell declined to hand the module more authority than it asked for.
		t.Logf("LOGIN SMOKE: refused %s — the shell would not hand the module a grant it could not "+
			"prove was exactly what it asked for. Login and session persistence passed; this refusal "+
			"is the designed outcome, not a failure.\n  %v", codeNarrowingUnavailable, err)
	default:
		t.Fatalf("the broker refused for a reason this slice does not accept: %v", err)
	}
}
