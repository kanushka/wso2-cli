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
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
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
	// ClientScopeMode is how the client-credentials grant treats the scopes a
	// request asks for: "honor" issues exactly them, "ignore" issues
	// ClientScopes whatever was asked for, "reject" answers invalid_scope. The
	// default is "honor". It is separate from RefreshScopeMode because a
	// deployment may narrow one grant and not the other.
	ClientScopeMode string
	// ClientScopes is the permission set the registered client carries. It is
	// what an "ignore" issuer answers a client-credentials request with,
	// modeling a deployment that hands out the application's whole authority
	// regardless of what a request asked for.
	ClientScopes []string
	// ClientSecret is the secret the registered client must present on the
	// client-credentials grant. When empty the issuer takes any non-empty
	// secret, so only a test whose subject is a wrong credential has to state
	// one.
	ClientSecret string
	// Audience is the aud claim minted into access tokens.
	Audience string
	// RotateRefreshTokens issues a new refresh token on every refresh,
	// invalidating the one presented.
	RotateRefreshTokens bool
	// OmitRefreshScopeField leaves the scope member out of refresh responses,
	// modeling issuers that answer without stating the effective scopes.
	OmitRefreshScopeField bool
	// AllowAnyLoopbackPort accepts a callback on any 127.0.0.1 port instead of
	// only the four registered ones, so a test can bind an ephemeral port and
	// run in parallel with anything else on the machine.
	AllowAnyLoopbackPort bool
	// OmitS256 leaves code_challenge_methods_supported out of the discovery
	// document, modeling a deployment that does not advertise PKCE.
	OmitS256 bool
	// OmitNonce leaves the nonce out of identity tokens, modeling an issuer
	// that does not echo the value the request bound the login to.
	OmitNonce bool
	// OmitRefreshToken answers the authorization code grant without a refresh
	// token, modeling an application that was never granted offline access.
	OmitRefreshToken bool
	// NegativeSerialCertificate publishes an x5c certificate chain on the JWKS
	// key whose certificate carries a negative serial number: forbidden by RFC
	// 5280 section 4.1.2.2, emitted by WSO2 deployments for years, and rejected
	// outright by Go's x509 parser since 1.23.
	//
	// The signing key itself stays valid — n and e are untouched — so this
	// models the real failure exactly: a deployment whose keys can verify a
	// token perfectly well, behind a certificate that nothing needs to read.
	NegativeSerialCertificate bool
}

// Issuer is one running fake issuer. Its URL doubles as the issuer identifier.
type Issuer struct {
	URL string

	opts   Options
	key    *rsa.PrivateKey
	keyID  string
	client *http.Client
	// certificate is the DER published in the key's x5c chain, empty unless a
	// test asked for one.
	certificate []byte

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
	opts.RefreshScopeMode = scopeMode(t, "RefreshScopeMode", opts.RefreshScopeMode)
	opts.ClientScopeMode = scopeMode(t, "ClientScopeMode", opts.ClientScopeMode)
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
	if opts.NegativeSerialCertificate {
		issuer.certificate = negativeSerialCertificate(t, key)
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
	issuer.client = server.Client()
	return issuer
}

// HTTPClient is a client that reaches this issuer. The issuer speaks plain
// HTTP on the loopback interface, so this is an ordinary client; it exists so a
// test never has to decide which client to hand the code under test.
func (i *Issuer) HTTPClient() *http.Client { return i.client }

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
	defer func() { _ = response.Body.Close() }()
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
	document := map[string]any{
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
	}
	if i.opts.OmitS256 {
		delete(document, "code_challenge_methods_supported")
	}
	writeJSON(w, http.StatusOK, document)
}

func (i *Issuer) handleJWKS(w http.ResponseWriter, _ *http.Request) {
	key := jose.JSONWebKey{
		Key: i.key.Public(), KeyID: i.keyID, Algorithm: string(jose.RS256), Use: "sig",
	}
	if len(i.certificate) == 0 {
		writeJSON(w, http.StatusOK, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{key}})
		return
	}
	// The chain is attached after go-jose has rendered the key, because
	// go-jose cannot marshal a certificate it would refuse to parse — which is
	// the entire point of this fixture.
	rendered, err := key.MarshalJSON()
	if err != nil {
		http.Error(w, "fakeissuer: render key: "+err.Error(), http.StatusInternalServerError)
		return
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(rendered, &members); err != nil {
		http.Error(w, "fakeissuer: reread key: "+err.Error(), http.StatusInternalServerError)
		return
	}
	chain, err := json.Marshal([]string{base64.StdEncoding.EncodeToString(i.certificate)})
	if err != nil {
		http.Error(w, "fakeissuer: render chain: "+err.Error(), http.StatusInternalServerError)
		return
	}
	members["x5c"] = chain
	writeJSON(w, http.StatusOK, map[string]any{"keys": []any{members}})
}

