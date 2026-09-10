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

package testkit

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/wso2/wso2-cli/sdk/commandtree"
)

// connectVerb is the one subcommand every product namespace answers to without
// its module declaring it: the shell implements "wso2 <namespace> connect
// <url>" itself, identically for every product, so no module's own command
// tree ever carries it. It is named here once, for every module, rather than
// copied into each module's own protection, which is what would let it drift
// the way a hand-maintained list does.
const connectVerb = "connect"

// helpFlag is the flag Cobra gives every command whether or not it declares
// one, spelled the way a reader runs it. It is not read from the constant a
// parser matches flags against because that constant does not carry the
// leading dashes a person types.
const helpFlag = "--" + commandtree.HelpFlagName

// OwnNamespaceViolations reports every place in a module's own Go source where
// user-facing text names one of the module's commands under a namespace other
// than its own, or tells the reader to record the product itself under some
// name other than its own namespace.
//
// It exists to catch the fault that shipped once: a repository-wide rename
// rewrote a product module's own namespace out of its next-step and recovery
// text, so a command that worked told the reader to run one that did not
// exist, and a recovery told them to record a product literally named for the
// wrong namespace. The module still compiled, its handlers still returned the
// right fields, and the shell still rendered them faithfully — only a person
// following the instruction found out.
//
// tree is the module's own declared command tree, ordinarily
// commands().Declare(). The commands this function protects — every word that
// follows the namespace on the module's own command line — are read off it
// rather than kept in a list a module author maintains, so the check cannot
// drift from the commands the module actually has: a command added to the tree
// is a command protected here, with nothing else to update. The one addition
// this function makes on top of the tree is connect, which every product
// answers to without any module declaring it, because the shell serves it
// itself; the help flag needs no such addition; because Cobra gives every
// command one whether or not it declares it, the tree's own root already
// carries it.
//
// The check reads source rather than driving the module through the contract,
// because driving it would only ever exercise the handful of code paths a test
// happens to script, while the damage this function exists to catch can sit in
// a "next" field or a recovery on a path nothing here would think to reach —
// an error branch, a zero-result branch, a branch behind access this test kit
// was never given. Every string literal in the module's own source is read
// instead, whether or not any test ever produces it.
//
// dir is scanned for its own non-test ".go" files; ordinarily this is called
// with "." from the package under test, which is where go test already runs
// it from.
func OwnNamespaceViolations(namespace string, tree commandtree.Tree, dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("testkit: reading %s: %w", dir, err)
	}

	commandChecks := ownCommandPatterns(tree)

	var violations []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := name
		if dir != "." {
			path = dir + string(os.PathSeparator) + name
		}
		literals, err := stringLiteralsIn(path)
		if err != nil {
			return nil, err
		}
		for _, literal := range literals {
			violations = append(violations, commandViolations(name, literal, namespace, commandChecks)...)
			violations = append(violations, productViolations(name, literal, namespace)...)
		}
	}
	return violations, nil
}

// commandCheck pairs one word this module answers to on its own command line
// with the pattern that finds it named under any namespace.
type commandCheck struct {
	word    string
	pattern *regexp.Regexp
}

// ownCommandPatterns builds one pattern per word the tree declares at its own
// top level, plus connect, which no tree declares because the shell serves it
// for every module alike.
//
// Only the top-level word is needed, not a command's full path: a literal
// naming "resource-servers" under the wrong namespace is already the fault
// this function looks for, whatever a reader would type after it.
func ownCommandPatterns(tree commandtree.Tree) []commandCheck {
	seen := map[string]bool{connectVerb: true}
	words := []string{connectVerb}
	if root, ok := tree.Root(); ok {
		if _, hasHelp := root.LookupFlag(commandtree.HelpFlagName); hasHelp {
			seen[helpFlag] = true
			words = append(words, helpFlag)
		}
	}
	for _, command := range tree.Commands {
		if len(command.Path) == 0 {
			continue
		}
		word := command.Path[0]
		if !seen[word] {
			seen[word] = true
			words = append(words, word)
		}
	}

	checks := make([]commandCheck, len(words))
	for index, word := range words {
		// One word between "wso2" and the command is the namespace a reader
		// would type. Requiring exactly one is what tells "wso2 identity
		// resource-servers" apart from a sentence that merely mentions
		// resource-servers somewhere after the word wso2.
		checks[index] = commandCheck{
			word:    word,
			pattern: regexp.MustCompile(`\bwso2\s+(\S+)\s+` + regexp.QuoteMeta(word) + `\b`),
		}
	}
	return checks
}

// commandViolations reports every place literal names one of this module's own
// commands under a namespace other than namespace.
func commandViolations(file, literal, namespace string, checks []commandCheck) []string {
	var violations []string
	for _, check := range checks {
		for _, match := range check.pattern.FindAllStringSubmatch(literal, -1) {
			named := match[1]
			if named == namespace {
				continue
			}
			violations = append(violations, fmt.Sprintf(
				"%s names %q under %q, which is not this module's namespace %q: %q",
				file, check.word, named, namespace, literal))
		}
	}
	return violations
}

// addProductProduct finds the product argument of an add-product example: the
// word naming which product a reader is told to record the account against.
var addProductProduct = regexp.MustCompile(`add-product\s+<\S+>\s+(\S+)`)

// productViolations reports every place literal tells the reader to record
// this product under a name other than its own namespace.
//
// A generic placeholder such as <namespace> is left alone: it is documentation
// naming the argument's role rather than an example naming a concrete, wrong
// product.
func productViolations(file, literal, namespace string) []string {
	var violations []string
	for _, match := range addProductProduct.FindAllStringSubmatch(literal, -1) {
		product := match[1]
		if product == namespace || isPlaceholder(product) {
			continue
		}
		violations = append(violations, fmt.Sprintf(
			"%s tells the reader to record this product as %q, not its own namespace %q: %q",
			file, product, namespace, literal))
	}
	return violations
}

// isPlaceholder reports whether word is documentation naming an argument's
// role, such as <namespace>, rather than a concrete value a reader would type.
func isPlaceholder(word string) bool {
	return strings.HasPrefix(word, "<") && strings.HasSuffix(word, ">")
}

// stringLiteralsIn reads every string literal in a Go source file, which is
// what a user can be shown; comments are deliberately not read, because they
// are for the module's own maintainers rather than for a person running the
// command.
func stringLiteralsIn(path string) ([]string, error) {
	set := token.NewFileSet()
	parsed, err := parser.ParseFile(set, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("testkit: cannot parse %s: %w", path, err)
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
	return literals, nil
}
