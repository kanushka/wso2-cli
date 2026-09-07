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
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/auth/issuertrust"
)

// TestUntrustedRecognizesAVerificationFailureFromARealDial proves the
// classifier sees through the wrapping net/http puts around a failed
// handshake, rather than matching only the bare crypto errors.
func TestUntrustedRecognizesAVerificationFailureFromARealDial(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = http.DefaultClient.Do(request)
	if err == nil {
		t.Fatal("a self-signed server was trusted without a CA file")
	}
	if !issuertrust.Untrusted(err) {
		t.Fatalf("Untrusted(%v) = false, want true", err)
	}
}

func TestUntrustedMatchesEveryVerificationErrorAndNothingElse(t *testing.T) {
	cases := map[string]struct {
		err  error
		want bool
	}{
		"unknown authority": {err: fmt.Errorf("wrapped: %w", x509.UnknownAuthorityError{}), want: true},
		"hostname":          {err: x509.HostnameError{Host: "idp.example"}, want: true},
		"invalid":           {err: x509.CertificateInvalidError{Reason: x509.Expired}, want: true},
		"tls verification":  {err: &tls.CertificateVerificationError{Err: errors.New("x")}, want: true},
		"connection refused": {
			err: errors.New("dial tcp 127.0.0.1:9443: connect: connection refused"), want: false},
		"nil": {err: nil, want: false},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			if got := issuertrust.Untrusted(testCase.err); got != testCase.want {
				t.Errorf("Untrusted = %v, want %v", got, testCase.want)
			}
		})
	}
}

// TestProblemNamesTheHostAndTheTwoCommands pins the refusal a user reads:
// the host the certificate came from, the exact export and the exact
// environment line with the real host and port substituted, the trust-store
// alternative, and that the variable is read by the shell that runs wso2.
func TestProblemNamesTheHostAndTheTwoCommands(t *testing.T) {
	typed := issuertrust.Problem("https://localhost:9443/oauth2/token")
	if typed.Code != "auth.certificate_untrusted" {
		t.Fatalf("code = %q", typed.Code)
	}
	if !strings.Contains(typed.Message, "localhost:9443") || !strings.Contains(typed.Message, "not trusted") {
		t.Errorf("message does not name the host as untrusted: %q", typed.Message)
	}
	for _, want := range []string{
		"openssl s_client -connect localhost:9443 -showcerts </dev/null 2>/dev/null | awk '/BEGIN CERT/,/END CERT/' > localhost-9443.pem",
		"export WSO2_CA_FILE=$PWD/localhost-9443.pem",
		"trust store",
		"shell that runs wso2",
	} {
		if !strings.Contains(typed.Recovery, want) {
			t.Errorf("recovery lacks %q:\n%s", want, typed.Recovery)
		}
	}
}

// TestProblemFillsInTheDefaultPort covers an issuer that states no port: the
// commands must still connect somewhere real.
func TestProblemFillsInTheDefaultPort(t *testing.T) {
	typed := issuertrust.Problem("https://api.asgardeo.io/t/acme/oauth2/token")
	if !strings.Contains(typed.Recovery, "-connect api.asgardeo.io:443 ") {
		t.Errorf("recovery does not connect to port 443:\n%s", typed.Recovery)
	}
	if !strings.Contains(typed.Recovery, "api.asgardeo.io-443.pem") {
		t.Errorf("recovery does not name a file after the host and port:\n%s", typed.Recovery)
	}
}
