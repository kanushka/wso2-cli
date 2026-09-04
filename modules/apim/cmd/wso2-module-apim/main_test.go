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

func TestStatusAnswersThroughTheContract(t *testing.T) {
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command: []string{"status"},
	})
	if outcome.Err != nil {
		t.Fatalf("the invocation failed: %v", outcome.Err)
	}
	if outcome.Problem != nil {
		t.Fatalf("status returned the problem %v", outcome.Problem)
	}
	if outcome.Result == nil || outcome.Result.Schema != StatusSchema {
		t.Fatalf("status returned %v", outcome.Result)
	}
	if got := outcome.Result.Fields[0].Name; got != "namespace" {
		t.Errorf("the first field is %q, want namespace", got)
	}
	assertEndsWithNext(t, outcome)
}

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
