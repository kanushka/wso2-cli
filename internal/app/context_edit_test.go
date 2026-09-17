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
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
)

func TestContextEditRefusesAnUnknownOutputMode(t *testing.T) {
	shell, _, _ := newContextShell(t)
	localSetup(t, shell)
	code, _, errOut := run(t, shell, "context", "edit", "--output", "bogus")
	if code != exit.Usage {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(errOut, "shell.unknown_output_mode") {
		t.Errorf("stderr does not carry shell.unknown_output_mode:\n%s", errOut)
	}
}

// TestContextEditRefusesAFrozenVersionOneDocument proves the write refusal
// fires before the editor ever opens: RunEditor fails the test if it is
// reached, which is how the ordering is proved rather than merely hoped for.
func TestContextEditRefusesAFrozenVersionOneDocument(t *testing.T) {
	shell, _, _ := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	installLegacy(t, shell)
	shell.RunEditor = func(string) error {
		t.Fatal("the editor was opened on a document this shell cannot write")
		return nil
	}
	code, _, errOut := run(t, shell, "context", "edit")
	if code != exit.Usage {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(errOut, "schema version 1") {
		t.Errorf("stderr does not explain the version 1 refusal:\n%s", errOut)
	}
}

// TestContextEditRefusesAMalformedDocumentOnDisk proves a document this
// shell cannot even parse is refused before the editor opens.
func TestContextEditRefusesAMalformedDocumentOnDisk(t *testing.T) {
	shell, _, _ := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	path := contexts.Path(shell.StateRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	shell.RunEditor = func(string) error {
		t.Fatal("the editor was opened on a document this shell could not parse")
		return nil
	}
	code, _, errOut := run(t, shell, "context", "edit")
	if code != exit.Usage {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(errOut, "contexts.document_malformed") {
		t.Errorf("stderr does not carry contexts.document_malformed:\n%s", errOut)
	}
}

// TestContextEditOnAFreshMachineStartsFromASchemaVersionZeroDocument proves
// edit works with no context document on disk at all: contexts.Load answers
// the zero Document, whose SchemaVersion is stamped current before it is
// handed to the editor.
func TestContextEditOnAFreshMachineStartsFromASchemaVersionZeroDocument(t *testing.T) {
	shell, _, _ := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	const solo = `{"schemaVersion": 4, "contexts": [{"name": "solo", "type": "cloud", "credentialRef": "solo",
	  "login": {"kind": "oauth-browser", "issuer": "https://idp.example", "clientId": "cli"}}]}`
	shell.RunEditor = func(path string) error {
		return os.WriteFile(path, []byte(solo), 0o600)
	}
	mustRun(t, shell, "context", "edit")
	document := loadDocument(t, shell)
	if _, found := document.Find("solo"); !found {
		t.Fatalf("document = %+v", document)
	}
}

// TestContextEditWithoutATerminalRefusesBeforeOpeningAnEditor is the mirror
// of TestContextEditWithoutATerminalPointsAtTheFile in context_test.go, kept
// here to sit beside the rest of edit's refusals.
func TestContextEditRunEditorFailureWritesNothing(t *testing.T) {
	shell, _, _ := newContextShell(t)
	localSetup(t, shell)
	before := mustReadFile(t, contexts.Path(shell.StateRoot))
	shell.RunEditor = func(string) error { return errors.New("editor crashed") }
	code, _, errOut := run(t, shell, "context", "edit")
	if code != exit.Usage {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(errOut, "shell.editor_failed") {
		t.Errorf("stderr does not carry shell.editor_failed:\n%s", errOut)
	}
	after := mustReadFile(t, contexts.Path(shell.StateRoot))
	if string(before) != string(after) {
		t.Error("a failed editor run still changed the document")
	}
}

// TestContextEditWithNoChangesWritesNothing proves the editor closing with
// the file exactly as it was written is reported and never reaches the
// write path.
func TestContextEditWithNoChangesWritesNothing(t *testing.T) {
	shell, _, _ := newContextShell(t)
	localSetup(t, shell)
	before := mustReadFile(t, contexts.Path(shell.StateRoot))
	shell.RunEditor = func(string) error { return nil }
	out := mustRun(t, shell, "context", "edit")
	if !strings.Contains(out, "No changes; the context document was not written.") {
		t.Errorf("the report does not say nothing changed:\n%s", out)
	}
	after := mustReadFile(t, contexts.Path(shell.StateRoot))
	if string(before) != string(after) {
		t.Error("a no-op edit still changed the document")
	}
}

// TestContextEditEndsSessionsAndReportsThem proves the sessions a rebinding
// edit ends are reported and their revocation notes are printed, the same
// path wso2 context product add --replace exercises for product records.
func TestContextEditEndsSessionsAndReportsThem(t *testing.T) {
	shell, _, _ := newContextShell(t)
	localSetup(t, shell)
	store := session.Store{StateRoot: shell.StateRoot}
	// No refresh token: Revoke.Run answers RevocationNotAttempted without any
	// network call, which is what deterministically exercises endedNotes'
	// "publishes no revocation endpoint" line regardless of what, if
	// anything, happens to be listening on thunderURL in this environment.
	if err := store.Save("local", session.Session{Issuer: thunderURL}); err != nil {
		t.Fatal(err)
	}
	shell.RunEditor = func(path string) error {
		data, _ := os.ReadFile(path)
		edited := strings.Replace(string(data), `"issuer": "`+thunderURL+`"`,
			`"issuer": "`+thunderURL+`/v2"`, 1)
		if edited == string(data) {
			t.Fatal("the string replace did not match the scratch document")
		}
		return os.WriteFile(path, []byte(edited), 0o600)
	}
	out := mustRun(t, shell, "context", "edit")
	if !strings.Contains(out, "Sessions ended because their records changed:") {
		t.Errorf("the report does not name the ended session:\n%s", out)
	}
	if !strings.Contains(out, "publishes no revocation endpoint") {
		t.Errorf("the report does not carry the not-attempted revocation note:\n%s", out)
	}
	if present, _ := store.Stored("local"); present {
		t.Error("the rebound login session was kept")
	}
}

// editorScript writes a tiny shell script standing in for $EDITOR: on its
// first run it leaves body1 in the file it is given, and on every run after
// that body2. It exists to drive contextEdit's real runEditor path (real
// os/exec, not the RunEditor test seam) through both an editor failure and a
// second, successful attempt.
func editorScript(t *testing.T, body1, body2 string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the editor script is a POSIX shell script")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	script := filepath.Join(dir, "editor.sh")
	content := "#!/bin/sh\n" +
		"if [ -f \"" + marker + "\" ]; then\n" +
		"  cat > \"$1\" <<'BODY2'\n" + body2 + "\nBODY2\n" +
		"else\n" +
		"  touch \"" + marker + "\"\n" +
		"  cat > \"$1\" <<'BODY1'\n" + body1 + "\nBODY1\n" +
		"fi\n"
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return script
}

// TestContextEditWithARealEditorAsksAgainAfterAMalformedAttempt drives
// runEditor's real os/exec branch (RunEditor left nil) with $EDITOR pointed
// at a script, and answers "Edit again?" through Shell.Reader: "n" refuses
// after one malformed attempt, "y" is asked again and the second, valid
// attempt is written.
func TestContextEditWithARealEditorAsksAgainAfterAMalformedAttempt(t *testing.T) {
	const valid = `{"schemaVersion": 4, "defaultContext": "local", "contexts": [{"name": "local",
	  "type": "onprem", "credentialRef": "local", "project": "retail",
	  "login": {"kind": "oauth-browser", "issuer": "` + thunderURL + `", "clientId": "wso2-cli",
	    "provider": "thunder", "product": "iam"},
	  "products": {"iam": {"url": "` + thunderURL + `", "audience": "https://localhost:8090/mcp", "scopes": ["system"]}}}]}`

	t.Run("answering no leaves the document as it was", func(t *testing.T) {
		shell, _, _ := newContextShell(t)
		localSetup(t, shell)
		before := mustReadFile(t, contexts.Path(shell.StateRoot))
		t.Setenv("VISUAL", "")
		t.Setenv("EDITOR", editorScript(t, "{not json at all", valid))
		shell.Reader = strings.NewReader("n\n")

		code, _, errOut := run(t, shell, "context", "edit")
		if code != exit.Usage {
			t.Fatalf("exit %d: %s", code, errOut)
		}
		if !strings.Contains(errOut, "contexts.document_malformed") {
			t.Errorf("stderr does not carry contexts.document_malformed:\n%s", errOut)
		}
		after := mustReadFile(t, contexts.Path(shell.StateRoot))
		if string(before) != string(after) {
			t.Error("a refused re-edit still changed the document")
		}
	})

	t.Run("answering yes reopens the editor and writes the valid attempt", func(t *testing.T) {
		shell, _, _ := newContextShell(t)
		localSetup(t, shell)
		t.Setenv("VISUAL", "")
		t.Setenv("EDITOR", editorScript(t, "{not json at all", valid))
		shell.Reader = strings.NewReader("y\n")

		out := mustRun(t, shell, "context", "edit")
		if !strings.Contains(out, "Wrote the context document.") {
			t.Errorf("the second, valid attempt was not written:\n%s", out)
		}
		if got := contextNamed(t, loadDocument(t, shell), "local").Project; got != "retail" {
			t.Errorf("project = %q", got)
		}
	})
}
