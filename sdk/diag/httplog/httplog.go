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

// Package httplog logs a module's HTTP requests to the diagnostic log its
// context carries. It is apart from diag so that what imports diag for
// redaction alone does not import an HTTP client.
package httplog

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/wso2/wso2-cli/sdk/diag"
)

// Transport wraps inner, or http.DefaultTransport when inner is nil, so that
// every request made with a context from the SDK logs its method, URL, status
// and duration to that context's logger.
//
// The URL is logged without its query and user information, and no header or
// body is logged at all: a query can carry an API key, and a header carries
// the bearer token.
func Transport(inner http.RoundTripper) http.RoundTripper {
	if inner == nil {
		inner = http.DefaultTransport
	}
	return loggingTransport{inner: inner}
}

type loggingTransport struct {
	inner http.RoundTripper
}

func (t loggingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	log := diag.FromContext(request.Context())
	started := time.Now()
	response, err := t.inner.RoundTrip(request)
	attributes := []any{
		"method", request.Method,
		"url", loggableURL(request.URL),
		"duration_ms", time.Since(started).Milliseconds(),
	}
	if err != nil {
		// A *url.Error repeats the full URL, query included, so only its cause
		// is logged.
		var wrapped *url.Error
		if errors.As(err, &wrapped) {
			err = wrapped.Err
		}
		log.Debug("a request failed", append(attributes, "error", err.Error())...)
		return response, err
	}
	log.Debug("sent a request", append(attributes, "status", response.StatusCode)...)
	return response, nil
}

func loggableURL(target *url.URL) string {
	if target == nil {
		return ""
	}
	trimmed := *target
	trimmed.User = nil
	trimmed.RawQuery = ""
	trimmed.ForceQuery = false
	trimmed.Fragment = ""
	trimmed.RawFragment = ""
	return trimmed.String()
}
