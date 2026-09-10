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
	"fmt"
	"go/ast"
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

func TestTheWorkspaceReplacesNothingOutsideAnSDKReleaseWindow(t *testing.T) {
	// A replacement here would let the workspace conceal a dependency a
	// released build would not have, so the only one permitted is the one that
	// cannot be avoided.
	//
	// The SDK release gate compares its tag against the version the product
	// modules require, so that requirement has to name the new version before
	// the tag exists — and the Go tool cannot build a module graph that
	// requires a version with no revision, so without a replacement every build
	// in the repository fails. The workspace carried exactly this until
	// sdk/v0.1.0 was published.
	//
	// One replacement is allowed, and only in the shape that window has: the
	// SDK, at exactly the version every product module requires, pointing at
	// this checkout. Anything else is the concealment this test exists to
	// prevent. TestEveryProductModuleRequiresAResolvableSDKVersion is the other
	// half — neither the requirement nor the replacement can move without the
	// other. See docs/adr/0009-sdk-versioning-and-publication.md and
	// docs/reference/release-artifacts.md.
	for _, replacement := range workspaceReplacements(t) {
		if !isSDKReleaseWindow(t, replacement) {
			t.Errorf("go.work declares %s; the only replacement permitted is %s, "+
				"the SDK release window", replacement, sdkReleaseWindowDescription(t))
		}
	}
}

// workspaceReplacement is one replace directive go.work declares.
type workspaceReplacement struct {
	Old struct{ Path, Version string }
	New struct{ Path, Version string }
}

func (r workspaceReplacement) String() string {
	old := r.Old.Path
	if r.Old.Version != "" {
		old += " " + r.Old.Version
	}
	return "replace " + old + " => " + r.New.Path
}

// workspaceReplacements reports every replace directive go.work declares.
//
// It reads them through `go work edit -json` rather than by matching text, for
// the reason requiredVersion reads a requirement that way: only the toolchain
// is guaranteed to read every spelling the file permits. A replacement inside a
// parenthesised block is the case a line-by-line reader gets wrong, and getting
// it wrong here would reject a legitimate release window rather than admit an
// illegitimate replacement.
func workspaceReplacements(t *testing.T) []workspaceReplacement {
	t.Helper()
	command := exec.Command("go", "work", "edit", "-json")
	command.Dir = repoRoot(t)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("go work edit -json: %v", err)
	}
	var parsed struct {
		Replace []workspaceReplacement
	}
	if err := json.Unmarshal(output, &parsed); err != nil {
		t.Fatalf("cannot parse go work edit -json output: %v", err)
	}
	return parsed.Replace
}

// isSDKReleaseWindow reports whether a replacement is the one a release window
// may declare: the SDK, at exactly the version the product modules require,
// from this checkout.
//
// Reading the version from the requirement rather than naming it here is what
// ties the two halves together — a requirement bumped without the replacement,
// or a replacement left behind after the tag, fails.
func isSDKReleaseWindow(t *testing.T, replacement workspaceReplacement) bool {
	t.Helper()
	return replacement.Old.Path == sdkModulePath &&
		replacement.Old.Version == requiredSDKVersion(t) &&
		replacement.New.Path == "./sdk"
}

// sdkReleaseWindowDescription names the permitted replacement for a failure
// message.
func sdkReleaseWindowDescription(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("replace %s %s => ./sdk", sdkModulePath, requiredSDKVersion(t))
}

// requiredSDKVersion reports the SDK version the product modules require,
// failing when they disagree: they are released together and a split
// requirement has no meaning for the gate that compares a tag against it.
func requiredSDKVersion(t *testing.T) string {
	t.Helper()
	var agreed string
	for _, module := range productModules(t) {
		path := filepath.Join(repoRoot(t), module, "go.mod")
		requirement := requiredVersion(t, path, sdkModulePath)
		if agreed == "" {
			agreed = requirement
			continue
		}
		if requirement != agreed {
			t.Fatalf("%s requires %s %s, but another product module requires %s; "+
				"product modules are released against one SDK version",
				path, sdkModulePath, requirement, agreed)
		}
	}
	if agreed == "" {
		t.Fatal("no product module requires the SDK, so there is no version to check")
	}
	return agreed
}

