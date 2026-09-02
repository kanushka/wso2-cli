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
			{Name: "region", Type: "string"},
			{Name: "all", Type: commandtree.TypeBool},
		}},
		{Path: []string{"apps", "list"}},
	})
	backward := commandtree.New([]commandtree.Command{
		{Path: []string{"apps", "list"}},
		{Path: nil, Flags: []commandtree.Flag{
			{Name: "all", Type: commandtree.TypeBool},
			{Name: "region", Type: "string"},
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

// TestChildResolvesOneLevelAtATime proves the tree answers the only routing
// question a caller should ask it: does the command in hand have a subcommand
// by this name. Resolving a whole path in one call would have to decide which
// words are flag values, and the tree does not know where a caller is on the
// line.
func TestChildResolvesOneLevelAtATime(t *testing.T) {
	tree := commandtree.New([]commandtree.Command{
		{Path: nil, Runnable: true},
		{Path: []string{"apps"}},
		{Path: []string{"apps", "list"}, Runnable: true},
		{Path: []string{"deploy"}, Runnable: true},
	})

	if _, ok := tree.Child(nil, "apps"); !ok {
		t.Error("a top-level command is not a child of the root")
	}
	if _, ok := tree.Child([]string{"apps"}, "list"); !ok {
		t.Error("a nested command is not a child of its parent")
	}
	if _, ok := tree.Child(nil, "list"); ok {
		t.Error("a nested command answered as a child of the root")
	}
	if _, ok := tree.Child([]string{"deploy"}, "list"); ok {
		t.Error("a command answered as a child of an unrelated leaf")
	}
}

// TestRootIsTheCommandAtTheEmptyPath proves the namespace's own command is
// reachable, since that is where every walk of a command line starts.
func TestRootIsTheCommandAtTheEmptyPath(t *testing.T) {
	tree := commandtree.New([]commandtree.Command{
		{Path: nil, Runnable: true, Short: "The namespace itself."},
		{Path: []string{"apps"}},
	})

	found, ok := tree.Root()
	if !ok {
		t.Fatal("the tree declares no root command")
	}
	if found.Short != "The namespace itself." {
		t.Errorf("the root is %+v", found)
	}
	if _, ok := (commandtree.Tree{}).Root(); ok {
		t.Error("the zero tree reported a root command")
	}
}

// TestHasChildrenDistinguishesAGroupFromALeaf proves the tree can say whether a
// command takes subcommands, which is what decides whether an unrecognised word
// under it is a mistake or an argument.
func TestHasChildrenDistinguishesAGroupFromALeaf(t *testing.T) {
	tree := commandtree.New([]commandtree.Command{
		{Path: nil},
		{Path: []string{"apps"}},
		{Path: []string{"apps", "list"}, Runnable: true},
		{Path: []string{"deploy"}, Runnable: true},
	})

	if !tree.HasChildren(nil) {
		t.Error("a namespace with commands reports none")
	}
	if !tree.HasChildren([]string{"apps"}) {
		t.Error("a group reports no children")
	}
	if tree.HasChildren([]string{"deploy"}) {
		t.Error("a leaf reports children")
	}
}

// TestOnlyABooleanFlagTakesNoValue proves the one distinction the parser needs
// from a flag's type. Getting it backwards makes "--all list" swallow "list" as
// --all's value, which turns a command path into a flag argument.
func TestOnlyABooleanFlagTakesNoValue(t *testing.T) {
	if (commandtree.Flag{Name: "all", Type: commandtree.TypeBool}).TakesValue() {
		t.Error("a boolean flag reports that it takes a value")
	}
	for _, kind := range []string{"string", "int", "stringSlice", ""} {
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
