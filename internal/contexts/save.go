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

package contexts

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
)

// lockDeadline bounds how long a writer waits for another invocation to finish
// its read-modify-write.
//
// The critical section is a file read, an in-memory change, and a file write,
// with no network call inside it by design (ADR 0011, #112 D8), so this is
// short — two orders of magnitude below the session lock's deadline, which has
// to outlast a token refresh round trip. A holder that has not finished in this
// long is stuck rather than slow, and waiting longer only delays the refusal.
const lockDeadline = 10 * time.Second

// documentMode is the context document's permissions. It names credential
// sources and never holds one, but the identities, issuers and organizations it
// lists are the shape of a deployment and are nobody else's business on a
// shared machine.
const documentMode fs.FileMode = 0o600

// LockPath reports where the context document's advisory lock lives.
//
// It sits beside the document rather than under cli/locks, which is the session
// store's per-credential-reference namespace. A reference is a bare word, so a
// document lock placed there under any fixed name could collide with a real
// identity's lock — an identity whose credentialRef happened to be that word
// would share a lock with the document itself, and a login would serialize
// against an unrelated write. Keying the lock by the document's own path cannot
// collide with anything, because the document has exactly one path.
func LockPath(stateRoot string) string { return Path(stateRoot) + ".lock" }

// Save writes the document to the state root, atomically and under the document
// lock.
//
// The encoded bytes are decoded back through this package's own reader before
// they are written, so the shell cannot write a document it would refuse to
// read. That is the property that makes a new writing command safe to add: a
// writer with a bug produces a refusal, not an unreadable state root.
//
// Writing grants nothing. A context and an identity hold target metadata and
// opaque credential references, and the types have nowhere to put a credential
// even if a writer tried, so this function needs no authority check. See
// docs/adr/0011-writing-a-context-or-identity-grants-nothing.md.
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
// The lock spans the read as well as the write on purpose. Two invocations that
// each read, then each write, would have one silently discard the other's
// context however atomic each individual write was.
//
// A state root with no document yields the zero Document, so the first write on
// a fresh machine is not a special case in every caller. A change that fails
// writes nothing: the document on disk is left exactly as it was found.
func Update(stateRoot string, change func(Document) (Document, error)) error {
	return withWritableDocument(stateRoot, func() error {
		current, err := Load(stateRoot)
		if err != nil {
			return err
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

// encodeReadable renders the document and proves the result reads back.
//
// Encode already validates, so the decode is not a second opinion on the same
// check: it closes the gap between the in-memory value and the bytes, where a
// marshalling defect or a field the reader is stricter about would otherwise
// reach disk unnoticed.
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
// created at 0700 because the document inside it is 0600: a private file in a
// world-readable directory still leaks its own existence and name.
func writeDocument(stateRoot string, data []byte) error {
	path := Path(stateRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		// MkdirAll already reports a path and an operation; nothing wraps it.
		return documentUnwritable(err)
	}
	if err := atomicfile.Write(path, data, documentMode); err != nil {
		// atomicfile names itself and repeats the target path. One unwrap
		// reaches the filesystem error underneath, which is the part a user can
		// act on; unwrapping further would reach a bare errno and lose the path.
		return documentUnwritable(errors.Unwrap(err))
	}
	return nil
}

// withWritableDocument runs fn while holding the document lock, having first
// proved that what is on disk may be overwritten at all.
//
// The refusal belongs here rather than on the way out. Encode refuses a
// document that is itself a compatibility read, which catches an amend-shaped
// change only by accident: a caller that replaces the document rather than
// amending it hands over a clean current-version value with nothing left to
// object to, and the file underneath is destroyed. D13 is a claim about the
// file, so the guard reads the file. Both writers inherit it from one place,
// and so does every writer added after them.
//
// This is a second read of the document in Update, which Loads it again inside
// fn. That is deliberate: the guard decodes one integer and never validates, so
// it cannot refuse a merely corrupt document that a writer ought to be able to
// repair, which a Load-shaped guard would. Both reads happen under the lock, so
// they cannot disagree.
//
// A failure inside fn is neither of the lock's own failures and passes through
// untouched, which is why the conditions are matched by type rather than by
// "err != nil": a refused change must reach the user as the refusal it is, not
// as a broken lock.
func withWritableDocument(stateRoot string, fn func() error) error {
	err := lockfile.With(LockPath(stateRoot), lockDeadline, func() error {
		if err := refuseFrozenDocument(stateRoot); err != nil {
			return err
		}
		return fn()
	})
	if errors.Is(err, lockfile.ErrBusy) {
		return contextProblem("contexts.document_busy",
			"another WSO2 CLI invocation is updating the context document",
			"Retry the command.")
	}
	var lockErr lockfile.Error
	if errors.As(err, &lockErr) {
		return contextProblem("contexts.document_unwritable",
			"the shell could not take the context document update lock",
			"Check that the WSO2 CLI state directory is writable, then retry the command.")
	}
	return err
}

// refuseFrozenDocument refuses to overwrite a document whose schema version
// this shell does not write.
//
// The rule is an allowlist, and that is the point of it. A denylist naming
// version 1 would let through a document written by a newer CLI on the same
// machine — a document Decode refuses to even read, so a writer that destroyed
// it would be doing something no reader in this package is allowed to do. The
// version this shell writes is the only version it may replace.
//
// Three things are not refusals. An absent file is a first write. A file that
// cannot be parsed at all has no version to honour, and a document too broken
// to read is one a create command should be able to replace, so refusing it
// would strand the user with no way back. And the current version is what this
// shell is for.
//
// A read that fails for any reason other than absence is a refusal: the shell
// cannot see what it would be destroying, which is the case where guessing
// costs the most.
//
// Only the version is decoded. Whether a current-version document is otherwise
// valid is Encode's business, checked against the value being written rather
// than against the file being replaced.
func refuseFrozenDocument(stateRoot string) error {
	path := Path(stateRoot)
	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		return nil
	case err != nil:
		// The same code and recovery Load reports for the same condition: the
		// file is there and the shell cannot see it, and neither the cause nor
		// the remedy depends on which of the two was trying to read it.
		return contextProblem("contexts.document_unreadable",
			"the WSO2 CLI context document cannot be read",
			"Check that the context document is readable, or remove it to run without a context.")
	}
	var probe struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&probe); err != nil {
		return nil
	}
	if probe.SchemaVersion == SchemaVersion {
		return nil
	}
	return documentFrozen(path, probe.SchemaVersion)
}

// documentFrozen reports that the document on disk is in a format this shell
// will not overwrite.
//
// It has its own code rather than sharing contexts.document_malformed, which
// means the document was read and is not valid and sends the user to a
// field-by-field reference. A version 1 file has nothing wrong with it, and a
// version this shell has never heard of is not its to judge, so that advice
// helps neither. The separate code is also what lets a command layer above
// catch this one condition and offer the user something better than a refusal,
// without matching on a message.
//
// The message names the version and the path, because a user being told their
// file will not be written needs to know which file, and why.
func documentFrozen(path string, version int) error {
	return contextProblem("contexts.document_frozen",
		fmt.Sprintf("the WSO2 CLI context document at %s is schema version %d, which this shell does not write",
			path, version),
		fmt.Sprintf("This shell writes schema version %d. Move the file aside to write a new document, "+
			"or run the WSO2 CLI version that manages this one.", SchemaVersion))
}

// documentUnwritable reports that the shell could not write the document, for a
// filesystem reason rather than because the document was wrong.
//
// The cause is carried into the message rather than dropped. This package is a
// leaf with no diagnostic log to fall back on, so a cause discarded here is
// discarded for good: no command above can report what it was never handed, and
// a user left with "could not be written" cannot tell a full disk from a
// read-only mount from a permission they can fix. The session lock drops its
// cause, but its one realistic cause is the one its recovery already names.
//
// Nothing here is credential material: a filesystem error carries a path inside
// the user's own state root and an operating system message.
func documentUnwritable(cause error) error {
	message := "the WSO2 CLI context document could not be written"
	if cause != nil {
		message += ": " + cause.Error()
	}
	return contextProblem("contexts.document_unwritable", message,
		"Check that the WSO2 CLI state directory is writable, then retry the command.")
}

// Writable reports whether this shell would be allowed to write the document
// that is on disk now.
//
// It exists for a command that does something irreversible before it writes.
// wso2 login mints a session in the secure store and only then records the
// identity that names it, so a document it was never going to be allowed to
// overwrite has to be refused before the login rather than after it: the
// alternative leaves a refresh token in the store that no identity names and no
// command reaches, and that every retry duplicates.
//
// It is an early answer, not a promise. Nothing is locked here, so a document
// frozen between this call and the write is still refused at the write — which
// is where the answer that decides anything is given. What this rules out is
// the deterministic case, which is the one that costs the user a credential.
func Writable(stateRoot string) error { return refuseFrozenDocument(stateRoot) }
