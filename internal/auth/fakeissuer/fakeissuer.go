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

// Package fakeissuer is a deterministic OIDC issuer for authentication tests.
//
// It serves discovery, authorization, token, JWKS, and introspection endpoints
// on an in-test HTTP server and signs real RS256 JWTs, so the code under test
// exercises the same verification path a production issuer would demand. It is
// a test fixture: auto-approving, single-user, and configurable only where a
// test needs to model a backend's behavioral differences (refresh scope
// handling and refresh-token rotation).
package fakeissuer

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

// Options configure how the issuer behaves at its token endpoint.
type Options struct {
	// RefreshScopeMode is how the refresh grant treats a narrower scope
	// request: "honor" narrows the issued token (IS-source behavior), "ignore"
	// returns the original full scope set, "reject" answers invalid_scope.
	// The default is "honor".
	RefreshScopeMode string
	// Audience is the aud claim minted into access tokens.
	Audience string
	// RotateRefreshTokens issues a new refresh token on every refresh,
	// invalidating the one presented.
	RotateRefreshTokens bool
	// OmitRefreshScopeField leaves the scope member out of refresh responses,
	// modeling issuers that answer without stating the effective scopes.
	OmitRefreshScopeField bool
}

// Issuer is one running fake issuer. Its URL doubles as the issuer identifier.
type Issuer struct {
	URL string

	opts  Options
	key   *rsa.PrivateKey
	keyID string

	mutex         sync.Mutex
	codes         map[string]codeGrant
	refreshTokens map[string][]string    // refresh token -> granted scopes
	accessTokens  map[string]tokenRecord // access token -> introspectable facts
}

type codeGrant struct {
	challenge   string
	scopes      []string
	redirectURI string
	nonce       string
	clientID    string
}

type tokenRecord struct {
	scopes   []string
	audience string
	subject  string
}

// loopbackRedirect is the documented registration: the four fixed loopback
// callback ports, path /callback, host 127.0.0.1.
var loopbackRedirect = regexp.MustCompile(`^http://127\.0\.0\.1:(10425|10426|10427|10428)/callback$`)

// New starts the issuer on an httptest server and closes it on test cleanup.
func New(t *testing.T, opts Options) *Issuer {
	t.Helper()
	if opts.RefreshScopeMode == "" {
		opts.RefreshScopeMode = "honor"
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("fakeissuer: generate signing key: %v", err)
	}
	issuer := &Issuer{
		opts:          opts,
		key:           key,
		keyID:         randomToken("key"),
		codes:         map[string]codeGrant{},
		refreshTokens: map[string][]string{},
		accessTokens:  map[string]tokenRecord{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", issuer.handleDiscovery)
	mux.HandleFunc("GET /jwks", issuer.handleJWKS)
	mux.HandleFunc("GET /authorize", issuer.handleAuthorize)
	mux.HandleFunc("POST /token", issuer.handleToken)
	mux.HandleFunc("POST /introspect", issuer.handleIntrospect)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	issuer.URL = server.URL
	return issuer
}

// SeedSession mints a live refresh token directly, so broker tests need no
// browser step. The returned value goes into session.Session.RefreshToken.
func (i *Issuer) SeedSession(scopes []string) string {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	seeded := randomToken("rt")
	i.refreshTokens[seeded] = append([]string(nil), scopes...)
	return seeded
}

// Introspect answers whether the issuer minted this exact access token, with
// its scopes and audience. Backed by the issuer's /introspect endpoint.
func (i *Issuer) Introspect(t *testing.T, token string) (active bool, scopes, audience []string) {
	t.Helper()
	response, err := http.PostForm(i.URL+"/introspect", map[string][]string{"token": {token}})
	if err != nil {
		t.Fatalf("fakeissuer: introspect: %v", err)
	}
	defer response.Body.Close()
	var report struct {
		Active   bool     `json:"active"`
		Scope    string   `json:"scope"`
		Audience []string `json:"aud"`
	}
	if err := json.NewDecoder(response.Body).Decode(&report); err != nil {
		t.Fatalf("fakeissuer: introspect decode: %v", err)
	}
	return report.Active, splitScopes(report.Scope), report.Audience
}

func (i *Issuer) handleDiscovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                i.URL,
		"authorization_endpoint":                i.URL + "/authorize",
		"token_endpoint":                        i.URL + "/token",
		"jwks_uri":                              i.URL + "/jwks",
		"introspection_endpoint":                i.URL + "/introspect",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"code_challenge_methods_supported":      []string{"S256"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token", "client_credentials"},
	})
}

