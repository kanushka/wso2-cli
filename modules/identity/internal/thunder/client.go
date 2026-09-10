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

package thunder

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

// responseLimit bounds what is read from a management response. A listing of
// users is kilobytes; anything approaching this is not one, and reading it
// into memory would be the deployment's decision rather than this module's.
const responseLimit = 8 << 20

// requestTimeout bounds one management call.
const requestTimeout = 30 * time.Second

// Client calls one ThunderID deployment's management API.
type Client struct {
	// Endpoint is the deployment's base URL, as the context records it.
	Endpoint string
	// Token is the access the shell brokered for this invocation.
	Token string
	// HTTP serves the calls. A nil value uses a client bounded by
	// requestTimeout.
	HTTP *http.Client
}

// Failure is a management call the deployment refused, carried far enough for
// a handler to state it in its own terms.
type Failure struct {
	// Status is the HTTP status the deployment answered with.
	Status int
	// Code is the deployment's own error code, empty when it stated none.
	Code string
	// Message is the deployment's own description, empty when it stated none.
	Message string
}

func (f Failure) Error() string {
	if f.Code == "" {
		return fmt.Sprintf("the deployment answered %d", f.Status)
	}
	return fmt.Sprintf("the deployment answered %d (%s)", f.Status, f.Code)
}

// Get reads one management resource into out.
func (c Client) Get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

// Post creates one management resource, reading the created resource into out.
func (c Client) Post(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPost, path, body, out)
}

func (c Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("thunder: encoding the request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method,
		strings.TrimSuffix(c.Endpoint, "/")+path, reader)
	if err != nil {
		return fmt.Errorf("thunder: building the request: %w", err)
	}
	// The brokered token and nothing else. This module never holds a
	// credential of its own, so there is no other value this header could
	// carry.
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
		return fmt.Errorf("thunder: calling %s: %w", path, err)
	}
	defer func() { _ = answer.Body.Close() }()
	read, err := io.ReadAll(io.LimitReader(answer.Body, responseLimit))
	if err != nil {
		return fmt.Errorf("thunder: reading the answer to %s: %w", path, err)
	}
	if answer.StatusCode < 200 || answer.StatusCode > 299 {
		return failure(answer.StatusCode, read)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(read, out); err != nil {
		return fmt.Errorf("thunder: reading the answer to %s: %w", path, err)
	}
	return nil
}

// failure reads whatever the deployment said about a refusal. Thunder states a
// code and a message object; a deployment that states neither still produces a
// Failure, because the status alone is worth reporting.
func failure(status int, body []byte) error {
	var stated struct {
		Code        string          `json:"code"`
		Message     json.RawMessage `json:"message"`
		Description json.RawMessage `json:"description"`
	}
	_ = json.Unmarshal(body, &stated)
	return Failure{Status: status, Code: stated.Code, Message: readMessage(stated.Message)}
}

// readMessage reads Thunder's message member, which is a plain string on some
// endpoints and an object carrying a default value on others.
func readMessage(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var plain string
	if json.Unmarshal(raw, &plain) == nil {
		return plain
	}
	var structured struct {
		DefaultValue string `json:"defaultValue"`
	}
	if json.Unmarshal(raw, &structured) == nil {
		return structured.DefaultValue
	}
	return ""
}
