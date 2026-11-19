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
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/modules/fixture"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/internal/state"
	"github.com/wso2/wso2-cli/sdk/commandtree"
)

// helpJSON is the shape wso2 help --output json renders, decoded loosely so a
// test reads what it asserts on and nothing else.
type helpJSON struct {
	Name     string            `json:"name"`
	Commands []helpJSONCommand `json:"commands"`
	Flags    []helpJSONFlag    `json:"flags"`
	Products []struct {
		Namespace   string            `json:"namespace"`
		Description string            `json:"description"`
		Installed   bool              `json:"installed"`
		Commands    []helpJSONCommand `json:"commands"`
		Flags       []helpJSONFlag    `json:"flags"`
	} `json:"products"`
	Warnings []string `json:"warnings"`
}

func flagNames(flags []helpJSONFlag) string {
	var names []string
	for _, flag := range flags {
		names = append(names, flag.Name)
	}
	return strings.Join(names, ",")
}

type helpJSONCommand struct {
	Path         []string       `json:"path"`
	Description  string         `json:"description"`
	Examples     []string       `json:"examples"`
	Runnable     bool           `json:"runnable"`
	Commands     []string       `json:"commands"`
	Flags        []helpJSONFlag `json:"flags"`
	ResultFields []string       `json:"resultFields"`
}

