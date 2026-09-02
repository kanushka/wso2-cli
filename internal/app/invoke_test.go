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
	"path/filepath"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/contexts/fixture"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/internal/parsetree"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// TestAModuleThatDeclaresNoTreeIsParsedAsItAlwaysWas covers the fallback, and
// is what protects a module the shell cannot ask about.
//
// A module built before command declarations existed, or one not built on
// Cobra, installs a receipt with no tree. For it the shell can only do what it
// did before: read the flags it owns, take the leading run of plain words as the
// command, and hand everything from the first flag it does not recognise to the
// module to interpret or refuse. Every case here passes the zero tree, so this
// is that path and not the declared one.
//
// The cost of the fallback is visible in the last two cases: a shell flag
// written after an unrecognised one reaches the module instead of the shell.
// That is the defect declaring a tree removes, and
// TestTheOutputFlagMeansTheSameWhereverItIsWritten is where it is pinned as
// fixed for a module that declares one.
func TestAModuleThatDeclaresNoTreeIsParsedAsItAlwaysWas(t *testing.T) {
	tests := map[string]struct {
		args        []string
		command     string
		arguments   string
		mode        output.Mode
		contextName string
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
		"a context after the command": {
			args:    []string{"status", "--context", "second"},
			command: "status", mode: output.ModeTable, contextName: "second",
		},
		"a context before the command": {
			args:    []string{"--context", "second", "status"},
			command: "status", mode: output.ModeTable, contextName: "second",
		},
		"a context joined by an equals sign": {
			args:    []string{"status", "--context=second"},
			command: "status", mode: output.ModeTable, contextName: "second",
		},
		"module flags after the command": {
			args:    []string{"status", "--since", "1h"},
			command: "status", arguments: "--since 1h", mode: output.ModeTable,
		},
		"a module flag that looks like a shell flag": {
			args:    []string{"status", "--outputs", "many"},
			command: "status", arguments: "--outputs many", mode: output.ModeTable,
		},
		"a module flag that looks like the context flag": {
			args:    []string{"status", "--contexts", "many"},
			command: "status", arguments: "--contexts many", mode: output.ModeTable,
		},
		"no command at all": {
			args: nil, command: "", mode: output.ModeTable,
		},
		"an output flag written after a module flag reaches the module": {
			args:    []string{"status", "--since", "1h", "--output", "json"},
			command: "status", arguments: "--since 1h --output json", mode: output.ModeTable,
		},
		"a context flag written after a module flag reaches the module": {
			args:    []string{"status", "--since", "1h", "--context", "second"},
			command: "status", arguments: "--since 1h --context second", mode: output.ModeTable,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			line, err := parseProductArgs("reference", parsetree.Tree{}, test.args)
			if err != nil {
				t.Fatalf("parsing %v failed: %v", test.args, err)
			}
			command, arguments := line.command, line.arguments
			mode, contextName := line.mode, line.contextName
			if got := strings.Join(command, " "); got != test.command {
				t.Errorf("command path is %q, want %q", got, test.command)
			}
			if got := strings.Join(arguments, " "); got != test.arguments {
				t.Errorf("module arguments are %q, want %q", got, test.arguments)
			}
			if mode != test.mode {
				t.Errorf("output mode is %q, want %q", mode, test.mode)
			}
			if contextName != test.contextName {
				t.Errorf("context name is %q, want %q", contextName, test.contextName)
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
		_, err := parseProductArgs("reference", parsetree.Tree{}, args)
		if code := usageProblemCode(t, err); code != "shell.unknown_output_mode" {
			t.Errorf("parsing %v gave problem %q, want %q", args, code, "shell.unknown_output_mode")
		}
	}
}

func TestAnOutputFlagWithoutAValueIsAUsageProblem(t *testing.T) {
	_, err := parseProductArgs("reference", parsetree.Tree{}, []string{"status", "--output"})
	if code := usageProblemCode(t, err); code != "shell.missing_flag_value" {
		t.Errorf("problem code is %q, want %q", code, "shell.missing_flag_value")
	}
}

func TestAnAttachedOutputFlagWithAnEmptyValueIsAUsageProblem(t *testing.T) {
	// The flag is the shell's however it is spelled, so an empty attached
	// value fails here rather than being handed to the module. A module asked
	// to interpret the shell's own --output would report it as its unknown
	// flag, sending the reader to the wrong place for the same mistake.
	for _, args := range [][]string{
		{"status", "--output="},
		{"status", "-o="},
	} {
		_, err := parseProductArgs("reference", parsetree.Tree{}, args)
		if code := usageProblemCode(t, err); code != "shell.unknown_output_mode" {
			t.Errorf("parsing %v gave problem %q, want %q", args, code, "shell.unknown_output_mode")
		}
	}
}

func TestMissingContextFlagValue(t *testing.T) {
	// An explicitly empty value is refused too: a user who named a context and
	// got the default one instead would never see that it happened.
	for name, args := range map[string][]string{
		"no value at all":                         {"status", "--context"},
		"an empty value":                          {"status", "--context", ""},
		"an empty value joined by an equals sign": {"status", "--context="},
		// Taking the next option as the name refuses as an unknown context and
		// sends the reader looking for a context they never asked for, instead
		// of at the flag they left empty. No context name may begin with "-".
		"the next option, not a name":            {"status", "--context", "--output", "json"},
		"the next option at the end of the line": {"status", "--context", "--output"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseProductArgs("reference", parsetree.Tree{}, args)
			if code := usageProblemCode(t, err); code != "shell.missing_flag_value" {
				t.Errorf("problem code is %q, want %q", code, "shell.missing_flag_value")
			}
		})
	}
}

// twoContextDocument declares two contexts on one identity, default "first".
func twoContextDocument() contexts.Document {
	return contexts.Document{
		SchemaVersion:  contexts.SchemaVersion,
		DefaultContext: "first",
		Identities: []contexts.Identity{{
			Name: "acme",
			Type: "onprem",
			Auth: contexts.IdentityAuth{Kind: contexts.KindPAT, CredentialRef: "acme-login"},
		}},
		Contexts: []contexts.Context{
			{Name: "first", Identity: "acme"},
			{Name: "second", Identity: "acme"},
		},
	}
}

func installSelectionDocument(t *testing.T) Shell {
	t.Helper()
	root := filepath.Join(t.TempDir(), "state")
	if err := fixture.WriteV2(root, twoContextDocument()); err != nil {
		t.Fatalf("fixture.WriteV2 returned %v", err)
	}
	return Shell{StateRoot: root}
}

func TestContextSelectionOrder(t *testing.T) {
	// Three-source resolution, most specific wins: the --context flag beats
	// WSO2_CONTEXT, which beats the document's default context.
	cases := []struct {
		name     string
		args     []string
		env      string // WSO2_CONTEXT value, "" unset
		expected string
	}{
		{"flag wins over env and default", []string{"--context", "second"}, "first", "second"},
		{"env wins over default", nil, "second", "second"},
		{"default when nothing is set", nil, "", "first"},
		{"flag with equals form", []string{"--context=second"}, "", "second"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			shell := installSelectionDocument(t)
			t.Setenv("WSO2_CONTEXT", testCase.env)

			line, err := parseProductArgs("reference", parsetree.Tree{}, testCase.args)
			if err != nil {
				t.Fatalf("parsing %v failed: %v", testCase.args, err)
			}
			selected, err := shell.selection(line.contextName)
			if err != nil {
				t.Fatalf("selection returned %v", err)
			}
			if selected.Context.Name != testCase.expected {
				t.Errorf("selected context is %q, want %q", selected.Context.Name, testCase.expected)
			}
			if selected.Identity.Name != "acme" {
				t.Errorf("the selection does not carry its identity: %+v", selected.Identity)
			}
		})
	}
}

func TestUnknownContextFlagIsTypedProblem(t *testing.T) {
	shell := installSelectionDocument(t)
	t.Setenv("WSO2_CONTEXT", "")

	_, err := shell.selection("ghost")

	if code := usageProblemCode(t, err); code != "contexts.unknown_context" {
		t.Errorf("problem code is %q, want %q", code, "contexts.unknown_context")
	}
}

func TestUnknownContextEnvIsTypedProblem(t *testing.T) {
	shell := installSelectionDocument(t)
	t.Setenv("WSO2_CONTEXT", "ghost")

	_, err := shell.selection("")

	if code := usageProblemCode(t, err); code != "contexts.unknown_context" {
		t.Errorf("problem code is %q, want %q", code, "contexts.unknown_context")
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
