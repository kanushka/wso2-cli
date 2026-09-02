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

package acceptance_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/auth/devtoken"
	"github.com/wso2/wso2-cli/internal/statusservice"
)

// The canary scan is the proof's central security claim, stated once: a module
// runs, and the credential the shell holds is not somewhere the module, the
// user, the file system, or a crash report can find it.
//
// Every other test in this package asserts the absence of the credential
// alongside whatever else it is checking. These tests do nothing else, and they
// go looking: they install a module written to disclose everything it can
// reach, they read the state the run left on disk, and they read the bytes of
// the shipped executables.
//
// Each one first proves it was looking at something. A scan of an empty stream
// passes for the wrong reason, and a security assertion that cannot fail is
// worse than no assertion at all.

// disclosurePrefix opens every line the disclosing fixture writes. It matches
// DisclosurePrefix in testdata/canarymodule.
const disclosurePrefix = "canary-disclosure: "

func TestAModuleThatDisclosesAllItCanReachStillDisclosesNoCredential(t *testing.T) {
	// This is the adversarial case. The module asks for access, is granted it,
	// and then publishes its arguments, its whole environment, the invocation,
	// the context, and the token itself through both channels it has.
	shell := buildShell(t)

	for _, mode := range []struct {
		name string
		args []string
	}{
		{name: "table", args: []string{"reference", "call"}},
		{name: "json", args: []string{"reference", "call", "--output", "json"}},
	} {
		t.Run(mode.name, func(t *testing.T) {
			stateRoot := isolatedStateRoot(t)
			installCanaryModule(t, stateRoot)
			service := startStatusService(t, statusservice.Options{})
			installReferenceContext(t, stateRoot, service.server.URL, credentialVariable)

			stdout, stderr := runShell(t, shell, stateRoot, mode.args...)

			disclosed := disclosures(t, stderr)
			// The module must have held real access, or the scan below proves
			// only that an empty token discloses nothing.
			token := disclosed["accessToken"]
			if !strings.HasPrefix(token, devtoken.Prefix) {
				t.Fatalf("the module was not granted a fixture token, so this run scans nothing; "+
					"accessToken = %q, accessError = %q", token, disclosed["accessError"])
			}
			if disclosed["accessExpiry"] == "" {
				t.Errorf("the granted access names no expiry, so the module holds access without end")
			}
			// The token is derived from the credential and must not carry it.
			// This is the one surface a module is meant to hold, so it is
			// worth stating separately from the sweep.
			if strings.Contains(token, canaryCredential) {
				t.Errorf("the fixture token carries the credential it was derived from")
			}
			if environment := disclosed["environment"]; strings.Contains(environment, credentialVariable) {
				t.Errorf("the module's environment names the credential source: %s", environment)
			}
			// Standard output is scanned too, so it also has to be carrying
			// what the module disclosed. A renderer that dropped this result
			// would leave a stream that passes the scan by being empty.
			if !strings.Contains(stdout, token) {
				t.Fatalf("the rendered output does not carry what the module disclosed, "+
					"so this run scans an empty stream\nstdout:\n%s", stdout)
			}

			assertNoCredentialDisclosure(t, stdout, stderr)
		})
	}
}

