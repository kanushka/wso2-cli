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

package httplog

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/sdk/diag"
)

func TestTransportLogsEachRequestWithoutItsSecrets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer server.Close()

	var captured bytes.Buffer
	ctx := diag.WithLogger(context.Background(), diag.New(&captured, false))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/apis?apiKey=k-must-never-appear", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer t-must-never-appear")
	response, err := (&http.Client{Transport: Transport(nil)}).Do(request)
	if err != nil {
		t.Fatalf("the request failed: %v", err)
	}
	_ = response.Body.Close()

	logged := captured.String()
	for _, expected := range []string{"sent a request", "method=GET", server.URL + "/apis", "status=418", "duration_ms="} {
		if !strings.Contains(logged, expected) {
			t.Errorf("the request log is missing %q:\n%s", expected, logged)
		}
	}
	for _, secret := range []string{"k-must-never-appear", "t-must-never-appear"} {
		if strings.Contains(logged, secret) {
			t.Errorf("the request log carries %q:\n%s", secret, logged)
		}
	}
}

func TestTransportLogsAFailureWithoutTheQuery(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	target := server.URL
	server.Close()

	var captured bytes.Buffer
	ctx := diag.WithLogger(context.Background(), diag.New(&captured, false))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target+"/apis?apiKey=k-must-never-appear", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&http.Client{Transport: Transport(nil)}).Do(request); err == nil {
		t.Fatal("a request to a closed server succeeded")
	}
	if !strings.Contains(captured.String(), "a request failed") {
		t.Fatalf("the failure was not logged:\n%s", &captured)
	}
	if strings.Contains(captured.String(), "k-must-never-appear") {
		t.Fatalf("the failure log carries the query:\n%s", &captured)
	}
}

type failingTransport struct{ err error }

func (f failingTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, f.err }

// A custom transport is free to quote the URL in its error. The caller still
// gets that error as it was returned; the log gets it without the query.
func TestTransportReturnsTheInnerErrorAndLogsItWithoutTheQuery(t *testing.T) {
	inner := errors.New(`proxy refused "https://user:pw@api.example.test/apis?apiKey=k-must-never-appear#frag"`)
	var captured bytes.Buffer
	ctx := diag.WithLogger(context.Background(), diag.New(&captured, false))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.test/apis", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, returned := Transport(failingTransport{err: inner}).RoundTrip(request)
	if returned != inner {
		t.Fatalf("RoundTrip returned %v, want the inner transport's error itself", returned)
	}
	for _, leaked := range []string{"k-must-never-appear", "user:pw", "#frag"} {
		if strings.Contains(captured.String(), leaked) {
			t.Fatalf("the failure log carries %q:\n%s", leaked, &captured)
		}
	}
	if !strings.Contains(captured.String(), "proxy refused") || !strings.Contains(captured.String(), "https://api.example.test/apis") {
		t.Fatalf("the failure log lost its cause:\n%s", &captured)
	}
}
