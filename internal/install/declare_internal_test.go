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
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// helperControl is the control file the stand-in module reads. It is spelled
// out here rather than imported, so the helper and the test agree through a
// file rather than through a shared type neither of them writes to disk.
type helperControl struct {
	Mode            string `json:"mode"`
	Namespace       string `json:"namespace"`
	Command         string `json:"command"`
	Flag            string `json:"flag"`
	EchoEnvironment string `json:"echoEnvironment"`
}

// buildHelper compiles the stand-in module once for the whole package.
//
// It is compiled rather than written as a shell script because the installer
// runs what it installed: on Windows os/exec resolves an executable by
// extension and never reads a shebang, so a script would not run at all and
// every test here would quietly assert nothing.
var buildHelper = sync.OnceValues(func() (string, error) {
	directory, err := os.MkdirTemp("", "wso2-declare-helper-")
	if err != nil {
		return "", err
	}
	binary := filepath.Join(directory, "declarehelper"+executableSuffix())
	command := exec.Command("go", "build", "-o", binary, "./testdata/declarehelper")
	if output, err := command.CombinedOutput(); err != nil {
		return "", fmt.Errorf("building the stand-in module: %w\n%s", err, output)
	}
	return binary, nil
})

// executableSuffix is what an executable is called on this platform.
func executableSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// installHelper puts the stand-in module in a version directory and steers it
// with a control file, returning the directory and the executable's name.
func installHelper(t *testing.T, control helperControl) (versionDir, name string) {
	t.Helper()
	source, err := buildHelper()
	if err != nil {
		t.Fatalf("preparing the stand-in module: %v", err)
	}
	content, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("reading the stand-in module: %v", err)
	}

	versionDir = t.TempDir()
	name = "wso2-module-reference" + executableSuffix()
	if err := os.WriteFile(filepath.Join(versionDir, name), content, 0o755); err != nil {
		t.Fatalf("installing the stand-in module: %v", err)
	}
	steering, err := json.Marshal(control)
	if err != nil {
		t.Fatalf("encoding the control file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, "control.json"), steering, 0o600); err != nil {
		t.Fatalf("writing the control file: %v", err)
	}
	return versionDir, name
}

// TestTheInstallerReadsTheTreeFromTheExecutable proves where a parsed tree comes
// from. It is read out of the binary being installed, not copied from the
// catalog entry that pointed at it, because the catalog is fetched over the
// network and is not signed, and a tree decides how a command line is read.
func TestTheInstallerReadsTheTreeFromTheExecutable(t *testing.T) {
	versionDir, name := installHelper(t, helperControl{
		Mode: "declare", Namespace: "reference", Command: "status", Flag: "since",
	})

	tree, err := declaredTree(t.Context(), "reference", versionDir, name)
	if err != nil {
		t.Fatalf("reading the declaration: %v", err)
	}

	command, ok := tree.Child(nil, "status")
	if !ok {
		t.Fatalf("the installer read the tree %+v", tree)
	}
	if !command.Runnable {
		t.Errorf("the declared command came back as %+v", command)
	}
	if flag, found := command.LookupFlag("since"); !found || !flag.TakesValue() {
		t.Errorf("the flag came back as %+v, found %v", flag, found)
	}
}

// TestAModuleThatCannotDeclareIsStillInstalled proves declaring is optional. A
// module built before declarations existed, or one that does not use Cobra,
// ignores the request and fails its own way; installation continues with no
// tree, and the shell parses for it the way it always did.
func TestAModuleThatCannotDeclareIsStillInstalled(t *testing.T) {
	versionDir, name := installHelper(t, helperControl{Mode: "fail"})

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
	versionDir, name := installHelper(t, helperControl{Mode: "hang"})
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
	versionDir, name := installHelper(t, helperControl{
		Mode: "declare", Namespace: "impostor", Command: "status",
	})

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
	versionDir, name := installHelper(t, helperControl{Mode: "garbage"})

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
	versionDir, name := installHelper(t, helperControl{
		Mode: "declare", Namespace: "reference", EchoEnvironment: "WSO2_TEST_CREDENTIAL",
	})

	tree, err := declaredTree(t.Context(), "reference", versionDir, name)
	if err != nil {
		t.Fatalf("reading the declaration: %v", err)
	}

	if _, ok := tree.Child(nil, "unset"); !ok {
		t.Errorf("the declaring process inherited the shell's environment: %+v", tree)
	}
}
