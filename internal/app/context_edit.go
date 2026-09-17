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
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// contextEditUsage is the way back from edit's usage refusals.
const contextEditUsage = "Run wso2 context edit."

func (s Shell) contextEditCommand() *cobra.Command {
	var noInput bool
	command := &cobra.Command{
		Use:   "edit",
		Short: "Open the context document in your editor, and write it only if it is still valid.",
		Long: "Opens the complete context document in $VISUAL or $EDITOR. When the editor closes, the " +
			"result is validated as a whole: an invalid document is refused and the file on disk is left " +
			"as it was. Sessions a changed record no longer matches are ended before the write. The " +
			"editor takes complete records only; to write short records with defaults filled in, use " +
			"wso2 context apply -f <file>.",
		Args: noArguments(contextEditUsage),
		RunE: func(command *cobra.Command, args []string) error {
			return s.contextEdit(command, noInput)
		},
	}
	command.Flags().BoolVar(&noInput, "no-input", false, "Refuse rather than open an editor.")
	return command
}

// contextEdit lets a person change the document by hand without ever leaving
// an invalid one behind.
func (s Shell) contextEdit(command *cobra.Command, noInput bool) error {
	if _, err := s.shellOutputMode(command); err != nil {
		return err
	}
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	if s.RunEditor == nil {
		if may, reason := s.mayPrompt(noInput); !may {
			return problem.New(problem.CategoryUsage, "shell.not_interactive",
				"wso2 context edit opens an editor, and "+reason).
				WithRecovery(fmt.Sprintf("Edit %s directly, or apply a file with wso2 context apply -f <file>.",
					contexts.Path(root)))
		}
	}
	if err := contexts.Writable(root); err != nil {
		return s.explainWriteRefusal(root, err)
	}
	document, err := contexts.Load(root)
	if err != nil {
		return err
	}
	if document.SchemaVersion == 0 {
		document.SchemaVersion = contexts.SchemaVersion
	}
	original, err := document.Encode()
	if err != nil {
		return err
	}
	scratch, err := os.CreateTemp("", "wso2-contexts-*.json")
	if err != nil {
		return err
	}
	path := scratch.Name()
	defer func() { _ = os.Remove(path) }()
	if _, err := scratch.Write(original); err != nil {
		_ = scratch.Close()
		return err
	}
	if err := scratch.Close(); err != nil {
		return err
	}

	var edited contexts.Document
	for {
		if err := s.runEditor(path); err != nil {
			return problem.New(problem.CategoryUsage, "shell.editor_failed",
				"the editor did not finish cleanly, so nothing was written").
				WithRecovery("Set VISUAL or EDITOR to an editor that exits with status 0, then retry.")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Equal(bytes.TrimSpace(data), bytes.TrimSpace(original)) {
			_, err := fmt.Fprintln(s.Streams.Out, "\nNo changes; the context document was not written.")
			return err
		}
		edited, err = contexts.Decode(data)
		if err == nil {
			_, err = edited.Encode()
		}
		if err == nil {
			break
		}
		output.Problem(s.Streams.Err, asProblem(err))
		if s.RunEditor != nil {
			return problem.New(problem.CategoryUsage, "contexts.document_malformed",
				"the edited document is not valid, so nothing was written").
				WithRecovery("Run wso2 context edit again and correct it.")
		}
		again, err := s.confirm("Edit again? [y/N] ")
		if err != nil {
			return err
		}
		if !again {
			return problem.New(problem.CategoryUsage, "contexts.document_malformed",
				"the edited document is not valid, so nothing was written").
				WithRecovery("Run wso2 context edit again and correct it.")
		}
	}

	plan, err := s.planChange(root, func(contexts.Document) (contexts.Document, error) { return edited, nil })
	if err != nil {
		return err
	}
	ended, err := s.writeChange(root, plan)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(s.Streams.Out, "\nWrote the context document."); err != nil {
		return err
	}
	if len(ended) > 0 {
		if _, err := fmt.Fprintf(s.Streams.Out, "Sessions ended because their records changed: %s\n",
			sessionsCell(nil, ended, false)); err != nil {
			return err
		}
	}
	for _, note := range endedNotes(ended) {
		if _, err := fmt.Fprintf(s.Streams.Out, "\n%s\n", note); err != nil {
			return err
		}
	}
	return nil
}

// runEditor opens the file in the test seam's editor, or the user's.
func (s Shell) runEditor(path string) error {
	if s.RunEditor != nil {
		return s.RunEditor(path)
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	fields := strings.Fields(editor)
	command := exec.Command(fields[0], append(fields[1:], path)...) //nolint:gosec // the user's own editor
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	return command.Run()
}
