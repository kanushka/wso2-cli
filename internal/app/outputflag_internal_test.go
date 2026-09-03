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
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/internal/parsetree"
)

// TestEveryOutputFlagInterpreterAgrees pins the parsers of the output flag to
// one another.
//
// The shell parses its own flags with pflag everywhere except the product
// namespace path, which parses them again by hand because flag parsing has to
// stay disabled there for a module's own arguments to arrive unparsed (#86's
// namespace boundary, deliberately left alone by #89). wso2 logout used to be
// a second hand-parser of this flag; #89 converted it to a declared flag, so
// this test now proves logout's declared flag agrees with pflag's own answer
// instead of proving two hand-written scanners agreed with each other.
func TestEveryOutputFlagInterpreterAgrees(t *testing.T) {
	for _, spelling := range [][]string{
		{"--output", "json"},
		{"--output=json"},
		{"-o", "json"},
		{"-o=json"},
		{"-ojson"},
		{"--output", "table"},
		{"--output=table"},
		{"-o", "table"},
		{"-o=table"},
		{"-otable"},
	} {
		t.Run(strings.Join(spelling, " "), func(t *testing.T) {
			shell := Shell{Streams: output.Streams{}}
			root := shell.rootCommand()
			// The root's own set, not its persistent one: #147 moved --output
			// off PersistentFlags so that a command which cannot act on it does
			// not advertise it in help. The root still declares it, for the
			// product-namespace dispatch this test compares against.
			if err := root.Flags().Parse(spelling); err != nil {
				t.Fatalf("pflag rejected %v: %v", spelling, err)
			}
			viaPflag, ok := output.ParseMode(root.Flags().Lookup(outputFlag).Value.String())
			if !ok {
				t.Fatalf("pflag accepted %v but the value is not an output mode", spelling)
			}

			line, err := parseProductArgs("reference", parsetree.Tree{}, append([]string{"status"}, spelling...))
			if err != nil {
				t.Fatalf("the namespace parser rejected %v: %v", spelling, err)
			}
			viaHand := line.mode

			if viaPflag != viaHand {
				t.Fatalf("the two parsers disagree on %v: pflag reports %q, the namespace parser reports %q",
					spelling, viaPflag, viaHand)
			}

			// logoutCommand declares no --output of its own: it reads the root's,
			// exactly as every other declared-flag command does. Finding it
			// from a freshly built root and parsing onto it (rather than
			// calling parseLogoutArgs, which #89 deleted) is what proves the
			// actual command wso2 logout runs agrees with pflag, not a
			// separate hand-rolled stand-in for it.
			logoutCmd, _, err := root.Find([]string{"logout"})
			if err != nil {
				t.Fatalf("root.Find(logout) failed: %v", err)
			}
			if err := logoutCmd.ParseFlags(spelling); err != nil {
				t.Fatalf("logout's own flag set rejected %v: %v", spelling, err)
			}
			viaLogout, err := shell.shellOutputMode(logoutCmd)
			if err != nil {
				t.Fatalf("shellOutputMode rejected what logout's own flags parsed from %v: %v", spelling, err)
			}
			if viaPflag != viaLogout {
				t.Fatalf("logout's declared flag disagrees on %v: pflag reports %q, logout reports %q",
					spelling, viaPflag, viaLogout)
			}
		})
	}
}
