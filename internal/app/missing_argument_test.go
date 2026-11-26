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
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/exit"
)

// TestAMissingArgumentNamesTheCommandThatListsItsValues proves a refusal for
// a left-out argument puts the command that lists the values it takes first
// in next, so an agent learns where an id comes from without reading help.
func TestAMissingArgumentNamesTheCommandThatListsItsValues(t *testing.T) {
	for _, test := range []struct {
		command []string
		flags   []string
		lister  string
	}{
		{[]string{"context", "use"}, nil, "wso2 context list"},
		{[]string{"context", "delete"}, nil, "wso2 context list"},
		{[]string{"context", "rename"}, nil, "wso2 context list"},
		{[]string{"context", "product", "remove"}, nil, "wso2 context show"},
		{[]string{"context", "product", "add"}, []string{"--no-input"}, "wso2 product list"},
		{[]string{"config", "get"}, nil, "wso2 config list"},
		{[]string{"config", "set"}, nil, "wso2 config list"},
		{[]string{"config", "unset"}, nil, "wso2 config list"},
		{[]string{"product", "install"}, nil, "wso2 product list"},
		{[]string{"product", "remove"}, nil, "wso2 product list"},
		{[]string{"product", "update"}, nil, "wso2 product list"},
	} {
		t.Run(strings.Join(test.command, " "), func(t *testing.T) {
			shell, out, _ := newShell(t)
			arguments := append(append(slices.Clone(test.command), test.flags...), "--output", "json")
			if code := shell.Run(arguments); code != exit.Usage {
				t.Fatalf("exit code = %d, want %d", code, exit.Usage)
			}
			var document struct {
				Error struct {
					Code string   `json:"code"`
					Next []string `json:"next"`
				} `json:"error"`
			}
			if err := json.Unmarshal(out.Bytes(), &document); err != nil {
				t.Fatalf("the refusal is not JSON: %v\n%s", err, out)
			}
			if document.Error.Code != "shell.missing_argument" {
				t.Fatalf("code = %q, want shell.missing_argument", document.Error.Code)
			}
			if len(document.Error.Next) < 2 || document.Error.Next[0] != test.lister {
				t.Errorf("next = %q, want %q first and the command after it", document.Error.Next, test.lister)
			}
			refused := "wso2 " + strings.Join(test.command, " ") + " "
			if !slices.ContainsFunc(document.Error.Next[1:], func(line string) bool {
				return strings.HasPrefix(line, refused)
			}) {
				t.Errorf("next = %q does not name the refused command after the list", document.Error.Next)
			}
		})
	}
}
