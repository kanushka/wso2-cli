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

package statusservice_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"

	"github.com/wso2/wso2-cli/internal/statusservice"
)

const (
	jwtAudience     = "reference-status"
	jwtScope        = "reference:status:read"
	jwtOrganization = "reference-org"
)

// issuerKeys are the private halves of the keys a test issuer publishes, so a
// test can sign with any algorithm that issuer offers.
type issuerKeys struct {
	rsa *rsa.PrivateKey
	ec  *ecdsa.PrivateKey
}

// serveJWKS starts an issuer that publishes its keys and the minimum discovery
// document go-oidc reads, and returns its URL and those keys. It is separate
// from fakeissuer because these tests mint the tokens themselves.
//
// It offers two signing algorithms, both of which go-oidc is willing to accept
// on an issuer's say-so. That is what lets a test show this service accepting
// only the one it was configured for rather than both.
func serveJWKS(t *testing.T) (issuerURL string, keys issuerKeys) {
	t.Helper()
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating a signing key: %v", err)
	}
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating an elliptic-curve signing key: %v", err)
	}
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	mux.HandleFunc("GET /.well-known/openid-configuration",
		func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, map[string]any{
				"issuer":                                server.URL,
				"authorization_endpoint":                server.URL + "/authorize",
				"token_endpoint":                        server.URL + "/token",
				"jwks_uri":                              server.URL + "/jwks",
				"id_token_signing_alg_values_supported": []string{"RS256", "ES256"},
			})
		})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(t, w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key: rsaKey.Public(), KeyID: "test-key", Algorithm: string(jose.RS256), Use: "sig",
		}, {
			Key: ecKey.Public(), KeyID: "ec-key", Algorithm: string(jose.ES256), Use: "sig",
		}}})
	})
	return server.URL, issuerKeys{rsa: rsaKey, ec: ecKey}
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, body any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Errorf("encoding a test response: %v", err)
	}
}

// mintJWT signs whatever claims a test asks for, including claim sets a correct
// issuer would never produce.
func mintJWT(t *testing.T, keys issuerKeys, algorithm jose.SignatureAlgorithm, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshalling claims: %v", err)
	}
	// Each algorithm names the published key that can check it, so a token is
	// refused on the algorithm alone and not because its key was unfindable.
	// The symmetric case is the exception and names no published key, because
	// no issuer here publishes a shared secret to find.
	var signingKey any = keys.rsa
	keyID := "test-key"
	switch algorithm {
	case jose.ES256:
		signingKey, keyID = keys.ec, "ec-key"
	case jose.HS256:
		signingKey = []byte("a-symmetric-key-the-service-must-not-accept")
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: algorithm, Key: signingKey},
		(&jose.SignerOptions{}).WithHeader("kid", keyID).WithType("JWT"))
	if err != nil {
		t.Fatalf("building a signer: %v", err)
	}
	signature, err := signer.Sign(payload)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	serialized, err := signature.CompactSerialize()
	if err != nil {
		t.Fatalf("serializing: %v", err)
	}
	return serialized
}

// validClaims are the claims a correct issuer produces, which each test then
// breaks in exactly one way. It names no organization, because that is what
// Asgardeo mints outside a sub-organization setup and therefore what the
// ordinary case looks like.
func validClaims(issuerURL string) map[string]any {
	now := time.Now()
	return map[string]any{
		"iss":   issuerURL,
		"sub":   "client-1",
		"aud":   []string{"some-client-id", jwtAudience},
		"scope": jwtScope,
		"exp":   now.Add(5 * time.Minute).Unix(),
		"iat":   now.Unix(),
	}
}

// callWith presents a token to a service and returns the HTTP status.
func callWith(t *testing.T, service *statusservice.Service, token string) int {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, statusservice.StatusPath, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set(statusservice.InvocationHeader, "invocation-1")
	recorder := httptest.NewRecorder()
	service.ServeHTTP(recorder, request)
	return recorder.Code
}

func newJWKSService(t *testing.T, issuerURL string) *statusservice.Service {
	t.Helper()
	service, err := statusservice.New(statusservice.Options{
		Audience:      jwtAudience,
		RequiredScope: jwtScope,
		Organization:  jwtOrganization,
		Issuer:        issuerURL,
	})
	if err != nil {
		t.Fatalf("statusservice.New returned %v", err)
	}
	return service
}

func TestAnIssuerMintedTokenIsAcceptedOnItsMerits(t *testing.T) {
	// The audience half of the boundary: a token this service never saw
	// minted, verified against keys the issuer publishes, accepted because
	// everything it asserts is what this service serves. The aud claim carries
	// a client identifier alongside the audience, which is the shape Identity
	// Server 7.3.0 produces; membership, not equality, is what admits it.
	issuerURL, keys := serveJWKS(t)
	service := newJWKSService(t, issuerURL)

	status := callWith(t, service, mintJWT(t, keys, jose.RS256, validClaims(issuerURL)))

	if status != http.StatusOK {
		t.Fatalf("a valid issuer-minted token was answered %d, want %d", status, http.StatusOK)
	}
}

