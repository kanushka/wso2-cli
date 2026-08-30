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
	"net/http"
	"strings"
	"testing"
	"time"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
)

// whoamiReport mirrors what wso2 whoami --output json publishes, so a test can
// decode it without depending on the command's own unexported type.
type whoamiReport struct {
	Configured    bool   `json:"configured"`
	Context       string `json:"context"`
	Identity      string `json:"identity"`
	Organization  string `json:"organization"`
	Subject       string `json:"subject"`
	Session       string `json:"session"`
	SessionExpiry string `json:"sessionExpiry"`
	Recovery      string `json:"recovery,omitempty"`
}

// decodeWhoamiReport parses wso2 whoami --output json.
func decodeWhoamiReport(t *testing.T, rendered []byte) whoamiReport {
	t.Helper()
	var report whoamiReport
	if err := json.Unmarshal(rendered, &report); err != nil {
		t.Fatalf("the output is not one JSON document: %v\n%s", err, rendered)
	}
	return report
}

// whoamiSeededDocument is one configured context, "acme", authenticating as
// the "acme-cloud" identity — the same shape doctor_test.go's happy-path
// fixture uses, so a session saved under "acme-cloud" is a session for the
// selected context.
func whoamiSeededDocument() contexts.Document {
	seeded := identityOnlyDocument()
	seeded.DefaultContext = "acme"
	seeded.Contexts = []contexts.Context{{Name: "acme", Identity: "acme-cloud", Organization: "acme-org"}}
	return seeded
}