func (i *Issuer) handleJWKS(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key: i.key.Public(), KeyID: i.keyID, Algorithm: string(jose.RS256), Use: "sig",
	}}})
}

// handleAuthorize auto-approves: the "user" always consents, so a test's whole
// login is one redirect-following request chain.
func (i *Issuer) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	redirectURI := query.Get("redirect_uri")
	switch {
	case query.Get("client_id") == "":
		http.Error(w, "missing client_id", http.StatusBadRequest)
	case !loopbackRedirect.MatchString(redirectURI):
		http.Error(w, "redirect_uri is not a registered loopback callback", http.StatusBadRequest)
	case query.Get("code_challenge") == "" || query.Get("code_challenge_method") != "S256":
		http.Error(w, "an S256 code challenge is required", http.StatusBadRequest)
	default:
		code := randomToken("code")
		i.mutex.Lock()
		i.codes[code] = codeGrant{
			challenge:   query.Get("code_challenge"),
			scopes:      splitScopes(query.Get("scope")),
			redirectURI: redirectURI,
			nonce:       query.Get("nonce"),
			clientID:    query.Get("client_id"),
		}
		i.mutex.Unlock()
		http.Redirect(w, r, fmt.Sprintf("%s?code=%s&state=%s", redirectURI, code, query.Get("state")), http.StatusFound)
	}
}

func (i *Issuer) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		i.exchangeCode(w, r)
	case "refresh_token":
		i.refreshGrant(w, r)
	case "client_credentials":
		i.clientCredentialsGrant(w, r)
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type")
	}
}

func (i *Issuer) exchangeCode(w http.ResponseWriter, r *http.Request) {
	i.mutex.Lock()
	grant, found := i.codes[r.PostForm.Get("code")]
	delete(i.codes, r.PostForm.Get("code")) // a code is single-use, spent even on failure
	i.mutex.Unlock()
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	proof := base64.RawURLEncoding.EncodeToString(sum[:])
	if !found || grant.clientID != r.PostForm.Get("client_id") ||
		grant.redirectURI != r.PostForm.Get("redirect_uri") ||
		subtle.ConstantTimeCompare([]byte(proof), []byte(grant.challenge)) != 1 {
		oauthError(w, http.StatusBadRequest, "invalid_grant")
		return
	}
	refreshToken := randomToken("rt")
	i.mutex.Lock()
	i.refreshTokens[refreshToken] = grant.scopes
	i.mutex.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  i.mintAccessToken("user-1", grant.scopes),
		"token_type":    "Bearer",
		"expires_in":    300,
		"refresh_token": refreshToken,
		"id_token":      i.mintIDToken(grant.clientID, grant.nonce),
		"scope":         strings.Join(grant.scopes, " "),
	})
}

func (i *Issuer) refreshGrant(w http.ResponseWriter, r *http.Request) {
	presented := r.PostForm.Get("refresh_token")
	i.mutex.Lock()
	original, found := i.refreshTokens[presented]
	i.mutex.Unlock()
	if !found {
		oauthError(w, http.StatusBadRequest, "invalid_grant")
		return
	}
	requested := splitScopes(r.PostForm.Get("scope"))
	issued := original
	if len(requested) > 0 {
		switch i.opts.RefreshScopeMode {
		case "honor":
			for _, scope := range requested {
				if !contains(original, scope) {
					oauthError(w, http.StatusBadRequest, "invalid_scope")
					return
				}
			}
			issued = requested
		case "ignore":
			issued = original
		case "reject":
			oauthError(w, http.StatusBadRequest, "invalid_scope")
			return
		}
	}
	response := map[string]any{
		"access_token": i.mintAccessToken("user-1", issued),
		"token_type":   "Bearer",
		"expires_in":   300,
	}
	if i.opts.RotateRefreshTokens {
		rotated := randomToken("rt")
		i.mutex.Lock()
		delete(i.refreshTokens, presented)
		i.refreshTokens[rotated] = original
		i.mutex.Unlock()
		response["refresh_token"] = rotated
	}
	if !i.opts.OmitRefreshScopeField {
		response["scope"] = strings.Join(issued, " ")
	}
	writeJSON(w, http.StatusOK, response)
}