func TestAnAudienceStatedAsOneStringIsRead(t *testing.T) {
	// RFC 7519 section 4.1.3 lets aud be a single string or an array, and an
	// Asgardeo token that names only the client identifier arrives as the
	// former. Reading one shape and not the other would make the audience
	// check depend on how an issuer chose to encode it.
	issuerURL, keys := serveJWKS(t)
	service := newJWKSService(t, issuerURL)
	claims := validClaims(issuerURL)
	claims["aud"] = jwtAudience

	status := callWith(t, service, mintJWT(t, keys, jose.RS256, claims))

	if status != http.StatusOK {
		t.Fatalf("a token naming its audience as one string was answered %d, want %d",
			status, http.StatusOK)
	}
}

func TestAnIssuerMintedTokenIsRefusedWhenItsClaimsAreNotServed(t *testing.T) {
	issuerURL, keys := serveJWKS(t)
	other, otherKeys := serveJWKS(t)

	for name, breakage := range map[string]struct {
		claims    func(map[string]any)
		algorithm jose.SignatureAlgorithm
		signWith  string // "issuer" or "other"
		want      int
	}{
		"the token names another issuer": {
			claims: func(c map[string]any) { c["iss"] = other }, want: http.StatusUnauthorized,
		},
		"the token was signed by another issuer's key": {
			signWith: "other", want: http.StatusUnauthorized,
		},
		"the token has expired": {
			claims: func(c map[string]any) { c["exp"] = time.Now().Add(-time.Minute).Unix() },
			want:   http.StatusUnauthorized,
		},
		"the token states no lifetime": {
			claims: func(c map[string]any) { delete(c, "exp") }, want: http.StatusUnauthorized,
		},
		// This one is not our configuration's refusal, and is here to pin a
		// refusal we depend on go-oidc for: it drops "none" and the HMAC
		// algorithms from an issuer's advertised set before a caller's
		// configuration is read, so a symmetric algorithm never reaches this
		// service's verifier. Nothing in statusservice would catch it if a
		// future library, or a swap to a different one, stopped doing that.
		"the token is signed with a symmetric algorithm": {
			algorithm: jose.HS256, want: http.StatusUnauthorized,
		},
		// This one is our configuration's refusal. The issuer advertises
		// ES256, publishes the key to check it, and signs a token correct in
		// every other respect — so go-oidc would accept it on the issuer's
		// say-so. What refuses it is SupportedSigningAlgs naming the single
		// algorithm this service accepts. Delete that line and this case
		// returns 200: the guarantee is that an issuer cannot widen the
		// algorithms this service checks its tokens against by advertising
		// more of them.
		"the token is signed with an algorithm this service does not accept": {
			algorithm: jose.ES256, want: http.StatusUnauthorized,
		},
		"the token names no audience this service serves": {
			claims: func(c map[string]any) { c["aud"] = []string{"some-client-id"} },
			want:   http.StatusForbidden,
		},
		"the token does not carry the required permission": {
			claims: func(c map[string]any) { c["scope"] = "reference:status:write" },
			want:   http.StatusForbidden,
		},
		"the token states no permissions": {
			claims: func(c map[string]any) { delete(c, "scope") }, want: http.StatusUnauthorized,
		},
		"the token names another organization": {
			claims: func(c map[string]any) { c["org_id"] = "another-organization" },
			want:   http.StatusForbidden,
		},
	} {
		t.Run(name, func(t *testing.T) {
			service := newJWKSService(t, issuerURL)
			claims := validClaims(issuerURL)
			if breakage.claims != nil {
				breakage.claims(claims)
			}
			algorithm := jose.RS256
			if breakage.algorithm != "" {
				algorithm = breakage.algorithm
			}
			signingKeys := keys
			if breakage.signWith == "other" {
				signingKeys = otherKeys
			}

			status := callWith(t, service, mintJWT(t, signingKeys, algorithm, claims))

			if status != breakage.want {
				t.Errorf("status = %d, want %d", status, breakage.want)
			}
		})
	}
}

func TestATokenNamingNoOrganizationIsBoundByItsIssuer(t *testing.T) {
	// Asgardeo mints no organization claim outside a sub-organization setup,
	// so a token that names none has to be accepted or nothing it issues would
	// work. What binds such a token to one organization is the issuer: this
	// service is configured with the issuer that speaks for its organization,
	// and a token minted by any other is refused however correct its claims.
	issuerURL, keys := serveJWKS(t)
	foreignURL, foreignKeys := serveJWKS(t)
	service := newJWKSService(t, issuerURL)

	own := mintJWT(t, keys, jose.RS256, validClaims(issuerURL))
	if status := callWith(t, service, own); status != http.StatusOK {
		t.Errorf("a token from this service's own issuer was answered %d, want %d",
			status, http.StatusOK)
	}

	foreign := mintJWT(t, foreignKeys, jose.RS256, validClaims(foreignURL))
	if status := callWith(t, service, foreign); status != http.StatusUnauthorized {
		t.Errorf("a token from another organization's issuer was answered %d, want %d",
			status, http.StatusUnauthorized)
	}
}

func TestAServiceMustSayHowItEstablishesTrust(t *testing.T) {
	issuerURL, _ := serveJWKS(t)
	base := statusservice.Options{
		Audience: jwtAudience, RequiredScope: jwtScope, Organization: jwtOrganization,
	}
	for name, options := range map[string]statusservice.Options{
		"neither a credential nor an issuer": base,
		"both a credential and an issuer": func() statusservice.Options {
			both := base
			both.SourceCredential, both.Issuer = "a-credential", issuerURL
			return both
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := statusservice.New(options); err == nil {
				t.Error("statusservice.New accepted a service that cannot say how it verifies tokens")
			}
		})
	}
}
