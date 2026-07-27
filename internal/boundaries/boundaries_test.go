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
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
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

	var paths []string
	for _, module := range goModules {
		paths = append(paths, goFiles(t, filepath.Join(root, module))...)
	}

	for _, path := range paths {
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
	const shellInternalPrefix = "github.com/wso2/wso2-cli/internal"

	for _, module := range []string{"sdk", "examples/reference-module"} {
		for _, path := range goFiles(t, filepath.Join(root, module)) {
			// The import declarations are parsed rather than matched as text:
			// a raw-string import literal is still an import, and a path
			// mentioned in a comment is not.
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("cannot parse %s: %v", path, err)
			}
			relative, _ := filepath.Rel(root, path)
			for _, imported := range file.Imports {
				importPath, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					t.Fatalf("%s has an unreadable import literal %s", relative, imported.Path.Value)
				}
				if importPath == shellInternalPrefix || strings.HasPrefix(importPath, shellInternalPrefix+"/") {
					t.Errorf("%s imports the shell internal package %q; %s must depend on public packages only",
						relative, importPath, module)
				}
			}
		}
	}
}

func TestTheReferenceModuleDependsOnThePublicSDKOnly(t *testing.T) {
	// A module that requires only the public SDK can move to another repository
	// without changing its imports. The requirements are read from the module
	// graph rather than matched as text, so block syntax and comments cannot
	// change the outcome.
	required := requiredModules(t, filepath.Join(repoRoot(t), "examples", "reference-module"))

	if !slices.Contains(required, "github.com/wso2/wso2-cli/sdk") {
		t.Errorf("the reference module does not require the public SDK; it requires %v", required)
	}
	if slices.Contains(required, "github.com/wso2/wso2-cli") {
		t.Errorf("the reference module requires the shell module; it must depend on the public SDK only")
	}
}

// requiredModules reports the module paths one module requires, read from its
// go.mod through the Go tool.
func requiredModules(t *testing.T, moduleDir string) []string {
	t.Helper()
	command := exec.Command("go", "mod", "edit", "-json")
	command.Dir = moduleDir
	command.Env = os.Environ()
	output, err := command.Output()
	if err != nil {
		t.Fatalf("go mod edit -json in %s failed: %v", moduleDir, err)
	}

	var parsed struct {
		Require []struct {
			Path string `json:"Path"`
		} `json:"Require"`
	}
	if err := json.Unmarshal(output, &parsed); err != nil {
		t.Fatalf("cannot read the module requirements of %s: %v", moduleDir, err)
	}
	paths := make([]string, 0, len(parsed.Require))
	for _, requirement := range parsed.Require {
		paths = append(paths, requirement.Path)
	}
	return paths
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

// goFiles lists every Go source file below a directory, without descending into
// another of this repository's modules.
func goFiles(t *testing.T, directory string) []string {
	t.Helper()
	root := repoRoot(t)
	var paths []string
	err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != directory && isModuleRoot(root, path) {
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
		t.Fatalf("walking %s: %v", directory, err)
	}
	if len(paths) == 0 {
		t.Fatalf("found no Go files below %s", directory)
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
