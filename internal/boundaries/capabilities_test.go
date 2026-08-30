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

package boundaries_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/modules"
)

// sdkModulePackage is the import path of the SDK package that defines Options.
// The local name a file binds it to is read from that file's imports, so an
// aliased import is still found.
const sdkModulePackage = "github.com/wso2/wso2-cli/sdk/module"

func TestEveryModuleServesTheCapabilitiesItsDeclarationPublishes(t *testing.T) {
	// A module states its access requests twice: in module.json, which is what
	// installation copies into the local receipt, and in the module.Options its
	// executable serves. The authentication broker refuses a request the
	// receipt did not authorize, so an audience present in one place and absent
	// from the other builds clean, tests clean, releases clean, and then fails
	// the first user who runs the command. Nothing else in the build compares
	// the two, which is why they are compared here.
	root := repoRoot(t)
	declarations := declaredCapabilities(t, root)

	for _, module := range productModules(t) {
		t.Run(module, func(t *testing.T) {
			declared, found := declarations[module]
			if !found {
				t.Fatalf("%s declares no %s; a module the shell can install has to publish its capabilities",
					module, catalog.DeclarationFileName)
			}
			served := servedCapabilities(t, filepath.Join(root, module))

			compareCapabilities(t, module, "authAudiences", declared.AuthAudiences, served.audiences)
			compareCapabilities(t, module, "authScopes", declared.AuthScopes, served.scopes)
		})
	}
}

// compareCapabilities reports a disagreement in either direction, naming both
// sides so the developer can see which one to change rather than being told
// only that they differ.
func compareCapabilities(t *testing.T, module, field string, declared, served []string) {
	t.Helper()
	if slices.Equal(sortedSet(declared), sortedSet(served)) {
		return
	}
	t.Errorf("%s declares %s %v in %s but its executable serves module.Options{%s: %v}; "+
		"make the two agree, because the broker refuses a request the receipt written from %s did not authorize",
		module, field, sortedSet(declared), catalog.DeclarationFileName,
		optionsField(field), sortedSet(served), catalog.DeclarationFileName)
}

// optionsField names the module.Options field a declaration field corresponds
// to, so a failure points at the Go identifier a developer has to edit.
func optionsField(declarationField string) string {
	return strings.ToUpper(declarationField[:1]) + declarationField[1:]
}

func sortedSet(values []string) []string {
	unique := append([]string(nil), values...)
	sort.Strings(unique)
	return slices.Compact(unique)
}

// declaredCapabilities reads what every module declaration publishes, keyed by
// the module's directory relative to the repository root.
//
// Discovery goes through the catalog rather than a second reader, because the
// catalog is what the release actually publishes from: a declaration this test
// read differently from the release would prove nothing about what gets
// installed.
func declaredCapabilities(t *testing.T, root string) map[string]modules.Capabilities {
	t.Helper()
	found, err := catalog.Discover(root)
	if err != nil {
		t.Fatalf("cannot discover the module declarations: %v", err)
	}
	byDirectory := make(map[string]modules.Capabilities, len(found))
	for _, declaration := range found {
		byDirectory[declaration.Directory] = declaration.Capabilities
	}
	return byDirectory
}

// servedOptions is what a module's source hands to the SDK.
type servedOptions struct {
	audiences []string
	scopes    []string
}

