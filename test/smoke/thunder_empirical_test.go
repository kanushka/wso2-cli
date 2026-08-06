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
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/test/smoke"
)

// TestThunderEmpirical answers the questions that decided how the shell derives
// access on a deployment which binds tokens to a named resource.
//
// They are the same two questions sections 3 and 3.1 of the research document
// ask of Asgardeo and Identity Server — does the refresh grant honour a
// narrower scope, and what lands in aud — plus the one those products never
// raise, which is whether the audience can be chosen at all. Keeping the first
// two makes Thunder's column comparable with the other two rather than a
// separate story.
//
// Each experiment prints one greppable verdict line for a human to copy into
// section 3.2. The test passes whatever the deployment answers: a refusal is a
// finding, and a run that failed on one would be reporting the deployment's
// behaviour as the shell's defect.
//
// These run against a deployment the Thunder walkthrough describes. Against
// Asgardeo or Identity Server the resource experiments are meaningless — those
// products take no resource indicator — so the run refuses to start unless the
// deployment says it is a Thunder one.
func TestThunderEmpirical(t *testing.T) {
	config := requireDeployment(t)
	if !smoke.Empirical(os.LookupEnv) {
		t.Skipf("set %s=1 to run the one-time empirical experiments "+
			"(see test/smoke/RUNNING.md)", smoke.EmpiricalVar)
	}
	if config.Provider != contexts.ProviderThunder {
		t.Skipf("these experiments describe a deployment that binds access to a named resource; "+
			"set %s=%s to run them", smoke.ProviderVar, contexts.ProviderThunder)
	}

	t.Setenv("WSO2_CONTEXT", "")
	t.Setenv("WSO2_NON_INTERACTIVE", "")

	t.Run("indicator-required", func(t *testing.T) { experimentIndicatorRequired(t, config) })
	t.Run("resource-narrowing", func(t *testing.T) { experimentResourceNarrowing(t, config) })
}

// experimentIndicatorRequired asks whether the deployment will establish a
// session at all without being told which protected resource it is for.
//
// This is the question that decided the derivation. If the answer is that it
// will, the shell's existing scoped refresh would have served Thunder unchanged
// and this slice would have been documentation. It will not, unless a default
// resource server has been configured — which is why the verdict distinguishes
// the two rather than reporting a bare yes or no.
func experimentIndicatorRequired(t *testing.T, config smoke.Config) {
	t.Logf("signing in for %v WITHOUT a resource indicator", config.Scopes)
	ctx, cancel := context.WithTimeout(context.Background(), config.Deadline)
	defer cancel()

	result, err := oauthflow.Login{
		Issuer:   config.Issuer,
		ClientID: config.ClientID,
		Scopes:   config.Scopes,
		Out:      os.Stderr,
		// Deliberately no Resource. This is the experiment.
	}.Run(ctx)

	var verdict string
	switch {
	case err != nil:
		verdict = "required (the deployment refused a login carrying no indicator)"
		t.Logf("the login ended with %s: %s", refusalCode(err), refusalMessage(err))
	case result.Token.RefreshToken == "":
		verdict = "inconclusive (the login completed but issued no refresh token)"
	default:
		verdict = "optional (a default resource server is configured; every token it " +
			"issues carries that default's audience whichever product asked)"
	}
	reportThunder(t, "THUNDER RESOURCE INDICATOR AT AUTHORIZATION", verdict, config)
}

// experimentResourceNarrowing asks the two questions the other products are
// asked, on a session established the way this product requires.
//
// It signs in for every configured permission, naming the product's resource,
// stores the session exactly as `wso2 login` would, and then asks the broker
// for one permission out of the set. The broker's own verification decides the
// verdict: it proves the issued token carries exactly the permissions asked for
// and is bound to the audience asked for, and refuses when it cannot. So a
// grant here is a statement about both narrowing and audience binding at once.
func experimentResourceNarrowing(t *testing.T, config smoke.Config) {
	target, err := config.NarrowTarget()
	if err != nil {
		t.Skipf("%v", err)
	}

	stateRoot := filepath.Join(t.TempDir(), "state")
	forgetSmokeSession(t)

	t.Logf("signing in for %v against %q, then asking for %q alone",
		config.Scopes, config.Audience, target)
	ctx, cancel := context.WithTimeout(context.Background(), config.Deadline)
	defer cancel()

	result, err := oauthflow.Login{
		Issuer:   config.Issuer,
		ClientID: config.ClientID,
		Scopes:   config.Scopes,
		Resource: config.Audience,
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

	_, err = broker.Acquire(auth.Request{Audience: config.Audience, Scopes: []string{target}})
	verdict := smoke.NarrowingVerdict(refusalCode(err), refusalMessage(err), []string{target})
	if err != nil {
		t.Logf("the broker reported %s: %s", refusalCode(err), refusalMessage(err))
	}
	reportThunder(t, "THUNDER REFRESH NARROWING AND AUDIENCE BINDING", verdict, config)
}

// reportThunder prints one verdict line, naming the section it belongs in.
//
// It is separate from the Asgardeo experiments' reporter for one reason: the
// section a verdict is recorded in is part of the verdict. A Thunder finding
// pasted into the Asgardeo table would be indistinguishable from a measurement
// of the wrong deployment.
func reportThunder(t *testing.T, question, verdict string, config smoke.Config) {
	t.Helper()
	_, _ = fmt.Fprintf(os.Stdout, "\n%s: %s\n  deployment: %s\n  recorded in: %s\n\n",
		question, verdict, config.Issuer,
		"docs/research/asgardeo-redirect-uri-and-scope-narrowing.md section 3.2")
	t.Logf("%s: %s", question, verdict)
}
