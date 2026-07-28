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
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// developmentFixtures are the packages that exist only to prove the
// architecture, keyed by the shell-relative directory holding each one.
//
// They are the parts of this repository a production shell deletes rather than
// hardens, so each one has to stay recognizable as a fixture and stay out of
// reach of anything a user runs.
var developmentFixtures = []string{
	"internal/auth/devtoken",
	"internal/statusservice",
	"internal/contexts/fixture",
	"internal/modules/fixture",
}

// fixtureDisclaimers are the phrases that mark a package as non-production. A
// fixture's own documentation is where a reader meets it first, so at least one
// of these has to appear there.
var fixtureDisclaimers = []string{"non-production", "test infrastructure", "architecture proof"}

func TestEveryDevelopmentFixtureNamesItselfNonProduction(t *testing.T) {
	root := repoRoot(t)

	for _, fixture := range developmentFixtures {
		documentation := packageDoc(t, filepath.Join(root, filepath.FromSlash(fixture)))
		if !containsAny(documentation, fixtureDisclaimers) {
			t.Errorf("the package documentation of %s does not name it a development fixture; "+
				"it must contain one of %v", fixture, fixtureDisclaimers)
		}
	}
}

func TestNoDevelopmentFixtureIsReachableFromTheShellBinary(t *testing.T) {
	// The shell binary is what a user runs. A fixture that reaches it is no
	// longer a fixture: it is a way to obtain development access from the
	// product, which is the failure this whole boundary exists to prevent.
	//
	// The issuer is the deliberate exception. Brokering access is shell work,
	// so the shell links the development issuer for as long as the proof is
	// what issues tokens. Everything else it serves — the local service, the
	// fixture writers — must be absent, and the issuer's own reach is bounded
	// by the reserved proof namespace rather than by the linker.
	linked := shellBinaryPackages(t)

	for _, fixture := range developmentFixtures {
		imported := "github.com/wso2/wso2-cli/" + fixture
		reachable := slices.Contains(linked, imported)
		switch {
		case fixture == "internal/auth/devtoken" && !reachable:
			t.Errorf("the shell binary no longer links %s; "+
				"the broker cannot issue proof access without it", imported)
		case fixture != "internal/auth/devtoken" && reachable:
			t.Errorf("the shell binary links the development fixture %s; "+
				"a fixture must not be reachable from anything a user runs", imported)
		}
	}
}

func TestNoDevelopmentFixtureIsReachableFromTheReferenceModule(t *testing.T) {
	// A module is where a fixture would do the most damage: it is the side of
	// the boundary that is not trusted, and it is the side a third party
	// writes. Nothing a module links may be able to mint its own access or
	// answer for its own audience.
	root := repoRoot(t)
	linked := listDeps(t, filepath.Join(root, "examples", "reference-module"), "./...")

	for _, dependency := range linked {
		if strings.HasPrefix(dependency, "github.com/wso2/wso2-cli/internal") {
			t.Errorf("the reference module links the shell internal package %q", dependency)
		}
	}
}

// issuerPublicNames are every package-level name the development issuer may
// declare, with what each one is.
//
// The issuer signs with the credential it is handed, so it holds no key of its
// own, and this list is how that stays true: a new package-level value in
// devtoken is exactly what a hardcoded key would look like, and adding one has
// to be a deliberate edit here rather than an unremarked commit.
var issuerPublicNames = map[string]string{
	"Prefix":       "the non-production marker every token opens with",
	"Lifetime":     "how long a fixture token is accepted",
	"ErrMalformed": "a refusal reason",
	"ErrSignature": "a refusal reason",
	"ErrExpired":   "a refusal reason",
	"encoding":     "the base64url encoding tokens are written in",
}

func TestTheDevelopmentIssuerHoldsNoFixedSecret(t *testing.T) {
	root := repoRoot(t)
	directory := filepath.Join(root, "internal", "auth", "devtoken")

	packages, err := parser.ParseDir(token.NewFileSet(), directory, nonTestFile, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("cannot parse the development issuer: %v", err)
	}
	for _, parsed := range packages {
		for path, file := range parsed.Files {
			relative, _ := filepath.Rel(root, path)
			for _, name := range packageLevelValues(file) {
				if _, allowed := issuerPublicNames[name]; !allowed {
					t.Errorf("%s declares the package-level value %q; the issuer signs with the "+
						"credential it is given and holds no value of its own. Add it to "+
						"issuerPublicNames only once it is certainly not key material.",
						relative, name)
				}
			}
		}
	}
}

// packageLevelValues reports the names every package-level constant and
// variable declaration in a file binds. Types and functions are not values and
// cannot hold key material.
func packageLevelValues(file *ast.File) []string {
	var names []string
	for _, declaration := range file.Decls {
		general, isGeneral := declaration.(*ast.GenDecl)
		if !isGeneral || (general.Tok != token.CONST && general.Tok != token.VAR) {
			continue
		}
		for _, specification := range general.Specs {
			value, isValue := specification.(*ast.ValueSpec)
			if !isValue {
				continue
			}
			for _, name := range value.Names {
				names = append(names, name.Name)
			}
		}
	}
	return names
}

// nonTestFile selects a package's own sources, so a test fixture's own test
// data is never mistaken for what the package ships.
func nonTestFile(entry os.FileInfo) bool {
	return !strings.HasSuffix(entry.Name(), "_test.go")
}

// packageDoc reads the package documentation comment of the package in a
// directory.
func packageDoc(t *testing.T, directory string) string {
	t.Helper()
	packages, err := parser.ParseDir(token.NewFileSet(), directory, nonTestFile, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("cannot parse %s: %v", directory, err)
	}
	var documentation strings.Builder
	for _, parsed := range packages {
		for _, file := range parsed.Files {
			documentation.WriteString(file.Doc.Text())
		}
	}
	if documentation.Len() == 0 {
		t.Fatalf("the package in %s has no package documentation", directory)
	}
	return documentation.String()
}

// shellBinaryPackages reports every package linked into the shell executable.
func shellBinaryPackages(t *testing.T) []string {
	t.Helper()
	return listDeps(t, repoRoot(t), "./cmd/wso2")
}

// listDeps reports the transitive package dependencies of a pattern, as the Go
// tool resolves them.
func listDeps(t *testing.T, directory, pattern string) []string {
	t.Helper()
	command := exec.Command("go", "list", "-deps", pattern)
	command.Dir = directory
	command.Env = os.Environ()
	output, err := command.Output()
	if err != nil {
		t.Fatalf("go list -deps %s in %s failed: %v", pattern, directory, err)
	}
	return strings.Fields(string(output))
}

func containsAny(text string, phrases []string) bool {
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}
