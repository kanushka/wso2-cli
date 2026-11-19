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

package diag

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

type loggerKey struct{}

// WithLogger returns ctx carrying log, for code that is handed a context but
// not a Request: an HTTP transport, a client several calls deep.
func WithLogger(ctx context.Context, log *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, log)
}

// FromContext returns the logger ctx carries, or one that discards when it
// carries none, so a caller never has to check.
func FromContext(ctx context.Context) *slog.Logger {
	if log, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok && log != nil {
		return log
	}
	return Discard()
}

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
	log := FromContext(request.Context())
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
