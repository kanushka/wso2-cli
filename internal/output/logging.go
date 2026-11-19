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
//
// It has two destinations. The diagnostic stream receives everything once
// Enable is called. A file attached with AttachFile receives every Record
// whether or not diagnostics are on, and the diagnostics themselves only once
// they are.
type Logger struct {
	stream slog.Handler
	file   slog.Handler
	// fileLevel admits only records into the file, its zero value being
	// slog.LevelInfo, until Enable lowers it to let diagnostics through.
	fileLevel slog.LevelVar
	// fileOnly writes to the file alone, for what the stream already showed
	// in its own form.
	fileOnly *slog.Logger
	delegate *slog.Logger
	// fileErr is why no file could be attached, written once diagnostics are
	// on so that a missing log is explained to the one person who looks.
	fileErr error
}

// NewLogger builds the log for one invocation. It writes nothing until Enable
// is called, because the flag that turns it on is parsed after the shell has
// already built the command tree that the call sites hang off.
func NewLogger() *Logger {
	return &Logger{}
}

// Enable starts writing diagnostics to w in the given result mode, and to the
// attached file.
//
// The handler follows the result rendering: a caller reading JSON results is
// reading them with a program, and that program's operator wants diagnostics
// they can parse too. The destination is always the diagnostic stream, whatever
// the mode, so enabling this can never corrupt a structured result.
func (l *Logger) Enable(w io.Writer, mode Mode) {
	if l == nil {
		return
	}
	options := &slog.HandlerOptions{Level: slog.LevelDebug}
	if mode == ModeJSON {
		l.stream = diag.Redacting(slog.NewJSONHandler(w, options))
	} else {
		l.stream = diag.Redacting(slog.NewTextHandler(w, options))
	}
	l.fileLevel.Set(slog.LevelDebug)
	l.rebuild()
	if l.fileErr != nil {
		l.Debug("the run log is not being written", "error", l.fileErr.Error())
	}
}

// AttachFile records this invocation in w as JSON lines, every record carrying
// attributes, whether or not diagnostics are ever enabled.
func (l *Logger) AttachFile(w io.Writer, attributes ...any) {
	if l == nil {
		return
	}
	handler := diag.Redacting(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: &l.fileLevel}))
	l.fileOnly = slog.New(handler).With(attributes...)
	l.file = l.fileOnly.Handler()
	l.rebuild()
}

// FileUnavailable records why no file could be attached.
func (l *Logger) FileUnavailable(err error) {
	if l != nil {
		l.fileErr = err
	}
}

func (l *Logger) rebuild() {
	var handlers []slog.Handler
	for _, handler := range []slog.Handler{l.stream, l.file} {
		if handler != nil {
			handlers = append(handlers, handler)
		}
	}
	l.delegate = slog.New(slog.NewMultiHandler(handlers...))
}

// Enabled reports whether diagnostics were asked for. It exists for a call
// site that would have to do work to assemble an attribute it is not going to
// log.
func (l *Logger) Enabled() bool {
	return l != nil && l.stream != nil
}

// Debug records one diagnostic. Every diagnostic the shell logs is at one
// level: the user has either asked to see what happened or has not, and a
// second level would be a second switch with no flag to set it.
func (l *Logger) Debug(message string, attributes ...any) {
	if !l.Enabled() {
		return
	}
	l.delegate.Debug(message, attributes...)
}

// Record writes one of the few facts kept about every run, diagnostics or not:
// to the attached file always, and to the stream once diagnostics are on.
func (l *Logger) Record(message string, attributes ...any) {
	if l == nil || l.delegate == nil {
		return
	}
	l.delegate.Info(message, attributes...)
}

// FileDebug writes a diagnostic to the attached file alone, once diagnostics
// are on, for what the stream already shows in another form.
func (l *Logger) FileDebug(message string, attributes ...any) {
	if !l.Enabled() || l.fileOnly == nil {
		return
	}
	l.fileOnly.Debug(message, attributes...)
}
