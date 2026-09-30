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

package trustedhttp_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/auth/trustedhttp"
)

// A 307 resends the body, so a token request redirected off HTTPS would carry
// its secret in the clear. The redirect is refused before any connection to
// the plaintext host is made.
func TestARedirectToPlainHTTPIsRefused(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://issuer.example/token", http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	_, err := trustedhttp.Client(server.Client()).Post(server.URL, "application/x-www-form-urlencoded",
		strings.NewReader("refresh_token=secret"))
	if err == nil || !strings.Contains(err.Error(), "not served over HTTPS") {
		t.Fatalf("err = %v, want a refused redirect", err)
	}
}

// Redirects that stay on HTTPS, or land on loopback, are followed as before.
func TestARedirectToHTTPSOrLoopbackIsFollowed(t *testing.T) {
	loopback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer loopback.Close()
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/https":
			http.Redirect(w, r, server.URL+"/done", http.StatusTemporaryRedirect)
		case "/loopback":
			http.Redirect(w, r, loopback.URL, http.StatusTemporaryRedirect)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	for _, path := range []string{"/https", "/loopback"} {
		response, err := trustedhttp.Client(server.Client()).Get(server.URL + path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusNoContent {
			t.Fatalf("%s: status = %d, want %d", path, response.StatusCode, http.StatusNoContent)
		}
	}
}

// The client's own redirect policy still runs after the HTTPS rule.
func TestTheClientsOwnRedirectPolicyStillApplies(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, server.URL+"/next", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	stop := errors.New("own policy")
	base := server.Client()
	base.CheckRedirect = func(*http.Request, []*http.Request) error { return stop }

	_, err := trustedhttp.Client(base).Get(server.URL + "/start")
	if !errors.Is(err, stop) {
		t.Fatalf("err = %v, want the client's own policy error", err)
	}
}

// A nil client stands for http.DefaultClient, which is left unchanged.
func TestANilClientIsTheDefaultClientGuarded(t *testing.T) {
	client := trustedhttp.Client(nil)
	if client == http.DefaultClient || client.CheckRedirect == nil {
		t.Fatal("want a guarded copy of http.DefaultClient")
	}
	if http.DefaultClient.CheckRedirect != nil {
		t.Fatal("http.DefaultClient was modified")
	}
}
