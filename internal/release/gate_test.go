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

// The release gate, proven over fixture data rather than over a real tag push.
//
// A pushed tag is the only thing that exercises the release workflow, and a
// tag is not something a test may create. The gate is therefore a pure
// function of the module's declared protocol versions and the released shell's
// supported set, and this file is the whole proof of its decision.
package release_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/catalog"

	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/release"
	"github.com/wso2/wso2-cli/sdk/protocol"
)

func TestGateAdmitsAModuleWithinTheWindow(t *testing.T) {
	if err := release.Gate("reference", "4.5.0",
		modules.Compatibility{Shell: ">=0.1.0 <2.0.0", ProtocolVersions: []int{2, 1}},
		[]int{2, 1}); err != nil {
		t.Fatalf("a module speaking the whole window was refused: %v", err)
	}
}

func TestGateAdmitsAModuleAtTheOlderEndOfTheWindow(t *testing.T) {
	// The older end is the case the window exists for: a user a generation
	// behind can still be served, so a module built against the predecessor
	// SDK has to publish.
	if err := release.Gate("reference", "4.5.0",
		modules.Compatibility{Shell: ">=0.1.0 <2.0.0", ProtocolVersions: []int{1}},
		[]int{2, 1}); err != nil {
		t.Fatalf("a module at the older end of the window was refused: %v", err)
	}
}

func TestGateRefusesAModuleNewerThanTheReleasedShell(t *testing.T) {
	err := release.Gate("reference", "4.5.0",
		modules.Compatibility{Shell: ">=0.1.0 <2.0.0", ProtocolVersions: []int{3}},
		[]int{2, 1})
	if err == nil {
		t.Fatal("a module requiring a protocol no released shell speaks was admitted")
	}
	refusal := err.Error()
	// Both sides, because a product team reading this has to decide between
	// waiting for a shell release and changing the module, and neither half
	// alone tells them which.
	for _, expected := range []string{"reference", "4.5.0", "v3", "v2, v1"} {
		if !strings.Contains(refusal, expected) {
			t.Errorf("the refusal does not name %q: %s", expected, refusal)
		}
	}
	if !strings.Contains(refusal, "wait for a shell release") {
		t.Errorf("the refusal does not say a shell release would admit it: %s", refusal)
	}
}

func TestGateRefusesAModuleOlderThanEveryReleasedShell(t *testing.T) {
	// The window moves on. A module still speaking only a retired protocol is
	// unlaunchable by every shell that exists, which is the same user-visible
	// failure and so is refused for the same reason.
	err := release.Gate("reference", "4.5.0",
		modules.Compatibility{Shell: ">=0.1.0 <2.0.0", ProtocolVersions: []int{1}},
		[]int{3, 2})
	if err == nil {
		t.Fatal("a module speaking only a retired protocol was admitted")
	}
	if !strings.Contains(err.Error(), "v1") || !strings.Contains(err.Error(), "v3, v2") {
		t.Errorf("the refusal does not name both sides: %v", err)
	}
}

func TestGateRefusesAModuleDeclaringNoProtocol(t *testing.T) {
	if err := release.Gate("reference", "4.5.0",
		modules.Compatibility{Shell: ">=0.1.0 <2.0.0"}, []int{2, 1}); err == nil {
		t.Fatal("a module declaring no protocol version was admitted")
	}
}

func TestGateRefusesWhenTheShellSupportsNothing(t *testing.T) {
	// Reading an empty window as "everything is allowed" would turn a broken
	// declaration into an open gate, so it fails closed instead.
	if err := release.Gate("reference", "4.5.0",
		modules.Compatibility{ProtocolVersions: []int{1}}, nil); err == nil {
		t.Fatal("an empty shell window admitted a release")
	}
}

func TestGateRefusesAnUnreadableShellRange(t *testing.T) {
	if err := release.Gate("reference", "4.5.0",
		modules.Compatibility{Shell: "not a range", ProtocolVersions: []int{2}},
		[]int{2, 1}); err == nil {
		t.Fatal("a module declaring an unreadable shell range was admitted")
	}
}

func TestShellWindowIsTheDeclarationTheShellReads(t *testing.T) {
	// The gate and the shell may not come to disagree about what is supported,
	// so there is one declaration and the gate reads it rather than restating
	// it.
	declared := protocol.Window()
	window := release.ShellWindow()
	if len(window) != len(declared) {
		t.Fatalf("the gate reads %v where the shell declares %v", window, declared)
	}
	for index := range window {
		if window[index] != declared[index] {
			t.Fatalf("the gate reads %v where the shell declares %v", window, declared)
		}
	}
}

// Every module this repository can build has to be releasable today. A
// declaration that drifted past the window the released shell supports would
// otherwise be found by a tag push, which is the one moment at which finding it
// is expensive.
func TestEveryDeclaredModuleWouldBeAdmitted(t *testing.T) {
	declarations, err := catalog.Discover(repositoryRoot(t))
	if err != nil {
		t.Fatalf("discovering the buildable modules returned %v", err)
	}
	if len(declarations) == 0 {
		t.Fatal("no module declaration was discovered, so nothing was gated")
	}
	for _, declaration := range declarations {
		if err := release.Gate(declaration.Namespace, "1.0.0",
			declaration.Compatibility, release.ShellWindow()); err != nil {
			t.Errorf("the %s module could not be released: %v", declaration.Namespace, err)
		}
	}
}

// repositoryRoot is this checkout, found from this file rather than from the
// working directory.
func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("the test source location is unknown")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}
