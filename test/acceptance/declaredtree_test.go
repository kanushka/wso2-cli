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
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/modules/fixture"
	"github.com/wso2/wso2-cli/internal/state"
	"github.com/wso2/wso2-cli/internal/statusservice"
	"github.com/wso2/wso2-cli/sdk/commandtree"
	"github.com/wso2/wso2-cli/sdk/module"
)

// installDeclaringModule builds the module that declares its command tree,
// reads that tree out of the built executable the way the installer does, and
// installs both together.
//
// The tree is extracted rather than written by hand, so what these tests parse
// against is what the module's own Cobra tree produced. A hand-written fixture
// tree would let the shell and the module agree on a declaration neither of
// them generates.
func installDeclaringModule(t *testing.T, stateRoot string) {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "declaringmodule"+executableSuffix())
	build(t, repoRoot(t), binary,
		"-X main.moduleVersion="+testModuleVersion+
			" -X github.com/wso2/wso2-cli/sdk/protocol.Version="+testProtocolVersion,
		"./test/acceptance/testdata/declaringmodule")

	if _, err := fixture.Install(state.ModuleStore(stateRoot), fixture.Module{
		Namespace:        "reference",
		Version:          testModuleVersion,
		ShellRange:       ">=0.1.0 <1.0.0",
		ProtocolVersions: []int{testProtocolVersionNumber},
		SourcePath:       binary,
		AuthAudiences:    []string{referenceAudience},
		AuthScopes:       []string{referenceReadScope},
		CommandTree:      extractCommandTree(t, binary),
	}); err != nil {
		t.Fatalf("fixture.Install returned %v", err)
	}
}

// extractCommandTree asks a built module for its declaration, the same way the
// installer asks an executable it has just unpacked.
func extractCommandTree(t *testing.T, binary string) commandtree.Tree {
	t.Helper()
	answer := filepath.Join(t.TempDir(), "declaration.json")
	command := exec.Command(binary)
	command.Env = append(os.Environ(), module.CommandTreeEnv+"="+answer)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("asking %s to declare itself failed: %v\n%s", binary, err, output)
	}
	content, err := os.ReadFile(answer)
	if err != nil {
		t.Fatalf("reading the declaration: %v", err)
	}
	var declared module.Declaration
	if err := json.Unmarshal(content, &declared); err != nil {
		t.Fatalf("decoding the declaration: %v", err)
	}
	if declared.CommandTree.Empty() {
		t.Fatal("the module declared no commands, so nothing under test would be exercised")
	}
	return declared.CommandTree
}

// TestTheOutputFlagIsReadWhereverItIsWrittenOnAProductLine is the defect this
// mechanism exists to remove, proven through the shell as a user runs it.
//
// Before a module declared its commands the shell stopped reading at the first
// flag it did not recognise, so "--output json" written after a product flag was
// forwarded to the module instead of read, and the user got a table with nothing
// saying their flag had been ignored.
func TestTheOutputFlagIsReadWhereverItIsWrittenOnAProductLine(t *testing.T) {
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	installDeclaringModule(t, stateRoot)
	service := startStatusService(t, statusservice.Options{})
	installReferenceContext(t, stateRoot, service.server.URL, credentialVariable)

	lines := map[string][]string{
		"before the product flag": {"reference", "status", "--output", "json", "--since", "1h"},
		"after the product flag":  {"reference", "status", "--since", "1h", "--output", "json"},
		"after a boolean flag":    {"reference", "status", "--all", "--output", "json"},
	}
	for name, args := range lines {
		t.Run(name, func(t *testing.T) {
			stdout, _ := runShell(t, shell, stateRoot, args...)

			var reported map[string]any
			if err := json.Unmarshal([]byte(stdout), &reported); err != nil {
				t.Fatalf("wso2 %s did not render json: %v\n%s",
					strings.Join(args, " "), err, stdout)
			}
			if reported["command"] != "status" {
				t.Errorf("the module answered for %q", reported["command"])
			}
			if arguments, _ := reported["arguments"].(string); strings.Contains(arguments, "--output") {
				t.Errorf("the shell forwarded its own flag to the module: %s", arguments)
			}
		})
	}
}

// TestAProductFlagTheCommandDoesNotDeclareIsRefused proves the shell now names a
// mistyped product flag itself, before anything is launched and before any
// access is brokered.
func TestAProductFlagTheCommandDoesNotDeclareIsRefused(t *testing.T) {
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	installDeclaringModule(t, stateRoot)
	service := startStatusService(t, statusservice.Options{})
	installReferenceContext(t, stateRoot, service.server.URL, credentialVariable)

	stdout, stderr, err := runShellWith(shell, shellEnvironment(stateRoot),
		"reference", "status", "--sinces", "1h")

	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 64 {
		t.Fatalf("exit status = %v, want 64\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stderr, "--sinces") {
		t.Errorf("the refusal does not name the flag:\n%s", stderr)
	}
	if !strings.Contains(stderr, "wso2 reference status --help") {
		t.Errorf("the refusal does not say how to see what the command takes:\n%s", stderr)
	}
}

// TestAMistypedProductCommandIsSuggested proves the shell can answer for a
// module's commands. Cobra's suggestions never reached inside a product
// namespace, because the shell did not know what was in one.
func TestAMistypedProductCommandIsSuggested(t *testing.T) {
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	installDeclaringModule(t, stateRoot)
	service := startStatusService(t, statusservice.Options{})
	installReferenceContext(t, stateRoot, service.server.URL, credentialVariable)

	stdout, stderr, err := runShellWith(shell, shellEnvironment(stateRoot), "reference", "stats")

	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 64 {
		t.Fatalf("exit status = %v, want 64\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stderr, "Did you mean wso2 reference status?") {
		t.Errorf("no suggestion was offered:\n%s", stderr)
	}
}

// TestANestedProductCommandRoutesToTheModule proves the declared path is what
// travels, so a command below the namespace's top level reaches its handler.
func TestANestedProductCommandRoutesToTheModule(t *testing.T) {
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	installDeclaringModule(t, stateRoot)
	service := startStatusService(t, statusservice.Options{})
	installReferenceContext(t, stateRoot, service.server.URL, credentialVariable)

	stdout, _ := runShell(t, shell, stateRoot, "reference", "apps", "list", "--output", "json")

	var reported map[string]any
	if err := json.Unmarshal([]byte(stdout), &reported); err != nil {
		t.Fatalf("wso2 reference apps list did not render json: %v\n%s", err, stdout)
	}
	if reported["command"] != "apps list" {
		t.Errorf("the module answered for %q", reported["command"])
	}
}