func (i *Issuer) clientCredentialsGrant(w http.ResponseWriter, r *http.Request) {
	clientID, clientSecret, ok := r.BasicAuth()
	if !ok {
		clientID, clientSecret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	if clientID == "" || clientSecret == "" {
		oauthError(w, http.StatusUnauthorized, "invalid_client")
		return
	}
	scopes := splitScopes(r.PostForm.Get("scope"))
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": i.mintAccessToken("client-1", scopes),
		"token_type":   "Bearer",
		"expires_in":   300,
		"scope":        strings.Join(scopes, " "),
	})
}

func (i *Issuer) handleIntrospect(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	i.mutex.Lock()
	record, found := i.accessTokens[r.PostForm.Get("token")]
	i.mutex.Unlock()
	if !found {
		writeJSON(w, http.StatusOK, map[string]any{"active": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"active": true,
		"scope":  strings.Join(record.scopes, " "),
		"aud":    []string{record.audience},
		"sub":    record.subject,
		"iss":    i.URL,
	})
}

// mintAccessToken signs a real RS256 access token and records it for
// introspection.
func (i *Issuer) mintAccessToken(subject string, scopes []string) string {
	now := time.Now()
	token := i.sign(map[string]any{
		"iss":   i.URL,
		"sub":   subject,
		"aud":   i.opts.Audience,
		"scope": strings.Join(scopes, " "),
		"exp":   now.Add(5 * time.Minute).Unix(),
		"iat":   now.Unix(),
	})
	i.mutex.Lock()
	i.accessTokens[token] = tokenRecord{
		scopes:   append([]string(nil), scopes...),
		audience: i.opts.Audience,
		subject:  subject,
	}
	i.mutex.Unlock()
	return token
}

func (i *Issuer) mintIDToken(clientID, nonce string) string {
	now := time.Now()
	return i.sign(map[string]any{
		"iss":   i.URL,
		"sub":   "user-1",
		"aud":   clientID,
		"exp":   now.Add(5 * time.Minute).Unix(),
		"iat":   now.Unix(),
		"nonce": nonce,
		"email": "dev@example.test",
	})
}

func (i *Issuer) sign(claims map[string]any) string {
	payload, err := json.Marshal(claims)
	if err != nil {
		panic(fmt.Sprintf("fakeissuer: marshal claims: %v", err))
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: i.key},
		(&jose.SignerOptions{}).WithHeader("kid", i.keyID).WithType("JWT"))
	if err != nil {
		panic(fmt.Sprintf("fakeissuer: build signer: %v", err))
	}
	signature, err := signer.Sign(payload)
	if err != nil {
		panic(fmt.Sprintf("fakeissuer: sign token: %v", err))
	}
	serialized, err := signature.CompactSerialize()
	if err != nil {
		panic(fmt.Sprintf("fakeissuer: serialize token: %v", err))
	}
	return serialized
}

func randomToken(prefix string) string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		panic(fmt.Sprintf("fakeissuer: random token: %v", err))
	}
	return prefix + "-" + hex.EncodeToString(raw)
}

func splitScopes(value string) []string {
	return strings.Fields(value)
}

func contains(scopes []string, wanted string) bool {
	for _, scope := range scopes {
		if scope == wanted {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		panic(fmt.Sprintf("fakeissuer: encode response: %v", err))
	}
}

func oauthError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}