// negativeSerialCertificate returns a self-signed certificate for key whose
// serial number is negative.
//
// It cannot be minted directly: crypto/x509.CreateCertificate refuses a
// negative serial ("serial number must be positive"), which is why the value
// is edited into the encoding afterwards. A serial whose leading value byte
// has its high bit set is encoded by Go with a 0x00 pad to keep it positive;
// removing that pad reinterprets the same four bytes as a negative
// two's-complement integer, which is precisely the encoding real deployments
// publish. Removing a byte shortens the two enclosing SEQUENCEs by one each.
func negativeSerialCertificate(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()
	const padded = "\x02\x05\x00\xc5\xb0\x7c\x97" // INTEGER, 5 bytes, positive
	const negative = "\x02\x04\xc5\xb0\x7c\x97"   // INTEGER, 4 bytes, negative
	template := &x509.Certificate{
		SerialNumber: big.NewInt(0xC5B07C97),
		Subject:      pkix.Name{CommonName: "fakeissuer negative serial"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("fakeissuer: create certificate: %v", err)
	}
	at := strings.Index(string(der), padded)
	if at < 0 {
		t.Fatal("fakeissuer: the serial number is not encoded where this fixture expects it")
	}
	edited := make([]byte, 0, len(der)-1)
	edited = append(edited, der[:at]...)
	edited = append(edited, negative...)
	edited = append(edited, der[at+len(padded):]...)
	// Both the Certificate and the TBSCertificate SEQUENCE use a long-form
	// two-byte length, and each now describes one byte less.
	for _, offset := range []int{2, 6} {
		if edited[offset-2] != 0x30 || edited[offset-1] != 0x82 {
			t.Fatalf("fakeissuer: unexpected DER header at offset %d", offset-2)
		}
		binary.BigEndian.PutUint16(edited[offset:], binary.BigEndian.Uint16(edited[offset:])-1)
	}
	if _, err := x509.ParseCertificate(edited); err == nil {
		t.Fatal("fakeissuer: the certificate this fixture exists to make unparseable parses")
	}
	return edited
}

// handleAuthorize auto-approves: the "user" always consents, so a test's whole
// login is one redirect-following request chain.
func (i *Issuer) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	redirectURI := query.Get("redirect_uri")
	switch {
	case query.Get("client_id") == "":
		http.Error(w, "missing client_id", http.StatusBadRequest)
	case !i.acceptsRedirect(redirectURI):
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
		callback := url.Values{"code": {code}, "state": {query.Get("state")}}
		http.Redirect(w, r, redirectURI+"?"+callback.Encode(), http.StatusFound)
	}
}

// acceptsRedirect reports whether the callback URL is one this issuer is
// registered for. The registration is the four documented loopback ports; a
// test that binds an ephemeral port opts into the wider check explicitly, so
// the strict rule is what every other test exercises.
func (i *Issuer) acceptsRedirect(raw string) bool {
	if loopbackRedirect.MatchString(raw) {
		return true
	}
	if !i.opts.AllowAnyLoopbackPort {
		return false
	}
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "http" &&
		parsed.Hostname() == "127.0.0.1" && parsed.Path == "/callback"
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
	if !found || grant.clientID != presentedClientID(r) ||
		grant.redirectURI != r.PostForm.Get("redirect_uri") ||
		subtle.ConstantTimeCompare([]byte(proof), []byte(grant.challenge)) != 1 {
		oauthError(w, http.StatusBadRequest, "invalid_grant")
		return
	}
	response := map[string]any{
		"access_token": i.mintAccessToken("user-1", grant.scopes),
		"token_type":   "Bearer",
		"expires_in":   300,
		"id_token":     i.mintIDToken(grant.clientID, grant.nonce),
		"scope":        strings.Join(grant.scopes, " "),
	}
	if !i.opts.OmitRefreshToken {
		refreshToken := randomToken("rt")
		i.mutex.Lock()
		i.refreshTokens[refreshToken] = grant.scopes
		i.mutex.Unlock()
		response["refresh_token"] = refreshToken
	}
	writeJSON(w, http.StatusOK, response)
}

// presentedClientID reads the client identifier a token request identifies
// itself with. A public client states it in the body and a confidential one
// presents it as HTTP Basic credentials; a real issuer accepts both, so the
// fixture does too rather than pinning the code under test to one style.
func presentedClientID(r *http.Request) string {
	id, _ := presentedClientCredentials(r)
	return id
}

