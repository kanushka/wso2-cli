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

package smoke_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/auth/oauthflow"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/test/smoke"
)

// TestAsgardeoEmpirical answers the two questions the redirect-and-narrowing
// research could not settle from public sources.
//
// Both are one-time experiments. Their output is the deliverable — each prints
// a single greppable verdict line that a human copies into
// docs/research/asgardeo-redirect-uri-and-scope-narrowing.md section 3, in
// place of the cell that currently says the answer is unknown. The test itself
// passes whatever the deployment answers: a rejection is a finding, not a
// defect, and a run that failed on one would be reporting the deployment's
// behavior as the shell's bug.
//
// The verdict lines are prefixed ASGARDEO because that is the deployment whose
// behavior is unknown, but the experiments run unchanged against an Identity
// Server. Each verdict is followed by the issuer it came from, so a verdict
// recorded from the wrong deployment cannot be mistaken for one from Asgardeo.
func TestAsgardeoEmpirical(t *testing.T) {
	config := requireDeployment(t)
	if !smoke.Empirical(os.LookupEnv) {
		t.Skipf("set %s=1 to run the one-time empirical experiments "+
			"(see test/smoke/RUNNING.md)", smoke.EmpiricalVar)
	}

	t.Setenv("WSO2_CONTEXT", "")
	t.Setenv("WSO2_NO_INPUT", "")

	t.Run("any-port-loopback", func(t *testing.T) { experimentAnyPortLoopback(t, config) })
	t.Run("refresh-narrowing", func(t *testing.T) { experimentRefreshNarrowing(t, config) })
}

// experimentAnyPortLoopback asks whether the deployment waives the port when
// matching a loopback redirect URI, as RFC 8252 section 7.3 asks of it and as
// WSO2 Identity Server documents from 6.0.0 onwards.
//
// The application under test registers 10425-10428 and nothing else. This login
// binds a port outside that range and offers the matching redirect URI. An
// issuer at IS parity accepts it; an exact-match issuer refuses, the browser
// never returns to the listener, and the flow ends at its deadline.
//
// Because a refusal is observed as a flow that never completes, the verdict is
// worth corroborating with what the browser actually displayed — an
// exact-match refusal shows a redirect-URI-mismatch error page. RUNNING.md says
// so at the point a human reads the verdict.
func experimentAnyPortLoopback(t *testing.T, config smoke.Config) {
	t.Logf("binding loopback port %d while the application registers only %v",
		config.UnregisteredPort, smoke.RegisteredPorts())
	t.Logf("if the deployment refuses, the browser shows a redirect-URI error and this "+
		"experiment waits out its %s deadline", config.Deadline)

	ctx, cancel := context.WithTimeout(context.Background(), config.Deadline)
	defer cancel()

	_, err := oauthflow.Login{
		Issuer:   config.Issuer,
		ClientID: config.ClientID,
		Scopes:   config.Scopes,
		Ports:    []int{config.UnregisteredPort},
		Out:      os.Stderr,
	}.Run(ctx)

	var verdict string
	switch {
	case err == nil:
		verdict = "supported"
	case refusalCode(err) == codeDiscoveryFailed:
		// The port was busy or the issuer was unreadable. Either way the
		// experiment never reached the question it was asking.
		verdict = "inconclusive (" + codeDiscoveryFailed + ")"
	default:
		verdict = "rejected"
	}
	if err != nil {
		t.Logf("the flow ended with %s: %s", refusalCode(err), refusalMessage(err))
	}
	report(t, "ASGARDEO ANY-PORT LOOPBACK", verdict, config)
}

// experimentRefreshNarrowing asks whether the deployment honors a narrower
// scope on the refresh grant, as RFC 6749 section 6 allows and as the WSO2 IS
// refresh grant handler implements.
//
// It signs in for every configured permission, stores that session exactly as
// `wso2 login` would, and then asks the broker for one permission out of the
// set. The broker's own verification decides the verdict: it proves the issued
// token carries exactly what was asked for and refuses when it cannot, so the
// three outcomes RFC 6749 allows arrive here as a grant or as one of the
// narrowing refusals, each with a distinguishable message.
func experimentRefreshNarrowing(t *testing.T, config smoke.Config) {
	target, err := config.NarrowTarget()
	if err != nil {
		t.Skipf("%v", err)
	}

	stateRoot := filepath.Join(t.TempDir(), "state")
	forgetSmokeSession(t)

	t.Logf("signing in for %v, then asking for %q alone", config.Scopes, target)
	ctx, cancel := context.WithTimeout(context.Background(), config.Deadline)
	defer cancel()

	result, err := oauthflow.Login{
		Issuer:   config.Issuer,
		ClientID: config.ClientID,
		Scopes:   config.Scopes,
		Out:      os.Stderr,
	}.Run(ctx)
	if err != nil {
		t.Fatalf("the experiment could not establish a session to narrow: %v", err)
	}
	if result.Token.RefreshToken == "" {
		t.Fatalf("the deployment issued no refresh token, so there is no session to narrow; " +
			"check that the application is allowed the refresh token grant")
	}

	store := session.Store{StateRoot: stateRoot}
	err = store.WithLock(smoke.CredentialRef, func() error {
		return store.Save(smoke.CredentialRef, session.Session{
			Issuer:       config.Issuer,
			RefreshToken: result.Token.RefreshToken,
			AccessToken:  result.Token.AccessToken,
			ExpiresAt:    result.Token.Expiry.UTC(),
		})
	})
	if err != nil {
		t.Fatalf("the experiment could not store the session it just established: %v", err)
	}

	selection, selectErr := config.Document().Select("")
	if selectErr != nil {
		t.Fatalf("the smoke document selects no context: %v", selectErr)
	}
	broker := &auth.Broker{
		Namespace:    smoke.Namespace,
		Capabilities: config.Capabilities(),
		Selection:    selection,
		InvocationID: "smoke-empirical",
		StateRoot:    stateRoot,
	}

	_, err = broker.Acquire(auth.Request{Audience: smoke.ModuleAudience, Scopes: []string{target}})
	// The classification lives in the untagged half of this package so that it
	// is unit-tested against the shell's real refusal wording. It decides a
	// finding that outlives the run, and it must not be decided by code that
	// only ever executes in front of a live tenant.
	verdict := smoke.NarrowingVerdict(refusalCode(err), refusalMessage(err), []string{target})
	if err != nil {
		t.Logf("the broker reported %s: %s", refusalCode(err), refusalMessage(err))
	}
	report(t, "ASGARDEO REFRESH NARROWING", verdict, config)
}

// report prints one verdict line.
//
// It goes to standard output rather than through t.Logf so that the line
// survives without -v and can be piped straight into a grep, which is how the
// make target and RUNNING.md tell a human to collect it.
func report(t *testing.T, question, verdict string, config smoke.Config) {
	t.Helper()
	_, _ = fmt.Fprintf(os.Stdout, "\n%s: %s\n  deployment: %s\n  recorded in: %s\n\n",
		question, verdict, config.Issuer,
		"docs/research/asgardeo-redirect-uri-and-scope-narrowing.md section 3")
	t.Logf("%s: %s", question, verdict)
}
