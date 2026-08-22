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

// What a scaffolded module has to be is not "the bytes we last generated": it
// is a module that builds and answers through the module contract against the
// SDK as it is today. A golden file would prove the template unchanged while
// the SDK moved out from under it, so the central test here generates a module,
// builds it, and runs the test the generation itself produced.
//
// The refusals are asserted at the same entry point rather than against a
// validator of their own, because a validator proven in isolation can still be
// called wrongly by the thing that generates.
//
// Every generation happens in a temporary repository rather than in this
// checkout. Generating here would write into modules/ and edit go.work while
// the rest of the suite is building in parallel, so a package compiling at the
// wrong moment would see a workspace entry for a directory being deleted. The
// temporary repository carries what a generation reads — a workspace and the
// reference module's requirements — and composes the SDK from this checkout the
// way a published release will be resolved from the proxy.
package scaffold_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/scaffold"
	"github.com/wso2/wso2-cli/sdk/protocol"
)

// TestAScaffoldedModuleBuildsAndAnswersTheContract is the test that makes the
// quick-start guide trustworthy: what the guide tells a developer to run
// produces something that works, with no editing in between.
func TestAScaffoldedModuleBuildsAndAnswersTheContract(t *testing.T) {
	root := temporaryRepository(t)
	namespace := "scaffoldproof"
	directory := filepath.Join(root, "modules", namespace)

	generated, err := scaffold.Generate(scaffold.Request{RepositoryRoot: root, Namespace: namespace})
	if err != nil {
		t.Fatalf("generating returned %v", err)
	}
	if generated.Directory != directory {
		t.Fatalf("generated into %s, want %s", generated.Directory, directory)
	}
	for _, file := range generated.Files {
		if _, err := os.Stat(file); err != nil {
			t.Fatalf("the generation reports %s, which is not there: %v", file, err)
		}
	}

	// Built and tested where it was generated, so what is proven is the module
	// a developer would have in front of them.
	runGo(t, directory, "build", "./...")
	runGo(t, directory, "test", "./...")
}

// TestAScaffoldedModuleIsGeneratedAgainstWhatTheCheckoutDeclares states the
// property a literal in a template cannot have.
//
// The versions in the temporary repository are moved to ones no template would
// contain before anything is generated. Comparing a generated module against
// the file it was derived from would pass for a template literal that happened
// to match today and would keep passing after the checkout moved; moving the
// declaration first is what makes the assertion about derivation rather than
// about coincidence.
func TestAScaffoldedModuleIsGeneratedAgainstWhatTheCheckoutDeclares(t *testing.T) {
	root := temporaryRepository(t)
	namespace := "scaffoldversion"
	directory := filepath.Join(root, "modules", namespace)

	const distinctSDK = "v0.42.7"
	const distinctCobra = "v1.8.1"
	referencePath := filepath.Join(root, "modules", "reference", "go.mod")
	declared := readFile(t, referencePath)
	declared = strings.Replace(declared,
		"github.com/wso2/wso2-cli/sdk "+sdkRequirementIn(t, declared),
		"github.com/wso2/wso2-cli/sdk "+distinctSDK, 1)
	declared = requireCobraAt(t, declared, distinctCobra)
	writeFile(t, referencePath, declared)

	if _, err := scaffold.Generate(scaffold.Request{RepositoryRoot: root, Namespace: namespace}); err != nil {
		t.Fatalf("generating returned %v", err)
	}

	generated := readFile(t, filepath.Join(directory, "go.mod"))
	if got := sdkRequirementIn(t, generated); got != distinctSDK {
		t.Errorf("the generated module requires the SDK at %q; the checkout declares %q", got, distinctSDK)
	}
	if !strings.Contains(generated, "github.com/spf13/cobra "+distinctCobra) {
		t.Errorf("the generated module does not take Cobra at the checkout's version %q:\n%s",
			distinctCobra, generated)
	}

	// The declared protocol versions are compared with what the SDK in this
	// checkout actually speaks, rather than merely being present: an empty or
	// stale array would otherwise satisfy the criterion.
	wantProtocols, err := json.Marshal(protocol.Supported())
	if err != nil {
		t.Fatalf("cannot encode the supported protocol versions: %v", err)
	}
	declaration := readFile(t, filepath.Join(directory, "module.json"))
	var parsed struct {
		Compatibility struct {
			ProtocolVersions []int `json:"protocolVersions"`
		} `json:"compatibility"`
	}
	if err := json.Unmarshal([]byte(declaration), &parsed); err != nil {
		t.Fatalf("the generated declaration is not readable JSON: %v\n%s", err, declaration)
	}
	gotProtocols, err := json.Marshal(parsed.Compatibility.ProtocolVersions)
	if err != nil {
		t.Fatalf("cannot encode the declared protocol versions: %v", err)
	}
	if string(gotProtocols) != string(wantProtocols) {
		t.Errorf("the generated declaration names protocol versions %s; this SDK speaks %s",
			gotProtocols, wantProtocols)
	}
	if len(parsed.Compatibility.ProtocolVersions) == 0 {
		t.Error("the generated declaration names no protocol versions")
	}
}

