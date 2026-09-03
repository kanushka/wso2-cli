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

package preferences

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/wso2/wso2-cli/internal/atomicfile"
	"github.com/wso2/wso2-cli/internal/lockfile"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// lockDeadline bounds how long a writer waits for another invocation to
// finish its read-modify-write. The critical section is a file read, an
// in-memory change, and a file write with no network call inside it, so this
// is short, matching internal/contexts' own deadline for the same shape of
// work.
const lockDeadline = 10 * time.Second

// documentMode is the preferences document's permissions. Neither key this
// document may hold is a credential (the set is closed to rule that out),
// but a configured output mode or catalog origin override is still a detail
// of one user's machine and nobody else's business on a shared one.
const documentMode fs.FileMode = 0o600

// LockPath reports where the preferences document's advisory lock lives.
//
// It sits beside the document rather than under cli/locks, for the same
// reason internal/contexts.LockPath does: that directory is the session
// store's per-credential-reference namespace, and a fixed name placed there
// could collide with a real credentialRef.
func LockPath(stateRoot string) string { return Path(stateRoot) + ".lock" }

// Save writes the document to the state root, atomically and under the
// document lock.
func Save(stateRoot string, document Document) error {
	data, err := encodeReadable(document)
	if err != nil {
		return err
	}
	return withWritableDocument(stateRoot, func() error { return writeDocument(stateRoot, data) })
}

// Update reads the document, applies change, and writes the result back,
// holding the lock across the whole read-modify-write.
//
// The read inside is Load, the lenient reader, not a strict one — but a
// diagnosed fallback refuses the update rather than silently treating the
// document as the zero Document. An earlier version of this comment reasoned
// that withWritableDocument's own guard (refuseFrozenDocument) had already
// agreed the file may be overwritten, so resetting to zero here was just the
// same "too broken to keep, so start fresh" case internal/contexts documents
// for Save. That reasoning missed a case refuseFrozenDocument cannot see:
// refuseFrozenDocument only probes the schemaVersion field, so a document
// that parses as JSON, carries this shell's own current schema version, but
// fails validation on one field — an outputMode a hand edit corrupted, say —
// passes the guard and then fails Load. Resetting to zero there would write
// back a document with only the key change is setting, silently discarding
// every other, perfectly valid field the original held, such as a
// catalogOrigin nobody asked to touch. Only Load's SILENT fallback — a
// genuinely absent file, which carries no diagnostic — is safe to treat as
// the zero Document; anything Load had to diagnose refuses instead.
func Update(stateRoot string, change func(Document) (Document, error)) error {
	return withWritableDocument(stateRoot, func() error {
		current, diagnostic := Load(stateRoot)
		if diagnostic != nil {
			return unreadableForUpdate(*diagnostic)
		}
		next, err := change(current)
		if err != nil {
			return err
		}
		data, err := encodeReadable(next)
		if err != nil {
			return err
		}
		return writeDocument(stateRoot, data)
	})
}

// unreadableForUpdate refuses a read-modify-write when the existing document
// could not be read cleanly, rather than silently treating it as the zero
// Document and overwriting whatever valid fields it held. Save carries no
// equivalent guard because it never reads a "current" value to begin with: a
// full replacement is what Save is for, and a caller invoking it already
// intends to specify every field.
//
// The cause is named by its code, not quoted by its message. Load's
// diagnostic message ends in "so this invocation falls back to default
// preferences", which is true of the read that produced it and false of this
// refusal, which falls back to nothing and writes nothing. Embedding it made
// the user read one sentence twice and be told, the second time, that a
// fallback they had just been refused had happened. The code is the part that
// identifies what is wrong with the document, and it is also what ties this
// refusal to the diagnostic printed just above it.
// UnreadableForUpdate is the same refusal, exported for a command that must
// make it before deciding a write is a no-op: wso2 config unset consults the
// document to skip writing when the key is already unset, and a document Load
// had to diagnose cannot answer that question — reporting "already unset"
// against it would claim knowledge of a file nobody could read, while the
// file stayed exactly as broken as before (review on #161).
func UnreadableForUpdate(cause problem.Problem) error {
	return unreadableForUpdate(cause)
}

