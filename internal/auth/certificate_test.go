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
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
)

// TestAnUntrustedIssuerCertificateIsNamedRatherThanReportedAsUnreachable is
// the defect seen against a fresh API Manager: its self-signed certificate
// failed discovery, and the shell said only that it could not read the OpenID
// configuration, so nothing pointed at the certificate. The refusal must name
// the certificate, the host it came from, and how to trust it.
func TestAnUntrustedIssuerCertificateIsNamedRatherThanReportedAsUnreachable(t *testing.T) {
	deployment := deployInline(t, fakeissuer.Options{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("the shell trusted a self-signed certificate without being told to")
		http.NotFound(w, r)
	}))
	defer server.Close()

	broker := deployment.broker(t)
	broker.Selection.Identity.Auth.Issuer = server.URL
	// The process-wide client trusts only the system roots, which is what a
	// machine without WSO2_CA_FILE dials with.
	broker.HTTPClient = http.DefaultClient

	_, err := broker.Acquire(declaredRequest())
	var denial auth.Denial
	if !errors.As(err, &denial) {
		t.Fatalf("Acquire returned %v, want a denial", err)
	}
	reported := denial.Reported()
	if reported.Code != "auth.certificate_untrusted" {
		t.Fatalf("code = %q, want auth.certificate_untrusted:\n%s\n%s",
			reported.Code, reported.Message, reported.Recovery)
	}
	host := strings.TrimPrefix(server.URL, "https://")
	if !strings.Contains(reported.Message, host) {
		t.Errorf("the message does not name the host %q: %q", host, reported.Message)
	}
	if !strings.Contains(reported.Recovery, "openssl s_client -connect "+host+" -showcerts") ||
		!strings.Contains(reported.Recovery, "export WSO2_CA_FILE=") {
		t.Errorf("the recovery does not give the export and the environment line for %q:\n%s",
			host, reported.Recovery)
	}
}
