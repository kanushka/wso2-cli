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

func TestBuiltInCommandsTakePrecedenceOverAReceiptNamespace(t *testing.T) {
	for _, namespace := range []string{"version", "help"} {
		t.Run(namespace, func(t *testing.T) {
			shell, out, errOut := newShell(t)
			// A module claiming a built-in namespace must not be reachable:
			// an installed module cannot shadow shell policy.
			installFixture(t, shell, fixture.Module{Namespace: namespace, Version: "9.9.9"})

			if code := shell.Run([]string{namespace}); code != exit.OK {
				t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
			}
			// Every failure reachable after launch carries an "rpc." code, so
			// its absence is evidence no module was invoked.
			if strings.Contains(errOut.String(), "rpc.") {
				t.Fatalf("the shell dispatched the %q namespace to a module:\n%s", namespace, errOut)
			}
			switch namespace {
			case "version":
				if !strings.Contains(out.String(), "WSO2 CLI") {
					t.Fatalf("wso2 version did not run the built-in command:\n%s", out)
				}
			case "help":
				if !strings.Contains(out.String(), "Usage: wso2") {
					t.Fatalf("wso2 help did not run the built-in command:\n%s", out)
				}
			}
		})
	}
}

func TestAnUnknownCommandIsAUsageProblem(t *testing.T) {
	shell, _, errOut := newShell(t)

	if code := shell.Run([]string{"nonexistent"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want the usage class %d", code, exit.Usage)
	}
	if !strings.Contains(errOut.String(), "shell.unknown_command") {
		t.Fatalf("stderr does not name the usage problem:\n%s", errOut)
	}
}

func TestAnInstalledExecutableThatDoesNotSpeakTheContractIsAProcessProblem(t *testing.T) {
	// The fixture installs an executable that exits without saying anything.
	// It resolves and launches, so the failure has to come from the contract
	// rather than from resolution.
	shell, out, errOut := newShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})

	if code := shell.Run([]string{"reference", "status"}); code != exit.ModuleProcess {
		t.Fatalf("exit code = %d, want the module process class %d; stderr: %s", code, exit.ModuleProcess, errOut)
	}
	if !strings.Contains(errOut.String(), "rpc.no_terminal_message") {
		t.Fatalf("stderr does not report the missing terminal message:\n%s", errOut)
	}
	if out.Len() != 0 {
		t.Fatalf("a failed invocation wrote to standard output:\n%s", out)
	}
}

func TestDispatchRejectsATamperedModuleBeforeAnyInvocation(t *testing.T) {
	shell, _, errOut := newShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})
	if err := fixture.TamperExecutable(storeRoot(shell), "reference", "0.1.0",
		"wso2-module-reference", []byte("tampered")); err != nil {
		t.Fatalf("TamperExecutable returned %v", err)
	}

	if code := shell.Run([]string{"reference", "status"}); code != exit.ModuleTrust {
		t.Fatalf("exit code = %d, want the module trust class %d; stderr: %s", code, exit.ModuleTrust, errOut)
	}
	if !strings.Contains(errOut.String(), "modules.executable_digest_mismatch") {
		t.Fatalf("stderr does not report the integrity failure:\n%s", errOut)
	}
}

func TestDispatchRejectsAnIncompatibleModuleWithTheModuleTrustExitClass(t *testing.T) {
	tests := []struct {
		name     string
		module   fixture.Module
		wantCode string
	}{
		{
			name:     "shell version outside the supported range",
			module:   fixture.Module{Namespace: "reference", Version: "0.1.0", ShellRange: ">=99.0.0"},
			wantCode: "modules.incompatible_shell",
		},
		{
			name:     "protocol version the shell does not speak",
			module:   fixture.Module{Namespace: "reference", Version: "0.1.0", ProtocolVersions: []int{42}},
			wantCode: "modules.incompatible_protocol",
		},
		{
			name:     "executable built for another platform",
			module:   fixture.Module{Namespace: "reference", Version: "0.1.0", OS: "plan9", Arch: "amd64"},
			wantCode: "modules.incompatible_platform",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			shell, _, errOut := newShell(t)
			installFixture(t, shell, test.module)

			if code := shell.Run([]string{"reference", "status"}); code != exit.ModuleTrust {
				t.Fatalf("exit code = %d, want the module trust class %d; stderr: %s", code, exit.ModuleTrust, errOut)
			}
			if !strings.Contains(errOut.String(), test.wantCode) {
				t.Fatalf("stderr does not report %s:\n%s", test.wantCode, errOut)
			}
		})
	}
}

func TestNoArgumentsShowsHelpOnStandardOutput(t *testing.T) {
	shell, out, errOut := newShell(t)

	if code := shell.Run(nil); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.Contains(out.String(), "Usage: wso2 <command>") {
		t.Fatalf("stdout does not show usage:\n%s", out)
	}
}
