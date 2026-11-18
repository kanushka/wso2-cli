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

// Package oidcdiscovery is the one OpenID discovery every part of the shell
// performs at an issuer: the broker's, the login flows', and wso2 doctor's.
// It lives apart from issuertrust because it carries go-oidc and with it an
// HTTP client, which issuertrust, imported by context validation, must not.
package oidcdiscovery

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	oidc "github.com/coreos/go-oidc/v3/oidc"

	"github.com/wso2/wso2-cli/internal/auth/issuertrust"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// maxNamedIssuer caps how much of a provider-named issuer a refusal quotes.
// The value is the provider's, not the user's, so it is bounded as well as
// escaped before the shell renders it.
const maxNamedIssuer = 64

// Discover reads the OpenID configuration of the provider at issuer, over the
// HTTP client ctx carries (oidc.ClientContext), and refuses one the shell may
// not use. issuerID is the issuer identifier the context declares the
// provider names, empty when it names issuer itself as OpenID Connect
// Discovery requires.
//
// A declared issuerID is handed to go-oidc through InsecureIssuerURLContext:
// the configuration is still fetched from issuer, and the identifier every
// identity token is later verified against becomes issuerID. That facility
// drops the library's own comparison of the two outright, so Discover makes
// it instead: the provider must name exactly issuerID, never whatever it
// chooses. See ADR 0020.
//
// Every error is a problem.Problem, and none repeats what the provider said
// beyond the issuer identifier it names: the provider's error text may quote
// the request that produced it, and the shell renders problems verbatim. Two
// causes are named because each has its own remedy: an untrusted certificate,
// which every fresh self-hosted deployment fails with, and a provider naming
// an issuer other than the one expected. Everything else stays the closed
// discovery failure, because guessing among the rest would put the shell in
// the position of explaining a deployment it cannot see.
func Discover(ctx context.Context, issuer, issuerID string) (*oidc.Provider, error) {
	if issuerID != "" {
		ctx = oidc.InsecureIssuerURLContext(ctx, issuerID)
	}
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		var mismatch *oidc.IssuerMismatchError
		switch {
		case issuertrust.Untrusted(err):
			return nil, issuertrust.Problem(issuer)
		case errors.As(err, &mismatch):
			return nil, mismatchProblem(mismatch.Discovered)
		case unanswered(err):
			return nil, Unreachable()
		}
		return nil, Unreadable()
	}
	plaintext := issuertrust.Plaintext
	if issuerID != "" {
		var named struct {
			Issuer string `json:"issuer"`
		}
		if provider.Claims(&named) != nil {
			return nil, Unreadable()
		}
		if named.Issuer != issuerID {
			return nil, mismatchProblem(named.Issuer)
		}
		plaintext = issuertrust.PlaintextEndpoints
	}
	if plaintext(provider.Claims) {
		return nil, issuertrust.PlaintextProblem()
	}
	return provider, nil
}

// Failed is a discovery refusal: the provider answered in a way the shell
// will not log in through. Every one keeps the code
// auth.discovery_failed, because the stable code list is closed; the message
// and recovery say which way and what to change.
func Failed(message, recovery string) problem.Problem {
	return problem.New(problem.CategoryAuthPolicy, "auth.discovery_failed", message).WithRecovery(recovery)
}

// discoveryRecovery is what to do about a configuration that could not be
// read, whether the provider answered badly or not at all.
const discoveryRecovery = "Check the issuer of the selected context and that this machine can reach it, then retry."

// Unreadable reports a provider that answered without a usable
// configuration.
func Unreadable() problem.Problem {
	return Failed("the shell could not read the identity provider's OpenID configuration", discoveryRecovery)
}

// Unreachable reports a provider that gave no answer at all. It keeps the
// discovery failure's code and recovery, and is told apart by its category,
// which exits 68 rather than 77 (ADR 0023).
func Unreachable() problem.Problem {
	return problem.New(problem.CategoryUnreachable, "auth.discovery_failed",
		"the shell could not reach the identity provider to read its OpenID configuration").
		WithRecovery(discoveryRecovery)
}

// unanswered reports a request that failed in the transport, before any HTTP
// response arrived. go-oidc returns a response it could not use as a plain
// error, so a *url.Error here means the request itself never completed.
func unanswered(err error) bool {
	var transport *url.Error
	return errors.As(err, &transport)
}

// mismatchProblem is the refusal for a provider whose configuration names
// the issuer named rather than the one the context expects. It says why,
// since the generic recovery would send the user to check a provider that
// answered. The provider's identifier is quoted, escaped and capped, because
// it is the value the user may need to declare; the context's own issuer and
// issuer identifier are not, because the shell never repeats a configured
// issuer.
func mismatchProblem(named string) problem.Problem {
	return Failed(fmt.Sprintf("the identity provider names a different issuer, %.*q, than the selected "+
		"context expects", maxNamedIssuer, named),
		"If that is expected, set login.issuerId in the selected context to the issuer the provider "+
			"names, then retry. Otherwise check the issuer of the selected context.")
}
