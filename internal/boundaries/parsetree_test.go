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
	"go/types"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	parseTreePackage = "github.com/wso2/wso2-cli/internal/parsetree"
	catalogPackage   = "github.com/wso2/wso2-cli/internal/catalog"
)

// TestTheParseableCommandTreeCannotReachTheCatalog states the trust boundary
// that lets this shell parse product flags without the catalog being signed.
//
// A module's command tree exists twice: in the local receipt, read from the
// installed executable and pinned to it by a digest checked before every launch,
// and in the catalog, which is fetched over the network and carries no
// signature. A command tree decides how the shell interprets what a user typed,
// so a remote file reaching a parser would let whoever served it change the
// meaning of a command already on screen. The catalog's copy exists to say that
// a command exists, never to say what one means.
//
// The whole dependency closure is checked rather than the package's own import
// lines, because a package reached through one more hop is reached all the same.
func TestTheParseableCommandTreeCannotReachTheCatalog(t *testing.T) {
	command := exec.Command("go", "list", "-deps", parseTreePackage)
	command.Dir = repoRoot(t)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("listing the dependencies of %s: %v\n%s", parseTreePackage, err, output)
	}

	for _, dependency := range strings.Fields(string(output)) {
		if dependency == catalogPackage {
			t.Errorf("%s depends on %s; a tree the shell parses with must not be reachable "+
				"from the unsigned catalog, or catalog signing becomes a prerequisite for "+
				"parsing product flags at all", parseTreePackage, catalogPackage)
		}
	}
}

// TestOnlyAVerifiedReceiptCanProduceAParseableTree proves the boundary is the
// compiler's to enforce rather than a reviewer's to notice.
//
// parsetree.Tree keeps its declaration in an unexported field, so the only way
// to obtain one is a constructor in that package. As long as the only such
// constructor takes a receipt, code holding a tree from anywhere else — the
// catalog above all — cannot hand it to anything that parses, because the call
// does not compile. A second constructor taking anything else would open that
// door quietly, which is what this test is here to notice.
func TestOnlyAVerifiedReceiptCanProduceAParseableTree(t *testing.T) {
	directory := filepath.Join(repoRoot(t), "internal", "parsetree")

	constructors := map[string]string{}
	for _, path := range goFiles(t, directory) {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			t.Fatalf("cannot parse %s: %v", path, err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil || !function.Name.IsExported() {
				continue
			}
			if !returnsTree(function) {
				continue
			}
			constructors[function.Name.Name] = parameterTypes(function)
		}
	}

	if len(constructors) != 1 {
		t.Fatalf("parsetree exports %d constructors of Tree (%v); exactly one, taking a receipt, "+
			"is what keeps a catalog-borne tree away from the parser", len(constructors), constructors)
	}
	taken, found := constructors["FromReceipt"]
	if !found {
		t.Fatalf("the only constructor of parsetree.Tree is %v, not FromReceipt", constructors)
	}
	if !strings.Contains(taken, "modules.Receipt") {
		t.Errorf("FromReceipt takes %s; it must take the verified receipt, so that holding a "+
			"parseable tree means having resolved an installation", taken)
	}
}

// returnsTree reports whether a function returns the package's Tree type.
func returnsTree(function *ast.FuncDecl) bool {
	if function.Type.Results == nil {
		return false
	}
	for _, result := range function.Type.Results.List {
		if identifier, ok := result.Type.(*ast.Ident); ok && identifier.Name == "Tree" {
			return true
		}
	}
	return false
}

// parameterTypes renders a function's parameter types, for a failure that names
// what the offending constructor actually accepts.
func parameterTypes(function *ast.FuncDecl) string {
	var rendered []string
	for _, parameter := range function.Type.Params.List {
		rendered = append(rendered, types.ExprString(parameter.Type))
	}
	return strings.Join(rendered, ", ")
}
