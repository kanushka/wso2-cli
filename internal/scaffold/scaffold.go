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

	sdkVersion, err := sdkVersion(request.RepositoryRoot)
	if err != nil {
		return Result{}, err
	}

	directory := filepath.Join(request.RepositoryRoot, ModulesDirectory, request.Namespace)
	data := templateData{
		Namespace:        request.Namespace,
		Executable:       "wso2-module-" + request.Namespace,
		SDKVersion:       sdkVersion,
		ShellRange:       shellRange,
		ProtocolVersions: protocol.Supported(),
		GoVersion:        goVersion(request.RepositoryRoot),
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
	return nil
}

// sdkVersion reports the SDK version this checkout builds modules against,
// which is the version the reference module requires.
//
// It is read rather than declared because there is no second place to declare
// it that would not immediately be able to disagree with the module the whole
// repository already builds. The requirement is matched on the module path, so
// a require block, a comment, or an indirect marker cannot change the answer.
func sdkVersion(repositoryRoot string) (string, error) {
	const modulePath = "github.com/wso2/wso2-cli/sdk"

	path := filepath.Join(repositoryRoot, ModulesDirectory, ReservedNamespace, "go.mod")
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("scaffold: cannot read the SDK version this checkout uses: %w", err)
	}
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		for index, field := range fields {
			if field == modulePath && index+1 < len(fields) {
				return fields[index+1], nil
			}
		}
	}
	return "", fmt.Errorf("scaffold: %s requires no SDK version to generate against", path)
}

// goVersion reports the language version the checkout's own modules declare, so
// a generated module does not ask for a toolchain the repository does not.
func goVersion(repositoryRoot string) string {
	const fallback = "1.25.0"

	content, err := os.ReadFile(filepath.Join(repositoryRoot, ModulesDirectory, ReservedNamespace, "go.mod"))
	if err != nil {
		return fallback
	}
	for _, line := range strings.Split(string(content), "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && fields[0] == "go" {
			return fields[1]
		}
	}
	return fallback
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

	entry := "\t./" + ModulesDirectory + "/" + namespace + "\n"
	if strings.Contains(string(content), strings.TrimSpace(entry)) {
		return nil
	}
	// The entry is placed beside the module it is closest to, which keeps the
	// use block in the order a reader expects rather than appending to the end
	// of the file where it would not be part of the block at all.
	anchor := "\t./" + ModulesDirectory + "/" + ReservedNamespace + "\n"
	updated := strings.Replace(string(content), anchor, anchor+entry, 1)
	if updated == string(content) {
		return fmt.Errorf("scaffold: %s does not compose %s/%s, so a generated module cannot join it",
			path, ModulesDirectory, ReservedNamespace)
	}
	return os.WriteFile(path, []byte(updated), 0o644)
}

// RemoveFromWorkspace takes a module's workspace entry back out. It exists for
// the tests that generate a module into this checkout and have to leave the
// repository as they found it.
func RemoveFromWorkspace(repositoryRoot, namespace string) error {
	path := filepath.Join(repositoryRoot, "go.work")
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	entry := "\t./" + ModulesDirectory + "/" + namespace + "\n"
	updated := strings.Replace(string(content), entry, "", 1)
	if updated == string(content) {
		return nil
	}
	return os.WriteFile(path, []byte(updated), 0o644)
}

// templateData is what every generated file is rendered from.
type templateData struct {
	Namespace        string
	Executable       string
	SDKVersion       string
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
