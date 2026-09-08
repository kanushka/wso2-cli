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

// Package amp is the Agent Manager REST client the amp module speaks through.
//
// It knows URLs and the shape of an error, and nothing about commands or
// results: a handler builds a Client with the endpoint the shell gave it and
// the token the shell brokered, calls one typed function, and turns the
// answer into a result. Every failure is one of four problems a user can act
// on: the deployment refused (with its own words), it could not be reached,
// its certificate is not trusted, or it answered something this module
// cannot read.
package amp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wso2/wso2-cli/sdk/problem"
)

// APIPath is where Agent Manager serves its management API, relative to the
// URL a user records as the product's endpoint. amctl appends the same path.
const APIPath = "/api/v1"

// callTimeout bounds one management call.
const callTimeout = 15 * time.Second

// Client reaches one Agent Manager deployment with one bearer token.
type Client struct {
	// Base is the deployment's URL, as the context's endpoint names it.
	Base string
	// HTTP is what carries the requests; New fills it with HTTPClient().
	HTTP *http.Client
	// Token is the brokered access token.
	Token string
}

// New builds a client for a deployment and a token.
func New(base, token string) *Client {
	return &Client{Base: strings.TrimRight(base, "/"), HTTP: HTTPClient(), Token: token}
}

// Refusal is a non-2xx answer, kept whole so the user sees what the
// deployment said.
type Refusal struct {
	Status int
	Body   string
}

func (r *Refusal) Error() string {
	body := strings.TrimSpace(r.Body)
	if body == "" {
		return fmt.Sprintf("status %d", r.Status)
	}
	return fmt.Sprintf("status %d: %s", r.Status, body)
}

// unreadable marks an answer whose body was not the JSON expected.
type unreadable struct{ err error }

func (u *unreadable) Error() string { return "unreadable answer: " + u.err.Error() }

// Get reads one resource under APIPath into into. path is relative to the
// API root, such as /orgs/acme/projects, and may carry a query string.
func (c *Client) Get(ctx context.Context, path string, into any) error {
	call, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(call, http.MethodGet, c.Base+APIPath+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.Token)
	response, err := c.HTTP.Do(request)
	if err != nil {
		return classifyTransport(c.Base, err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return &Refusal{Status: response.StatusCode, Body: string(body)}
	}
	if into == nil {
		return nil
	}
	if err := json.Unmarshal(body, into); err != nil {
		return &unreadable{err: err}
	}
	return nil
}

// Problem turns a client error into the problem a handler returns. doing is
// what was being attempted, in the user's terms: "the project listing".
func Problem(err error, doing string) problem.Problem {
	var refusal *Refusal
	var bad *unreadable
	var cert *UntrustedCertificate
	switch {
	case errors.As(err, &cert):
		return certificateProblem(cert.Host)
	case errors.As(err, &refusal):
		return problem.New(problem.CategoryProductService, "amp.refused",
			fmt.Sprintf("Agent Manager refused %s: %s", doing, refusal.Error())).
			WithRecovery("Read the deployment's answer above; it names what it did not accept. " +
				"A 401 or 403 usually means the identity's amp product records scopes the deployment " +
				"did not grant this user; a 404 usually means the organization or project name is wrong.")
	case errors.As(err, &bad):
		return problem.New(problem.CategoryProductService, "amp.unreadable",
			fmt.Sprintf("Agent Manager answered %s with something this module cannot read", doing)).
			WithRecovery("Check that the endpoint names an Agent Manager deployment this module version supports.")
	default:
		return problem.New(problem.CategoryProductService, "amp.unavailable",
			fmt.Sprintf("Agent Manager did not answer %s: %v", doing, err)).
			WithRecovery("Check that the deployment is running and the endpoint on this identity reaches it.")
	}
}
