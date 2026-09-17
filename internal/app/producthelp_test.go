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
	"github.com/wso2/wso2-cli/sdk/commandtree"
)

// flaggedTree declares a namespace with a group, a leaf beneath it, and a
// runnable command carrying both a shorthand boolean flag and a plain
// string flag, so wso2 <namespace> --help and wso2 <namespace> status --help
// exercise every row renderProductHelp and flagSpelling can print: the
// command table, the flags table, and both spellings a flag can take.
func flaggedTree() commandtree.Tree {
	return commandtree.New([]commandtree.Command{
		{Path: nil, Short: "Explore the widget product.", Flags: []commandtree.Flag{
			{Name: "verbose", Shorthand: "v", Type: "bool", NoOptDefault: "true", Usage: "Print more detail."},
		}},
		{Path: []string{"status"}, Runnable: true, Short: "Report the widget status.", Flags: []commandtree.Flag{
			{Name: "env", Type: "string", Usage: "The environment to query."},
			{Name: "verbose", Shorthand: "v", Type: "bool", NoOptDefault: "true", Usage: "Print more detail."},
		}},
		{Path: []string{"apps"}, Short: "Manage apps."},
		{Path: []string{"apps", "list"}, Runnable: true, Short: "List apps."},
	})
}

// TestProductHelpRendersCommandsAndFlagsAtTheRoot proves the root page lists
// its children, marks the command line with <command> and [flags] once
// either is present, and renders the flags table for the namespace's own
// root flag.
func TestProductHelpRendersCommandsAndFlagsAtTheRoot(t *testing.T) {
	shell, out, errOut := newShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "widget", Version: "0.1.0", CommandTree: flaggedTree()})

	if code := shell.Run([]string{"help", "widget"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	page := out.String()
	for _, want := range []string{
		"Explore the widget product.",
		"Usage:\n  wso2 widget <command> [flags]",
		"status", "apps",
		"-v, --verbose", "Print more detail.",
		"Shell flags, accepted on every command:",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the root page does not contain %q:\n%s", want, page)
		}
	}
}

// TestProductHelpRendersALeafsOwnFlagsWithAndWithoutAShorthand proves a leaf
// command with no children of its own omits <command>, and that
// flagSpelling renders a flag with a shorthand differently from one without,
// and a value-taking flag with its type where a boolean has none.
func TestProductHelpRendersALeafsOwnFlagsWithAndWithoutAShorthand(t *testing.T) {
	shell, out, errOut := newShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "widget", Version: "0.1.0", CommandTree: flaggedTree()})

	if code := shell.Run([]string{"help", "widget", "status"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	page := out.String()
	if strings.Contains(page, "<command>") {
		t.Errorf("a leaf with no children of its own advertises <command>:\n%s", page)
	}
	for _, want := range []string{
		"    --env string", "The environment to query.",
		"-v, --verbose", "Print more detail.",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the status page does not contain %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, "-v, --verbose string") {
		t.Errorf("a boolean flag was rendered with a value type:\n%s", page)
	}
}
