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
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/sdk/problem"
)

const identityRenameUsage = "Run wso2 account rename <account> <new-name>."

// identityRenameCommand renames an account.
//
// It matters because a new account is named account-N unless someone names it
// (#175), and a numbered name says nothing about the deployment behind it.
func (s Shell) identityRenameCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "rename <account> <new-name>",
		Short: "Rename an account, and every context that uses it.",
		Args:  exactlyTwoArguments("the account and its new name", identityRenameUsage),
		RunE: func(command *cobra.Command, args []string) error {
			return s.identityRename(command, args[0], args[1])
		},
	}
}

// identityRename renames the account and repoints every context naming it.
//
// Two things keep their names. The account's credential reference names its
// secure-store entries — the login session and every product session derived
// from it — and the store cannot list what it holds, so an entry moved and
// then lost could never be found to revoke. Leaving the reference where it is
// moves nothing, and the reference and the name were only ever equal by
// convention. And a context is a separate handle with its own name, which a
// user may have typed into scripts, so a same-named one is left as it is and
// the report says so.
func (s Shell) identityRename(command *cobra.Command, from, to string) error {
	mode, err := s.shellOutputMode(command)
	if err != nil {
		return err
	}
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	// Checked before the document is opened, for the reason add-product
	// checks its namespace there: a name that never reached the file must not
	// be reported as a malformed file.
	if !contexts.ValidName(to) {
		return problem.New(problem.CategoryUsage, "shell.invalid_argument",
			fmt.Sprintf("%q cannot be used as an account name", to)).
			WithRecovery(fmt.Sprintf("An account name is %s. %s", contexts.NameRule, identityRenameUsage))
	}
	if err := contexts.Writable(root); err != nil {
		return s.explainWriteRefusal(root, err)
	}

	s.log.Debug("renaming an account", "identity", from, "to", to, "document", contexts.Path(root))

	renamed := accountRenamed{From: from, To: to, Contexts: []string{}}
	err = contexts.Update(root, func(document contexts.Document) (contexts.Document, error) {
		position := slices.IndexFunc(document.Accounts, func(candidate contexts.Account) bool {
			return candidate.Name == from
		})
		if position < 0 {
			return document, unknownIdentity(from, len(document.Accounts) > 0)
		}
		if declaresIdentity(document, to) {
			return document, identityExists(to)
		}
		document.Accounts[position].Name = to
		for index, context := range document.Contexts {
			if context.Account == from {
				document.Contexts[index].Account = to
				renamed.Contexts = append(renamed.Contexts, context.Name)
			}
		}
		return document, nil
	})
	if err != nil {
		return s.explainWriteRefusal(root, err)
	}

	if mode == output.ModeJSON {
		return renderContext(s.Streams.Out, mode, renamed)
	}
	if _, err := fmt.Fprintf(s.Streams.Out, "\nRenamed account %q to %q.\n\n", from, to); err != nil {
		return err
	}
	if err := renderContext(s.Streams.Out, mode, renamed); err != nil {
		return err
	}
	if !slices.Contains(renamed.Contexts, from) {
		return nil
	}
	_, err = fmt.Fprintf(s.Streams.Out, "\nThe context %q keeps its name. Run wso2 context create %s "+
		"--account %s to add one under the new name.\n", from, to, to)
	return err
}

// accountRenamed is what wso2 account rename reports: the two names, and the
// contexts that now use the new one.
type accountRenamed struct {
	From     string   `json:"from"`
	To       string   `json:"to"`
	Contexts []string `json:"contexts"`
}

func (r accountRenamed) fields() [][2]string {
	return [][2]string{
		{"From", r.From},
		{"To", r.To},
		{"Contexts", strings.Join(r.Contexts, ",")},
	}
}
