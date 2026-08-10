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
	"strconv"
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
// into the operating system's secure store, and two brokered acquisitions on
// top of the session that login established.
//
// The second acquisition is the one that measures anything about narrowing.
// Asking for every permission the session carries — which is all this test used
// to do — leaves the broker comparing the issued scopes against an identical
// request, so the check holds no matter what came back and a deployment that
// disregarded the request entirely would still be reported as granted. Asking
// for a strict subset is the request a module actually makes, and it is the
// only form of it that can fail.
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
	// One broker per acquisition, because the shell allows a module one
	// acquisition per command and refuses a second with auth.already_granted.
	// Two brokers is therefore not a way around that rule but the accurate
	// model of what happens: two commands, run in turn against one session, the
	// way a developer uses the shell.
	brokerFor := func(invocation string) *auth.Broker {
		return &auth.Broker{
			Namespace:    smoke.Namespace,
			Capabilities: config.Capabilities(),
			Selection:    selection,
			InvocationID: invocation,
			StateRoot:    stateRoot,
		}
	}

	// Two acquisitions, because they fail for different reasons and only one of
	// them can catch a deployment that disregards a narrowing request.
	//
	// The first asks for every permission the session already carries. It
	// proves the broker can derive access at all, which is what a first run
	// against a new deployment needs to know. What it cannot prove is
	// narrowing: internal/auth/narrowing.go compares the issued scopes against
	// the requested ones, and when the request is the whole session that
	// comparison is a set against itself. It holds however the deployment
	// behaved.
	if !acquire(t, brokerFor("smoke-login-broad"),
		auth.Request{Audience: config.Audience, Scopes: config.Scopes},
		"granted", "everything the session carries") {
		// The broad request already refused, and the narrow one would refuse
		// the same way for the same reason — a deployment that will not issue
		// against this audience at all says nothing about narrowing. Running it
		// would add a second copy of one finding.
		return
	}

	// The second asks for one permission out of the several the session holds,
	// which is the request an actual module makes. Here the verification has
	// something it can disagree with: a deployment that answered with the full
	// set is caught, and a granted line means the shell watched a real session
	// be narrowed rather than assuming it.
	//
	// Against a deployment that rotates refresh tokens, this second run also
	// proves the first one persisted its replacement — it can only reach the
	// token endpoint at all with what the first run stored.
	target, err := config.NarrowTarget()
	if err != nil {
		t.Logf("LOGIN SMOKE: narrowing not measured — %v", err)
		return
	}
	acquire(t, brokerFor("smoke-login-narrowed"),
		auth.Request{Audience: config.Audience, Scopes: []string{target}},
		"narrowed", "one permission out of the "+strconv.Itoa(len(config.Scopes))+" the session holds")
}

// acquire runs one brokered acquisition and reports it the way a human reading
// a smoke run needs, returning whether access was granted.
//
// asked describes the request in prose, because the interesting part of a
// verdict line is not the scope list but why that list was chosen.
func acquire(t *testing.T, broker *auth.Broker, request auth.Request, verdict, asked string) bool {
	t.Helper()

	grant, err := broker.Acquire(request)
	switch {
	case err == nil:
		t.Logf("LOGIN SMOKE: %s — asked for %s, received access of %d characters bound to %q "+
			"carrying %v, expiring %s",
			verdict, asked, len(grant.Token), request.Audience, request.Scopes,
			grant.ExpiresAt.Format("15:04:05Z07:00"))
		return true
	case refusalCode(err) == codeNarrowingUnavailable:
		// Documented, correct behavior. See this test's own doc comment and
		// docs/guides/login.md's troubleshooting section.
		//
		// auth.narrowing_unavailable covers five distinct causes — see
		// internal/auth/narrowing.go's verify() and the table under
		// auth.narrowing_unavailable in docs/guides/login.md section 6 — so this
		// summary must not name one of them (a "narrowed grant" specifically).
		// The interpolated error text below is what actually says which of the
		// five happened; this sentence only states what is true regardless: the
		// shell declined to hand the module more authority than it asked for.
		t.Logf("LOGIN SMOKE: refused %s — asked for %s, and the shell would not hand the module a "+
			"grant it could not prove was exactly what it asked for. Login and session persistence "+
			"passed; this refusal is the designed outcome, not a failure.\n  %v",
			codeNarrowingUnavailable, asked, err)
		return false
	default:
		t.Fatalf("the broker refused for a reason this slice does not accept: %v", err)
		return false
	}
}
