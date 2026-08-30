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
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
	// A session against a deployment that decides the audience at authorization
	// time could only have been established by naming a resource, so the seeded
	// one names the product's. Seeding without it would model a session no such
	// deployment can issue.
	seeded := issuer.SeedSession([]string{readScope, writeScope})
	if options.RequireResource {
		seeded = issuer.SeedSessionFor([]string{readScope, writeScope}, audience)
	}
	store := session.Store{StateRoot: root}
	if err := store.Save(sessionRef, session.Session{Issuer: issuer.URL, RefreshToken: seeded}); err != nil {
		t.Fatalf("seeding the stored session: %v", err)
	}
	return browserDeployment{issuer: issuer, stateRoot: root, seeded: seeded}
}

// broker builds the broker one invocation would build for this deployment.
//
// It starts from the shared production identity and points it at the running
// issuer, so the only things that differ from every other production-policy
// test are the ones a browser derivation actually needs: where the issuer is,
// which secure-store entry holds the session, and the state root its rotation
// lock lives under.
func (d browserDeployment) broker(t *testing.T) *auth.Broker {
	t.Helper()
	broker := productionBroker(t, contexts.KindOAuthBrowser)
	broker.Selection.Identity.Auth.Issuer = d.issuer.URL
	broker.Selection.Identity.Auth.CredentialRef = sessionRef
	withProduct(broker, contexts.Product{
		Endpoint: "https://reference.example.test",
		Audience: audience,
		Scopes:   []string{readScope, writeScope},
	})
	broker.StateRoot = d.stateRoot
	broker.HTTPClient = d.issuer.HTTPClient()
	// The issuer states a token's life relative to now, so this derivation is
	// clocked by the real time the running issuer is minting against, not by
	// the fixed instant the fixture-token tests pin.
	broker.Now = nil
	return broker
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

func TestSessionSourceNarrowsTheSessionToWhatTheModuleAsked(t *testing.T) {
	// The session holds two permissions and the module declares one. What
	// reaches the module must be the one it asked for: the issuer minted it,
	// it is bound to the product's audience, and it carries nothing more.
	deployment := seedBrowserSession(t, fakeissuer.Options{RefreshScopeMode: "honor"})

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
	if grant.ExpiresAt.IsZero() || !grant.ExpiresAt.After(time.Now()) {
		t.Errorf("the grant expires at %s, want a near-term future expiry", grant.ExpiresAt)
	}
}

func TestSessionSourceStatesTheEffectiveScopesWhenTheIssuerDoesNot(t *testing.T) {
	// An issuer that answers a refresh without naming the effective scopes is
	// still provably narrowed: the access token itself carries the claim.
	deployment := seedBrowserSession(t, fakeissuer.Options{
		RefreshScopeMode: "honor", OmitRefreshScopeField: true,
	})

	grant, err := deployment.broker(t).Acquire(declaredRequest())
	if err != nil {
		t.Fatalf("Acquire returned %v", err)
	}

	_, scopes, _ := deployment.issuer.Introspect(t, grant.Token)
	if len(scopes) != 1 || scopes[0] != readScope {
		t.Errorf("token scopes = %v, want exactly [%s]", scopes, readScope)
	}
}

func TestSessionSourcePersistsTheRotatedRefreshTokenBeforeGranting(t *testing.T) {
	// A rotating issuer invalidates the token it was presented. If the shell
	// granted access before storing the replacement, one crash would strand
	// the session; the next invocation proves the replacement was stored.
	deployment := seedBrowserSession(t, fakeissuer.Options{
		RefreshScopeMode: "honor", RotateRefreshTokens: true,
	})

	if _, err := deployment.broker(t).Acquire(declaredRequest()); err != nil {
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
	grant, err := deployment.broker(t).Acquire(declaredRequest())
	if err != nil {
		t.Fatalf("the second Acquire returned %v", err)
	}
	if active, _, _ := deployment.issuer.Introspect(t, grant.Token); !active {
		t.Fatal("the second invocation was granted a token the issuer did not mint")
	}
}

// TestSessionSourceRotationRecordsTheDisclosedRefreshTokenExpiry pins R7
// (#112): a rotation that discloses the rotated refresh token's own lifetime
// records it as SessionExpiresAt, inside the same save that already persists
// the rotated token — no second write, no widened save condition.
//
// Mutation-proved: reverting the `stored.SessionExpiresAt = now.Add(...)`
// assignment in source_session.go's derive (leaving only the reset to the
// zero value above it) makes this test fail, because the recorded value would
// stay zero instead of falling inside [before+lifetime, after+lifetime].
func TestSessionSourceRotationRecordsTheDisclosedRefreshTokenExpiry(t *testing.T) {
	const disclosedLifetimeSeconds = 3600
	deployment := seedBrowserSession(t, fakeissuer.Options{
		RefreshScopeMode: "honor", RotateRefreshTokens: true,
		RefreshTokenExpiresIn: disclosedLifetimeSeconds,
	})

	before := time.Now()
	if _, err := deployment.broker(t).Acquire(declaredRequest()); err != nil {
		t.Fatalf("Acquire returned %v", err)
	}
	after := time.Now()

	rotated := deployment.storedSession(t)
	if rotated.SessionExpiresAt.IsZero() {
		t.Fatal("the rotation did not record the disclosed refresh-token expiry")
	}
	earliest := before.Add(disclosedLifetimeSeconds * time.Second)
	latest := after.Add(disclosedLifetimeSeconds * time.Second)
	if rotated.SessionExpiresAt.Before(earliest) || rotated.SessionExpiresAt.After(latest) {
		t.Errorf("SessionExpiresAt = %v, want between %v and %v", rotated.SessionExpiresAt, earliest, latest)
	}
}

// TestSessionSourceRotationWithoutDisclosureLeavesExpiryAtTheZeroValue proves
// the expected case per R7: an issuer that rotates without disclosing a new
// refresh-token lifetime leaves SessionExpiresAt at the zero value, rather
// than inventing one or carrying forward whatever an earlier login or
// rotation happened to record about the refresh token this rotation just
// replaced.
func TestSessionSourceRotationWithoutDisclosureLeavesExpiryAtTheZeroValue(t *testing.T) {
	deployment := seedBrowserSession(t, fakeissuer.Options{
		RefreshScopeMode: "honor", RotateRefreshTokens: true,
	})
	// A stale expiry from an earlier login, seeded deliberately: if the
	// rotation path merely left SessionExpiresAt untouched instead of
	// resetting it, this stale value would still be here afterwards and the
	// test below would pass for the wrong reason.
	if err := (session.Store{StateRoot: deployment.stateRoot}).Save(sessionRef, session.Session{
		Issuer: deployment.issuer.URL, RefreshToken: deployment.seeded,
		SessionExpiresAt: time.Now().Add(999 * time.Hour),
	}); err != nil {
		t.Fatalf("seeding a stale expiry: %v", err)
	}

	if _, err := deployment.broker(t).Acquire(declaredRequest()); err != nil {
		t.Fatalf("Acquire returned %v", err)
	}

	rotated := deployment.storedSession(t)
	if !rotated.SessionExpiresAt.IsZero() {
		t.Errorf("SessionExpiresAt = %v, want the zero value: this rotation disclosed no lifetime",
			rotated.SessionExpiresAt)
	}
}

func TestSessionSourceRefusesOnceTheRotatedTokenSupersedesTheStoredOne(t *testing.T) {
	// The token the issuer replaced is dead. A session still holding it is a
	// session to log in again for, not one to keep retrying.
	deployment := seedBrowserSession(t, fakeissuer.Options{
		RefreshScopeMode: "honor", RotateRefreshTokens: true,
	})
	if _, err := deployment.broker(t).Acquire(declaredRequest()); err != nil {
		t.Fatalf("the first Acquire returned %v", err)
	}
	store := session.Store{StateRoot: deployment.stateRoot}
	if err := store.Save(sessionRef, session.Session{
		Issuer: deployment.issuer.URL, RefreshToken: deployment.seeded,
	}); err != nil {
		t.Fatalf("restoring the superseded session: %v", err)
	}

	refusal := denied(t, deployment.broker(t), declaredRequest())

	if refusal.Problem.Code != "auth.login_required" {
		t.Errorf("code = %q, want auth.login_required", refusal.Problem.Code)
	}
}

func TestSessionSourceRefusesRatherThanAcceptABroaderGrant(t *testing.T) {
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

			refusal := denied(t, deployment.broker(t), declaredRequest())

			if refusal.Problem.Code != "auth.narrowing_unavailable" {
				t.Errorf("code = %q, want auth.narrowing_unavailable", refusal.Problem.Code)
			}
		})
	}
}

