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

// Package apim is the API Manager REST client the apim module speaks through.
//
// It knows URLs, bodies and the shape of an error, and nothing about commands
// or results: a handler builds a Client with the endpoint the shell gave it
// and the token the shell brokered, calls one typed function, and turns the
// answer into a result. Every failure is one of three problems a user can act
// on: the deployment refused (with its own words), the deployment could not
// be reached, or it answered something this module cannot read.
package apim

import (
	"bytes"
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

// callTimeout bounds one management call.
const callTimeout = 15 * time.Second

// Client reaches one API Manager deployment with one bearer token.
type Client struct {
	// Base is the deployment's URL, as the context's endpoint names it.
	Base string
	// HTTP is what carries the requests; New fills it with HTTPClient().
	HTTP *http.Client
	// Token is the brokered access token; empty for unauthenticated calls.
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

// Get reads one resource into into.
func (c *Client) Get(ctx context.Context, path string, into any) error {
	return c.do(ctx, http.MethodGet, path, nil, into)
}

// Post sends body as JSON and reads the answer into into, which may be nil.
func (c *Client) Post(ctx context.Context, path string, body, into any) error {
	return c.do(ctx, http.MethodPost, path, body, into)
}

func (c *Client) do(ctx context.Context, method, path string, body, into any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	call, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(call, method, c.Base+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		request.Header.Set("Authorization", "Bearer "+c.Token)
	}
	return c.read(request, into)
}

// Problem turns a client error into the problem a handler returns. doing is
// what was being attempted, in the user's terms: "the role creation".
func Problem(err error, doing string) problem.Problem {
	var refusal *Refusal
	var bad *unreadable
	switch {
	case errors.As(err, &refusal):
		return problem.New(problem.CategoryProductService, "apim.refused",
			fmt.Sprintf("API Manager refused %s: %s", doing, refusal.Error())).
			WithRecovery("Read the deployment's answer above; it names what it did not accept.")
	case errors.As(err, &bad):
		return problem.New(problem.CategoryProductService, "apim.unreadable",
			fmt.Sprintf("API Manager answered %s with something this module cannot read", doing)).
			WithRecovery("Check that the endpoint names a API Manager deployment this module version supports.")
	default:
		return problem.New(problem.CategoryProductService, "apim.unavailable",
			fmt.Sprintf("API Manager did not answer %s: %v", doing, err)).
			WithRecovery("Check that the deployment is running and the endpoint on this identity reaches it.")
	}
}

// IsRefusalContaining reports whether err is a refusal whose body carries the
// phrase, filling refusal when it is. It is how a handler tells "already
// exists" from a real failure without parsing the deployment's error shapes.
func IsRefusalContaining(err error, refusal **Refusal, phrase string) bool {
	if !errors.As(err, refusal) {
		return false
	}
	return strings.Contains((*refusal).Body, phrase)
}
