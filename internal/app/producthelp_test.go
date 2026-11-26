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

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/modules"
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

// TestProductHelpShowsTheArgumentsALeafDeclares proves a leaf's usage line
// names the arguments its declaration carries, so a reader sees what to type
// before a run that refuses, and that machine-readable help carries them too.
func TestProductHelpShowsTheArgumentsALeafDeclares(t *testing.T) {
	shell, out, errOut := newShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "widget", Version: "0.1.0", CommandTree: guidedTree()})

	if code := shell.Run([]string{"widget", "api", "deploy", "--help"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if want := "Usage:\n  wso2 widget api deploy <api> --gateway-id <gateway> [flags]\n"; !strings.Contains(out.String(), want) {
		t.Errorf("the deploy page does not contain %q:\n%s", want, out)
	}

	out.Reset()
	if code := shell.Run([]string{"help", "widget", "api", "deploy", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if want := `"arguments": "<api> --gateway-id <gateway>"`; !strings.Contains(out.String(), want) {
		t.Errorf("the JSON help does not contain %s:\n%s", want, out)
	}

	out.Reset()
	if code := shell.Run([]string{"help", "context", "use", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if want := `"arguments": "<name>"`; !strings.Contains(out.String(), want) {
		t.Errorf("the shell's JSON help does not contain %s:\n%s", want, out)
	}
}

// guidedTree declares a namespace the way the command guideline asks a module
// to: a root example naming the first thing to run once set up, a group whose
// own examples name its two common actions, a group that declares none and
// leaves them to its leaves, and a leaf with a runnable example and two
// required flags.
func guidedTree() commandtree.Tree {
	return withHelpFlag([]commandtree.Command{
		{Path: nil, Short: "Work with the widget product.", Example: "  wso2 widget project list"},
		{Path: []string{"status"}, Runnable: true, Short: "Report the widget status.",
			Example: "  wso2 widget status"},
		{Path: []string{"project"}, Short: "Read and create projects.",
			Example: "  wso2 widget project list\n  wso2 widget project create <name>"},
		{Path: []string{"project", "list"}, Runnable: true, Short: "List projects."},
		{Path: []string{"project", "create"}, Runnable: true, Short: "Create a project.",
			Example: "  wso2 widget project create <name>"},
		{Path: []string{"project", "delete"}, Runnable: true, Short: "Delete a project.",
			Example: "  wso2 widget project delete <id> --yes"},
		{Path: []string{"api"}, Short: "Read and design APIs."},
		{Path: []string{"api", "create"}, Runnable: true, Short: "Create an API.",
			Example: "  wso2 widget api create -f <file> --project <id>",
			Flags: []commandtree.Flag{
				{Name: "file", Shorthand: "f", Type: "string", Required: true, Usage: "The API document."},
				{Name: "project", Type: "string", Required: true,
					Usage: "The project to create it in; wso2 widget project list shows the ids."},
				{Name: "dry-run", Type: "bool", Usage: "Show what would be created."},
			}},
		{Path: []string{"api", "deploy"}, Runnable: true, Short: "Deploy an API.",
			Arguments: "<api> --gateway-id <gateway>",
			Example:   "  wso2 widget api deploy <id> --gateway-id <gateway>"},
		{Path: []string{"api", "list"}, Runnable: true, Short: "List APIs."},
	})
}

// withHelpFlag gives every command the help flag the SDK's extractor declares
// on each one, so --help routes the way it does for a real module.
func withHelpFlag(commands []commandtree.Command) commandtree.Tree {
	for index := range commands {
		commands[index].Flags = append(commands[index].Flags, commandtree.Flag{
			Name: commandtree.HelpFlagName, Shorthand: commandtree.HelpFlagShorthand,
			Type: commandtree.TypeBool, Usage: "Show help for this command.",
		})
	}
	return commandtree.New(commands)
}

// TestLeafHelpMarksRequiredFlagsAndShowsItsExample proves a leaf page is what
// the guideline's table asks for: the flags with the required ones marked, and
// the one runnable example, with the listing command for an id-taking flag
// where the module wrote it, in the flag's usage.
func TestLeafHelpMarksRequiredFlagsAndShowsItsExample(t *testing.T) {
	shell, out, errOut := newShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "widget", Version: "0.1.0", CommandTree: guidedTree()})

	if code := shell.Run([]string{"widget", "api", "create", "--help"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	page := out.String()
	for _, want := range []string{
		"Examples:\n  wso2 widget api create -f <file> --project <id>\n",
		"-f, --file string", "The API document. (required)",
		"    --project string", "wso2 widget project list shows the ids. (required)",
		"    --dry-run", "Show what would be created.\n",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the leaf page does not contain %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, "Show what would be created. (required)") {
		t.Errorf("an optional flag was marked required:\n%s", page)
	}
	if strings.Contains(page, "Next  ") {
		t.Errorf("a leaf page carries a next line, which only the namespace's page computes:\n%s", page)
	}
}

// TestGroupHelpShowsTheGroupsOwnExamplesOrItsLeavesFirstTwo proves a group page
// shows the examples the module declared on the group, and when it declared
// none, the first two of its leaves' own: the two most common actions, as far
// as a declaration that does not rank them can say.
func TestGroupHelpShowsTheGroupsOwnExamplesOrItsLeavesFirstTwo(t *testing.T) {
	shell, out, errOut := newShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "widget", Version: "0.1.0", CommandTree: guidedTree()})

	if code := shell.Run([]string{"help", "widget", "project"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	own := "Examples:\n  wso2 widget project list\n  wso2 widget project create <name>\n"
	if !strings.Contains(out.String(), own) {
		t.Errorf("the group page does not show the group's own examples:\n%s", out)
	}
	if strings.Contains(out.String(), "project delete") {
		t.Errorf("the group page shows a leaf's example beside the group's own:\n%s", out)
	}

	out.Reset()
	if code := shell.Run([]string{"widget", "api"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	fromLeaves := "Examples:\n  wso2 widget api create -f <file> --project <id>\n" +
		"  wso2 widget api deploy <id> --gateway-id <gateway>\n"
	if !strings.Contains(out.String(), fromLeaves) {
		t.Errorf("a group with no examples of its own does not show its leaves' first two:\n%s", out)
	}
}

// TestNamespaceHelpShowsOneExamplePerGroupAndTheSetupStepWhenNothingIsRecorded
// proves the namespace's page shows one example per command beneath it and
// ends with a next line computed from state: with no context on this machine
// the step is recording where the product runs, and for a product that is a
// login provider, creating the context that logs in through it.
func TestNamespaceHelpShowsOneExamplePerGroupAndTheSetupStepWhenNothingIsRecorded(t *testing.T) {
	shell, out, errOut := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	installFixture(t, shell, fixture.Module{Namespace: "widget", Version: "0.1.0", CommandTree: guidedTree()})
	installFixture(t, shell, fixture.Module{Namespace: "idp", Version: "0.1.0", CommandTree: guidedTree(),
		Product: &modules.ProductDescriptor{Provider: "thunder", Audience: "resource", Scopes: []string{"system"}}})

	if code := shell.Run([]string{"widget", "--help"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	page := out.String()
	examples := "Examples:\n  wso2 widget api create -f <file> --project <id>\n" +
		"  wso2 widget project list\n  wso2 widget status\n"
	if !strings.Contains(page, examples) {
		t.Errorf("the namespace page does not show one example per group:\n%s", page)
	}
	if !strings.HasSuffix(page, "\nNext  Run `wso2 context product add widget --url <url>` to record where the product runs.\n") {
		t.Errorf("the namespace page does not end with the setup step:\n%s", page)
	}

	out.Reset()
	if code := shell.Run([]string{"help", "idp"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.Contains(out.String(), "Next  Run `wso2 context create <name> --login-product idp --url <url> --use`, then `wso2 login`.") {
		t.Errorf("a login provider's page does not name the context to create:\n%s", out)
	}
}

// TestNamespaceHelpNextLineFollowsTheLoginState proves the computed next line
// moves with this machine's state and never launches the module: a recorded
// product with no stored session names wso2 login, and a stored session names
// the module's own first command, its root example. Nothing here starts a
// process, because the fixture's executable is not one.
func TestNamespaceHelpNextLineFollowsTheLoginState(t *testing.T) {
	keyring.MockInit()
	shell, out, errOut := newLoginShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0", CommandTree: guidedTree()})
	installLogin(t, shell, browserDoc("https://issuer.example.test"))

	if code := shell.Run([]string{"help", "reference"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.HasSuffix(out.String(), "\nNext  Run `wso2 login` to log in to the acme-dev context.\n") {
		t.Errorf("a recorded product with no session does not name wso2 login:\n%s", out)
	}

	store := session.Store{StateRoot: shell.StateRoot}
	if err := store.Save(credentialRef, session.Session{Issuer: "https://issuer.example.test", RefreshToken: "r"}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := shell.Run([]string{"reference", "--help"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.HasSuffix(out.String(), "\nNext  Run `wso2 widget project list`.\n") {
		t.Errorf("a logged-in machine is not sent to the module's first command:\n%s", out)
	}
}
