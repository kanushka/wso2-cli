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
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/auth/devtoken"
	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/oauthflow"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	contextfixture "github.com/wso2/wso2-cli/internal/contexts/fixture"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/modules/fixture"
	"github.com/wso2/wso2-cli/sdk/protocol"
	"github.com/wso2/wso2-cli/sdk/protocol/contractv1"
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

// TestVerboseRecordsWhatALogoutEstablished proves the logout path emits the one
// fact nothing else on this machine records: what the issuer did with its own
// copy of the session. Revocation is best effort, so "confirmed" and "failed"
// are both ordinary outcomes of a successful logout, and which one happened is
// what a user reports. See docs/adr/0010-best-effort-revocation-on-session-end.md.
func TestVerboseRecordsWhatALogoutEstablished(t *testing.T) {
	keyring.MockInit()
	issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "reference-status"})
	shell, _, errOut := newLoginShell(t)
	installLogin(t, shell, browserDoc(issuer.URL))
	shell.OpenBrowser = followAuthorizationURL()
	if code := shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	errOut.Reset()

	if code := shell.Run([]string{"--verbose", "logout"}); code != exit.OK {
		t.Fatalf("logout failed: exit %d, stderr %s", code, errOut)
	}
	for _, expected := range []string{
		"ending a session",
		"the session ended",
		"revocation=" + string(oauthflow.RevocationConfirmed),
		"session_removed=true",
	} {
		if !strings.Contains(errOut.String(), expected) {
			t.Fatalf("the logout diagnostics are missing %q in:\n%s", expected, errOut)
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

// The reference deployment this file's broker arm runs against. The names are
// the ones the acceptance harness deploys under, so a value logged here is a
// value logged there.
const (
	// brokerCredential is the development credential the broker signs a
	// fixture token with. It is the secret this arm is looking for: the shell
	// holds it for the length of the invocation and the module must never see
	// it, so neither may a diagnostic.
	brokerCredential   = "canary-source-credential-2f8c-do-not-disclose"
	brokerCredentialIn = "WSO2_REFERENCE_DEV_CREDENTIAL"
	brokerContextName  = "reference-local"
	brokerOrganization = "reference-org"
	brokerAudience     = "reference-status"
	brokerReadScope    = "reference:status:read"
)

// TestVerboseLoggingNeverLeaksTheBrokerCredential is the leak test for the
// other credential path.
//
// The login arm above covers a session: material an issuer minted, held in the
// secure store. This one covers the broker's: a development credential the
// shell reads from the environment and derives a fixture token from, which is
// what "brokering module access" is logged beside. The two are different
// secrets from different sources, and a denylist that catches one says nothing
// about the other.
//
// The fixture module cannot be launched — its executable is not one — so the
// run fails after the broker line is written and before any module asks for
// access. That is the whole of what this arm can observe in process, and it is
// the part that matters: the credential is in the shell's environment for the
// entire invocation, so every line the invocation writes is a chance to leak
// it. Whether a granted token leaks is the acceptance canary's subject, which
// runs a real module against a real service.
func TestVerboseLoggingNeverLeaksTheBrokerCredential(t *testing.T) {
	shell, out, errOut := newShell(t)
	installFixture(t, shell, fixture.Module{
		Namespace:     "reference",
		Version:       "0.1.0",
		AuthAudiences: []string{brokerAudience},
		AuthScopes:    []string{brokerReadScope},
	})
	installDevelopmentContext(t, shell)
	t.Setenv(brokerCredentialIn, brokerCredential)

	// The exit code is deliberately not asserted: the launch fails, and this
	// test is about what the failing run wrote rather than about how it failed.
	shell.Run([]string{"--verbose", "reference", "status"})

	written := out.String() + errOut.String()
	// The broker line has to be there, or the greps below read an empty page.
	if !strings.Contains(errOut.String(), "brokering module access") {
		t.Fatalf("the invocation wrote no broker diagnostics to grep:\n%s", errOut)
	}
	if strings.Contains(written, brokerCredential) {
		t.Fatalf("the development credential reached the shell's output under --verbose:\n%s", written)
	}
	// Every fixture token opens with this prefix, for exactly this purpose: one
	// that escapes into a log is recognizable as a token whatever attribute
	// name it escaped under.
	if strings.Contains(written, devtoken.Prefix) {
		t.Fatalf("a fixture token reached the shell's output under --verbose:\n%s", written)
	}
}

// installDevelopmentContext writes the reference deployment's context: a
// development-credential identity naming the environment variable the broker
// reads, and nothing that holds the credential itself.
func installDevelopmentContext(t *testing.T, shell app.Shell) {
	t.Helper()
	if err := contextfixture.Install(shell.StateRoot, contextfixture.LegacyDocument{
		SchemaVersion:  contexts.SchemaVersionLegacy,
		DefaultContext: brokerContextName,
		Contexts: []contextfixture.LegacyContext{{
			Name:           brokerContextName,
			OrganizationID: brokerOrganization,
			Endpoint:       "https://reference.invalid",
			Auth: contextfixture.LegacyAuth{
				Method:             contexts.MethodDevelopmentCredential,
				CredentialVariable: brokerCredentialIn,
			},
		}},
	}); err != nil {
		t.Fatalf("installing the reference context: %v", err)
	}
}

// TestVerboseIsHonoredAfterTheCommandName covers the position users actually
// type. login, logout, and module disable Cobra's flag parsing and read their
// own arguments, so a flag written after the command name reaches their parsers
// rather than the root's. Before takeVerboseFlag existed, all three refused it
// as an unknown flag — which is the worst possible answer to a user who is
// already trying to diagnose something else.
func TestVerboseIsHonoredAfterTheCommandName(t *testing.T) {
	t.Run("login", func(t *testing.T) {
		keyring.MockInit()
		issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "reference-status"})
		shell, _, errOut := newLoginShell(t)
		installLogin(t, shell, browserDoc(issuer.URL))
		shell.OpenBrowser = followAuthorizationURL()

		if code := shell.Run([]string{"login", "--verbose"}); code != exit.OK {
			t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
		}
		if !strings.Contains(errOut.String(), "starting a login") {
			t.Fatalf("wso2 login --verbose wrote no diagnostics:\n%s", errOut)
		}
	})

	t.Run("logout", func(t *testing.T) {
		keyring.MockInit()
		issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "reference-status"})
		shell, _, errOut := newLoginShell(t)
		installLogin(t, shell, browserDoc(issuer.URL))
		shell.OpenBrowser = followAuthorizationURL()
		if code := shell.Run([]string{"login"}); code != exit.OK {
			t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
		}
		errOut.Reset()

		if code := shell.Run([]string{"logout", "--verbose"}); code != exit.OK {
			t.Fatalf("logout failed: exit %d, stderr %s", code, errOut)
		}
		if !strings.Contains(errOut.String(), "the shell started") {
			t.Fatalf("wso2 logout --verbose wrote no diagnostics:\n%s", errOut)
		}
	})

	// wso2 module list takes no arguments, so before the strip this refused
	// with shell.unexpected_argument — a message that describes the wrong
	// mistake. Stripping the flag before the argument check settles both.
	t.Run("module list", func(t *testing.T) {
		shell, _, errOut := newShell(t)
		if code := shell.Run([]string{"module", "list", "--verbose"}); code != exit.OK {
			t.Fatalf("module list failed: exit %d, stderr %s", code, errOut)
		}
		if !strings.Contains(errOut.String(), "the shell started") {
			t.Fatalf("wso2 module list --verbose wrote no diagnostics:\n%s", errOut)
		}
	})
}

