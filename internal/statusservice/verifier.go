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

package statusservice

import (
	"errors"
	"slices"
	"time"

	"github.com/wso2/wso2-cli/internal/auth/devtoken"
)

// access is what a verified token asserts, normalized across token formats.
//
// A verifier answers in this shape so that authorize never learns which format
// it was handed. A member the format does not carry is left zero, and every
// check that reads one states what it does with the zero value — because "the
// token does not say" and "the token says something this service rejects" are
// different answers, and conflating them is how an absent claim becomes an
// unenforced one.
type access struct {
	// Audiences are the services the token names. RFC 7519 section 4.1.3
	// allows one or many, so this is always a list.
	Audiences []string
	// Scopes are the permissions the token conveys.
	Scopes []string
	// Organization is the organization the token itself names. It is empty
	// when the issuer mints no organization claim.
	Organization string
	// Invocation is the shell invocation the token was bound to. It is empty
	// for issuer-minted tokens, because no OAuth issuer mints such a claim.
	Invocation string
}

// serves reports whether the token is bound to the given audience.
func (a access) serves(audience string) bool {
	return slices.Contains(a.Audiences, audience)
}

// allows reports whether the token conveys the given permission.
func (a access) allows(scope string) bool {
	return slices.Contains(a.Scopes, scope)
}

// The refusals a verifier may report.
//
// They are meant to stay this coarse. A caller is told that its access expired
// or that it was not accepted, and the intent is that it is never told which
// token format failed, which key did not match, or how a signature was
// malformed — none of which it is owed, and all of which describe the service's
// own configuration.
var (
	errAccessExpired  = errors.New("statusservice: the presented access has expired")
	errAccessRejected = errors.New("statusservice: the presented access was not issued for this service")
)

// verifier establishes that a presented token is genuine and reads what it
// asserts.
//
// It proves origin only. Whether the claims are the ones this service serves is
// authorize's decision, kept there so that one policy answers for every token
// format rather than each format carrying its own copy of the rules.
// Each implementation holds its own clock, because go-oidc keeps the one it
// verifies with and a seam that also passed a time would let the two disagree.
type verifier interface {
	verify(presented string) (access, error)
}

// devtokenVerifier accepts the architecture proof's fixture token, which the
// shell signs and this service verifies with a source credential they both
// hold. It is why the fixture is not a production design, and it is the only
// verifier that can read an invocation binding.
type devtokenVerifier struct {
	sourceCredential string
	// now reads the current time, so a test that moves the service's clock
	// moves expiry with it.
	now func() time.Time
}

func (v devtokenVerifier) verify(presented string) (access, error) {
	claims, err := devtoken.Verify(v.sourceCredential, presented, v.now())
	switch {
	case errors.Is(err, devtoken.ErrExpired):
		return access{}, errAccessExpired
	case err != nil:
		return access{}, errAccessRejected
	}
	// This format carries both claims on every token, so an absent one is not
	// "the token does not say" — it is a token this verifier will not vouch
	// for. Refusing it here rather than leaving it to authorize is what keeps
	// the fixture path strict without depending on the minter's own guards:
	// authorize must let an absent organization through for the issuer-minted
	// format, which legitimately mints none.
	if claims.Organization == "" || claims.Invocation == "" {
		return access{}, errAccessRejected
	}
	return access{
		Audiences:    []string{claims.Audience},
		Scopes:       claims.Scopes,
		Organization: claims.Organization,
		Invocation:   claims.Invocation,
	}, nil
}
