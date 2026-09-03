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

// Package preferences reads and writes the shell's non-secret, machine-local
// preferences document.
//
// The key set this document may hold is closed (R8, #112): exactly the
// default output mode and the catalog origin override. Each already has a
// higher-precedence source — the --output flag and WSO2_CLI_CATALOG_ORIGIN
// respectively — and this document is always the new, lowest layer above the
// shell's own built-in default. It never overrides something more specific,
// and it must never be read by anything that would let it override
// WSO2_CLI_CATALOG_ORIGIN: that variable exists so the acceptance suite can
// point at a local origin and no test may ever reach the real one, and a
// configuration file with higher precedence than the variable would let a
// developer's saved preference silently redirect the test suite.
//
// A colour preference was in the original design (R8's brief named three
// keys) and was cut before shipping: output.ColorEnabled has zero production
// call sites today, so a key that claimed to govern colour would change
// nothing a user could observe, and a closed set whose members do nothing is
// worse than a smaller, honest one. Colour is the obvious first key to add
// once something in this shell actually renders in it.
//
// This document reads leniently and writes strictly, and the asymmetry is
// deliberate (R9). A lost preference is recoverable in a way a lost context
// is not, so Load falls back to defaults — with a diagnostic, never silently
// — on anything that keeps the file from being read cleanly: absent is normal
// and carries no diagnostic, but corrupt, invalid, or written by a newer CLI
// all do. The write path (Save, Update) keeps the same allowlist
// internal/contexts uses: it must never overwrite a document a newer CLI
// wrote, because this shell cannot know what a version it does not recognise
// means.
package preferences

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/wso2/wso2-cli/sdk/problem"
)

// SchemaVersion is the current preferences-document schema.
const SchemaVersion = 1

// FileName is the preferences document's fixed name inside the shell state
// tree.
const FileName = "preferences.json"

// Key names one of the closed set of preferences this shell reads and
// writes. The set is closed on purpose: naming a key here and switching on it
// everywhere else in this package is what stops wso2 config set from
// becoming a place arbitrary state accumulates and then has to be supported
// forever.
//
// This package emits problem codes under two prefixes, and the rule is what a
// script is matching on. config.unknown_key and config.invalid_value refuse
// what a caller typed at wso2 config, so they are named for the command
// surface that raised them and belong with that family's other refusals; a
// script matching them is handling its own arguments. Everything else this
// package refuses — preferences.document_malformed and its siblings — is
// about the document on disk, which no wso2 config argument chose and which
// every other command reads too, so those are named for the store; a script
// matching them is handling a broken machine. A single prefix would put those
// two unrelated jobs behind one pattern.
type Key string

const (
	// KeyOutputMode governs what --output defaults to when the flag is not
	// given.
	KeyOutputMode Key = "output"
	// KeyCatalogOrigin governs what catalog.Origin returns between
	// WSO2_CLI_CATALOG_ORIGIN and catalog.DefaultOrigin.
	KeyCatalogOrigin Key = "catalog-origin"
)

// Keys lists the closed set, in the order a refusal names them.
func Keys() []Key { return []Key{KeyOutputMode, KeyCatalogOrigin} }

// The values the output-mode key accepts.
//
// These mirror internal/output's Mode ("table", "json") by literal value, not
// by import: this package must not import internal/output, since a future
// colour consumer there would need to import this package the other way, and
// a two-way import would cycle.
const (
	outputModeTable = "table"
	outputModeJSON  = "json"
)

// ParseKey reports whether name is one of the closed set of preference keys.
func ParseKey(name string) (Key, bool) {
	for _, key := range Keys() {
		if string(key) == name {
			return key, true
		}
	}
	return "", false
}

// Document is the shell's preferences store.
type Document struct {
	// SchemaVersion identifies the document format.
	SchemaVersion int `json:"schemaVersion"`
	// OutputMode is the configured default for --output: "table", "json", or
	// empty when not configured.
	OutputMode string `json:"outputMode,omitempty"`
	// CatalogOrigin is the configured catalog origin override, or empty when
	// not configured.
	CatalogOrigin string `json:"catalogOrigin,omitempty"`
}

// Path reports the preferences document's location inside a state root.
func Path(stateRoot string) string {
	return filepath.Join(stateRoot, "cli", FileName)
}

