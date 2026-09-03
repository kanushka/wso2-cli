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

	stdout, stderr := runShell(t, shell, stateRoot, "reference", "call", "--output", "json")

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
// out. Nothing is touched at all now that an SDK release is published: the copy
// resolves the SDK by the version it requires, which is what a module in a
// product team's own repository does.
func relocateReferenceModule(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	source := filepath.Join(root, "modules", "reference")
	// t.TempDir is outside this checkout, so the copy is outside the Go
	// workspace as well: nothing here composes it with the SDK any more.
	destination := filepath.Join(t.TempDir(), "product-repository")

	copyTree(t, source, destination)
	assertSourcesIdentical(t, source, destination)

	// GOWORK=off is what makes this a real relocation. With the workspace
	// still in force the copy would resolve the SDK from this checkout however
	// far away it was moved, and prove nothing.
	//
	// Nothing is edited on the way out any more. This copy resolves the SDK the
	// way any consumer does — by the version its own go.mod requires, from the
	// module proxy — which is what an SDK release made possible and is the whole
	// of what this test now proves. It needs a warm or reachable module cache,
	// like every other build in this repository.
	environment := []string{"GOWORK=off"}
	skipUntilTheRequiredSDKIsPublished(t, destination, environment)
	// The SDK's own dependencies are recorded here, because a module outside a
	// workspace has to carry its own checksums.
	runGoIn(t, destination, environment, "mod", "tidy")

	binary := filepath.Join(destination, "wso2-module-reference"+executableSuffix())
	runGoIn(t, destination, environment, "build", "-ldflags", strings.Join([]string{
		"-X main.moduleVersion=" + testModuleVersion,
		"-X github.com/wso2/wso2-cli/sdk/module.SDKVersion=" + testSDKVersion,
		"-X github.com/wso2/wso2-cli/sdk/protocol.Version=" + testProtocolVersion,
	}, " "), "-o", binary, "./cmd/wso2-module-reference")
	return binary
}

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

// skipUntilTheRequiredSDKIsPublished skips this test while the SDK version the
// reference module requires has no tag yet.
//
// This is the release bootstrap, and it has a precedent. Until sdk/v0.1.0 the
// workspace carried a replace for an SDK version that had never been published,
// because the Go tool cannot build a module graph that requires a version with
// no revision, and go.work says so. That replace covers every build in this
// repository except this one: the relocation is defined by GOWORK=off, so it
// resolves the SDK from the module proxy and cannot see the workspace at all.
//
// The window is narrow and self-closing. Bumping modules/reference/go.mod is
// what the SDK release gate compares its tag against, so the requirement has to
// name the new version before the tag exists, and the tag makes it fetchable
// moments later. What this skip buys is that the commit in between builds and
// its suite is honest, rather than red for a reason unrelated to the change.
//
// A proxy that cannot be reached is a failure rather than an empty answer, on
// the same reasoning scripts/previous-protocol.sh states: reading a network
// blip as "nothing is published" would turn this into a test that skips itself
// whenever it is inconvenient, which is the opposite of what it is for.
func skipUntilTheRequiredSDKIsPublished(t *testing.T, moduleDirectory string, environment []string) {
	t.Helper()
	required := requiredSDKVersion(t, moduleDirectory)
	for _, published := range publishedSDKVersions(t, environment) {
		if published == required {
			return
		}
	}
	t.Skipf("the reference module requires SDK %s, which is not published yet: "+
		"relocation resolves the SDK from the module proxy, so it cannot be "+
		"proven until the tag exists. Push sdk/%s to close this window.",
		required, required)
}

// requiredSDKVersion reads the SDK version the relocated module requires.
//
// It is read through go rather than by matching text, for the reason the SDK
// release gate reads it that way: a go.mod spells a single requirement on one
// line and a grouped one over several, and only go itself reads both the way
// the toolchain does.
func requiredSDKVersion(t *testing.T, moduleDirectory string) string {
	t.Helper()
	command := exec.Command("go", "mod", "edit", "-json")
	command.Dir = moduleDirectory
	output, err := command.Output()
	if err != nil {
		t.Fatalf("reading the relocated module's requirements failed: %v", err)
	}
	var parsed struct {
		Require []struct {
			Path    string
			Version string
		}
	}
	if err := json.Unmarshal(output, &parsed); err != nil {
		t.Fatalf("the relocated module's requirements are unreadable: %v", err)
	}
	for _, requirement := range parsed.Require {
		if requirement.Path == sdkModulePath {
			return requirement.Version
		}
	}
	t.Fatalf("the relocated module requires no %s version", sdkModulePath)
	return ""
}

// publishedSDKVersions asks the module proxy what the SDK has published.
//
// The question is asked from a probe module rather than from the relocated copy
// because the copy is what may require an unpublished version, and go cannot
// load a module graph it cannot resolve. The probe requires nothing, so the
// listing succeeds whatever the copy asks for.
func publishedSDKVersions(t *testing.T, environment []string) []string {
	t.Helper()
	probe := t.TempDir()
	if err := os.WriteFile(filepath.Join(probe, "go.mod"),
		[]byte("module probe\n\ngo 1.25.0\n"), 0o644); err != nil {
		t.Fatalf("writing the probe module failed: %v", err)
	}
	command := exec.Command("go", "list", "-m", "-versions", sdkModulePath)
	command.Dir = probe
	command.Env = append(os.Environ(), environment...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("the module proxy could not be asked what the SDK has published, "+
			"so whether the relocated module can resolve it is unknown: %v\n%s", err, output)
	}
	// The answer is the module path followed by every version, space separated.
	fields := strings.Fields(string(output))
	if len(fields) == 0 {
		return nil
	}
	return fields[1:]
}

// sdkModulePath is the published SDK a relocated module resolves by version.
const sdkModulePath = "github.com/wso2/wso2-cli/sdk"
