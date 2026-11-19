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

// Package diag is the diagnostic log the shell and its modules share: the
// switch that turns it on, and the redaction every record passes through on
// its way to standard error or the shell's log file.
//
// It lives in the SDK rather than the shell so that one list of secret
// attribute names covers both sides of the module boundary. See
// docs/adr/0026-debug-logging-from-the-environment-and-a-run-log.md.
//
// Redaction masks an attribute by its key, wherever in a record it sits, and
// inside every string or error value it masks the secret parameters of any
// URL and any JWT. It does not read the record's message, and it cannot
// recognise a secret that has neither a telling name nor one of those shapes,
// so a call site still owns what free text it logs.
package diag

import (
	"context"
	"io"
	"log/slog"
	"strings"
)

// EnvVar turns diagnostics on for the shell and for every module it launches.
// The shell sets it for a module whenever its own diagnostics are on, whether
// --verbose or the variable asked.
const EnvVar = "WSO2_DEBUG"

// Requested reports whether a value of EnvVar asks for diagnostics: anything
// but empty, 0 or false. The two spellings of "no" are honoured because a
// variable exported as WSO2_DEBUG=0 to switch it off reads as off to anyone
// who sees it.
func Requested(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "0", "false":
		return false
	}
	return true
}

// Redacted replaces a sensitive attribute value. It is written rather than
// dropped so that a reader can tell "the value was here and hidden" from "there
// was no value", which is the difference between a redaction and a bug.
const Redacted = "[redacted]"

// sensitiveKeys are the attribute names whose values never reach a log, keyed
// by their normalized spelling. Each one is something the shell or a module
// actually holds:
//
//   - accesstoken, refreshtoken, idtoken: the three token fields of a stored
//     session and of an issuer's token response.
//   - token: the bare spelling used by the token and introspection endpoints,
//     by the fixture token the broker mints, and by the access a module is
//     handed.
//   - clientsecret: the secret a client-credentials account presents.
//   - code, devicecode, usercode: the authorization code of a browser login
//     and RFC 8628's two device-flow codes, each exchangeable for a token
//     until it is used.
//   - codeverifier: the PKCE verifier, which is what stops a stolen
//     authorization code from being redeemed.
//   - authorization: the request header a bearer token travels in.
//   - credential, sourcecredential: the shell-owned credential the broker
//     signs a fixture token with, which a module must never see.
//   - password, secret, apikey: held by product modules (a gateway's API key,
//     a user's initial password), and cheap to name before a call site needs
//     them.
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

// Sensitive reports whether an attribute named key is masked, whatever its
// spelling: accessToken, access_token and Access-Token are one key.
func Sensitive(key string) bool {
	return sensitiveKeys[normalizeKey(key)]
}

// New returns a logger writing every record to w through redaction, as JSON
// lines when json is true and as slog text otherwise.
func New(w io.Writer, json bool) *slog.Logger {
	options := &slog.HandlerOptions{Level: slog.LevelDebug}
	var handler slog.Handler = slog.NewTextHandler(w, options)
	if json {
		handler = slog.NewJSONHandler(w, options)
	}
	return slog.New(Redacting(handler))
}

// Discard returns a logger that writes nothing, for a caller whose
// diagnostics are off: a call site logs unconditionally and never has to ask.
func Discard() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// Redacting wraps a handler so every attribute named for a secret is masked
// before inner sees it.
//
// It wraps rather than riding on slog.HandlerOptions.ReplaceAttr because
// ReplaceAttr does not reach every attribute: slog calls it for the leaves
// inside a group but never for the group attribute itself, so
// slog.Group("token", "value", secret) would reach the stream intact.
func Redacting(inner slog.Handler) slog.Handler {
	return redactingHandler{inner: inner}
}

type redactingHandler struct {
	inner slog.Handler
	// masked records that an enclosing group was itself named for something
	// secret. Everything below such a group is masked whatever its own key is.
	masked bool
}

func (h redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h redactingHandler) Handle(ctx context.Context, record slog.Record) error {
	// Rebuilt rather than edited: slog.Record shares its backing storage with
	// copies of itself, so editing in place would reach a record another
	// handler is holding.
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
		masked: h.masked || Sensitive(name),
	}
}

func (h redactingHandler) redact(attribute slog.Attr) slog.Attr {
	// Resolved first, so that a value that computes itself is inspected as what
	// it will actually print.
	attribute.Value = attribute.Value.Resolve()
	if h.masked || Sensitive(attribute.Key) {
		return slog.String(attribute.Key, Redacted)
	}
	switch attribute.Value.Kind() {
	case slog.KindString:
		return slog.String(attribute.Key, maskText(attribute.Value.String()))
	case slog.KindAny:
		// An error renders as its text, so it is masked as text.
		if err, ok := attribute.Value.Any().(error); ok {
			return slog.String(attribute.Key, maskText(err.Error()))
		}
		return attribute
	case slog.KindGroup:
	default:
		return attribute
	}
	group := attribute.Value.Group()
	redacted := make([]slog.Attr, 0, len(group))
	for _, nested := range group {
		redacted = append(redacted, h.redact(nested))
	}
	return slog.Attr{Key: attribute.Key, Value: slog.GroupValue(redacted...)}
}

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
