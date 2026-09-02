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

package install

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeExecutable writes a shell script standing in for an installed module and
// returns the directory holding it. A script is enough because everything under
// test is the contract at the process boundary: the environment variable going
// in, and the file coming back.
func writeExecutable(t *testing.T, body string) (versionDir, name string) {
	t.Helper()
	versionDir = t.TempDir()
	name = "wso2-module-reference"
	path := filepath.Join(versionDir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("writing the stand-in module: %v", err)
	}
	return versionDir, name
}

// TestTheInstallerReadsTheTreeFromTheExecutable proves where a parsed tree comes
// from. It is read out of the binary being installed, not copied from the
// catalog entry that pointed at it, because the catalog is fetched over the
// network and is not signed, and a tree decides how a command line is read.
func TestTheInstallerReadsTheTreeFromTheExecutable(t *testing.T) {
	versionDir, name := writeExecutable(t, `cat > "$WSO2_MODULE_COMMAND_TREE" <<'JSON'
{"module":{"namespace":"reference"},
 "commandTree":{"commands":[{"path":["status"],"runnable":true,
   "flags":[{"name":"since","type":"string"}]}]}}
JSON`)

	tree, err := declaredTree(t.Context(), "reference", versionDir, name)
	if err != nil {
		t.Fatalf("reading the declaration: %v", err)
	}

	command, remaining, ok := tree.Find([]string{"status", "--since", "1h"})
	if !ok {
		t.Fatalf("the installer read the tree %+v", tree)
	}
	if flag, found := command.LookupFlag("since"); !found || !flag.TakesValue() {
		t.Errorf("the flag came back as %+v, found %v", flag, found)
	}
	if len(remaining) != 2 {
		t.Errorf("the command left %q", remaining)
	}
}

// TestAModuleThatCannotDeclareIsStillInstalled proves declaring is optional. A
// module built before declarations existed, or one that does not use Cobra,
// ignores the request and fails its own way; installation continues with no
// tree, and the shell parses for it the way it always did.
func TestAModuleThatCannotDeclareIsStillInstalled(t *testing.T) {
	versionDir, name := writeExecutable(t, `echo "unexpected argument" >&2; exit 2`)

	tree, err := declaredTree(t.Context(), "reference", versionDir, name)

	if err != nil {
		t.Fatalf("a module that cannot declare failed the install: %v", err)
	}
	if !tree.Empty() {
		t.Errorf("a module that declared nothing produced %+v", tree)
	}
}

// TestAModuleThatNeverAnswersDoesNotHangTheInstall proves the timeout. A module
// that ignores the request and waits on standard input the way the protocol
// does would otherwise stall installation with nothing on screen.
func TestAModuleThatNeverAnswersDoesNotHangTheInstall(t *testing.T) {
	versionDir, name := writeExecutable(t, `sleep 60`)
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	started := time.Now()
	tree, err := declaredTree(ctx, "reference", versionDir, name)

	if err != nil {
		t.Fatalf("a module that never answered failed the install: %v", err)
	}
	if !tree.Empty() {
		t.Errorf("a module that never answered produced %+v", tree)
	}
	if elapsed := time.Since(started); elapsed > 30*time.Second {
		t.Errorf("the install waited %s for a module that never answered", elapsed)
	}
}

// TestATreeFromAnExecutableServingAnotherNamespaceIsDiscarded proves the one
// thing this step can establish that the unsigned catalog cannot.
//
// An entry filed under one namespace can point at an archive holding another
// module. Its tree describes commands under a name it does not serve, so parsing
// a user's line against it would parse against a module that is not there. The
// tree is dropped and the module installs without one, which is the same state
// as a module that declares nothing.
func TestATreeFromAnExecutableServingAnotherNamespaceIsDiscarded(t *testing.T) {
	versionDir, name := writeExecutable(t, `cat > "$WSO2_MODULE_COMMAND_TREE" <<'JSON'
{"module":{"namespace":"impostor"},
 "commandTree":{"commands":[{"path":["status"],"runnable":true}]}}
JSON`)

	tree, err := declaredTree(t.Context(), "reference", versionDir, name)

	if err != nil {
		t.Fatalf("a disputed namespace failed the install: %v", err)
	}
	if !tree.Empty() {
		t.Errorf("the shell kept a tree from an executable serving %q: %+v", "impostor", tree)
	}
}

// TestAnUnreadableDeclarationLeavesTheModuleWithoutATree proves a malformed
// answer is not a failed install. Falling back returns the module to the
// positional passthrough every module used before declarations, which forwards
// the arguments to the module to accept or refuse for itself — the same place
// the decision sat before, rather than anywhere more permissive.
func TestAnUnreadableDeclarationLeavesTheModuleWithoutATree(t *testing.T) {
	versionDir, name := writeExecutable(t, `printf 'not json' > "$WSO2_MODULE_COMMAND_TREE"`)

	tree, err := declaredTree(t.Context(), "reference", versionDir, name)

	if err != nil {
		t.Fatalf("an unreadable declaration failed the install: %v", err)
	}
	if !tree.Empty() {
		t.Errorf("an unreadable declaration produced %+v", tree)
	}
}

// TestTheDeclaringProcessInheritsNothing proves this launch is sanitized like
// the other one.
//
// A module process is started twice in a module's life: here, to ask a
// just-downloaded executable what commands it serves, and again for every
// invocation. The invocation path builds the child environment from nothing
// because the shell decides what a module may see rather than filtering what it
// must not. This path runs a binary that arrived seconds ago over an unsigned
// catalog, so handing it the ambient environment — every WSO2_ variable, every
// credential a CI runner exports — would be the worse of the two places to do
// it.
//
// The stand-in reports what it can see by declaring it as a command name, which
// is the only channel it has here.
func TestTheDeclaringProcessInheritsNothing(t *testing.T) {
	t.Setenv("WSO2_TEST_CREDENTIAL", "leaked")
	versionDir, name := writeExecutable(t,
		`printf '{"module":{"namespace":"reference"},"commandTree":{"commands":`+
			`[{"path":["%s"],"runnable":true}]}}' "${WSO2_TEST_CREDENTIAL:-unset}" `+
			`> "$WSO2_MODULE_COMMAND_TREE"`)

	tree, err := declaredTree(t.Context(), "reference", versionDir, name)
	if err != nil {
		t.Fatalf("reading the declaration: %v", err)
	}

	if _, _, ok := tree.Find([]string{"unset"}); !ok {
		t.Errorf("the declaring process inherited the shell's environment: %+v", tree)
	}
}