func TestEveryProductModuleRequiresAResolvableSDKVersion(t *testing.T) {
	// Requiring the version this release actually published is what makes a
	// module an ordinary consumer of the SDK, and therefore what makes it
	// something a product team can own and release. A module requiring a version
	// nothing published builds only where a workspace resolves it, which is the
	// dependency on this repository the boundary exists to prevent.
	//
	// The one exception is the SDK release window, and it is not an exception to
	// the rule so much as the moment the rule is being satisfied: the release
	// gate compares its tag against this requirement, so the requirement names
	// the new version first and the tag makes it resolvable moments later. The
	// window is only open while go.work replaces exactly that version — see
	// TestTheWorkspaceReplacesNothingOutsideAnSDKReleaseWindow — so a
	// requirement on an unpublished version with no replacement behind it still
	// fails, and so does a replacement left behind after the tag.
	//
	// Every product module is checked rather than the reference module alone, so
	// a scaffolded module that drifted to some other version would be caught.
	required := requiredSDKVersion(t)
	if required == sdkPublishedVersion {
		if replacements := workspaceReplacements(t); len(replacements) > 0 {
			t.Errorf("the product modules require the published SDK %s, so go.work "+
				"needs no replacement, but it declares %s", sdkPublishedVersion, replacements[0])
		}
		return
	}

	// Not the published version, so this must be a release window: the
	// workspace has to be carrying the matching replacement.
	if !slices.ContainsFunc(workspaceReplacements(t), func(replacement workspaceReplacement) bool {
		return isSDKReleaseWindow(t, replacement)
	}) {
		t.Errorf("the product modules require %s %s, which is not the published SDK %s, "+
			"and go.work does not declare %s; a requirement on an unpublished version "+
			"resolves only inside this workspace",
			sdkModulePath, required, sdkPublishedVersion, sdkReleaseWindowDescription(t))
	}
}

const (
	// sdkModulePath is the published SDK every product module consumes.
	sdkModulePath = "github.com/wso2/wso2-cli/sdk"
	// sdkPublishedVersion is the newest SDK release. Bump it as part of
	// publishing one, together with dropping the window replacement from
	// go.work; the two are the same piece of work.
	sdkPublishedVersion = "v0.2.0"
)

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

// TestShellReaderIsAssignedOnlyInCmdWso2 pins an architectural rule that used
// to be true only by accident of who happened to write Shell literals: the
// confirmation gate in front of an irreversible os.RemoveAll (internal/app's
// mayPrompt, #112 fix round 1 finding F3) trusts any Shell.Reader that is not
// the process's own os.Stdin, on the theory that only a test ever wires it to
// anything else. That theory holds today because cmd/wso2/main.go is the only
// non-test assignment, but nothing enforced that before this test: a future
// command wiring Reader to a pipe would silently void the terminal refusal in
// front of that deletion, and every other test in this repository would still
// pass.
//
// The source is parsed rather than matched as text. A substring check for
// "Reader:" sees the composite-literal form only, so the plain assignment
// s.Reader = strings.NewReader("y\n") walks straight through it — which is
// the form a real command would most likely use — while any unrelated struct
// with a field named Reader anywhere in the repository fails it for nothing.
//
// The scan is narrowed to the files that can name the type at all: internal/
// app's own non-test sources and every non-test file importing that package.
// A file that can reference neither the Shell type nor a value of it cannot
// assign its Reader field, so nothing that could void the gate is outside the
// scanned set. Within that set both forms are reported — a Reader key in a
// composite literal and an assignment to a .Reader selector — without
// resolving the target's type, so an unrelated Reader field in one of those
// few files would be reported too. That direction is the safe one: it asks
// for a look at the change, rather than passing it silently.
func TestShellReaderIsAssignedOnlyInCmdWso2(t *testing.T) {
	root := repoRoot(t)
	allowed := filepath.Join("cmd", "wso2", "main.go")

	for _, assignment := range shellReaderAssignments(t, root, allowed) {
		t.Errorf("%s; Shell.Reader must be assigned only in %s, where it is set "+
			"to os.Stdin, so mayPrompt's terminal check keeps meaning what it says",
			assignment, allowed)
	}
}

