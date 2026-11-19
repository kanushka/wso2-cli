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

package auth_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/contexts"
)

// plaintextRecorder answers any request to plaintext.example itself, so a
// request that reached it is counted rather than failing on DNS.
type plaintextRecorder struct {
	reached *atomic.Int32
}

func (r plaintextRecorder) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Hostname() == "plaintext.example" {
		r.reached.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}")),
			Header: http.Header{}, Request: request}, nil
	}
	return http.DefaultTransport.RoundTrip(request)
}

func TestCheckClientGrantDoesNotFollowARedirectOffHTTPS(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer": server.URL, "token_endpoint": server.URL + "/token",
				"authorization_endpoint": server.URL + "/authorize", "jwks_uri": server.URL + "/jwks",
			})
		case "/token":
			http.Redirect(w, r, "http://plaintext.example/token", http.StatusTemporaryRedirect)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	target := contexts.Context{
		Name: "ci", Type: contexts.TypeCloud,
		Login: contexts.Login{Kind: contexts.KindClientCredentials, Issuer: server.URL, ClientID: "ci-client",
			ClientSecretVariable: "WSO2_CLIENT_SECRET"},
		Products: map[string]contexts.Product{"apim": {Audience: "urn:wso2:cloud", Scopes: []string{"ap:api:view"}}},
	}
	var reached atomic.Int32
	client := &http.Client{Transport: plaintextRecorder{reached: &reached}}
	_, err := auth.CheckClientGrant(context.Background(), client,
		contexts.Selection{Context: target, Identity: target.Account()}, "apim", "s3cret")
	if err == nil {
		t.Error("a token endpoint that redirected off HTTPS was answered as a grant")
	}
	if reached.Load() != 0 {
		t.Error("the client secret was sent to a plaintext URL")
	}
}
