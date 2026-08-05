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

package auth_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
)

const (
	// clientSecretVariable is the environment variable an inline identity
	// names as its client secret source.
	clientSecretVariable = "WSO2_REFERENCE_CLIENT_SECRET"
	// clientSecret is the canary. It is what CI would really hold, and no
	// refusal, no grant, and no surface the shell writes may contain it.
	clientSecret = "canary-client-secret-4b71"
)

// inlineDeployment is one client-credentials identity against a running issuer.
// There is nothing to seed: the whole point of the kind is that the credential
// is already on the machine and no login ever happened.
type inlineDeployment struct {
	issuer    *fakeissuer.Issuer
	stateRoot string
}

func deployInline(t *testing.T, options fakeissuer.Options) inlineDeployment {
	t.Helper()
	// The secure store is mocked so a test can prove the inline path leaves it
	// empty rather than merely believing it does.
	keyring.MockInit()
	if options.Audience == "" {
		options.Audience = audience
	}
	if options.ClientSecret == "" {
		options.ClientSecret = clientSecret
	}
	return inlineDeployment{issuer: fakeissuer.New(t, options), stateRoot: t.TempDir()}
}

// broker builds the broker one CI invocation would build for this deployment,
// with the client secret held where the identity says it is.
func (d inlineDeployment) broker(t *testing.T) *auth.Broker {
	t.Helper()
	return d.brokerHolding(t, clientSecret, true)
}

// brokerHolding is broker, with the environment answering exactly as stated —
// so a test can model a variable that is unset or blank.
func (d inlineDeployment) brokerHolding(t *testing.T, value string, present bool) *auth.Broker {
	t.Helper()
	broker := productionBroker(t, contexts.KindClientCredentials)
	broker.Selection.Identity.Auth.Issuer = d.issuer.URL
	// A non-interactive identity holds no secure-store reference; the document
	// schema refuses one, and the broker must not need one either.
	broker.Selection.Identity.Auth.CredentialRef = ""
	broker.Selection.Identity.Auth.ClientSecretVariable = clientSecretVariable
	broker.StateRoot = d.stateRoot
	broker.HTTPClient = d.issuer.HTTPClient()
	broker.Credentials = func(name string) (string, bool) {
		if name != clientSecretVariable {
			return "", false
		}
		return value, present
	}
	// The broker's clock stays pinned. The deployment states a lifetime rather
	// than an instant, so when the grant expires is the shell's arithmetic and
	// can be asserted exactly.
	return broker
}

func TestAnInlineIdentityAcquiresAccessDuringTheCommandItself(t *testing.T) {
	// CI runs one command and gets one token. The grant is the issuer's own,
	// carries exactly the permission the module asked for, and is bound to the
	// product's audience — the same three facts the browser derivation proves,
	// reached without a login step.
	deployment := deployInline(t, fakeissuer.Options{})

	grant, err := deployment.broker(t).Acquire(declaredRequest())
	if err != nil {
		t.Fatalf("Acquire returned %v", err)
	}

	active, scopes, audiences := deployment.issuer.Introspect(t, grant.Token)
	if !active {
		t.Fatal("the module was granted a token the issuer did not mint")
	}
	if len(scopes) != 1 || scopes[0] != readScope {
		t.Errorf("token scopes = %v, want exactly [%s]", scopes, readScope)
	}
	if len(audiences) != 1 || audiences[0] != audience {
		t.Errorf("token audience = %v, want [%s]", audiences, audience)
	}
	// The deployment states a five-minute lifetime, counted from the moment
	// the shell asked.
	if wanted := acquiredAt.Add(5 * time.Minute); !grant.ExpiresAt.Equal(wanted) {
		t.Errorf("the grant expires at %s, want %s", grant.ExpiresAt, wanted)
	}
}

