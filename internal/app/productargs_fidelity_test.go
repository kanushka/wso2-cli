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
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/parsetree"
	"github.com/wso2/wso2-cli/sdk/cobratree"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/result"
)

// fidelityTree builds one Cobra tree and returns both the tree itself and the
// declaration extracted from it, so a test can ask the same question of each.
func fidelityTree(t *testing.T) (*cobra.Command, parsetree.Tree) {
	t.Helper()
	root := &cobra.Command{Use: "reference", Run: func(*cobra.Command, []string) {}}
	root.PersistentFlags().Bool("verbose", false, "Say more.")
	// A counter is not boolean and still takes no value, which is the case a
	// parser reading flag types instead of pflag's NoOptDefVal gets wrong.
	root.PersistentFlags().CountP("loud", "L", "Say more, repeatedly.")
	status := &cobra.Command{Use: "status", Run: func(*cobra.Command, []string) {}}
	status.Flags().String("since", "", "How far back.")
	status.Flags().BoolP("all", "a", false, "Everything.")
	status.Flags().StringP("region", "r", "", "Where.")
	apps := &cobra.Command{Use: "apps", Aliases: []string{"a"}, Run: func(*cobra.Command, []string) {}}
	list := &cobra.Command{Use: "list", Aliases: []string{"ls"}, Run: func(*cobra.Command, []string) {}}
	list.Flags().Int("limit", 0, "How many.")
	apps.AddCommand(list)
	root.AddCommand(status, apps)

	noop := func(context.Context, module.Request) (result.Result, error) {
		return result.Result{}, nil
	}
	declared := cobratree.New(root).Handle(root, noop).Handle(status, noop).
		Handle(apps, noop).Handle(list, noop).Declare()
	return root, parsetree.FromReceipt(modules.Receipt{CommandTree: declared})
}

// TestTheShellLandsWhereTheModuleWouldLand is the fidelity check the whole
// declaration mechanism rests on, and it is differential rather than expected:
// the answers are not written down here, they are asked of Cobra.
//
// The shell parses a product command line so that it can read its own flags
// wherever they appear. That is only safe while the shell reaches the same
// command Cobra would reach and forwards something Cobra would accept. Writing
// the expected answers by hand would pin what this parser does, which is the one
// thing already known; comparing against the module's own framework pins what it
// has to agree with. Both halves are checked — where the line landed, and that
// the module's own flag set takes what was forwarded.
func TestTheShellLandsWhereTheModuleWouldLand(t *testing.T) {
	_, declared := fidelityTree(t)

	lines := [][]string{
		{"status"},
		{"status", "--since", "1h"},
		{"status", "--since=1h"},
		{"status", "--all"},
		{"status", "-a"},
		{"status", "-r", "eu"},
		{"status", "-ar", "eu"},
		{"status", "-areu"},
		{"status", "--verbose"},
		{"--verbose", "status"},
		{"--since", "1h", "status"},
		{"-a", "status"},
		{"apps", "list"},
		{"apps", "list", "--limit", "5"},
		{"apps", "list", "--verbose"},
		{"--verbose", "apps", "list"},
		{"apps", "myapp"},
		{"status", "--", "--since"},
		{"status", "--all=false"},
		{"status", "--all=true"},
		{"status", "-a=false"},
		{"status", "--help=false"},
		{"status", "-h=false"},
		{"--loud", "status"},
		{"-L", "status"},
		{"--loud", "--loud", "status"},
		{"a", "list"},
		{"apps", "ls"},
		{"a", "ls"},
		{"a", "list", "--limit", "5"},
	}

	for _, args := range lines {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			// Cobra's verdict is Find followed by parsing: locating a command
			// is not accepting the line, and a tree is rebuilt per case
			// because parsing leaves flag state behind.
			fresh, _ := fidelityTree(t)
			found, rest, findErr := fresh.Find(args)
			// Cobra gives a command its help flag as it executes, not as it is
			// built, so a tree parsed without that step does not know --help.
			// Adding it here is what Execute would do, and is the same reason
			// Declare synthesises one into the declaration.
			if findErr == nil {
				found.InitDefaultHelpFlag()
			}
			cobraAccepts := findErr == nil && found.ParseFlags(rest) == nil

			line, parseErr := parseProductArgs("reference", declared, args)

			if !cobraAccepts {
				if parseErr == nil {
					t.Errorf("the shell accepted %q, which the module's own parser refuses", args)
				}
				return
			}
			if parseErr != nil {
				t.Fatalf("the shell refused %q, which cobra routes to %q and accepts: %v",
					args, found.CommandPath(), parseErr)
			}

			wanted := strings.TrimPrefix(strings.TrimPrefix(found.CommandPath(), "reference"), " ")
			if got := strings.Join(line.command, " "); got != wanted {
				t.Errorf("the shell reached %q, cobra reaches %q", got, wanted)
			}
			// What the shell forwards has to be something the command it landed
			// on can actually parse.
			verifier, _ := fidelityTree(t)
			target, _, err := verifier.Find(line.command)
			if err != nil {
				t.Fatalf("cobra cannot find the path the shell resolved (%q): %v", line.command, err)
			}
			target.InitDefaultHelpFlag()
			if err := target.ParseFlags(line.arguments); err != nil {
				t.Errorf("the module refuses what the shell forwarded (%q): %v", line.arguments, err)
			}
		})
	}
}
