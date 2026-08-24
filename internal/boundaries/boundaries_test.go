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
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// goModules reports every Go module in this repository, by directory relative
// to the repository root.
//
// The product modules are discovered rather than listed, because a list is a
// second place a module has to be registered and the one place a new module is
// forgotten. Every boundary these tests state — the license header, the
// prohibition on replace directives, workspace composition, and the rule that a
// module may not reach into the shell's internals — then covers a module the
// moment it exists, including one a scaffold has just created.
func goModules(t *testing.T) []string {
	t.Helper()
	modules := []string{".", "sdk"}
	modules = append(modules, productModules(t)...)
	return modules
}

// productModules reports the product modules, by directory relative to the
// repository root.
func productModules(t *testing.T) []string {
	t.Helper()
	root := repoRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, "modules"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("cannot read the modules directory: %v", err)
	}

	var modules []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// A directory is a module when it declares one. Anything else there is
		// not something these boundaries are about.
		if _, err := os.Stat(filepath.Join(root, "modules", entry.Name(), "go.mod")); err != nil {
			continue
		}
		modules = append(modules, "modules/"+entry.Name())
	}
	if len(modules) == 0 {
		t.Fatal("no product module was found under modules/")
	}
	return modules
}

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
	for _, module := range goModules(t) {
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

	for _, module := range goModules(t) {
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

	for _, module := range goModules(t) {
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

func TestTheWorkspaceDeclaresNoReplacements(t *testing.T) {
	// The workspace replaced an SDK version that had never been published,
	// because the Go tool cannot build a module graph that requires a version
	// with no revision. sdk/v0.1.0 exists now, so every module requires a
	// version the module proxy can serve and the replacement is gone.
	//
	// A replacement here would let the workspace conceal a dependency a
	// released build would not have, and there is no longer a reason to allow
	// one. See docs/adr/0009-sdk-versioning-and-publication.md.
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "go.work"))
	if err != nil {
		t.Fatalf("cannot read go.work: %v", err)
	}

	for _, line := range strings.Split(string(data), "\n") {
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "replace") {
			t.Errorf("go.work declares the replacement %q; the published SDK needs none", trimmed)
		}
	}
}

func TestEveryProductModuleRequiresAPublishedSDKVersion(t *testing.T) {
	// The placeholder was never published, so a module requiring it builds only
	// where a workspace or a replacement resolves it. Requiring the version this
	// release actually published is what makes a module an ordinary consumer of
	// the SDK, and therefore what makes it something a product team can own and
	// release.
	//
	// Every product module is checked rather than the reference module alone, so
	// a scaffolded module that reintroduced the placeholder, or drifted to some
	// other unpublished version, would be caught.
	const (
		sdkModule    = "github.com/wso2/wso2-cli/sdk"
		sdkPublished = "v0.1.0"
	)

	for _, module := range productModules(t) {
		path := filepath.Join(repoRoot(t), module, "go.mod")
		requirement := requiredVersion(t, path, sdkModule)
		if requirement != sdkPublished {
			t.Errorf("%s requires %s %s; the published SDK is %s", path, sdkModule, requirement, sdkPublished)
		}
	}
}

// requiredVersion reports the version at which a go.mod requires the named
// module, via `go mod edit -json` rather than a text match, so a require
// spread across a block, or reordered by `go mod tidy`, is still found.
func requiredVersion(t *testing.T, goModPath, modulePath string) string {
	t.Helper()

	output, err := exec.Command("go", "mod", "edit", "-json", goModPath).Output()
	if err != nil {
		t.Fatalf("go mod edit -json %s: %v", goModPath, err)
	}

	var parsed struct {
		Require []struct {
			Path    string
			Version string
		}
	}
	if err := json.Unmarshal(output, &parsed); err != nil {
		t.Fatalf("cannot parse go mod edit -json output for %s: %v", goModPath, err)
	}

	for _, requirement := range parsed.Require {
		if requirement.Path == modulePath {
			return requirement.Version
		}
	}
	t.Fatalf("%s does not require %s", goModPath, modulePath)
	return ""
}

func TestTheSDKAndReferenceModuleCannotImportShellInternals(t *testing.T) {
	root := repoRoot(t)
	const shellInternalPrefix = "github.com/wso2/wso2-cli/internal"

	for _, module := range append([]string{"sdk"}, productModules(t)...) {
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
	required := requiredModules(t, filepath.Join(repoRoot(t), "modules", "reference"))

	if !slices.Contains(required, "github.com/wso2/wso2-cli/sdk") {
		t.Errorf("the reference module does not require the public SDK; it requires %v", required)
	}
	if slices.Contains(required, "github.com/wso2/wso2-cli") {
		t.Errorf("the reference module requires the shell module; it must depend on the public SDK only")
	}
}

func TestNoModuleWritesToStandardOutputOutsideTheProtocol(t *testing.T) {
	// A module's standard output carries protocol frames only, so anything else
	// written there corrupts the stream the shell is reading. The Cobra adapter
	// points every writer in a command tree at standard error, but it cannot
	// stop a handler printing directly, so the source is asserted as well.
	//
	// sdk/module/serve.go is the one legitimate writer: it is what hands the
	// stream to the protocol in the first place.
	root := repoRoot(t)
	allowed := filepath.Join("sdk", "module", "serve.go")

	for _, tree := range []string{"sdk", "modules"} {
		err := filepath.WalkDir(filepath.Join(root, tree), func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			relative, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			if relative == allowed || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			source, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			for _, forbidden := range []string{"os.Stdout", "fmt.Print(", "fmt.Println(", "fmt.Printf("} {
				if strings.Contains(string(source), forbidden) {
					t.Errorf("%s writes to standard output with %s; a module's standard output carries protocol frames only",
						relative, forbidden)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s failed: %v", tree, err)
		}
	}
}

func TestTheShellLinksTheCommandFrameworkAndNotItsDocumentationGenerator(t *testing.T) {
	// Cobra's documentation generator pulls a Markdown renderer and a YAML
	// parser into the module graph. Neither belongs in a binary whose premise is
	// verified execution, and neither is needed to route commands, so the linked
	// set is asserted rather than left to whoever adds the next import. Man page
	// or Markdown generation belongs in a separate developer tool.
	linked := shellBinaryPackages(t)

	for _, required := range []string{"github.com/spf13/cobra", "github.com/spf13/pflag"} {
		if !slices.Contains(linked, required) {
			t.Errorf("the shell binary does not link %s", required)
		}
	}
	for _, forbidden := range []string{
		"github.com/spf13/cobra/doc",
		"github.com/cpuguy83/go-md2man/v2/md2man",
		"go.yaml.in/yaml/v3",
		"gopkg.in/yaml.v3",
	} {
		if slices.Contains(linked, forbidden) {
			t.Errorf("the shell binary links %s; command routing does not need it", forbidden)
		}
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
	for _, module := range goModules(t) {
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
			if path != directory && isModuleRoot(t, root, path) {
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
func isModuleRoot(t *testing.T, root, directory string) bool {
	t.Helper()
	for _, module := range goModules(t) {
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
