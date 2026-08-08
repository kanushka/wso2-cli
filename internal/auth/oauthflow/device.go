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

package oauthflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	oidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// DeviceLogin runs one RFC 8628 Device Authorization Grant login.
//
// It exists for the machine the browser login cannot serve. That login binds a
// loopback listener and waits to be redirected back to it, so finishing it
// needs a browser that can reach 127.0.0.1 on this machine — which a developer
// working over SSH, or inside a container, does not have. This flow binds
// nothing and waits for nothing local: the shell prints a short URI and a code,
// and the approval happens on whatever device the person actually has a browser
// on.
//
// What it produces is what the browser login produces, so everything downstream
// of a session is identical and no product module can tell the two apart.
type DeviceLogin struct {
	// Issuer is the OpenID provider to discover and authenticate against.
	Issuer string
	// ClientID is the public OAuth client this shell presents itself as. As in
	// the browser login there is no client secret; here the device code is what
	// binds the eventual token to the request this process made.
	ClientID string
	// Scopes are the permissions to request beyond openid and offline_access —
	// the identity's product scope union.
	Scopes []string
	// HTTPClient serves discovery, the device authorization request, and the
	// polling. It defaults to http.DefaultClient.
	HTTPClient *http.Client
	// Out receives the verification instructions. It defaults to standard
	// output, because a device login whose code goes nowhere is a login nobody
	// can complete.
	Out io.Writer
}

// Run performs the login and returns the issued token with the identity behind
// it.
//
// The protocol is x/oauth2's, deliberately: it owns the polling rule this flow
// most needs to get right — poll no faster than the interval the deployment
// advertised, add five seconds for this and every later request when told to
// slow down, and stop when the code expires. Reimplementing that would put the
// shell in the position of load-testing deployments it does not own.
//
// What this file owns is the part no library can: refusing a deployment that
// does not offer the grant before anything is printed, presenting two values so
// they survive being carried to another device, and turning every failure into
// a typed problem that never repeats the deployment's own words.
func (d DeviceLogin) Run(ctx context.Context) (Result, error) {
	ctx = oidc.ClientContext(ctx, d.httpClient())
	provider, err := oidc.NewProvider(ctx, d.Issuer)
	if err != nil {
		return Result{}, discoveryFailed(
			"the shell could not read the identity provider's OpenID configuration",
			"Check the issuer of the selected context and that this machine can reach it, then retry.")
	}

	endpoint := provider.Endpoint()
	// The advertised endpoint is the capability test, and it is made before a
	// code is printed rather than after. A deployment that does not offer the
	// grant would otherwise leave a user reading out a code towards an approval
	// screen that does not exist, and blaming themselves for it.
	if endpoint.DeviceAuthURL == "" {
		return Result{}, discoveryFailed(
			"the identity provider does not advertise the device authorization grant",
			"Enable the device authorization grant on the registered OAuth application, or select a "+
				"context whose identity logs in through the browser. Not every deployment offers this "+
				"grant.")
	}
	// A public client names itself in the request body, as RFC 6749 requires of
	// one. Saying so explicitly also spares every request the library's probe
	// with HTTP Basic credentials this shell does not have.
	endpoint.AuthStyle = oauth2.AuthStyleInParams
	config := oauth2.Config{ClientID: d.ClientID, Endpoint: endpoint, Scopes: d.scopes()}

	authorization, err := config.DeviceAuth(ctx)
	if err != nil {
		return Result{}, notCompleted(
			"the identity provider would not start a device authorization for this login",
			"Confirm the client identifier in the selected context, and that its OAuth application is "+
				"registered for the device authorization grant, then retry wso2 login.")
	}
	authorization.Interval = usableInterval(authorization.Interval)
	if err := d.present(authorization); err != nil {
		return Result{}, err
	}

	token, err := config.DeviceAccessToken(ctx, authorization)
	if err != nil {
		return Result{}, approvalFailed(err)
	}
	return d.identify(ctx, provider, token)
}

const (
	// defaultPollIntervalSeconds is the interval RFC 8628 section 3.2 requires
	// a client to assume when the deployment advertises none.
	defaultPollIntervalSeconds = 5
	// maxPollIntervalSeconds is the longest advertised interval this shell will
	// carry into its polling arithmetic. It is far beyond any deadline a login
	// runs under, so clamping here cannot make the shell poll sooner than a
	// deployment asked: an interval this long means the code expires before a
	// single poll either way.
	maxPollIntervalSeconds = 3600
)

// usableInterval replaces a polling interval this shell cannot act on.
//
// RFC 8628 lets the deployment choose the interval, and x/oauth2 substitutes
// the specification's default only when the member is exactly zero. Every other
// unusable value is carried into time.NewTicker, which panics on a non-positive
// duration — so a deployment answering with a negative interval, or one large
// enough to overflow the conversion to nanoseconds, would take the shell down
// with a stack trace.
//
// That matters beyond tidiness. A panic escapes the problem type entirely: it
// prints a Go stack trace rather than a refusal, and it exits with a code
// outside the class list a script branches on. This shell renders typed
// problems; what a deployment says must not be able to stop it doing that.
func usableInterval(advertised int64) int64 {
	switch {
	case advertised <= 0:
		return defaultPollIntervalSeconds
	case advertised > maxPollIntervalSeconds:
		return maxPollIntervalSeconds
	default:
		return advertised
	}
}

