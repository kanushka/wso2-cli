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

// Package oauthflow runs one interactive login, in either of the two modes an
// OIDC identity can be established by.
//
// Login is the browser Authorization Code + PKCE mode, the default for a person
// sitting at the machine. DeviceLogin is the RFC 8628 Device Authorization
// Grant mode, for a machine whose user has no browser it can reach — a remote
// shell, a container — where the approval happens on another device entirely.
// Both produce the same Result, so a session established either way is
// indistinguishable everywhere downstream.
//
// The standard libraries own the protocol: go-oidc discovers the issuer and
// verifies the identity token, and x/oauth2 builds the authorization URL,
// exchanges the code, and runs the device polling loop. This package owns only
// what they cannot — binding a callback port the OAuth application is actually
// registered for, printing what the user must act on before anything else can
// fail, proving the callback that arrives belongs to the login this process
// started, and turning every failure into a typed problem that never repeats
// the deployment's own words.
//
// No value the flows produce is ever written to their output stream: the
// terminal sees a URL, a user code, and progress — never token material, and
// never the device code.
package oauthflow

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	oidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/wso2/wso2-cli/sdk/problem"
)

const (
	// callbackHost is the only interface a login callback is accepted on. A
	// loopback address is what makes a public client safe without a secret:
	// nothing off this machine can receive the authorization code.
	callbackHost = "127.0.0.1"
	// callbackPath is the fixed path of the registered callback URL.
	callbackPath = "/callback"
	// challengeMethodS256 is the only code challenge method this shell logs in
	// with. A deployment that does not advertise it is refused, not downgraded.
	challengeMethodS256 = "S256"
	// scopeOfflineAccess asks for the refresh token the session is built on.
	scopeOfflineAccess = "offline_access"
)

// LoopbackPorts is the fixed callback port sequence, tried in order. All four
// URLs are part of the documented OAuth application registration, so a shell
// whose first choice is busy still lands on a registered redirect.
//
// It is a function rather than a variable so no importer can change where this
// shell will accept an authorization code.
func LoopbackPorts() []int { return []int{10425, 10426, 10427, 10428} }

// Login runs one browser Authorization Code + PKCE login.
type Login struct {
	// Issuer is the OpenID provider to discover and authenticate against.
	Issuer string
	// ClientID is the public OAuth client this shell presents itself as. There
	// is deliberately no client secret: PKCE is the proof of possession.
	ClientID string
	// Scopes are the permissions to request beyond openid and offline_access —
	// the identity's product scope union.
	Scopes []string
	// HTTPClient serves discovery and the code exchange. It defaults to
	// http.DefaultClient.
	HTTPClient *http.Client
	// OpenBrowser opens the authorization URL. It defaults to the OS opener.
	// Failing to open a browser is not failing to log in: the URL is printed
	// first, so the user can complete the login from anywhere.
	OpenBrowser func(url string) error
	// Out receives the always-printed authorization URL and progress lines. It
	// defaults to standard output, because a login whose URL goes nowhere is
	// a login nobody can complete.
	Out io.Writer
	// Resource is the protected resource this session is for, sent as an RFC
	// 8707 resource indicator. It is empty for a deployment that decides the
	// audience from the application's registration instead, and naming one
	// there would ask for a narrowing the deployment has no way to honor.
	//
	// It belongs to the login rather than to the grant that follows because the
	// deployments that take it decide the audience once, at authorization: a
	// session established without it cannot be bound to a resource afterwards,
	// and one established with it reaches that resource and no other.
	Resource string
	// Ports overrides LoopbackPorts. Tests bind an ephemeral port with []int{0}.
	Ports []int
}

// Result is a completed login. It holds credential material and is never
// rendered: the shell reads the subject and email from it and stores the rest.
type Result struct {
	// Token is the issued token set, including the refresh token.
	Token *oauth2.Token
	// Subject is the verified identity token's subject.
	Subject string
	// Email is the verified identity token's email claim, when it carries one.
	Email string
}

