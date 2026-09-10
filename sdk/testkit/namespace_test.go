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

package testkit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/sdk/commandtree"
	"github.com/wso2/wso2-cli/sdk/testkit"
)

// fixtureTree is a small command tree standing in for a product module's own:
// one group with a child, so a top-level word is exercised, and a declared
// help flag on the root, the same way a real module's Declare always carries
// one.
func fixtureTree() commandtree.Tree {
	return commandtree.New([]commandtree.Command{
		{Flags: []commandtree.Flag{
			{Name: commandtree.HelpFlagName, Shorthand: commandtree.HelpFlagShorthand, Type: commandtree.TypeBool},
		}},
		{Path: []string{"resource-servers"}},
		{Path: []string{"resource-servers", "list"}, Runnable: true},
	})
}

// writeFixture writes one throwaway Go source file into a fresh directory and
// returns the directory, so a test can point OwnNamespaceViolations at exactly
// the fault it is proving is caught.
func writeFixture(t *testing.T, source string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\n"+source+"\n"), 0o644); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	return dir
}

// TestOwnNamespaceViolationsCatchesACommandNamedUnderAnotherNamespace
// reintroduces the historical fault directly: a rename swept "identity" out of
// a result's next-step text and left the command's own name behind, so the
// text told the reader to run a command belonging to a namespace that has no
// such subcommand.
func TestOwnNamespaceViolationsCatchesACommandNamedUnderAnotherNamespace(t *testing.T) {
	dir := writeFixture(t, `const next = "Run wso2 account resource-servers list to see what those users can be granted."`)

	violations, err := testkit.OwnNamespaceViolations("identity", fixtureTree(), dir)
	if err != nil {
		t.Fatalf("OwnNamespaceViolations returned %v", err)
	}
	if len(violations) == 0 {
		t.Fatal("a command named under another namespace was not caught")
	}
}

// TestOwnNamespaceViolationsCatchesTheProductNamedAsAnotherNamespace
// reintroduces the historical fault's second shape: a recovery told the reader
// to record a product literally named for the wrong namespace.
func TestOwnNamespaceViolationsCatchesTheProductNamedAsAnotherNamespace(t *testing.T) {
	dir := writeFixture(t, `const next = "wso2 account add-product <account> account --endpoint <url>, or " +
		"wso2 identity connect <url> once module.json declares a product descriptor."`)

	violations, err := testkit.OwnNamespaceViolations("identity", fixtureTree(), dir)
	if err != nil {
		t.Fatalf("OwnNamespaceViolations returned %v", err)
	}
	if len(violations) == 0 {
		t.Fatal("recording the product under another namespace's name was not caught")
	}
}

// TestOwnNamespaceViolationsCatchesConnectNamedUnderAnotherNamespace covers
// connect on its own: the shell serves it identically for every product, so no
// module's own tree ever declares it, and a check that only read the tree
// would miss this exact historical line.
func TestOwnNamespaceViolationsCatchesConnectNamedUnderAnotherNamespace(t *testing.T) {
	dir := writeFixture(t, `const next = "wso2 account connect <url> once module.json declares a product descriptor."`)

	violations, err := testkit.OwnNamespaceViolations("identity", fixtureTree(), dir)
	if err != nil {
		t.Fatalf("OwnNamespaceViolations returned %v", err)
	}
	if len(violations) == 0 {
		t.Fatal("connect named under another namespace was not caught")
	}
}

// TestOwnNamespaceViolationsCatchesTheHelpFlagNamedUnderAnotherNamespace covers
// the third damaged line from the same incident: "wso2 account --help" no
// longer refers to a self-report this module owns. The help flag is derived
// from the tree's own root rather than named by hand, so this also proves that
// derivation reaches the check.
func TestOwnNamespaceViolationsCatchesTheHelpFlagNamedUnderAnotherNamespace(t *testing.T) {
	dir := writeFixture(t, `const next = "Run wso2 account --help to see what this module can do."`)

	violations, err := testkit.OwnNamespaceViolations("identity", fixtureTree(), dir)
	if err != nil {
		t.Fatalf("OwnNamespaceViolations returned %v", err)
	}
	if len(violations) == 0 {
		t.Fatal("--help named under another namespace was not caught")
	}
}