func TestAnInlineAcquisitionStoresNothingAnywhere(t *testing.T) {
	// Inline means inline. The credential was already on the machine, so a
	// session entry would be a second copy of an authority CI already has, and
	// a lock file would be state nobody asked the shell to keep.
	deployment := deployInline(t, fakeissuer.Options{})

	if _, err := deployment.broker(t).Acquire(declaredRequest()); err != nil {
		t.Fatalf("Acquire returned %v", err)
	}

	for _, ref := range []string{"reference-cloud", clientSecretVariable} {
		if _, err := keyring.Get(session.Service, ref); !errors.Is(err, keyring.ErrNotFound) {
			t.Errorf("the secure store holds an entry under %q after an inline acquisition", ref)
		}
	}
	entries, err := os.ReadDir(deployment.stateRoot)
	if err != nil {
		t.Fatalf("reading the state root: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("an inline acquisition wrote %d entries to the state root", len(entries))
	}
}

func TestAnInlineIdentityWithoutItsClientSecretRefusesAndTellsOnlyTheUserWhere(t *testing.T) {
	// The module is owed a refusal it can return; the user is owed the name of
	// the variable to set. They are not the same statement, and the module
	// must never receive the second one: knowing the variable is knowing where
	// the credential lives.
	for name, testcase := range map[string]struct {
		value   string
		present bool
	}{
		"the variable is unset":  {value: "", present: false},
		"the variable is empty":  {value: "", present: true},
		"the variable is blank":  {value: "   ", present: true},
		"the variable is a tab":  {value: "\t", present: true},
		"the variable is spaces": {value: "  \n ", present: true},
	} {
		t.Run(name, func(t *testing.T) {
			deployment := deployInline(t, fakeissuer.Options{})
			broker := deployment.brokerHolding(t, testcase.value, testcase.present)

			refusal := denied(t, broker, declaredRequest())

			if refusal.Problem.Code != "auth.credential_unavailable" {
				t.Errorf("code = %q, want auth.credential_unavailable", refusal.Problem.Code)
			}
			if !strings.Contains(refusal.Reported().Recovery, clientSecretVariable) {
				t.Errorf("the user is not told which variable to set: %q",
					refusal.Reported().Recovery)
			}
			moduleSafe := refusal.Problem.Message + " " + refusal.Problem.Recovery
			if strings.Contains(moduleSafe, clientSecretVariable) {
				t.Errorf("the module-safe refusal names the credential source: %q", moduleSafe)
			}
		})
	}
}

func TestAnInlineIdentityRefusesADeploymentThatWillNotNarrow(t *testing.T) {
	// The verification is the browser source's, unchanged. A deployment that
	// answers a narrowed request with the client's whole authority, refuses to
	// narrow, or binds the token elsewhere gets no grant at all.
	for name, options := range map[string]fakeissuer.Options{
		"the deployment ignores the narrower request": {
			ClientScopeMode: "ignore",
			ClientScopes:    []string{readScope, writeScope},
		},
		"the deployment refuses to narrow": {ClientScopeMode: "reject"},
		"the deployment states no permissions at all": {
			ClientScopeMode: "ignore",
			ClientScopes:    nil,
		},
		"the deployment registers another audience": {Audience: "other-api"},
	} {
		t.Run(name, func(t *testing.T) {
			deployment := deployInline(t, options)

			refusal := denied(t, deployment.broker(t), declaredRequest())

			if refusal.Problem.Code != "auth.narrowing_unavailable" {
				t.Errorf("code = %q, want auth.narrowing_unavailable", refusal.Problem.Code)
			}
		})
	}
}

func TestAnInlineIdentityWhoseSecretTheDeploymentRejectsIsNotSentToLogIn(t *testing.T) {
	// A rotated or mistyped secret is the likeliest CI failure there is. It is
	// reported as a credential the deployment would not take — never as a
	// login to run, because wso2 login refuses this kind of identity outright
	// and would leave the user with nothing to do.
	deployment := deployInline(t, fakeissuer.Options{})
	broker := deployment.brokerHolding(t, "a-secret-the-deployment-does-not-know", true)

	refusal := denied(t, broker, declaredRequest())

	if refusal.Problem.Code != "auth.credential_unavailable" {
		t.Errorf("code = %q, want auth.credential_unavailable", refusal.Problem.Code)
	}
	if !strings.Contains(refusal.Reported().Recovery, clientSecretVariable) {
		t.Errorf("the user is not told which variable to correct: %q", refusal.Reported().Recovery)
	}
}

func TestNoInlineRefusalOrGrantRevealsTheClientSecret(t *testing.T) {
	// The sweep covers every way an inline acquisition can end: the grant it
	// hands the module, and each refusal it can raise once the secret has been
	// read. None of them may carry the value.
	deployment := deployInline(t, fakeissuer.Options{})
	grant, err := deployment.broker(t).Acquire(declaredRequest())
	if err != nil {
		t.Fatalf("Acquire returned %v", err)
	}
	if strings.Contains(grant.Token, clientSecret) {
		t.Error("the token handed to the module carries the client secret")
	}

	for name, options := range map[string]fakeissuer.Options{
		"the deployment refuses to narrow":          {ClientScopeMode: "reject"},
		"the deployment registers another audience": {Audience: "other-api"},
	} {
		t.Run(name, func(t *testing.T) {
			refusing := deployInline(t, options)

			refusal := denied(t, refusing.broker(t), declaredRequest())

			rendered := refusal.Problem.Message + " " + refusal.Problem.Recovery + " " +
				refusal.Reported().Message + " " + refusal.Reported().Recovery + " " +
				refusal.Error()
			if strings.Contains(rendered, clientSecret) {
				t.Errorf("a refusal revealed the client secret: %s", rendered)
			}
		})
	}

	rejected := deployInline(t, fakeissuer.Options{})
	refusal := denied(t, rejected.brokerHolding(t, clientSecret+"-stale", true), declaredRequest())
	rendered := refusal.Problem.Message + " " + refusal.Problem.Recovery + " " +
		refusal.Reported().Message + " " + refusal.Reported().Recovery + " " + refusal.Error()
	if strings.Contains(rendered, clientSecret) {
		t.Errorf("a rejected-credential refusal revealed the client secret: %s", rendered)
	}
}
