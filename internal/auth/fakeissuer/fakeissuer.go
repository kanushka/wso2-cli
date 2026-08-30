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
	// Audience is the aud claim minted into access tokens. A request carrying a
	// resource indicator overrides it, exactly as a deployment that binds tokens
	// to a named resource server does.
	Audience string
	// OrganizationClaim is the org_id an access token names. When it is empty
	// a token carries no organization claim at all, which is what Asgardeo
	// issues outside a sub-organization setup — and therefore what a resource
	// server has to accept, binding the token to an organization through the
	// issuer it trusts rather than through a claim.
	OrganizationClaim string
	// RequireResource refuses any authorization request that carries no RFC 8707
	// resource indicator, and mints the audience from the one it was given.
	//
	// It models a deployment that decides the audience at authorization time
	// rather than from the application's registration — ThunderID, measured at
	// v1.0.0-beta, which answers invalid_target without one. The consequence
	// worth modeling is not the refusal but what follows from it: a session
	// established this way reaches exactly one protected resource.
	RequireResource bool
	// RegisteredResource is the only protected resource this deployment knows,
	// when it is set. A request naming any other is refused with invalid_target,
	// modeling a resource server that was never registered — the same OAuth
	// error as a request that named none, arriving for the opposite reason.
	RegisteredResource string
	// RotateRefreshTokens issues a new refresh token on every refresh,
	// invalidating the one presented.
	RotateRefreshTokens bool
	// OmitRefreshScopeField leaves the scope member out of refresh responses,
	// modeling issuers that answer without stating the effective scopes.
	OmitRefreshScopeField bool
	// RefreshTokenExpiresIn states a refresh token's own lifetime, in seconds,
	// on the authorization-code grant's response and on a rotated refresh
	// token's response. Zero leaves the member out entirely, modeling the many
	// issuers that disclose no refresh-token lifetime at all — the expected
	// case R7 (#112) is written against, not the exceptional one.
	RefreshTokenExpiresIn int
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

	// DeviceOutcome is how a device authorization ends once the pending and
	// slow-down answers below are exhausted: "approve" issues tokens, "deny"
	// answers access_denied, "expire" answers expired_token. The default is
	// "approve".
	DeviceOutcome string
	// DevicePendingPolls is how many polls answer authorization_pending before
	// DeviceOutcome is applied. The default is zero, so the first poll settles
	// the flow — a test that cares about the waiting states asks for them.
	DevicePendingPolls int
	// DeviceSlowDownPolls is how many polls answer slow_down. They are served
	// before the pending ones, so a test can ask for both and know which
	// arrives first.
	DeviceSlowDownPolls int
	// DeviceInterval is the polling interval the device authorization response
	// advertises, in seconds. Zero leaves the member out entirely, which is how
	// a test reaches the client's own default. Any other value is sent
	// verbatim, negative and absurd ones included: RFC 8628 constrains what a
	// deployment should say and nothing constrains what one can say, and a
	// client that carried such a value into its own arithmetic would fail in a
	// way no refusal describes.
	//
	// It is an int64 because the shell parses the member into one, so a test may
	// advertise a value beyond a 32-bit int on any platform. An int here would
	// instead stop the 32-bit builds compiling at the test that asks for one.
	DeviceInterval int64
	// DeviceExpiresIn is the lifetime the device authorization response
	// advertises, in seconds. The default is 600, which is the order of
	// magnitude real deployments publish.
	DeviceExpiresIn int
	// OmitDeviceEndpoint leaves device_authorization_endpoint and the device
	// grant out of the discovery document, modeling a deployment that does not
	// serve the grant at all. Thunder is such a deployment today.
	OmitDeviceEndpoint bool

	// OmitRevocationEndpoint leaves revocation_endpoint out of the discovery
	// document, modeling a deployment that publishes no way to retract a
	// refresh token. Whether any supported deployment is such a deployment was
	// unmeasured when logout was designed, which is exactly why the shell
	// discovers this rather than assuming it. See
	// docs/adr/0010-best-effort-revocation-on-session-end.md.
	OmitRevocationEndpoint bool
	// RefuseRevocation advertises the endpoint and then answers every
	// revocation request with invalid_request, modeling the deployment that
	// says it revokes and will not. RFC 7009 tells a server to answer 200 even
	// for a token it does not recognize, so a refusal here is a deployment
	// disagreeing with the request itself — a client the endpoint declines to
	// serve, most plausibly a public one.
	RefuseRevocation bool
	// OmitDeviceVerificationURIComplete leaves verification_uri_complete out of
	// the device authorization response, modeling the many deployments that
	// publish only the code and the plain URI. RFC 8628 makes the member
	// optional, so a client may not depend on it.
	OmitDeviceVerificationURIComplete bool
	// OmitDeviceIDToken answers the device grant without an identity token.
	//
	// Whether Asgardeo and Identity Server return one from this grant is not
	// measured, so both answers are modeled rather than assumed. See issue #42.
	OmitDeviceIDToken bool
	// OmitDeviceRefreshToken answers the device grant without a refresh token,
	// modeling an application that was never granted offline access. A session
	// is a refresh token, so this is the answer that produces a login with
	// nothing to store.
	OmitDeviceRefreshToken bool
	// DeviceIDTokenAudience overrides the audience minted into the device
	// grant's identity token. A value naming another application models the
	// commonest real cause of a token that will not verify: a context document
	// whose client identifier is not the one the deployment signed in.
	DeviceIDTokenAudience string
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
	refreshTokens map[string]refreshRecord // refresh token -> what it may renew
	accessTokens  map[string]tokenRecord   // access token -> introspectable facts
	deviceGrants  map[string]*deviceGrant
	devicePolls   []time.Time
	// lastDeviceCode is the most recently minted device code, recorded because
	// map iteration order could not name "most recent" if a test ever started
	// two authorizations.
	lastDeviceCode string
}

