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

package issuertrust_test

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/wso2/wso2-cli/internal/auth/issuertrust"
)

func TestPlaintextRefusesAnyDiscoveredEndpointOffLoopbackHTTP(t *testing.T) {
	secure := `{"issuer": "https://id.example.test", "token_endpoint": "https://id.example.test/token",
		"jwks_uri": "https://id.example.test/jwks"}`
	for document, want := range map[string]bool{
		secure: false,
		`{"issuer": "http://localhost:9443", "token_endpoint": "http://127.0.0.1:9443/token"}`:    false,
		`{"issuer": "https://id.example.test", "token_endpoint": "http://id.example.test/token"}`: true,
		`{"issuer": "https://id.example.test", "jwks_uri": "http://id.example.test/jwks"}`:        true,
		`{"issuer": "https://id.example.test", "device_authorization_endpoint": "http://d.test"}`: true,
		`{"issuer": "https://id.example.test", "revocation_endpoint": "http://r.test/revoke"}`:    true,
		`{"issuer": "https://id.example.test", "end_session_endpoint": "http://e.test/logout"}`:   true,
		`{"issuer": "http://id.example.test"}`:                                                    true,
		`{"issuer": "https://id.example.test", "token_endpoint": 7}`:                              true,
	} {
		claims := func(into any) error { return json.Unmarshal([]byte(document), into) }
		if got := issuertrust.Plaintext(claims); got != want {
			t.Errorf("Plaintext(%s) = %v, want %v", document, got, want)
		}
	}
}

func TestSecureAllowsPlainHTTPOnlyOnLoopback(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://login.example.test/oauth2/token": true,
		"HTTPS://login.example.test":              true,
		"http://login.example.test/oauth2/token":  false,
		"http://10.0.0.5:9443":                    false,
		"http://192.168.1.10":                     false,
		"http://localhost.example.test":           false,
		"http://127.0.0.1.example.test":           false,
		"http://localhost:9443/oauth2/token":      true,
		"http://LocalHost:9443":                   true,
		"http://127.0.0.1:8090":                   true,
		"http://127.255.255.254":                  true,
		"http://[::1]:9443":                       true,
		"ftp://login.example.test":                false,
		"login.example.test":                      false,
	} {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		if got := issuertrust.Secure(parsed); got != want {
			t.Errorf("Secure(%q) = %v, want %v", raw, got, want)
		}
	}
}
