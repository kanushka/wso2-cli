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

// Confirming before wso2 module remove and wso2 module update --all destroy
// anything (#112 §7). These tests drive the shell in-process rather than
// through the built binary, which is the whole reason Shell grew a reader
// (#86): a piped-stdin refusal is proved against the real, unmodified
// process standard input (nil Reader, the ordinary case for a Shell built
// directly), and an answered prompt is proved by injecting a reader that is
// not that descriptor, which is the only way this process can hand a
// confirmation an answer without a real terminal to type it at.
package app_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/modules/fixture"
)

// newModuleShell is a shell with WSO2_NO_INPUT neutralized, so a developer's
// own environment cannot decide what a test proves.
func newModuleShell(t *testing.T) (app.Shell, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	t.Setenv("WSO2_NO_INPUT", "")
	return newShell(t)
}

// failIfReadReader fails the test the moment anything reads from it. It
// proves a code path never reaches the point of asking a question, rather
// than reaching it and getting an answer that happens not to matter.
type failIfReadReader struct{ t *testing.T }

func (r failIfReadReader) Read([]byte) (int, error) {
	r.t.Fatal("the shell read from standard input when it should have refused before asking")
	return 0, io.EOF
}

// installedNamespace reports whether one namespace has anything in the
// shell's managed module store.
func installedNamespace(t *testing.T, shell app.Shell, namespace string) bool {
	t.Helper()
	store := modules.NewStore(storeRoot(shell))
	installed, err := store.Installed(namespace)
	if err != nil {
		t.Fatalf("Store.Installed(%q) returned %v", namespace, err)
	}
	return installed
}

// pinModule writes a version policy pinning a namespace at an exact version,
// the same fact installer.Update reads to decide a module is held rather
// than movable.
func pinModule(t *testing.T, shell app.Shell, namespace, version string) {
	t.Helper()
	store := modules.NewStore(storeRoot(shell))
	policy := modules.Policy{
		SchemaVersion: modules.PolicySchemaVersion,
		Namespace:     namespace,
		PinnedVersion: version,
	}
	data, err := policy.Encode()
	if err != nil {
		t.Fatalf("Policy.Encode returned %v", err)
	}
	if err := os.WriteFile(store.PolicyPath(namespace), data, 0o600); err != nil {
		t.Fatalf("writing the version policy returned %v", err)
	}
}

// TestModuleRemoveRefusesToPromptOnPipedStdin pins the trap this task names
// directly: a confirmation that reads when it should not is worse than no
// confirmation. No reader is injected, so this Shell reads the real test
// process's own standard input, which `go test` connects to /dev/null — not
// a terminal, so mayPrompt (prompt.go) must refuse before anything is read.
//
// Deleting the terminal check in mayPrompt is the mutant this is written
// against: the code would then fall through to confirm(), which scans
// /dev/null, gets EOF immediately, reads that as "no", and the module would
// still survive — but the exit code and problem code would change from this
// test's 64/shell.non_interactive to 0 with a "cancelled" message on Out
// instead of Err, and this test asserts both.
func TestModuleRemoveRefusesToPromptOnPipedStdin(t *testing.T) {
	shell, out, errOut := newModuleShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})

	if code := shell.Run([]string{"module", "remove", "reference"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want %d (usage); stderr: %s", code, exit.Usage, errOut)
	}
	requireRefusal(t, errOut.String(), "shell.non_interactive")
	if !strings.Contains(errOut.String(), "standard input is not a terminal") {
		t.Errorf("the refusal does not name standard input:\n%s", errOut)
	}
	// The terminal is the way out only here, where no control asked for
	// non-interactive operation. See TestModuleRemoveRefusalNamesTheControl
	// ThatFired for the cases where offering it would be unactionable advice.
	if !strings.Contains(errOut.String(), "run this where standard input is a terminal") {
		t.Errorf("the recovery does not offer the terminal, the way out that applies here:\n%s", errOut)
	}
	if !installedNamespace(t, shell, "reference") {
		t.Error("the module was removed despite the refusal")
	}
	if out.String() != "" {
		t.Errorf("a refused removal wrote to standard output:\n%s", out)
	}
}