type codeGrant struct {
	challenge   string
	scopes      []string
	redirectURI string
	nonce       string
	clientID    string
	resource    string
}

// refreshRecord is what a refresh token may renew: the permissions it was
// granted, and the protected resource the authorization bound it to.
//
// The resource travels with the token because that is what the deployments
// requiring one do — the binding is established once, at authorization, and
// every renewal inherits it. A fixture that dropped it would let the shell's
// refresh look correct while a real deployment returned access bound elsewhere.
type refreshRecord struct {
	scopes   []string
	resource string
}

// deviceGrant is one device authorization awaiting approval.
type deviceGrant struct {
	userCode string
	scopes   []string
	clientID string
	// pending and slowDown count down the answers still owed before the
	// outcome applies. They live on the grant rather than on the issuer so two
	// concurrent tests cannot consume each other's waiting states.
	pending  int
	slowDown int
	// redeemed marks a grant whose approval has already produced tokens. The
	// grant is kept rather than deleted so a later poll is answered the way a
	// deployment answers a spent code, and so LastDeviceCode can still name it.
	redeemed bool
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
	opts.DeviceOutcome = deviceOutcome(t, opts.DeviceOutcome)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("fakeissuer: generate signing key: %v", err)
	}
	issuer := &Issuer{
		opts:          opts,
		key:           key,
		keyID:         randomToken("key"),
		codes:         map[string]codeGrant{},
		refreshTokens: map[string]refreshRecord{},
		accessTokens:  map[string]tokenRecord{},
		deviceGrants:  map[string]*deviceGrant{},
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
	mux.HandleFunc("POST /revoke", issuer.handleRevoke)
	mux.HandleFunc("POST /device_authorize", issuer.handleDeviceAuthorize)
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
	i.refreshTokens[seeded] = refreshRecord{scopes: append([]string(nil), scopes...)}
	return seeded
}