// Get reads the stored value for key and whether one is configured. An unset
// value is not itself a refusal: it is what lets a consumer's own default,
// or the next layer up, take over.
func (d Document) Get(key Key) (value string, set bool) {
	switch key {
	case KeyOutputMode:
		return d.OutputMode, d.OutputMode != ""
	case KeyCatalogOrigin:
		return d.CatalogOrigin, d.CatalogOrigin != ""
	default:
		return "", false
	}
}

// Set returns a copy of the document with key set to value, refusing an
// unknown key or a value that key does not accept.
func (d Document) Set(key Key, value string) (Document, error) {
	switch key {
	case KeyOutputMode:
		if value != outputModeTable && value != outputModeJSON {
			return Document{}, invalidValue(key, value, "table or json")
		}
		d.OutputMode = value
	case KeyCatalogOrigin:
		if !validOrigin(value) {
			return Document{}, invalidValue(key, value, "an absolute http or https URL, such as https://example.com")
		}
		d.CatalogOrigin = value
	default:
		return Document{}, UnknownKey(string(key))
	}
	return d, nil
}

// Unset returns a copy of the document with key cleared, refusing an unknown
// key. Clearing a key that is not set is not a refusal: the caller asked for
// the state the document is already in, and Get already treats "unset" as a
// fact rather than a failure. What clearing restores is not this package's to
// say — the next layer up (a flag, an environment variable, or the consumer's
// built-in default) governs again, exactly as if the key had never been set.
func (d Document) Unset(key Key) (Document, error) {
	switch key {
	case KeyOutputMode:
		d.OutputMode = ""
	case KeyCatalogOrigin:
		d.CatalogOrigin = ""
	default:
		return Document{}, UnknownKey(string(key))
	}
	return d, nil
}

// validOrigin reports whether value is usable as a catalog origin: an
// absolute http or https URL with a host. This is deliberately stricter than
// "has a scheme and a host", the same way internal/catalog's own
// validArtifactURL restricts an artifact URL to http/https rather than
// accepting any scheme a net/url.Parse call happens to tolerate — a typo
// here would silently point every module install this shell runs at the
// wrong place, and a scheme like javascript: or file: is never a catalog
// origin regardless of what URL syntax allows.
func validOrigin(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != ""
}

// Decode parses and strictly validates a preferences document. It is used by
// the write path (through encodeReadable, which proves a document reads back
// before it is written) and by Load, which downgrades any error here to
// defaults with a diagnostic rather than propagating it — see the package
// doc comment on why the two halves differ.
//
// Unknown JSON members are refused, the opposite of internal/contexts, which
// tolerates them on purpose so a newer shell can add non-secret context facts
// within one schema version. That tolerance does not transfer here: this
// store's whole design is a CLOSED key set (R8), so a member this package
// does not define — a hand-added accessToken, say — is not a forward-
// compatibility signal to preserve, it is exactly the kind of drift a closed
// set exists to catch. Silently dropping it on the next write (which
// DisallowUnknownFields prevents) would make the set closed by struct shape
// only, with nothing that actually says so.
func Decode(data []byte) (Document, error) {
	var document Document
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return Document{}, malformed("is not valid JSON, or names a field this closed store does not define")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return Document{}, malformed("contains more than one JSON document")
	}
	if err := document.validate(); err != nil {
		return Document{}, err
	}
	return document, nil
}

// Encode renders the document as the canonical on-disk form, refusing a
// document this shell would not read back.
func (d Document) Encode() ([]byte, error) {
	if err := d.validate(); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("preferences: cannot encode the preferences document: %w", err)
	}
	return append(data, '\n'), nil
}

// validate proves the document is internally consistent before any command
// depends on it, and before it is written.
func (d Document) validate() error {
	if d.SchemaVersion != SchemaVersion {
		return schemaUnsupported(d.SchemaVersion)
	}
	if d.OutputMode != "" && d.OutputMode != outputModeTable && d.OutputMode != outputModeJSON {
		return malformed(fmt.Sprintf("has an invalid output mode %q", d.OutputMode))
	}
	if d.CatalogOrigin != "" && !validOrigin(d.CatalogOrigin) {
		return malformed(fmt.Sprintf("has an invalid catalog origin %q", d.CatalogOrigin))
	}
	return nil
}

