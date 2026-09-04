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

package thunder

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
)

// ConsoleLogin names the seeded console client a fresh ThunderID ships with,
// which is the only client able to obtain a system-scoped token before this
// CLI has registered its own. Every member is a flag with these defaults.
type ConsoleLogin struct {
	Issuer         string
	ClientID       string // "CONSOLE"
	RedirectURI    string // "{issuer}/console"
	SystemResource string // "https://localhost:8090/mcp"
	Scope          string // "openid system"
	Username       string
	Password       string
}

// stepFailure says which step of the console login the deployment refused.
type stepFailure struct {
	step string
	err  error
}

func (s *stepFailure) Error() string { return s.step + ": " + s.err.Error() }
func (s *stepFailure) Unwrap() error { return s.err }

// SystemToken drives the console client's authorization-code flow through
// ThunderID's flow API and returns a bearer token carrying the system scope.
// It is the sequence a browser would perform on the console's sign-in page:
// authorize, submit the credentials to the flow engine, hand the resulting
// assertion back, and exchange the code.
func SystemToken(ctx context.Context, base *http.Client, login ConsoleLogin) (string, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return "", err
	}
	client := *base
	client.Jar = jar
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	issuer := strings.TrimRight(login.Issuer, "/")

	verifier, challenge, err := pkcePair()
	if err != nil {
		return "", err
	}
	query := url.Values{
		"response_type": {"code"}, "client_id": {login.ClientID}, "redirect_uri": {login.RedirectURI},
		"scope": {login.Scope}, "resource": {login.SystemResource}, "state": {"wso2-iam-bootstrap"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"},
	}
	location, err := redirectOf(ctx, &client, issuer+"/oauth2/authorize?"+query.Encode())
	if err != nil {
		return "", &stepFailure{"the authorization request", err}
	}
	authID := firstQuery(location, "authId", "auth_id")
	executionID := firstQuery(location, "executionId", "execution_id")
	if authID == "" || executionID == "" {
		return "", &stepFailure{"the authorization request",
			fmt.Errorf("the sign-in redirect names no authId and executionId: %s", location)}
	}

	inputs := map[string]string{"username": login.Username, "password": login.Password}
	var challenged struct {
		ChallengeToken string `json:"challengeToken"`
	}
	if err := postJSON(ctx, &client, issuer+"/flow/execute",
		map[string]any{"executionId": executionID, "inputs": inputs}, &challenged); err != nil {
		return "", &stepFailure{"submitting the credentials", err}
	}
	var asserted struct {
		Assertion string `json:"assertion"`
		Status    string `json:"flowStatus"`
		Reason    string `json:"failureReason"`
	}
	if err := postJSON(ctx, &client, issuer+"/flow/execute", map[string]any{
		"executionId": executionID, "challengeToken": challenged.ChallengeToken,
		"action": "action_001", "inputs": inputs,
	}, &asserted); err != nil {
		return "", &stepFailure{"confirming the credentials", err}
	}
	if asserted.Assertion == "" {
		reason := asserted.Reason
		if reason == "" {
			reason = "no assertion was issued"
		}
		return "", &stepFailure{"confirming the credentials", &Refusal{Status: http.StatusUnauthorized,
			Body: reason + "; the username or password is not accepted"}}
	}
	var callback struct {
		RedirectURI string `json:"redirect_uri"`
	}
	if err := postJSON(ctx, &client, issuer+"/oauth2/auth/callback",
		map[string]any{"authId": authID, "assertion": asserted.Assertion}, &callback); err != nil {
		return "", &stepFailure{"completing the authorization", err}
	}
	code := firstQuery(callback.RedirectURI, "code")
	if code == "" {
		return "", &stepFailure{"completing the authorization",
			fmt.Errorf("no code in the callback redirect: %s", callback.RedirectURI)}
	}

	form := url.Values{
		"grant_type": {"authorization_code"}, "client_id": {login.ClientID}, "code": {code},
		"redirect_uri": {login.RedirectURI}, "code_verifier": {verifier},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, issuer+"/oauth2/token",
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.Do(request)
	if err != nil {
		return "", &stepFailure{"the token request", err}
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode != http.StatusOK {
		return "", &stepFailure{"the token request", &Refusal{Status: response.StatusCode, Body: string(body)}}
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &token); err != nil || token.AccessToken == "" {
		return "", &stepFailure{"the token request", errors.New("no access token in the answer")}
	}
	return token.AccessToken, nil
}

// LoginProblem turns a SystemToken failure into the problem bootstrap returns.
func LoginProblem(err error) error {
	var step *stepFailure
	if errors.As(err, &step) {
		return Problem(step.err, step.step+" of the administrator login")
	}
	return Problem(err, "the administrator login")
}

func redirectOf(ctx context.Context, client *http.Client, target string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<16))
	if response.StatusCode < 300 || response.StatusCode > 399 {
		return "", &Refusal{Status: response.StatusCode, Body: string(body)}
	}
	return response.Header.Get("Location"), nil
}

func postJSON(ctx context.Context, client *http.Client, target string, body, into any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(string(encoded)))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return &Refusal{Status: response.StatusCode, Body: string(answer)}
	}
	if err := json.Unmarshal(answer, into); err != nil {
		return &unreadable{err: err}
	}
	return nil
}

// firstQuery returns the first named query member present in a URL.
func firstQuery(raw string, names ...string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	for _, name := range names {
		if value := parsed.Query().Get(name); value != "" {
			return value
		}
	}
	return ""
}

func pkcePair() (verifier, challenge string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}
