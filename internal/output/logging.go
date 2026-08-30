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
	"strings"
)

// The diagnostic log lives in this package rather than beside it because this
// package is the shell's one owner of user-facing bytes: standard output
// carries the result and standard error carries diagnostics, and a log is a
// diagnostic. A second package writing to Streams.Err would be a second owner
// of the same stream, with its own idea of when a byte may be written there,
// which is exactly what docs/adr/0003-shell-owned-output.md rules out.

// redactedValue replaces a sensitive attribute value. It is written rather than
// dropped so that a reader can tell "the shell had a token here and hid it"
// from "the shell never had one", which is the difference between a redaction
// and a bug.
const redactedValue = "[redacted]"

// sensitiveKeys are the attribute names whose values never reach a log, keyed
// by their normalized spelling. Each one is something this codebase actually
// holds:
//
//   - accesstoken, refreshtoken, idtoken: the three token fields of a stored
//     session and of an issuer's token response (internal/auth/session).
//   - token: the bare spelling used by the token and introspection endpoints,
//     and by the fixture token the broker mints (internal/auth/devtoken).
//   - clientsecret: the secret a client-credentials identity presents, read
//     from the environment variable the context document names.
//   - code, devicecode, usercode: the authorization code of a browser login
//     and RFC 8628's two device-flow codes, each exchangeable for a token
//     until it is used.
//   - codeverifier: the PKCE verifier, which is what stops a stolen
//     authorization code from being redeemed.
//   - authorization: the request header a bearer token travels in.
//   - credential, sourcecredential: the shell-owned credential the broker
//     signs a fixture token with, which a module must never see.
//   - password, secret, apikey: not held by any current call site, and listed
//     because the cost of naming them now is a line each, and the cost of
//     leaving them out is discovering the omission from a user's bug report.
var sensitiveKeys = map[string]bool{
	"accesstoken":      true,
	"refreshtoken":     true,
	"idtoken":          true,
	"token":            true,
	"clientsecret":     true,
	"code":             true,
	"devicecode":       true,
	"usercode":         true,
	"codeverifier":     true,
	"authorization":    true,
	"credential":       true,
	"sourcecredential": true,
	"password":         true,
	"secret":           true,
	"apikey":           true,
}

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
	options := &slog.HandlerOptions{
		// Every diagnostic this shell writes is worth seeing once the user has
		// asked for diagnostics at all, so the handler filters nothing and the
		// call sites carry the judgement instead.
		Level: slog.LevelDebug,
		// Redaction is done through ReplaceAttr rather than by wrapping the
		// handler, because the built-in handlers apply it to attributes added
		// through WithAttrs and inside groups as well. A wrapper would have to
		// reimplement that reach, and a redaction that covers only some of the
		// ways an attribute can arrive is worse than none, because it reads as
		// though it covers all of them.
		ReplaceAttr: redactAttr,
	}
	if mode == ModeJSON {
		l.delegate = slog.New(slog.NewJSONHandler(w, options))
		return
	}
	l.delegate = slog.New(slog.NewTextHandler(w, options))
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

// redactAttr masks the value of an attribute whose key names something secret.
//
// It is deliberately key-based and therefore only as complete as
// sensitiveKeys. That incompleteness is why the leak test greps real captured
// output for real fixture token values rather than asserting this map's
// contents: a value logged under a name nobody thought of is caught there, not
// here.
func redactAttr(_ []string, attribute slog.Attr) slog.Attr {
	if sensitiveKeys[normalizeKey(attribute.Key)] {
		return slog.String(attribute.Key, redactedValue)
	}
	return attribute
}

// normalizeKey reduces an attribute name to the spelling sensitiveKeys is
// written in, so that accessToken, access_token, and Access-Token are one key
// and not three chances to miss one.
func normalizeKey(key string) string {
	var normalized strings.Builder
	for _, letter := range strings.ToLower(key) {
		switch letter {
		case '_', '-', '.', ' ':
			continue
		default:
			normalized.WriteRune(letter)
		}
	}
	return normalized.String()
}