// SeedSessionFor stores a session established against one protected resource,
// as a login carrying a resource indicator leaves behind.
//
// It is separate from SeedSession because the binding is the point: a test that
// seeds without one and then asserts the audience would be asserting the
// registration's audience, and would pass whether or not the renewal carried
// anything forward.
func (i *Issuer) SeedSessionFor(scopes []string, resource string) string {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	seeded := randomToken("rt")
	i.refreshTokens[seeded] = refreshRecord{
		scopes:   append([]string(nil), scopes...),
		resource: resource,
	}
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
		"revocation_endpoint":                   i.URL + "/revoke",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"code_challenge_methods_supported":      []string{"S256"},
		"grant_types_supported": []string{
			"authorization_code", "refresh_token", "client_credentials", deviceGrantType,
		},
		"device_authorization_endpoint": i.URL + "/device_authorize",
	}
	if i.opts.OmitS256 {
		delete(document, "code_challenge_methods_supported")
	}
	if i.opts.OmitRevocationEndpoint {
		delete(document, "revocation_endpoint")
	}
	if i.opts.OmitDeviceEndpoint {
		// Both go, because a deployment without the grant advertises neither.
		// Dropping only the endpoint would model something that does not exist
		// and would let a client pass by reading the wrong member.
		delete(document, "device_authorization_endpoint")
		document["grant_types_supported"] = []string{
			"authorization_code", "refresh_token", "client_credentials",
		}
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
	case i.opts.RequireResource && query.Get("resource") == "":
		// The refusal is redirected rather than served, because that is what a
		// deployment requiring a resource indicator does: the browser comes back
		// to the callback carrying an error, and the login reads it there.
		refusal := url.Values{
			"error":             {"invalid_target"},
			"error_description": {"No resource parameter supplied and no default resource server is configured"},
			"state":             {query.Get("state")},
		}
		http.Redirect(w, r, redirectURI+"?"+refusal.Encode(), http.StatusFound)
	default:
		code := randomToken("code")
		i.mutex.Lock()
		i.codes[code] = codeGrant{
			challenge:   query.Get("code_challenge"),
			scopes:      splitScopes(query.Get("scope")),
			redirectURI: redirectURI,
			nonce:       query.Get("nonce"),
			clientID:    query.Get("client_id"),
			resource:    query.Get("resource"),
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
	case deviceGrantType:
		i.deviceGrant(w, r)
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
		"access_token": i.mintAccessTokenFor("user-1", grant.scopes, grant.resource),
		"token_type":   "Bearer",
		"expires_in":   300,
		"id_token":     i.mintIDToken(grant.clientID, grant.nonce),
		"scope":        strings.Join(grant.scopes, " "),
	}
	if !i.opts.OmitRefreshToken {
		refreshToken := randomToken("rt")
		i.mutex.Lock()
		i.refreshTokens[refreshToken] = refreshRecord{scopes: grant.scopes, resource: grant.resource}
		i.mutex.Unlock()
		response["refresh_token"] = refreshToken
		if i.opts.RefreshTokenExpiresIn != 0 {
			response["refresh_token_expires_in"] = i.opts.RefreshTokenExpiresIn
		}
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
	record, found := i.refreshTokens[presented]
	original := record.scopes
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
		// The replacement inherits the resource as well as the permissions. A
		// rotation that dropped the binding would leave the session renewable
		// but bound to nothing, which no deployment does.
		i.refreshTokens[rotated] = record
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
	// The renewal carries the binding the authorization established, which is
	// what makes a resource indicator unnecessary on this grant.
	response := map[string]any{
		"access_token": i.mintAccessTokenFor("user-1", issued, record.resource),
		"token_type":   "Bearer",
		"expires_in":   300,
	}
	if rotated != "" {
		response["refresh_token"] = rotated
		if i.opts.RefreshTokenExpiresIn != 0 {
			response["refresh_token_expires_in"] = i.opts.RefreshTokenExpiresIn
		}
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
	resource := r.PostForm.Get("resource")
	// There is no earlier authorization for this grant to inherit a binding
	// from, so a deployment that decides the audience per request has nothing to
	// go on and refuses outright.
	if i.opts.RequireResource && resource == "" {
		oauthError(w, http.StatusBadRequest, "invalid_target")
		return
	}
	if i.opts.RegisteredResource != "" && resource != i.opts.RegisteredResource {
		oauthError(w, http.StatusBadRequest, "invalid_target")
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
		"access_token": i.mintAccessTokenFor("client-1", issued, resource),
		"token_type":   "Bearer",
		"expires_in":   300,
		"scope":        strings.Join(issued, " "),
	})
}

// deviceGrantType is RFC 8628's grant type identifier.
const deviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"

// defaultDeviceExpiresIn is the device code lifetime this issuer advertises
// when a test states none, in seconds.
const defaultDeviceExpiresIn = 600

// handleDeviceAuthorize starts one device authorization.
//
// It mints both codes and records what the eventual approval will be worth.
// Nothing here waits: the waiting states a real deployment produces while a
// human walks to another device are modeled by the counters the token endpoint
// draws down, so a test states the shape of the wait rather than living
// through it.
func (i *Issuer) handleDeviceAuthorize(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if i.opts.OmitDeviceEndpoint {
		// A deployment that does not advertise the grant does not serve it
		// either. Answering here anyway would let a client that ignored
		// discovery pass a test it should fail.
		oauthError(w, http.StatusNotFound, "invalid_request")
		return
	}
	clientID := presentedClientID(r)
	if clientID == "" {
		oauthError(w, http.StatusBadRequest, "invalid_client")
		return
	}
	deviceCode := randomToken("dc")
	userCode := randomUserCode()
	i.mutex.Lock()
	i.deviceGrants[deviceCode] = &deviceGrant{
		userCode: userCode,
		scopes:   splitScopes(r.PostForm.Get("scope")),
		clientID: clientID,
		pending:  i.opts.DevicePendingPolls,
		slowDown: i.opts.DeviceSlowDownPolls,
	}
	i.lastDeviceCode = deviceCode
	i.mutex.Unlock()

	expiresIn := i.opts.DeviceExpiresIn
	if expiresIn == 0 {
		expiresIn = defaultDeviceExpiresIn
	}
	response := map[string]any{
		"device_code":      deviceCode,
		"user_code":        userCode,
		"verification_uri": i.URL + "/device",
		"expires_in":       expiresIn,
	}
	if !i.opts.OmitDeviceVerificationURIComplete {
		response["verification_uri_complete"] = i.URL + "/device?user_code=" + userCode
	}
	// A zero interval is left out rather than sent as zero. RFC 8628 gives the
	// member a default precisely so a deployment may omit it, and a client that
	// reads a missing member as "poll as fast as you like" is a client this
	// fixture exists to catch. Every other value, negative ones included, is
	// sent exactly as the test asked for it.
	if i.opts.DeviceInterval != 0 {
		response["interval"] = i.opts.DeviceInterval
	}
	writeJSON(w, http.StatusOK, response)
}

// deviceGrant answers one poll of the token endpoint.
//
// Every poll is timestamped before anything else, including the ones that end
// the flow. That record is what lets a test assert the client honored the
// interval it was given and backed off when told to — a property observable
// only from the deployment's side, which is where this fixture stands.
func (i *Issuer) deviceGrant(w http.ResponseWriter, r *http.Request) {
	i.mutex.Lock()
	i.devicePolls = append(i.devicePolls, time.Now())
	grant, found := i.deviceGrants[r.PostForm.Get("device_code")]
	// A grant already redeemed is treated as one that was never here. A device
	// code is single-use, exactly as an authorization code is, and this fixture
	// is the oracle the device tests read "the session came from one approval"
	// off — one that answered a replay would let a real double-redemption
	// defect through.
	if found && grant.redeemed {
		found = false
	}
	var answer string
	if found {
		switch {
		case grant.slowDown > 0:
			grant.slowDown--
			answer = "slow_down"
		case grant.pending > 0:
			grant.pending--
			answer = "authorization_pending"
		case i.opts.DeviceOutcome == "deny":
			answer = "access_denied"
		case i.opts.DeviceOutcome == "expire":
			answer = "expired_token"
		}
	}
	// The grant is read, its counters drawn down, and its redemption recorded in
	// one critical section, so two concurrent polls can neither both consume the
	// last pending answer nor both be approved.
	scopes, clientID := []string(nil), ""
	if found {
		scopes, clientID = grant.scopes, grant.clientID
		if answer == "" {
			grant.redeemed = true
		}
	}
	i.mutex.Unlock()

	switch {
	case !found:
		// An unknown device code is a spent or forged one. RFC 8628 sends the
		// client to RFC 6749's invalid_grant for both.
		oauthError(w, http.StatusBadRequest, "invalid_grant")
	case answer != "":
		oauthError(w, http.StatusBadRequest, answer)
	default:
		i.issueDeviceTokens(w, scopes, clientID)
	}
}

// issueDeviceTokens answers an approved device authorization.
//
// The token set matches what the authorization code grant produces, minus the
// nonce: RFC 8628 defines no nonce parameter, so an identity token here carries
// none and a client that demanded one would be demanding something the flow
// cannot supply.
func (i *Issuer) issueDeviceTokens(w http.ResponseWriter, scopes []string, clientID string) {
	// No resource: RFC 8628 defines no resource indicator on the device
	// authorization request, and the shell sends none, so a session established
	// this way is bound by the registration's audience like every session was
	// before resource indicators existed.
	response := map[string]any{
		"access_token": i.mintAccessTokenFor("user-1", scopes, ""),
		"token_type":   "Bearer",
		"expires_in":   300,
		"scope":        strings.Join(scopes, " "),
	}
	if !i.opts.OmitDeviceRefreshToken {
		refreshToken := randomToken("rt")
		i.mutex.Lock()
		i.refreshTokens[refreshToken] = refreshRecord{scopes: scopes}
		i.mutex.Unlock()
		response["refresh_token"] = refreshToken
	}
	if !i.opts.OmitDeviceIDToken {
		audience := clientID
		if i.opts.DeviceIDTokenAudience != "" {
			audience = i.opts.DeviceIDTokenAudience
		}
		response["id_token"] = i.mintIDToken(audience, "")
	}
	writeJSON(w, http.StatusOK, response)
}

// DevicePolls returns when each poll of the device grant arrived, in order.
// A test reads the gaps between them; the absolute times mean nothing.
func (i *Issuer) DevicePolls() []time.Time {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	return append([]time.Time(nil), i.devicePolls...)
}

// LastDeviceCode is the device code this issuer most recently minted.
//
// It exists for the non-disclosure sweep: the device code is the one value in
// this flow that is exchangeable for a session, and a test cannot prove the
// shell kept it off the terminal without knowing what to look for.
func (i *Issuer) LastDeviceCode() string {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	return i.lastDeviceCode
}

// userCodeAlphabet is RFC 8628 section 6.1's recommended character set: upper
// case, and free of the pairs a person mishears or mistypes — no 0/O, no 1/I.
const userCodeAlphabet = "BCDFGHJKLMNPQRSTVWXZ"

// randomUserCode mints a code in the WDJB-MJHT shape the RFC uses as its
// example. The separator is part of the value, so a client that strips or
// reformats it fails against this fixture exactly as it would against a
// deployment that expects its own code back.
func randomUserCode() string {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		panic(fmt.Sprintf("fakeissuer: random user code: %v", err))
	}
	letters := make([]byte, 0, 9)
	for at, value := range raw {
		if at == 4 {
			letters = append(letters, '-')
		}
		letters = append(letters, userCodeAlphabet[int(value)%len(userCodeAlphabet)])
	}
	return string(letters)
}

