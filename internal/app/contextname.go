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
	"fmt"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/wizard"
)

// assignedNamePrefix is what a name the shell assigns starts with. A context
// is a saved setup, not a person and not the software it logs in to, so the
// name the shell gives one says nothing about either (#175): naming it after
// the provider named the software, and two deployments of it collided.
const assignedNamePrefix = "context-"

// nextFreeContextName is the lowest context-N no context already holds.
func nextFreeContextName(document contexts.Document) string {
	for number := 1; ; number++ {
		name := fmt.Sprintf("%s%d", assignedNamePrefix, number)
		if !declaresContext(document, name) {
			return name
		}
	}
}

// assignedNameNote is the line a report adds under a name the shell assigned:
// that it was assigned, the flag that would have named it, and the command
// that changes it now.
func assignedNameNote(flag, name string) string {
	return fmt.Sprintf("The name %q was assigned. Pass %s <name> to choose one, or run "+
		"wso2 context rename %s <name>.", name, flag, name)
}

// askContextName is the name a new context is given, and whether the user
// typed it rather than accepting the one the shell assigned.
//
// When nothing may prompt it takes the next free context-N without asking, so
// a script or a CI job never waits on a question nobody will answer. When a
// prompt is allowed it offers that name as the default, and asks again rather
// than refusing when an answer is not a legal name or is already taken: the
// person is still at the terminal, and making them re-run the whole command to
// try another name would cost more than the question did.
func (s Shell) askContextName(document contexts.Document, noInput bool) (string, bool, error) {
	fallback := nextFreeContextName(document)
	if may, _ := s.mayPrompt(noInput); !may {
		return fallback, false, nil
	}
	answer, err := s.ask("Context name", fallback, func(answer string) error {
		switch {
		case !contexts.ValidName(answer):
			return wizard.Hint(fmt.Sprintf("%q cannot be used as a context name: a name is %s.", answer, contexts.NameRule))
		case declaresContext(document, answer):
			return wizard.Hint(fmt.Sprintf("%q is already taken by a context.", answer))
		}
		return nil
	})
	if err != nil {
		return "", false, err
	}
	return answer, answer != fallback, nil
}
