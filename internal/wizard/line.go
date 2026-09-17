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

package wizard

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// linePrompter asks with numbered, line-oriented prompts.
type linePrompter struct {
	in  io.Reader
	out io.Writer
}

func (p linePrompter) Select(title string, options []Option, fallback int) (int, error) {
	if _, err := fmt.Fprintln(p.out, title); err != nil {
		return 0, err
	}
	for index, option := range options {
		if _, err := fmt.Fprintf(p.out, "  %d. %s\n", index+1, option.Label); err != nil {
			return 0, err
		}
	}
	for {
		if _, err := fmt.Fprintf(p.out, "Choose [%d]: ", fallback+1); err != nil {
			return 0, err
		}
		answer, ok, err := p.readLine()
		if err != nil {
			return 0, err
		}
		if !ok || answer == "" {
			return fallback, nil
		}
		picked, convErr := strconv.Atoi(answer)
		var why string
		switch {
		case convErr != nil || picked < 1 || picked > len(options):
			why = fmt.Sprintf("Enter a number from 1 to %d.", len(options))
		case options[picked-1].Unavailable != "":
			why = options[picked-1].Unavailable
		default:
			return picked - 1, nil
		}
		if _, err := fmt.Fprintln(p.out, why); err != nil {
			return 0, err
		}
	}
}

func (p linePrompter) Input(title, fallback string, validate func(string) error) (string, error) {
	prompt := title + ": "
	if fallback != "" {
		prompt = fmt.Sprintf("%s [%s]: ", title, fallback)
	}
	for {
		if _, err := fmt.Fprint(p.out, prompt); err != nil {
			return "", err
		}
		// A read that failed is not the end of input, or a broken terminal
		// would answer with the default.
		answer, ok, err := p.readLine()
		if err != nil {
			return "", err
		}
		if answer == "" {
			answer = fallback
		}
		refusal := check(validate, answer)
		if !ok {
			if answer == "" || refusal != nil {
				return "", ErrNoAnswer
			}
			return answer, nil
		}
		if refusal == nil {
			return answer, nil
		}
		if _, err := fmt.Fprintln(p.out, refusal.Error()); err != nil {
			return "", err
		}
	}
}

func (p linePrompter) Confirm(title string, fallback bool) (bool, error) {
	hint := "[y/N]"
	if fallback {
		hint = "[Y/n]"
	}
	if _, err := fmt.Fprintf(p.out, "%s %s ", title, hint); err != nil {
		return false, err
	}
	answer, ok, err := p.readLine()
	if err != nil {
		return false, err
	}
	if !ok || answer == "" {
		return fallback, nil
	}
	return Affirmative(answer), nil
}

// readLine reads one line, trimmed, and reports false at end of input with
// nothing read.
//
// It reads a byte at a time rather than through a bufio.Scanner, because a
// scanner reads ahead and a wizard asks several questions in a row: the
// answers to the later ones would be swallowed by the first question's
// buffer and lost when it is dropped.
func (p linePrompter) readLine() (string, bool, error) {
	var line []byte
	var single [1]byte
	for {
		n, err := p.in.Read(single[:])
		if n == 1 {
			if single[0] == '\n' {
				return strings.TrimSpace(string(line)), true, nil
			}
			line = append(line, single[0])
			continue
		}
		if err == io.EOF {
			return strings.TrimSpace(string(line)), len(line) > 0, nil
		}
		if err != nil {
			return "", false, err
		}
	}
}

// check runs validate on answer, treating a nil validate as accepting
// anything but an empty answer.
func check(validate func(string) error, answer string) error {
	if validate != nil {
		return validate(answer)
	}
	if answer == "" {
		return errEmpty
	}
	return nil
}

const errEmpty = Hint("Enter a value.")

// Affirmative is the whole of the shell's consent predicate. Only "y" or
// "yes", checked case-insensitively after trimming surrounding whitespace,
// count as yes. Every other line is a no, because a question guarding an
// action nothing can undo must fail closed on ambiguity.
func Affirmative(line string) bool {
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}
