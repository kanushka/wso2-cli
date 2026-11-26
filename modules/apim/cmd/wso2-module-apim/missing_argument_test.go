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
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/testkit"
)

// TestALeftOutIDNamesTheCommandThatListsIt proves a command refused for a
// left-out id names, first in its recovery, the command that lists the ids,
// so the shell's next leads with it.
func TestALeftOutIDNamesTheCommandThatListsIt(t *testing.T) {
	for _, test := range []struct {
		command   []string
		arguments []string
		lister    string
	}{
		{[]string{"project", "delete"}, nil, "ws apim project list"},
		{[]string{"api", "deploy"}, []string{"--gateway-id", "g"}, "ws apim api list"},
		{[]string{"api", "undeploy"}, nil, "ws apim api list"},
		{[]string{"api", "delete"}, nil, "ws apim api list"},
		{[]string{"api", "invoke"}, nil, "ws apim api list"},
		{[]string{"api", "get-token"}, nil, "ws apim api list"},
		{[]string{"gateway", "token", "create"}, nil, "ws apim gateway list"},
		{[]string{"api", "deploy"}, []string{"hello-api-v1", "--gateway-id", ""}, "ws apim gateway list"},
	} {
		t.Run(strings.Join(test.command, " "), func(t *testing.T) {
			outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
				Command:   test.command,
				Arguments: test.arguments,
				Context:   module.Context{Name: "c1", Endpoint: "http://127.0.0.1:1"},
				Access:    &testkit.Access{Token: "t"},
			})
			if outcome.Problem == nil {
				t.Fatalf("a command line with no id was not refused: %+v", outcome)
			}
			if want := "Run " + test.lister + " "; !strings.HasPrefix(outcome.Problem.Recovery, want) {
				t.Errorf("recovery = %q, want it to start %q", outcome.Problem.Recovery, want)
			}
			if !strings.Contains(outcome.Problem.Recovery, "then ws apim "+strings.Join(test.command, " ")) {
				t.Errorf("recovery = %q does not name the refused command after the list", outcome.Problem.Recovery)
			}
		})
	}
}
