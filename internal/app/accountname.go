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

package app

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/wso2/wso2-cli/internal/contexts"
)

// assignedNamePrefix is what a name the shell assigns starts with. An account
// is a saved login setup, not a person and not the software it logs in to, so
// the name the shell gives one says nothing about either (#175): naming it
// after the provider named the software, and two deployments of it collided.
const assignedNamePrefix = "account-"

// nextFreeAccountName is the lowest account-N that neither an account nor a
// context already holds. A context is checked too because the account is
// created with a same-named context beside it, and a name only one of the two
// could take is no name at all.
func nextFreeAccountName(document contexts.Document) string {
	for number := 1; ; number++ {
		name := fmt.Sprintf("%s%d", assignedNamePrefix, number)
		if !accountNameTaken(document, name) {
			return name
		}
	}
}

// accountNameTaken reports whether a new account and its same-named context
// could not both be written under this name.
func accountNameTaken(document contexts.Document, name string) bool {
	return declaresIdentity(document, name) || declaresContext(document, name)
}

// assignedNameNote is the line a report adds under a name the shell assigned:
// that it was assigned, the flag that would have named it, and the command
// that changes it now.
func assignedNameNote(flag, name string) string {
	return fmt.Sprintf("The name %q was assigned. Pass %s <name> to choose one, or run "+
		"wso2 account rename %s <name>.", name, flag, name)
}

// askAccountName is the name a new account is given, and whether the user
// typed it rather than accepting the one the shell assigned.
//
// When nothing may prompt it takes the next free account-N without asking, so
// a script or a CI job never waits on a question nobody will answer. When a
// prompt is allowed it offers that name as the default, and asks again rather
// than refusing when an answer is not a legal name or is already taken: the
// person is still at the terminal, and making them re-run the whole command to
// try another name would cost more than the question did.
func (s Shell) askAccountName(document contexts.Document, noInput bool) (string, bool, error) {
	fallback := nextFreeAccountName(document)
	if may, _ := s.mayPrompt(noInput); !may {
		return fallback, false, nil
	}
	scanner := bufio.NewScanner(s.reader())
	for {
		if _, err := fmt.Fprintf(s.Streams.Err, "Account name [%s]: ", fallback); err != nil {
			return "", false, err
		}
		// End of input is the same as pressing return: the default.
		if !scanner.Scan() {
			return fallback, false, nil
		}
		answer := strings.TrimSpace(scanner.Text())
		var why string
		switch {
		case answer == "":
			return fallback, false, nil
		case !contexts.ValidName(answer):
			why = fmt.Sprintf("%q cannot be used as an account name: a name is %s.", answer, contexts.NameRule)
		case accountNameTaken(document, answer):
			why = fmt.Sprintf("%q is already taken by an account or a context.", answer)
		default:
			return answer, true, nil
		}
		if _, err := fmt.Fprintln(s.Streams.Err, why); err != nil {
			return "", false, err
		}
	}
}