func TestNoFileTheRunLeavesBehindHoldsTheCredential(t *testing.T) {
	// Receipts, the active-version pointer, and the context document are all
	// written before or during a run. None of them may hold a credential: the
	// context names where to read one, and that is the whole of what state
	// knows about it.
	shell := buildShell(t)
	deployed := deploy(t, statusservice.Options{})

	runShell(t, shell, deployed.stateRoot, "reference", "call")

	scanned := 0
	err := filepath.WalkDir(deployed.stateRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		scanned++
		if strings.Contains(string(content), canaryCredential) {
			relative, _ := filepath.Rel(deployed.stateRoot, path)
			t.Errorf("the state file %s holds the source credential", relative)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the isolated state root: %v", err)
	}
	// A run installs a module executable, a receipt, an active-version
	// pointer, and a context document. Finding fewer files than that means the
	// walk missed the state rather than that the state is clean.
	if scanned < 4 {
		t.Fatalf("the scan read only %d state files, so it proves nothing", scanned)
	}
}

func TestNoTypedProblemDisclosesTheCredential(t *testing.T) {
	// A refusal is where a credential is most likely to escape, because a
	// refusal is written to explain itself. Each of these fails for a
	// different reason, and none of them may explain itself with the
	// credential.
	shell := buildShell(t)

	for _, refusal := range []struct {
		name    string
		arrange func(t *testing.T) string
		problem string
	}{
		{
			name:    "an undeclared audience",
			problem: "auth.audience_not_declared",
			arrange: func(t *testing.T) string {
				// The disclosing module receives this denial and republishes
				// it, so the scan reads the denial as the module saw it and
				// as the user sees it.
				stateRoot := isolatedStateRoot(t)
				installModule(t, stateRoot, canaryModuleBinary(t), nil, nil)
				service := startStatusService(t, statusservice.Options{})
				installReferenceContext(t, stateRoot, service.server.URL, credentialVariable)
				return stateRoot
			},
		},
		{
			name:    "a service that refuses the claims",
			problem: "reference.status_access_rejected",
			arrange: func(t *testing.T) string {
				deployed := deploy(t, statusservice.Options{Organization: "another-org"})
				return deployed.stateRoot
			},
		},
		{
			name:    "a module the shell refuses to invoke",
			problem: "rpc.identity_mismatch",
			arrange: func(t *testing.T) string {
				stateRoot := isolatedStateRoot(t)
				installFaultyModule(t, stateRoot, faultNamespaceMismatch)
				service := startStatusService(t, statusservice.Options{})
				installReferenceContext(t, stateRoot, service.server.URL, credentialVariable)
				return stateRoot
			},
		},
	} {
		t.Run(refusal.name, func(t *testing.T) {
			stateRoot := refusal.arrange(t)

			stdout, stderr, _ := runShellWithCeiling(t, shell, stateRoot, "reference", "call")

			// Proving the run failed the way it was meant to is what makes the
			// scan meaningful: a run that succeeded quietly would have no
			// problem text to search.
			if !strings.Contains(stderr, refusal.problem) {
				t.Fatalf("the run did not report %s, so this scan reads the wrong failure\nstderr:\n%s",
					refusal.problem, stderr)
			}
			assertNoCredentialDisclosure(t, stdout, stderr)
		})
	}
}

func TestNoCrashDiagnosticDisclosesTheCredential(t *testing.T) {
	// A panicking module writes a stack trace the shell did not compose, and a
	// flooding module writes more than the shell will keep. Neither is text
	// the shell can redact after the fact, so neither may contain a credential
	// in the first place.
	shell := buildShell(t)

	for _, fault := range []string{faultPanic, faultFloodDiagnostics, faultExtraFrame} {
		t.Run(fault, func(t *testing.T) {
			stateRoot := isolatedStateRoot(t)
			installFaultyModule(t, stateRoot, fault)
			service := startStatusService(t, statusservice.Options{})
			installReferenceContext(t, stateRoot, service.server.URL, credentialVariable)

			stdout, stderr, _ := runShellWithCeiling(t, shell, stateRoot, "reference", "call")

			if stderr == "" {
				t.Fatalf("the %s fault produced no diagnostics, so this scan reads nothing", fault)
			}
			assertNoCredentialDisclosure(t, stdout, stderr)
		})
	}
}

func TestTheBuiltArtifactsCarryNoDevelopmentCredential(t *testing.T) {
	// A credential a build embeds is one every copy of that build carries. The
	// executables are read as bytes, because a Go string constant survives
	// compilation as its own bytes.
	for _, artifact := range []struct {
		name string
		path string
	}{
		{name: "the shell", path: buildShell(t)},
		{name: "the reference module", path: buildReferenceModule(t)},
	} {
		content, err := os.ReadFile(artifact.path)
		if err != nil {
			t.Fatalf("cannot read %s: %v", artifact.name, err)
		}
		if len(content) == 0 {
			t.Fatalf("%s is empty, so this scan reads nothing", artifact.name)
		}
		if strings.Contains(string(content), canaryCredential) {
			t.Errorf("%s was built with the source credential inside it", artifact.name)
		}
	}
}

func TestTheShellHasNoCredentialToFallBackOn(t *testing.T) {
	// The scan above proves no credential was compiled in under the name this
	// harness uses. This proves the stronger thing: with no credential in the
	// environment at all, the shell has nothing to authenticate with and says
	// so, rather than reaching for a built-in one.
	shell := buildShell(t)
	deployed := deploy(t, statusservice.Options{})

	stdout, stderr, err := runShellWithoutCredential(t, shell, deployed.stateRoot, "reference", "call")

	if exitCode(t, err) != exitAuthPolicy {
		t.Fatalf("exit status = %v, want the authentication class %d\nstderr:\n%s", err, exitAuthPolicy, stderr)
	}
	if !strings.Contains(stderr, "auth.credential_unavailable") {
		t.Errorf("stderr does not report the missing credential:\n%s", stderr)
	}
	if deployed.calls.Load() != 0 {
		t.Errorf("a shell with no credential still called the status service %d times", deployed.calls.Load())
	}
	if stdout != "" {
		t.Errorf("a shell with no credential still wrote a result:\n%s", stdout)
	}
}

// runShellWithoutCredential runs the shell with the credential source removed
// from its environment, leaving everything else a run needs.
func runShellWithoutCredential(t *testing.T, shell, stateRoot string, args ...string) (string, string, error) {
	t.Helper()
	var environment []string
	for _, entry := range shellEnvironment(stateRoot) {
		if !strings.HasPrefix(entry, credentialVariable+"=") {
			environment = append(environment, entry)
		}
	}

	return runShellWith(shell, environment, args...)
}

// disclosures reads the values the disclosing fixture wrote to standard error,
// keyed by name.
func disclosures(t *testing.T, stderr string) map[string]string {
	t.Helper()
	disclosed := make(map[string]string)
	for _, line := range strings.Split(stderr, "\n") {
		// The shell attributes every diagnostic to the namespace that wrote
		// it, so the fixture's own prefix is inside the rendered line rather
		// than at the start of it.
		_, reported, found := strings.Cut(strings.TrimSpace(line), disclosurePrefix)
		if !found {
			continue
		}
		name, value, _ := strings.Cut(reported, "=")
		disclosed[name] = value
	}
	if len(disclosed) == 0 {
		t.Fatalf("the module disclosed nothing, so this run scans nothing\nstderr:\n%s", stderr)
	}
	return disclosed
}

// installCanaryModule installs the disclosing fixture under the reference
// module's namespace, version, and declared access.
func installCanaryModule(t *testing.T, stateRoot string) {
	t.Helper()
	installModule(t, stateRoot, canaryModuleBinary(t),
		[]string{referenceAudience}, []string{referenceReadScope})
}

// canaryModuleBinary builds the disclosing fixture.
func canaryModuleBinary(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "canarymodule"+executableSuffix())
	build(t, repoRoot(t), binary,
		"-X main.moduleVersion="+testModuleVersion+
			" -X github.com/wso2/wso2-cli/sdk/protocol.Version="+testProtocolVersion,
		"./test/acceptance/testdata/canarymodule")
	return binary
}
