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

	"github.com/wso2/wso2-cli/sdk/problem"
)

// whoamiPath is where the status service reports the claims it verified. Like
// statusPath it is knowledge this module carries rather than imports.
const whoamiPath = "/whoami"

// brokeredAccess is what the audience verified about the access it was shown.
type brokeredAccess struct {
	Organization string `json:"organization"`
	Audiences    string `json:"audiences"`
	Scopes       string `json:"scopes"`
	Invocation   string `json:"invocation"`
	BoundTo      string `json:"boundTo"`
}

// readWhoami asks the audience what the presented access says.
//
// The answer comes from the service rather than from this process on purpose. A
// module is given a token and a way to spend it, never a way to read it: it
// holds opaque access material, and the only party that can say what that
// material conveys is the audience that verifies it. Introspecting the token
// here would work for the one token format this fixture mints and would be the
// wrong shape for every real one.
func readWhoami(ctx context.Context, endpoint, invocationID, token string) (brokeredAccess, error) {
	if endpoint == "" {
		return brokeredAccess{}, problem.New(problem.CategoryUsage, "reference.no_endpoint",
			"the selected context does not name a reference status service").
			WithRecovery("Select a context whose endpoint names the local reference status service.")
	}
	target, err := url.JoinPath(endpoint, whoamiPath)
	if err != nil {
		return brokeredAccess{}, problem.New(problem.CategoryUsage, "reference.unreadable_endpoint",
			"the selected context names an endpoint this module cannot call").
			WithRecovery("Select a context whose endpoint is an absolute HTTP URL.")
	}

	call, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(call, http.MethodGet, target, nil)
	if err != nil {
		return brokeredAccess{}, unavailable("the reference status service cannot be called")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set(invocationHeader, invocationID)
	request.Header.Set("Accept", "application/json")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return brokeredAccess{}, unavailable("the reference status service did not answer")
	}
	defer func() { _ = response.Body.Close() }()

	if failure := statusFailure(response.StatusCode); failure != nil {
		return brokeredAccess{}, failure
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return brokeredAccess{}, unavailable("the reference status service stopped part-way through its answer")
	}
	var granted brokeredAccess
	if err := json.Unmarshal(body, &granted); err != nil {
		return brokeredAccess{}, unavailable("the reference status service answered with something this module cannot read")
	}
	if strings.TrimSpace(granted.Audiences) == "" {
		return brokeredAccess{}, unavailable("the reference status service reported no verified audience")
	}
	return granted, nil
}