// TestVerboseWrittenTwiceEnablesTheLogOnce proves the two doors into the log —
// the root's parser and takeVerboseFlag — do not both open it.
func TestVerboseWrittenTwiceEnablesTheLogOnce(t *testing.T) {
	shell, _, errOut := newShell(t)
	if code := shell.Run([]string{"--verbose", "module", "list", "--verbose"}); code != exit.OK {
		t.Fatalf("module list failed: exit %d, stderr %s", code, errOut)
	}
	if started := strings.Count(errOut.String(), "the shell started"); started != 1 {
		t.Fatalf("the log announced itself %d times:\n%s", started, errOut)
	}
}

// TestVerboseWithAnExplicitValueIsRead pins the --verbose=false spelling: it is
// stripped like any other, and it does not turn the log on.
func TestVerboseWithAnExplicitValueIsRead(t *testing.T) {
	shell, _, errOut := newShell(t)
	if code := shell.Run([]string{"module", "list", "--verbose=false"}); code != exit.OK {
		t.Fatalf("module list failed: exit %d, stderr %s", code, errOut)
	}
	if errOut.Len() != 0 {
		t.Fatalf("--verbose=false wrote diagnostics:\n%s", errOut)
	}

	shell, _, errOut = newShell(t)
	if code := shell.Run([]string{"module", "list", "--verbose=true"}); code != exit.OK {
		t.Fatalf("module list failed: exit %d, stderr %s", code, errOut)
	}
	if !strings.Contains(errOut.String(), "the shell started") {
		t.Fatalf("--verbose=true wrote no diagnostics:\n%s", errOut)
	}

	shell, _, errOut = newShell(t)
	if code := shell.Run([]string{"module", "list", "--verbose=maybe"}); code != exit.Usage {
		t.Fatalf("--verbose=maybe exited %d, want the usage class %d; stderr: %s",
			code, exit.Usage, errOut)
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

// TestVerboseTakesItsLastOccurrence pins the spelling against the parser that
// reads the same flag on the other side of the command name. pflag lets the
// last value win, so a hand-rolled scanner that treated the flag as cumulative
// would leave "--verbose --verbose=false" meaning one thing before a command
// name and the opposite after it — and the user reading a log they had just
// switched off is the worse half of that.
func TestVerboseTakesItsLastOccurrence(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "bare then off", args: []string{"module", "list", "--verbose", "--verbose=false"}},
		{name: "off then bare", args: []string{"module", "list", "--verbose=false", "--verbose"}, want: true},
		{name: "off then on", args: []string{"module", "list", "--verbose=false", "--verbose=true"}, want: true},
		{name: "on then off", args: []string{"module", "list", "--verbose=true", "--verbose=false"}},
		// The same spelling on the root's own parser, which is what the three
		// arms above are pinned to rather than to a rule stated twice.
		{name: "bare then off, before the command name", args: []string{"--verbose", "--verbose=false", "version"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			shell, _, errOut := newShell(t)
			if code := shell.Run(test.args); code != exit.OK {
				t.Fatalf("%v failed: exit %d, stderr %s", test.args, code, errOut)
			}
			if got := strings.Contains(errOut.String(), "the shell started"); got != test.want {
				t.Fatalf("%v wrote diagnostics = %t, want %t:\n%s", test.args, got, test.want, errOut)
			}
		})
	}
}

