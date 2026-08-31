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
	"io"
	"os"
	"strings"

	"github.com/wso2/wso2-cli/internal/output"
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

// confirm asks a yes/no question on s.Streams.Err — a prompt is a diagnostic,
// not a result, so it must never land on Out and corrupt --output json — and
// reports the answer read from s.reader(), decided by isAffirmative.
func (s Shell) confirm(prompt string) (bool, error) {
	if _, err := fmt.Fprint(s.Streams.Err, prompt); err != nil {
		return false, err
	}
	scanner := bufio.NewScanner(s.reader())
	if !scanner.Scan() {
		// EOF or an empty stream is the same as no answer, which is a no:
		// the ordinary case of a person pressing return with nothing typed,
		// and also what a reader that carries nothing at all produces.
		return false, nil
	}
	return isAffirmative(scanner.Text()), nil
}

// isAffirmative is the whole of this shell's consent predicate: the one line
// standing in front of an irreversible os.RemoveAll or an unbounded update.
// Only "y" or "yes", checked case-insensitively after trimming surrounding
// whitespace, count as yes. Every other line — empty, whitespace-only, "no",
// garbage, or anything that is not one of those two words — is a no, because
// a question guarding an action nothing here can undo must fail closed on
// ambiguity rather than fail open on the first line that looks like consent.
// prompt_internal_test.go table-tests this directly and mutation-proves it in
// both directions, since nothing else in this file would catch a change here.
func isAffirmative(line string) bool {
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}
