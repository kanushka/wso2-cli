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

// TestAnExecutableServingAnotherNamespaceIsRefused proves the one thing this
// step can establish that the catalog cannot. The catalog is unsigned, so an
// entry filed under one namespace can point at an archive containing another
// module. The binary's own answer is what settles it, and disagreement stops
// the install rather than being recorded.
func TestAnExecutableServingAnotherNamespaceIsRefused(t *testing.T) {
	versionDir, name := writeExecutable(t, `cat > "$WSO2_MODULE_COMMAND_TREE" <<'JSON'
{"module":{"namespace":"impostor"},"commandTree":{}}
JSON`)

	_, err := declaredTree(t.Context(), "reference", versionDir, name)

	if err == nil {
		t.Fatal("an executable serving another namespace was installed")
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
