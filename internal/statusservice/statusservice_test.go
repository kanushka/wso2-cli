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

package statusservice_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wso2/wso2-cli/internal/auth/devtoken"
	"github.com/wso2/wso2-cli/internal/statusservice"
)

const (
	sourceCredential = "canary-source-credential-2f8c"
	audience         = "reference-status"
	readScope        = "reference:status:read"
	organization     = "reference-org"
	invocation       = "invocation-7f2a"
)

var checkedAt = time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC)

func options() statusservice.Options {
	return statusservice.Options{
		Audience:         audience,
		RequiredScope:    readScope,
		Organization:     organization,
		SourceCredential: sourceCredential,
		Now:              func() time.Time { return checkedAt },
	}
}

func TestAValidTokenReturnsTheServiceStatus(t *testing.T) {
	response := call(t, options(), request(t, mint(t, claims())))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200\n%s", response.Code, response.Body.String())
	}
	var status struct {
		Organization string `json:"organization"`
		Service      string `json:"service"`
		Status       string `json:"status"`
		CheckedAt    string `json:"checkedAt"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatalf("the service answered with invalid JSON: %v\n%s", err, response.Body.String())
	}
	if status.Organization != organization {
		t.Errorf("organization = %q, want %q", status.Organization, organization)
	}
	if status.Service == "" {
		t.Error("the service did not name itself")
	}
	if status.Status != "operational" {
		t.Errorf("status = %q, want %q", status.Status, "operational")
	}
	if parsed, err := time.Parse(time.RFC3339, status.CheckedAt); err != nil {
		t.Errorf("checkedAt %q is not an RFC 3339 time: %v", status.CheckedAt, err)
	} else if !parsed.Equal(checkedAt) {
		t.Errorf("checkedAt = %s, want %s", parsed, checkedAt)
	}
}

func TestATokenForAnotherAudienceIsRejected(t *testing.T) {
	wrong := claims()
	wrong.Audience = "another-audience"

	response := call(t, options(), request(t, mint(t, wrong)))

	assertRejected(t, response, http.StatusForbidden)
}

func TestATokenWithoutTheReadScopeIsRejected(t *testing.T) {
	wrong := claims()
	wrong.Scopes = []string{"reference:status:write"}

	response := call(t, options(), request(t, mint(t, wrong)))

	assertRejected(t, response, http.StatusForbidden)
}

func TestATokenForAnotherOrganizationIsRejected(t *testing.T) {
	wrong := claims()
	wrong.Organization = "another-org"

	response := call(t, options(), request(t, mint(t, wrong)))

	assertRejected(t, response, http.StatusForbidden)
}

func TestAFixtureTokenThatNamesNoOrganizationIsRejected(t *testing.T) {
	// A wrong organization is refused by the service's own policy, which the
	// test above covers. An absent one is not: that policy lets a token naming
	// no organization through, because an issuer-minted token legitimately
	// names none and is bound to its organization by its issuer instead. The
	// fixture format has no such binding, so its verifier is what has to refuse
	// this — and it has to refuse it on its own, rather than by trusting that
	// the minter would never produce one.
	//
	// The verifier refuses a token naming no invocation for the same reason.
	// That case has no test because devtoken.Mint refuses to produce such a
	// token, and reproducing its signature here would copy the token format
	// into this file.
	anonymous := claims()
	anonymous.Organization = ""

	response := call(t, options(), request(t, mint(t, anonymous)))

	assertRejected(t, response, http.StatusUnauthorized)
}

func TestATokenMintedForAnotherInvocationIsRejected(t *testing.T) {
	// The token is bound to the invocation that acquired it, so a caller
	// acting as one invocation cannot present access granted to another.
	replayed := claims()
	replayed.Invocation = "an-earlier-invocation"

	response := call(t, options(), request(t, mint(t, replayed)))

	assertRejected(t, response, http.StatusForbidden)
}

func TestAnExpiredTokenIsRejected(t *testing.T) {
	expired := options()
	expired.Now = func() time.Time { return checkedAt.Add(devtoken.Lifetime + time.Second) }

	response := call(t, expired, request(t, mint(t, claims())))

	assertRejected(t, response, http.StatusUnauthorized)
}

func TestATokenSignedByAnotherCredentialIsRejected(t *testing.T) {
	foreign, err := devtoken.Mint("another-source-credential", claims(), checkedAt)
	if err != nil {
		t.Fatalf("Mint returned %v", err)
	}

	response := call(t, options(), request(t, foreign))

	assertRejected(t, response, http.StatusUnauthorized)
}

func TestAnUnauthenticatedRequestIsRejected(t *testing.T) {
	for name, prepare := range map[string]func(*http.Request){
		"no authorization":     func(r *http.Request) { r.Header.Del("Authorization") },
		"no bearer scheme":     func(r *http.Request) { r.Header.Set("Authorization", "Basic abcdef") },
		"empty bearer":         func(r *http.Request) { r.Header.Set("Authorization", "Bearer ") },
		"not a fixture token":  func(r *http.Request) { r.Header.Set("Authorization", "Bearer not-a-token") },
		"no invocation header": func(r *http.Request) { r.Header.Del(statusservice.InvocationHeader) },
	} {
		t.Run(name, func(t *testing.T) {
			outgoing := request(t, mint(t, claims()))
			prepare(outgoing)

			response := call(t, options(), outgoing)

			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401\n%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestTheServiceIsReadOnly(t *testing.T) {
	outgoing := request(t, mint(t, claims()))
	outgoing.Method = http.MethodPost

	response := call(t, options(), outgoing)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405\n%s", response.Code, response.Body.String())
	}
}

func TestAFaultyServiceFailsWithoutAnswering(t *testing.T) {
	// The proof needs a service failure that is not an access failure, so the
	// shell can be shown mapping the two to different exit classes.
	faulty := options()
	faulty.Fault = true

	response := call(t, faulty, request(t, mint(t, claims())))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500\n%s", response.Code, response.Body.String())
	}
}

func TestNoAnswerCarriesTheSourceCredential(t *testing.T) {
	rejected := claims()
	rejected.Audience = "another-audience"

	for _, response := range []*httptest.ResponseRecorder{
		call(t, options(), request(t, mint(t, claims()))),
		call(t, options(), request(t, mint(t, rejected))),
	} {
		if strings.Contains(response.Body.String(), sourceCredential) {
			t.Fatalf("the service answered with the source credential:\n%s", response.Body.String())
		}
	}
}

func TestTheServiceRefusesToStartWithoutItsOwnPolicy(t *testing.T) {
	for name, mutate := range map[string]func(*statusservice.Options){
		"no audience":       func(o *statusservice.Options) { o.Audience = "" },
		"no required scope": func(o *statusservice.Options) { o.RequiredScope = "" },
		"no organization":   func(o *statusservice.Options) { o.Organization = "" },
		// Clearing the source credential leaves no issuer either, so this is
		// the case where the service is given no way at all to verify a token.
		"neither a source credential nor an issuer": func(o *statusservice.Options) { o.SourceCredential = "" },
	} {
		t.Run(name, func(t *testing.T) {
			incomplete := options()
			mutate(&incomplete)
			if _, err := statusservice.New(incomplete); err == nil {
				t.Fatal("New accepted a service with incomplete policy")
			}
		})
	}
}

func claims() devtoken.Claims {
	return devtoken.Claims{
		Audience:     audience,
		Scopes:       []string{readScope},
		Organization: organization,
		Invocation:   invocation,
	}
}

func mint(t *testing.T, claims devtoken.Claims) string {
	t.Helper()
	token, err := devtoken.Mint(sourceCredential, claims, checkedAt)
	if err != nil {
		t.Fatalf("Mint returned %v", err)
	}
	return token
}

// request builds the call the reference module makes.
func request(t *testing.T, token string) *http.Request {
	t.Helper()
	outgoing := httptest.NewRequest(http.MethodGet, statusservice.StatusPath, nil)
	outgoing.Header.Set("Authorization", "Bearer "+token)
	outgoing.Header.Set(statusservice.InvocationHeader, invocation)
	return outgoing
}

func call(t *testing.T, options statusservice.Options, outgoing *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	service, err := statusservice.New(options)
	if err != nil {
		t.Fatalf("New returned %v", err)
	}
	response := httptest.NewRecorder()
	service.ServeHTTP(response, outgoing)
	return response
}

func assertRejected(t *testing.T, response *httptest.ResponseRecorder, want int) {
	t.Helper()
	if response.Code != want {
		t.Fatalf("status = %d, want %d\n%s", response.Code, want, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "operational") {
		t.Fatalf("a rejected request still received the service status:\n%s", response.Body.String())
	}
}

// whoamiRequest builds the whoami call the reference module makes.
func whoamiRequest(t *testing.T, token string) *http.Request {
	t.Helper()
	outgoing := httptest.NewRequest(http.MethodGet, statusservice.WhoamiPath, nil)
	outgoing.Header.Set("Authorization", "Bearer "+token)
	outgoing.Header.Set(statusservice.InvocationHeader, invocation)
	return outgoing
}

func decodeWhoami(t *testing.T, response *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200\n%s", response.Code, response.Body.String())
	}
	var granted map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &granted); err != nil {
		t.Fatalf("the whoami answer is not JSON: %v\n%s", err, response.Body.String())
	}
	return granted
}

func TestWhoamiReportsTheClaimsTheServiceVerified(t *testing.T) {
	granted := decodeWhoami(t, call(t, options(), whoamiRequest(t, mint(t, claims()))))

	for field, want := range map[string]string{
		"organization": organization,
		"audiences":    audience,
		"scopes":       readScope,
		"invocation":   invocation,
		"boundTo":      invocation,
	} {
		if granted[field] != want {
			t.Errorf("whoami reports %s = %q, want %q", field, granted[field], want)
		}
	}
}

func TestWhoamiIsAuthorizedExactlyAsTheStatusPathIs(t *testing.T) {
	// The endpoint reports claims, so an unauthorized caller learning them
	// would be a disclosure. It must refuse whatever the status path refuses.
	rejected := map[string]func() *http.Request{
		"no token": func() *http.Request {
			outgoing := httptest.NewRequest(http.MethodGet, statusservice.WhoamiPath, nil)
			outgoing.Header.Set(statusservice.InvocationHeader, invocation)
			return outgoing
		},
		"another invocation": func() *http.Request {
			bound := claims()
			bound.Invocation = "a-different-invocation"
			return whoamiRequest(t, mint(t, bound))
		},
	}
	for name, build := range rejected {
		t.Run(name, func(t *testing.T) {
			response := call(t, options(), build())
			if response.Code == http.StatusOK {
				t.Fatalf("whoami answered a request it should have refused:\n%s", response.Body.String())
			}
			if strings.Contains(response.Body.String(), audience) {
				t.Fatalf("a refused caller still learned the verified claims:\n%s", response.Body.String())
			}
		})
	}
}

func TestNoWhoamiAnswerCarriesTheSourceCredential(t *testing.T) {
	// whoami describes a token, so it is the endpoint most likely to echo one.
	token := mint(t, claims())
	response := call(t, options(), whoamiRequest(t, token))

	body := response.Body.String()
	if strings.Contains(body, token) {
		t.Fatalf("the whoami answer repeated the presented token:\n%s", body)
	}
	if strings.Contains(body, sourceCredential) {
		t.Fatalf("the whoami answer carried the source credential:\n%s", body)
	}
}
