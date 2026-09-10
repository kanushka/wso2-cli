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

// TestEveryCommandThisModuleNamesIsItsOwn guards against a fault that shipped
// once, in another module: a rename swept that module's own namespace out of
// its next-step and recovery text, so a working command told the reader to run
// one under a namespace that has no such subcommand — invisible to every other
// check, because the module still compiled and still answered correctly
// through the contract.
//
// The check lives in sdk/testkit, shared by every product module, and derives
// the commands it protects from this module's own declared tree —
// commands().Declare() — rather than from a list kept here by hand, so it
// cannot drift from the commands this module actually serves as it grows past
// the one command this template generates.
func TestEveryCommandThisModuleNamesIsItsOwn(t *testing.T) {
	violations, err := testkit.OwnNamespaceViolations(Namespace, commands().Declare(), ".")
	if err != nil {
		t.Fatalf("checking that %s names its own commands: %v", Namespace, err)
	}
	for _, violation := range violations {
		t.Error(violation)
	}
}
