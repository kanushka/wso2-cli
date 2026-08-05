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
	"strings"
	"testing"
	"time"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/modules"
)

const (
	// writeScope is the second permission the login session holds. It is what
	// makes narrowing observable: a session granted two permissions and a
	// module declaring one proves the grant carries exactly the one asked for.
	writeScope = "reference:status:write"
	// sessionRef is the secure-store entry the browser identity's session
	// lives under.
	sessionRef = "reference-cloud"
)

// browserDeployment is one seeded browser identity: a running issuer, a state
// root for the rotation lock, and the session the login step would have left.
type browserDeployment struct {
	issuer    *fakeissuer.Issuer
	stateRoot string
	// seeded is the refresh token the stored session starts with.
	seeded string
}

// seedBrowserSession starts an issuer, mocks the OS secure store, and stores a
// session granted both product permissions — the state a completed wso2 login
// leaves behind.
func seedBrowserSession(t *testing.T, options fakeissuer.Options) browserDeployment {
	t.Helper()
	keyring.MockInit()
	if options.Audience == "" {
		options.Audience = audience
	}
	issuer := fakeissuer.New(t, options)
	root := t.TempDir()
	seeded := issuer.SeedSession([]string{readScope, writeScope})
	store := session.Store{StateRoot: root}
	if err := store.Save(sessionRef, session.Session{Issuer: issuer.URL, RefreshToken: seeded}); err != nil {
		t.Fatalf("seeding the stored session: %v", err)
	}
	return browserDeployment{issuer: issuer, stateRoot: root, seeded: seeded}
}

// browserBroker builds the broker one invocation would build for a browser
// identity whose products are fully registered.
func (d browserDeployment) broker() *auth.Broker {
	return &auth.Broker{
		Namespace:    "reference",
		Capabilities: modules.Capabilities{AuthAudiences: []string{audience}, AuthScopes: []string{readScope}},
		Selection: contexts.Selection{
			Context: contexts.Context{
				Name:         "reference-cloud",
				Identity:     "reference-cloud",
				Organization: homeTenant,
			},
			Identity: contexts.Identity{
				Name: "reference-cloud",
				Type: "cloud",
				Auth: contexts.IdentityAuth{
					Kind:          contexts.KindOAuthBrowser,
					Issuer:        d.issuer.URL,
					ClientID:      "wso2cli",
					Tenant:        homeTenant,
					CredentialRef: sessionRef,
				},
				Products: map[string]contexts.Product{
					"reference": {
						Endpoint: "https://reference.example.test",
						Audience: audience,
						Scopes:   []string{readScope, writeScope},
					},
				},
			},
		},
		InvocationID: invocationID,
		StateRoot:    d.stateRoot,
		HTTPClient:   d.issuer.HTTPClient(),
	}
}

// storedSession reads the session the secure store holds now.
func (d browserDeployment) storedSession(t *testing.T) session.Session {
	t.Helper()
	stored, err := session.Store{StateRoot: d.stateRoot}.Load(sessionRef)
	if err != nil {
		t.Fatalf("loading the stored session: %v", err)
	}
	return stored
}

func TestBrowserSourceNarrowsTheSessionToWhatTheModuleAsked(t *testing.T) {
	// The session holds two permissions and the module declares one. What
	// reaches the module must be the one it asked for: the issuer minted it,
	// it is bound to the product's audience, and it carries nothing more.
	deployment := seedBrowserSession(t, fakeissuer.Options{RefreshScopeMode: "honor"})

	grant, err := deployment.broker().Acquire(declaredRequest())
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
	if grant.ExpiresAt.IsZero() || !grant.ExpiresAt.After(time.Now()) {
		t.Errorf("the grant expires at %s, want a near-term future expiry", grant.ExpiresAt)
	}
}

func TestBrowserSourceStatesTheEffectiveScopesWhenTheIssuerDoesNot(t *testing.T) {
	// An issuer that answers a refresh without naming the effective scopes is
	// still provably narrowed: the access token itself carries the claim.
	deployment := seedBrowserSession(t, fakeissuer.Options{
		RefreshScopeMode: "honor", OmitRefreshScopeField: true,
	})

	grant, err := deployment.broker().Acquire(declaredRequest())
	if err != nil {
		t.Fatalf("Acquire returned %v", err)
	}

	_, scopes, _ := deployment.issuer.Introspect(t, grant.Token)
	if len(scopes) != 1 || scopes[0] != readScope {
		t.Errorf("token scopes = %v, want exactly [%s]", scopes, readScope)
	}
}

func TestBrowserSourcePersistsTheRotatedRefreshTokenBeforeGranting(t *testing.T) {
	// A rotating issuer invalidates the token it was presented. If the shell
	// granted access before storing the replacement, one crash would strand
	// the session; the next invocation proves the replacement was stored.
	deployment := seedBrowserSession(t, fakeissuer.Options{
		RefreshScopeMode: "honor", RotateRefreshTokens: true,
	})

	if _, err := deployment.broker().Acquire(declaredRequest()); err != nil {
		t.Fatalf("the first Acquire returned %v", err)
	}

	rotated := deployment.storedSession(t)
	if rotated.RefreshToken == deployment.seeded {
		t.Fatal("the rotated refresh token was not persisted")
	}
	if rotated.Issuer != deployment.issuer.URL {
		t.Errorf("the stored session issuer = %q, want %q", rotated.Issuer, deployment.issuer.URL)
	}
	// A second invocation is a second broker: nothing carries over but what
	// the secure store holds.
	grant, err := deployment.broker().Acquire(declaredRequest())
	if err != nil {
		t.Fatalf("the second Acquire returned %v", err)
	}
	if active, _, _ := deployment.issuer.Introspect(t, grant.Token); !active {
		t.Fatal("the second invocation was granted a token the issuer did not mint")
	}
}