// TestOwnNamespaceViolationsDerivesCommandsFromTheTree proves the check tracks
// what the tree actually declares rather than a fixed list: a word the fixture
// tree does not declare is not protected, and the same word once added to the
// tree is.
func TestOwnNamespaceViolationsDerivesCommandsFromTheTree(t *testing.T) {
	dir := writeFixture(t, `const next = "Run wso2 account apps list to see what this module records."`)

	emptyTree := commandtree.New(nil)
	violations, err := testkit.OwnNamespaceViolations("identity", emptyTree, dir)
	if err != nil {
		t.Fatalf("OwnNamespaceViolations returned %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("a word no tree declares was flagged anyway: %v", violations)
	}

	withApps := commandtree.New([]commandtree.Command{
		{Path: []string{"apps"}},
		{Path: []string{"apps", "list"}, Runnable: true},
	})
	violations, err = testkit.OwnNamespaceViolations("identity", withApps, dir)
	if err != nil {
		t.Fatalf("OwnNamespaceViolations returned %v", err)
	}
	if len(violations) == 0 {
		t.Fatal("adding apps to the tree did not extend the protection to it")
	}
}

// TestOwnNamespaceViolationsAllowsTextNamedUnderItsOwnNamespace is the
// complement every positive case above needs: the same shapes, spelled
// correctly, must pass cleanly, or every module's own tests would fail from
// the day this check landed.
func TestOwnNamespaceViolationsAllowsTextNamedUnderItsOwnNamespace(t *testing.T) {
	dir := writeFixture(t, `const next = "Run wso2 identity resource-servers list to see what those users can be granted. " +
		"Record it with wso2 account add-product <account> identity --endpoint <url>. " +
		"Or run wso2 identity connect <url>, or wso2 identity --help."`)

	violations, err := testkit.OwnNamespaceViolations("identity", fixtureTree(), dir)
	if err != nil {
		t.Fatalf("OwnNamespaceViolations returned %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("correctly namespaced text was flagged: %v", violations)
	}
}

// TestOwnNamespaceViolationsIgnoresAGenericPlaceholder covers the documentation
// shape modules/identity actually ships: an example that names its argument's
// role with <namespace> rather than a concrete product. That is not a module
// naming itself wrong, and flagging it would be a false positive every such
// doc comment would trip.
func TestOwnNamespaceViolationsIgnoresAGenericPlaceholder(t *testing.T) {
	dir := writeFixture(t, `const next = "Record one on an account with wso2 account add-product <account> <namespace> " +
		"--endpoint <url> --audience <identifier>."`)

	violations, err := testkit.OwnNamespaceViolations("identity", fixtureTree(), dir)
	if err != nil {
		t.Fatalf("OwnNamespaceViolations returned %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("a generic placeholder was flagged as a wrong product name: %v", violations)
	}
}

// TestOwnNamespaceViolationsIgnoresComments proves comments are read the same
// way TestNoUserVisibleStringCallsAnAccountAnIdentity reads them: not at all.
// A comment is for the module's own maintainers, and flagging one would not
// change anything a user reads.
func TestOwnNamespaceViolationsIgnoresComments(t *testing.T) {
	dir := writeFixture(t, "// wso2 account resource-servers list is what a maintainer might jot here.\nconst notUsed = 0")

	violations, err := testkit.OwnNamespaceViolations("identity", fixtureTree(), dir)
	if err != nil {
		t.Fatalf("OwnNamespaceViolations returned %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("a comment was flagged as user-facing text: %v", violations)
	}
}

// TestOwnNamespaceViolationsIgnoresTestFiles proves the scan is the module's
// own shipped source, not its tests: a fixture in a _test.go file is not
// something a user ever reads.
func TestOwnNamespaceViolationsIgnoresTestFiles(t *testing.T) {
	dir := t.TempDir()
	source := `package main

const notShipped = "wso2 account resource-servers list"
`
	if err := os.WriteFile(filepath.Join(dir, "main_test.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}

	violations, err := testkit.OwnNamespaceViolations("identity", fixtureTree(), dir)
	if err != nil {
		t.Fatalf("OwnNamespaceViolations returned %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("a _test.go file was scanned as shipped source: %v", violations)
	}
}

// TestOwnNamespaceViolationsNamesTheOffendingFile keeps the failure actionable:
// a developer reading a test failure has to know which file to open.
func TestOwnNamespaceViolationsNamesTheOffendingFile(t *testing.T) {
	dir := writeFixture(t, `const next = "Run wso2 account resource-servers list to see what those users can be granted."`)

	violations, err := testkit.OwnNamespaceViolations("identity", fixtureTree(), dir)
	if err != nil {
		t.Fatalf("OwnNamespaceViolations returned %v", err)
	}
	if len(violations) == 0 || !strings.Contains(violations[0], "main.go") {
		t.Fatalf("the violation does not name the offending file: %v", violations)
	}
}
