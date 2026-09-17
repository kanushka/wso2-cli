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

package fakeissuer_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	oidc "github.com/coreos/go-oidc/v3/oidc"

	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
)

// pkce is one prepared verifier/challenge pair.
type pkce struct{ verifier, challenge string }

func newPKCE(t *testing.T) pkce {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("random verifier: %v", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	return pkce{verifier: verifier, challenge: base64.RawURLEncoding.EncodeToString(sum[:])}
}

// authorize drives the auto-approving authorization endpoint and returns the
// code from the redirect it answers with.
func authorize(t *testing.T, issuer *fakeissuer.Issuer, challenge, scope, state string) string {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	query := url.Values{
		"response_type":         {"code"},
		"client_id":             {"client-123"},
		"redirect_uri":          {"http://127.0.0.1:10425/callback"},
		"scope":                 {scope},
		"state":                 {state},
		"nonce":                 {"nonce-1"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	response, err := client.Get(issuer.URL + "/authorize?" + query.Encode())
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusFound {
		t.Fatalf("authorize status = %d", response.StatusCode)
	}
	location, err := url.Parse(response.Header.Get("Location"))
	if err != nil {
		t.Fatalf("authorize redirect location: %v", err)
	}
	if got := location.Query().Get("state"); got != state {
		t.Fatalf("state echoed = %q, want %q", got, state)
	}
	code := location.Query().Get("code")
	if code == "" {
		t.Fatal("authorize issued no code")
	}
	return code
}

// token posts one grant to the token endpoint and returns the decoded body and
// HTTP status.
func token(t *testing.T, issuer *fakeissuer.Issuer, form url.Values) (map[string]any, int) {
	t.Helper()
	response, err := http.PostForm(issuer.URL+"/token", form)
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("token response decode: %v", err)
	}
	return body, response.StatusCode
}

func text(body map[string]any, key string) string {
	value, _ := body[key].(string)
	return value
}

func TestDiscoveryAdvertisesS256(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	response, err := http.Get(issuer.URL + "/.well-known/openid-configuration")
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	var document struct {
		Issuer                string   `json:"issuer"`
		AuthorizationEndpoint string   `json:"authorization_endpoint"`
		TokenEndpoint         string   `json:"token_endpoint"`
		JWKSURI               string   `json:"jwks_uri"`
		IntrospectionEndpoint string   `json:"introspection_endpoint"`
		CodeChallengeMethods  []string `json:"code_challenge_methods_supported"`
		GrantTypes            []string `json:"grant_types_supported"`
	}
	if err := json.NewDecoder(response.Body).Decode(&document); err != nil {
		t.Fatalf("discovery decode: %v", err)
	}
	if document.Issuer != issuer.URL {
		t.Fatalf("issuer = %q, want %q", document.Issuer, issuer.URL)
	}
	if !slices.Contains(document.CodeChallengeMethods, "S256") {
		t.Fatalf("S256 not advertised: %v", document.CodeChallengeMethods)
	}
	for _, grant := range []string{"authorization_code", "refresh_token", "client_credentials"} {
		if !slices.Contains(document.GrantTypes, grant) {
			t.Fatalf("grant %q not advertised: %v", grant, document.GrantTypes)
		}
	}
	if document.AuthorizationEndpoint == "" || document.TokenEndpoint == "" ||
		document.JWKSURI == "" || document.IntrospectionEndpoint == "" {
		t.Fatalf("discovery misses endpoints: %+v", document)
	}
}

func TestCodePKCEExchangeRoundTrips(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "reference-status"})
	proof := newPKCE(t)
	code := authorize(t, issuer, proof.challenge, "openid offline_access reference:status:read", "state 1&+/=")

	body, status := token(t, issuer, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {proof.verifier},
		"redirect_uri":  {"http://127.0.0.1:10425/callback"},
		"client_id":     {"client-123"},
	})
	if status != http.StatusOK {
		t.Fatalf("exchange status = %d, body %v", status, body)
	}
	accessToken := text(body, "access_token")
	if accessToken == "" || text(body, "refresh_token") == "" {
		t.Fatalf("exchange missing tokens: %v", body)
	}

	// The access token must verify against the issuer's own JWKS.
	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, issuer.URL)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	verified, err := provider.Verifier(&oidc.Config{ClientID: "reference-status"}).Verify(ctx, accessToken)
	if err != nil {
		t.Fatalf("access token does not verify against JWKS: %v", err)
	}
	if verified.Subject != "user-1" {
		t.Fatalf("subject = %q", verified.Subject)
	}
	var claims struct {
		Scope string `json:"scope"`
	}
	if err := verified.Claims(&claims); err != nil {
		t.Fatalf("claims: %v", err)
	}
	if !strings.Contains(claims.Scope, "reference:status:read") {
		t.Fatalf("scope claim = %q", claims.Scope)
	}

	// The ID token carries the interactive identity and echoes the nonce.
	idToken, err := provider.Verifier(&oidc.Config{ClientID: "client-123"}).Verify(ctx, text(body, "id_token"))
	if err != nil {
		t.Fatalf("id token does not verify: %v", err)
	}
	var identity struct {
		Email string `json:"email"`
		Nonce string `json:"nonce"`
	}
	if err := idToken.Claims(&identity); err != nil {
		t.Fatalf("id claims: %v", err)
	}
	if idToken.Subject != "user-1" || identity.Email != "dev@example.test" || identity.Nonce != "nonce-1" {
		t.Fatalf("identity claims: subject=%q email=%q nonce=%q", idToken.Subject, identity.Email, identity.Nonce)
	}
}

