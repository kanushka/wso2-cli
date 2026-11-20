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

package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/sdk/commandtree"
)

// Every command teaches itself: an example on every page, and on every leaf
// the members its result carries, ending with next.
func TestEveryCommandDeclaresItsHelp(t *testing.T) {
	for _, command := range commands().Declare().Commands {
		path := strings.Join(command.Path, " ")
		examples := command.Examples()
		if len(examples) == 0 {
			t.Errorf("%q declares no example", path)
		}
		if !command.Runnable {
			continue
		}
		if fields := command.ResultFields; len(fields) == 0 || fields[len(fields)-1] != NextField {
			t.Errorf("%q declares result fields %v, want them to end with %s", path, fields, NextField)
		}
		// A leaf's example is what a missing required flag's recovery names,
		// so it runs the leaf itself.
		if !strings.HasPrefix(examples[0], strings.TrimSpace("wso2 "+Namespace+" "+path)) {
			t.Errorf("%q example %q runs another command", path, examples[0])
		}
	}
}

// Each example names only commands the tree has and flags they accept, and
// gives every flag the command requires.
func TestEveryExampleRunsAsWritten(t *testing.T) {
	declared := commands().Declare()
	byPath := map[string]commandtree.Command{}
	for _, command := range declared.Commands {
		byPath[strings.Join(command.Path, " ")] = command
	}
	for _, command := range declared.Commands {
		for _, example := range command.Examples() {
			words := strings.Fields(strings.TrimPrefix(example, "wso2 "+Namespace))
			var path []string
			for len(words) > 0 && !strings.HasPrefix(words[0], "-") && !strings.HasPrefix(words[0], "<") {
				if _, known := byPath[strings.Join(append(path, words[0]), " ")]; !known {
					break
				}
				path, words = append(path, words[0]), words[1:]
			}
			target, known := byPath[strings.Join(path, " ")]
			if !known || !target.Runnable {
				t.Errorf("example %q does not name a runnable command", example)
				continue
			}
			var given []string
			for _, word := range words {
				if name, isFlag := strings.CutPrefix(word, "--"); isFlag {
					given = append(given, name)
				}
			}
			for _, name := range given {
				if !slices.ContainsFunc(target.Flags, func(f commandtree.Flag) bool { return f.Name == name }) {
					t.Errorf("example %q gives --%s, which %q does not take", example, name, strings.Join(path, " "))
				}
			}
			for _, flag := range target.Flags {
				if flag.Required && !slices.Contains(given, flag.Name) {
					t.Errorf("example %q leaves out the required --%s", example, flag.Name)
				}
			}
		}
	}
}

// The flags a command cannot run without are marked, so help shows them and
// an agent need not learn them from a refusal.
func TestTheFlagsACommandNeedsAreMarkedRequired(t *testing.T) {
	want := map[string][]string{
		"integration create":          {"type", "runtime", "repo", "branch"},
		"integration deploy":          {"env"},
		"integration promote":         {"from", "to"},
		"integration stop":            {"env"},
		"integration redeploy":        {"env"},
		"integration logs":            {"env"},
		"integration deployment show": {"env"},
	}
	for _, command := range commands().Declare().Commands {
		path := strings.Join(command.Path, " ")
		var required []string
		for _, flag := range command.Flags {
			if flag.Required {
				required = append(required, flag.Name)
			}
		}
		slices.Sort(required)
		expected := slices.Clone(want[path])
		slices.Sort(expected)
		if !slices.Equal(required, expected) {
			t.Errorf("%q marks %v required, want %v", path, required, expected)
		}
	}
}
