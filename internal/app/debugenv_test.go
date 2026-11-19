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

package app_test

import (
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/modules/fixture"
)

const debugStarted = "the shell started"

func TestWSO2DebugTurnsDiagnosticsOn(t *testing.T) {
	for _, value := range []string{"1", "true", "yes"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("WSO2_DEBUG", value)
			shell, _, errOut := newShell(t)
			if code := shell.Run([]string{"version"}); code != exit.OK {
				t.Fatalf("version failed: exit %d, stderr %s", code, errOut)
			}
			if !strings.Contains(errOut.String(), debugStarted) {
				t.Fatalf("WSO2_DEBUG=%s wrote no diagnostics:\n%s", value, errOut)
			}
		})
	}
}

func TestWSO2DebugOffSpellingsWriteNothing(t *testing.T) {
	for _, value := range []string{"", "0", "false", "FALSE"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("WSO2_DEBUG", value)
			shell, _, errOut := newShell(t)
			if code := shell.Run([]string{"version"}); code != exit.OK {
				t.Fatalf("version failed: exit %d, stderr %s", code, errOut)
			}
			if errOut.Len() != 0 {
				t.Fatalf("WSO2_DEBUG=%q wrote diagnostics:\n%s", value, errOut)
			}
		})
	}
}

// An explicit flag is what the person running the command can see, so it wins
// over a variable exported long ago, wherever on the line it is written.
func TestAnExplicitVerboseFalseBeatsWSO2Debug(t *testing.T) {
	for name, args := range map[string][]string{
		"before the command": {"--verbose=false", "version"},
		"after the command":  {"version", "--verbose=false"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("WSO2_DEBUG", "1")
			shell, _, errOut := newShell(t)
			if code := shell.Run(args); code != exit.OK {
				t.Fatalf("version failed: exit %d, stderr %s", code, errOut)
			}
			if strings.Contains(errOut.String(), debugStarted) {
				t.Fatalf("%v still wrote diagnostics under WSO2_DEBUG=1:\n%s", args, errOut)
			}
		})
	}
}

func TestWSO2DebugReachesTheProductNamespacePath(t *testing.T) {
	t.Setenv("WSO2_DEBUG", "1")
	shell, _, errOut := newShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})
	shell.Run([]string{"reference", "status"})
	for _, expected := range []string{debugStarted, "resolved a product namespace"} {
		if !strings.Contains(errOut.String(), expected) {
			t.Fatalf("WSO2_DEBUG=1 wso2 reference status is missing %q in:\n%s", expected, errOut)
		}
	}
}

func TestAnExplicitVerboseFalseBeatsWSO2DebugOnTheProductNamespacePath(t *testing.T) {
	for name, args := range map[string][]string{
		"before the namespace":                        {"--verbose=false", "reference", "status"},
		"after the namespace":                         {"reference", "status", "--verbose=false"},
		"after the namespace, a shell flag before it": {"--output", "json", "reference", "status", "--verbose=false"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("WSO2_DEBUG", "1")
			shell, _, errOut := newShell(t)
			installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})
			shell.Run(args)
			if strings.Contains(errOut.String(), debugStarted) {
				t.Fatalf("%v still wrote diagnostics under WSO2_DEBUG=1:\n%s", args, errOut)
			}
		})
	}
}