// servedCapabilities reports the access requests a module's executable serves,
// read from every module.Options composite literal in the module's own source.
//
// The source is the seam because the module contract does not carry these
// values: the hello a module sends announces its namespace, its version, and
// the protocol versions it speaks, and nothing about the audiences it may
// request. Building the module and driving the handshake would therefore prove
// nothing here. The reference module happens to expose a test-only
// --module-info switch that prints the whole descriptor, but that switch is the
// reference module's own invention and the scaffold does not generate it, so a
// check resting on it would silently stop covering the modules it matters most
// for. Reading the literal covers a module the moment it exists.
//
// The cost is that a value assembled at run time cannot be read, so this
// insists on string literals and package-level constants and says so when it
// meets anything else.
func servedCapabilities(t *testing.T, moduleDirectory string) servedOptions {
	t.Helper()

	var sources []string
	for _, path := range goFiles(t, moduleDirectory) {
		if !strings.HasSuffix(path, "_test.go") {
			sources = append(sources, path)
		}
	}

	fileSet := token.NewFileSet()
	files := make(map[string]*ast.File, len(sources))
	// Package-scope names are collected per directory, because a constant is
	// visible to every file of its package and modules conventionally declare
	// their audiences as named constants beside the handler that requests them.
	constants := make(map[string]map[string]ast.Expr)
	for _, path := range sources {
		file, err := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("cannot parse %s: %v", path, err)
		}
		files[path] = file
		directory := filepath.Dir(path)
		if constants[directory] == nil {
			constants[directory] = make(map[string]ast.Expr)
		}
		collectPackageValues(file, constants[directory])
	}

	var served servedOptions
	// Where each literal was found, so more than one can be named rather than
	// merged. Merging is the failure worth preventing: a second literal that
	// the executable never serves would top up the union until it matched the
	// declaration, and the check would pass while the running module declared
	// less than module.json publishes and the receipt authorizes.
	var sites []string
	for _, path := range sources {
		file := files[path]
		local, imported := sdkModuleName(file)
		if !imported {
			continue
		}
		scope := constants[filepath.Dir(path)]
		// Failures name the file the way the rest of these boundaries do, from
		// the repository root, so the message can be pasted into an editor.
		relative, err := filepath.Rel(repoRoot(t), path)
		if err != nil {
			relative = path
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, isComposite := node.(*ast.CompositeLit)
			if !isComposite || !isOptionsType(literal.Type, local) {
				return true
			}
			sites = append(sites, fmt.Sprintf("%s:%d", relative, fileSet.Position(literal.Pos()).Line))
			served.audiences = append(served.audiences,
				stringsFrom(t, relative, "AuthAudiences", literal, scope)...)
			served.scopes = append(served.scopes,
				stringsFrom(t, relative, "AuthScopes", literal, scope)...)
			return true
		})
	}
	if len(sites) == 0 {
		t.Fatalf("no module.Options literal was found in %s; the check that its declared capabilities match "+
			"what it serves cannot run, so construct the options where they can be read",
			relativeTo(t, moduleDirectory))
	}
	if len(sites) > 1 {
		t.Fatalf("%s builds module.Options in more than one place (%s); which one the executable serves "+
			"cannot be told apart here, so declare the capabilities once and pass that value to module.Serve",
			relativeTo(t, moduleDirectory), strings.Join(sites, ", "))
	}
	return served
}

// sdkModuleName reports the name a file binds the SDK's module package to.
func sdkModuleName(file *ast.File) (string, bool) {
	for _, imported := range file.Imports {
		path, err := strconv.Unquote(imported.Path.Value)
		if err != nil || path != sdkModulePackage {
			continue
		}
		if imported.Name != nil {
			return imported.Name.Name, true
		}
		return "module", true
	}
	return "", false
}

// isOptionsType reports whether a composite literal builds module.Options, in
// either its value or its pointer form.
func isOptionsType(expression ast.Expr, local string) bool {
	if star, isPointer := expression.(*ast.StarExpr); isPointer {
		expression = star.X
	}
	// A dot import puts Options in the file's own scope, so the literal is a
	// bare identifier with no qualifier to match. Without this the literal is
	// not recognised, no site is found, and the module is reported as
	// constructing its options somewhere unreadable, which sends the developer
	// looking for a problem they do not have.
	if local == "." {
		identifier, isIdentifier := expression.(*ast.Ident)
		return isIdentifier && identifier.Name == "Options"
	}
	selector, isSelector := expression.(*ast.SelectorExpr)
	if !isSelector || selector.Sel.Name != "Options" {
		return false
	}
	qualifier, isIdentifier := selector.X.(*ast.Ident)
	return isIdentifier && qualifier.Name == local
}

