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

// TestAFlagTakesNoValueWhenPflagGivesItOneWithout is the distinction the parser
// reads a flag for, and it is pflag's NoOptDefVal rather than the type.
//
// A boolean is the common case. A counter is the one that catches a parser
// reading types: its type is "count", it is not boolean, and pflag still leaves
// the next argument alone because it has a NoOptDefVal of "+1" — verified
// against pflag, where "--verbose status" leaves status unconsumed. Reading the
// type would have the shell swallow a command name as a counter's value.
func TestAFlagTakesNoValueWhenPflagGivesItOneWithout(t *testing.T) {
	standsAlone := []commandtree.Flag{
		{Name: "all", Type: commandtree.TypeBool, NoOptDefault: "true"},
		{Name: "verbose", Type: "count", NoOptDefault: "+1"},
	}
	for _, flag := range standsAlone {
		if flag.TakesValue() {
			t.Errorf("%q takes a value although pflag would not read one", flag.Name)
		}
	}
	for _, kind := range []string{"string", "int", "stringSlice", ""} {
		if !(commandtree.Flag{Name: "region", Type: kind}).TakesValue() {
			t.Errorf("a flag of type %q reports that it takes no value", kind)
		}
	}
}

// TestNewGivesABooleanTheDefaultPflagWouldGiveIt proves a tree built by hand
// cannot declare a boolean the parser then feeds an argument to. pflag fills
// this in for every boolean it declares; New fills it in for every boolean
// anyone else writes.
func TestNewGivesABooleanTheDefaultPflagWouldGiveIt(t *testing.T) {
	tree := commandtree.New([]commandtree.Command{
		{Path: []string{"status"}, Flags: []commandtree.Flag{
			{Name: "all", Type: commandtree.TypeBool},
		}},
	})

	command, ok := tree.Child(nil, "status")
	if !ok {
		t.Fatal("the command is absent")
	}
	flag, ok := command.LookupFlag("all")
	if !ok {
		t.Fatal("the flag is absent")
	}
	if flag.TakesValue() {
		t.Errorf("a hand-declared boolean takes a value: %+v", flag)
	}
}

// TestChildAcceptsAnAliasAndAnswersWithTheCanonicalPath proves the alias is a
// way in, not a command. What comes back carries the canonical path, because
// that is the path the module binds its handler to.
func TestChildAcceptsAnAliasAndAnswersWithTheCanonicalPath(t *testing.T) {
	tree := commandtree.New([]commandtree.Command{
		{Path: nil},
		{Path: []string{"apps"}, Aliases: []string{"a"}},
		{Path: []string{"apps", "list"}, Aliases: []string{"ls"}, Runnable: true},
	})

	group, ok := tree.Child(nil, "a")
	if !ok {
		t.Fatal("the alias reached no command")
	}
	if len(group.Path) != 1 || group.Path[0] != "apps" {
		t.Errorf("the alias answered with the path %q, want [apps]", group.Path)
	}
	leaf, ok := tree.Child(group.Path, "ls")
	if !ok {
		t.Fatal("the nested alias reached no command")
	}
	if len(leaf.Path) != 2 || leaf.Path[1] != "list" {
		t.Errorf("the nested alias answered with %q, want [apps list]", leaf.Path)
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
