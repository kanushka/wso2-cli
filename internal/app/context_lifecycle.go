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

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// The way back from delete's and rename's usage refusals.
const (
	contextDeleteUsage = "Run wso2 context delete <name> [--dry-run]."
	contextRenameUsage = "Run wso2 context rename <name> <new-name>."
)

func (s Shell) contextDeleteCommand() *cobra.Command {
	var dryRun bool
	command := &cobra.Command{
		Use:               "delete <name>",
		ValidArgsFunction: s.completeFirstContextName,
		Short:             "Delete a context, ending every session it holds first.",
		Args:              exactlyOneArgument("the name of the context to delete", contextDeleteUsage),
		RunE: func(command *cobra.Command, args []string) error {
			return s.contextDelete(command, args[0], dryRun)
		},
	}
	command.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be deleted and ended, and change nothing.")
	return command
}

func (s Shell) contextRenameCommand() *cobra.Command {
	return &cobra.Command{
		Use:               "rename <name> <new-name>",
		ValidArgsFunction: s.completeFirstContextName,
		Short:             "Rename a context. Its sessions stay where they are.",
		Args:              exactlyTwoArguments("the context's name and its new name", contextRenameUsage),
		RunE: func(command *cobra.Command, args []string) error {
			return s.contextRename(command, args[0], args[1])
		},
	}
}

// contextDelete removes one context after ending every session under its
// credential reference: the login session and each product's and gateway's.
//
// The sessions go first. If the write then fails, the context is still there
// without a session, which a login fixes; the reverse order would leave
// secrets in the secure store that no document names and no command can find.
func (s Shell) contextDelete(command *cobra.Command, name string, dryRun bool) error {
	mode, err := s.shellOutputMode(command)
	if err != nil {
		return err
	}
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	selected := false
	plan, err := s.planChange(root, func(document contexts.Document) (contexts.Document, error) {
		if !declaresContext(document, name) {
			return document, contextNotFound(name)
		}
		selected = document.DefaultContext == name
		return document.Without(name), nil
	})
	if err != nil {
		return err
	}
	report := contextDeleted{Context: name, WasSelected: selected, DryRun: dryRun,
		Ending: endingLines(plan.before, plan.ending), Sessions: []endedSession{}}
	if !dryRun {
		ended, err := s.writeChange(root, plan)
		if err != nil {
			return err
		}
		if ended != nil {
			report.Sessions = ended
		}
	}
	if mode == output.ModeJSON {
		return renderContext(s.Streams.Out, mode, report)
	}
	verb := "Deleted"
	if dryRun {
		verb = "Would delete"
	}
	if _, err := fmt.Fprintf(s.Streams.Out, "\n%s the %q context.\n\n", verb, name); err != nil {
		return err
	}
	if err := renderContext(s.Streams.Out, mode, report); err != nil {
		return err
	}
	for _, note := range endedNotes(report.Sessions) {
		if _, err := fmt.Fprintf(s.Streams.Out, "\n%s\n", note); err != nil {
			return err
		}
	}
	return nil
}

// contextRename changes a context's name and nothing else. The credential
// reference is a stable identifier that never follows the name, so no stored
// session moves and no login is needed.
func (s Shell) contextRename(command *cobra.Command, name, renamed string) error {
	mode, err := s.shellOutputMode(command)
	if err != nil {
		return err
	}
	if !contexts.ValidName(renamed) {
		return problem.New(problem.CategoryUsage, "shell.invalid_argument",
			fmt.Sprintf("%q cannot be used as a context name", renamed)).
			WithRecovery(fmt.Sprintf("A context name is %s. %s", contexts.NameRule, contextRenameUsage))
	}
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	selected := false
	err = contexts.Update(root, func(document contexts.Document) (contexts.Document, error) {
		target, found := document.Find(name)
		if !found {
			return document, contextNotFound(name)
		}
		if declaresContext(document, renamed) {
			return document, contextExists(renamed)
		}
		selected = document.DefaultContext == name
		document = document.Without(name)
		target.Name = renamed
		document = document.Put(target)
		if selected {
			document.DefaultContext = renamed
		}
		return document, nil
	})
	if err != nil {
		return s.explainWriteRefusal(root, err)
	}
	report := contextRenamed{Context: renamed, Previous: name, Selected: selected}
	if mode == output.ModeJSON {
		return renderContext(s.Streams.Out, mode, report)
	}
	if _, err := fmt.Fprintf(s.Streams.Out, "\nRenamed the %q context to %q. Its sessions are unchanged.\n",
		name, renamed); err != nil {
		return err
	}
	return nil
}

// contextNotFound refuses a name the document does not declare.
func contextNotFound(name string) problem.Problem {
	return problem.New(problem.CategoryUsage, "contexts.unknown_context",
		fmt.Sprintf("no context named %q is configured", name)).
		WithRecovery("Run wso2 context list to see the configured contexts.")
}

// The results delete and rename report.
type (
	contextDeleted struct {
		Context     string         `json:"context"`
		WasSelected bool           `json:"wasSelected"`
		DryRun      bool           `json:"dryRun"`
		Ending      []string       `json:"ending"`
		Sessions    []endedSession `json:"sessions"`
	}
	contextRenamed struct {
		Context  string `json:"context"`
		Previous string `json:"previous"`
		Selected bool   `json:"selected"`
	}
)

func (c contextDeleted) fields() [][2]string {
	return [][2]string{
		{"Context", c.Context},
		{"Was selected", yesNo(c.WasSelected)},
		{"Sessions ending", sessionsCell(c.Ending, c.Sessions, c.DryRun)},
	}
}

func (c contextDeleted) next() string {
	if c.WasSelected && !c.DryRun {
		return "No context is selected now. Run wso2 context use <name> to select one."
	}
	return ""
}

func (c contextRenamed) fields() [][2]string {
	return [][2]string{
		{"Context", c.Context},
		{"Previous name", c.Previous},
		{"Selected", yesNo(c.Selected)},
	}
}
