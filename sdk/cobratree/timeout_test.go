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

package cobratree_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/sdk/cobratree"
)

// A command that waits on its product declares how long it needs, in whole
// seconds rounded up, and every other command declares nothing.
func TestWithTimeoutIsDeclaredInWholeSecondsRoundedUp(t *testing.T) {
	root := &cobra.Command{Use: "reference"}
	wait := cobratree.WithTimeout(&cobra.Command{Use: "wait"}, 15*time.Minute)
	brief := cobratree.WithTimeout(&cobra.Command{Use: "brief"}, 1500*time.Millisecond)
	plain := &cobra.Command{Use: "plain"}
	root.AddCommand(wait, brief, plain)

	declared := cobratree.New(root).Handle(wait, noop).Handle(brief, noop).Handle(plain, noop).Declare()

	for name, want := range map[string]int{"wait": 900, "brief": 2, "plain": 0} {
		command, ok := findDeclared(declared, name)
		if !ok {
			t.Fatalf("%s is absent", name)
		}
		if command.TimeoutSeconds != want {
			t.Errorf("%s declares %d seconds, want %d", name, command.TimeoutSeconds, want)
		}
	}
	encoded, err := json.Marshal(declared)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"timeoutSeconds":900`) {
		t.Errorf("the declared tree does not carry the timeout: %s", encoded)
	}
	if strings.Count(string(encoded), "timeoutSeconds") != 2 {
		t.Errorf("a command that declared no timeout carries one: %s", encoded)
	}
}

func TestWithTimeoutOfZeroRemovesTheDeclaration(t *testing.T) {
	command := cobratree.WithTimeout(&cobra.Command{Use: "wait"}, time.Minute)
	cobratree.WithTimeout(command, 0)

	root := &cobra.Command{Use: "reference"}
	root.AddCommand(command)
	declared := cobratree.New(root).Handle(command, noop).Declare()

	if found, _ := findDeclared(declared, "wait"); found.TimeoutSeconds != 0 {
		t.Errorf("a removed timeout is still declared as %d seconds", found.TimeoutSeconds)
	}
}

// Only WithTimeout writes the annotation; one set by hand that is not a
// positive whole number of seconds declares nothing rather than something
// the shell would have to guess at.
func TestAMalformedTimeoutAnnotationDeclaresNothing(t *testing.T) {
	root := &cobra.Command{Use: "reference"}
	for _, value := range []string{"15m", "-5", "0", ""} {
		command := &cobra.Command{Use: "c" + strings.Trim(value, "-"),
			Annotations: map[string]string{cobratree.TimeoutAnnotation: value}}
		root.AddCommand(command)
		declared := cobratree.New(root).Handle(command, noop).Declare()
		if found, _ := findDeclared(declared, command.Name()); found.TimeoutSeconds != 0 {
			t.Errorf("annotation %q declared %d seconds", value, found.TimeoutSeconds)
		}
	}
}
