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
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/testkit"
)

// TestALeftOutIDNamesTheCommandThatListsIt proves a command refused for a
// left-out id names, first in its recovery, the command that lists the ids,
// so the shell's next leads with it. A command given its integration but
// not its build names that integration's own build list.
func TestALeftOutIDNamesTheCommandThatListsIt(t *testing.T) {
	for _, test := range []struct {
		command   []string
		arguments []string
		lister    string
	}{
		{[]string{"project", "show"}, nil, "wso2 intg project list"},
		{[]string{"project", "delete"}, nil, "wso2 intg project list"},
		{[]string{"integration", "show"}, nil, "wso2 intg integration list"},
		{[]string{"integration", "delete"}, nil, "wso2 intg integration list"},
		{[]string{"integration", "deploy"}, []string{"--env", "dev"}, "wso2 intg integration list"},
		{[]string{"integration", "promote"}, []string{"--from", "dev", "--to", "prod"}, "wso2 intg integration list"},
		{[]string{"integration", "stop"}, []string{"--env", "dev"}, "wso2 intg integration list"},
		{[]string{"integration", "redeploy"}, []string{"--env", "dev"}, "wso2 intg integration list"},
		{[]string{"integration", "logs"}, []string{"--env", "dev"}, "wso2 intg integration list"},
		{[]string{"integration", "build", "run"}, nil, "wso2 intg integration list"},
		{[]string{"integration", "build", "list"}, nil, "wso2 intg integration list"},
		{[]string{"integration", "build", "show"}, nil, "wso2 intg integration list"},
		{[]string{"integration", "build", "show"}, []string{"order-api"}, "wso2 intg integration build list order-api"},
		{[]string{"integration", "build", "logs"}, []string{"order-api"}, "wso2 intg integration build list order-api"},
		{[]string{"integration", "deployment", "list"}, nil, "wso2 intg integration list"},
		{[]string{"integration", "deployment", "show"}, []string{"--env", "dev"}, "wso2 intg integration list"},
		{[]string{"integration", "deploy"}, []string{"order-api", "--env", ""}, "wso2 intg environment list"},
		{[]string{"integration", "promote"}, []string{"order-api", "--from", "", "--to", "prod"}, "wso2 intg environment list"},
	} {
		t.Run(strings.Join(append(test.command, test.arguments...), " "), func(t *testing.T) {
			stub := newPlatformStub(t, nil)
			outcome := run(test.command, test.arguments, stub.endpoint(), &testkit.Access{Token: "t"})

			assertProblem(t, outcome, "intg.invalid_argument", problem.CategoryUsage)
			if want := "Run " + test.lister + " "; !strings.HasPrefix(outcome.Problem.Recovery, want) {
				t.Errorf("recovery = %q, want it to start %q", outcome.Problem.Recovery, want)
			}
			if !strings.Contains(outcome.Problem.Recovery, "then wso2 intg "+strings.Join(test.command, " ")) {
				t.Errorf("recovery = %q does not name the refused command after the list", outcome.Problem.Recovery)
			}
		})
	}
}
