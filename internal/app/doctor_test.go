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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// doctorReport mirrors what wso2 doctor --output json publishes, so a test can
// decode it without depending on the command's own unexported type.
type doctorReport struct {
	Checks []struct {
		Check    string `json:"check"`
		Status   string `json:"status"`
		Detail   string `json:"detail"`
		Recovery string `json:"recovery,omitempty"`
	} `json:"checks"`
}

// findingFor returns the one check by name, failing the test if it is absent.
func (r doctorReport) findingFor(t *testing.T, check string) struct {
	Check    string `json:"check"`
	Status   string `json:"status"`
	Detail   string `json:"detail"`
	Recovery string `json:"recovery,omitempty"`
} {
	t.Helper()
	for _, finding := range r.Checks {
		if finding.Check == check {
			return finding
		}
	}
	t.Fatalf("no %q check in the report: %+v", check, r.Checks)
	panic("unreached")
}

// decodeDoctorReport parses wso2 doctor --output json.
func decodeDoctorReport(t *testing.T, rendered []byte) doctorReport {
	t.Helper()
	var report doctorReport
	if err := json.Unmarshal(rendered, &report); err != nil {
		t.Fatalf("the output is not one JSON document: %v\n%s", err, rendered)
	}
	return report
}

// installMalformedDocument writes a context document this shell cannot decode,
// without going through a writer: no writer in the repository produces one.
func installMalformedDocument(t *testing.T, shell app.Shell) {
	t.Helper()
	path := contexts.Path(shell.StateRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("seed a malformed document: %v", err)
	}
}

