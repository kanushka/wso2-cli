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

// Package statusservice is the architecture proof's local read-only status
// service.
//
// It stands in for a product service so the brokered-access boundary has
// something real to be proved against: it accepts a request only when the
// presented fixture token is the one this invocation was granted, for this
// audience, scope, and organization, and is still within its life. Anything
// else is refused without the service doing any work.
//
// It is test infrastructure. It is an internal package with no shell command
// behind it, it is never linked into the shell binary, and it establishes no
// product API. A real product service replaces it and enforces the same claims
// through its own identity provider.
package statusservice

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/wso2/wso2-cli/internal/auth/devtoken"
)

// StatusPath is the only path the service serves.
//
// The reference module repeats this path, the bearer scheme, and
// InvocationHeader rather than importing them: a product module cannot depend
// on a shell package, so a service's call shape is knowledge a module carries
// on its own. The duplication is the boundary being real.
const StatusPath = "/status"

// InvocationHeader names the shell invocation a request belongs to.
//
// The service compares it with the token's own invocation claim, so a caller
// cannot present access granted for one invocation as part of another. Both
// values come from the caller, so this is a binding check rather than replay
// detection: it proves the token belongs to the invocation the caller is
// acting as, not that the caller is that invocation. Proving the latter needs
// an identity provider the audience trusts, which is exactly what the
// production replacement for this fixture brings.
const InvocationHeader = "X-WSO2-Invocation-Id"

// ServiceName is the service this fixture reports itself as.
const ServiceName = "reference"

// Options are the policy one service instance enforces.
type Options struct {
	// Audience is the audience this service serves. A token for any other
	// audience is refused.
	Audience string
	// RequiredScope is the permission a caller must hold.
	RequiredScope string
	// Organization is the organization this service answers for.
	Organization string
	// SourceCredential is the development credential the shell's issuer signs
	// with. Holding it is what lets this fixture verify a token without an
	// identity provider, and it is why the fixture is not a production design.
	SourceCredential string
	// Now reads the current time. It defaults to time.Now, and a test sets it
	// to prove expiry.
	Now func() time.Time
	// Fault makes the service fail instead of answering, so a product-service
	// failure can be told apart from an access failure.
	Fault bool
}

// Service answers status requests for one audience and organization.
type Service struct {
	options Options
}

// New builds a service, refusing to start without the policy it enforces. A
// service that cannot say what it accepts would accept anything.
func New(options Options) (*Service, error) {
	switch {
	case options.Audience == "":
		return nil, errors.New("statusservice: an audience is required")
	case options.RequiredScope == "":
		return nil, errors.New("statusservice: a required scope is required")
	case options.Organization == "":
		return nil, errors.New("statusservice: an organization is required")
	case options.SourceCredential == "":
		return nil, errors.New("statusservice: a source credential is required to verify tokens")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Service{options: options}, nil
}

// ServeHTTP answers one status request.
func (s *Service) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		s.fail(writer, http.StatusMethodNotAllowed, "method_not_allowed",
			"the reference status service is read-only")
		return
	}
	if request.URL.Path != StatusPath {
		s.fail(writer, http.StatusNotFound, "not_found", "the reference status service serves "+StatusPath)
		return
	}

	claims, failure := s.authorize(request)
	if failure != nil {
		s.fail(writer, failure.status, failure.code, failure.message)
		return
	}

	// The fault is applied after authorization, so the failure a test observes
	// is the service failing rather than the request being refused.
	if s.options.Fault {
		s.fail(writer, http.StatusInternalServerError, "unavailable",
			"the reference status service cannot read its status")
		return
	}

	checkedAt := s.options.Now().UTC()
	s.write(writer, http.StatusOK, map[string]string{
		"organization": claims.Organization,
		"service":      ServiceName,
		"status":       "operational",
		"checkedAt":    checkedAt.Format(time.RFC3339),
	})
}

// refusal is why a request was not served.
type refusal struct {
	status  int
	code    string
	message string
}

// authorize proves the caller presented this invocation's granted access.
//
// Every check is against what the service itself serves, never against what the
// request asks for, so a caller cannot widen its access by asserting a
// different audience, scope, organization, or invocation.
func (s *Service) authorize(request *http.Request) (devtoken.Claims, *refusal) {
	presented, found := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer ")
	if !found || strings.TrimSpace(presented) == "" {
		return devtoken.Claims{}, &refusal{http.StatusUnauthorized, "unauthenticated",
			"the request presented no bearer token"}
	}
	invocation := request.Header.Get(InvocationHeader)
	if invocation == "" {
		return devtoken.Claims{}, &refusal{http.StatusUnauthorized, "unauthenticated",
			"the request does not name the invocation it belongs to"}
	}

	claims, err := devtoken.Verify(s.options.SourceCredential, strings.TrimSpace(presented), s.options.Now())
	switch {
	case errors.Is(err, devtoken.ErrExpired):
		return devtoken.Claims{}, &refusal{http.StatusUnauthorized, "token_expired",
			"the presented access has expired"}
	case err != nil:
		return devtoken.Claims{}, &refusal{http.StatusUnauthorized, "token_rejected",
			"the presented access was not issued for this service"}
	}

	switch {
	case claims.Audience != s.options.Audience:
		return devtoken.Claims{}, notAccepted("audience")
	case !claims.Allows(s.options.RequiredScope):
		return devtoken.Claims{}, notAccepted("scope")
	case claims.Organization != s.options.Organization:
		return devtoken.Claims{}, notAccepted("organization")
	case claims.Invocation != invocation:
		return devtoken.Claims{}, notAccepted("invocation")
	}
	return claims, nil
}

// notAccepted refuses a token whose claims are not the ones this service
// serves. The claim that failed is named because it is not secret and it is
// what a developer needs; the values are not.
func notAccepted(claim string) *refusal {
	return &refusal{http.StatusForbidden, "access_not_accepted",
		fmt.Sprintf("the presented access is not for the %s this service serves", claim)}
}

func (s *Service) fail(writer http.ResponseWriter, status int, code, message string) {
	s.write(writer, status, map[string]string{
		"code":    "status_service." + code,
		"message": message,
	})
}

// write answers with one JSON document. Nothing derived from the source
// credential is ever part of it.
func (s *Service) write(writer http.ResponseWriter, status int, document map[string]string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(document)
}