// deviceOutcome validates a configured outcome, so a typo in a test's options
// fails the test rather than silently approving the login it meant to refuse.
func deviceOutcome(t *testing.T, configured string) string {
	t.Helper()
	switch configured {
	case "":
		return "approve"
	case "approve", "deny", "expire":
		return configured
	default:
		t.Fatalf("fakeissuer: DeviceOutcome = %q, want approve, deny, or expire", configured)
		return ""
	}
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

// handleRevoke retracts a refresh token per RFC 7009.
//
// An unknown token is answered 200 with an empty body, which is the RFC's
// instruction rather than this fixture's leniency: a client must not be able to
// tell a token that was already dead from one it just killed, or revocation
// becomes an oracle for guessing tokens. The consequence for the shell is that
// a confirmed revocation confirms the deployment was told, not that anything
// was found to retract.
//
// No client authentication is required, because this fixture stands in for a
// deployment that lets its public client revoke. RefuseRevocation is how a test
// asks for the other kind.
func (i *Issuer) handleRevoke(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if i.opts.OmitRevocationEndpoint {
		// A deployment that advertises no endpoint serves none either.
		// Answering here anyway would let a client that never read discovery
		// pass a test it should fail.
		oauthError(w, http.StatusNotFound, "invalid_request")
		return
	}
	if i.opts.RefuseRevocation {
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if presentedClientID(r) == "" {
		// RFC 7009 leaves client authentication to the deployment, but a
		// request that names no client at all is malformed on any of them.
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	presented := r.PostForm.Get("token")
	if presented == "" {
		// RFC 7009 section 2.1 makes token a required parameter. Answering 200
		// to a request that carries none would let a client regression that
		// stopped sending the refresh token look like a successful revocation
		// in every test that asserts the outcome.
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	i.mutex.Lock()
	delete(i.refreshTokens, presented)
	i.mutex.Unlock()
	w.WriteHeader(http.StatusOK)
}

// RefreshTokenLive reports whether this issuer would still renew a session with
// the given refresh token.
//
// It exists because revocation is otherwise unobservable from the client side:
// the endpoint answers 200 whether or not it found anything, so a test that
// only read the response could not tell a revocation that worked from one that
// was politely ignored.
func (i *Issuer) RefreshTokenLive(token string) bool {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	_, found := i.refreshTokens[token]
	return found
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
// mintAccessTokenFor mints access bound to one named resource, falling back to
// the registration's audience when the request named none. A deployment that
// takes a resource indicator binds the token to it and to nothing else, which
// is the whole reason the indicator is worth sending.
func (i *Issuer) mintAccessTokenFor(subject string, scopes []string, resource string) string {
	audience := i.opts.Audience
	if resource != "" {
		audience = resource
	}
	now := time.Now()
	claims := map[string]any{
		"iss":   i.URL,
		"sub":   subject,
		"aud":   audience,
		"scope": strings.Join(scopes, " "),
		"exp":   now.Add(5 * time.Minute).Unix(),
		"iat":   now.Unix(),
	}
	if i.opts.OrganizationClaim != "" {
		claims["org_id"] = i.opts.OrganizationClaim
	}
	token := i.sign(claims)
	i.mutex.Lock()
	i.accessTokens[token] = tokenRecord{
		scopes:   append([]string(nil), scopes...),
		audience: audience,
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