func TestSessionSourceRefusesWhatNoSessionCanAnswer(t *testing.T) {
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

			refusal := denied(t, deployment.broker(t), declaredRequest())

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

	refusal := denied(t, deployment.broker(t), declaredRequest())

	if !strings.Contains(refusal.Reported().Recovery, "wso2 login") {
		t.Errorf("the recovery %q does not name wso2 login", refusal.Reported().Recovery)
	}
}

func TestNoRefusalCarriesSessionMaterial(t *testing.T) {
	// A refusal is rendered. The refresh token behind it is the one thing in
	// this flow that survives the command, so no refusal may repeat it.
	for name, testcase := range map[string]fakeissuer.Options{
		"narrowing ignored": {RefreshScopeMode: "ignore"},
		"narrowing refused": {RefreshScopeMode: "reject"},
		"wrong audience":    {RefreshScopeMode: "honor", Audience: "other-api"},
	} {
		t.Run(name, func(t *testing.T) {
			deployment := seedBrowserSession(t, testcase)

			refusal := denied(t, deployment.broker(t), declaredRequest())

			rendered := refusal.Problem.Message + " " + refusal.Problem.Recovery + " " +
				refusal.Reported().Message + " " + refusal.Reported().Recovery
			if strings.Contains(rendered, deployment.seeded) {
				t.Fatalf("a refusal disclosed the stored refresh token: %s", rendered)
			}
		})
	}
}

