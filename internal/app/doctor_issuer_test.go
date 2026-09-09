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

package app_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
)

// reachableCatalog points the catalog check at a local origin that answers,
// so an --online run's exit status is decided by the issuer check alone.
func reachableCatalog(t *testing.T) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+catalog.IndexPath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"schemaVersion":1,"modules":[]}`))
	}))
	t.Cleanup(server.Close)
	t.Setenv(catalog.OriginEnvVar, server.URL)
}

// healthyShellAgainst is a shell whose selected context names issuer, with
// the session the offline checks look for already stored, so an --online run
// against it has only the network checks left to decide.
func healthyShellAgainst(t *testing.T, issuer string) (app.Shell, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	shell, out, errOut := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	seeded := identityOnlyDocument()
	seeded.Identities[0].Auth.Issuer = issuer
	seeded.DefaultContext = "acme"
	seeded.Contexts = []contexts.Context{{Name: "acme", Identity: "acme-cloud"}}
	installLogin(t, shell, seeded)
	store := session.Store{StateRoot: shell.StateRoot}
	if err := store.Save("acme-cloud", session.Session{Issuer: issuer, RefreshToken: "rt-1"}); err != nil {
		t.Fatalf("seed a session: %v", err)
	}
	return shell, out, errOut
}

// TestDoctorOnlineReportsAnUntrustedIssuerCertificate is the defect wso2
// doctor missed: every offline check passed against an API Manager whose
// self-signed certificate no product command could get past. Under --online
// the issuer check dials the selected identity's issuer and reports the
// certificate, with the same recovery a product command gives.
func TestDoctorOnlineReportsAnUntrustedIssuerCertificate(t *testing.T) {
	keyring.MockInit()
	reachableCatalog(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	shell, out, errOut := healthyShellAgainst(t, server.URL)
	if code := shell.Run([]string{"doctor", "--online", "--output", "json"}); code != exit.AuthPolicy {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.AuthPolicy, errOut)
	}
	requireRefusal(t, errOut.String(), "auth.certificate_untrusted")
	report := decodeDoctorReport(t, out.Bytes())
	finding := report.findingFor(t, "issuer")
	host := strings.TrimPrefix(server.URL, "https://")
	if finding.Status != "fail" || !strings.Contains(finding.Detail, host) {
		t.Errorf("issuer finding = %+v, want a failure naming %s", finding, host)
	}
	if !strings.Contains(finding.Recovery, "openssl s_client -connect "+host+" -showcerts") ||
		!strings.Contains(finding.Recovery, "export WSO2_CA_FILE=") {
		t.Errorf("issuer recovery does not give the two commands:\n%s", finding.Recovery)
	}
	if catalogFinding := report.findingFor(t, "catalog"); catalogFinding.Status != "pass" {
		t.Errorf("catalog finding = %+v, want pass so the issuer decides the exit", catalogFinding)
	}
}

// TestDoctorOnlinePassesTheIssuerCheckAgainstAReadableIssuer proves the check
// is a real discovery fetch: an issuer that serves its configuration passes.
func TestDoctorOnlinePassesTheIssuerCheckAgainstAReadableIssuer(t *testing.T) {
	keyring.MockInit()
	reachableCatalog(t)
	issuer := fakeissuer.New(t, fakeissuer.Options{})

	shell, out, errOut := healthyShellAgainst(t, issuer.URL)
	if code := shell.Run([]string{"doctor", "--online", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	finding := decodeDoctorReport(t, out.Bytes()).findingFor(t, "issuer")
	if finding.Status != "pass" || !strings.Contains(finding.Detail, issuer.URL) {
		t.Errorf("issuer finding = %+v, want pass naming %s", finding, issuer.URL)
	}
}

// TestDoctorOnlineReportsAnUnreachableIssuerAsDiscoveryFailed keeps the
// existing code for every failure that is not the certificate.
func TestDoctorOnlineReportsAnUnreachableIssuerAsDiscoveryFailed(t *testing.T) {
	keyring.MockInit()
	reachableCatalog(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	shell, out, errOut := healthyShellAgainst(t, server.URL)
	if code := shell.Run([]string{"doctor", "--online", "--output", "json"}); code != exit.AuthPolicy {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.AuthPolicy, errOut)
	}
	requireRefusal(t, errOut.String(), "auth.discovery_failed")
	if finding := decodeDoctorReport(t, out.Bytes()).findingFor(t, "issuer"); finding.Status != "fail" {
		t.Errorf("issuer finding = %+v, want fail", finding)
	}
}

// TestDoctorWithoutOnlineNeverAddsAnIssuerCheck keeps the offline contract:
// without --online the issuer is never dialled and never reported on.
func TestDoctorWithoutOnlineNeverAddsAnIssuerCheck(t *testing.T) {
	keyring.MockInit()
	shell, out, _ := newShell(t)
	shell.Run([]string{"doctor", "--output", "json"})
	for _, finding := range decodeDoctorReport(t, out.Bytes()).Checks {
		if finding.Check == "issuer" {
			t.Fatalf("an issuer finding is present without --online: %+v", finding)
		}
	}
}
