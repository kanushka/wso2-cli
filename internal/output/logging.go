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
	"log/slog"

	"github.com/wso2/wso2-cli/sdk/diag"
)

// The diagnostic log lives in this package rather than beside it because this
// package is the shell's one owner of user-facing bytes: standard output
// carries the result and standard error carries diagnostics, and a log is a
// diagnostic. A second package writing to Streams.Err would be a second owner
// of the same stream, with its own idea of when a byte may be written there,
// which is exactly what docs/adr/0003-shell-owned-output.md rules out.
//
// Redaction is the SDK's, so the shell and every module mask the same
// attribute names; see sdk/diag.

// Logger is the shell's diagnostic log for one invocation.
//
// Its zero value and a nil pointer write nothing, which is what --verbose being
// absent means: a call site logs unconditionally and the flag decides whether
// anything is emitted, so no caller has to ask whether logging is on.
type Logger struct {
	delegate *slog.Logger
}

// NewLogger builds the log for one invocation. It writes nothing until Enable
// is called, because the flag that turns it on is parsed after the shell has
// already built the command tree that the call sites hang off.
func NewLogger() *Logger {
	return &Logger{}
}

// Enable starts writing diagnostics to w in the given result mode.
//
// The handler follows the result rendering: a caller reading JSON results is
// reading them with a program, and that program's operator wants diagnostics
// they can parse too. The destination is always the diagnostic stream, whatever
// the mode, so enabling this can never corrupt a structured result.
func (l *Logger) Enable(w io.Writer, mode Mode) {
	if l == nil {
		return
	}
	l.delegate = diag.New(w, mode == ModeJSON)
}

// Enabled reports whether anything would be written. It exists for a call site
// that would have to do work to assemble an attribute it is not going to log.
func (l *Logger) Enabled() bool {
	return l != nil && l.delegate != nil
}

// Debug records one diagnostic. Everything the shell logs is at one level: the
// user has either asked to see what happened or has not, and a second level
// would be a second switch with no flag to set it.
func (l *Logger) Debug(message string, attributes ...any) {
	if !l.Enabled() {
		return
	}
	l.delegate.Debug(message, attributes...)
}
