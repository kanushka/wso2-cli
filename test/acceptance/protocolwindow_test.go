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
	"strconv"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/modules/fixture"
	"github.com/wso2/wso2-cli/internal/state"
	"github.com/wso2/wso2-cli/internal/statusservice"
	"github.com/wso2/wso2-cli/sdk/protocol"
)

// The protocol window is the shell's promise that a user one protocol
// generation behind still gets module releases. Proving it needs a real shell
// launching a real module across the generation gap, so every test here builds
// the shell with the window it declares — no protocol ldflag — and builds the
// reference module against one protocol version at a time.

// TestTheDeclaredWindowIsTheCurrentProtocolAndItsPredecessor pins the shape of
// the single declaration the shell reads, and proves the shell speaks it.
func TestTheDeclaredWindowIsTheCurrentProtocolAndItsPredecessor(t *testing.T) {
	window := protocol.Window()

	if len(window) != 2 {
		t.Fatalf("protocol.Window() = %v, want the current version and its predecessor", window)
	}
	if window[0] != window[1]+1 {
		t.Fatalf("protocol.Window() = %v, want consecutive versions newest first", window)
	}

	shell := buildShellSpeaking(t, "")
	stdout, _ := runShell(t, shell, isolatedStateRoot(t), "version")

	want := "v" + strconv.Itoa(window[0]) + ", v" + strconv.Itoa(window[1])
	if !strings.Contains(stdout, want) {
		t.Fatalf("wso2 version does not report the protocol window %q:\n%s", want, stdout)
	}
}

// TestAModuleAtEitherEndOfTheProtocolWindowRuns is the criterion the window
// exists for: the module is a real subprocess built against one protocol
// version, and the shell speaking both launches it.
func TestAModuleAtEitherEndOfTheProtocolWindowRuns(t *testing.T) {
	shell := buildShellSpeaking(t, "")
	window := protocol.Window()

	for _, version := range window {
		t.Run("protocol v"+strconv.Itoa(version), func(t *testing.T) {
			deployment := deployModuleSpeaking(t, version, testModuleVersion)

			stdout, stderr := deployment.run(t, shell, "reference", "status")

			if !strings.Contains(stdout, "operational") {
				t.Fatalf("wso2 reference status did not report the service:\n%s", stdout)
			}
			if stderr != "" {
				t.Errorf("wso2 reference status wrote diagnostics:\n%s", stderr)
			}
		})
	}
}

// TestAModuleOutsideTheProtocolWindowIsRefusedAsACompatibilityProblem proves
// the refusal names both sides, so a user reads a compatibility problem rather
// than a generic failure.
func TestAModuleOutsideTheProtocolWindowIsRefusedAsACompatibilityProblem(t *testing.T) {
	shell := buildShellSpeaking(t, "")
	window := protocol.Window()
	beyond := window[0] + 1

	deployment := deployModuleSpeaking(t, beyond, testModuleVersion)

	stdout, stderr, err := deployment.try(shell, "reference", "status")

	if got := exitCode(t, err); got != exitModuleTrust {
		t.Fatalf("exit status = %d, want %d\nstderr:\n%s", got, exitModuleTrust, stderr)
	}
	if !strings.Contains(stderr, "modules.incompatible_protocol") {
		t.Errorf("stderr does not report a protocol compatibility problem:\n%s", stderr)
	}
	for _, version := range append([]int{beyond}, window...) {
		if !strings.Contains(stderr, "v"+strconv.Itoa(version)) {
			t.Errorf("stderr does not name protocol v%d:\n%s", version, stderr)
		}
	}
	if stdout != "" {
		t.Errorf("a refused command still wrote to standard output:\n%s", stdout)
	}
}

// TestAModuleVersionFarFromTheShellsRunsNormally proves the launch gate is the
// protocol window intersected with the platform and nothing else. The rule and
// its reason are recorded at negotiateProtocol in internal/modules/resolve.go
// and in docs/architecture.md section 10.
func TestAModuleVersionFarFromTheShellsRunsNormally(t *testing.T) {
	shell := buildShellSpeaking(t, "")
	current := protocol.Window()[0]

	for _, moduleVersion := range []string{"0.0.1", "4242.7.0"} {
		t.Run("module v"+moduleVersion, func(t *testing.T) {
			deployment := deployModuleSpeaking(t, current, moduleVersion)

			stdout, stderr := deployment.run(t, shell, "reference", "status")

			if !strings.Contains(stdout, "operational") {
				t.Fatalf("wso2 reference status did not report the service:\n%s", stdout)
			}
			if stderr != "" {
				t.Errorf("wso2 reference status wrote diagnostics:\n%s", stderr)
			}
		})
	}
}

// deployModuleSpeaking installs a reference module built against one protocol
// version, at one module version, and deploys the status service it answers
// from.
func deployModuleSpeaking(t *testing.T, protocolVersion int, moduleVersion string) deployment {
	t.Helper()
	stateRoot := isolatedStateRoot(t)
	if _, err := fixture.Install(state.ModuleStore(stateRoot), fixture.Module{
		Namespace:        "reference",
		Version:          moduleVersion,
		ProtocolVersions: []int{protocolVersion},
		SourcePath:       buildReferenceModuleSpeaking(t, strconv.Itoa(protocolVersion), moduleVersion),
		AuthAudiences:    []string{referenceAudience},
		AuthScopes:       []string{referenceReadScope},
	}); err != nil {
		t.Fatalf("fixture.Install returned %v", err)
	}
	return deployInstalled(t, stateRoot, statusservice.Options{})
}