func TestWrongVerifierIsRejected(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	proof := newPKCE(t)
	code := authorize(t, issuer, proof.challenge, "openid", "state-1")
	body, status := token(t, issuer, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {"not-the-verifier-that-was-committed-to"},
		"redirect_uri":  {"http://127.0.0.1:10425/callback"},
		"client_id":     {"client-123"},
	})
	if status != http.StatusBadRequest || text(body, "error") != "invalid_grant" {
		t.Fatalf("wrong verifier accepted: status=%d body=%v", status, body)
	}
}

func TestCodeIsSingleUse(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	proof := newPKCE(t)
	code := authorize(t, issuer, proof.challenge, "openid", "state-1")
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {proof.verifier},
		"redirect_uri":  {"http://127.0.0.1:10425/callback"},
		"client_id":     {"client-123"},
	}
	if _, status := token(t, issuer, form); status != http.StatusOK {
		t.Fatalf("first exchange failed: %d", status)
	}
	if body, status := token(t, issuer, form); status != http.StatusBadRequest || text(body, "error") != "invalid_grant" {
		t.Fatalf("code was reusable: status=%d body=%v", status, body)
	}
}

func TestAuthorizeRejectsForeignRedirect(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	query := url.Values{
		"response_type":         {"code"},
		"client_id":             {"client-123"},
		"redirect_uri":          {"http://127.0.0.1:9999/callback"},
		"scope":                 {"openid"},
		"state":                 {"state-1"},
		"code_challenge":        {newPKCE(t).challenge},
		"code_challenge_method": {"S256"},
	}
	response, err := client.Get(issuer.URL + "/authorize?" + query.Encode())
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("foreign redirect accepted: %d", response.StatusCode)
	}
}

func refresh(t *testing.T, issuer *fakeissuer.Issuer, refreshToken, scope string) (map[string]any, int) {
	t.Helper()
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}, "client_id": {"client-123"}}
	if scope != "" {
		form.Set("scope", scope)
	}
	return token(t, issuer, form)
}

func TestRefreshHonorsNarrowerScope(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{RefreshScopeMode: "honor", Audience: "reference-status"})
	seeded := issuer.SeedSession([]string{"reference:status:read", "reference:logs:read"})
	body, status := refresh(t, issuer, seeded, "reference:status:read")
	if status != http.StatusOK {
		t.Fatalf("refresh status = %d, body %v", status, body)
	}
	if got := text(body, "scope"); got != "reference:status:read" {
		t.Fatalf("scope = %q, want the narrowed set", got)
	}
	active, scopes, _ := issuer.Introspect(t, text(body, "access_token"))
	if !active || !slices.Equal(scopes, []string{"reference:status:read"}) {
		t.Fatalf("issued token scopes = %v (active=%v)", scopes, active)
	}
}

func TestRefreshIgnoreModeReturnsOriginalScopes(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{RefreshScopeMode: "ignore"})
	seeded := issuer.SeedSession([]string{"reference:status:read", "reference:logs:read"})
	body, status := refresh(t, issuer, seeded, "reference:status:read")
	if status != http.StatusOK {
		t.Fatalf("refresh status = %d, body %v", status, body)
	}
	if got := text(body, "scope"); got != "reference:status:read reference:logs:read" {
		t.Fatalf("scope = %q, want the original full set", got)
	}
}

func TestRefreshRejectModeAnswersInvalidScope(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{RefreshScopeMode: "reject"})
	seeded := issuer.SeedSession([]string{"reference:status:read", "reference:logs:read"})
	body, status := refresh(t, issuer, seeded, "reference:status:read")
	if status != http.StatusBadRequest || text(body, "error") != "invalid_scope" {
		t.Fatalf("reject mode answered status=%d body=%v", status, body)
	}
}

func TestRefreshWithoutScopeKeepsOriginal(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{RefreshScopeMode: "honor"})
	seeded := issuer.SeedSession([]string{"reference:status:read", "reference:logs:read"})
	body, status := refresh(t, issuer, seeded, "")
	if status != http.StatusOK {
		t.Fatalf("refresh status = %d, body %v", status, body)
	}
	if got := text(body, "scope"); got != "reference:status:read reference:logs:read" {
		t.Fatalf("scope = %q, want the original set", got)
	}
}

func TestRefreshRotationInvalidatesPreviousToken(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{RotateRefreshTokens: true})
	seeded := issuer.SeedSession([]string{"reference:status:read"})
	body, status := refresh(t, issuer, seeded, "")
	if status != http.StatusOK {
		t.Fatalf("refresh status = %d, body %v", status, body)
	}
	rotated := text(body, "refresh_token")
	if rotated == "" || rotated == seeded {
		t.Fatalf("rotation did not issue a new refresh token: %v", body)
	}
	if replay, status := refresh(t, issuer, seeded, ""); status != http.StatusBadRequest || text(replay, "error") != "invalid_grant" {
		t.Fatalf("previous refresh token still valid: status=%d body=%v", status, replay)
	}
	if _, status := refresh(t, issuer, rotated, ""); status != http.StatusOK {
		t.Fatalf("rotated refresh token does not work: %d", status)
	}
}

func TestRefreshWithoutRotationKeepsToken(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	seeded := issuer.SeedSession([]string{"reference:status:read"})
	if _, status := refresh(t, issuer, seeded, ""); status != http.StatusOK {
		t.Fatalf("first refresh failed: %d", status)
	}
	if _, status := refresh(t, issuer, seeded, ""); status != http.StatusOK {
		t.Fatalf("refresh token was invalidated without rotation: %d", status)
	}
}

