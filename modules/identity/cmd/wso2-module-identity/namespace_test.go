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

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// ownCommands are the words that follow this module's namespace on its own
// command line. A string naming one of them under any other namespace is
// naming a command that does not exist.
var ownCommands = []string{"users", "apps", "resource-servers", "status", "connect", "--help"}

// TestEveryCommandThisModuleNamesIsItsOwn catches the mistake that shipped
// once: a repository-wide rename of the word "identity" rewrote this module's
// own namespace out of its next-step and recovery text, so wso2 identity users
// list told the reader to run wso2 account resource-servers list — a command
// no shell has.
//
// It is worth a test rather than care because the damage is invisible to every
// other check: the module compiles, its handlers return the right fields, and
// the shell renders them faithfully. Only a person following the instruction
// finds out.
func TestEveryCommandThisModuleNamesIsItsOwn(t *testing.T) {
	for _, path := range moduleSources(t) {
		for _, literal := range literalsIn(t, path) {
			for _, command := range ownCommands {
				if strings.Contains(literal, "wso2 account "+command) {
					t.Errorf("%s names %q under the account command, which owns no such subcommand: %q",
						filepath.Base(path), command, literal)
				}
			}
			// The namespace argument of add-product is this module's
			// namespace, not the word account.
			if strings.Contains(literal, "add-product <account> account") {
				t.Errorf("%s tells the reader to record a product called \"account\": %q",
					filepath.Base(path), literal)
			}
		}
	}
}

func moduleSources(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the module directory: %v", err)
	}
	var paths []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".go") && !strings.HasSuffix(entry.Name(), "_test.go") {
			paths = append(paths, entry.Name())
		}
	}
	return paths
}

func literalsIn(t *testing.T, path string) []string {
	t.Helper()
	set := token.NewFileSet()
	parsed, err := parser.ParseFile(set, path, nil, 0)
	if err != nil {
		t.Fatalf("cannot parse %s: %v", path, err)
	}
	var literals []string
	ast.Inspect(parsed, func(node ast.Node) bool {
		if lit, ok := node.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if text, err := strconv.Unquote(lit.Value); err == nil {
				literals = append(literals, text)
			}
		}
		return true
	})
	return literals
}
