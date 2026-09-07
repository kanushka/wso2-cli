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
	"context"
	"testing"

	"github.com/wso2/wso2-cli/sdk/testkit"
)

// The module is driven through the module contract rather than by calling its
// handler, because the contract is what the shell speaks: a handler that returns
// the right fields inside a module that answers the wrong message is not a
// working module.
func TestStatusAnswersThroughTheContract(t *testing.T) {
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command: []string{"status"},
	})

	// The exchange is checked before the answer. Result and Problem are both
	// nil when the handshake or the serve loop failed, so reading the result
	// first would panic instead of reporting what went wrong.
	if outcome.Err != nil {
		t.Fatalf("the invocation failed: %v", outcome.Err)
	}
	if outcome.Problem != nil {
		t.Fatalf("status returned the problem %v", outcome.Problem)
	}
	if outcome.Result == nil {
		t.Fatal("status returned no result")
	}
	if outcome.Result.Schema != StatusSchema {
		t.Errorf("schema = %q, want %q", outcome.Result.Schema, StatusSchema)
	}
	if len(outcome.Result.Fields) == 0 {
		t.Fatal("status returned no fields")
	}
	// The first field is what a table shows first, so the order is part of the
	// answer rather than an accident of how the result was built.
	if got := outcome.Result.Fields[0].Name; got != "namespace" {
		t.Errorf("the first field is %q, want %q", got, "namespace")
	}
	assertEndsWithNext(t, outcome)
}

// An unknown command has to be refused by the module rather than answered, so
// the shell can tell a typo from a command that failed.
func TestAnUnknownCommandIsRefused(t *testing.T) {
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command: []string{"nosuchcommand"},
	})

	if outcome.Err != nil {
		t.Fatalf("the invocation failed: %v", outcome.Err)
	}
	if outcome.Problem == nil {
		t.Fatalf("an unknown command was answered with %v", outcome.Result)
	}
}

// The tree this module serves is what the shell parses a command line against
// before the module is launched. A module whose declaration is empty still
// works, but the shell then cannot answer --help or name a mistyped command,
// and that is a regression this test catches.
func TestTheCommandTreeIsDeclared(t *testing.T) {
	declared := commands().Declare()
	if len(declared.Commands) == 0 {
		t.Fatal("the module declares no commands")
	}
}

// assertEndsWithNext pins the rule every result of this module follows: the
// last field says what a user most likely runs next.
func assertEndsWithNext(t *testing.T, outcome testkit.Outcome) {
	t.Helper()
	if outcome.Result == nil || len(outcome.Result.Fields) == 0 {
		t.Fatal("no result to check")
	}
	last := outcome.Result.Fields[len(outcome.Result.Fields)-1]
	if last.Name != NextField || last.Value == "" {
		t.Errorf("the result does not end with a next step: %+v", outcome.Result.Fields)
	}
}