// Run performs the login and returns the issued token with the verified
// identity behind it.
//
// Every failure is a typed authentication-policy problem. None of them repeat
// what the issuer said: a provider's error text may quote the request that
// produced it, and this shell renders problems verbatim.
func (l Login) Run(ctx context.Context) (Result, error) {
	ctx = oidc.ClientContext(ctx, l.httpClient())
	provider, err := oidc.NewProvider(ctx, l.Issuer)
	if err != nil {
		return Result{}, discoveryFailed(
			"the shell could not read the identity provider's OpenID configuration",
			"Check the issuer of the selected context and that this machine can reach it, then retry.")
	}
	var capabilities struct {
		CodeChallengeMethods []string `json:"code_challenge_methods_supported"`
	}
	if err := provider.Claims(&capabilities); err != nil ||
		!slices.Contains(capabilities.CodeChallengeMethods, challengeMethodS256) {
		return Result{}, discoveryFailed(
			"the identity provider does not advertise the S256 code challenge method",
			"Enable PKCE with S256 on the registered OAuth application, or select a context whose "+
				"issuer supports it. This shell is a public client and does not log in without it.")
	}

	// The one-time values are drawn before anything is bound or opened, so a
	// failure to generate them cannot leave a listener behind.
	verifier := oauth2.GenerateVerifier()
	state, err := randomValue()
	if err != nil {
		return Result{}, notCompleted("the shell could not generate the values that bind this login to this terminal",
			"Retry the command. Report the failure if it persists.")
	}
	nonce, err := randomValue()
	if err != nil {
		return Result{}, notCompleted("the shell could not generate the values that bind this login to this terminal",
			"Retry the command. Report the failure if it persists.")
	}

	listener, err := bindLoopback(l.ports())
	if err != nil {
		return Result{}, err
	}
	callback := serveCallback(listener, state)
	defer callback.close()

	// A public client has no secret to present, so it identifies itself in the
	// request body, as RFC 6749 requires of one. Saying so explicitly also
	// spares every exchange the library's probe request with HTTP Basic
	// credentials this shell does not have.
	endpoint := provider.Endpoint()
	endpoint.AuthStyle = oauth2.AuthStyleInParams
	config := oauth2.Config{
		ClientID:    l.ClientID,
		Endpoint:    endpoint,
		RedirectURL: redirectURL(listener),
		Scopes:      l.scopes(),
	}
	authOptions := []oauth2.AuthCodeOption{
		oauth2.S256ChallengeOption(verifier),
		oidc.Nonce(nonce),
	}
	// The indicator is sent on both legs. The authorization request is what
	// binds the session, and the exchange repeats it because a deployment is
	// entitled to check that the code it is redeeming was asked for on the same
	// terms it was issued under.
	if l.Resource != "" {
		authOptions = append(authOptions, oauth2.SetAuthURLParam("resource", l.Resource))
	}
	authURL := config.AuthCodeURL(state, authOptions...)
	if _, err := fmt.Fprintf(l.out(), "Open this URL to log in:\n%s\n", authURL); err != nil {
		return Result{}, notCompleted("the shell could not print the authorization URL this login needs",
			"Run wso2 login with standard output attached to your terminal.")
	}
	// Best effort by contract: the URL is already printed, so a machine with no
	// browser logs in exactly as well as one with a browser.
	_ = l.openBrowser(authURL)

	code, err := callback.wait(ctx)
	if err != nil {
		return Result{}, err
	}
	exchangeOptions := []oauth2.AuthCodeOption{oauth2.VerifierOption(verifier)}
	if l.Resource != "" {
		exchangeOptions = append(exchangeOptions, oauth2.SetAuthURLParam("resource", l.Resource))
	}
	token, err := config.Exchange(ctx, code, exchangeOptions...)
	if err != nil {
		return Result{}, notCompleted("the identity provider refused to exchange this login for a session",
			"Retry wso2 login. If it keeps failing, confirm the client identifier and the registered "+
				"callback URLs of the OAuth application.")
	}
	rawIDToken, _ := token.Extra("id_token").(string)
	if rawIDToken == "" {
		return Result{}, notCompleted("the identity provider returned no identity token for this login",
			"Grant the OAuth application the openid scope, then retry wso2 login.")
	}
	idToken, err := provider.Verifier(&oidc.Config{ClientID: l.ClientID}).Verify(ctx, rawIDToken)
	if err != nil {
		return Result{}, identityNotVerified(err)
	}
	// The nonce proves the identity token was minted for this login and not
	// replayed from another one.
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(nonce)) != 1 {
		return Result{}, notCompleted("the identity token this login returned belongs to another request",
			"Retry wso2 login from a single terminal and complete only the sign-in it opens.")
	}
	var claims struct {
		Email string `json:"email"`
	}
	// An issuer that discloses no email is a legal issuer; the login reports
	// what it verified rather than refusing over a claim it did not need.
	_ = idToken.Claims(&claims)
	return Result{Token: token, Subject: idToken.Subject, Email: claims.Email}, nil
}

