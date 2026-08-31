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
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/modules/fixture"
	"github.com/wso2/wso2-cli/internal/preferences"
)

// configEntry mirrors what wso2 config get/set/list publish in JSON, so a
// test can decode it without depending on the command's own unexported type.
type configEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Set   bool   `json:"set"`
}

type configListing struct {
	Entries []configEntry `json:"entries"`
}

// TestConfigListShowsTheClosedKeySetUnset proves the closed set (R8) by
// listing it: exactly two keys, none configured on a fresh machine. The set
// was three at the brief's original writing; a colour preference was cut
// before shipping (fix round 1, F3) because output.ColorEnabled has zero
// production callers, so a key that claimed to govern it would change
// nothing observable.
func TestConfigListShowsTheClosedKeySetUnset(t *testing.T) {
	shell, out, errOut := newShell(t)
	if code := shell.Run([]string{"config", "list", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	var listing configListing
	if err := json.Unmarshal(out.Bytes(), &listing); err != nil {
		t.Fatalf("json.Unmarshal: %v; output: %s", err, out)
	}
	if len(listing.Entries) != 2 {
		t.Fatalf("len(Entries) = %d, want 2: %+v", len(listing.Entries), listing.Entries)
	}
	want := map[string]bool{"output": false, "catalog-origin": false}
	for _, entry := range listing.Entries {
		if _, known := want[entry.Key]; !known {
			t.Errorf("unexpected key %q in the listing", entry.Key)
		}
		if entry.Set {
			t.Errorf("key %q is reported set on a fresh machine", entry.Key)
		}
		delete(want, entry.Key)
	}
	if len(want) != 0 {
		t.Errorf("missing keys from the listing: %v", want)
	}
}

// TestConfigListRenderingsAgreeAndPublishNoSchemaKey pins Global Constraint
// 6: the table and JSON renderings of wso2 config list must report the same
// facts, and neither publishes a "schema" discriminator key.
func TestConfigListRenderingsAgreeAndPublishNoSchemaKey(t *testing.T) {
	shell, out, errOut := newShell(t)
	if code := shell.Run([]string{"config", "set", "output", "json"}); code != exit.OK {
		t.Fatalf("config set exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}

	out.Reset()
	if code := shell.Run([]string{"config", "list", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if strings.Contains(out.String(), `"schema"`) {
		t.Errorf("config list --output json publishes a schema key:\n%s", out)
	}
	var listing configListing
	if err := json.Unmarshal(out.Bytes(), &listing); err != nil {
		t.Fatalf("json.Unmarshal: %v; output: %s", err, out)
	}

	out.Reset()
	if code := shell.Run([]string{"config", "list", "--output", "table"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	for _, entry := range listing.Entries {
		if !strings.Contains(out.String(), entry.Key) || !strings.Contains(out.String(), entry.Value) {
			t.Errorf("the table rendering does not agree with the JSON one for %+v:\n%s", entry, out)
		}
	}
}

// TestConfigSetThenGetRoundTrips proves the write path and the read path
// agree on what was written.
func TestConfigSetThenGetRoundTrips(t *testing.T) {
	shell, out, errOut := newShell(t)
	if code := shell.Run([]string{"config", "set", "output", "json"}); code != exit.OK {
		t.Fatalf("config set exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}

	out.Reset()
	if code := shell.Run([]string{"config", "get", "output", "--output", "json"}); code != exit.OK {
		t.Fatalf("config get exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	var entry configEntry
	if err := json.Unmarshal(out.Bytes(), &entry); err != nil {
		t.Fatalf("json.Unmarshal: %v; output: %s", err, out)
	}
	if entry != (configEntry{Key: "output", Value: "json", Set: true}) {
		t.Errorf("config get output = %+v, want {output json true}", entry)
	}
}

// TestConfigSetDoesNotDisturbTheOtherKey is the app-level half of the
// preferences package's own proof: a read-modify-write through the command
// layer preserves what an earlier wso2 config set wrote.
func TestConfigSetDoesNotDisturbTheOtherKey(t *testing.T) {
	shell, _, errOut := newShell(t)
	if code := shell.Run([]string{"config", "set", "output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if code := shell.Run([]string{"config", "set", "catalog-origin", "https://example.com"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}

	document, diagnostic := preferences.Load(shell.StateRoot)
	if diagnostic != nil {
		t.Fatalf("preferences.Load returned a diagnostic: %v", diagnostic)
	}
	if document.OutputMode != "json" {
		t.Errorf("outputMode = %q, want json to have survived the second set", document.OutputMode)
	}
	if document.CatalogOrigin != "https://example.com" {
		t.Errorf("catalogOrigin = %q, want it set", document.CatalogOrigin)
	}
}

// TestConfigSetOutputJSONRendersTheWrittenEntry is F5's other missing test:
// --output json on config set itself, not just config get afterward.
func TestConfigSetOutputJSONRendersTheWrittenEntry(t *testing.T) {
	shell, out, errOut := newShell(t)
	if code := shell.Run([]string{"config", "set", "catalog-origin", "https://example.com", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if strings.Contains(out.String(), `"schema"`) {
		t.Errorf("config set --output json publishes a schema key:\n%s", out)
	}
	var entry configEntry
	if err := json.Unmarshal(out.Bytes(), &entry); err != nil {
		t.Fatalf("json.Unmarshal: %v; output: %s", err, out)
	}
	if entry != (configEntry{Key: "catalog-origin", Value: "https://example.com", Set: true}) {
		t.Errorf("config set --output json = %+v, want {catalog-origin https://example.com true}", entry)
	}
}

// TestConfigGetRefusesAnUnknownKey proves the closed key set is enforced at
// the command layer too, and the refusal names the valid keys — not asserted
// in a comment, but as the actual recovery text a user reads.
func TestConfigGetRefusesAnUnknownKey(t *testing.T) {
	shell, out, errOut := newShell(t)
	if code := shell.Run([]string{"config", "get", "access-token"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want %d (usage); stdout: %s", code, exit.Usage, out)
	}
	requireRefusal(t, errOut.String(), "config.unknown_key")
	for _, key := range []string{"output", "catalog-origin"} {
		if !strings.Contains(errOut.String(), key) {
			t.Errorf("the refusal does not name the valid key %q:\n%s", key, errOut)
		}
	}
}

// TestConfigSetRefusesAnUnknownKeyWithNoWrite proves the same refusal on the
// write side, and that nothing was written: this is the closed set's real
// proof that no credential-shaped key can ever be stored, since neither
// accepted key can hold one.
func TestConfigSetRefusesAnUnknownKeyWithNoWrite(t *testing.T) {
	shell, _, errOut := newShell(t)
	if code := shell.Run([]string{"config", "set", "client-secret", "sh"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want %d (usage); stderr: %s", code, exit.Usage, errOut)
	}
	requireRefusal(t, errOut.String(), "config.unknown_key")
	if _, err := os.Stat(preferences.Path(shell.StateRoot)); !os.IsNotExist(err) {
		t.Errorf("a refused config set wrote a preferences document anyway: stat err = %v", err)
	}
}

// TestConfigSetRefusesAnInvalidOutputMode proves a value a key does not
// accept is refused, naming what is acceptable.
func TestConfigSetRefusesAnInvalidOutputMode(t *testing.T) {
	shell, _, errOut := newShell(t)
	if code := shell.Run([]string{"config", "set", "output", "yaml"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want %d (usage); stderr: %s", code, exit.Usage, errOut)
	}
	requireRefusal(t, errOut.String(), "config.invalid_value")
	if !strings.Contains(errOut.String(), "table") || !strings.Contains(errOut.String(), "json") {
		t.Errorf("the refusal does not name the acceptable values:\n%s", errOut)
	}
}

// TestConfigSetWrites0600IntoA0700Directory pins the state-root convention at
// the command layer, asserted against the actual mode rather than assumed
// from the package-level test.
func TestConfigSetWrites0600IntoA0700Directory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes are not enforced on Windows")
	}
	shell, _, errOut := newShell(t)
	if code := shell.Run([]string{"config", "set", "catalog-origin", "https://example.com"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	path := preferences.Path(shell.StateRoot)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%q): %v", path, err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("document mode = %o, want 0600", perm)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Stat(dir): %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("directory mode = %o, want 0700", perm)
	}
}

// TestOutputFlagWinsOverConfiguredOutputMode and
// TestConfiguredOutputModeWinsOverTheBuiltInDefault together mutation-prove
// the output-mode precedence table this task pins: --output, then the
// "output" preference, then output.ModeTable. wso2 context list is used as
// the probe command because its table and JSON renderings are easy to tell
// apart even on an unconfigured machine (a sentence versus a JSON object).
func TestOutputFlagWinsOverConfiguredOutputMode(t *testing.T) {
	shell, out, errOut := newShell(t)
	if code := shell.Run([]string{"config", "set", "output", "json"}); code != exit.OK {
		t.Fatalf("config set exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}

	out.Reset()
	if code := shell.Run([]string{"context", "list", "--output", "table"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if strings.HasPrefix(strings.TrimSpace(out.String()), "{") {
		t.Errorf("--output table rendered JSON despite the configured default:\n%s", out)
	}
}

func TestConfiguredOutputModeWinsOverTheBuiltInDefault(t *testing.T) {
	shell, out, errOut := newShell(t)
	if code := shell.Run([]string{"config", "set", "output", "json"}); code != exit.OK {
		t.Fatalf("config set exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}

	out.Reset()
	if code := shell.Run([]string{"context", "list"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.HasPrefix(strings.TrimSpace(out.String()), "{") {
		t.Errorf("no --output flag did not fall back to the configured json default:\n%s", out)
	}
}

func TestUnconfiguredOutputModeDefaultsToTable(t *testing.T) {
	shell, out, errOut := newShell(t)
	if code := shell.Run([]string{"context", "list"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if strings.HasPrefix(strings.TrimSpace(out.String()), "{") {
		t.Errorf("an unconfigured machine rendered JSON instead of the built-in table default:\n%s", out)
	}
}

// TestACorruptPreferencesDocumentFallsBackWithADiagnostic proves R9's
// promise end to end: a preferences document this shell cannot read must not
// break the command that happens to run next, but the fallback must be
// diagnosed on Streams.Err, not silent. The diagnostic is written once, by
// dispatch (see app.go, moved there from applyShellFlags in fix round 1,
// F1), before wso2 config list even runs — this asserts the diagnostic
// itself, not just that the command still produced defaults.
func TestACorruptPreferencesDocumentFallsBackWithADiagnostic(t *testing.T) {
	shell, out, errOut := newShell(t)
	path := preferences.Path(shell.StateRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if code := shell.Run([]string{"config", "list", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.Contains(errOut.String(), "warning:") || !strings.Contains(errOut.String(), "preferences.document_malformed") {
		t.Errorf("a corrupt preferences document produced no diagnostic:\n%s", errOut)
	}

	var listing configListing
	if err := json.Unmarshal(out.Bytes(), &listing); err != nil {
		t.Fatalf("json.Unmarshal: %v; output: %s", err, out)
	}
	for _, entry := range listing.Entries {
		if entry.Set {
			t.Errorf("key %q reported set after falling back to defaults: %+v", entry.Key, entry)
		}
	}
}

// TestAConfigSubcommandRefusesContextFlag proves shellFlagsFor's entry for
// "config" (outputFlag only, no context) is enforced: a preference is
// machine-local, not context-scoped, so --context has nothing to select
// here.
func TestAConfigSubcommandRefusesContextFlag(t *testing.T) {
	shell, _, errOut := newShell(t)
	if code := shell.Run([]string{"config", "list", "--context", "prod"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want %d (usage); stderr: %s", code, exit.Usage, errOut)
	}
	requireRefusal(t, errOut.String(), "shell.unsupported_flag")
}

// TestProductNamespaceDispatchDiagnosesACorruptPreferencesDocument is F1's
// exact regression (fix round 1): dispatchNamespace never entered the Cobra
// command tree, so applyShellFlags — the only place the preferences
// diagnostic used to be written — never ran for it, silently breaking R9's
// "never silent" promise for the ordinary case of a product-namespace
// command. The diagnostic now lives in dispatch, before the Cobra/namespace
// fork (see app.go), so it fires here too.
func TestProductNamespaceDispatchDiagnosesACorruptPreferencesDocument(t *testing.T) {
	shell, _, errOut := newShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})
	path := preferences.Path(shell.StateRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// The module invocation itself fails (the fixture executable speaks no
	// contract) — irrelevant to what this proves, which is that dispatch
	// took the dispatchNamespace path at all, never touching applyShellFlags.
	shell.Run([]string{"reference", "status"})
	if !strings.Contains(errOut.String(), "warning:") || !strings.Contains(errOut.String(), "preferences.document_malformed") {
		t.Errorf("a corrupt preferences document produced no diagnostic on the product-namespace path:\n%s", errOut)
	}
}

// TestDoctorOnlineHonorsTheConfiguredCatalogOrigin is F8: coverage, not a
// fix. The review dismissed the residual risk behind the missing test (both
// production callers of catalog.Origin were checked by hand to pass a real
// Shell.StateRoot) but asked for the acceptance-shaped proof anyway, since
// WSO2_CLI_CATALOG_ORIGIN is the test suite's own safety belt and this is
// the command that would notice if a real Shell ever passed the wrong root.
func TestDoctorOnlineHonorsTheConfiguredCatalogOrigin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+catalog.IndexPath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"schemaVersion":1,"modules":[]}`))
	}))
	defer server.Close()

	shell, out, errOut := newShell(t)
	if code := shell.Run([]string{"config", "set", "catalog-origin", server.URL}); code != exit.OK {
		t.Fatalf("config set exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	out.Reset()

	if code := shell.Run([]string{"doctor", "--online", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	report := decodeDoctorReport(t, out.Bytes())
	if finding := report.findingFor(t, "catalog"); finding.Status != "pass" {
		t.Errorf("catalog check = %q, want pass against the configured origin", finding.Status)
	}
}

// TestConfigRefusesAnUnknownSubcommand pins the decision that a mistyped
// subcommand is a usage error rather than a success. Cobra's default for a
// non-Runnable parent is to print help and exit 0, which reports a typo to a
// script as everything having worked; wso2 org refuses instead, and this
// branch introduces both families, so they must not disagree with each other.
//
// The bare form is covered too, because the catch-all has to tell the two
// apart: nothing typed is a missing argument, not an unknown command.
func TestConfigRefusesAnUnknownSubcommand(t *testing.T) {
	for name, invocation := range map[string]struct {
		args []string
		code string
	}{
		"unknown": {args: []string{"config", "bogus"}, code: "shell.unknown_command"},
		"bare":    {args: []string{"config"}, code: "shell.missing_argument"},
	} {
		t.Run(name, func(t *testing.T) {
			shell, out, errOut := newShell(t)
			if code := shell.Run(invocation.args); code != exit.Usage {
				t.Fatalf("exit code = %d, want %d (usage); stdout: %s stderr: %s",
					code, exit.Usage, out, errOut)
			}
			requireRefusal(t, errOut.String(), invocation.code)
			for _, named := range []string{"config list", "config get", "config set"} {
				if !strings.Contains(errOut.String(), named) {
					t.Errorf("the refusal does not name %q as a way forward:\n%s", named, errOut)
				}
			}
		})
	}
}

// TestConfigSetOutputConfirmsInTheModeItJustWrote pins the decision behind
// the branch review's finding 10. The mode is resolved before the write, so
// the confirmation for the one command that changes the mode was rendered in
// the mode being replaced: setting json answered with a table, and setting
// table answered with JSON. Both directions are asserted, because a fix that
// only ever rendered JSON would pass one of them.
//
// The third case is the limit of the rule: an explicit --output is the more
// specific source and still wins, so a script asking for JSON gets JSON even
// while writing table as the default for everything else.
func TestConfigSetOutputConfirmsInTheModeItJustWrote(t *testing.T) {
	isJSON := func(reported string) bool {
		return json.Unmarshal([]byte(reported), &configEntry{}) == nil
	}
	for name, testCase := range map[string]struct {
		args     []string
		wantJSON bool
	}{
		"json":  {args: []string{"config", "set", "output", "json"}, wantJSON: true},
		"table": {args: []string{"config", "set", "output", "table"}, wantJSON: false},
		"an explicit flag wins": {
			args:     []string{"config", "set", "output", "table", "--output", "json"},
			wantJSON: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			shell, out, errOut := newShell(t)
			if code := shell.Run(testCase.args); code != exit.OK {
				t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
			}
			if got := isJSON(out.String()); got != testCase.wantJSON {
				t.Errorf("rendered as JSON = %t, want %t:\n%s", got, testCase.wantJSON, out)
			}
		})
	}
}