// stubTokenEndpoint is an issuer that answers every renewal with one canned
// status and body.
//
// The fake issuer models deployments that behave; this models one that does
// not, which is the only way to reach the paths where the shell must tell a
// broken endpoint apart from a deliberate refusal.
func stubTokenEndpoint(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration",
		func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(writer).Encode(map[string]any{
				"issuer":                                server.URL,
				"authorization_endpoint":                server.URL + "/authorize",
				"token_endpoint":                        server.URL + "/token",
				"jwks_uri":                              server.URL + "/jwks",
				"id_token_signing_alg_values_supported": []string{"RS256"},
			}); err != nil {
				t.Errorf("stub issuer could not answer discovery: %v", err)
			}
		})
	mux.HandleFunc("POST /token", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		if _, err := io.WriteString(writer, body); err != nil {
			t.Errorf("stub issuer could not answer the token request: %v", err)
		}
	})
	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestOnlyABadRequestReadsAsARefusalToNarrow(t *testing.T) {
	// invalid_scope means "I will not scope this down" on the status RFC 6749
	// defines it for. On any other status the endpoint is failing, and calling
	// that a registration problem would send a user to change something that
	// was never wrong.
	for name, testcase := range map[string]struct {
		status int
		code   string
	}{
		"the deployment refuses to narrow":    {http.StatusBadRequest, "auth.narrowing_unavailable"},
		"the deployment is failing":           {http.StatusInternalServerError, "auth.login_required"},
		"the session is no longer authorized": {http.StatusUnauthorized, "auth.login_required"},
	} {
		t.Run(name, func(t *testing.T) {
			keyring.MockInit()
			stub := stubTokenEndpoint(t, testcase.status, `{"error":"invalid_scope"}`)
			root := t.TempDir()
			if err := (session.Store{StateRoot: root}).Save(sessionRef, session.Session{
				Issuer: stub.URL, RefreshToken: "rt-stored",
			}); err != nil {
				t.Fatalf("seeding the stored session: %v", err)
			}
			broker := productionBroker(t, contexts.KindOAuthBrowser)
			broker.Selection.Identity.Auth.Issuer = stub.URL
			broker.Selection.Identity.Auth.CredentialRef = sessionRef
			broker.StateRoot = root
			broker.HTTPClient = stub.Client()
			broker.Now = nil

			refusal := denied(t, broker, declaredRequest())

			if refusal.Problem.Code != testcase.code {
				t.Errorf("code = %q, want %q", refusal.Problem.Code, testcase.code)
			}
		})
	}
}