func (l Login) scopes() []string {
	requested := []string{oidc.ScopeOpenID, scopeOfflineAccess}
	for _, scope := range l.Scopes {
		if !slices.Contains(requested, scope) {
			requested = append(requested, scope)
		}
	}
	return requested
}

func (l Login) ports() []int {
	if len(l.Ports) > 0 {
		return l.Ports
	}
	return LoopbackPorts()
}

// httpClient is the client every fetch this login makes goes through, wrapped
// so that a key set is read for its keys and not for the certificates beside
// them. See certificateStripper.
func (l Login) httpClient() *http.Client {
	base := http.DefaultClient
	if l.HTTPClient != nil {
		base = l.HTTPClient
	}
	stripped := *base
	stripped.Transport = certificateStripper{base: base.Transport}
	return &stripped
}

func (l Login) out() io.Writer {
	if l.Out != nil {
		return l.Out
	}
	return os.Stdout
}

func (l Login) openBrowser(target string) error {
	if l.OpenBrowser != nil {
		return l.OpenBrowser(target)
	}
	return Open(target)
}

// bindLoopback takes the first free callback port, in the registered order.
func bindLoopback(ports []int) (net.Listener, error) {
	for _, port := range ports {
		listener, err := net.Listen("tcp4", net.JoinHostPort(callbackHost, strconv.Itoa(port)))
		if err == nil {
			return listener, nil
		}
	}
	// The stable code list is closed and names no port-exhaustion code, so this
	// travels as a discovery failure whose message states the real cause.
	return nil, discoveryFailed("no loopback callback port is available for the browser login",
		fmt.Sprintf("Free one of the ports %s on 127.0.0.1, then retry. The shell accepts a login "+
			"callback only on the ports the OAuth application registers.", portList()))
}

// redirectURL is the callback the issuer must redirect to. It is derived from
// the bound listener, so it always names the port actually being listened on.
func redirectURL(listener net.Listener) string {
	address := listener.Addr().(*net.TCPAddr)
	return "http://" + net.JoinHostPort(callbackHost, strconv.Itoa(address.Port)) + callbackPath
}

func portList() string {
	ports := LoopbackPorts()
	names := make([]string, 0, len(ports))
	for _, port := range ports {
		names = append(names, strconv.Itoa(port))
	}
	return strings.Join(names[:len(names)-1], ", ") + ", or " + names[len(names)-1]
}

func randomValue() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func discoveryFailed(message, recovery string) problem.Problem {
	return problem.New(problem.CategoryAuthPolicy, "auth.discovery_failed", message).WithRecovery(recovery)
}

// notCompleted reports a login that started and produced no session.
//
// It reuses auth.credential_unavailable rather than minting a code: the stable
// code list is closed, and every cause here — a refused exchange, an identity
// token that did not verify, a browser that never came back — leaves the caller
// in the same place, holding no credential and needing to log in again.
func notCompleted(message, recovery string) problem.Problem {
	return problem.New(problem.CategoryAuthPolicy, "auth.credential_unavailable", message).WithRecovery(recovery)
}

