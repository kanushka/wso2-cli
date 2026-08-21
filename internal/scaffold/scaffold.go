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

// Package scaffold creates a new product module in this repository.
//
// A developer's first hour should be spent on their product, not on
// reconstructing the shape of a module from the reference module's source. What
// this package writes builds and passes its own test with no editing, so there
// is a known-good baseline before anything is changed.
//
// Two facts are read from the checkout rather than written into a template: the
// SDK version a module depends on, and the protocol versions it declares. A
// literal would be correct until the next release and would then produce a
// release-gate refusal the developer did not cause.
package scaffold

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"text/template"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/sdk/protocol"
)

// ReservedNamespace is the namespace the reference module owns. It proves the
// shell, the SDK, and the module contract, and a product module claiming it
// would displace the thing every other module is checked against.
const ReservedNamespace = "reference"

// ModulesDirectory is where a product module lives, one directory per
// namespace. Catalog discovery already scans it, so a module generated here is
// catalog-eligible without anything else being told about it.
const ModulesDirectory = "modules"

// namespacePattern is what a namespace a user has to type may look like: lower
// case, starting with a letter, letters and digits only. It is deliberately
// narrower than a directory name, because a namespace is the first word of a
// command.
var namespacePattern = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// shellRange is the shell versions a generated module declares support for.
//
// Unlike the SDK version and the protocol versions, this is a policy statement
// rather than a fact about the checkout: it says which shells the module's
// author intends to support, and the author is the one who narrows it.
const shellRange = ">=0.1.0 <2.0.0"

// Request is one generation.
type Request struct {
	// RepositoryRoot is the checkout to generate into.
	RepositoryRoot string
	// Namespace is the product namespace the new module will own. It is also
	// its directory name and the suffix of its executable.
	Namespace string
}

// Result reports what a generation wrote.
type Result struct {
	// Directory is the module's root.
	Directory string
	// Files are every file written, in the order they were written.
	Files []string
}

// Generate creates a new product module.
//
// Nothing is written until every refusal has been decided, so a refused
// generation leaves the checkout exactly as it was. A developer who picked a
// namespace badly should not have to clean up after finding out.
func Generate(request Request) (Result, error) {
	if err := checkNamespace(request); err != nil {
		return Result{}, err
	}

	declared, err := readReference(request.RepositoryRoot)
	if err != nil {
		return Result{}, err
	}

	directory := filepath.Join(request.RepositoryRoot, ModulesDirectory, request.Namespace)
	data := templateData{
		Namespace:        request.Namespace,
		Executable:       "wso2-module-" + request.Namespace,
		SDKVersion:       declared.Requires["github.com/wso2/wso2-cli/sdk"],
		CobraVersion:     declared.Requires["github.com/spf13/cobra"],
		ShellRange:       shellRange,
		ProtocolVersions: protocol.Supported(),
		GoVersion:        declared.GoVersion,
	}

	result := Result{Directory: directory}
	for _, file := range files {
		path := filepath.Join(directory, filepath.FromSlash(file.path(data)))
		if err := writeGenerated(path, file.template, data); err != nil {
			// A partial module is worse than none: it would build or not build
			// for reasons that have nothing to do with what the developer does
			// next. Take the whole directory back out.
			_ = os.RemoveAll(directory)
			return Result{}, err
		}
		result.Files = append(result.Files, path)
	}

	if err := addToWorkspace(request.RepositoryRoot, request.Namespace); err != nil {
		_ = os.RemoveAll(directory)
		return Result{}, err
	}
	return result, nil
}

// checkNamespace decides every reason a namespace may not be created. Each
// refusal names the rule rather than reporting the namespace invalid, because
// "invalid" is not something a developer can act on.
func checkNamespace(request Request) error {
	if !namespacePattern.MatchString(request.Namespace) {
		return fmt.Errorf(
			"%q cannot be a product namespace: a namespace is lowercase letters and digits, starting with a letter",
			request.Namespace)
	}
	if request.Namespace == ReservedNamespace {
		return fmt.Errorf("%q is the reserved namespace of the reference module", request.Namespace)
	}
	// The load-bearing refusal. The shell resolves its own commands before it
	// consults an installed module, so a module here would build, release,
	// install, and then never run: every invocation would reach the shell
	// command instead.
	if commands := app.CommandNames(); slices.Contains(commands, request.Namespace) {
		return fmt.Errorf(
			"%q is a shell command, so a module owning that namespace could never be reached; the shell owns %s",
			request.Namespace, strings.Join(commands, ", "))
	}

	declarations, err := catalog.Discover(request.RepositoryRoot)
	if err != nil {
		return err
	}
	for _, declaration := range declarations {
		if declaration.Namespace == request.Namespace {
			return fmt.Errorf("the %q namespace is already declared by %s",
				request.Namespace, declaration.Directory)
		}
	}
	directory := filepath.Join(request.RepositoryRoot, ModulesDirectory, request.Namespace)
	if _, err := os.Stat(directory); err == nil {
		return fmt.Errorf("%s already exists", directory)
	}
	// Asked here rather than after the files are written, so that every reason a
	// generation can fail is decided before anything is created. A workspace
	// that cannot be joined would otherwise be found out at the end, and the
	// cleanup for it would have to undo a module that already looked complete.
	return checkWorkspaceCanBeJoined(request.RepositoryRoot)
}

// checkWorkspaceCanBeJoined reports whether the workspace has the line a new
// module's entry is placed after.
func checkWorkspaceCanBeJoined(repositoryRoot string) error {
	path := filepath.Join(repositoryRoot, "go.work")
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("scaffold: cannot read the workspace: %w", err)
	}
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == workspaceAnchor {
			return nil
		}
	}
	return fmt.Errorf("scaffold: %s does not compose %s on a line of its own, so a generated module cannot join it",
		path, workspaceAnchor)
}

