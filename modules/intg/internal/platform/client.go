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

// Package platform calls the WSO2 Cloud Integration Platform API, the
// ipaas-service that the Integration Platform console reads.
package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// responseLimit bounds what is read from one answer. The API's largest answers
// are log listings capped at a few thousand lines; anything approaching this
// is not one of them.
const responseLimit = 8 << 20

// requestTimeout bounds one call.
const requestTimeout = 30 * time.Second

// Client calls one Integration Platform API.
type Client struct {
	// Endpoint is the API's base URL as the context records it, including the
	// path the API is served under.
	Endpoint string
	// Token is the access the shell brokered for this invocation.
	Token string
	// HTTP serves the calls. A nil value uses a client bounded by
	// requestTimeout.
	HTTP *http.Client
}

// Failure is a call the API refused, carried far enough for a handler to
// state it in its own terms.
type Failure struct {
	// Status is the HTTP status the API answered with.
	Status int
	// Code is the API's short error name, such as "Not Found"; empty when it
	// stated none.
	Code string
	// Message is the API's own description; empty when it stated none.
	Message string
}

func (f Failure) Error() string {
	if f.Code == "" {
		return fmt.Sprintf("the API answered %d", f.Status)
	}
	return fmt.Sprintf("the API answered %d (%s)", f.Status, f.Code)
}

// Get reads one resource into out.
func (c Client) Get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

// Post creates one resource from body, reading the answer into out.
func (c Client) Post(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPost, path, body, out)
}

// Put replaces or changes one resource from body, reading the answer into out.
func (c Client) Put(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPut, path, body, out)
}

// Delete removes one resource. The API answers 204 with no body, so there is
// nothing to read.
func (c Client) Delete(ctx context.Context, path string) error {
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("platform: encoding the request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method,
		strings.TrimSuffix(c.Endpoint, "/")+path, reader)
	if err != nil {
		return fmt.Errorf("platform: building the request: %w", err)
	}
	// The brokered token and nothing else: this module holds no credential of
	// its own, so there is no other value this header could carry.
	request.Header.Set("Authorization", "Bearer "+c.Token)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: requestTimeout}
	}
	answer, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("platform: calling %s: %w", path, err)
	}
	defer func() { _ = answer.Body.Close() }()
	read, err := io.ReadAll(io.LimitReader(answer.Body, responseLimit))
	if err != nil {
		return fmt.Errorf("platform: reading the answer to %s: %w", path, err)
	}
	if answer.StatusCode < 200 || answer.StatusCode > 299 {
		return failure(answer.StatusCode, read)
	}
	if out == nil || len(read) == 0 {
		return nil
	}
	if err := json.Unmarshal(read, out); err != nil {
		return fmt.Errorf("platform: reading the answer to %s: %w", path, err)
	}
	return nil
}

// failure reads the API's ErrorResponse, {"error": ..., "message": ...}. An
// answer that states neither still produces a Failure, because the status
// alone is worth reporting.
func failure(status int, body []byte) error {
	var stated struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &stated)
	return Failure{Status: status, Code: stated.Error, Message: stated.Message}
}