// identityNotVerified reports an identity token the shell would not accept,
// and says which kind of failure it was.
//
// The code stays the same for all of them, because the caller is left in one
// place. The message does not, because the reader is not: a token the issuer's
// keys did not sign, a token minted for a different application, and a key set
// the shell could not read are three different things to go and fix, and only
// one of them is helped by trying again. Retrying is the default advice
// precisely because it is the honest one when the cause is unknown, and it is
// the wrong advice for a cause the shell can name.
//
// The library states these failures in prose rather than in typed errors, so
// the classification reads its words. A wording change upstream costs the
// specific message and falls back to the general one; it cannot cost the
// refusal itself.
func identityNotVerified(err error) problem.Problem {
	var expired *oidc.TokenExpiredError
	switch reason := err.Error(); {
	case errors.As(err, &expired):
		return notCompleted("the identity token this login returned had already expired",
			"Check that this machine's clock is correct, then retry wso2 login.")
	case strings.Contains(reason, "fetching keys"):
		return notCompleted("the shell could not read the signing keys the identity provider publishes",
			"Confirm this machine can reach the issuer's JWKS endpoint. If it is reachable, the "+
				"deployment is publishing a key set this shell cannot parse; report it with the "+
				"issuer URL.")
	case strings.Contains(reason, "expected audience"):
		return notCompleted("the identity token this login returned was issued for a different application",
			"Confirm the client identifier in the selected context names the OAuth application this "+
				"issuer signed you in to.")
	case strings.Contains(reason, "failed to verify signature"):
		return notCompleted("the identity token this login returned was not signed by the identity provider's keys",
			"Retry wso2 login. If it keeps failing, confirm the issuer in the selected context is the "+
				"deployment that signed you in.")
	default:
		return notCompleted("the identity token this login returned did not verify",
			"Retry wso2 login. The shell does not accept an identity it cannot verify against the issuer's keys.")
	}
}

// callback is the loopback listener one login waits on.
type callback struct {
	server  *http.Server
	results chan callbackResult
}

// callbackResult is the single answer a login's callback produces.
type callbackResult struct {
	code string
	err  error
}

// callbackReadHeaderTimeout bounds how long a connection may hold the listener
// without stating what it wants.
const callbackReadHeaderTimeout = 10 * time.Second

// serveCallback starts serving the loopback callback for one login.
//
// The state is captured here rather than checked by the caller so that no path
// through this package can accept a code without it.
func serveCallback(listener net.Listener, state string) *callback {
	waiting := &callback{results: make(chan callbackResult, 1)}
	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		// A callback carrying another login's state is not this login's. It is
		// answered and discarded while the listener keeps waiting, which is what
		// makes a stray or forged browser tab pointed at this port harmless.
		if subtle.ConstantTimeCompare([]byte(query.Get("state")), []byte(state)) != 1 {
			respond(w, http.StatusBadRequest,
				"This callback does not belong to a login started from this terminal.")
			return
		}
		if query.Get("error") != "" {
			// The provider's error code is not echoed to the terminal; what the
			// user must do is the same whichever refusal it was.
			respond(w, http.StatusOK, "Login failed. Return to the terminal.")
			waiting.finish(callbackResult{err: notCompleted(
				"the identity provider refused this login",
				"Retry wso2 login and complete the sign-in the browser asks for.")})
			return
		}
		code := query.Get("code")
		if code == "" {
			respond(w, http.StatusBadRequest, "This callback carries no authorization code.")
			return
		}
		respond(w, http.StatusOK, "Login complete. You can close this tab and return to the terminal.")
		waiting.finish(callbackResult{code: code})
	})
	waiting.server = &http.Server{Handler: mux, ReadHeaderTimeout: callbackReadHeaderTimeout}
	go func() { _ = waiting.server.Serve(listener) }()
	return waiting
}

// finish delivers the first outcome and drops every later one: a login has
// exactly one answer, and a second callback must not block the browser that
// sent it.
func (c *callback) finish(result callbackResult) {
	select {
	case c.results <- result:
	default:
	}
}

// wait blocks until the browser comes back or the caller gives up.
func (c *callback) wait(ctx context.Context) (string, error) {
	select {
	case result := <-c.results:
		return result.code, result.err
	case <-ctx.Done():
		return "", notCompleted("the browser login did not complete",
			"Run wso2 login again and finish the sign-in in the browser, or open the printed URL yourself.")
	}
}

// close stops the listener. It runs even on the paths that already have a code,
// so no login leaves a port bound behind it.
func (c *callback) close() { _ = c.server.Close() }

// respond writes the one page the user ever sees from the shell's own listener.
// It is flushed before the code is delivered, so shutting the listener down
// cannot truncate the page in the browser.
func respond(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, "<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\">"+
		"<title>WSO2 CLI</title></head><body><p>"+message+"</p></body></html>\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}