// present writes the two values the user has to carry to another device.
//
// Both are printed on their own indented line rather than inside a sentence,
// because a terminal that wraps must not be able to split either one. The user
// code is written exactly as the deployment issued it: RFC 8628 section 6.1
// already asks a deployment to make the value readable, and the deployment is
// the party that validates what gets typed back, so a shell that re-cased it or
// stripped its separator would be prettifying a value it does not own.
//
// The device code is never printed. It is the value exchangeable for a session,
// and only the user code is meant for human eyes.
func (d DeviceLogin) present(authorization *oauth2.DeviceAuthResponse) error {
	_, err := fmt.Fprintf(d.out(),
		"To log in, visit:\n\n    %s\n\nand enter the code:\n\n    %s\n\n",
		authorization.VerificationURI, authorization.UserCode)
	if err != nil {
		return notCompleted("the shell could not print the instructions this login needs",
			"Run wso2 login with its diagnostic output attached to your terminal.")
	}
	// The complete URI carries the code already, so it saves typing on a device
	// that can follow a link. It is offered second and never alone: it cannot be
	// read aloud, and RFC 8628 makes it optional, so a login must not depend on
	// a deployment publishing one.
	if authorization.VerificationURIComplete != "" {
		_, _ = fmt.Fprintf(d.out(), "Or open this link, which carries the code:\n\n    %s\n\n",
			authorization.VerificationURIComplete)
	}
	_, _ = fmt.Fprint(d.out(), "Waiting for you to approve this login...\n")
	return nil
}

// identify reads who the login proved you are.
//
// Absent and invalid are two different answers here, and the difference is the
// whole of this function.
//
// A *missing* identity token does not fail a device login, unlike a browser
// one. The browser login can afford to refuse because the authorization code
// flow is defined to carry one and there is a nonce to check it against; RFC
// 8628 defines no nonce, and whether WSO2 deployments return an identity token
// from this grant is not measured. The session is the refresh token, so a login
// that produced one has produced everything the shell needs, and refusing over
// a claim nothing depends on would let an unmeasured behaviour decide whether
// this flow works at all. What binds the answer to this process instead is the
// device code: it was minted for this request and is spent by it.
//
// A token that is *present and does not verify* is refused, exactly as the
// browser login refuses it. Nothing was wrong with the deployment's silence;
// something is wrong with its answer — a client identifier naming another
// application, an issuer that did not sign what it sent, a clock that disagrees.
// Every one of those is a fault the user can go and fix, and every one of them
// is invisible if the shell quietly reports no subject and carries on. Tolerating
// silence is a decision about an unmeasured protocol; tolerating a bad signature
// would be a decision to stop looking.
func (d DeviceLogin) identify(
	ctx context.Context, provider *oidc.Provider, token *oauth2.Token,
) (Result, error) {
	result := Result{Token: token}
	raw, _ := token.Extra("id_token").(string)
	if raw == "" {
		return result, nil
	}
	verified, err := provider.Verifier(&oidc.Config{ClientID: d.ClientID}).Verify(ctx, raw)
	if err != nil {
		return Result{}, identityNotVerified(err)
	}
	var claims struct {
		Email string `json:"email"`
	}
	_ = verified.Claims(&claims)
	result.Subject = verified.Subject
	result.Email = claims.Email
	return result, nil
}

// approvalFailed reports a device authorization that ended without a token, and
// says which of the endings it was.
//
// The code is the same for all of them because the caller is left in one place,
// holding no session. The message is not, because the reader is not: a person
// who declined the request themselves, a person who was too slow, and a person
// whose deployment broke have three different things to do, and only one of them
// is helped by simply running the command again.
//
// RFC 8628's two waiting answers — authorization_pending and slow_down — never
// arrive here. They are how the deployment says "keep going", the library acts
// on both, and neither is a failure to report.
func approvalFailed(err error) error {
	var refusal *oauth2.RetrieveError
	if errors.As(err, &refusal) {
		switch refusal.ErrorCode {
		case "access_denied":
			return notCompleted("the login was declined at the identity provider",
				"Run wso2 login again and approve the request, checking that the code shown in the "+
					"terminal is the one on the approval screen.")
		case "expired_token":
			return notCompleted("the approval window closed before this login was approved",
				"Run wso2 login again and approve the request promptly. The code is short-lived by "+
					"design.")
		}
		return notCompleted("the identity provider refused to complete this device login",
			"Retry wso2 login. If it keeps failing, confirm the client identifier and that the OAuth "+
				"application is registered for the device authorization grant.")
	}
	// The library turns an elapsed deadline into the context's own error. The
	// user is where an expired code leaves them, and is told the same thing.
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return notCompleted("this login was not approved in time",
			"Run wso2 login again and approve the request promptly. The code is short-lived by design.")
	}
	return notCompleted("the shell lost contact with the identity provider while waiting for approval",
		"Check that this machine can reach the issuer of the selected context, then retry wso2 login.")
}

// scopes asks for the same permissions a browser login asks for, so a session
// established either way narrows down to the same product access afterwards.
func (d DeviceLogin) scopes() []string {
	return Login{Scopes: d.Scopes}.scopes()
}

// httpClient wraps the caller's client so a key set is read for its keys and
// not for the certificates beside them. See certificateStripper — WSO2
// deployments publish certificates Go will not parse, and this flow verifies
// identity tokens against those key sets exactly as the browser login does.
func (d DeviceLogin) httpClient() *http.Client {
	return Login{HTTPClient: d.HTTPClient}.httpClient()
}

func (d DeviceLogin) out() io.Writer {
	return Login{Out: d.Out}.out()
}
