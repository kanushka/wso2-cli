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
	"errors"
	"io"
	"os"

	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/internal/wizard"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// The reasons mayPrompt gives for refusing, shared verbatim with
// resolveClientID's own refusal so a login's client-ID prompt and a
// destructive command's confirmation are recognisable as the same rule
// firing rather than two rules that happen to agree today.
const (
	reasonNoInputFlag  = "--no-input asked that nothing prompt"
	reasonNoInputEnv   = NoInputEnvVar + " asked that nothing prompt"
	reasonNotATerminal = "standard input is not a terminal, so nothing can be asked"
)

// reader is where a prompt reads its answer from.
//
// It defaults to the process's real standard input, which is also what
// cmd/wso2/main.go sets Shell.Reader to explicitly. A Shell built directly —
// every test in this package does that — leaves Reader nil, and this treats
// that exactly like os.Stdin: the safe, ordinary default, not a hole to work
// around, the same way a nil s.log is.
func (s Shell) reader() io.Reader {
	if s.Reader != nil {
		return s.Reader
	}
	return os.Stdin
}

// nonInteractiveControl reports which of --no-input or WSO2_NO_INPUT asked
// that nothing run interactively, or the empty string when neither did.
//
// It is deliberately narrower than mayPrompt: it says whether a flow may
// wait on a human at all, not whether an answer can be read back from
// standard input. wso2 login's browser and device flows ask exactly this and
// nothing more — both wait on a person without ever reading this process's
// own stdin, so a terminal on that descriptor is not what either needs.
func (s Shell) nonInteractiveControl(noInput bool) string {
	if noInput {
		return "--no-input"
	}
	if os.Getenv(NoInputEnvVar) != "" {
		return NoInputEnvVar
	}
	return ""
}

// mayPrompt decides whether a question may be put to standard input and read
// back, and reports which control refused it when it may not. It is the one
// place --no-input, WSO2_NO_INPUT, and a terminal check are consulted
// together for that decision, checked in that order: the flag is what the
// person running the command can see and drop, the environment variable is
// not, and a variable set in a shell profile months ago is otherwise a
// refusal with nothing in it to search for.
//
// The terminal check applies only when the shell is reading from the
// process's own standard input. A Shell reading from anything else has been
// handed that reader on purpose — the seam Shell.Reader exists for — and this
// process has no way to fabricate a real terminal to satisfy the check with,
// so a reader that is not os.Stdin is trusted to be exactly what the caller
// intended it to be. That trust is only as good as who gets to assign
// Shell.Reader in the first place. internal/boundaries pins it: a test there
// (TestShellReaderIsAssignedOnlyInCmdWso2) parses every non-test file that
// can name this type and reports both an assignment to a Reader field and a
// Reader key in a composite literal, so cmd/wso2/main.go, which sets it to
// os.Stdin, is the only non-test site that does — a checked fact rather than
// merely a hope.
func (s Shell) mayPrompt(noInput bool) (bool, string) {
	switch s.nonInteractiveControl(noInput) {
	case "--no-input":
		return false, reasonNoInputFlag
	case NoInputEnvVar:
		return false, reasonNoInputEnv
	}
	if s.reader() == io.Reader(os.Stdin) && !output.StdinIsTerminal() {
		return false, reasonNotATerminal
	}
	return true, ""
}

// promptTUIOffEnvVar, when set to anything, keeps the shell's questions in
// their line-oriented form even on a terminal: for a screen reader, or a
// terminal that draws forms badly.
const promptTUIOffEnvVar = "WSO2_ACCESSIBLE"

// prompter is how this shell asks a question once mayPrompt has allowed it.
// It writes to s.Streams.Err, never Out: a prompt is a diagnostic, not a
// result, and must never corrupt --output json. It draws a form only when
// both ends are a real terminal with a size; a reader handed in through
// Shell.Reader, a pipe, a redirected standard error, or an unsized terminal
// gets line prompts.
//
// The wizard is handed standard error unwrapped from the shell's invoked
// name (output.Named): the form library finds the terminal only through the
// real file, and draws nothing through the wrapper.
func (s Shell) prompter() wizard.Prompter {
	errOut := output.Unnamed(s.Streams.Err)
	tui := s.reader() == io.Reader(os.Stdin) &&
		output.StdinIsTerminal() &&
		output.IsTerminal(errOut) &&
		wizard.Drawable(errOut) &&
		os.Getenv(promptTUIOffEnvVar) == ""
	return wizard.New(s.reader(), errOut, tui)
}

// confirm asks a yes/no question whose default is no, decided by
// isAffirmative.
func (s Shell) confirm(title string) (bool, error) {
	yes, err := s.prompter().Confirm(title, false)
	return yes, cancelled(err)
}

// choose asks a question with numbered options and returns the index of the
// option picked. Pressing return takes fallback, which must be available.
func (s Shell) choose(question string, options []wizard.Option, fallback int) (int, error) {
	picked, err := s.prompter().Select(question, options, fallback)
	return picked, cancelled(err)
}

// ask reads one answer, checked with validate and asked again when refused.
// An empty answer takes fallback. End of input with no fallback is
// wizard.ErrNoAnswer, which the caller turns into its own refusal.
func (s Shell) ask(title, fallback string, validate func(string) error) (string, error) {
	answer, err := s.prompter().Input(title, fallback, validate)
	return answer, cancelled(err)
}

// cancelled turns a question the person cancelled into the shell's refusal
// for it. Nothing has been written by then: every command asks before it
// changes anything.
func cancelled(err error) error {
	if errors.Is(err, wizard.ErrAborted) {
		return problem.New(problem.CategoryUsage, "shell.cancelled", "cancelled at a prompt").
			WithRecovery("Nothing was written. Run the command again to start over.")
	}
	return err
}

// isAffirmative is the whole of this shell's consent predicate: the one line
// standing in front of an irreversible os.RemoveAll or an unbounded update.
// It is wizard.Affirmative; prompt_internal_test.go table-tests it here,
// where the commands it guards live.
func isAffirmative(line string) bool {
	return wizard.Affirmative(line)
}
