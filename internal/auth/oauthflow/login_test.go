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

package oauthflow_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/oauthflow"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// recorder is an output stream a test can read while the login under test is
// still writing to it, which is how a test plays the user who copies the
// printed URL into a browser.
type recorder struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (r *recorder) Write(data []byte) (int, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return r.buffer.Write(data)
}

func (r *recorder) String() string {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return r.buffer.String()
}

// authorizationURL returns the URL the login printed, waiting for it to appear.
func (r *recorder) authorizationURL(t *testing.T) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, line := range strings.Split(r.String(), "\n") {
			if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
				return line
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the login never printed an authorization URL, printed:\n%s", r.String())
	return ""
}

func testContext(t *testing.T, timeout time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	return ctx
}

// visit plays the browser: it follows the authorization URL, which the fake
// issuer auto-approves and redirects back to the shell's loopback listener.
func visit(issuer *fakeissuer.Issuer, target string) {
	response, err := issuer.HTTPClient().Get(target)
	if err == nil {
		_ = response.Body.Close()
	}
}

// interceptCode drives the authorization endpoint without following its
// redirect, so a test holds a genuine authorization code the shell has not
// seen. It is how a forged callback carries a real code and a wrong state.
func interceptCode(t *testing.T, issuer *fakeissuer.Issuer, authURL string) (code, callbackURL string) {
	t.Helper()
	client := *issuer.HTTPClient()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Get(authURL)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	location, err := response.Location()
	if err != nil {
		t.Fatalf("authorize did not redirect: %v", err)
	}
	redirect := *location
	redirect.RawQuery = ""
	return location.Query().Get("code"), redirect.String()
}

// requireProblem asserts the error is a typed problem with the given code in
// the authentication policy class, carrying recovery guidance.
func requireProblem(t *testing.T, err error, code string) problem.Problem {
	t.Helper()
	var typed problem.Problem
	if !errors.As(err, &typed) {
		t.Fatalf("expected a typed problem, got %v", err)
	}
	if typed.Code != code {
		t.Fatalf("expected problem %q, got %q (%s)", code, typed.Code, typed.Message)
	}
	if typed.Category != problem.CategoryAuthPolicy {
		t.Fatalf("problem %q is in category %q, not the authentication policy class", typed.Code, typed.Category)
	}
	if typed.Recovery == "" {
		t.Fatalf("problem %q states no recovery", typed.Code)
	}
	return typed
}

func browserLogin(issuer *fakeissuer.Issuer, out *recorder, open func(string) error) oauthflow.Login {
	return oauthflow.Login{
		Issuer:      issuer.URL,
		ClientID:    "client-123",
		Scopes:      []string{"reference:status:read"},
		HTTPClient:  issuer.HTTPClient(),
		Out:         out,
		Ports:       []int{0},
		OpenBrowser: open,
	}
}

func TestBrowserLoginRoundTrip(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "reference-status", AllowAnyLoopbackPort: true})
	printed := &recorder{}
	login := browserLogin(issuer, printed, func(authURL string) error {
		go visit(issuer, authURL)
		return nil
	})

	result, err := login.Run(testContext(t, 30*time.Second))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if result.Token == nil || result.Token.RefreshToken == "" {
		t.Fatal("no refresh token issued")
	}
	if result.Token.AccessToken == "" || result.Token.Expiry.IsZero() {
		t.Fatalf("the login produced no dated access token: %+v", result.Token)
	}
	if result.Subject != "user-1" || result.Email != "dev@example.test" {
		t.Fatalf("identity claims: subject %q email %q", result.Subject, result.Email)
	}
	if !strings.Contains(printed.String(), issuer.URL+"/authorize") {
		t.Fatalf("the authorization URL was not printed:\n%s", printed.String())
	}
	for _, secret := range []string{result.Token.RefreshToken, result.Token.AccessToken} {
		if strings.Contains(printed.String(), secret) {
			t.Fatalf("token material leaked into the login output:\n%s", printed.String())
		}
	}
}

// TestLoginVerifiesThroughACertificateItCannotParse proves a key set is read
// for the keys in it and not for the certificates published beside them.
//
// WSO2 deployments — Asgardeo tenants and Identity Servers alike — publish
// signing certificates whose serial numbers are negative, which RFC 5280
// forbids and which Go's x509 parser has rejected since 1.23. go-jose parses
// x5c eagerly while unmarshalling a key set and fails the whole document when
// one certificate in it does not parse, so such a deployment leaves the shell
// with no readable keys and no login is possible at all. The signing key was
// never the problem: n and e describe it completely.
func TestLoginVerifiesThroughACertificateItCannotParse(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{
		Audience:                  "reference-status",
		AllowAnyLoopbackPort:      true,
		NegativeSerialCertificate: true,
	})
	printed := &recorder{}
	login := browserLogin(issuer, printed, func(authURL string) error {
		go visit(issuer, authURL)
		return nil
	})

	result, err := login.Run(testContext(t, 30*time.Second))
	if err != nil {
		t.Fatalf("login refused an issuer whose signing keys are perfectly readable: %v", err)
	}
	if result.Token == nil || result.Token.RefreshToken == "" {
		t.Fatal("no refresh token issued")
	}
	if result.Subject != "user-1" {
		t.Fatalf("identity subject %q, want user-1", result.Subject)
	}
}

