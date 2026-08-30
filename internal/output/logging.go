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
	"context"
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
	}
	var handler slog.Handler = slog.NewTextHandler(w, options)
	if mode == ModeJSON {
		handler = slog.NewJSONHandler(w, options)
	}
	// Redaction wraps the handler rather than riding on
	// slog.HandlerOptions.ReplaceAttr, because ReplaceAttr does not reach every
	// attribute: slog calls it for the leaves inside a group but never for the
	// group attribute itself, so slog.Group("token", "value", secret) would
	// reach the stream with the secret intact. A wrapper sees every attribute
	// before the handler does, whichever way it arrived.
	l.delegate = slog.New(redactingHandler{inner: handler})
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

// redactingHandler masks every attribute whose key names something secret,
// wherever in a record that attribute sits.
//
// It wraps rather than replaces a handler because the rendering is not its
// business: the same redaction has to hold for the text handler a human reads
// and the JSON handler a program reads, and neither should be able to differ
// from the other about what a secret is.
type redactingHandler struct {
	inner slog.Handler
	// masked records that an enclosing group was itself named for something
	// secret. Everything below such a group is masked whatever its own key is,
	// because a group named "token" holding an attribute named "value" is still
	// a token reaching the stream.
	masked bool
}

func (h redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h redactingHandler) Handle(ctx context.Context, record slog.Record) error {
	// The record is rebuilt rather than edited: slog.Record shares its backing
	// storage with copies of itself, so editing attributes in place would reach
	// a record another handler is holding.
	redacted := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	record.Attrs(func(attribute slog.Attr) bool {
		redacted.AddAttrs(h.redact(attribute))
		return true
	})
	return h.inner.Handle(ctx, redacted)
}

func (h redactingHandler) WithAttrs(attributes []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, 0, len(attributes))
	for _, attribute := range attributes {
		redacted = append(redacted, h.redact(attribute))
	}
	return redactingHandler{inner: h.inner.WithAttrs(redacted), masked: h.masked}
}

func (h redactingHandler) WithGroup(name string) slog.Handler {
	return redactingHandler{
		inner:  h.inner.WithGroup(name),
		masked: h.masked || sensitiveKeys[normalizeKey(name)],
	}
}

// redact masks one attribute, recursing into a group whose own name is
// innocent so that a secret nested inside it is still caught.
//
// It is deliberately key-based and therefore only as complete as
// sensitiveKeys. That incompleteness is why the leak test greps real captured
// output for real fixture token values rather than asserting this map's
// contents: a value logged under a name nobody thought of is caught there, not
// here.
func (h redactingHandler) redact(attribute slog.Attr) slog.Attr {
	// Resolved first, so that a value that computes itself is inspected as what
	// it will actually print rather than as the promise to print it.
	attribute.Value = attribute.Value.Resolve()
	if h.masked || sensitiveKeys[normalizeKey(attribute.Key)] {
		return slog.String(attribute.Key, redactedValue)
	}
	if attribute.Value.Kind() != slog.KindGroup {
		return attribute
	}
	group := attribute.Value.Group()
	redacted := make([]slog.Attr, 0, len(group))
	for _, nested := range group {
		redacted = append(redacted, h.redact(nested))
	}
	return slog.Attr{Key: attribute.Key, Value: slog.GroupValue(redacted...)}
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
