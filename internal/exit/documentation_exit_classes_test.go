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

package exit

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// commandsReference is the document that publishes the exit classes to the
// audience that will never read this package: a script author branching on the
// process status.
const commandsReference = "../../docs/reference/commands.md"

// documentedExitClass matches one row of the "## Exit classes" table, which
// opens with the status in backticks.
//
// The table is read as text rather than parsed as Markdown because the shell
// has no Markdown reader and must not gain one to test its own documentation.
// That is the same trade internal/contexts/documentation_examples_test.go
// makes against the credentialRef examples.
var documentedExitClass = regexp.MustCompile("^\\|\\s*`(\\d+)`\\s*\\|")

// TestTheDocumentedExitClassesAreTheOnesThisPackageDefines pins the published
// table to the constants, in both directions: every Code constant appears
// exactly once, and the table claims no status the package does not define.
//
// What it does NOT check is whether each row's prose is true. A row may name
// the wrong problem for its status and still pass here — which is exactly what
// went wrong once already, when the 64 row claimed an unselected context that
// is in fact CategoryAuthPolicy and so exit 77. Only a reader comparing a row
// against the category a refusal carries catches that. This test guards the
// cheaper and commoner drift: a class added, removed, or renumbered in code
// with the document left behind.
func TestTheDocumentedExitClassesAreTheOnesThisPackageDefines(t *testing.T) {
	defined := definedExitClasses(t)
	documented := documentedExitClasses(t)

	for name, status := range defined {
		if documented[status] == 0 {
			t.Errorf("exit.%s is %d, which the exit class table does not document", name, status)
			continue
		}
		if documented[status] > 1 {
			t.Errorf("the exit class table documents %d in %d rows", status, documented[status])
		}
	}
	for status := range documented {
		if !hasStatus(defined, status) {
			t.Errorf("the exit class table documents %d, which this package does not define", status)
		}
	}
}

// definedExitClasses reads the constants out of this package's own source
// rather than listing them here, so a class added to the package is compared
// against the document without anyone remembering to add it twice.
//
// It reads every non-test file in the package directory, not exit.go alone, so
// that a class declared in a file this test has never heard of is still
// compared. It counts only constants declared of type Code: an unrelated
// untyped integer constant added beside them is not an exit class, and
// admitting one would fail this test for a status no process ever returns.
func definedExitClasses(t *testing.T) map[string]int {
	t.Helper()
	packages, err := parser.ParseDir(token.NewFileSet(), ".", func(file os.FileInfo) bool {
		return !strings.HasSuffix(file.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse the exit package: %v", err)
	}
	classes := map[string]int{}
	for _, parsed := range packages {
		for _, file := range parsed.Files {
			collectExitClasses(t, file, classes)
		}
	}
	if len(classes) == 0 {
		t.Fatal("found no constants of type Code; has the package been restructured?")
	}
	return classes
}

// collectExitClasses adds one file's Code constants to classes.
//
// The declared type is tracked across the specs of a const block rather than
// read from each one, because Go lets a block state the type once and leave the
// following lines to repeat it implicitly. Reading each spec alone would see
// the first constant and miss every one after it.
func collectExitClasses(t *testing.T, file *ast.File, classes map[string]int) {
	t.Helper()
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.CONST {
			continue
		}
		declaredType := ""
		for _, specification := range general.Specs {
			value, ok := specification.(*ast.ValueSpec)
			if !ok {
				continue
			}
			if identifier, named := value.Type.(*ast.Ident); named {
				declaredType = identifier.Name
			}
			if declaredType != "Code" || len(value.Names) != 1 || len(value.Values) != 1 {
				continue
			}
			literal, ok := value.Values[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.INT {
				continue
			}
			status, err := strconv.Atoi(literal.Value)
			if err != nil {
				t.Fatalf("exit.%s has an unreadable value %q", value.Names[0].Name, literal.Value)
			}
			classes[value.Names[0].Name] = status
		}
	}
}

// documentedExitClasses counts the rows the table gives each status, so a
// status documented twice is a failure rather than a silent pass.
func documentedExitClasses(t *testing.T) map[int]int {
	t.Helper()
	data, err := os.ReadFile(commandsReference)
	if err != nil {
		t.Fatalf("read %s: %v", commandsReference, err)
	}
	section, _, found := strings.Cut(afterHeading(string(data), "## Exit classes"), "\n## ")
	if !found {
		t.Fatalf("%s has no section after ## Exit classes; has the reference been restructured?",
			commandsReference)
	}
	rows := map[int]int{}
	for _, line := range strings.Split(section, "\n") {
		match := documentedExitClass.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		status, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("unreadable status %q in the exit class table", match[1])
		}
		rows[status]++
	}
	if len(rows) == 0 {
		t.Fatalf("%s documents no exit classes; has the ## Exit classes table been removed?",
			commandsReference)
	}
	return rows
}

func afterHeading(document, heading string) string {
	_, rest, found := strings.Cut(document, heading)
	if !found {
		return ""
	}
	return rest
}

func hasStatus(classes map[string]int, status int) bool {
	for _, defined := range classes {
		if defined == status {
			return true
		}
	}
	return false
}
