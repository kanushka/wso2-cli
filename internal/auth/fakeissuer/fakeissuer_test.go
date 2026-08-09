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
