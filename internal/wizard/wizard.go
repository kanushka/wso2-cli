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

// Package wizard asks the shell's interactive questions.
//
// It knows how to ask, never whether to: the shell decides that (mayPrompt in
// internal/app) before it builds a Prompter. A Prompter renders one of two
// ways. On a terminal it draws a form with charmbracelet/huh, where options
// are picked with the arrow keys and an answer is checked as it is typed.
// Everywhere else it prints numbered, line-oriented prompts and reads the
// answer a byte at a time, so scripted input and screen readers get the same
// questions. huh's own accessible mode is not used for that: it builds a
// buffered scanner per question, which swallows the answers to the questions
// after it, and it cannot report end of input.
//
// Every prompt is written to the writer the Prompter is given, which the
// shell sets to standard error: a prompt is a diagnostic, not a result, and
// must never land on standard output and corrupt --output json (ADR 0003).
//
// This is the only package that may import huh; internal/boundaries checks.
package wizard

import (
	"errors"
	"io"
)

// ErrAborted is returned when the person cancels a question, with Ctrl+C or
// Esc on a terminal. Nothing a wizard asked for has been acted on yet.
var ErrAborted = errors.New("the question was cancelled")

// ErrNoAnswer is returned by Input when input ended before an answer was
// given and the question has no default to fall back on.
var ErrNoAnswer = errors.New("input ended before the question was answered")

// Option is one answer Select offers. An option with an Unavailable note is
// listed but cannot be chosen: picking it shows the note and asks again.
type Option struct {
	Label       string
	Unavailable string
}

// Prompter asks one question at a time.
type Prompter interface {
	// Select asks title and returns the index of the option picked. Pressing
	// return, or end of input, takes fallback, which must be available.
	Select(title string, options []Option, fallback int) (int, error)
	// Input asks title and returns the trimmed answer. An empty answer takes
	// fallback when there is one. Every answer, fallback included, is checked
	// with validate when it is non-nil, and a refused one is asked again with
	// the refusal shown. End of input takes fallback, or returns ErrNoAnswer
	// when fallback is empty.
	Input(title, fallback string, validate func(string) error) (string, error)
	// Confirm asks a yes/no question. Only y or yes is a yes, and an empty
	// answer or end of input takes fallback.
	Confirm(title string, fallback bool) (bool, error)
}

// New returns a Prompter that reads from in and writes to out, drawing forms
// when tui is true and printing line prompts otherwise.
func New(in io.Reader, out io.Writer, tui bool) Prompter {
	if tui {
		return formPrompter{in: in, out: out}
	}
	return linePrompter{in: in, out: out}
}

// Hint is a refusal a validate function returns: a sentence shown to the
// person under the question before it is asked again.
type Hint string

func (h Hint) Error() string { return string(h) }