func TestLoginCompletesFromThePrintedURLWhenTheBrowserCannotOpen(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "reference-status", AllowAnyLoopbackPort: true})
	printed := &recorder{}
	login := browserLogin(issuer, printed, func(string) error {
		return errors.New("this machine has no browser to open")
	})

	type outcome struct {
		result oauthflow.Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := login.Run(testContext(t, 30*time.Second))
		done <- outcome{result, err}
	}()

	// The user copies the printed URL into a browser on another machine.
	visit(issuer, printed.authorizationURL(t))

	got := <-done
	if got.err != nil {
		t.Fatalf("login driven from the printed URL: %v", got.err)
	}
	if got.result.Token == nil || got.result.Token.RefreshToken == "" {
		t.Fatal("the login completed without a refresh token")
	}
}

func TestStateMismatchDoesNotCompleteTheLogin(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "reference-status", AllowAnyLoopbackPort: true})
	printed := &recorder{}
	login := browserLogin(issuer, printed, func(authURL string) error {
		go func() {
			// A genuine code delivered under another request's state: were the
			// state unchecked, this would log the shell in.
			code, callbackURL := interceptCode(t, issuer, authURL)
			visit(issuer, callbackURL+"?"+url.Values{
				"code": {code}, "state": {"not-the-state-this-login-sent"},
			}.Encode())
		}()
		return nil
	})

	result, err := login.Run(testContext(t, 2*time.Second))
	if err == nil {
		t.Fatal("a callback carrying the wrong state completed the login")
	}
	if result.Token != nil {
		t.Fatal("a callback carrying the wrong state produced a token")
	}
	_ = requireProblem(t, err, "auth.credential_unavailable")
}

func TestLoginSurvivesAStateMismatchAndCompletes(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "reference-status", AllowAnyLoopbackPort: true})
	printed := &recorder{}
	login := browserLogin(issuer, printed, func(authURL string) error {
		go func() {
			code, callbackURL := interceptCode(t, issuer, authURL)
			forged := url.Values{"code": {code}, "state": {"not-the-state-this-login-sent"}}
			visit(issuer, callbackURL+"?"+forged.Encode())

			authorization, err := url.Parse(authURL)
			if err != nil {
				return
			}
			honest := url.Values{"code": {code}, "state": {authorization.Query().Get("state")}}
			visit(issuer, callbackURL+"?"+honest.Encode())
		}()
		return nil
	})

	result, err := login.Run(testContext(t, 30*time.Second))
	if err != nil {
		t.Fatalf("the login did not survive a rejected callback: %v", err)
	}
	if result.Subject != "user-1" {
		t.Fatalf("unexpected subject %q", result.Subject)
	}
}

// TestLoginRefusesAnIdentityTokenWithoutTheNonce covers an issuer that does not
// echo the value binding the identity token to this request. The shell sends a
// nonce, so an identity token that omits it cannot be shown to belong to this
// login and is refused rather than trusted.
func TestLoginRefusesAnIdentityTokenWithoutTheNonce(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{
		Audience: "reference-status", AllowAnyLoopbackPort: true, OmitNonce: true,
	})
	printed := &recorder{}
	login := browserLogin(issuer, printed, func(authURL string) error {
		go visit(issuer, authURL)
		return nil
	})

	_, err := login.Run(testContext(t, 30*time.Second))
	if err == nil {
		t.Fatal("an identity token with no nonce was accepted")
	}
	_ = requireProblem(t, err, "auth.credential_unavailable")
}

func TestLoginRefusesAnIssuerWithoutS256(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{
		Audience: "reference-status", AllowAnyLoopbackPort: true, OmitS256: true,
	})
	printed := &recorder{}
	login := browserLogin(issuer, printed, func(string) error {
		t.Error("the login opened a browser against an issuer without S256")
		return nil
	})

	_, err := login.Run(testContext(t, 30*time.Second))
	if err == nil {
		t.Fatal("an issuer that does not advertise S256 was accepted")
	}
	_ = requireProblem(t, err, "auth.discovery_failed")
}

func TestLoginRefusesAnUnreachableIssuer(t *testing.T) {
	login := oauthflow.Login{
		Issuer:   "http://127.0.0.1:1",
		ClientID: "client-123",
		Out:      &recorder{},
		Ports:    []int{0},
		OpenBrowser: func(string) error {
			t.Error("the login opened a browser against an unreachable issuer")
			return nil
		},
	}

	_, err := login.Run(testContext(t, 30*time.Second))
	if err == nil {
		t.Fatal("an unreachable issuer was accepted")
	}
	_ = requireProblem(t, err, "auth.discovery_failed")
}

func TestLoginRefusesWhenNoLoopbackPortIsFree(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "reference-status"})
	for _, port := range oauthflow.LoopbackPorts() {
		// A port this test cannot take is already busy, which is the condition
		// under test either way.
		listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err == nil {
			t.Cleanup(func() { _ = listener.Close() })
		}
	}
	printed := &recorder{}
	login := oauthflow.Login{
		Issuer:     issuer.URL,
		ClientID:   "client-123",
		HTTPClient: issuer.HTTPClient(),
		Out:        printed,
		OpenBrowser: func(string) error {
			t.Error("the login opened a browser with no callback port bound")
			return nil
		},
	}

	_, err := login.Run(testContext(t, 30*time.Second))
	if err == nil {
		t.Fatal("the login ran with every callback port busy")
	}
	refusal := requireProblem(t, err, "auth.discovery_failed")
	for _, port := range oauthflow.LoopbackPorts() {
		if !strings.Contains(refusal.Recovery, strconv.Itoa(port)) {
			t.Fatalf("the recovery does not name port %d: %s", port, refusal.Recovery)
		}
	}
}
