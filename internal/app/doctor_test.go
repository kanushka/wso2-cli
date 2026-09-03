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

// tableStatus reads the status cell of one check's row out of a rendered
// table, so a test can pin what the table says about a specific check rather
// than merely that the word appears somewhere in the output. Fields, not a
// fixed column width, because output.Table pads with spaces and the gap
// varies with the widest cell in each column.
func tableStatus(t *testing.T, rendered, check string) string {
	t.Helper()
	for _, line := range strings.Split(rendered, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == check {
			return fields[1]
		}
	}
	t.Fatalf("no row for the %q check in the table:\n%s", check, rendered)
	panic("unreached")
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
// carries the same fact as the JSON rendering, per constraint 6: not just that
// each check is named, but that each row's own status cell agrees with what
// JSON reports for that check.
func TestDoctorOnAnUnconfiguredMachineTableModeSaysSo(t *testing.T) {
	shell, out, errOut := newShell(t)

	if code := shell.Run([]string{"doctor"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	for _, check := range []string{"context", "secure-store", "session"} {
		if status := tableStatus(t, out.String(), check); status != "not-applicable" {
			t.Errorf("%s row status = %q, want not-applicable on an unconfigured machine:\n%s", check, status, out)
		}
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

	tableShell, tableOut, tableErrOut := newShell(t)
	installMalformedDocument(t, tableShell)
	if code := tableShell.Run([]string{"doctor"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want %d (usage); stderr: %s", code, exit.Usage, tableErrOut)
	}
	if status := tableStatus(t, tableOut.String(), "context"); status != "fail" {
		t.Errorf("context row status = %q, want fail for a malformed document:\n%s", status, tableOut)
	}
}

// TestDoctorRanksTheDocumentAboveAnAbsentSession pins R1: when the document is
// invalid (exit.Usage, 64) and no session can be established (not-applicable,
// since a broken document leaves no credential reference to ask the store
// about — see doctor.go), the document's own class is what the shell exits
// with. This is a weaker end-to-end pin than the rank's top position, which
// TestDoctorRanksSecureStoreAboveTheDocument covers instead: a malformed
// document is the only failing thing here, so the interesting fact this test
// pins is that session's inapplicability does not somehow suppress or alter
// the document's own failure.
func TestDoctorRanksTheDocumentAboveAnAbsentSession(t *testing.T) {
	keyring.MockInit()
	shell, out, errOut := newShell(t)
	installMalformedDocument(t, shell)

	if code := shell.Run([]string{"doctor", "--output", "json"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want %d (usage, the document's own class); stderr: %s",
			code, exit.Usage, errOut)
	}
	report := decodeDoctorReport(t, out.Bytes())
	if finding := report.findingFor(t, "context"); finding.Status != "fail" {
		t.Errorf("context check = %q, want fail", finding.Status)
	}
	if finding := report.findingFor(t, "session"); finding.Status != "not-applicable" {
		t.Errorf("session check = %q, want not-applicable: a broken document leaves no "+
			"credential reference to check a session against", finding.Status)
	}
	if finding := report.findingFor(t, "secure-store"); finding.Status != "pass" {
		t.Errorf("secure-store check = %q, want pass", finding.Status)
	}
	requireRefusal(t, errOut.String(), "contexts.document_malformed")
}

// TestDoctorRanksSecureStoreAboveTheDocument pins the top of R1's rank end to
// end: when the document is invalid (exit.Usage, 64) and the secure store is
// unreachable (exit.AuthPolicy, 77) both fail in the same run, secure-store's
// class decides the exit status because R1 ranks it above the document — not
// because 77 is the larger class, which a naive "pick the biggest number"
// implementation would also get right here for the wrong reason. Reversing
// the rank's first two entries, or picking the smallest class, both make this
// test fail.
func TestDoctorRanksSecureStoreAboveTheDocument(t *testing.T) {
	keyring.MockInitWithError(errors.New("no secret service"))
	shell, out, errOut := newShell(t)
	installMalformedDocument(t, shell)

	if code := shell.Run([]string{"doctor", "--output", "json"}); code != exit.AuthPolicy {
		t.Fatalf("exit code = %d, want %d (auth policy, secure-store's own class); stderr: %s",
			code, exit.AuthPolicy, errOut)
	}
	report := decodeDoctorReport(t, out.Bytes())
	if finding := report.findingFor(t, "context"); finding.Status != "fail" {
		t.Errorf("context check = %q, want fail: both checks must genuinely fail for this test to prove anything", finding.Status)
	}
	if finding := report.findingFor(t, "secure-store"); finding.Status != "fail" {
		t.Errorf("secure-store check = %q, want fail", finding.Status)
	}
	// The rendered problem is secure-store's, not the document's: proof that
	// the exit status followed R1's rank and not the smaller exit class.
	requireRefusal(t, errOut.String(), "auth.keyring_unavailable")
	if strings.Contains(errOut.String(), "contexts.document_malformed") {
		t.Errorf("stderr names the document failure instead of the higher-ranked secure-store failure:\n%s", errOut)
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
		if status := tableStatus(t, tableOut.String(), check); status != "pass" {
			t.Errorf("%s row status = %q, want pass to agree with the JSON rendering:\n%s", check, status, tableOut)
		}
	}
}

// TestDoctorReportsALoggedOutContextAsNoneNotAFault is finding N8: a
// configured context with no stored session is the state a confirmed
// wso2 logout leaves behind, so doctor reports the session check as none —
// not fail, which made monitoring wrappers alert on a deliberate action, and
// not not-applicable, which would claim the check could not be asked — keeps
// the login pointer in the recovery column, exits 0, and writes no error line
// to stderr.
func TestDoctorReportsALoggedOutContextAsNoneNotAFault(t *testing.T) {
	keyring.MockInit()
	seeded := identityOnlyDocument()
	seeded.DefaultContext = "acme"
	seeded.Contexts = []contexts.Context{{Name: "acme", Identity: "acme-cloud"}}

	shell, out, errOut := newShell(t)
	installLogin(t, shell, seeded)
	if code := shell.Run([]string{"doctor", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	report := decodeDoctorReport(t, out.Bytes())
	finding := report.findingFor(t, "session")
	if finding.Status != "none" {
		t.Errorf("session check = %q, want none on a logged-out context", finding.Status)
	}
	if !strings.Contains(finding.Recovery, "wso2 login") {
		t.Errorf("session recovery = %q, want the wso2 login pointer kept", finding.Recovery)
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr is not empty on a healthy, logged-out machine:\n%s", errOut)
	}

	tableShell, tableOut, tableErrOut := newShell(t)
	installLogin(t, tableShell, seeded)
	if code := tableShell.Run([]string{"doctor"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, tableErrOut)
	}
	if status := tableStatus(t, tableOut.String(), "session"); status != "none" {
		t.Errorf("session row status = %q, want none to agree with the JSON rendering:\n%s", status, tableOut)
	}
	if tableErrOut.Len() != 0 {
		t.Errorf("stderr restates a finding no longer reported as a failure:\n%s", tableErrOut)
	}
}

// TestDoctorStillFailsAnUnreadableStoredSession pins the boundary of the none
// status: only absence is normal. An entry that exists but cannot be decoded
// is a genuine fault, so the session check still fails and still decides the
// exit status with its own class, exit.AuthPolicy.
func TestDoctorStillFailsAnUnreadableStoredSession(t *testing.T) {
	keyring.MockInit()
	shell, out, errOut := newShell(t)
	seeded := identityOnlyDocument()
	seeded.DefaultContext = "acme"
	seeded.Contexts = []contexts.Context{{Name: "acme", Identity: "acme-cloud"}}
	installLogin(t, shell, seeded)
	if err := keyring.Set(session.Service, "acme-cloud", "not json"); err != nil {
		t.Fatalf("seed an undecodable entry: %v", err)
	}

	if code := shell.Run([]string{"doctor", "--output", "json"}); code != exit.AuthPolicy {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.AuthPolicy, errOut)
	}
	report := decodeDoctorReport(t, out.Bytes())
	if finding := report.findingFor(t, "session"); finding.Status != "fail" {
		t.Errorf("session check = %q, want fail for a stored entry that cannot be read", finding.Status)
	}
	requireRefusal(t, errOut.String(), "auth.login_required")
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
// local origin serving a valid index, the catalog check passes and the run
// exits 0; pointed at one that answers nothing, it fails, and — because
// catalog is ranked in severityRank same as any other check, just last — that
// failure's own class, exit.ModuleProcess (70), decides the exit status on an
// otherwise healthy, unconfigured machine.
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
		// An otherwise healthy, unconfigured machine still exits nonzero once
		// --online adds a failing catalog check: catalog decides the exit
		// status like any other check when it is the only one that failed.
		if code := shell.Run([]string{"doctor", "--online", "--verbose", "--output", "json"}); code != exit.ModuleProcess {
			t.Fatalf("exit code = %d, want %d (module process, the catalog client's own class); stderr: %s",
				code, exit.ModuleProcess, errOut)
		}
		// The probe shares the module commands' diagnostic log, so --verbose
		// surfaces the raw transport detail here exactly as wso2 module list
		// would. Without the wiring this line is absent and the raw cause is
		// dropped for good.
		if !strings.Contains(errOut.String(), "a catalog request failed") {
			t.Errorf("doctor --online --verbose dropped the catalog transport detail:\n%s", errOut)
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

// TestDoctorHonorsContextPrecedence pins doctor.go's duplicated copy of
// selection()'s precedence (internal/app/invoke.go:152): --context wins over
// WSO2_CONTEXT, which wins over the document's default context. Two contexts
// with distinct identities, and a session stored for only one of them, turn
// "which context got selected" into an observable fact: the session check's
// own status names which context doctor actually reported on.
func TestDoctorHonorsContextPrecedence(t *testing.T) {
	keyring.MockInit()
	seeded := identityOnlyDocument()
	seeded.Identities = append(seeded.Identities, contexts.Identity{
		Name: "beta-cloud",
		Type: "cloud",
		Auth: contexts.IdentityAuth{
			Kind:          contexts.KindOAuthBrowser,
			Issuer:        "https://idp.example",
			ClientID:      "wso2-cli",
			CredentialRef: "beta-cloud",
		},
	})
	seeded.DefaultContext = "acme"
	seeded.Contexts = []contexts.Context{
		{Name: "acme", Identity: "acme-cloud"},
		{Name: "beta", Identity: "beta-cloud"},
	}
	// A session exists only for beta's identity, so "session: pass" is only
	// possible when beta is the context doctor actually resolved.
	seedBetaSession := func(t *testing.T, shell app.Shell) {
		t.Helper()
		installLogin(t, shell, seeded)
		store := session.Store{StateRoot: shell.StateRoot}
		if err := store.Save("beta-cloud", session.Session{
			Issuer: "https://idp.example", RefreshToken: "rt-1",
		}); err != nil {
			t.Fatalf("seed a session: %v", err)
		}
	}

	t.Run("the document default, with neither flag nor variable set", func(t *testing.T) {
		shell, out, errOut := newShell(t)
		seedBetaSession(t, shell)

		if code := shell.Run([]string{"doctor", "--output", "json"}); code != exit.OK {
			t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
		}
		report := decodeDoctorReport(t, out.Bytes())
		if finding := report.findingFor(t, "session"); finding.Status != "none" {
			t.Errorf("session check = %q, want none: the default context is acme, which has no session", finding.Status)
		}
	})

	t.Run("WSO2_CONTEXT overrides the document default", func(t *testing.T) {
		shell, out, errOut := newShell(t)
		seedBetaSession(t, shell)
		t.Setenv("WSO2_CONTEXT", "beta")

		if code := shell.Run([]string{"doctor", "--output", "json"}); code != exit.OK {
			t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
		}
		report := decodeDoctorReport(t, out.Bytes())
		if finding := report.findingFor(t, "session"); finding.Status != "pass" {
			t.Errorf("session check = %q, want pass: WSO2_CONTEXT=beta should have been reported on", finding.Status)
		}
	})

	t.Run("--context overrides WSO2_CONTEXT", func(t *testing.T) {
		shell, out, errOut := newShell(t)
		seedBetaSession(t, shell)
		t.Setenv("WSO2_CONTEXT", "beta")

		if code := shell.Run([]string{"doctor", "--context", "acme", "--output", "json"}); code != exit.OK {
			t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
		}
		report := decodeDoctorReport(t, out.Bytes())
		if finding := report.findingFor(t, "session"); finding.Status != "none" {
			t.Errorf("session check = %q, want none: --context acme must win over WSO2_CONTEXT=beta", finding.Status)
		}
	})
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
