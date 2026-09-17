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
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/state"
)

// TestProductWordsStepOverTheRootFlags pins where completion finds the product
// namespace: after the root's own flags and their values, read the way pflag
// reads them.
func TestProductWordsStepOverTheRootFlags(t *testing.T) {
	root := Shell{}.rootCommand()
	for _, test := range []struct {
		words     string
		namespace string
		rest      string
	}{
		{"api apis", "api", "apis"},
		{"--context prod api", "api", ""},
		{"--context=prod api", "api", ""},
		{"--verbose api", "api", ""},
		{"-o json api", "api", ""},
		{"-ojson api", "api", ""},
		{"-o=json api", "api", ""},
		{"-ho json api list", "api", "list"},
		{"-oh api", "api", ""},
		{"-h api", "api", ""},
		{"--output", "", ""},
		{"-x api", "api", ""},
		{"-- api", "", ""},
		// An unknown root flag is not stepped over as though it took a value
		// (rootFlagTakesValue finding no candidate flag at all), so the word
		// after it is read as the namespace instead of api.
		{"--nosuch x api", "x", "api"},
	} {
		t.Run(test.words, func(t *testing.T) {
			namespace, rest, found := productWords(root, strings.Fields(test.words))
			if namespace != test.namespace || found != (test.namespace != "") {
				t.Fatalf("namespace = %q (found %v), want %q", namespace, found, test.namespace)
			}
			if want := strings.Fields(test.rest); !slices.Equal(rest, want) && len(rest)+len(want) > 0 {
				t.Fatalf("rest = %q, want %q", rest, want)
			}
		})
	}
}

// TestCompleteFirstContextNameOffersNothingForASecondArgument proves the
// completion registered for a command whose first argument names a context
// (and takes nothing after it) stops offering context names once that
// argument is already there.
func TestCompleteFirstContextNameOffersNothingForASecondArgument(t *testing.T) {
	completions, directive := Shell{}.completeFirstContextName(nil, []string{"already"}, "")
	if completions != nil {
		t.Errorf("completions = %v, want none for a second argument", completions)
	}
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("directive = %v, want ShellCompDirectiveNoFileComp", directive)
	}
}

// TestCompletionOffersNothingWithAnUnresolvableStateRoot proves that
// completeNamespaces and completeContextNames degrade to no completions,
// rather than failing, when the state root cannot even be resolved:
// completion is not where a broken environment gets reported.
func TestCompletionOffersNothingWithAnUnresolvableStateRoot(t *testing.T) {
	t.Setenv(state.RootEnvVar, "relative/not-absolute")
	shell := Shell{}
	root := shell.rootCommand()

	if got := shell.completeNamespaces(root, ""); got != nil {
		t.Errorf("completeNamespaces = %v, want none", got)
	}
	completions, directive := shell.completeContextNames(nil, nil, "")
	if completions != nil {
		t.Errorf("completeContextNames = %v, want none", completions)
	}
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("directive = %v, want ShellCompDirectiveNoFileComp", directive)
	}
}

// TestCompleteContextNamesOffersNothingForAMalformedDocument proves that a
// context document this shell cannot decode offers no completions, the same
// way an unresolvable state root does, rather than failing the completion.
func TestCompleteContextNamesOffersNothingForAMalformedDocument(t *testing.T) {
	shell := Shell{StateRoot: t.TempDir()}
	path := contexts.Path(shell.StateRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	completions, directive := shell.completeContextNames(nil, nil, "")
	if completions != nil {
		t.Errorf("completions = %v, want none for a malformed document", completions)
	}
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("directive = %v, want ShellCompDirectiveNoFileComp", directive)
	}
}
