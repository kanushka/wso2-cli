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

package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/wso2/wso2-cli/sdk/problem"
)

// The call shape of the local status service.
//
// A product module cannot import a shell package, so what a service expects is
// knowledge the module carries on its own. These values are repeated in
// internal/statusservice, and the duplication is the boundary being real: the
// module and the service agree by contract, not by sharing code.
const (
	statusPath = "/status"
	// invocationHeader names the invocation this call belongs to. The service
	// compares it with the token's own invocation claim, so access granted for
	// one command cannot be presented under another.
	invocationHeader = "X-WSO2-Invocation-Id"
)

// defaultStatusTimeout bounds one call to the status service when the
// invocation names no other. It is well inside the shell's invocation deadline,
// so a slow service produces this module's typed problem rather than the shell
// terminating the process. "wso2 reference call --timeout" overrides it, which
// is what gives this module a declared flag to exercise (#153).
const defaultStatusTimeout = 5 * time.Second

// serviceStatus is the status service's answer.
type serviceStatus struct {
	Organization string `json:"organization"`
	Service      string `json:"service"`
	Status       string `json:"status"`
	CheckedAt    string `json:"checkedAt"`
}

// readStatus reads the status service with brokered access.
//
// Everything that can go wrong becomes a typed problem the shell can render and
// map to an exit class. None of them repeats the token: a problem is rendered
// verbatim, and access material has no place in user-facing text.
func readStatus(ctx context.Context, endpoint, invocationID, token string, timeout time.Duration) (serviceStatus, error) {
	if endpoint == "" {
		return serviceStatus{}, problem.New(problem.CategoryUsage, "reference.no_endpoint",
			"the selected context does not name a reference status service").
			WithRecovery("Select a context whose endpoint names the local reference status service.")
	}
	target, err := url.JoinPath(endpoint, statusPath)
	if err != nil {
		return serviceStatus{}, problem.New(problem.CategoryUsage, "reference.unreadable_endpoint",
			"the selected context names an endpoint this module cannot call").
			WithRecovery("Select a context whose endpoint is an absolute HTTP URL.")
	}

	call, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(call, http.MethodGet, target, nil)
	if err != nil {
		return serviceStatus{}, unavailable(target, "cannot be called")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set(invocationHeader, invocationID)
	request.Header.Set("Accept", "application/json")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return serviceStatus{}, unavailable(target, "did not answer")
	}
	defer func() { _ = response.Body.Close() }()

	if failure := statusFailure(target, response.StatusCode); failure != nil {
		return serviceStatus{}, failure
	}

	// The body is bounded: a service that answered with a stream rather than a
	// status document must not be able to exhaust this process.
	body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return serviceStatus{}, unavailable(target, "stopped part-way through its answer")
	}
	// A body that is not a status document is not a service having a bad
	// moment: the endpoint is answering, and answering with something else.
	// Reported apart from the transient failures above so that the recovery can
	// say what would actually recover.
	var status serviceStatus
	if err := json.Unmarshal(body, &status); err != nil {
		return serviceStatus{}, unreadable(target, "answered with something this module cannot read")
	}
	if strings.TrimSpace(status.Status) == "" {
		return serviceStatus{}, unreadable(target, "answered without a status")
	}
	return status, nil
}

// statusFailure maps a refused or failed answer onto this module's problems.
//
// A service that would not take the access it was given is reported apart from
// a service that failed, and both are product-service failures: the shell
// granted the access, so neither is a broker denial. Keeping them apart from
// each other, and from shell policy, is what lets automation tell "you may not"
// from "it is broken".
func statusFailure(target string, status int) error {
	switch {
	case status == http.StatusOK:
		return nil
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return problem.New(problem.CategoryProductService, "reference.status_access_rejected",
			"the reference status service at "+target+
				" did not accept the access this command was granted").
			WithRecovery("Retry the command. Report the failure if the service keeps refusing valid access.")
	case status == http.StatusNotFound || status == http.StatusMethodNotAllowed || status == http.StatusGone:
		// The host answered and does not serve this path. Retrying cannot
		// change that, so this must not be reported as a service that failed.
		return problem.New(problem.CategoryProductService, "reference.status_not_served",
			"no reference status service is served at "+target).
			WithRecovery("Check the endpoint recorded for the reference product in wso2 identity list. " +
				"Retrying will not change this answer.")
	default:
		return unavailable(target, "could not report its status")
	}
}

// unavailable reports a status service that may answer correctly on another
// attempt: it did not answer, stopped part-way, or failed.
//
// The endpoint is named because the module is the only party that knows which
// URL it called: the shell's diagnostics stop at the access it brokered, and a
// user reading this error would otherwise have to cross-reference
// wso2 identity list against this module's source to find out (#147).
func unavailable(target, what string) problem.Problem {
	return problem.New(problem.CategoryProductService, "reference.status_unavailable",
		"the reference status service at "+target+" "+what).
		WithRecovery("Retry the command. Report the failure if it persists.")
}

// unreadable reports an endpoint that answered with something other than a
// status document.
//
// It carries the same category and exit class as unavailable, because the shell
// granted the access and neither is a broker denial. What it must not carry is
// unavailable's recovery: an endpoint that is not a status service answers the
// same way every time, and "retry the command" would send the user round a loop
// that cannot terminate (#147).
func unreadable(target, what string) problem.Problem {
	return problem.New(problem.CategoryProductService, "reference.status_unavailable",
		"the reference status service at "+target+" "+what).
		WithRecovery("Check that this endpoint is a reference status service; it is recorded for the " +
			"reference product in wso2 identity list. Retrying will not change this answer.")
}
