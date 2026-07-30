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
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/statusservice"
)

// A product module is meant to live in the product's own repository, written by
// people who have no access to this one. The reference module lives here only
// because there is nothing to publish yet, and that convenience is exactly what
// could quietly become a dependency: an import of a shell internal, a
// requirement on the shell module, or a build that works only inside this
// workspace.
//
// The other boundary tests read the module's imports and requirements and find
// nothing wrong. This test stops reading and moves it.

func TestTheReferenceModuleWorksFromAnotherRepository(t *testing.T) {
	shell := buildShell(t)
	relocated := relocateReferenceModule(t)

	stateRoot := isolatedStateRoot(t)
	installReferenceModule(t, stateRoot, relocated)
	service := startStatusService(t, statusservice.Options{})
	installReferenceContext(t, stateRoot, service.server.URL, credentialVariable)

	stdout, stderr := runShell(t, shell, stateRoot, "reference", "status", "--output", "json")

	// The module built somewhere else answers the same command through the
	// same contract with the same brokered access. Nothing about being outside
	// this repository changed what it is.
	status := decodeStatusJSON(t, stdout)
	if status["organization"] != referenceOrganization {
		t.Errorf("organization = %q, want %q", status["organization"], referenceOrganization)
	}
	if status["status"] != "operational" {
		t.Errorf("status = %q, want %q", status["status"], "operational")
	}
	if service.calls.Load() != 1 {
		t.Errorf("the relocated module called the status service %d times, want 1", service.calls.Load())
	}
	assertNoCredentialDisclosure(t, stdout, stderr)
}

// relocateReferenceModule copies the reference module outside this repository,
// resolves the SDK the way a published dependency would be resolved, builds it
// there, and returns the executable.
//
// It proves the copy is a copy: every Go source file has to arrive byte for
// byte, so a relocation cannot be made to work by editing an import on the way
// out. Only go.mod is touched, and only to say where the SDK is — which is the
// one thing publishing an SDK release makes unnecessary.
func relocateReferenceModule(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	source := filepath.Join(root, "examples", "reference-module")
	// t.TempDir is outside this checkout, so the copy is outside the Go
	// workspace as well: nothing here composes it with the SDK any more.
	destination := filepath.Join(t.TempDir(), "product-repository")

	copyTree(t, source, destination)
	assertSourcesIdentical(t, source, destination)

	// GOWORK=off is what makes this a real relocation. With the workspace
	// still in force the copy would resolve the SDK from this checkout however
	// far away it was moved, and prove nothing.
	environment := []string{"GOWORK=off"}
	sdkRequirement := "github.com/wso2/wso2-cli/sdk@" + sdkPlaceholderVersion
	runGoIn(t, destination, environment,
		"mod", "edit", "-replace", sdkRequirement+"="+filepath.Join(root, "sdk"))
	// The published SDK's own dependencies are recorded here, because a module
	// outside a workspace has to carry its own checksums.
	runGoIn(t, destination, environment, "mod", "tidy")

	binary := filepath.Join(destination, "wso2-module-reference"+executableSuffix())
	runGoIn(t, destination, environment, "build", "-ldflags", strings.Join([]string{
		"-X main.moduleVersion=" + testModuleVersion,
		"-X github.com/wso2/wso2-cli/sdk/module.SDKVersion=" + testSDKVersion,
		"-X github.com/wso2/wso2-cli/sdk/protocol.Version=" + testProtocolVersion,
	}, " "), "-o", binary, "./cmd/wso2-module-reference")
	return binary
}

// sdkPlaceholderVersion is the unpublished SDK version the reference module
// requires. It is the version go.work resolves from this checkout today, and
// the version a real release replaces.
const sdkPlaceholderVersion = "v0.0.0"

// copyTree copies a directory recursively, preserving whether a file is
// executable.
func copyTree(t *testing.T, source, destination string) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, content, info.Mode().Perm())
	})
	if err != nil {
		t.Fatalf("copying the reference module to %s: %v", destination, err)
	}
}

// assertSourcesIdentical proves every Go file arrived unchanged.
func assertSourcesIdentical(t *testing.T, source, destination string) {
	t.Helper()
	compared := 0
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		original, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		copied, err := os.ReadFile(filepath.Join(destination, relative))
		if err != nil {
			return err
		}
		compared++
		if string(original) != string(copied) {
			t.Errorf("%s changed on the way out of this repository", relative)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("comparing the relocated sources: %v", err)
	}
	if compared == 0 {
		t.Fatal("the relocated module has no Go sources, so nothing was compared")
	}
}

// runGoIn runs the Go tool in a directory with extra environment entries.
func runGoIn(t *testing.T, directory string, environment []string, args ...string) {
	t.Helper()
	command := exec.Command("go", args...)
	command.Dir = directory
	command.Env = append(os.Environ(), environment...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("go %s in %s failed: %v\n%s", strings.Join(args, " "), directory, err, output)
	}
}