// Load reads the preferences document, falling back to defaults on anything
// that keeps it from being read cleanly.
//
// An absent file is normal — most machines have configured nothing — and
// returns defaults with a nil diagnostic. Anything else that stops the
// document being read (a permission error, JSON that does not parse, a
// document that fails validation, or one written by a newer CLI) also falls
// back to defaults, but returns a non-nil diagnostic naming what was wrong.
// The caller — the shell, which owns Streams.Err — is the one with somewhere
// to put it; this package has no diagnostic log of its own to fall back on,
// so a diagnostic dropped here is dropped for good. See the package doc
// comment: this is the lenient half of R9, and it must never be silent.
func Load(stateRoot string) (Document, *problem.Problem) {
	data, err := os.ReadFile(Path(stateRoot))
	switch {
	case os.IsNotExist(err):
		return Document{}, nil
	case err != nil:
		diagnostic := preferenceProblem("preferences.document_unreadable",
			fmt.Sprintf("the WSO2 CLI preferences document could not be read: %s", err.Error()),
			"Falling back to default preferences. Check that the preferences document is "+
				"readable, or remove it to clear this warning.")
		return Document{}, &diagnostic
	}
	document, decodeErr := Decode(data)
	if decodeErr != nil {
		// decodeErr is always a problem.Problem in practice: every return
		// inside Decode and Document.validate constructs one. Its own Code
		// and Recovery are reused rather than collapsed into one generic
		// preferences.document_malformed — a newer-version document (Code
		// preferences.schema_unsupported, Recovery "Update the WSO2 CLI...")
		// is a different fact from a corrupt one (Code
		// preferences.document_malformed, Recovery "Correct the
		// document..."), and telling a user their up-to-date file is
		// "corrupt" sends them chasing the wrong fix. Its own Message is
		// used rather than decodeErr.Error(), which would double-prefix:
		// Problem.Error() already renders "usage: <code>: <message>", so
		// wrapping that whole string inside another "...could not be used:
		// ..." sentence said "document" twice and buried the code inside
		// prose instead of surfacing it as this diagnostic's own Code.
		var typed problem.Problem
		if errors.As(decodeErr, &typed) {
			diagnostic := preferenceProblem(typed.Code,
				fmt.Sprintf("the WSO2 CLI preferences document at %s could not be used, so this "+
					"invocation falls back to default preferences: %s", Path(stateRoot), typed.Message),
				typed.Recovery)
			return Document{}, &diagnostic
		}
		diagnostic := preferenceProblem("preferences.document_malformed",
			fmt.Sprintf("the WSO2 CLI preferences document at %s could not be used: %s",
				Path(stateRoot), decodeErr.Error()),
			"Falling back to default preferences. Correct the document, or remove it to use defaults.")
		return Document{}, &diagnostic
	}
	return document, nil
}

// UnknownKey refuses a key outside the closed set, naming the valid ones.
func UnknownKey(name string) problem.Problem {
	valid := make([]string, len(Keys()))
	for index, key := range Keys() {
		valid[index] = string(key)
	}
	return problem.New(problem.CategoryUsage, "config.unknown_key",
		fmt.Sprintf("%q is not a wso2 config key", name)).
		WithRecovery(fmt.Sprintf("Valid keys: %s.", strings.Join(valid, ", ")))
}

// invalidValue refuses a value a known key does not accept, naming what is.
func invalidValue(key Key, value, acceptable string) problem.Problem {
	return problem.New(problem.CategoryUsage, "config.invalid_value",
		fmt.Sprintf("%q is not a valid value for %q", value, string(key))).
		WithRecovery(fmt.Sprintf("Use %s.", acceptable))
}

// schemaUnsupported refuses a document version this shell does not read.
func schemaUnsupported(version int) problem.Problem {
	return preferenceProblem("preferences.schema_unsupported",
		fmt.Sprintf("preferences document schema version %d is not supported by this shell", version),
		"Update the WSO2 CLI, or remove the preferences document to use defaults.")
}

// malformed reports a document this shell cannot make sense of.
func malformed(detail string) problem.Problem {
	return preferenceProblem("preferences.document_malformed",
		"the WSO2 CLI preferences document "+detail,
		"Correct the preferences document, or remove it to use defaults.")
}

func preferenceProblem(code, message, recovery string) problem.Problem {
	return problem.New(problem.CategoryUsage, code, message).WithRecovery(recovery)
}
