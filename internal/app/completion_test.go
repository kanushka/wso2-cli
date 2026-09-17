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
	"slices"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/modules/fixture"
	"github.com/wso2/wso2-cli/sdk/commandtree"
)

// completionTree is a product with a group, a hidden command, and flags that
// do and do not take a value.
func completionTree() commandtree.Tree {
	return commandtree.New([]commandtree.Command{
		{Path: nil, Short: "Manage APIs."},
		{Path: []string{"apis"}, Short: "Work with APIs.", Flags: []commandtree.Flag{
			{Name: "all", Type: "bool", NoOptDefault: "true", Usage: "Include every API."},
		}},
		{Path: []string{"apis", "list"}, Runnable: true, Short: "List APIs.", Flags: []commandtree.Flag{
			{Name: "output", Shorthand: "o", Type: "string", Usage: "Render results as table or json."},
			{Name: "context", Type: "string", Usage: "Use the named context."},
			{Name: "env", Type: "string", Usage: "The environment."},
			{Name: "all", Type: "bool", NoOptDefault: "true", Usage: "Include every API."},
		}},
		{Path: []string{"apis", "delete"}, Runnable: true, Short: "Delete an API."},
		{Path: []string{"secret"}, Runnable: true, Hidden: true},
		{Path: []string{"status"}, Runnable: true, Short: "Report the status."},
	})
}

// completionShell is a shell with the api product installed and two contexts
// configured.
func completionShell(t *testing.T) app.Shell {
	t.Helper()
	shell, _, _ := newShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "api", Version: "0.1.0", CommandTree: completionTree()})
	installFixture(t, shell, fixture.Module{Namespace: "bare", Version: "0.1.0"})
	document := contexts.Document{SchemaVersion: contexts.SchemaVersion,
		Contexts: []contexts.Context{acmeCloud("prod"), acmeCloud("stage")}}
	if err := contexts.Save(shell.StateRoot, document); err != nil {
		t.Fatal(err)
	}
	return shell
}

// complete runs one completion request the way a completion script does and
// returns the offered words and the directive line.
func complete(t *testing.T, shell app.Shell, words ...string) ([]string, string) {
	t.Helper()
	out, errOut := &strings.Builder{}, &strings.Builder{}
	shell.Streams.Out, shell.Streams.Err = out, errOut
	if code := shell.Run(append([]string{"__completeNoDesc"}, words...)); code != exit.OK {
		t.Fatalf("__complete %q exited %d; stderr: %s", words, code, errOut)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	return lines[:len(lines)-1], lines[len(lines)-1]
}

func TestCompletionOffersWhatTheShellKnows(t *testing.T) {
	const noFiles, files = ":4", ":0"
	for _, test := range []struct {
		name      string
		words     []string
		want      []string
		directive string
	}{
		{"built-ins and namespaces", []string{""}, []string{"api", "bare", "completion", "context", "login", "version"}, noFiles},
		{"namespace prefix", []string{"a"}, []string{"api"}, noFiles},
		{"after a root flag", []string{"--context", "prod", "a"}, []string{"api"}, noFiles},
		{"built-in subcommand", []string{"context", "u"}, []string{"use"}, noFiles},
		{"context name argument", []string{"context", "use", ""}, []string{"prod", "stage"}, noFiles},
		{"root context flag", []string{"--context", "s"}, []string{"stage"}, noFiles},
		{"built-in output flag", []string{"context", "list", "--output", ""}, []string{"table", "json"}, noFiles},
		{"product commands", []string{"api", ""}, []string{"apis", "status"}, noFiles},
		{"product subcommands", []string{"api", "apis", ""}, []string{"list", "delete"}, noFiles},
		{"product subcommand prefix", []string{"api", "apis", "l"}, []string{"list"}, noFiles},
		{"product flags", []string{"api", "apis", "list", "--"}, []string{"--output", "--context", "--env", "--all", "--verbose", "--no-input"}, noFiles},
		{"product flag prefix", []string{"api", "apis", "list", "--e"}, []string{"--env"}, noFiles},
		{"product output value", []string{"api", "apis", "list", "--output", ""}, []string{"table", "json"}, noFiles},
		{"product output shorthand", []string{"api", "apis", "list", "-o", "j"}, []string{"json"}, noFiles},
		{"product attached value", []string{"api", "apis", "list", "--context=p"}, []string{"prod"}, noFiles},
		{"shell flag before namespace", []string{"--output", "json", "api", "apis", "list", "--context", ""}, []string{"prod", "stage"}, noFiles},
		{"other product flag value", []string{"api", "apis", "list", "--env", ""}, nil, files},
		{"after a boolean flag", []string{"api", "apis", "--all", ""}, []string{"list", "delete"}, noFiles},
		{"leaf command argument", []string{"api", "status", ""}, nil, files},
		{"unknown product word", []string{"api", "nope", ""}, nil, files},
		{"undeclared tree", []string{"bare", ""}, nil, files},
		{"unknown namespace", []string{"nope", ""}, nil, noFiles},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, directive := complete(t, completionShell(t), test.words...)
			if directive != test.directive {
				t.Errorf("directive = %s, want %s", directive, test.directive)
			}
			for _, want := range test.want {
				if !slices.Contains(got, want) {
					t.Errorf("completions %q do not offer %q", got, want)
				}
			}
			if test.want == nil && len(got) > 0 {
				t.Errorf("completions = %q, want none", got)
			}
			for _, hidden := range []string{"secret", "__complete"} {
				if slices.Contains(got, hidden) {
					t.Errorf("completions %q offer %q", got, hidden)
				}
			}
		})
	}
}

func TestProductSubcommandsAreOfferedAlone(t *testing.T) {
	got, _ := complete(t, completionShell(t), "api", "")
	slices.Sort(got)
	if want := []string{"apis", "status"}; !slices.Equal(got, want) {
		t.Fatalf("completions = %q, want %q", got, want)
	}
}

func TestTheCompletionScriptIsNamedAfterTheShell(t *testing.T) {
	shell, out, errOut := newShell(t)
	if code := shell.Run([]string{"completion", "zsh"}); code != exit.OK {
		t.Fatalf("exit code = %d; stderr: %s", code, errOut)
	}
	if !strings.Contains(out.String(), "#compdef wso2") {
		t.Fatalf("the zsh script does not complete wso2:\n%.200s", out)
	}
}