type helpJSONFlag struct {
	Name        string `json:"name"`
	Shorthand   string `json:"shorthand"`
	Type        string `json:"type"`
	Default     string `json:"default"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

func decodeHelp(t *testing.T, document string) helpJSON {
	t.Helper()
	var decoded helpJSON
	if err := json.Unmarshal([]byte(document), &decoded); err != nil {
		t.Fatalf("help did not render one JSON document: %v\n%s", err, document)
	}
	return decoded
}

func findPath(commands []helpJSONCommand, path ...string) (helpJSONCommand, bool) {
	for _, command := range commands {
		if strings.Join(command.Path, " ") == strings.Join(path, " ") {
			return command, true
		}
	}
	return helpJSONCommand{}, false
}

// TestHelpJSONRendersTheWholeTreeInOneDocument proves wso2 help --output json
// is one JSON document carrying the shell's commands and flags and every
// product the release or this machine knows: an installed one with the tree
// its receipt declares, a published one marked not installed. The fixture's
// executable is not a program, so the document also proves no module was
// launched to render it; and the release's own catalog copy is all it reads,
// so an unreachable catalog changes nothing.
func TestHelpJSONRendersTheWholeTreeInOneDocument(t *testing.T) {
	shell, out, errOut := newShell(t)
	releasedWith(&shell, stable("api", "API Platform"))
	installFixture(t, shell, fixture.Module{Namespace: "widget", Version: "0.1.0", CommandTree: guidedTree()})
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0", CommandTree: referenceTree()})

	if code := shell.Run([]string{"help", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	document := decodeHelp(t, out.String())
	if document.Name != "wso2" {
		t.Errorf("name = %q, want wso2", document.Name)
	}
	if _, ok := findPath(document.Commands, "context", "create"); !ok {
		t.Errorf("the shell's commands do not reach context create: %v", document.Commands)
	}
	if _, ok := findPath(document.Commands, "help"); !ok {
		t.Errorf("help itself is not listed")
	}
	// The document's own flags are the ones every shell command takes; the
	// root's --context is not, and a shell command that takes it lists it.
	if got := flagNames(document.Flags); got != "help,no-input,output,verbose" {
		t.Errorf("shell flags = %q, want help,no-input,output,verbose", got)
	}
	if list, ok := findPath(document.Commands, "context", "list"); !ok || !strings.Contains(flagNames(list.Flags), "output") {
		t.Errorf("context list does not list the --output it declares: %+v", list)
	}
	if len(document.Warnings) != 0 {
		t.Errorf("a complete document carries warnings: %v", document.Warnings)
	}
	products := map[string]bool{}
	for _, product := range document.Products {
		products[product.Namespace] = product.Installed
		switch product.Namespace {
		case "api":
			if product.Installed || len(product.Commands) != 0 || len(product.Flags) != 0 || product.Description != "API Platform" {
				t.Errorf("a published, uninstalled product is described as %+v", product)
			}
		case "widget":
			if !product.Installed || product.Description != "Work with the widget product." {
				t.Errorf("the installed product is described as %+v", product)
			}
			// The shell flags a product command line takes travel with the
			// product, as the product page prints them.
			if got := flagNames(product.Flags); got != "context,no-input,output" {
				t.Errorf("the product's shell flags = %q, want context,no-input,output", got)
			}
			if root, ok := findPath(product.Commands, "widget"); !ok || root.Runnable ||
				strings.Join(root.Commands, ",") != "api,project,status" {
				t.Errorf("the namespace's own command is %+v", root)
			}
			if _, ok := findPath(product.Commands, "widget", "api", "create"); !ok {
				t.Errorf("the installed tree does not reach widget api create")
			}
		}
	}
	if len(products) != 3 {
		t.Errorf("products = %v, want api, reference and widget", products)
	}
	if strings.Contains(out.String(), "\\u003c") {
		t.Errorf("placeholders were HTML-escaped:\n%s", out)
	}
}

// TestHelpJSONSaysWhenItCannotTellWhatIsInstalled is the JSON form of
// TestHelpSaysWhenItCannotTellWhatIsInstalled: an unreadable store leaves
// every released product unmarked, and the document says so in a warning
// instead of passing as complete.
func TestHelpJSONSaysWhenItCannotTellWhatIsInstalled(t *testing.T) {
	t.Setenv(state.RootEnvVar, "relative/not-absolute")
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	shell := app.Shell{Streams: output.Streams{Out: out, Err: errOut}}
	releasedWith(&shell, stable("api", "API Platform"))

	if code := shell.Run([]string{"help", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	document := decodeHelp(t, out.String())
	if len(document.Products) != 1 || document.Products[0].Installed {
		t.Errorf("products = %+v, want api, unmarked", document.Products)
	}
	if strings.Join(document.Warnings, ";") != "The installed products could not be read, so none is marked." {
		t.Errorf("warnings = %q, want the one the table form prints", document.Warnings)
	}
}

// TestHelpJSONForALeafCarriesItsFlagsWithTypeDefaultAndRequired proves the
// topic form renders the one command asked about, flags with their type,
// default and required marker, examples, and the result fields the module
// declared.
func TestHelpJSONForALeafCarriesItsFlagsWithTypeDefaultAndRequired(t *testing.T) {
	tree := guidedTree()
	for index := range tree.Commands {
		if strings.Join(tree.Commands[index].Path, " ") == "api create" {
			tree.Commands[index].ResultFields = []string{"id", "displayName", "next"}
			tree.Commands[index].Flags = append(tree.Commands[index].Flags,
				commandtree.Flag{Name: "region", Type: "string", Default: "us-east", Usage: "The region."})
		}
	}
	shell, out, errOut := newShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "widget", Version: "0.1.0", CommandTree: tree})

	if code := shell.Run([]string{"help", "widget", "api", "create", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	document := decodeHelp(t, out.String())
	if len(document.Commands) != 0 || len(document.Products) != 1 || len(document.Products[0].Commands) != 1 {
		t.Fatalf("the topic form carries more than the one command: %+v", document)
	}
	command := document.Products[0].Commands[0]
	if strings.Join(command.Path, " ") != "widget api create" || !command.Runnable {
		t.Errorf("command = %+v", command)
	}
	if len(command.Examples) != 1 || command.Examples[0] != "wso2 widget api create -f <file> --project <id>" {
		t.Errorf("examples = %v", command.Examples)
	}
	if strings.Join(command.ResultFields, ",") != "id,displayName,next" {
		t.Errorf("resultFields = %v", command.ResultFields)
	}
	flags := map[string]helpJSONFlag{}
	for _, flag := range command.Flags {
		flags[flag.Name] = flag
	}
	for name, want := range map[string]helpJSONFlag{
		"file":    {Name: "file", Shorthand: "f", Type: "string", Required: true, Description: "The API document."},
		"project": {Name: "project", Type: "string", Required: true, Description: "The project to create it in; wso2 widget project list shows the ids."},
		"dry-run": {Name: "dry-run", Type: "bool", Required: false, Description: "Show what would be created."},
		"region":  {Name: "region", Type: "string", Default: "us-east", Required: false, Description: "The region."},
	} {
		if flags[name] != want {
			t.Errorf("flag %s = %+v, want %+v", name, flags[name], want)
		}
	}
}

// TestHelpJSONIsRenderedForAShellCommandAndForTheHelpFlagForm proves the two
// other spellings: a shell command's subtree, and a product command's --help
// with --output json on the product command line itself.
func TestHelpJSONIsRenderedForAShellCommandAndForTheHelpFlagForm(t *testing.T) {
	shell, out, errOut := newShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "widget", Version: "0.1.0", CommandTree: guidedTree()})

	if code := shell.Run([]string{"help", "context", "-o", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	document := decodeHelp(t, out.String())
	if len(document.Products) != 0 {
		t.Errorf("a shell command's document carries products: %+v", document.Products)
	}
	if family, ok := findPath(document.Commands, "context"); !ok || len(family.Commands) == 0 {
		t.Errorf("the context family is not the document's root: %+v", document.Commands)
	}
	if _, ok := findPath(document.Commands, "context", "create"); !ok {
		t.Errorf("the family's subtree does not reach context create")
	}
	if _, ok := findPath(document.Commands, "login"); ok {
		t.Errorf("a shell command's document carries a command outside its subtree")
	}

	out.Reset()
	if code := shell.Run([]string{"widget", "project", "--help", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	document = decodeHelp(t, out.String())
	if len(document.Products) != 1 || len(document.Products[0].Commands) != 4 {
		t.Fatalf("the --help form does not carry the group and its three leaves: %+v", document)
	}
	if group, ok := findPath(document.Products[0].Commands, "widget", "project"); !ok ||
		strings.Join(group.Examples, "|") != "wso2 widget project list|wso2 widget project create <name>" {
		t.Errorf("the group's examples = %+v", group)
	}
}

// TestHelpTableFormIsUnchangedAndABadModeIsRefused proves --output json took
// nothing from the pages: help without the flag is the same table page, and a
// mode the shell does not render is refused the way every command refuses it.
func TestHelpTableFormIsUnchangedAndABadModeIsRefused(t *testing.T) {
	shell, out, errOut := newShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "widget", Version: "0.1.0", CommandTree: guidedTree()})

	if code := shell.Run([]string{"help", "widget"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.HasPrefix(out.String(), "Work with the widget product.\n\nUsage:\n  wso2 widget <command> [flags]\n") {
		t.Errorf("the table page changed:\n%s", out)
	}
	if code := shell.Run([]string{"help", "--output", "yaml"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want the usage class %d; stderr: %s", code, exit.Usage, errOut)
	}
	if !strings.Contains(errOut.String(), "shell.unknown_output_mode") {
		t.Errorf("a bad mode was not refused as one:\n%s", errOut)
	}
}

// TestHelpJSONForAProductCarriesItsNextStep proves the namespace page's Next
// line reaches the JSON form as next, and a command's page, which has no Next
// line, carries none.
func TestHelpJSONForAProductCarriesItsNextStep(t *testing.T) {
	shell, out, errOut := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	installFixture(t, shell, fixture.Module{Namespace: "widget", Version: "0.1.0", CommandTree: guidedTree()})

	for _, line := range [][]string{{"help", "widget", "-o", "json"}, {"widget", "--help", "-o", "json"}} {
		out.Reset()
		if code := shell.Run(line); code != exit.OK {
			t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
		}
		var document struct {
			Next []string `json:"next"`
		}
		if err := json.Unmarshal(out.Bytes(), &document); err != nil {
			t.Fatalf("not JSON: %v\n%s", err, out)
		}
		if strings.Join(document.Next, "|") != "wso2 context product add widget --url <url>" {
			t.Errorf("%v: next = %q", line, document.Next)
		}
	}

	out.Reset()
	if code := shell.Run([]string{"help", "widget", "project", "-o", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	var page map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &page); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if _, found := page["next"]; found {
		t.Errorf("a command's page carries a next step:\n%s", out)
	}
}

func TestHelpJSONCarriesAProductsRootExampleWhole(t *testing.T) {
	keyring.MockInit()
	shell, out, errOut := newLoginShell(t)
	tree := guidedTree()
	tree.Commands[0].Example = "  wso2 widget project list --sort in"
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0", CommandTree: tree})
	installLogin(t, shell, browserDoc("https://issuer.example.test"))
	store := session.Store{StateRoot: shell.StateRoot}
	if err := store.Save(credentialRef, session.Session{Issuer: "https://issuer.example.test", RefreshToken: "r"}); err != nil {
		t.Fatal(err)
	}

	if code := shell.Run([]string{"help", "reference", "-o", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	var document struct {
		Next []string `json:"next"`
	}
	if err := json.Unmarshal(out.Bytes(), &document); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if strings.Join(document.Next, "|") != "wso2 widget project list --sort in" {
		t.Errorf("next = %q, want the root example whole", document.Next)
	}
}

func TestASavedJSONPreferenceRendersAProductsHelpAsJSON(t *testing.T) {
	shell, out, errOut := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	installFixture(t, shell, fixture.Module{Namespace: "widget", Version: "0.1.0", CommandTree: guidedTree()})
	if code := shell.Run([]string{"config", "set", "output", "json"}); code != exit.OK {
		t.Fatalf("config set output json: exit %d: %s", code, errOut)
	}

	out.Reset()
	if code := shell.Run([]string{"widget", "--help"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	var page map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &page); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if _, wrapped := page["message"]; wrapped {
		t.Errorf("the product's help was rendered as a table and wrapped:\n%s", out)
	}

	out.Reset()
	if code := shell.Run([]string{"widget", "--help", "--output", "table"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if json.Valid(out.Bytes()) {
		t.Errorf("--output table did not override the saved preference:\n%s", out)
	}
}