// shellReaderAssignments reports every assignment to a Reader field below root,
// outside the one file allowed to make one, as "<path>:<line> <form>" with the
// path relative to root. The walk is a function of its root so a test can point
// it at a tree it built itself.
func shellReaderAssignments(t *testing.T, root, allowed string) []string {
	t.Helper()
	var assignments []string

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || (path != root && isCheckoutRoot(path)) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if relative == allowed {
			return nil
		}

		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, 0)
		if parseErr != nil {
			t.Fatalf("cannot parse %s: %v", relative, parseErr)
		}
		if !canNameShell(root, path, file) {
			return nil
		}

		report := func(position token.Pos, form string) {
			assignments = append(assignments,
				fmt.Sprintf("%s:%d %s", relative, fileSet.Position(position).Line, form))
		}

		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.AssignStmt:
				for _, target := range typed.Lhs {
					if selector, ok := target.(*ast.SelectorExpr); ok && selector.Sel.Name == "Reader" {
						report(selector.Sel.Pos(), "assigns a Reader field")
					}
				}
			case *ast.CompositeLit:
				for _, element := range typed.Elts {
					pair, ok := element.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					if key, ok := pair.Key.(*ast.Ident); ok && key.Name == "Reader" {
						report(key.Pos(), "sets a Reader field in a composite literal")
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s failed: %v", root, err)
	}
	return assignments
}

// isCheckoutRoot reports whether a directory is the root of a checkout of its
// own: a nested clone or a linked worktree, either of which holds a .git entry
// — a directory for a clone, a file for a worktree.
//
// These boundaries are about this checkout's source. A nested checkout holds
// another revision's copy of the same files, so judging it by the rules of the
// revision being tested reports violations that are not in this tree and cannot
// be fixed from it. The walks therefore stop at one, wherever a developer
// happens to have put it.
func isCheckoutRoot(directory string) bool {
	_, err := os.Lstat(filepath.Join(directory, ".git"))
	return err == nil
}

// TestTheSourceWalkStopsAtANestedCheckout pins the skip above. A nested clone
// or worktree inside the working tree is ordinary — a developer's own scratch
// checkout, a vendored one — and its copy of a file this repository's rules
// name would otherwise be read as if it belonged to this revision.
func TestTheSourceWalkStopsAtANestedCheckout(t *testing.T) {
	root := t.TempDir()
	// The same offending file three times: in a nested clone, in a nested
	// linked worktree, and in a plain directory of this tree. Only the last is
	// this checkout's own source, so only the last may be reported.
	writeShellReaderFixture(t, filepath.Join(root, "clone", "cmd", "wso2"))
	writeShellReaderFixture(t, filepath.Join(root, "worktree", "cmd", "wso2"))
	writeShellReaderFixture(t, filepath.Join(root, "internal", "app"))

	if err := os.MkdirAll(filepath.Join(root, "clone", ".git"), 0o755); err != nil {
		t.Fatalf("cannot create the nested clone's .git directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "worktree", ".git"),
		[]byte("gitdir: /elsewhere/.git/worktrees/worktree\n"), 0o644); err != nil {
		t.Fatalf("cannot create the nested worktree's .git file: %v", err)
	}

	assignments := shellReaderAssignments(t, root, filepath.Join("cmd", "wso2", "main.go"))

	want := []string{filepath.Join("internal", "app", "wire.go") + ":5 sets a Reader field in a composite literal"}
	if !slices.Equal(assignments, want) {
		t.Errorf("the walk reported %v; want %v — a nested checkout is not this checkout's source", assignments, want)
	}
}

// writeShellReaderFixture writes, in the given directory, a non-test Go file
// that names internal/app's Shell type and sets its Reader field: what the walk
// reports when it reads it.
func writeShellReaderFixture(t *testing.T, directory string) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("cannot create %s: %v", directory, err)
	}
	const source = `package fixture

import "github.com/wso2/wso2-cli/internal/app"

var shell = app.Shell{Reader: nil}
`
	if err := os.WriteFile(filepath.Join(directory, "wire.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("cannot write the fixture in %s: %v", directory, err)
	}
}