// TestVerboseFollowsAnOutputModeWrittenAfterTheCommandName covers the mode the
// root never parsed. logout disables Cobra's flag parsing, so for
// "wso2 logout --output json --verbose" the root's flag is still table while
// the command's own parser renders JSON. Diagnostics interleaved with a
// machine-readable result have to be machine-readable too, or the caller
// parsing stderr hits prose.
func TestVerboseFollowsAnOutputModeWrittenAfterTheCommandName(t *testing.T) {
	// Every spelling the command's own parser accepts, because a spelling that
	// selected the result's format and not the diagnostics' would be the same
	// split under a different name.
	for _, spelling := range [][]string{
		{"--output", "json"},
		{"-o", "json"},
		{"--output=json"},
		{"-ojson"},
	} {
		t.Run(strings.Join(spelling, " "), func(t *testing.T) {
			keyring.MockInit()
			issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "reference-status"})
			shell, _, errOut := newLoginShell(t)
			installLogin(t, shell, browserDoc(issuer.URL))
			shell.OpenBrowser = followAuthorizationURL()
			if code := shell.Run([]string{"login"}); code != exit.OK {
				t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
			}
			errOut.Reset()

			args := append([]string{"logout"}, spelling...)
			if code := shell.Run(append(args, "--verbose")); code != exit.OK {
				t.Fatalf("verbose logout failed: exit %d, stderr %s", code, errOut)
			}
			if errOut.Len() == 0 {
				t.Fatal("--verbose wrote no diagnostics")
			}
			for _, line := range strings.Split(strings.TrimSpace(errOut.String()), "\n") {
				var record map[string]any
				if err := json.Unmarshal([]byte(line), &record); err != nil {
					t.Fatalf("the diagnostic line %q is not JSON under %v: %v", line, spelling, err)
				}
			}
		})
	}
}

