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

package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/sdk/problem"
)

func TestTheShellParsesOnlyItsOwnFlags(t *testing.T) {
	// Everything the shell does not own belongs to the module, so a module can
	// add flags without the shell being released.
	tests := map[string]struct {
		args      []string
		command   string
		arguments string
		mode      output.Mode
	}{
		"a bare command": {
			args: []string{"status"}, command: "status", mode: output.ModeTable,
		},
		"a nested command": {
			args: []string{"status", "detail"}, command: "status detail", mode: output.ModeTable,
		},
		"an output mode after the command": {
			args: []string{"status", "--output", "json"}, command: "status", mode: output.ModeJSON,
		},
		"an output mode before the command": {
			args: []string{"--output", "json", "status"}, command: "status", mode: output.ModeJSON,
		},
		"an output mode joined by an equals sign": {
			args: []string{"status", "--output=json"}, command: "status", mode: output.ModeJSON,
		},
		"the short output flag": {
			args: []string{"status", "-o", "json"}, command: "status", mode: output.ModeJSON,
		},
		"module flags after the command": {
			args:    []string{"status", "--since", "1h"},
			command: "status", arguments: "--since 1h", mode: output.ModeTable,
		},
		"a module flag that looks like a shell flag": {
			args:    []string{"status", "--outputs", "many"},
			command: "status", arguments: "--outputs many", mode: output.ModeTable,
		},
		"no command at all": {
			args: nil, command: "", mode: output.ModeTable,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			command, arguments, mode, err := parseProductArgs("reference", test.args)
			if err != nil {
				t.Fatalf("parsing %v failed: %v", test.args, err)
			}
			if got := strings.Join(command, " "); got != test.command {
				t.Errorf("command path is %q, want %q", got, test.command)
			}
			if got := strings.Join(arguments, " "); got != test.arguments {
				t.Errorf("module arguments are %q, want %q", got, test.arguments)
			}
			if mode != test.mode {
				t.Errorf("output mode is %q, want %q", mode, test.mode)
			}
		})
	}
}

func TestAnUnsupportedOutputModeIsAUsageProblem(t *testing.T) {
	for _, args := range [][]string{
		{"status", "--output", "yaml"},
		{"status", "--output=yaml"},
		{"status", "-o", "yaml"},
	} {
		_, _, _, err := parseProductArgs("reference", args)
		if code := usageProblemCode(t, err); code != "shell.unknown_output_mode" {
			t.Errorf("parsing %v gave problem %q, want %q", args, code, "shell.unknown_output_mode")
		}
	}
}

func TestAnOutputFlagWithoutAValueIsAUsageProblem(t *testing.T) {
	_, _, _, err := parseProductArgs("reference", []string{"status", "--output"})
	if code := usageProblemCode(t, err); code != "shell.missing_flag_value" {
		t.Errorf("problem code is %q, want %q", code, "shell.missing_flag_value")
	}
}

// usageProblemCode reports the code of a usage problem, failing the test when
// the error is not one.
func usageProblemCode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("parsing reported no failure")
	}
	var typed problem.Problem
	if !errors.As(err, &typed) {
		t.Fatalf("parsing failed with an untyped error: %v", err)
	}
	if typed.Category != problem.CategoryUsage {
		t.Errorf("problem category is %q, want %q", typed.Category, problem.CategoryUsage)
	}
	return typed.Code
}
