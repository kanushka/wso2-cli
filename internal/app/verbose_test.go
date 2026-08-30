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

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/exit"
)

// TestWithoutVerboseNothingIsWritten pins the default. A command that renders
// its result cleanly today must keep doing so, because the diagnostic stream is
// where a script's operator looks for real trouble and a log the user did not
// ask for is noise there.
func TestWithoutVerboseNothingIsWritten(t *testing.T) {
	shell, out, errOut := newShell(t)
	if code := shell.Run([]string{"version"}); code != exit.OK {
		t.Fatalf("version failed: exit %d, stderr %s", code, errOut)
	}
	if errOut.Len() != 0 {
		t.Fatalf("wso2 version wrote diagnostics without being asked:\n%s", errOut)
	}
	if out.Len() == 0 {
		t.Fatal("wso2 version wrote no result")
	}
}

// TestVerboseWritesDiagnosticsToStandardErrorOnly is the rule
// docs/adr/0003-shell-owned-output.md sets, asserted on the flag that is most
// likely to break it: the result stream stays exactly what it was.
func TestVerboseWritesDiagnosticsToStandardErrorOnly(t *testing.T) {
	quiet, quietOut, _ := newShell(t)
	if code := quiet.Run([]string{"version"}); code != exit.OK {
		t.Fatalf("version failed: exit %d", code)
	}

	shell, out, errOut := newShell(t)
	if code := shell.Run([]string{"--verbose", "version"}); code != exit.OK {
		t.Fatalf("verbose version failed: exit %d, stderr %s", code, errOut)
	}
	if errOut.Len() == 0 {
		t.Fatal("--verbose wrote no diagnostics")
	}
	if !strings.Contains(errOut.String(), "the shell started") {
		t.Fatalf("the diagnostics do not say what ran:\n%s", errOut)
	}
	if out.String() != quietOut.String() {
		t.Fatalf("--verbose changed the result stream:\nwith:\n%s\nwithout:\n%s", out, quietOut)
	}
}

// TestVerboseFollowsTheOutputMode proves the handler pairs with the rendering:
// a caller parsing results with a program can parse the diagnostics too.
func TestVerboseFollowsTheOutputMode(t *testing.T) {
	keyring.MockInit()
	issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "reference-status"})
	shell, _, errOut := newLoginShell(t)
	installLogin(t, shell, browserDoc(issuer.URL))
	shell.OpenBrowser = followAuthorizationURL()
	if code := shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	errOut.Reset()

	// logout is the built-in that honours --output, because it is the one whose
	// result a script has to read.
	if code := shell.Run([]string{"--verbose", "--output", "json", "logout"}); code != exit.OK {
		t.Fatalf("verbose logout failed: exit %d, stderr %s", code, errOut)
	}
	if errOut.Len() == 0 {
		t.Fatal("--verbose wrote no diagnostics")
	}
	for _, line := range strings.Split(strings.TrimSpace(errOut.String()), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("the diagnostic line %q is not JSON under --output json: %v", line, err)
		}
	}
}

// TestVerboseRecordsWhatALoginAttempted proves the login path emits the facts a
// refusal from an issuer has to be read against.
func TestVerboseRecordsWhatALoginAttempted(t *testing.T) {
	keyring.MockInit()
	issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "reference-status"})
	shell, _, errOut := newLoginShell(t)
	installLogin(t, shell, browserDoc(issuer.URL))
	shell.OpenBrowser = followAuthorizationURL()

	if code := shell.Run([]string{"--verbose", "login"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	for _, expected := range []string{
		"starting a login",
		"grant_kind=oauth-browser",
		"issuer=" + issuer.URL,
		"client_id=client-123",
		"the login completed",
	} {
		if !strings.Contains(errOut.String(), expected) {
			t.Fatalf("the login diagnostics are missing %q in:\n%s", expected, errOut)
		}
	}
}

// TestVerboseLoggingNeverLeaksTokenMaterial is the leak test.
//
// It does not assert the denylist's contents. It runs a real login against the
// fixture issuer with diagnostics on, takes the token values that issuer
// actually minted for this run, and greps everything the shell wrote for them.
// That is deliberate: a denylist can only be as complete as whoever last edited
// it, so the thing under test is the output, not the list. A later call site
// that logs a token under an attribute name nobody added to sensitiveKeys is
// caught here, because the value it leaks is the value this test is looking
// for.
func TestVerboseLoggingNeverLeaksTokenMaterial(t *testing.T) {
	keyring.MockInit()
	issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "reference-status"})
	shell, out, errOut := newLoginShell(t)
	installLogin(t, shell, browserDoc(issuer.URL))
	shell.OpenBrowser = followAuthorizationURL()

	if code := shell.Run([]string{"--verbose", "login"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}

	stored, err := session.Store{StateRoot: shell.StateRoot}.Load(credentialRef)
	if err != nil {
		t.Fatalf("session not stored: %v", err)
	}
	// Both are the issuer's own fixture values for this run, not literals
	// invented here: the refresh token is what the login stored, and the access
	// token is what the same exchange returned alongside it.
	secrets := map[string]string{
		"refresh token": stored.RefreshToken,
		"access token":  stored.AccessToken,
	}
	written := out.String() + errOut.String()
	for name, secret := range secrets {
		if secret == "" {
			t.Fatalf("the login stored no %s, so this test proves nothing", name)
		}
		if strings.Contains(written, secret) {
			t.Fatalf("the %s reached the shell's output under --verbose:\n%s", name, written)
		}
	}
	// The run has to have logged something, or the grep above passed by
	// looking at an empty page.
	if !strings.Contains(errOut.String(), "the login completed") {
		t.Fatalf("the login wrote no diagnostics to grep:\n%s", errOut)
	}
}

// followAuthorizationURL plays the user who lands on the authorization page and
// is redirected back to the shell's callback.
func followAuthorizationURL() func(string) error {
	return func(authURL string) error {
		go func() {
			response, err := http.Get(authURL)
			if err == nil {
				_ = response.Body.Close()
			}
		}()
		return nil
	}
}