// collectPackageValues records the package-level constants and variables of a
// file, so a field set to a named audience can be resolved back to its string.
func collectPackageValues(file *ast.File, into map[string]ast.Expr) {
	for _, declaration := range file.Decls {
		generic, isGeneric := declaration.(*ast.GenDecl)
		if !isGeneric || (generic.Tok != token.CONST && generic.Tok != token.VAR) {
			continue
		}
		for _, specification := range generic.Specs {
			value, isValue := specification.(*ast.ValueSpec)
			if !isValue {
				continue
			}
			for index, name := range value.Names {
				if index < len(value.Values) {
					into[name.Name] = value.Values[index]
				}
			}
		}
	}
}

// stringsFrom reads one module.Options field as the list of strings it serves.
// A field the literal does not set serves nothing, which is exactly what an
// empty declaration has to agree with.
func stringsFrom(t *testing.T, path, field string, literal *ast.CompositeLit, scope map[string]ast.Expr) []string {
	t.Helper()
	for _, element := range literal.Elts {
		keyed, isKeyed := element.(*ast.KeyValueExpr)
		if !isKeyed {
			continue
		}
		key, isIdentifier := keyed.Key.(*ast.Ident)
		if !isIdentifier || key.Name != field {
			continue
		}
		return stringList(t, path, field, keyed.Value, scope)
	}
	return nil
}

// stringList evaluates an expression that has to be a list of strings, and
// fails the test rather than guessing when it is something this cannot read.
func stringList(t *testing.T, path, field string, expression ast.Expr, scope map[string]ast.Expr) []string {
	t.Helper()
	switch value := expression.(type) {
	case *ast.CompositeLit:
		entries := make([]string, 0, len(value.Elts))
		for _, element := range value.Elts {
			entries = append(entries, stringValue(t, path, field, element, scope))
		}
		return entries
	case *ast.Ident:
		if value.Name == "nil" {
			return nil
		}
		referenced, known := scope[value.Name]
		if !known {
			t.Fatalf("%s sets module.Options.%s to %s, which is not declared at package level; "+
				"declare the capability as a string literal or a package constant so it can be compared with %s",
				path, field, value.Name, catalog.DeclarationFileName)
		}
		return stringList(t, path, field, referenced, scope)
	default:
		t.Fatalf("%s sets module.Options.%s to a value that is assembled rather than written down; "+
			"declare the capability as a string literal or a package constant so it can be compared with %s",
			path, field, catalog.DeclarationFileName)
		return nil
	}
}

// stringValue evaluates a single element of a capability list.
func stringValue(t *testing.T, path, field string, expression ast.Expr, scope map[string]ast.Expr) string {
	t.Helper()
	switch value := expression.(type) {
	case *ast.BasicLit:
		if value.Kind == token.STRING {
			unquoted, err := strconv.Unquote(value.Value)
			if err != nil {
				t.Fatalf("%s has an unreadable string in module.Options.%s: %s", path, field, value.Value)
			}
			return unquoted
		}
	case *ast.Ident:
		referenced, known := scope[value.Name]
		if !known {
			t.Fatalf("%s names %s in module.Options.%s, but it is not declared at package level; "+
				"declare the capability as a string literal or a package constant so it can be compared with %s",
				path, value.Name, field, catalog.DeclarationFileName)
		}
		return stringValue(t, path, field, referenced, scope)
	}
	t.Fatalf("%s sets an entry of module.Options.%s to a value that is assembled rather than written down; "+
		"declare the capability as a string literal or a package constant so it can be compared with %s",
		path, field, catalog.DeclarationFileName)
	return ""
}

// relativeTo names a path from the repository root, which is how every other
// message in these boundaries names one.
func relativeTo(t *testing.T, path string) string {
	t.Helper()
	if shortened, err := filepath.Rel(repoRoot(t), path); err == nil {
		return shortened
	}
	return path
}