// TestDoctorOnAnUnconfiguredMachineExitsCleanly is #121's core requirement: a
// machine nobody has configured yet is reported as unconfigured, not as
// broken.
func TestDoctorOnAnUnconfiguredMachineExitsCleanly(t *testing.T) {
	shell, out, errOut := newShell(t)

	if code := shell.Run([]string{"doctor", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	report := decodeDoctorReport(t, out.Bytes())
	for _, check := range []string{"context", "secure-store", "session"} {
		finding := report.findingFor(t, check)
		if finding.Status != "not-applicable" {
			t.Errorf("%s check = %q, want not-applicable on an unconfigured machine", check, finding.Status)
		}
	}
	if strings.Contains(strings.ToLower(out.String()), "broken") {
		t.Errorf("an unconfigured machine is reported as broken:\n%s", out)
	}
}

// TestDoctorOnAnUnconfiguredMachineTableModeSaysSo proves the table rendering
// carries the same fact as the JSON rendering, per constraint 6.
func TestDoctorOnAnUnconfiguredMachineTableModeSaysSo(t *testing.T) {
	shell, out, errOut := newShell(t)

	if code := shell.Run([]string{"doctor"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	for _, check := range []string{"context", "secure-store", "session"} {
		if !strings.Contains(out.String(), check) {
			t.Errorf("table output does not mention the %q check:\n%s", check, out)
		}
	}
	if !strings.Contains(out.String(), "not-applicable") {
		t.Errorf("table output does not report not-applicable:\n%s", out)
	}
}

// TestDoctorReportsAMalformedDocumentAsAFailingContextCheck covers item 1: a
// document that fails to decode is a failing context check exiting 64, the
// contexts package's own exit class for a malformed document.
func TestDoctorReportsAMalformedDocumentAsAFailingContextCheck(t *testing.T) {
	keyring.MockInit()
	shell, out, errOut := newShell(t)
	installMalformedDocument(t, shell)

	if code := shell.Run([]string{"doctor", "--output", "json"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want %d (usage); stderr: %s", code, exit.Usage, errOut)
	}
	report := decodeDoctorReport(t, out.Bytes())
	if finding := report.findingFor(t, "context"); finding.Status != "fail" {
		t.Errorf("context check = %q, want fail for a malformed document", finding.Status)
	}
	requireRefusal(t, errOut.String(), "contexts.document_malformed")
}

// TestDoctorRanksTheDocumentAboveAnAbsentSession pins R1: when the document is
// invalid (exit.Usage, 64) and the session is absent (exit.AuthPolicy, 77) both
// fail in the same run, the document's class decides the exit status because
// R1 ranks it above session, not because 64 is the smaller number. Reversing
// the rank, or picking the largest class, both make this test fail: it is
// the test that pins the ranking rather than leaving it a comment.
func TestDoctorRanksTheDocumentAboveAnAbsentSession(t *testing.T) {
	keyring.MockInit()
	shell, out, errOut := newShell(t)
	installMalformedDocument(t, shell)
	// No session is seeded anywhere in the keyring, so whatever reference the
	// session check resolves to, Store.Load reports it absent.

	if code := shell.Run([]string{"doctor", "--output", "json"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want %d (usage, the document's own class); stderr: %s",
			code, exit.Usage, errOut)
	}
	report := decodeDoctorReport(t, out.Bytes())
	if finding := report.findingFor(t, "context"); finding.Status != "fail" {
		t.Errorf("context check = %q, want fail", finding.Status)
	}
	if finding := report.findingFor(t, "session"); finding.Status != "fail" {
		t.Errorf("session check = %q, want fail: both checks must genuinely fail for this test to prove anything", finding.Status)
	}
	if finding := report.findingFor(t, "secure-store"); finding.Status != "pass" {
		t.Errorf("secure-store check = %q, want pass", finding.Status)
	}
	// The rendered problem is the document's, not the session's: proof that the
	// exit status followed R1's rank and not the session failure that also
	// occurred.
	requireRefusal(t, errOut.String(), "contexts.document_malformed")
	if strings.Contains(errOut.String(), "auth.login_required") {
		t.Errorf("stderr names the session failure instead of the higher-ranked document failure:\n%s", errOut)
	}
}

// TestDoctorHappyPathPassesEveryCheck proves a fully healthy machine passes
// every check and exits 0, and that both renderings agree on the set of
// checks that ran.
func TestDoctorHappyPathPassesEveryCheck(t *testing.T) {
	keyring.MockInit()
	shell, out, errOut := newShell(t)
	seeded := identityOnlyDocument()
	seeded.DefaultContext = "acme"
	seeded.Contexts = []contexts.Context{{Name: "acme", Identity: "acme-cloud"}}
	installLogin(t, shell, seeded)
	store := session.Store{StateRoot: shell.StateRoot}
	if err := store.Save("acme-cloud", session.Session{
		Issuer:       "https://idp.example",
		RefreshToken: "rt-1",
	}); err != nil {
		t.Fatalf("seed a session: %v", err)
	}

	if code := shell.Run([]string{"doctor", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	report := decodeDoctorReport(t, out.Bytes())
	jsonChecks := map[string]bool{}
	for _, check := range []string{"context", "secure-store", "session"} {
		finding := report.findingFor(t, check)
		if finding.Status != "pass" {
			t.Errorf("%s check = %q, want pass on a healthy machine", check, finding.Status)
		}
		jsonChecks[check] = true
	}

	tableShell, tableOut, tableErrOut := newShell(t)
	installLogin(t, tableShell, seeded)
	tableStore := session.Store{StateRoot: tableShell.StateRoot}
	if err := tableStore.Save("acme-cloud", session.Session{
		Issuer: "https://idp.example", RefreshToken: "rt-1",
	}); err != nil {
		t.Fatalf("seed a session: %v", err)
	}
	if code := tableShell.Run([]string{"doctor"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, tableErrOut)
	}
	for check := range jsonChecks {
		if !strings.Contains(tableOut.String(), check) {
			t.Errorf("table rendering omits the %q check that JSON reported:\n%s", check, tableOut)
		}
	}
}

// TestDoctorJSONCarriesEveryFindingOnAFailingRun proves a caller can read the
// findings off a failing run: --output json is not suppressed by a nonzero
// exit.
func TestDoctorJSONCarriesEveryFindingOnAFailingRun(t *testing.T) {
	keyring.MockInit()
	shell, out, errOut := newShell(t)
	installMalformedDocument(t, shell)

	if code := shell.Run([]string{"doctor", "--output", "json"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.Usage, errOut)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("the output is not one JSON document: %v\n%s", err, out)
	}
	if _, published := decoded["schema"]; published {
		t.Errorf("the result publishes a schema key the rest of the shell suppresses:\n%s", out)
	}
	report := decodeDoctorReport(t, out.Bytes())
	if len(report.Checks) != 3 {
		t.Fatalf("expected 3 findings without --online, got %d: %+v", len(report.Checks), report.Checks)
	}
}

// failingTransport, errNoNetwork, and TestTheNetworkGuardWouldNoticeARequest
// are declared in context_test.go, in this same package, and are reused here
// rather than redeclared.

// TestDoctorOpensNoNetworkConnectionWithoutOnline is the D8-style guard for
// this command: without --online, no check may dial anything, including the
// refusal paths.
func TestDoctorOpensNoNetworkConnectionWithoutOnline(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = failingTransport{t: t}

	invocations := map[string][]string{
		"unconfigured":       {"doctor"},
		"malformed document": {"doctor", "--output", "json"},
		"stray argument":     {"doctor", "extra"},
	}
	for name, args := range invocations {
		t.Run(name, func(t *testing.T) {
			shell, _, _ := newShell(t)
			if name == "malformed document" {
				installMalformedDocument(t, shell)
			}
			keyring.MockInit()
			shell.Run(args)
		})
	}
}

// TestDoctorOnlineChecksCatalogReachability proves --online is wired to a real
// reachability probe rather than being a flag nothing reads: pointed at a
// local origin serving a valid index, the catalog check passes; pointed at one
// that answers nothing, it fails. Neither outcome changes the exit status,
// because catalog never decides it (see doctor.go's severityRank).
func TestDoctorOnlineChecksCatalogReachability(t *testing.T) {
	keyring.MockInit()

	t.Run("reachable", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/"+catalog.IndexPath {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"schemaVersion":1,"modules":[]}`))
		}))
		defer server.Close()
		t.Setenv(catalog.OriginEnvVar, server.URL)

		shell, out, errOut := newShell(t)
		if code := shell.Run([]string{"doctor", "--online", "--output", "json"}); code != exit.OK {
			t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
		}
		report := decodeDoctorReport(t, out.Bytes())
		if finding := report.findingFor(t, "catalog"); finding.Status != "pass" {
			t.Errorf("catalog check = %q, want pass against a reachable origin", finding.Status)
		}
	})

	t.Run("unreachable", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		server.Close() // closed before use: nothing answers this origin.
		t.Setenv(catalog.OriginEnvVar, server.URL)

		shell, out, errOut := newShell(t)
		// The catalog check never decides the exit status, so a healthy
		// unconfigured machine with an unreachable catalog still exits 0.
		if code := shell.Run([]string{"doctor", "--online", "--output", "json"}); code != exit.OK {
			t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
		}
		report := decodeDoctorReport(t, out.Bytes())
		if finding := report.findingFor(t, "catalog"); finding.Status != "fail" {
			t.Errorf("catalog check = %q, want fail against an unreachable origin", finding.Status)
		}
	})
}

// TestDoctorWithoutOnlineNeverAddsACatalogCheck proves catalog is genuinely a
// fourth check that --online adds, not one that always runs and is only
// sometimes reported.
func TestDoctorWithoutOnlineNeverAddsACatalogCheck(t *testing.T) {
	keyring.MockInit()
	shell, out, errOut := newShell(t)

	if code := shell.Run([]string{"doctor", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	report := decodeDoctorReport(t, out.Bytes())
	for _, finding := range report.Checks {
		if finding.Check == "catalog" {
			t.Fatalf("a catalog finding is present without --online: %+v", report.Checks)
		}
	}
}

// TestDoctorRefusesAnUnknownContextAsUsage proves an unresolvable --context
// name is refused as the argument mistake it is, rather than folded into the
// health report as a finding.
func TestDoctorRefusesAnUnknownContextAsUsage(t *testing.T) {
	keyring.MockInit()
	shell, _, errOut := newShell(t)
	seeded := identityOnlyDocument()
	seeded.DefaultContext = "acme"
	seeded.Contexts = []contexts.Context{{Name: "acme", Identity: "acme-cloud"}}
	installLogin(t, shell, seeded)

	if code := shell.Run([]string{"doctor", "--context", "nosuch"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want %d (usage); stderr: %s", code, exit.Usage, errOut)
	}
	requireRefusal(t, errOut.String(), "contexts.unknown_context")
}

// TestDoctorProbeReferenceCannotNameARealIdentity pins R3: the reference
// session.Store.Probe reads under is illegal as a credentialRef, proven
// against the contexts package's own decoder rather than by re-implementing
// its pattern here. A document that assigns it to a real identity is refused
// as malformed, so nothing a user writes or wso2 login writes can ever collide
// with the probe's reserved entry.
func TestDoctorProbeReferenceCannotNameARealIdentity(t *testing.T) {
	document := fmt.Sprintf(`{
  "schemaVersion": 2,
  "defaultContext": "acme",
  "identities": [
    {
      "name": "acme-cloud",
      "type": "cloud",
      "auth": {
        "kind": "oauth-browser",
        "issuer": "https://idp.example",
        "clientId": "wso2-cli",
        "credentialRef": %q
      }
    }
  ],
  "contexts": [{"name": "acme", "identity": "acme-cloud"}]
}`, session.ProbeCredentialRef)

	_, err := contexts.Decode([]byte(document))
	if err == nil {
		t.Fatalf("a document assigning the probe reference %q to a real identity was accepted", session.ProbeCredentialRef)
	}
	requireProblemCode(t, err, "contexts.document_malformed")
}

// requireProblemCode asserts err is a typed problem carrying the given code.
func requireProblemCode(t *testing.T, err error, code string) {
	t.Helper()
	var typed problem.Problem
	if !errors.As(err, &typed) || typed.Code != code {
		t.Fatalf("expected problem code %q, got %v", code, err)
	}
}
