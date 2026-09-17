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

package app_test

import (
	"strings"
	"testing"
)

func TestShellNamesCommandsAfterTheNameItWasInvokedAs(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		stderr bool
		want   string
	}{
		{name: "no contexts guidance", args: []string{"context", "list"}, want: "ws context apply -f"},
		{name: "unknown command recovery", args: []string{"product-list"}, stderr: true, want: "ws help"},
		{name: "usage error message", args: []string{"version", "extra"}, stderr: true, want: "ws version takes no arguments"},
		{name: "usage error recovery", args: []string{"version", "extra"}, stderr: true, want: "Run `ws version`."},
		{name: "root usage line", args: []string{"help"}, want: "ws <command> [arguments]"},
		{name: "empty context document", args: []string{"context", "show"}, want: "ws context apply -f"},
		{name: "json recovery member", args: []string{"whoami", "--output", "json"}, want: `"recovery": "Run ws context apply`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NO_COLOR", "1")
			shell, out, errOut := newShell(t)
			shell.Name = "ws"
			shell.Run(tc.args)

			got := out.String()
			if tc.stderr {
				got = errOut.String()
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("output does not contain %q:\nstdout: %s\nstderr: %s", tc.want, out, errOut)
			}
			if strings.Contains(out.String()+errOut.String(), "wso2 ") {
				t.Errorf("output still names wso2:\nstdout: %s\nstderr: %s", out, errOut)
			}
		})
	}
}

func TestShellWithoutANameKeepsTheDefaultName(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	shell, _, errOut := newShell(t)
	shell.Run([]string{"version", "extra"})
	if !strings.Contains(errOut.String(), "Run `wso2 version`.") {
		t.Fatalf("stderr = %q, want the default name", errOut)
	}
}