// TestModuleRemoveRefusalNamesTheControlThatFired covers WSO2_NO_INPUT and
// --no-input, and that the flag is named in preference to the variable when
// both are set — the flag is on the command line in front of the reader, the
// variable can have been set in a shell profile months ago.
//
// The recovery is asserted as well, in both directions. mayPrompt consults
// these two controls before the terminal check, so telling a caller who set
// one of them to "run this where standard input is a terminal" advises a
// change that would not alter the outcome — and CI, where they are set
// deliberately, is where this refusal is most often read. The negative
// assertion is the load-bearing half: a recovery that lists every way out
// would satisfy wantRecovery alone.
func TestModuleRemoveRefusalNamesTheControlThatFired(t *testing.T) {
	for _, testCase := range []struct {
		name         string
		args         []string
		env          string
		want         string
		wantRecovery string
	}{
		{"the flag", []string{"--no-input"}, "", "--no-input", "drop --no-input"},
		{"the environment variable", nil, "1", "WSO2_NO_INPUT", "unset WSO2_NO_INPUT"},
		{"both set, the flag wins", []string{"--no-input"}, "1", "--no-input", "drop --no-input"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			shell, out, errOut := newModuleShell(t)
			installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})
			if testCase.env != "" {
				t.Setenv("WSO2_NO_INPUT", testCase.env)
			}
			// Injected on purpose: if the wrong control were consulted first,
			// a read here would prove it by failing the test rather than by a
			// silently wrong message.
			shell.Reader = failIfReadReader{t}

			args := append([]string{"module", "remove", "reference"}, testCase.args...)
			if code := shell.Run(args); code != exit.Usage {
				t.Fatalf("exit code = %d, want %d (usage); stderr: %s", code, exit.Usage, errOut)
			}
			requireRefusal(t, errOut.String(), "shell.non_interactive")
			if !strings.Contains(errOut.String(), testCase.want) {
				t.Errorf("the refusal does not name %s:\n%s", testCase.want, errOut)
			}
			if !strings.Contains(errOut.String(), testCase.wantRecovery) {
				t.Errorf("the recovery does not offer %q, the one way out that applies:\n%s",
					testCase.wantRecovery, errOut)
			}
			if strings.Contains(errOut.String(), "standard input is a terminal") {
				t.Errorf("the recovery advises a terminal, which %s makes irrelevant:\n%s",
					testCase.want, errOut)
			}
			if !installedNamespace(t, shell, "reference") {
				t.Error("the module was removed despite the refusal")
			}
			if out.String() != "" {
				t.Errorf("a refused removal wrote to standard output:\n%s", out)
			}
		})
	}
}

// TestModuleRemoveYesSkipsThePromptAndRemoves proves --yes never has to ask.
func TestModuleRemoveYesSkipsThePromptAndRemoves(t *testing.T) {
	shell, out, errOut := newModuleShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})
	// A reader that fails the test if read: --yes must never reach confirm().
	shell.Reader = failIfReadReader{t}

	if code := shell.Run([]string{"module", "remove", "reference", "--yes"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.Contains(out.String(), "Removed the reference module") {
		t.Errorf("stdout does not report the removal:\n%s", out)
	}
	if installedNamespace(t, shell, "reference") {
		t.Error("--yes did not remove the module")
	}
}

// TestModuleRemoveDryRunRemovesNothing asserts against the state root itself,
// not just the rendered output: a dry run that quietly still removed
// something would still print a plausible-looking preview.
func TestModuleRemoveDryRunRemovesNothing(t *testing.T) {
	shell, out, errOut := newModuleShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})
	shell.Reader = failIfReadReader{t}

	if code := shell.Run([]string{"module", "remove", "reference", "--dry-run"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.Contains(out.String(), "reference") {
		t.Errorf("stdout does not name the module the run would remove:\n%s", out)
	}
	if !installedNamespace(t, shell, "reference") {
		t.Error("--dry-run removed the module")
	}
}

// TestModuleRemoveRefusesYesAndDryRunTogether pins the choice this task left
// open: asked to both skip the prompt and skip acting, the command refuses as
// a usage error rather than picking one silently.
func TestModuleRemoveRefusesYesAndDryRunTogether(t *testing.T) {
	shell, _, errOut := newModuleShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})
	shell.Reader = failIfReadReader{t}

	code := shell.Run([]string{"module", "remove", "reference", "--yes", "--dry-run"})
	if code != exit.Usage {
		t.Fatalf("exit code = %d, want %d (usage); stderr: %s", code, exit.Usage, errOut)
	}
	requireRefusal(t, errOut.String(), "shell.conflicting_arguments")
	if !installedNamespace(t, shell, "reference") {
		t.Error("a refused invocation removed the module")
	}
}

