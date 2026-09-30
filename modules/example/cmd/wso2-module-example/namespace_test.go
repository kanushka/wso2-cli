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

// TestEveryCommandThisModuleNamesIsItsOwn guards against the fault that once
// shipped in the identity module: a rename that swept this module's own
// namespace out of its next-step and recovery text would leave a working
// command telling the reader to run one under a namespace that has no such
// subcommand — invisible to every other check, because the module still
// compiles and still answers correctly through the contract.
//
// The check lives in sdk/testkit, shared by every product module, and derives
// the commands it protects from this module's own declared tree —
// commandTree().Declare() — rather than from a list kept here by hand, so it
// cannot drift from the commands this module actually serves.
func TestEveryCommandThisModuleNamesIsItsOwn(t *testing.T) {
	violations, err := testkit.OwnNamespaceViolations(Namespace, commandTree().Declare(), ".")
	if err != nil {
		t.Fatalf("checking that %s names its own commands: %v", Namespace, err)
	}
	for _, violation := range violations {
		t.Error(violation)
	}
}