// TestWhoamiOnAnUnconfiguredMachineReportsPlainly proves an unconfigured
// machine is reported as a state, not refused: exit 0, nothing on stderr, and
// the exact sentence wso2 context current already uses for the same fact.
func TestWhoamiOnAnUnconfiguredMachineReportsPlainly(t *testing.T) {
	shell, out, errOut := newShell(t)

	if code := shell.Run([]string{"whoami"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if errOut.Len() != 0 {
		t.Errorf("nothing should reach stderr for an unconfigured machine:\n%s", errOut)
	}
	if !strings.Contains(out.String(), "No context is configured") {
		t.Errorf("the table rendering does not report the unconfigured state:\n%s", out)
	}

	jsonShell, jsonOut, jsonErrOut := newShell(t)
	if code := jsonShell.Run([]string{"whoami", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, jsonErrOut)
	}
	report := decodeWhoamiReport(t, jsonOut.Bytes())
	if report.Configured {
		t.Errorf("configured = true on an unconfigured machine: %+v", report)
	}
	if report.Session != "none" {
		t.Errorf("session = %q, want \"none\" on an unconfigured machine", report.Session)
	}
	// Recovery is present exactly when Session is not "present" (whoami.go's
	// own claim about whoamiReport.Recovery): an unconfigured machine is one
	// of the two whoamiSessionNone causes, and it must not be the one case
	// that comment's biconditional silently excepts.
	if report.Recovery == "" {
		t.Errorf("recovery = \"\", want a way back for an unconfigured machine: %+v", report)
	}
}

// TestWhoamiWithNoSessionNamesLogin proves a selected context with nothing
// stored under its credential reference is reported as a state naming wso2
// login, in both renderings, and does not fail the command.
func TestWhoamiWithNoSessionNamesLogin(t *testing.T) {
	shell, out, errOut := newShell(t)
	installLogin(t, shell, whoamiSeededDocument())

	if code := shell.Run([]string{"whoami", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	report := decodeWhoamiReport(t, out.Bytes())
	if !report.Configured {
		t.Fatalf("configured = false with a selected context: %+v", report)
	}
	if report.Session != "none" {
		t.Errorf("session = %q, want \"none\"", report.Session)
	}
	if !strings.Contains(report.Recovery, "wso2 login") {
		t.Errorf("recovery = %q, want it to name wso2 login", report.Recovery)
	}

	tableShell, tableOut, tableErrOut := newShell(t)
	installLogin(t, tableShell, whoamiSeededDocument())
	if code := tableShell.Run([]string{"whoami"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, tableErrOut)
	}
	if !strings.Contains(tableOut.String(), "wso2 login") {
		t.Errorf("the table rendering does not name wso2 login:\n%s", tableOut)
	}
}

// TestWhoamiReportsAPresentSessionWithUndisclosedExpiry proves the expected
// case per R7 (#112): an issuer that discloses no refresh-token lifetime is
// reported as a present session whose expiry is not stated, never as expired
// and never with the access token's own expiry substituted for it.
func TestWhoamiReportsAPresentSessionWithUndisclosedExpiry(t *testing.T) {
	keyring.MockInit()
	shell, out, errOut := newShell(t)
	installLogin(t, shell, whoamiSeededDocument())
	store := session.Store{StateRoot: shell.StateRoot}
	if err := store.Save("acme-cloud", session.Session{
		Issuer:       "https://idp.example",
		RefreshToken: "rt-1",
		Subject:      "user-1",
		// AccessToken and ExpiresAt are set to prove they are never read: a
		// short access-token expiry in the past must not leak into the
		// session's own reported state.
		AccessToken: "at-1",
		ExpiresAt:   time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("seed a session: %v", err)
	}

	if code := shell.Run([]string{"whoami", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	report := decodeWhoamiReport(t, out.Bytes())
	if report.Session != "present" {
		t.Errorf("session = %q, want \"present\": an undisclosed refresh-token lifetime is not expiry", report.Session)
	}
	if report.SessionExpiry != "not stated by the issuer" {
		t.Errorf("sessionExpiry = %q, want the not-stated wording", report.SessionExpiry)
	}
	if report.Subject != "user-1" {
		t.Errorf("subject = %q, want %q", report.Subject, "user-1")
	}
	if report.Recovery != "" {
		t.Errorf("recovery = %q, want empty for a present session", report.Recovery)
	}
}

// TestWhoamiReportsADisclosedFutureExpiry proves a disclosed, still-future
// refresh-token expiry is rendered as the timestamp, and the session is
// present rather than expired.
func TestWhoamiReportsADisclosedFutureExpiry(t *testing.T) {
	keyring.MockInit()
	shell, out, errOut := newShell(t)
	installLogin(t, shell, whoamiSeededDocument())
	future := time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Second)
	store := session.Store{StateRoot: shell.StateRoot}
	if err := store.Save("acme-cloud", session.Session{
		Issuer: "https://idp.example", RefreshToken: "rt-1",
		SessionExpiresAt: future,
	}); err != nil {
		t.Fatalf("seed a session: %v", err)
	}

	if code := shell.Run([]string{"whoami", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	report := decodeWhoamiReport(t, out.Bytes())
	if report.Session != "present" {
		t.Errorf("session = %q, want \"present\"", report.Session)
	}
	if report.SessionExpiry != future.Format(time.RFC3339) {
		t.Errorf("sessionExpiry = %q, want %q", report.SessionExpiry, future.Format(time.RFC3339))
	}
}

// TestWhoamiReportsAnExpiredSession proves a disclosed refresh-token expiry
// that has passed is reported as expired, naming wso2 login, in both
// renderings.
func TestWhoamiReportsAnExpiredSession(t *testing.T) {
	keyring.MockInit()
	shell, out, errOut := newShell(t)
	installLogin(t, shell, whoamiSeededDocument())
	past := time.Now().Add(-30 * 24 * time.Hour).UTC().Truncate(time.Second)
	store := session.Store{StateRoot: shell.StateRoot}
	if err := store.Save("acme-cloud", session.Session{
		Issuer: "https://idp.example", RefreshToken: "rt-1",
		SessionExpiresAt: past,
	}); err != nil {
		t.Fatalf("seed a session: %v", err)
	}

	if code := shell.Run([]string{"whoami", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	report := decodeWhoamiReport(t, out.Bytes())
	if report.Session != "expired" {
		t.Errorf("session = %q, want \"expired\"", report.Session)
	}
	if report.SessionExpiry != past.Format(time.RFC3339) {
		t.Errorf("sessionExpiry = %q, want %q", report.SessionExpiry, past.Format(time.RFC3339))
	}
	if !strings.Contains(report.Recovery, "wso2 login") {
		t.Errorf("recovery = %q, want it to name wso2 login", report.Recovery)
	}

	tableShell, tableOut, tableErrOut := newShell(t)
	installLogin(t, tableShell, whoamiSeededDocument())
	if err := (session.Store{StateRoot: tableShell.StateRoot}).Save("acme-cloud", session.Session{
		Issuer: "https://idp.example", RefreshToken: "rt-1", SessionExpiresAt: past,
	}); err != nil {
		t.Fatalf("seed a session: %v", err)
	}
	if code := tableShell.Run([]string{"whoami"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, tableErrOut)
	}
	if !strings.Contains(tableOut.String(), "expired") {
		t.Errorf("the table rendering does not say expired:\n%s", tableOut)
	}
}

// TestWhoamiRendersAPreR6SessionAsUnknownAndNotStated is the compatibility
// proof R6/R7 (#112) demand: a keychain entry written before this change
// carries neither the subject nor the session-expiry member at all. It is
// constructed as raw JSON, deliberately bypassing session.Session, so this
// test cannot pass merely because the struct's zero values happen to agree
// with what whoami wants to report — it fails if whoami ever starts requiring
// either member to be present to load the session at all, which is exactly
// the defect this proof exists to catch.
func TestWhoamiRendersAPreR6SessionAsUnknownAndNotStated(t *testing.T) {
	keyring.MockInit()
	shell, out, errOut := newShell(t)
	installLogin(t, shell, whoamiSeededDocument())
	if err := keyring.Set(session.Service, "acme-cloud",
		`{"issuer":"https://idp.example","refreshToken":"rt-1"}`); err != nil {
		t.Fatalf("seed a pre-R6 session: %v", err)
	}

	if code := shell.Run([]string{"whoami", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	report := decodeWhoamiReport(t, out.Bytes())
	if report.Session != "present" {
		t.Errorf("session = %q, want \"present\": a pre-R6 session with a live refresh token is not absent", report.Session)
	}
	if report.Subject != "unknown" {
		t.Errorf("subject = %q, want \"unknown\" for a pre-R6 session", report.Subject)
	}
	if report.SessionExpiry != "not stated by the issuer" {
		t.Errorf("sessionExpiry = %q, want the not-stated wording for a pre-R6 session", report.SessionExpiry)
	}

	tableShell, tableOut, tableErrOut := newShell(t)
	installLogin(t, tableShell, whoamiSeededDocument())
	if err := keyring.Set(session.Service, "acme-cloud",
		`{"issuer":"https://idp.example","refreshToken":"rt-1"}`); err != nil {
		t.Fatalf("seed a pre-R6 session: %v", err)
	}
	if code := tableShell.Run([]string{"whoami"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, tableErrOut)
	}
	if !strings.Contains(tableOut.String(), "unknown") {
		t.Errorf("the table rendering does not say unknown for a pre-R6 subject:\n%s", tableOut)
	}
	if strings.Contains(tableOut.String(), "\x00") {
		t.Errorf("unexpected control byte in the table rendering:\n%s", tableOut)
	}
}

// TestWhoamiRefusesAnUnknownContextAsUsage proves an unresolvable --context
// name is refused as the argument mistake it is, rather than folded into the
// report as a state.
func TestWhoamiRefusesAnUnknownContextAsUsage(t *testing.T) {
	shell, _, errOut := newShell(t)
	installLogin(t, shell, whoamiSeededDocument())

	if code := shell.Run([]string{"whoami", "--context", "nosuch"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want %d (usage); stderr: %s", code, exit.Usage, errOut)
	}
	requireRefusal(t, errOut.String(), "contexts.unknown_context")
}

// TestWhoamiHonorsContextPrecedence pins whoami.go's duplicated copy of
// selection()'s precedence (internal/app/invoke.go:152), mirroring
// doctor_test.go's TestDoctorHonorsContextPrecedence: --context wins over
// WSO2_CONTEXT, which wins over the document's default context. Two contexts
// with distinct identities, and a session stored for only one of them, turn
// "which context got selected" into an observable fact: reporting "present"
// is only possible when the context whoami actually resolved is the one the
// session was seeded under.
func TestWhoamiHonorsContextPrecedence(t *testing.T) {
	keyring.MockInit()
	seeded := whoamiSeededDocument()
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
	seeded.Contexts = append(seeded.Contexts, contexts.Context{Name: "beta", Identity: "beta-cloud"})
	seedBetaSession := func(t *testing.T, shell app.Shell) {
		t.Helper()
		installLogin(t, shell, seeded)
		if err := (session.Store{StateRoot: shell.StateRoot}).Save("beta-cloud", session.Session{
			Issuer: "https://idp.example", RefreshToken: "rt-1",
		}); err != nil {
			t.Fatalf("seed a session: %v", err)
		}
	}

	t.Run("the document default, with neither flag nor variable set", func(t *testing.T) {
		shell, out, errOut := newShell(t)
		seedBetaSession(t, shell)

		if code := shell.Run([]string{"whoami", "--output", "json"}); code != exit.OK {
			t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
		}
		report := decodeWhoamiReport(t, out.Bytes())
		if report.Context != "acme" || report.Session != "none" {
			t.Errorf("report = %+v, want context acme with no session: acme has none seeded", report)
		}
	})

	t.Run("WSO2_CONTEXT overrides the document default", func(t *testing.T) {
		shell, out, errOut := newShell(t)
		seedBetaSession(t, shell)
		t.Setenv("WSO2_CONTEXT", "beta")

		if code := shell.Run([]string{"whoami", "--output", "json"}); code != exit.OK {
			t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
		}
		report := decodeWhoamiReport(t, out.Bytes())
		if report.Context != "beta" || report.Session != "present" {
			t.Errorf("report = %+v, want context beta with a present session: WSO2_CONTEXT=beta "+
				"should have been reported on", report)
		}
	})

	t.Run("--context overrides WSO2_CONTEXT", func(t *testing.T) {
		shell, out, errOut := newShell(t)
		seedBetaSession(t, shell)
		t.Setenv("WSO2_CONTEXT", "beta")

		if code := shell.Run([]string{"whoami", "--context", "acme", "--output", "json"}); code != exit.OK {
			t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
		}
		report := decodeWhoamiReport(t, out.Bytes())
		if report.Context != "acme" || report.Session != "none" {
			t.Errorf("report = %+v, want context acme with no session: --context acme must win "+
				"over WSO2_CONTEXT=beta", report)
		}
	})
}

// TestWhoamiBothRenderingsAgree proves table and JSON agree on the facts, and
// that no schema discriminator is published, per constraint 6.
func TestWhoamiBothRenderingsAgree(t *testing.T) {
	keyring.MockInit()
	// An expired session, deliberately, rather than a merely present one:
	// SessionExpiry and Recovery are both non-empty only in this state, and
	// constraint 6 must hold for every field this command reports, not only
	// the ones a present session happens to populate.
	past := time.Now().Add(-30 * 24 * time.Hour).UTC().Truncate(time.Second)
	seedExpiredSession := func(t *testing.T, shell app.Shell) {
		t.Helper()
		installLogin(t, shell, whoamiSeededDocument())
		if err := (session.Store{StateRoot: shell.StateRoot}).Save("acme-cloud", session.Session{
			Issuer: "https://idp.example", RefreshToken: "rt-1", Subject: "user-1",
			SessionExpiresAt: past,
		}); err != nil {
			t.Fatalf("seed a session: %v", err)
		}
	}

	shell, out, errOut := newShell(t)
	seedExpiredSession(t, shell)
	if code := shell.Run([]string{"whoami", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("the output is not one JSON document: %v\n%s", err, out)
	}
	if _, published := decoded["schema"]; published {
		t.Errorf("the result publishes a schema key the rest of the shell suppresses:\n%s", out)
	}
	report := decodeWhoamiReport(t, out.Bytes())
	if report.SessionExpiry == "" || report.Recovery == "" {
		t.Fatalf("report = %+v, want both SessionExpiry and Recovery populated for an expired session", report)
	}

	tableShell, tableOut, tableErrOut := newShell(t)
	seedExpiredSession(t, tableShell)
	if code := tableShell.Run([]string{"whoami"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, tableErrOut)
	}
	// Every field the JSON rendering carries, SessionExpiry and Recovery
	// included: a fact present in one rendering and not checked in the other
	// is exactly the gap constraint 6 exists to close. Mutation-checked:
	// deleting the {"Session expiry", ...} row from whoamiReport.fields()
	// left this test green before this fix; it now fails that mutation.
	for _, want := range []string{
		report.Context, report.Identity, report.Organization, report.Subject,
		report.Session, report.SessionExpiry, report.Recovery,
	} {
		if !strings.Contains(tableOut.String(), want) {
			t.Errorf("the table rendering is missing %q, present in JSON:\n%s", want, tableOut)
		}
	}
}

// TestWhoamiNeverRendersCredentialMaterial proves neither rendering leaks a
// token, asserted against the actual output rather than by inspection.
func TestWhoamiNeverRendersCredentialMaterial(t *testing.T) {
	keyring.MockInit()
	shell, out, errOut := newShell(t)
	installLogin(t, shell, whoamiSeededDocument())
	const refreshSecret = "rt-super-secret-value"
	const accessSecret = "at-super-secret-value"
	if err := (session.Store{StateRoot: shell.StateRoot}).Save("acme-cloud", session.Session{
		Issuer: "https://idp.example", RefreshToken: refreshSecret,
		AccessToken: accessSecret, Subject: "user-1",
	}); err != nil {
		t.Fatalf("seed a session: %v", err)
	}

	if code := shell.Run([]string{"whoami", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	for _, secret := range []string{refreshSecret, accessSecret} {
		if strings.Contains(out.String(), secret) || strings.Contains(errOut.String(), secret) {
			t.Fatalf("token material leaked into whoami output:\n%s", out)
		}
	}

	tableShell, tableOut, tableErrOut := newShell(t)
	installLogin(t, tableShell, whoamiSeededDocument())
	if err := (session.Store{StateRoot: tableShell.StateRoot}).Save("acme-cloud", session.Session{
		Issuer: "https://idp.example", RefreshToken: refreshSecret,
		AccessToken: accessSecret, Subject: "user-1",
	}); err != nil {
		t.Fatalf("seed a session: %v", err)
	}
	if code := tableShell.Run([]string{"whoami"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, tableErrOut)
	}
	for _, secret := range []string{refreshSecret, accessSecret} {
		if strings.Contains(tableOut.String(), secret) || strings.Contains(tableErrOut.String(), secret) {
			t.Fatalf("token material leaked into whoami table output:\n%s", tableOut)
		}
	}
}

// TestWhoamiOpensNoNetworkConnection is the D8-style guard for this command:
// on any path, including refusals, whoami must not dial anything. It makes no
// network call at all — everything it reports comes from local state.
func TestWhoamiOpensNoNetworkConnection(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = failingTransport{t: t}
	keyring.MockInit()

	// No "unsupported flag" case: unlike wso2 context's subcommands, whoami's
	// own shellFlagsFor entry grants both contextFlag and outputFlag, so there
	// is no shell-owned flag this command actually refuses (--verbose is
	// exempt from every command's shellFlagsFor list — see command.go's own
	// comment on shellFlagsFor — so {"--verbose", "whoami"} is a supported
	// invocation that exits 0, not a refusal; doctor_test.go's equivalent
	// guard carries no such case either, for the same reason).
	invocations := map[string][]string{
		"unconfigured":        {"whoami"},
		"unconfigured, json":  {"whoami", "--output", "json"},
		"configured, session": {"whoami", "--output", "json"},
		"unknown context":     {"whoami", "--context", "nosuch"},
		"stray argument":      {"whoami", "extra"},
	}
	for name, args := range invocations {
		t.Run(name, func(t *testing.T) {
			shell, _, _ := newShell(t)
			if name == "configured, session" {
				installLogin(t, shell, whoamiSeededDocument())
				if err := (session.Store{StateRoot: shell.StateRoot}).Save("acme-cloud", session.Session{
					Issuer: "https://idp.example", RefreshToken: "rt-1",
				}); err != nil {
					t.Fatalf("seed a session: %v", err)
				}
			} else if name == "unknown context" {
				installLogin(t, shell, whoamiSeededDocument())
			}
			shell.Run(args)
		})
	}
}

// failingTransport, errNoNetwork, and TestTheNetworkGuardWouldNoticeARequest
// are declared in context_test.go, in this same package, and are reused here
// rather than redeclared — the same note doctor_test.go makes.