func (i *Issuer) refreshGrant(w http.ResponseWriter, r *http.Request) {
	presented := r.PostForm.Get("refresh_token")

	// Finding the presented token and retiring it happen in one critical
	// section. Split across two, a pair of concurrent refreshes presenting the
	// same token both pass the lookup and both rotate, so the issuer honors a
	// token it had already replaced. That matters more here than it would in a
	// product: the rotation assertions in test/acceptance/login_test.go and
	// source_browser_test.go treat this issuer as the oracle for "the previous
	// refresh token is dead", and an oracle that admits the replay would let a
	// real double-rotation defect through.
	//
	// The scope decision is inside the lock because a refusal must leave the
	// presented token alive — a deployment that rejects a request does not
	// retire the credential that made it — and that cannot be decided before
	// the lookup it depends on. It is map reads and a slice scan, so nothing
	// blocks on it.
	requested := splitScopes(r.PostForm.Get("scope"))

	i.mutex.Lock()
	original, found := i.refreshTokens[presented]
	issued := original
	rejected := false
	if found && len(requested) > 0 {
		switch i.opts.RefreshScopeMode {
		case "honor":
			for _, scope := range requested {
				if !slices.Contains(original, scope) {
					rejected = true
				}
			}
			if !rejected {
				issued = requested
			}
		case "ignore":
			issued = original
		case "reject":
			rejected = true
		}
	}
	var rotated string
	if found && !rejected && i.opts.RotateRefreshTokens {
		rotated = randomToken("rt")
		delete(i.refreshTokens, presented)
		i.refreshTokens[rotated] = original
	}
	i.mutex.Unlock()

	if !found {
		oauthError(w, http.StatusBadRequest, "invalid_grant")
		return
	}
	if rejected {
		oauthError(w, http.StatusBadRequest, "invalid_scope")
		return
	}
	response := map[string]any{
		"access_token": i.mintAccessToken("user-1", issued),
		"token_type":   "Bearer",
		"expires_in":   300,
	}
	if rotated != "" {
		response["refresh_token"] = rotated
	}
	if !i.opts.OmitRefreshScopeField {
		response["scope"] = strings.Join(issued, " ")
	}
	writeJSON(w, http.StatusOK, response)
}

func (i *Issuer) clientCredentialsGrant(w http.ResponseWriter, r *http.Request) {
	clientID, clientSecret := presentedClientCredentials(r)
	if clientID == "" || clientSecret == "" ||
		(i.opts.ClientSecret != "" && clientSecret != i.opts.ClientSecret) {
		oauthError(w, http.StatusUnauthorized, "invalid_client")
		return
	}
	requested := splitScopes(r.PostForm.Get("scope"))
	issued := requested
	switch i.opts.ClientScopeMode {
	case "honor":
		issued = requested
	case "ignore":
		// The deployment hands back the registered client's whole authority,
		// whatever the request narrowed itself to.
		issued = i.opts.ClientScopes
	case "reject":
		oauthError(w, http.StatusBadRequest, "invalid_scope")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": i.mintAccessToken("client-1", issued),
		"token_type":   "Bearer",
		"expires_in":   300,
		"scope":        strings.Join(issued, " "),
	})
}

// presentedClientCredentials reads the client identifier and secret a
// confidential client identified itself with. RFC 6749 requires the values to
// be form-encoded before they are used as HTTP Basic credentials, so they are
// decoded here — a real authorization server has to, and a fixture that did not
// would quietly accept a client the deployment would reject.
func presentedClientCredentials(r *http.Request) (id, secret string) {
	id, secret, ok := r.BasicAuth()
	if !ok {
		return r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	if decoded, err := url.QueryUnescape(id); err == nil {
		id = decoded
	}
	if decoded, err := url.QueryUnescape(secret); err == nil {
		secret = decoded
	}
	return id, secret
}

// scopeMode validates a configured scope mode, so a typo in a test's options
// fails the test rather than silently selecting the permissive default.
func scopeMode(t *testing.T, option, configured string) string {
	t.Helper()
	switch configured {
	case "":
		return "honor"
	case "honor", "ignore", "reject":
		return configured
	default:
		t.Fatalf("fakeissuer: %s = %q, want honor, ignore, or reject", option, configured)
		return ""
	}
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
	report := map[string]any{
		"active": true,
		"scope":  strings.Join(record.scopes, " "),
		"sub":    record.subject,
		"iss":    i.URL,
	}
	if record.audience != "" {
		report["aud"] = []string{record.audience}
	}
	writeJSON(w, http.StatusOK, report)
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
	claims := map[string]any{
		"iss":   i.URL,
		"sub":   "user-1",
		"aud":   clientID,
		"exp":   now.Add(5 * time.Minute).Unix(),
		"iat":   now.Unix(),
		"nonce": nonce,
		"email": "dev@example.test",
	}
	if i.opts.OmitNonce {
		delete(claims, "nonce")
	}
	return i.sign(claims)
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