// canNameShell reports whether a parsed file could refer to internal/app's
// Shell type: it is one of that package's own sources, or it imports it.
func canNameShell(root, path string, file *ast.File) bool {
	const shellPackage = "github.com/wso2/wso2-cli/internal/app"

	if relativeDir, err := filepath.Rel(root, filepath.Dir(path)); err == nil {
		if filepath.ToSlash(relativeDir) == "internal/app" && file.Name.Name == "app" {
			return true
		}
	}
	for _, imported := range file.Imports {
		if importPath, err := strconv.Unquote(imported.Path.Value); err == nil && importPath == shellPackage {
			return true
		}
	}
	return false
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
// another of this repository's modules or into a nested checkout.
func goFiles(t *testing.T, directory string) []string {
	t.Helper()
	root := repoRoot(t)
	var paths []string
	err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != directory && (isModuleRoot(t, root, path) || isCheckoutRoot(path)) {
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

// protectedIdentityTerms are the uses of "identity" that survive the rename in
// ADR 0015: two OIDC terms, a product name, a provider value, and the redirect
// that exists precisely to name the word a user typed.
var protectedIdentityTerms = []string{
	"identity provider", "Identity Provider", "identity providers", "Identity Providers",
	"identity token", "Identity token", "identity tokens",
	"Identity Server", "identity-server",
	"wso2 identity %s is now wso2 account %s",
	// "wso2 identity ..." is the product namespace's own command line, which
	// ADR 0015 gave that word to. It is the one place the word is not the
	// account concept, and a module naming its own commands has to use it.
	"wso2 identity",
	// The namespace argument of add-product, for the identity product itself.
	"add-product <account> identity",
	// The product the identity namespace reaches, named in its own prose. The
	// rename turned it into "the account product", which names nothing.
	"identity product",
}

// TestNoUserVisibleStringCallsAnAccountAnIdentity holds the rename ADR 0015
// made. The shell's identity command and its concept became account, and
// `identity` became a product namespace, so a message still calling an account
// an identity is now ambiguous rather than merely dated: the same word names
// the thing a user logs in as and the product they reach.
//
// It scans prose — a literal carrying a space — rather than every literal,
// because the single-word ones are not prose at all: "identity" is the product
// namespace, contexts.identity_exists is a problem code a script matches on,
// json:"identities" is the pre-rename schema key the migration reads, and an
// import path names a directory. Renaming any of those would break something
// while fixing nothing a user reads.
//
// The first pass over this repository renamed every "wso2 identity <verb>" and
// missed the prose around them, which is the mistake this test exists to catch
// next time.
func TestNoUserVisibleStringCallsAnAccountAnIdentity(t *testing.T) {
	root := repoRoot(t)
	for _, module := range goModules(t) {
		for _, path := range goFiles(t, filepath.Join(root, module)) {
			relative, _ := filepath.Rel(root, path)
			// Generated protocol code is excluded: its descriptor bytes carry
			// ModuleIdentity and ShellIdentity, which name which module and
			// which shell are speaking and have nothing to do with an account.
			// Renaming them would change the wire contract.
			if strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, ".pb.go") ||
				strings.Contains(relative, filepath.Join("internal", "boundaries")) {
				continue
			}
			for _, literal := range stringLiterals(t, path) {
				if !strings.Contains(literal, " ") {
					continue
				}
				held := literal
				for _, term := range protectedIdentityTerms {
					held = strings.ReplaceAll(held, term, "")
				}
				if strings.Contains(strings.ToLower(held), "identit") {
					t.Errorf("%s: a user-visible string calls an account an identity: %q",
						relative, literal)
				}
			}
		}
	}
}

// stringLiterals reads every string literal in a Go file, which is what a user
// can be shown; comments are deliberately not read.
func stringLiterals(t *testing.T, path string) []string {
	t.Helper()
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		t.Fatalf("cannot parse %s: %v", path, err)
	}
	var literals []string
	ast.Inspect(parsed, func(node ast.Node) bool {
		if literal, ok := node.(*ast.BasicLit); ok && literal.Kind == token.STRING {
			if unquoted, err := strconv.Unquote(literal.Value); err == nil {
				literals = append(literals, unquoted)
			}
		}
		return true
	})
	return literals
}