// requireCobraAt rewrites a go.mod's Cobra requirement, adding one when the
// fixture does not already carry it.
func requireCobraAt(t *testing.T, goMod, version string) string {
	t.Helper()
	const modulePath = "github.com/spf13/cobra"

	for _, line := range strings.Split(goMod, "\n") {
		fields := strings.Fields(line)
		for index, field := range fields {
			if field == modulePath && index+1 < len(fields) {
				return strings.Replace(goMod, line,
					strings.Replace(line, fields[index+1], version, 1), 1)
			}
		}
	}
	return goMod + "\nrequire " + modulePath + " " + version + "\n"
}

// TestAScaffoldedModuleImportsNoShellPackage keeps the generated module a
// module. A template that reached into the shell would produce something that
// cannot be released or moved, and the developer would inherit that by default.
func TestAScaffoldedModuleImportsNoShellPackage(t *testing.T) {
	root := temporaryRepository(t)
	namespace := "scaffoldimports"
	directory := filepath.Join(root, "modules", namespace)

	if _, err := scaffold.Generate(scaffold.Request{RepositoryRoot: root, Namespace: namespace}); err != nil {
		t.Fatalf("generating returned %v", err)
	}

	err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		content := readFile(t, path)
		if strings.Contains(content, "github.com/wso2/wso2-cli/internal") {
			t.Errorf("%s imports a shell internal package", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the generated module returned %v", err)
	}
}

// TestGenerationIsRefusedAndWritesNothing covers every namespace a module may
// not own. Each is asserted at the generation entry point, and each has to
// leave nothing behind: a refusal that had already written half a module would
// be worse than no refusal at all.
func TestGenerationIsRefusedAndWritesNothing(t *testing.T) {
	root := temporaryRepository(t)

	refusals := []struct {
		name      string
		namespace string
		reason    string
	}{
		// The load-bearing one. The shell resolves its own commands first, so
		// a module here would build, release, install, and never run.
		{name: "a shell command name", namespace: app.CommandNames()[0], reason: "shell command"},
		{name: "the reserved reference namespace", namespace: "reference", reason: "reserved"},
		{name: "an uppercase name", namespace: "MyCloud", reason: "lowercase"},
		{name: "a name with a hyphen", namespace: "my-cloud", reason: "lowercase"},
		{name: "a name starting with a digit", namespace: "1cloud", reason: "lowercase"},
		// Longer than the shell's own namespace bound. Generating it would
		// write a module the catalog later refuses, so the developer would
		// find out at release time instead of here.
		{name: "a name past the length the shell allows", namespace: strings.Repeat("c", 33), reason: "lowercase"},
		{name: "an empty name", namespace: "", reason: "lowercase"},
	}

	for _, refusal := range refusals {
		t.Run(refusal.name, func(t *testing.T) {
			_, err := scaffold.Generate(scaffold.Request{
				RepositoryRoot: root,
				Namespace:      refusal.namespace,
			})
			if err == nil {
				t.Fatalf("generating %q succeeded", refusal.namespace)
			}
			// The refusal has to say which rule was broken. "invalid" tells a
			// developer nothing they can act on.
			if !strings.Contains(strings.ToLower(err.Error()), refusal.reason) {
				t.Errorf("the refusal does not name %q: %v", refusal.reason, err)
			}
			if refusal.namespace != "" && refusal.namespace != "reference" {
				if _, statErr := os.Stat(filepath.Join(root, "modules", refusal.namespace)); !os.IsNotExist(statErr) {
					t.Errorf("a refused generation left a directory behind: %v", statErr)
				}
			}
		})
	}
}

// TestANamespaceAnotherModuleDeclaresIsRefused covers the collision a directory
// listing cannot see. A namespace is declared by a module rather than by the
// directory it sits in, so two modules can collide on a namespace while their
// directories do not, and the refusal has to name the module that already owns
// it rather than a path that does not exist.
func TestANamespaceAnotherModuleDeclaresIsRefused(t *testing.T) {
	root := temporaryRepository(t)
	const namespace = "elsewhere"

	// Declared from a directory of another name, which is exactly the case a
	// check on the target directory would miss.
	declaring := filepath.Join(root, "modules", "under-another-name")
	if err := os.MkdirAll(declaring, 0o755); err != nil {
		t.Fatalf("cannot create the declaring module: %v", err)
	}
	writeFile(t, filepath.Join(declaring, "module.json"),
		`{"schemaVersion": 1, "namespace": "`+namespace+`"}`+"\n")

	_, err := scaffold.Generate(scaffold.Request{RepositoryRoot: root, Namespace: namespace})
	if err == nil {
		t.Fatalf("generating the already declared namespace %q succeeded", namespace)
	}
	if !strings.Contains(err.Error(), "already") {
		t.Errorf("the refusal does not say the namespace is taken: %v", err)
	}
	// Naming where it is taken is the difference between a refusal a developer
	// can act on and one that just says no.
	if !strings.Contains(err.Error(), "under-another-name") {
		t.Errorf("the refusal does not name the module that declares it: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "modules", namespace)); !os.IsNotExist(statErr) {
		t.Errorf("the refused generation left a directory behind: %v", statErr)
	}
}

// TestAGenerationIntoARepositoryWithNoWorkspaceIsRefusedBeforeWriting keeps the
// last failure path from being the one that leaves half a module behind: a
// workspace that cannot be joined is decided before anything is created.
func TestAGenerationIntoARepositoryWithNoWorkspaceIsRefusedBeforeWriting(t *testing.T) {
	root := temporaryRepository(t)
	if err := os.Remove(filepath.Join(root, "go.work")); err != nil {
		t.Fatalf("cannot remove the workspace: %v", err)
	}

	_, err := scaffold.Generate(scaffold.Request{RepositoryRoot: root, Namespace: "noworkspace"})
	if err == nil {
		t.Fatal("generating into a repository with no workspace succeeded")
	}
	if _, statErr := os.Stat(filepath.Join(root, "modules", "noworkspace")); !os.IsNotExist(statErr) {
		t.Errorf("the refused generation left a directory behind: %v", statErr)
	}
}

// TestAModuleWhoseNameIsAPrefixOfAnotherJoinsTheWorkspace pins the difference
// between a whole-line match and a substring one. "cloud" is a substring of
// "cloudops", and a generation that took that for an existing entry would report
// success while leaving the module out of the workspace — where it would then
// resolve the SDK from the proxy rather than from this checkout.
func TestAModuleWhoseNameIsAPrefixOfAnotherJoinsTheWorkspace(t *testing.T) {
	root := temporaryRepository(t)

	if _, err := scaffold.Generate(scaffold.Request{RepositoryRoot: root, Namespace: "cloudops"}); err != nil {
		t.Fatalf("generating cloudops returned %v", err)
	}
	if _, err := scaffold.Generate(scaffold.Request{RepositoryRoot: root, Namespace: "cloud"}); err != nil {
		t.Fatalf("generating cloud returned %v", err)
	}

	workspace := readFile(t, filepath.Join(root, "go.work"))
	for _, entry := range []string{"./modules/cloudops", "./modules/cloud"} {
		found := false
		for _, line := range strings.Split(workspace, "\n") {
			if strings.TrimSpace(line) == entry {
				found = true
			}
		}
		if !found {
			t.Errorf("%s is not composed by the workspace:\n%s", entry, workspace)
		}
	}
}

// TestGeneratingOverAnExistingModuleIsRefused keeps a second run from
// overwriting a developer's work. The namespace already declared here is one a
// generation created, so this is the mistake a developer makes by repeating the
// command rather than one they make by choosing badly.
func TestGeneratingOverAnExistingModuleIsRefused(t *testing.T) {
	root := temporaryRepository(t)
	namespace := "scaffoldtwice"
	directory := filepath.Join(root, "modules", namespace)

	if _, err := scaffold.Generate(scaffold.Request{RepositoryRoot: root, Namespace: namespace}); err != nil {
		t.Fatalf("the first generation returned %v", err)
	}
	before := readFile(t, filepath.Join(directory, "module.json"))

	_, err := scaffold.Generate(scaffold.Request{RepositoryRoot: root, Namespace: namespace})
	if err == nil {
		t.Fatal("generating over an existing module succeeded")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "already") {
		t.Errorf("the refusal does not say the namespace is already taken: %v", err)
	}
	if after := readFile(t, filepath.Join(directory, "module.json")); after != before {
		t.Error("the refused second generation changed the existing module")
	}
}

// TestAScaffoldedModuleJoinsTheWorkspace covers the thing a developer would
// otherwise have to know: a module the workspace does not compose is one that
// resolves the SDK from the proxy instead of from the checkout, so a local SDK
// change would not reach it.
func TestAScaffoldedModuleJoinsTheWorkspace(t *testing.T) {
	root := temporaryRepository(t)
	namespace := "scaffoldworkspace"

	if _, err := scaffold.Generate(scaffold.Request{RepositoryRoot: root, Namespace: namespace}); err != nil {
		t.Fatalf("generating returned %v", err)
	}

	workspace := readFile(t, filepath.Join(root, "go.work"))
	if !strings.Contains(workspace, "./modules/"+namespace) {
		t.Errorf("the generated module is not composed by the workspace:\n%s", workspace)
	}
}

// checkoutRoot finds the checkout this test runs in, which is where the SDK a
// generated module is built against lives.
func checkoutRoot(t *testing.T) string {
	t.Helper()
	output, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("cannot find the repository root: %v", err)
	}
	return strings.TrimSpace(string(output))
}

// temporaryRepository builds the smallest repository a generation can run in: a
// workspace, and the reference module's go.mod, which is where the SDK version
// and the language version are read from.
//
// The workspace resolves the SDK from this checkout, which is what makes a
// generated module buildable before any SDK version is published. That
// replacement is the temporary repository's, not this one's.
func temporaryRepository(t *testing.T) string {
	t.Helper()
	checkout := checkoutRoot(t)
	root := t.TempDir()

	reference := filepath.Join(root, "modules", "reference")
	if err := os.MkdirAll(reference, 0o755); err != nil {
		t.Fatalf("cannot create the reference module directory: %v", err)
	}
	referenceGoMod := readFile(t, filepath.Join(checkout, "modules", "reference", "go.mod"))
	writeFile(t, filepath.Join(reference, "go.mod"), referenceGoMod)

	// The reference module has to be a module the workspace can compose, so it
	// needs at least one package. Nothing here builds it; it exists so the
	// workspace is valid.
	writeFile(t, filepath.Join(reference, "doc.go"), "// Package reference is a fixture.\npackage reference\n")

	workspace := "go " + goDirective(t, referenceGoMod) + "\n\nuse (\n\t./modules/reference\n)\n\n" +
		"replace github.com/wso2/wso2-cli/sdk " + sdkRequirementIn(t, referenceGoMod) +
		" => " + filepath.Join(checkout, "sdk") + "\n"
	writeFile(t, filepath.Join(root, "go.work"), workspace)
	return root
}

// goDirective reports the language version a go.mod declares.
func goDirective(t *testing.T, goMod string) string {
	t.Helper()
	for _, line := range strings.Split(goMod, "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && fields[0] == "go" {
			return fields[1]
		}
	}
	t.Fatal("the reference module declares no language version")
	return ""
}

// sdkRequirementIn reports the SDK version a go.mod's text requires.
func sdkRequirementIn(t *testing.T, goMod string) string {
	t.Helper()
	for _, line := range strings.Split(goMod, "\n") {
		fields := strings.Fields(line)
		for index, field := range fields {
			if field == "github.com/wso2/wso2-cli/sdk" && index+1 < len(fields) {
				return fields[index+1]
			}
		}
	}
	t.Fatal("the reference module requires no SDK version")
	return ""
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("cannot write %s: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}
	return string(content)
}

// runGo runs one go command in a directory and fails the test with its output.
func runGo(t *testing.T, directory string, args ...string) {
	t.Helper()
	command := exec.Command("go", args...)
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("go %s in %s failed: %v\n%s", strings.Join(args, " "), directory, err, output)
	}
}
