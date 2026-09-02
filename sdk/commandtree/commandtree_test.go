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

package commandtree_test

import (
	"encoding/json"
	"testing"

	"github.com/wso2/wso2-cli/sdk/commandtree"
)

// TestNewOrdersCommandsAndFlagsWhateverOrderTheyArriveIn proves the tree is
// canonical rather than merely sorted once. A declaration is written into a
// receipt, published in a catalog, and compared between the two, so two trees
// carrying the same commands have to serialize identically no matter what order
// the extractor walked them in. Cobra hands its commands back in whatever order
// a map ranged, which is where an unstable declaration would come from.
func TestNewOrdersCommandsAndFlagsWhateverOrderTheyArriveIn(t *testing.T) {
	forward := commandtree.New([]commandtree.Command{
		{Path: []string{"apps"}},
		{Path: nil, Flags: []commandtree.Flag{
			{Name: "region", Type: commandtree.TypeString},
			{Name: "all", Type: commandtree.TypeBool},
		}},
		{Path: []string{"apps", "list"}},
	})
	backward := commandtree.New([]commandtree.Command{
		{Path: []string{"apps", "list"}},
		{Path: nil, Flags: []commandtree.Flag{
			{Name: "all", Type: commandtree.TypeBool},
			{Name: "region", Type: commandtree.TypeString},
		}},
		{Path: []string{"apps"}},
	})

	first, err := json.Marshal(forward)
	if err != nil {
		t.Fatalf("marshalling the forward tree: %v", err)
	}
	second, err := json.Marshal(backward)
	if err != nil {
		t.Fatalf("marshalling the backward tree: %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("the same commands serialized two ways:\n forward: %s\nbackward: %s", first, second)
	}
}

// TestFindMatchesTheLongestCommandPath proves a nested command wins over its
// parent. "apps list" is the command the user typed; answering with "apps"
// and treating "list" as an argument would send the wrong path to the module.
func TestFindMatchesTheLongestCommandPath(t *testing.T) {
	tree := commandtree.New([]commandtree.Command{
		{Path: nil, Runnable: true},
		{Path: []string{"apps"}},
		{Path: []string{"apps", "list"}, Runnable: true},
	})

	cases := []struct {
		args      []string
		path      []string
		remaining []string
	}{
		{args: []string{"apps", "list", "--all"}, path: []string{"apps", "list"}, remaining: []string{"--all"}},
		{args: []string{"apps", "--all"}, path: []string{"apps"}, remaining: []string{"--all"}},
		{args: []string{"--all"}, path: nil, remaining: []string{"--all"}},
		{args: []string{"apps", "list", "one", "two"}, path: []string{"apps", "list"}, remaining: []string{"one", "two"}},
	}
	for _, testCase := range cases {
		found, remaining, ok := tree.Find(testCase.args)
		if !ok {
			t.Errorf("Find(%q) found no command", testCase.args)
			continue
		}
		if !equal(found.Path, testCase.path) {
			t.Errorf("Find(%q) matched %q, want %q", testCase.args, found.Path, testCase.path)
		}
		if !equal(remaining, testCase.remaining) {
			t.Errorf("Find(%q) left %q, want %q", testCase.args, remaining, testCase.remaining)
		}
	}
}

// TestFindReportsAnUnknownCommand proves a word that names no command is not
// quietly treated as an argument to the namespace's default command. That is
// the difference between reporting a typo and running something else.
func TestFindReportsAnUnknownCommand(t *testing.T) {
	tree := commandtree.New([]commandtree.Command{
		{Path: []string{"apps"}, Runnable: true},
	})

	if _, _, ok := tree.Find([]string{"aps"}); ok {
		t.Error("Find matched a command the tree does not declare")
	}
}

// TestFindTellsAPositionalArgumentFromABadSubcommand proves the tree itself
// answers what an unmatched word means. Under a command that groups others it
// is a subcommand the module does not serve, and reporting it is the point of
// declaring a tree. Under a leaf it is that command's own argument, and refusing
// it would make every command that takes one unreachable.
func TestFindTellsAPositionalArgumentFromABadSubcommand(t *testing.T) {
	tree := commandtree.New([]commandtree.Command{
		{Path: []string{"apps"}},
		{Path: []string{"apps", "list"}, Runnable: true},
		{Path: []string{"deploy"}, Runnable: true},
	})

	if _, _, ok := tree.Find([]string{"apps", "lst"}); ok {
		t.Error("a mistyped subcommand under a group was accepted as an argument")
	}
	found, remaining, ok := tree.Find([]string{"deploy", "app.zip"})
	if !ok {
		t.Fatal("a leaf command refused its own positional argument")
	}
	if !equal(found.Path, []string{"deploy"}) || !equal(remaining, []string{"app.zip"}) {
		t.Errorf("Find matched %q leaving %q, want [deploy] leaving [app.zip]", found.Path, remaining)
	}
}

// TestFindOnAnEmptyTreeMatchesNothing proves the zero tree — a module that
// declares none — never claims a command. The shell reads that as "fall back to
// passing the arguments through", so a tree that answered here would silently
// take over parsing for a module that declared nothing.
func TestFindOnAnEmptyTreeMatchesNothing(t *testing.T) {
	var empty commandtree.Tree

	if !empty.Empty() {
		t.Error("the zero tree does not report itself empty")
	}
	if _, _, ok := empty.Find([]string{"apps"}); ok {
		t.Error("the zero tree matched a command")
	}
}

// TestOnlyABooleanFlagTakesNoValue proves the one distinction the parser needs
// from a flag's type. Getting it backwards makes "--all list" swallow "list" as
// --all's value, which turns a command path into a flag argument.
func TestOnlyABooleanFlagTakesNoValue(t *testing.T) {
	if (commandtree.Flag{Name: "all", Type: commandtree.TypeBool}).TakesValue() {
		t.Error("a boolean flag reports that it takes a value")
	}
	for _, kind := range []string{commandtree.TypeString, "int", "stringSlice", ""} {
		if !(commandtree.Flag{Name: "region", Type: kind}).TakesValue() {
			t.Errorf("a flag of type %q reports that it takes no value", kind)
		}
	}
}

// TestLookupFlagFindsAFlagBySpellingAndShorthand proves both spellings reach the
// same declaration, since a user may write either and the parser has to know
// whether a value follows in both cases.
func TestLookupFlagFindsAFlagBySpellingAndShorthand(t *testing.T) {
	command := commandtree.Command{Flags: []commandtree.Flag{
		{Name: "all", Shorthand: "a", Type: commandtree.TypeBool},
	}}

	if flag, ok := command.LookupFlag("all"); !ok || flag.Shorthand != "a" {
		t.Errorf("looking up the long spelling found %+v, %v", flag, ok)
	}
	if flag, ok := command.LookupShorthand('a'); !ok || flag.Name != "all" {
		t.Errorf("looking up the shorthand found %+v, %v", flag, ok)
	}
	if _, ok := command.LookupFlag("a"); ok {
		t.Error("the shorthand answered to the long spelling")
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
