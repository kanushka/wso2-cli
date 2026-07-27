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

// Package boundaries_test enforces the build boundaries the architecture proof
// depends on: three independently buildable Go modules, an SDK that cannot
// reach shell internals, and no committed local dependency replacement.
//
// These are tests rather than documentation because a boundary that is not
// enforced automatically is a boundary that erodes.
package boundaries_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// goModules are every Go module in this repository, by directory relative to
// the repository root.
var goModules = []string{".", "sdk", "examples/reference-module"}

// buildArgs builds a module's packages, writing any executable to a temporary
// directory so a build check never leaves artifacts in the working tree. The Go
// tool rejects an output directory for a module with no main package, so a
// library-only module builds without one.
func buildArgs(t *testing.T, module string) []string {
	t.Helper()
	if module == "sdk" {
		return []string{"build", "./..."}
	}
	return []string{"build", "-o", t.TempDir() + string(os.PathSeparator), "./..."}
}

// licenseHeader is the Apache-2.0 notice every Go file in this repository must
// carry, as the first bytes of the file.
const licenseHeader = `// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
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
`

func TestEveryGoFileCarriesTheLicenseHeader(t *testing.T) {
	root := repoRoot(t)

	for _, path := range goFiles(t, root) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("cannot read %s: %v", path, err)
		}
		relative, _ := filepath.Rel(root, path)
		if !strings.HasPrefix(string(data), licenseHeader) {
			t.Errorf("%s does not begin with the Apache-2.0 license header", relative)
			continue
		}
		// A blank line keeps the header out of the package documentation.
		if !strings.HasPrefix(string(data[len(licenseHeader):]), "\n") {
			t.Errorf("%s does not separate the license header from the following declaration with a blank line", relative)
		}
	}
}

func TestEveryModuleBuildsInTheLocalWorkspace(t *testing.T) {
	root := repoRoot(t)

	for _, module := range goModules {
		t.Run(module, func(t *testing.T) {
			runGo(t, filepath.Join(root, module), nil, buildArgs(t, module)...)
		})
	}
}

func TestTheSDKBuildsAndTestsWithWorkspaceCompositionDisabled(t *testing.T) {
	// The SDK is published and consumed independently, so it must be valid
	// without the local workspace composing anything for it.
	sdk := filepath.Join(repoRoot(t), "sdk")

	runGo(t, sdk, []string{"GOWORK=off"}, "build", "./...")
	runGo(t, sdk, []string{"GOWORK=off"}, "test", "./...")
}

func TestNoCommittedModuleReplacesALocalCheckout(t *testing.T) {
	// A committed local replace would let the workspace conceal an invalid
	// release dependency. Local composition belongs in go.work only.
	root := repoRoot(t)
	replaceDirective := regexp.MustCompile(`(?m)^\s*replace\s|^\s*replace\s*\(`)

	for _, module := range goModules {
		path := filepath.Join(root, module, "go.mod")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("cannot read %s: %v", path, err)
		}
		if replaceDirective.Match(data) {
			t.Errorf("%s contains a replace directive; compose local modules with go.work instead", path)
		}
	}
}

func TestTheSDKAndReferenceModuleCannotImportShellInternals(t *testing.T) {
	root := repoRoot(t)
	shellInternal := regexp.MustCompile(`"github\.com/wso2/wso2-cli/internal[/"]`)

	for _, module := range []string{"sdk", "examples/reference-module"} {
		moduleRoot := filepath.Join(root, module)
		err := filepath.WalkDir(moduleRoot, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if shellInternal.Match(data) {
				relative, _ := filepath.Rel(root, path)
				t.Errorf("%s imports a shell internal package; %s must depend on public packages only", relative, module)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", moduleRoot, err)
		}
	}
}

func TestTheReferenceModuleDependsOnThePublicSDKOnly(t *testing.T) {
	// A module that imports only the public SDK can move to another
	// repository without changing its imports.
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "examples", "reference-module", "go.mod"))
	if err != nil {
		t.Fatalf("cannot read the reference module go.mod: %v", err)
	}
	text := string(data)

	if !strings.Contains(text, "github.com/wso2/wso2-cli/sdk") {
		t.Fatal("the reference module does not require the public SDK")
	}
	if strings.Contains(text, "github.com/wso2/wso2-cli v") {
		t.Fatal("the reference module requires the shell module; it must depend on the public SDK only")
	}
}

func TestTheWorkspaceComposesEveryModule(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "go.work"))
	if err != nil {
		t.Fatalf("cannot read go.work: %v", err)
	}
	for _, module := range goModules {
		entry := module
		if entry != "." {
			entry = "./" + filepath.ToSlash(entry)
		}
		if !strings.Contains(string(data), "\t"+entry+"\n") {
			t.Errorf("go.work does not compose %q", module)
		}
	}
}

// goFiles lists every Go source file tracked in the repository, across all
// three modules.
func goFiles(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	for _, module := range goModules {
		err := filepath.WalkDir(filepath.Join(root, module), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				// Nested modules are walked under their own entry.
				if path != filepath.Join(root, module) && isModuleRoot(root, path) {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(path, ".go") {
				paths = append(paths, path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", module, err)
		}
	}
	if len(paths) == 0 {
		t.Fatal("found no Go files to check")
	}
	return paths
}

// isModuleRoot reports whether the directory is one of this repository's other
// Go modules.
func isModuleRoot(root, directory string) bool {
	for _, module := range goModules {
		if module != "." && directory == filepath.Join(root, module) {
			return true
		}
	}
	return false
}

// repoRoot walks up from the test's working directory to the directory holding
// go.work.
func repoRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot determine the working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.work")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("cannot locate the repository root: no go.work found in any parent directory")
		}
		directory = parent
	}
}

// runGo runs the Go tool in the given directory with extra environment
// entries, failing the test with the tool's combined output.
func runGo(t *testing.T, directory string, environment []string, args ...string) {
	t.Helper()
	command := exec.Command("go", args...)
	command.Dir = directory
	command.Env = append(os.Environ(), environment...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("go %s in %s failed: %v\n%s", strings.Join(args, " "), directory, err, output)
	}
}
