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
	"testing"

	"github.com/wso2/wso2-cli/sdk/testkit"
)

// TestEveryCommandThisModuleNamesIsItsOwn catches the mistake that shipped
// once: a repository-wide rename of the word "identity" rewrote this module's
// own namespace out of its next-step and recovery text, so wso2 identity users
// list told the reader to run wso2 account resource-servers list — a command
// no shell has.
//
// It is worth a test rather than care because the damage is invisible to every
// other check: the module compiles, its handlers return the right fields, and
// the shell renders them faithfully. Only a person following the instruction
// finds out.
//
// The check itself lives in sdk/testkit, shared by every product module,
// because the commands it protects come from this module's own declared
// tree — commands().Declare(), the same declaration the shell parses a command
// line against — rather than from a list kept here by hand. A hand list is
// exactly what let the fault ship in the first place: nothing forced it to be
// updated when a command was, so it drifted from what the module actually
// serves.
func TestEveryCommandThisModuleNamesIsItsOwn(t *testing.T) {
	violations, err := testkit.OwnNamespaceViolations(Namespace, commands().Declare(), ".")
	if err != nil {
		t.Fatalf("checking that %s names its own commands: %v", Namespace, err)
	}
	for _, violation := range violations {
		t.Error(violation)
	}
}