func unreadableForUpdate(cause problem.Problem) error {
	return preferenceProblem("preferences.document_unreadable_for_update",
		fmt.Sprintf("the existing WSO2 CLI preferences document could not be read cleanly (%s), "+
			"so it cannot be safely changed without risking the fields already in it", cause.Code),
		"Correct the preferences document, or remove it to start a fresh one, then retry.")
}

// encodeReadable renders the document and proves the result reads back,
// closing the gap between the in-memory value and the bytes that would
// otherwise reach disk unnoticed.
func encodeReadable(document Document) ([]byte, error) {
	data, err := document.Encode()
	if err != nil {
		return nil, err
	}
	if _, err := Decode(data); err != nil {
		return nil, err
	}
	return data, nil
}

// writeDocument replaces the document on disk in one step. The directory is
// created at 0700 because the document inside it is 0600.
func writeDocument(stateRoot string, data []byte) error {
	path := Path(stateRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return documentUnwritable(err)
	}
	if err := atomicfile.Write(path, data, documentMode); err != nil {
		return documentUnwritable(errors.Unwrap(err))
	}
	return nil
}

// withWritableDocument runs fn while holding the document lock, having first
// proved that what is on disk may be overwritten at all. See
// internal/contexts.withWritableDocument, whose four-layer shape (lock the
// whole read-modify-write, allowlist the schema version, encode-then-decode
// before writing, atomicfile.Write) this copies exactly; only the document
// and its problem codes differ.
func withWritableDocument(stateRoot string, fn func() error) error {
	err := lockfile.With(LockPath(stateRoot), lockDeadline, func() error {
		if err := refuseFrozenDocument(stateRoot); err != nil {
			return err
		}
		return fn()
	})
	if errors.Is(err, lockfile.ErrBusy) {
		return preferenceProblem("preferences.document_busy",
			"another WSO2 CLI invocation is updating the preferences document",
			"Retry the command.")
	}
	var lockErr lockfile.Error
	if errors.As(err, &lockErr) {
		return preferenceProblem("preferences.document_unwritable",
			"the shell could not take the preferences document update lock",
			"Check that the WSO2 CLI state directory is writable, then retry the command.")
	}
	return err
}

// refuseFrozenDocument refuses to overwrite a document whose schema version
// this shell does not write.
//
// The rule is the same allowlist internal/contexts.refuseFrozenDocument
// applies, kept unchanged on purpose (R9): a denylist naming version 1 would
// let a write silently destroy a document a newer CLI wrote and this shell
// cannot understand. An absent file is a first write. A file that cannot be
// parsed at all has no version to honour, and is one a write is allowed to
// replace — recoverable is the whole point of a preference, doubly so here.
// The current version is what this shell is for. Anything else is refused.
func refuseFrozenDocument(stateRoot string) error {
	path := Path(stateRoot)
	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		return nil
	case err != nil:
		return preferenceProblem("preferences.document_unreadable",
			"the WSO2 CLI preferences document cannot be read",
			"Check that the preferences document is readable, or remove it to write a fresh one.")
	}
	var probe struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if jsonErr := json.NewDecoder(bytes.NewReader(data)).Decode(&probe); jsonErr != nil {
		return nil
	}
	if probe.SchemaVersion == SchemaVersion {
		return nil
	}
	return preferenceProblem("preferences.document_frozen",
		fmt.Sprintf("the WSO2 CLI preferences document at %s is schema version %d, which this shell does not write",
			path, probe.SchemaVersion),
		fmt.Sprintf("This shell writes schema version %d. Move the file aside to write a fresh one.", SchemaVersion))
}

// documentUnwritable reports that the shell could not write the document,
// for a filesystem reason rather than because the document was wrong.
func documentUnwritable(cause error) error {
	message := "the WSO2 CLI preferences document could not be written"
	if cause != nil {
		message += ": " + cause.Error()
	}
	return preferenceProblem("preferences.document_unwritable", message,
		"Check that the WSO2 CLI state directory is writable, then retry the command.")
}