// TestAccessIsGrantedWhenTheDeploymentBindsTheRegisteredAudience models
// Asgardeo, where an access token's aud is the client ID and never the API
// resource the module names.
//
// The module asks by the logical name its API is known by, compiled in and the
// same against every deployment. The identity registers the concrete string
// this deployment stamps into aud. The two differ here, which is the case the
// broker used to refuse outright as auth.product_not_configured — making the
// reference module installable only where its constant happened to match a
// deployment value. What has to hold instead is that the grant succeeds and the
// token really is bound to what the identity registered.
func TestAccessIsGrantedWhenTheDeploymentBindsTheRegisteredAudience(t *testing.T) {
	const clientIDAudience = "M0Hkzofj2ZoTEKuEJEPS75EfW8ga"

	deployment := seedBrowserSession(t, fakeissuer.Options{
		Audience:         clientIDAudience,
		RefreshScopeMode: "honor",
	})
	broker := deployment.broker(t)
	withProduct(broker, contexts.Product{
		Endpoint: "https://reference.example.test",
		Audience: clientIDAudience,
		Scopes:   []string{readScope, writeScope},
	})

	// declaredRequest asks for "reference-status": the module's own name for the
	// API, which this deployment will never put in a token.
	grant, err := broker.Acquire(declaredRequest())
	if err != nil {
		t.Fatalf("Acquire returned %v, want a grant: a module's logical audience differing from "+
			"the registered one is not a misconfiguration", err)
	}

	active, scopes, audiences := deployment.issuer.Introspect(t, grant.Token)
	if !active {
		t.Fatal("the module was granted a token the issuer did not mint")
	}
	if len(audiences) != 1 || audiences[0] != clientIDAudience {
		t.Errorf("token audience = %v, want [%s]: the binding proved must be the registered one",
			audiences, clientIDAudience)
	}
	if len(scopes) != 1 || scopes[0] != readScope {
		t.Errorf("token scopes = %v, want exactly [%s]", scopes, readScope)
	}
}

// A token bound to something other than what the identity registered is still
// refused. This is the check that carries the weight now that the module's
// logical name is not compared against the registration.
func TestAccessIsRefusedWhenTheDeploymentBindsAnotherAudience(t *testing.T) {
	deployment := seedBrowserSession(t, fakeissuer.Options{
		Audience:         "some-other-api",
		RefreshScopeMode: "honor",
	})
	broker := deployment.broker(t)
	withProduct(broker, contexts.Product{
		Endpoint: "https://reference.example.test",
		Audience: "M0Hkzofj2ZoTEKuEJEPS75EfW8ga",
		Scopes:   []string{readScope, writeScope},
	})

	refusal := denied(t, broker, declaredRequest())

	if refusal.Problem.Code != "auth.narrowing_unavailable" {
		t.Fatalf("code = %q, want auth.narrowing_unavailable", refusal.Problem.Code)
	}
}