func TestRefreshScopeFieldCanBeOmitted(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{OmitRefreshScopeField: true})
	seeded := issuer.SeedSession([]string{"reference:status:read"})
	body, status := refresh(t, issuer, seeded, "")
	if status != http.StatusOK {
		t.Fatalf("refresh status = %d", status)
	}
	if _, present := body["scope"]; present {
		t.Fatalf("scope field present despite OmitRefreshScopeField: %v", body)
	}
}

func TestUnknownRefreshTokenIsInvalidGrant(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	body, status := refresh(t, issuer, "never-issued", "")
	if status != http.StatusBadRequest || text(body, "error") != "invalid_grant" {
		t.Fatalf("unknown refresh token answered status=%d body=%v", status, body)
	}
}

func TestClientCredentialsGrant(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "reference-status"})
	body, status := token(t, issuer, url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {"client-123"},
		"client_secret": {"secret-123"},
		"scope":         {"reference:status:read"},
	})
	if status != http.StatusOK {
		t.Fatalf("client credentials status = %d, body %v", status, body)
	}
	if text(body, "refresh_token") != "" {
		t.Fatal("client credentials grant issued a refresh token")
	}
	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, issuer.URL)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	verified, err := provider.Verifier(&oidc.Config{ClientID: "reference-status"}).Verify(ctx, text(body, "access_token"))
	if err != nil {
		t.Fatalf("access token does not verify: %v", err)
	}
	if verified.Subject != "client-1" {
		t.Fatalf("subject = %q, want client-1", verified.Subject)
	}
}

func TestClientCredentialsRequiresSecret(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	body, status := token(t, issuer, url.Values{
		"grant_type": {"client_credentials"},
		"client_id":  {"client-123"},
	})
	if status != http.StatusUnauthorized || text(body, "error") != "invalid_client" {
		t.Fatalf("secretless client accepted: status=%d body=%v", status, body)
	}
}

func TestClientCredentialsAcceptsBasicAuth(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	request, err := http.NewRequest(http.MethodPost, issuer.URL+"/token",
		strings.NewReader(url.Values{"grant_type": {"client_credentials"}, "scope": {"reference:status:read"}}.Encode()))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.SetBasicAuth("client-123", "secret-123")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("basic-authenticated grant status = %d", response.StatusCode)
	}
}

func TestIntrospectionReportsMintedTokens(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "reference-status"})
	proof := newPKCE(t)
	code := authorize(t, issuer, proof.challenge, "openid reference:status:read", "state-1")
	body, status := token(t, issuer, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {proof.verifier},
		"redirect_uri":  {"http://127.0.0.1:10425/callback"},
		"client_id":     {"client-123"},
	})
	if status != http.StatusOK {
		t.Fatalf("exchange status = %d", status)
	}
	active, scopes, audience := issuer.Introspect(t, text(body, "access_token"))
	if !active {
		t.Fatal("issuer-minted token reported inactive")
	}
	if !slices.Contains(scopes, "reference:status:read") {
		t.Fatalf("introspected scopes = %v", scopes)
	}
	if !slices.Contains(audience, "reference-status") {
		t.Fatalf("introspected audience = %v", audience)
	}
}

func TestIntrospectionReportsForeignTokensInactive(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	active, _, _ := issuer.Introspect(t, "eyJhbGciOiJSUzI1NiJ9.foreign.token")
	if active {
		t.Fatal("foreign token reported active")
	}
}

func TestAnAccessTokenCarriesAnOrganizationClaimOnlyWhenAsked(t *testing.T) {
	// Asgardeo mints no organization claim outside a sub-organization setup,
	// so the default has to be a token that carries none. A deployment that
	// does mint one is the other case a resource server must handle, and the
	// option is how a test reaches it.
	for name, configured := range map[string]string{
		"the deployment states an organization": "reference-org",
		"the deployment states none":            "",
	} {
		t.Run(name, func(t *testing.T) {
			issuer := fakeissuer.New(t, fakeissuer.Options{
				Audience: "reference-status", OrganizationClaim: configured,
			})
			seeded := issuer.SeedSession([]string{"reference:status:read"})

			body, status := refresh(t, issuer, seeded, "")
			if status != http.StatusOK {
				t.Fatalf("the refresh grant answered %d, want %d", status, http.StatusOK)
			}

			if got := claimFromToken(t, text(body, "access_token"), "org_id"); got != configured {
				t.Errorf("org_id = %q, want %q", got, configured)
			}
		})
	}
}

// claimFromToken reads one string claim out of a JWT payload without verifying
// it. This is a test reading a fixture's own output, not a security decision.
func claimFromToken(t *testing.T, token, claim string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("the token is not a three-part JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decoding the token payload: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("the token payload is not JSON: %v", err)
	}
	value, present := claims[claim]
	if !present {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		t.Fatalf("the %q claim is %T, want a string", claim, value)
	}
	return text
}

// deviceAuthorize starts one device authorization and returns its response.
func deviceAuthorize(t *testing.T, issuer *fakeissuer.Issuer) map[string]any {
	t.Helper()
	response, err := http.PostForm(issuer.URL+"/device_authorize",
		url.Values{"client_id": {"client-123"}, "scope": {"openid"}})
	if err != nil {
		t.Fatalf("device authorization request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("device authorization decode: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("device authorization answered status=%d body=%v", response.StatusCode, body)
	}
	return body
}

// pollDevice redeems a device code once.
func pollDevice(t *testing.T, issuer *fakeissuer.Issuer, deviceCode string) (map[string]any, int) {
	t.Helper()
	return token(t, issuer, url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {deviceCode},
		"client_id":   {"client-123"},
	})
}