// TestModuleRemoveAnsweringNoLeavesTheModuleInstalled reaches the actual
// prompt by injecting a reader (#86, prompt.go): this is what
// Shell.Reader exists for, since the real test process's own stdin can never
// be made to look like a terminal.
func TestModuleRemoveAnsweringNoLeavesTheModuleInstalled(t *testing.T) {
	shell, out, errOut := newModuleShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})
	shell.Reader = strings.NewReader("no\n")

	if code := shell.Run([]string{"module", "remove", "reference"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.Contains(out.String(), "cancelled") {
		t.Errorf("stdout does not report the cancellation:\n%s", out)
	}
	if !strings.Contains(errOut.String(), "Remove the reference module?") {
		t.Errorf("stderr does not carry the prompt itself:\n%s", errOut)
	}
	if !installedNamespace(t, shell, "reference") {
		t.Error("answering no removed the module")
	}
}

// TestModuleRemoveOnAModuleThatIsNotInstalledDoesNotPromptFirst is the
// ordering this task asked to be stated and pinned: the store is asked
// whether the namespace is installed before anything about confirmation
// runs, so a typo never gets as far as a prompt asking to confirm deleting
// something that was never there.
func TestModuleRemoveOnAModuleThatIsNotInstalledDoesNotPromptFirst(t *testing.T) {
	shell, out, errOut := newModuleShell(t)
	// Nothing is installed. A reader that fails the test the moment it is
	// read proves the not-installed refusal returns before any prompt would
	// have consulted it.
	shell.Reader = failIfReadReader{t}

	if code := shell.Run([]string{"module", "remove", "reference"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want %d (usage); stderr: %s", code, exit.Usage, errOut)
	}
	requireRefusal(t, errOut.String(), "shell.module_not_installed")
	if out.String() != "" {
		t.Errorf("a refused removal wrote to standard output:\n%s", out)
	}
}

// catalogServing starts an origin that answers index.json with the given
// body and points the shell environment at it for the duration of the test.
func catalogServing(t *testing.T, body string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+catalog.IndexPath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	t.Setenv(catalog.OriginEnvVar, server.URL)
}

// TestModuleUpdateAllRefusesToPromptOnPipedStdin is update's half of the same
// trap remove is proved against: wso2 module update --all is the form #112
// §7 names as acting immediately today. An unpinned module is installed so
// NothingWouldMove (F6, fix round 1) does not skip the confirmation this test
// is proving: without something that might move, there would be nothing to
// refuse permission for.
func TestModuleUpdateAllRefusesToPromptOnPipedStdin(t *testing.T) {
	shell, out, errOut := newModuleShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})

	if code := shell.Run([]string{"module", "update", "--all"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want %d (usage); stderr: %s", code, exit.Usage, errOut)
	}
	requireRefusal(t, errOut.String(), "shell.non_interactive")
	if !strings.Contains(errOut.String(), "standard input is not a terminal") {
		t.Errorf("the refusal does not name standard input:\n%s", errOut)
	}
	if out.String() != "" {
		t.Errorf("a refused update wrote to standard output:\n%s", out)
	}
}

// TestModuleUpdateAllSkipsThePromptWhenNothingIsInstalled is F6 from the
// first fix round: asking permission to do nothing trains a person to answer
// without reading. With nothing installed, NothingWouldMove is true from
// local state alone, so this reaches success without ever consulting
// mayPrompt — proved by leaving standard input exactly as piped as the test
// above, which would otherwise refuse.
func TestModuleUpdateAllSkipsThePromptWhenNothingIsInstalled(t *testing.T) {
	shell, out, errOut := newModuleShell(t)
	shell.Reader = failIfReadReader{t}

	if code := shell.Run([]string{"module", "update", "--all"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.Contains(out.String(), "No modules are installed.") {
		t.Errorf("stdout = %q, want the no-modules report", out.String())
	}
}

// TestModuleUpdateAllSkipsThePromptWhenEveryInstalledModuleIsPinned is F6's
// other network-free case: every installed module pinned is, like nothing
// installed, knowable without asking the catalog anything, so this also
// reaches success on piped standard input without prompting. Executing the
// (no-op, since everything is pinned) run afterward still needs a catalog
// origin, since Update fetches the index once it has anything selected.
func TestModuleUpdateAllSkipsThePromptWhenEveryInstalledModuleIsPinned(t *testing.T) {
	shell, out, errOut := newModuleShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})
	pinModule(t, shell, "reference", "0.1.0")
	shell.Reader = failIfReadReader{t}
	catalogServing(t, `{"schemaVersion":1,"modules":[]}`)

	if code := shell.Run([]string{"module", "update", "--all"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.Contains(out.String(), "reference is pinned to v0.1.0 and was not updated.") {
		t.Errorf("stdout does not report the pin:\n%s", out)
	}
}

// TestModuleUpdateAllYesSkipsThePrompt proves --yes reaches the real update
// path rather than being refused, using an empty inventory so the run needs
// no catalog at all — install.Update returns before any request when nothing
// is installed, so this is a network-free proof that the prompt, not the
// update itself, is what --yes bypassed.
func TestModuleUpdateAllYesSkipsThePrompt(t *testing.T) {
	shell, out, errOut := newModuleShell(t)
	shell.Reader = failIfReadReader{t}

	if code := shell.Run([]string{"module", "update", "--all", "--yes"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.Contains(out.String(), "No modules are installed.") {
		t.Errorf("stdout = %q, want the no-modules report", out.String())
	}
}

// TestModuleUpdateAllRefusesYesAndDryRunTogether mirrors remove's pinned
// choice: the same conflict is refused the same way on both commands, which
// is what "one shared answer" for a usage conflict is supposed to mean.
func TestModuleUpdateAllRefusesYesAndDryRunTogether(t *testing.T) {
	shell, _, errOut := newModuleShell(t)
	shell.Reader = failIfReadReader{t}

	code := shell.Run([]string{"module", "update", "--all", "--yes", "--dry-run"})
	if code != exit.Usage {
		t.Fatalf("exit code = %d, want %d (usage); stderr: %s", code, exit.Usage, errOut)
	}
	requireRefusal(t, errOut.String(), "shell.conflicting_arguments")
}

// TestModuleUpdateAllAnsweringNoChangesNothing reaches the prompt the same
// way the remove test does: by injecting a reader, since confirm() returns
// before installer.Update ever runs, this needs no catalog origin either.
func TestModuleUpdateAllAnsweringNoChangesNothing(t *testing.T) {
	shell, out, errOut := newModuleShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})
	shell.Reader = strings.NewReader("no\n")

	if code := shell.Run([]string{"module", "update", "--all"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.Contains(out.String(), "cancelled") {
		t.Errorf("stdout does not report the cancellation:\n%s", out)
	}
	active, err := modules.NewStore(storeRoot(shell)).ReadActive("reference")
	if err != nil {
		t.Fatalf("ReadActive returned %v", err)
	}
	if active.Version != "0.1.0" {
		t.Errorf("the active version is %s, want the untouched 0.1.0", active.Version)
	}
}

// TestModuleUpdateAllDryRunReportsWithoutChanging exercises --dry-run against
// a real catalog response and a module the index reports no newer version
// for, asserting against the store, not just the message, that nothing moved.
func TestModuleUpdateAllDryRunReportsWithoutChanging(t *testing.T) {
	shell, out, errOut := newModuleShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})
	shell.Reader = failIfReadReader{t}
	// The channel publishes the version already installed, so this is a
	// genuinely current module, not #135's unpublished one: an empty catalog
	// here would say nothing about currency at all.
	catalogServing(t, `{"schemaVersion":1,"modules":[`+
		`{"namespace":"reference","path":"reference","channels":`+
		`[{"channel":"stable","version":"0.1.0"}]}]}`)

	if code := shell.Run([]string{"module", "update", "--all", "--dry-run"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.Contains(out.String(), "reference is already current at v0.1.0") {
		t.Errorf("stdout does not report the plan:\n%s", out)
	}
	active, err := modules.NewStore(storeRoot(shell)).ReadActive("reference")
	if err != nil {
		t.Fatalf("ReadActive returned %v", err)
	}
	if active.Version != "0.1.0" {
		t.Errorf("the active version is %s, want the untouched 0.1.0", active.Version)
	}
}

// TestModuleUpdateAllDryRunReportsAPinnedModuleWithoutChanging exercises
// dryRunUpdateLine's Pinned branch, which mirrors updateOne's own Pinned
// branch: a held module is reported as held, never as a candidate to move.
func TestModuleUpdateAllDryRunReportsAPinnedModuleWithoutChanging(t *testing.T) {
	shell, out, errOut := newModuleShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})
	pinModule(t, shell, "reference", "0.1.0")
	shell.Reader = failIfReadReader{t}
	// A newer stable version is published, so a mutant that dropped the
	// Pinned check ahead of the Update check would report this as movable
	// instead of held.
	catalogServing(t, `{"schemaVersion":1,"modules":[`+
		`{"namespace":"reference","path":"reference","channels":`+
		`[{"channel":"stable","version":"0.2.0"}]}]}`)

	if code := shell.Run([]string{"module", "update", "--all", "--dry-run"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.Contains(out.String(), "reference is pinned to v0.1.0 and would not be updated.") {
		t.Errorf("stdout does not report the pin:\n%s", out)
	}
	active, err := modules.NewStore(storeRoot(shell)).ReadActive("reference")
	if err != nil {
		t.Fatalf("ReadActive returned %v", err)
	}
	if active.Version != "0.1.0" {
		t.Errorf("the active version is %s, want the untouched 0.1.0", active.Version)
	}
}

// TestModuleUpdateAllDryRunReportsAnAvailableUpdateWithoutChanging exercises
// dryRunUpdateLine's Update branch — the entire point of a dry run, saying
// what WOULD change — and proves it changes nothing itself.
func TestModuleUpdateAllDryRunReportsAnAvailableUpdateWithoutChanging(t *testing.T) {
	shell, out, errOut := newModuleShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})
	shell.Reader = failIfReadReader{t}
	catalogServing(t, `{"schemaVersion":1,"modules":[`+
		`{"namespace":"reference","path":"reference","channels":`+
		`[{"channel":"stable","version":"0.2.0"}]}]}`)

	if code := shell.Run([]string{"module", "update", "--all", "--dry-run"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.Contains(out.String(), "reference would be updated from v0.1.0 to v0.2.0.") {
		t.Errorf("stdout does not report the available update:\n%s", out)
	}
	active, err := modules.NewStore(storeRoot(shell)).ReadActive("reference")
	if err != nil {
		t.Fatalf("ReadActive returned %v", err)
	}
	if active.Version != "0.1.0" {
		t.Errorf("--dry-run changed the active version to %s", active.Version)
	}
}

// TestModuleUpdateNamedModuleDryRunReportsWithoutChanging covers --dry-run on
// a named target, which parseUpdateArguments and reportUpdatePlan both accept
// regardless of scope even though the confirmation gate itself only guards
// --all.
func TestModuleUpdateNamedModuleDryRunReportsWithoutChanging(t *testing.T) {
	shell, out, errOut := newModuleShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})
	shell.Reader = failIfReadReader{t}
	catalogServing(t, `{"schemaVersion":1,"modules":[`+
		`{"namespace":"reference","path":"reference","channels":`+
		`[{"channel":"stable","version":"0.2.0"}]}]}`)

	if code := shell.Run([]string{"module", "update", "reference", "--dry-run"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.Contains(out.String(), "reference would be updated from v0.1.0 to v0.2.0.") {
		t.Errorf("stdout does not report the plan:\n%s", out)
	}
	active, err := modules.NewStore(storeRoot(shell)).ReadActive("reference")
	if err != nil {
		t.Fatalf("ReadActive returned %v", err)
	}
	if active.Version != "0.1.0" {
		t.Errorf("--dry-run changed the active version to %s", active.Version)
	}
}

// TestModuleUpdateOfOneNamedModuleNeedsNoConfirmation pins the scope judgment
// call this task left open: #112 §7 names wso2 module update --all as the
// form that acts immediately, not a named update, which is already as
// explicit an intent as this shell asks anywhere else (the same intent a
// single wso2 module remove <module> already carries). No reader is
// injected and no answer is possible, so this also proves the confirmation
// gate is not consulted for a named target: a real (non-failing) reader
// would be needed if it were.
// TestModuleUpdateUnknownFlagRecoveryNamesTheAcceptedFlags is F4 from the
// first fix round: a user who mistypes one of update's own new flags used to
// get a recovery message that did not mention it, since the recovery text
// predated --yes, --dry-run, and --no-input.
func TestModuleUpdateUnknownFlagRecoveryNamesTheAcceptedFlags(t *testing.T) {
	shell, _, errOut := newModuleShell(t)

	if code := shell.Run([]string{"module", "update", "--all", "--bogus"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want %d (usage); stderr: %s", code, exit.Usage, errOut)
	}
	requireRefusal(t, errOut.String(), "shell.unknown_flag")
	for _, want := range []string{"--yes", "--dry-run", "--no-input"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("the recovery does not name %s:\n%s", want, errOut)
		}
	}
}

func TestModuleUpdateOfOneNamedModuleNeedsNoConfirmation(t *testing.T) {
	shell, out, errOut := newModuleShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})
	shell.Reader = failIfReadReader{t}
	// The channel publishes the version already installed, so this is a
	// genuinely current module, not #135's unpublished one: an empty catalog
	// here would say nothing about currency at all.
	catalogServing(t, `{"schemaVersion":1,"modules":[`+
		`{"namespace":"reference","path":"reference","channels":`+
		`[{"channel":"stable","version":"0.1.0"}]}]}`)

	if code := shell.Run([]string{"module", "update", "reference"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.Contains(out.String(), "reference is current at v0.1.0") {
		t.Errorf("stdout does not report the outcome:\n%s", out)
	}
}
