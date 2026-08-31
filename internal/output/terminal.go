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

package output

import (
	"io"
	"os"
)

// IsTerminal reports whether w is attached to a terminal.
//
// A writer that does not expose a file descriptor — every *bytes.Buffer in
// this repository's test suite, and anything else standing in for a real
// stream — is reported as not a terminal. That is the correct default rather
// than an accident: a caller that cannot ask the question behaves as if a
// human is not watching, which is the safer of the two answers.
func IsTerminal(w io.Writer) bool {
	descriptor, ok := w.(interface{ Fd() uintptr })
	if !ok {
		return false
	}
	return isTerminal(descriptor.Fd())
}

// StdinIsTerminal reports whether the process's standard input is attached to
// a terminal.
//
// This is the one stream Streams does not carry (see login_create.go's
// resolveClientID and #86), so a caller that needs to know whether a person
// could answer a prompt asks the process directly rather than through a
// Shell field.
func StdinIsTerminal() bool {
	return isTerminal(os.Stdin.Fd())
}

// ColorEnabled reports whether w should receive ANSI color escapes.
//
// NO_COLOR (https://no-color.org) disables color when the variable is
// "present and not an empty string", so NO_COLOR= (present, empty) does NOT
// disable color and NO_COLOR=1 does. This follows the cited spec exactly
// rather than a stricter presence-only test: NO_COLOR is a cross-tool
// convention, not this repository's own, and a user who sets it expects the
// documented behaviour rather than a variant this shell invented.
//
// This is deliberately inconsistent with WSO2_NO_INPUT, which IS an
// emptiness test (WSO2_NO_INPUT= and WSO2_NO_INPUT=0 both mean "no input").
// That difference is not an oversight: WSO2_NO_INPUT is this repository's
// own variable and this repository's own rule to set, while NO_COLOR is
// not ours to redefine.
//
// With NO_COLOR absent or empty, the answer falls back to whether w is a
// terminal: a piped or redirected stream gets no escape codes even without
// the variable set, because there is nothing there to render them.
func ColorEnabled(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return IsTerminal(w)
}