func TestADeviceCodeIsSpentByTheApprovalItCarries(t *testing.T) {
	// The property the device tests read off this fixture is that a session
	// came from one approval. An issuer that answered a replayed device code
	// would satisfy a shell that redeemed the same approval twice, so the
	// second redemption is refused here exactly as a deployment refuses it.
	issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "reference-status"})
	authorization := deviceAuthorize(t, issuer)
	deviceCode := text(authorization, "device_code")

	granted, status := pollDevice(t, issuer, deviceCode)
	if status != http.StatusOK || text(granted, "access_token") == "" {
		t.Fatalf("the first redemption did not issue tokens: status=%d body=%v", status, granted)
	}

	replayed, status := pollDevice(t, issuer, deviceCode)
	if status != http.StatusBadRequest || text(replayed, "error") != "invalid_grant" {
		t.Fatalf("a spent device code was redeemed a second time: status=%d body=%v",
			status, replayed)
	}
}

func TestAnUnknownDeviceCodeIsInvalidGrant(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	body, status := pollDevice(t, issuer, "never-issued")
	if status != http.StatusBadRequest || text(body, "error") != "invalid_grant" {
		t.Fatalf("unknown device code answered status=%d body=%v", status, body)
	}
}

// TestHTTPClientReachesTheIssuer pins that HTTPClient hands back a client
// that can actually talk to this issuer's loopback server.
func TestHTTPClientReachesTheIssuer(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	response, err := issuer.HTTPClient().Get(issuer.URL + "/.well-known/openid-configuration")
	if err != nil {
		t.Fatalf("HTTPClient().Get returned %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("discovery via HTTPClient answered status = %d", response.StatusCode)
	}
}

// TestSeedSessionForBindsTheResourceARenewalCarriesForward pins the seam
// SeedSessionFor exists for: the resource a refresh renews under must be the
// one the session was seeded against, not the registration's default.
func TestSeedSessionForBindsTheResourceARenewalCarriesForward(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	seeded := issuer.SeedSessionFor([]string{"reference:status:read"}, "https://resource.example")

	body, status := refresh(t, issuer, seeded, "")
	if status != http.StatusOK {
		t.Fatalf("refresh status = %d, want 200", status)
	}
	if got := claimFromToken(t, text(body, "access_token"), "aud"); got != "https://resource.example" {
		t.Errorf("aud = %q, want the resource the session was seeded for", got)
	}
}

// TestNegativeSerialCertificatePublishesAnUnparseableChain pins the JWKS
// x5c branch: the certificate chain must be present and its DER must not
// parse, which is the whole point of the fixture.
func TestNegativeSerialCertificatePublishesAnUnparseableChain(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{NegativeSerialCertificate: true})
	response, err := http.Get(issuer.URL + "/jwks")
	if err != nil {
		t.Fatalf("jwks: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	var document struct {
		Keys []struct {
			X5C []string `json:"x5c"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(response.Body).Decode(&document); err != nil {
		t.Fatalf("decoding jwks: %v", err)
	}
	if len(document.Keys) != 1 || len(document.Keys[0].X5C) != 1 {
		t.Fatalf("jwks keys = %+v, want exactly one key carrying one certificate", document.Keys)
	}
	raw, err := base64.StdEncoding.DecodeString(document.Keys[0].X5C[0])
	if err != nil {
		t.Fatalf("decoding the published certificate: %v", err)
	}
	if _, err := x509.ParseCertificate(raw); err == nil {
		t.Fatal("the published certificate parsed, want the negative-serial encoding to be rejected")
	}
}

// TestOnRevokeRunsOnceBeforeTheRevocationAnswers pins the one race this hook
// exists for: it fires inside the revocation request, and only the first
// time.
func TestOnRevokeRunsOnceBeforeTheRevocationAnswers(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	seeded := issuer.SeedSession(nil)
	calls := 0
	issuer.OnRevoke(func() { calls++ })

	revoke(t, issuer, seeded, "client-123")
	revoke(t, issuer, "some-other-token", "client-123")

	if calls != 1 {
		t.Errorf("OnRevoke fired %d times, want exactly 1", calls)
	}
}

// revoke posts one revocation request and returns its status.
func revoke(t *testing.T, issuer *fakeissuer.Issuer, refreshToken, clientID string) int {
	t.Helper()
	response, err := http.PostForm(issuer.URL+"/revoke",
		url.Values{"token": {refreshToken}, "client_id": {clientID}})
	if err != nil {
		t.Fatalf("revoke request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	return response.StatusCode
}

// TestHandleRevokeRetractsAKnownTokenAndIgnoresAnUnknownOne pins RFC 7009's
// instruction that a client cannot tell a token that was already dead from
// one it just killed: both answer 200, but only the known one stops
// RefreshTokenLive reporting it live.
func TestHandleRevokeRetractsAKnownTokenAndIgnoresAnUnknownOne(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	seeded := issuer.SeedSession(nil)
	if !issuer.RefreshTokenLive(seeded) {
		t.Fatal("a freshly seeded token reports as not live")
	}

	if status := revoke(t, issuer, seeded, "client-123"); status != http.StatusOK {
		t.Fatalf("revoking a known token answered %d, want 200", status)
	}
	if issuer.RefreshTokenLive(seeded) {
		t.Error("the revoked token still reports as live")
	}

	if status := revoke(t, issuer, "never-issued", "client-123"); status != http.StatusOK {
		t.Fatalf("revoking an unknown token answered %d, want 200", status)
	}
}

// TestHandleRevokeRefusesARequestNamingNoClient and
// TestHandleRevokeRefusesARequestNamingNoToken pin the two malformed-request
// refusals RFC 7009 leaves to the deployment.
func TestHandleRevokeRefusesARequestNamingNoClient(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	if status := revoke(t, issuer, "some-token", ""); status != http.StatusBadRequest {
		t.Fatalf("revoke with no client answered %d, want 400", status)
	}
}

func TestHandleRevokeRefusesARequestNamingNoToken(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	if status := revoke(t, issuer, "", "client-123"); status != http.StatusBadRequest {
		t.Fatalf("revoke with no token answered %d, want 400", status)
	}
}

// TestRefuseRevocationDeclinesEveryRequest pins the deployment-disagrees
// case: the endpoint is advertised but answers invalid_request regardless
// of what it is asked to retract.
func TestRefuseRevocationDeclinesEveryRequest(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{RefuseRevocation: true})
	seeded := issuer.SeedSession(nil)
	if status := revoke(t, issuer, seeded, "client-123"); status != http.StatusBadRequest {
		t.Fatalf("a refusing issuer answered %d, want 400", status)
	}
	if !issuer.RefreshTokenLive(seeded) {
		t.Error("a refused revocation retracted the token anyway")
	}
}

// TestOmitRevocationEndpointServesNeitherDiscoveryNorTheEndpoint pins that a
// deployment that does not advertise revocation does not serve it either.
func TestOmitRevocationEndpointServesNeitherDiscoveryNorTheEndpoint(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{OmitRevocationEndpoint: true})
	response, err := http.Get(issuer.URL + "/.well-known/openid-configuration")
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	var document map[string]any
	if err := json.NewDecoder(response.Body).Decode(&document); err != nil {
		t.Fatalf("decoding discovery: %v", err)
	}
	if _, present := document["revocation_endpoint"]; present {
		t.Error("discovery still advertises revocation_endpoint")
	}

	seeded := issuer.SeedSession(nil)
	if status := revoke(t, issuer, seeded, "client-123"); status != http.StatusNotFound {
		t.Fatalf("revoke on an unadvertised endpoint answered %d, want 404", status)
	}
}

// TestMintAccessTokenIsIntrospectable pins that a directly minted token is
// exactly as introspectable as one a real grant issued.
func TestMintAccessTokenIsIntrospectable(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "reference-status"})
	minted := issuer.MintAccessToken([]string{"reference:status:read"}, "")

	active, scopes, audience := issuer.Introspect(t, minted)
	if !active {
		t.Fatal("a directly minted token reports inactive")
	}
	if !slices.Contains(scopes, "reference:status:read") {
		t.Errorf("scopes = %v, want reference:status:read", scopes)
	}
	if !slices.Contains(audience, "reference-status") {
		t.Errorf("audience = %v, want reference-status", audience)
	}
}

// TestLogoutRequestsRecordsWhatEachEndSessionRequestCarried pins the
// end-session endpoint: it answers a page and records the query it was
// given, in order, so a test can see what a logout told the deployment.
func TestLogoutRequestsRecordsWhatEachEndSessionRequestCarried(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	response, err := http.Get(issuer.URL + "/logout?id_token_hint=abc&post_logout_redirect_uri=https://app.example")
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("logout status = %d, want 200", response.StatusCode)
	}

	requests := issuer.LogoutRequests()
	if len(requests) != 1 {
		t.Fatalf("LogoutRequests = %v, want exactly one recorded request", requests)
	}
	if requests[0].Get("id_token_hint") != "abc" {
		t.Errorf("id_token_hint = %q, want abc", requests[0].Get("id_token_hint"))
	}
	if requests[0].Get("post_logout_redirect_uri") != "https://app.example" {
		t.Errorf("post_logout_redirect_uri = %q, want https://app.example",
			requests[0].Get("post_logout_redirect_uri"))
	}
}

// TestDevicePollsRecordsEveryPollInOrder pins that DevicePolls times every
// poll of the device grant, including ones that end the flow, and that
// LastDeviceCode names the most recently minted device code.
func TestDevicePollsRecordsEveryPollInOrder(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "reference-status"})
	authorization := deviceAuthorize(t, issuer)
	deviceCode := text(authorization, "device_code")

	if got := issuer.LastDeviceCode(); got != deviceCode {
		t.Fatalf("LastDeviceCode = %q, want %q", got, deviceCode)
	}

	if _, status := pollDevice(t, issuer, deviceCode); status != http.StatusOK {
		t.Fatalf("the poll did not succeed")
	}

	polls := issuer.DevicePolls()
	if len(polls) != 1 {
		t.Fatalf("DevicePolls = %v, want exactly one recorded poll", polls)
	}
}

// TestExchangeGrantBindsTheAudienceToTheResourceIndicator pins RFC 8693 as
// this fixture models it: the resource indicator decides the audience, no
// refresh token comes back, and the subject token's own permissions do not
// cross into what is issued.
func TestExchangeGrantBindsTheAudienceToTheResourceIndicator(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{ExchangeGrant: true, Audience: "reference-status"})
	subject := issuer.MintAccessToken([]string{"reference:status:read"}, "")

	body, status := token(t, issuer, url.Values{
		"grant_type":    {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"subject_token": {subject},
		"resource":      {"https://exchanged.example"},
		"client_id":     {"client-123"},
	})
	if status != http.StatusOK {
		t.Fatalf("exchange status = %d, body %v", status, body)
	}
	if text(body, "refresh_token") != "" {
		t.Error("the exchange grant issued a refresh token, want none")
	}
	if got := claimFromToken(t, text(body, "access_token"), "aud"); got != "https://exchanged.example" {
		t.Errorf("aud = %q, want the requested resource", got)
	}
	if body["scope"] != "openid" {
		t.Errorf("scope = %v, want only openid: the subject token's own permissions must not cross", body["scope"])
	}
}

// TestExchangeGrantRefusesWhenNotRegistered and
// TestExchangeGrantRefusesAnUnmintedSubjectToken pin exchangeGrant's two
// refusals: a deployment that never registered the grant, and an assertion
// this issuer never minted.
func TestExchangeGrantRefusesWhenNotRegistered(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	body, status := token(t, issuer, url.Values{
		"grant_type":    {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"subject_token": {"whatever"},
		"client_id":     {"client-123"},
	})
	if status != http.StatusBadRequest || text(body, "error") != "unauthorized_client" {
		t.Fatalf("unregistered exchange answered status=%d body=%v", status, body)
	}
}

func TestExchangeGrantRefusesAnUnmintedSubjectToken(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{ExchangeGrant: true})
	body, status := token(t, issuer, url.Values{
		"grant_type":    {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"subject_token": {"never-minted"},
		"client_id":     {"client-123"},
	})
	if status != http.StatusBadRequest || text(body, "error") != "invalid_grant" {
		t.Fatalf("exchange of an unminted subject token answered status=%d body=%v", status, body)
	}
}

// TestExchangeGrantRefusesARequestNamingNoClient pins that the exchange
// grant, like the others, demands a client identifier.
func TestExchangeGrantRefusesARequestNamingNoClient(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{ExchangeGrant: true})
	subject := issuer.MintAccessToken(nil, "")
	body, status := token(t, issuer, url.Values{
		"grant_type":    {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"subject_token": {subject},
	})
	if status != http.StatusUnauthorized || text(body, "error") != "invalid_client" {
		t.Fatalf("clientless exchange answered status=%d body=%v", status, body)
	}
}

// TestRefuseExchangeDeclinesEveryLaterExchange pins RefuseExchange: it flips
// a registered grant to answer unauthorized_client, the way an issuer
// answers a client the grant is not registered on.
func TestRefuseExchangeDeclinesEveryLaterExchange(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{ExchangeGrant: true})
	subject := issuer.MintAccessToken(nil, "")
	issuer.RefuseExchange()

	body, status := token(t, issuer, url.Values{
		"grant_type":    {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"subject_token": {subject},
		"client_id":     {"client-123"},
	})
	if status != http.StatusBadRequest || text(body, "error") != "unauthorized_client" {
		t.Fatalf("a refused exchange answered status=%d body=%v", status, body)
	}
}

// TestBearerGrantAcceptsAnAssertionFromTheTrustedIssuer and its refusal
// siblings pin RFC 7523 as this fixture models it: the assertion must be an
// identity token the trusted issuer minted for the registered alias.
func TestBearerGrantAcceptsAnAssertionFromTheTrustedIssuer(t *testing.T) {
	trusted := fakeissuer.New(t, fakeissuer.Options{})
	proof := newPKCE(t)
	code := authorize(t, trusted, proof.challenge, "openid", "state-1")
	body, status := token(t, trusted, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {proof.verifier},
		"redirect_uri":  {"http://127.0.0.1:10425/callback"},
		"client_id":     {"client-123"},
	})
	if status != http.StatusOK {
		t.Fatalf("minting the assertion returned status=%d body=%v", status, body)
	}
	assertion := text(body, "id_token")

	relying := fakeissuer.New(t, fakeissuer.Options{
		TrustedIssuer: trusted, BearerAlias: "client-123",
	})
	bearerBody, status := token(t, relying, url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {assertion},
		"scope":      {"reference:status:read"},
		"client_id":  {"client-123"},
	})
	if status != http.StatusOK {
		t.Fatalf("the bearer grant answered status=%d body=%v", status, bearerBody)
	}
	if text(bearerBody, "access_token") == "" {
		t.Error("the bearer grant issued no access token")
	}
	if bearerBody["scope"] != "reference:status:read" {
		t.Errorf("scope = %v, want the requested scope honored", bearerBody["scope"])
	}
}

func TestBearerGrantRefusesWhenNoIssuerIsTrusted(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	body, status := token(t, issuer, url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {"whatever"},
		"client_id":  {"client-123"},
	})
	if status != http.StatusBadRequest || text(body, "error") != "unsupported_grant_type" {
		t.Fatalf("bearer grant with no trust configured answered status=%d body=%v", status, body)
	}
}

func TestBearerGrantRefusesAnAssertionForTheWrongAlias(t *testing.T) {
	trusted := fakeissuer.New(t, fakeissuer.Options{})
	proof := newPKCE(t)
	code := authorize(t, trusted, proof.challenge, "openid", "state-1")
	body, status := token(t, trusted, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {proof.verifier},
		"redirect_uri":  {"http://127.0.0.1:10425/callback"},
		"client_id":     {"client-123"},
	})
	if status != http.StatusOK {
		t.Fatalf("minting the assertion returned status=%d body=%v", status, body)
	}
	assertion := text(body, "id_token")

	relying := fakeissuer.New(t, fakeissuer.Options{
		TrustedIssuer: trusted, BearerAlias: "trusted-alias",
	})
	bearerBody, status := token(t, relying, url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {assertion},
		"client_id":  {"client-123"},
	})
	if status != http.StatusBadRequest || text(bearerBody, "error") != "invalid_grant" {
		t.Fatalf("an assertion for the wrong alias answered status=%d body=%v", status, bearerBody)
	}
}

func TestBearerGrantRefusesARequestNamingNoClient(t *testing.T) {
	trusted := fakeissuer.New(t, fakeissuer.Options{})
	relying := fakeissuer.New(t, fakeissuer.Options{TrustedIssuer: trusted, BearerAlias: "trusted-alias"})
	body, status := token(t, relying, url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {"whatever"},
	})
	if status != http.StatusUnauthorized || text(body, "error") != "invalid_client" {
		t.Fatalf("clientless bearer grant answered status=%d body=%v", status, body)
	}
}

// TestBearerScopeModeDefaultIssuesOnlyTheDefaultScope and
// TestBearerScopeModePartialDropsTheLastScope pin what BearerScopeMode does
// once the assertion is accepted.
func TestBearerScopeModeDefaultIssuesOnlyTheDefaultScope(t *testing.T) {
	trusted := fakeissuer.New(t, fakeissuer.Options{})
	proof := newPKCE(t)
	code := authorize(t, trusted, proof.challenge, "openid", "state-1")
	minted, status := token(t, trusted, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {proof.verifier},
		"redirect_uri":  {"http://127.0.0.1:10425/callback"},
		"client_id":     {"client-123"},
	})
	if status != http.StatusOK {
		t.Fatalf("minting the assertion returned status=%d body=%v", status, minted)
	}

	relying := fakeissuer.New(t, fakeissuer.Options{
		TrustedIssuer: trusted, BearerAlias: "client-123", BearerScopeMode: "default",
	})
	body, status := token(t, relying, url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {text(minted, "id_token")},
		"scope":      {"reference:status:read reference:status:write"},
		"client_id":  {"client-123"},
	})
	if status != http.StatusOK {
		t.Fatalf("the bearer grant answered status=%d body=%v", status, body)
	}
	if body["scope"] != "default" {
		t.Errorf("scope = %v, want only default", body["scope"])
	}
}

// TestRefreshTokenExpiresInCanBeSentAsAString pins the non-standard shape
// RefreshTokenExpiresInAsString asks for.
func TestRefreshTokenExpiresInCanBeSentAsAString(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{
		RefreshTokenExpiresIn: 3600, RefreshTokenExpiresInAsString: true,
	})
	proof := newPKCE(t)
	code := authorize(t, issuer, proof.challenge, "openid", "state-1")
	body, status := token(t, issuer, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {proof.verifier},
		"redirect_uri":  {"http://127.0.0.1:10425/callback"},
		"client_id":     {"client-123"},
	})
	if status != http.StatusOK {
		t.Fatalf("exchange status = %d", status)
	}
	if got, ok := body["refresh_token_expires_in"].(string); !ok || got != "3600" {
		t.Errorf("refresh_token_expires_in = %v (%T), want the string \"3600\"",
			body["refresh_token_expires_in"], body["refresh_token_expires_in"])
	}
}

// TestOmitDeviceEndpointServesNeitherDiscoveryNorTheEndpoint pins that a
// deployment without the device grant advertises neither the endpoint nor
// the grant type, and serves neither the authorization nor the poll.
func TestOmitDeviceEndpointServesNeitherDiscoveryNorTheEndpoint(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{OmitDeviceEndpoint: true})
	response, err := http.Get(issuer.URL + "/.well-known/openid-configuration")
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	var document struct {
		DeviceAuthorizationEndpoint string   `json:"device_authorization_endpoint"`
		GrantTypesSupported         []string `json:"grant_types_supported"`
	}
	if err := json.NewDecoder(response.Body).Decode(&document); err != nil {
		t.Fatalf("decoding discovery: %v", err)
	}
	if document.DeviceAuthorizationEndpoint != "" {
		t.Error("discovery still advertises device_authorization_endpoint")
	}
	if slices.Contains(document.GrantTypesSupported, "urn:ietf:params:oauth:grant-type:device_code") {
		t.Error("discovery still advertises the device grant type")
	}

	authResponse, err := http.PostForm(issuer.URL+"/device_authorize",
		url.Values{"client_id": {"client-123"}})
	if err != nil {
		t.Fatalf("device authorize request: %v", err)
	}
	defer func() { _ = authResponse.Body.Close() }()
	if authResponse.StatusCode != http.StatusNotFound {
		t.Errorf("device authorize on an unadvertised endpoint answered %d, want 404", authResponse.StatusCode)
	}
}

// TestDeviceAuthorizeRefusesARequestNamingNoClient pins the one refusal
// handleDeviceAuthorize itself can produce.
func TestDeviceAuthorizeRefusesARequestNamingNoClient(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	response, err := http.PostForm(issuer.URL+"/device_authorize", url.Values{})
	if err != nil {
		t.Fatalf("device authorize request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("clientless device authorization answered %d, want 400", response.StatusCode)
	}
}

// TestAuthorizeRejectsAMissingClientID and
// TestAuthorizeRejectsAMissingCodeChallenge pin handleAuthorize's other
// refusals, beyond the foreign-redirect one already pinned elsewhere.
func TestAuthorizeRejectsAMissingClientID(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	response, err := http.Get(issuer.URL + "/authorize?redirect_uri=http://127.0.0.1:10425/callback")
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("clientless authorize answered %d, want 400", response.StatusCode)
	}
}

func TestAuthorizeRejectsAMissingCodeChallenge(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	query := url.Values{
		"client_id":    {"client-123"},
		"redirect_uri": {"http://127.0.0.1:10425/callback"},
	}
	response, err := http.Get(issuer.URL + "/authorize?" + query.Encode())
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("authorize with no code challenge answered %d, want 400", response.StatusCode)
	}
}

// TestRequireResourceRefusesAuthorizationWithoutOne and
// TestRegisteredResourceRefusesAnUnregisteredOne pin the resource-indicator
// refusals, which are redirected rather than served directly.
func TestRequireResourceRefusesAuthorizationWithoutOne(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{RequireResource: true})
	proof := newPKCE(t)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	query := url.Values{
		"client_id":             {"client-123"},
		"redirect_uri":          {"http://127.0.0.1:10425/callback"},
		"code_challenge":        {proof.challenge},
		"code_challenge_method": {"S256"},
		"state":                 {"state-1"},
	}
	response, err := client.Get(issuer.URL + "/authorize?" + query.Encode())
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusFound {
		t.Fatalf("authorize status = %d, want a redirect carrying the refusal", response.StatusCode)
	}
	location, err := url.Parse(response.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parsing the redirect location: %v", err)
	}
	if location.Query().Get("error") != "invalid_target" {
		t.Errorf("error = %q, want invalid_target", location.Query().Get("error"))
	}
}

func TestRegisteredResourceRefusesAnUnregisteredOne(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{RegisteredResource: "https://known.example"})
	proof := newPKCE(t)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	query := url.Values{
		"client_id":             {"client-123"},
		"redirect_uri":          {"http://127.0.0.1:10425/callback"},
		"code_challenge":        {proof.challenge},
		"code_challenge_method": {"S256"},
		"state":                 {"state-1"},
		"resource":              {"https://unknown.example"},
	}
	response, err := client.Get(issuer.URL + "/authorize?" + query.Encode())
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusFound {
		t.Fatalf("authorize status = %d, want a redirect carrying the refusal", response.StatusCode)
	}
	location, err := url.Parse(response.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parsing the redirect location: %v", err)
	}
	if location.Query().Get("error") != "invalid_target" {
		t.Errorf("error = %q, want invalid_target", location.Query().Get("error"))
	}
}

// TestClientCredentialsRegisteredResourceRefusesAnUnregisteredOne pins the
// same registration check on the client-credentials grant, which has no
// earlier authorization to inherit a binding from.
func TestClientCredentialsRegisteredResourceRefusesAnUnregisteredOne(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{RegisteredResource: "https://known.example"})
	body, status := token(t, issuer, url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {"client-123"},
		"client_secret": {"secret-123"},
		"resource":      {"https://unknown.example"},
	})
	if status != http.StatusBadRequest || text(body, "error") != "invalid_target" {
		t.Fatalf("an unregistered resource answered status=%d body=%v", status, body)
	}
}

// TestClientCredentialsRequireResourceRefusesWithoutOne pins the sibling
// refusal on the client-credentials grant.
func TestClientCredentialsRequireResourceRefusesWithoutOne(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{RequireResource: true})
	body, status := token(t, issuer, url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {"client-123"},
		"client_secret": {"secret-123"},
	})
	if status != http.StatusBadRequest || text(body, "error") != "invalid_target" {
		t.Fatalf("a required-but-missing resource answered status=%d body=%v", status, body)
	}
}

// TestClientCredentialsIgnoreModeIssuesTheRegisteredScopes pins the "ignore"
// branch of ClientScopeMode: the deployment hands back the registered
// client's whole authority, whatever the request narrowed itself to.
func TestClientCredentialsIgnoreModeIssuesTheRegisteredScopes(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{
		ClientScopeMode: "ignore", ClientScopes: []string{"reference:status:read", "reference:status:write"},
	})
	body, status := token(t, issuer, url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {"client-123"},
		"client_secret": {"secret-123"},
		"scope":         {"reference:status:read"},
	})
	if status != http.StatusOK {
		t.Fatalf("client credentials status = %d, body %v", status, body)
	}
	if body["scope"] != "reference:status:read reference:status:write" {
		t.Errorf("scope = %v, want the client's whole registered authority", body["scope"])
	}
}

// TestClientCredentialsRejectModeAnswersInvalidScope pins the third mode.
func TestClientCredentialsRejectModeAnswersInvalidScope(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{ClientScopeMode: "reject"})
	body, status := token(t, issuer, url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {"client-123"},
		"client_secret": {"secret-123"},
		"scope":         {"reference:status:read"},
	})
	if status != http.StatusBadRequest || text(body, "error") != "invalid_scope" {
		t.Fatalf("reject mode answered status=%d body=%v", status, body)
	}
}

// TestUnsupportedGrantTypeIsRefused pins handleToken's default branch.
func TestUnsupportedGrantTypeIsRefused(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	body, status := token(t, issuer, url.Values{"grant_type": {"not-a-real-grant"}})
	if status != http.StatusBadRequest || text(body, "error") != "unsupported_grant_type" {
		t.Fatalf("unsupported grant answered status=%d body=%v", status, body)
	}
}

// TestATokenRequestWithAMalformedBodyIsRefused pins the ParseForm failure
// path on the token endpoint.
func TestATokenRequestWithAMalformedBodyIsRefused(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	request, err := http.NewRequest(http.MethodPost, issuer.URL+"/token",
		strings.NewReader("%zz"))
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("a malformed token request answered %d, want 400", response.StatusCode)
	}
}