// TestVerboseIsHonoredAfterAProductNamespace covers the path that never reaches
// Cobra at all: a product namespace is routed straight to the module store, so
// a flag written after it is taken there or nowhere.
func TestVerboseIsHonoredAfterAProductNamespace(t *testing.T) {
	shell, _, errOut := newShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})

	// The fixture executable says nothing the contract recognizes, so the run
	// fails after the module is resolved. What it wrote on the way there is the
	// subject, so the exit code is not asserted.
	shell.Run([]string{"reference", "status", "--verbose"})

	for _, expected := range []string{"the shell started", "resolved a product namespace"} {
		if !strings.Contains(errOut.String(), expected) {
			t.Fatalf("wso2 reference status --verbose is missing %q in:\n%s", expected, errOut)
		}
	}
}

// TestVerboseIsNotForwardedToAModule is the other half. Until a module declares
// its command tree the shell cannot tell a flag it should pass on from one the
// module owns, so a module that does not know --verbose would refuse the whole
// command. The flag is taken and forwarded to nothing; see forwardShellFlags.
func TestVerboseIsNotForwardedToAModule(t *testing.T) {
	directory := t.TempDir()
	hello := filepath.Join(directory, "hello.bin")
	invocation := filepath.Join(directory, "invocation.bin")
	var handshake bytes.Buffer
	if err := protocol.NewWriter(&handshake).WriteEnvelope(&contractv1.Envelope{
		Message: &contractv1.Envelope_Hello{Hello: &contractv1.Hello{
			Module:           &contractv1.ModuleIdentity{Namespace: "reference", Version: "0.1.0"},
			ProtocolVersions: []uint32{1},
		}},
	}); err != nil {
		t.Fatalf("encoding the fixture handshake: %v", err)
	}
	if err := os.WriteFile(hello, handshake.Bytes(), 0o600); err != nil {
		t.Fatalf("writing the fixture handshake: %v", err)
	}

	shell, _, errOut := newShell(t)
	installFixture(t, shell, fixture.Module{
		Namespace: "reference",
		Version:   "0.1.0",
		// The invocation reaches a module on its standard input and nowhere
		// else, so answering the handshake and keeping a copy of what arrives
		// next is the only way to see what the module was actually told. The
		// wait ends the moment the copy lands rather than after a fixed pause,
		// and is bounded so a shell that sends nothing fails the test instead
		// of hanging it.
		// The copy runs in the background off an explicitly duplicated
		// descriptor: a shell redirects a background job's standard input from
		// /dev/null otherwise, and the copy would record nothing.
		Contents: []byte("#!/bin/sh\ncat " + hello + "\nexec 3<&0\ncat <&3 > " + invocation + " &\n" +
			"attempts=0\nwhile [ ! -s " + invocation + " ] && [ $attempts -lt 200 ]; do\n" +
			"attempts=$((attempts+1))\nsleep 0.05\ndone\nexit 0\n"),
	})

	// The fixture answers the handshake and then says nothing the contract
	// recognizes, so the run fails. What the module was sent is the subject,
	// so the exit code is not asserted.
	shell.Run([]string{"reference", "status", "--verbose"})

	sent, err := os.ReadFile(invocation)
	if err != nil {
		t.Fatalf("the module recorded no invocation: %v; stderr: %s", err, errOut)
	}
	// The command the user did ask for has to be in there, or the check below
	// is reading an empty page.
	if !strings.Contains(string(sent), "status") {
		t.Fatalf("the recorded invocation does not name the command:\n%q", sent)
	}
	if strings.Contains(string(sent), "--verbose") {
		t.Fatalf("--verbose reached the module:\n%q", sent)
	}
}