// reference describes what the reference module declares: the language version,
// and the version of each dependency a generated module shares with it.
type reference struct {
	GoVersion string
	Requires  map[string]string
}

// readReference reports what a generated module should be built against, read
// from the module this repository already builds.
//
// There is no second place to declare these that could not immediately disagree
// with the module the whole repository is checked against. The module graph is
// asked for them rather than the file scanned as text: a commented-out line, an
// exclude, or a versionless replace all mention a module path, and only the
// graph knows which one is the requirement.
func readReference(repositoryRoot string) (reference, error) {
	directory := filepath.Join(repositoryRoot, ModulesDirectory, ReservedNamespace)

	command := exec.Command("go", "mod", "edit", "-json")
	command.Dir = directory
	command.Env = os.Environ()
	output, err := command.Output()
	if err != nil {
		return reference{}, fmt.Errorf(
			"scaffold: cannot read what %s is built against: %w", directory, err)
	}

	var parsed struct {
		Go      string `json:"Go"`
		Require []struct {
			Path    string `json:"Path"`
			Version string `json:"Version"`
		} `json:"Require"`
	}
	if err := json.Unmarshal(output, &parsed); err != nil {
		return reference{}, fmt.Errorf("scaffold: cannot read the module graph of %s: %w", directory, err)
	}

	declared := reference{GoVersion: parsed.Go, Requires: map[string]string{}}
	for _, requirement := range parsed.Require {
		declared.Requires[requirement.Path] = requirement.Version
	}
	if declared.GoVersion == "" {
		return reference{}, fmt.Errorf("scaffold: %s declares no language version", directory)
	}
	for _, path := range sharedRequirements {
		if declared.Requires[path] == "" {
			return reference{}, fmt.Errorf("scaffold: %s does not require %s, so there is no version to generate against",
				directory, path)
		}
	}
	return declared, nil
}

// sharedRequirements are the dependencies a generated module takes at the same
// version the reference module takes them at.
//
// The SDK is here for the obvious reason. Cobra is here because a generated
// module declares its commands with it, and two modules in one workspace
// resolving different Cobra versions would build against a version neither was
// checked with.
var sharedRequirements = []string{
	"github.com/wso2/wso2-cli/sdk",
	"github.com/spf13/cobra",
}

// addToWorkspace composes the new module from source, so a local SDK change
// reaches it. A module the workspace does not compose resolves the SDK from the
// proxy instead, which is not what a developer changing both at once wants.
func addToWorkspace(repositoryRoot, namespace string) error {
	path := filepath.Join(repositoryRoot, "go.work")
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("scaffold: cannot read the workspace: %w", err)
	}

	// Matched as a whole line. A substring test would find "./modules/foo"
	// inside "./modules/foobar" and report a module composed that is not, and
	// the module would then resolve the SDK from the proxy instead of from this
	// checkout.
	entry := "./" + ModulesDirectory + "/" + namespace
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == entry {
			return nil
		}
	}
	// Placed beside the reference module, so the entry lands inside the use
	// block in the order a reader expects rather than after the end of the file
	// where it would not be part of the block at all. The anchor's own
	// indentation is reused, so nothing here assumes how the file is formatted.
	lines := strings.Split(string(content), "\n")
	for index, line := range lines {
		if strings.TrimSpace(line) != workspaceAnchor {
			continue
		}
		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		lines = slices.Insert(lines, index+1, indent+entry)
		return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
	}
	return fmt.Errorf("scaffold: %s does not compose %s on a line of its own, so a generated module cannot join it",
		path, workspaceAnchor)
}

// workspaceAnchor is the workspace entry a new module's entry is placed after.
// It is the reference module because that is the one product module every
// checkout has.
var workspaceAnchor = "./" + ModulesDirectory + "/" + ReservedNamespace

// templateData is what every generated file is rendered from.
type templateData struct {
	Namespace        string
	Executable       string
	SDKVersion       string
	CobraVersion     string
	ShellRange       string
	ProtocolVersions []int
	GoVersion        string
}

// ProtocolVersionsJSON renders the declared protocol versions as a JSON array,
// which is the shape module.json carries.
func (d templateData) ProtocolVersionsJSON() string {
	encoded, err := json.Marshal(d.ProtocolVersions)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

// Title is the namespace with an initial capital, for prose.
func (d templateData) Title() string {
	return strings.ToUpper(d.Namespace[:1]) + d.Namespace[1:]
}

// generatedFile is one file a generation writes. The path is a template too,
// because a module's executable directory carries its namespace.
type generatedFile struct {
	pathTemplate string
	template     string
}

func (f generatedFile) path(data templateData) string {
	rendered, err := template.New("path").Parse(f.pathTemplate)
	if err != nil {
		return f.pathTemplate
	}
	var builder strings.Builder
	if err := rendered.Execute(&builder, data); err != nil {
		return f.pathTemplate
	}
	return builder.String()
}

// writeGenerated renders one file, creating the directories it needs.
func writeGenerated(path, name string, data templateData) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("scaffold: cannot create %s: %w", filepath.Dir(path), err)
	}
	parsed, err := template.ParseFS(templateFS, "templates/"+name)
	if err != nil {
		return fmt.Errorf("scaffold: %s is not a usable template: %w", name, err)
	}
	// Rendered into memory first, so a template that fails halfway leaves no
	// half-written file for the caller's cleanup to have to reason about.
	var rendered strings.Builder
	if err := parsed.Execute(&rendered, data); err != nil {
		return fmt.Errorf("scaffold: cannot render %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(rendered.String()), 0o644); err != nil {
		return fmt.Errorf("scaffold: cannot write %s: %w", path, err)
	}
	return nil
}
