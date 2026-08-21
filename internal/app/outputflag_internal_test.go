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
)

// TestBothOutputFlagInterpretersAgree pins the two parsers of the output flag
// to one another.
//
// The shell parses its own flags with pflag, and parses them again by hand on
// the product namespace path, because flag parsing has to be disabled there for
// a module's arguments to arrive unparsed. Two parsers of one flag can drift,
// so they are asserted to agree for as long as both exist. The duplication ends
// when a module declares its command tree and the namespace path no longer
// needs unparsed arguments.
func TestBothOutputFlagInterpretersAgree(t *testing.T) {
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
			if err := root.PersistentFlags().Parse(spelling); err != nil {
				t.Fatalf("pflag rejected %v: %v", spelling, err)
			}
			viaPflag, ok := output.ParseMode(root.PersistentFlags().Lookup(outputFlag).Value.String())
			if !ok {
				t.Fatalf("pflag accepted %v but the value is not an output mode", spelling)
			}

			_, _, viaHand, _, err := parseProductArgs("reference", append([]string{"status"}, spelling...))
			if err != nil {
				t.Fatalf("the namespace parser rejected %v: %v", spelling, err)
			}

			if viaPflag != viaHand {
				t.Fatalf("the two parsers disagree on %v: pflag reports %q, the namespace parser reports %q",
					spelling, viaPflag, viaHand)
			}
		})
	}
}
