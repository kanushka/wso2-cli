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
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/exit"
)

// TestDestructiveContextCommandsAskAndTakeYes proves context delete and
// context product remove ask at a terminal, act on yes, change nothing on no,
// read nothing with --yes, and refuse under --no-input naming --yes.
func TestDestructiveContextCommandsAskAndTakeYes(t *testing.T) {
	for _, command := range [][]string{
		{"context", "delete", "local"},
		{"context", "product", "remove", "api/gateway"},
	} {
		name := strings.Join(command[:len(command)-1], " ")
		t.Run(name+" answered yes", func(t *testing.T) {
			shell, _, _ := newContextShell(t)
			localSetup(t, shell)
			shell.Reader = strings.NewReader("y\n")
			code, _, errOut := run(t, shell, command...)
			if code != exit.OK {
				t.Fatalf("exit %d: %s", code, errOut)
			}
			if !strings.Contains(errOut, "?") {
				t.Errorf("nothing was asked:\n%s", errOut)
			}
		})
		t.Run(name+" answered no", func(t *testing.T) {
			shell, _, _ := newContextShell(t)
			localSetup(t, shell)
			before := loadDocument(t, shell)
			shell.Reader = strings.NewReader("n\n")
			code, _, errOut := run(t, shell, command...)
			if code != exit.Usage || !strings.Contains(errOut, "shell.cancelled") {
				t.Fatalf("exit %d, want a shell.cancelled refusal: %s", code, errOut)
			}
			if after := loadDocument(t, shell); !reflect.DeepEqual(after, before) {
				t.Error("a declined command changed the document")
			}
		})
		t.Run(name+" with --yes", func(t *testing.T) {
			shell, _, _ := newContextShell(t)
			localSetup(t, shell)
			shell.Reader = failIfReadReader{t}
			if code, _, errOut := run(t, shell, append(command, "--yes")...); code != exit.OK {
				t.Fatalf("exit %d: %s", code, errOut)
			}
		})
		t.Run(name+" under --no-input", func(t *testing.T) {
			shell, _, _ := newContextShell(t)
			localSetup(t, shell)
			shell.Reader = failIfReadReader{t}
			code, _, errOut := run(t, shell, append(command, "--no-input")...)
			if code != exit.Usage || !strings.Contains(errOut, "shell.non_interactive") ||
				!strings.Contains(errOut, "--yes") {
				t.Fatalf("exit %d, want shell.non_interactive naming --yes: %s", code, errOut)
			}
		})
	}
}

// TestContextUseWithNoNamePicksAtATerminal proves a bare context use offers
// the configured contexts to pick from, and stays the missing-argument
// refusal where nothing may be asked.
func TestContextUseWithNoNamePicksAtATerminal(t *testing.T) {
	shell, _, _ := newContextShell(t)
	installLogin(t, shell, twoContextDocument())
	shell.Reader = strings.NewReader("2\n")
	if code, _, errOut := run(t, shell, "context", "use"); code != exit.OK || !strings.Contains(errOut, "Context:") {
		t.Fatalf("exit %d, want a pick: %s", code, errOut)
	}
	if selected := loadDocument(t, shell).DefaultContext; selected != "beta" {
		t.Errorf("selected %q, want beta, the second context offered", selected)
	}

	shell.Reader = failIfReadReader{t}
	code, _, errOut := run(t, shell, "context", "use", "--no-input")
	if code != exit.Usage || !strings.Contains(errOut, "shell.missing_argument") {
		t.Fatalf("exit %d, want shell.missing_argument: %s", code, errOut)
	}
}

// TestApplyingAFileAsksNothingWhenNoSessionIsStored proves context apply
// counts the sessions held in the secure store, not the records whose
// binding changes: on a machine that never logged in, nothing is lost and
// nothing is asked.
func TestApplyingAFileAsksNothingWhenNoSessionIsStored(t *testing.T) {
	shell, _, _ := newContextShell(t)
	localSetup(t, shell)
	exported := mustRun(t, shell, "context", "export", "local")
	file := filepath.Join(t.TempDir(), "local.yaml")
	changed := strings.Replace(exported, apiURL, "https://api.other.example", 1)
	if changed == exported {
		t.Fatalf("the export does not name %s:\n%s", apiURL, exported)
	}
	if err := os.WriteFile(file, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	shell.Reader = failIfReadReader{t}
	mustRun(t, shell, "context", "apply", "-f", file, "--no-input")
}