func TestBrowserSourceRefusesOnceTheRotatedTokenSupersedesTheStoredOne(t *testing.T) {
	// The token the issuer replaced is dead. A session still holding it is a
	// session to log in again for, not one to keep retrying.
	deployment := seedBrowserSession(t, fakeissuer.Options{
		RefreshScopeMode: "honor", RotateRefreshTokens: true,
	})
	if _, err := deployment.broker().Acquire(declaredRequest()); err != nil {
		t.Fatalf("the first Acquire returned %v", err)
	}
	store := session.Store{StateRoot: deployment.stateRoot}
	if err := store.Save(sessionRef, session.Session{
		Issuer: deployment.issuer.URL, RefreshToken: deployment.seeded,
	}); err != nil {
		t.Fatalf("restoring the superseded session: %v", err)
	}

	refusal := denied(t, deployment.broker(), declaredRequest())

	if refusal.Problem.Code != "auth.login_required" {
		t.Errorf("code = %q, want auth.login_required", refusal.Problem.Code)
	}
}

func TestBrowserSourceRefusesRatherThanAcceptABroaderGrant(t *testing.T) {
	// Narrowing that is ignored, refused, or answered against the wrong
	// audience all end the same way: no grant. A module that cannot be given
	// exactly what it asked for is given nothing.
	for name, testcase := range map[string]fakeissuer.Options{
		"the issuer ignores the narrower request": {RefreshScopeMode: "ignore"},
		"the issuer refuses to narrow":            {RefreshScopeMode: "reject"},
		"the issuer states no effective scopes": {
			RefreshScopeMode: "ignore", OmitRefreshScopeField: true,
		},
		"the deployment registers another audience": {
			RefreshScopeMode: "honor", Audience: "other-api",
		},
	} {
		t.Run(name, func(t *testing.T) {
			deployment := seedBrowserSession(t, testcase)

			refusal := denied(t, deployment.broker(), declaredRequest())

			if refusal.Problem.Code != "auth.narrowing_unavailable" {
				t.Errorf("code = %q, want auth.narrowing_unavailable", refusal.Problem.Code)
			}
		})
	}
}

func TestBrowserSourceRefusesWhatNoSessionCanAnswer(t *testing.T) {
	for name, testcase := range map[string]struct {
		prepare func(*testing.T, browserDeployment)
		code    string
	}{
		"no session was ever stored": {
			prepare: func(t *testing.T, d browserDeployment) {
				t.Helper()
				if err := keyring.Delete(session.Service, sessionRef); err != nil {
					t.Fatalf("clearing the stored session: %v", err)
				}
			},
			code: "auth.login_required",
		},
		"the stored session came from another issuer": {
			prepare: func(t *testing.T, d browserDeployment) {
				t.Helper()
				store := session.Store{StateRoot: d.stateRoot}
				if err := store.Save(sessionRef, session.Session{
					Issuer: "https://elsewhere.example.test", RefreshToken: d.seeded,
				}); err != nil {
					t.Fatalf("storing the foreign session: %v", err)
				}
			},
			code: "auth.session_issuer_mismatch",
		},
		"the stored session was revoked": {
			prepare: func(t *testing.T, d browserDeployment) {
				t.Helper()
				store := session.Store{StateRoot: d.stateRoot}
				if err := store.Save(sessionRef, session.Session{
					Issuer: d.issuer.URL, RefreshToken: "rt-nobody-minted",
				}); err != nil {
					t.Fatalf("storing the revoked session: %v", err)
				}
			},
			code: "auth.login_required",
		},
	} {
		t.Run(name, func(t *testing.T) {
			deployment := seedBrowserSession(t, fakeissuer.Options{RefreshScopeMode: "honor"})
			testcase.prepare(t, deployment)

			refusal := denied(t, deployment.broker(), declaredRequest())

			if refusal.Problem.Code != testcase.code {
				t.Errorf("code = %q, want %q", refusal.Problem.Code, testcase.code)
			}
		})
	}
}

func TestALoginRequiredRefusalNamesTheCommandThatFixesIt(t *testing.T) {
	deployment := seedBrowserSession(t, fakeissuer.Options{RefreshScopeMode: "honor"})
	if err := keyring.Delete(session.Service, sessionRef); err != nil {
		t.Fatalf("clearing the stored session: %v", err)
	}

	refusal := denied(t, deployment.broker(), declaredRequest())

	if !strings.Contains(refusal.Reported().Recovery, "wso2 login") {
		t.Errorf("the recovery %q does not name wso2 login", refusal.Reported().Recovery)
	}
}

func TestNoBrowserRefusalCarriesSessionMaterial(t *testing.T) {
	// A refusal is rendered. The refresh token behind it is the one thing in
	// this flow that survives the command, so no refusal may repeat it.
	for name, testcase := range map[string]fakeissuer.Options{
		"narrowing ignored": {RefreshScopeMode: "ignore"},
		"narrowing refused": {RefreshScopeMode: "reject"},
		"wrong audience":    {RefreshScopeMode: "honor", Audience: "other-api"},
	} {
		t.Run(name, func(t *testing.T) {
			deployment := seedBrowserSession(t, testcase)

			refusal := denied(t, deployment.broker(), declaredRequest())

			rendered := refusal.Problem.Message + " " + refusal.Problem.Recovery + " " +
				refusal.Reported().Message + " " + refusal.Reported().Recovery
			if strings.Contains(rendered, deployment.seeded) {
				t.Fatalf("a refusal disclosed the stored refresh token: %s", rendered)
			}
		})
	}
}
