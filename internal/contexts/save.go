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
	"errors"
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
	return withDocumentLock(stateRoot, func() error { return writeDocument(stateRoot, data) })
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
	return withDocumentLock(stateRoot, func() error {
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
		return documentUnwritable()
	}
	if err := atomicfile.Write(path, data, documentMode); err != nil {
		return documentUnwritable()
	}
	return nil
}

// withDocumentLock runs fn while holding the document lock, translating the two
// failures internal/lockfile reports into this package's voice.
//
// A failure inside fn is neither of them and passes through untouched, which is
// why the conditions are matched by type rather than by "err != nil": a refused
// change must reach the user as the refusal it is, not as a broken lock.
func withDocumentLock(stateRoot string, fn func() error) error {
	err := lockfile.With(LockPath(stateRoot), lockDeadline, fn)
	if errors.Is(err, lockfile.ErrBusy) {
		return contextProblem("contexts.document_busy",
			"another WSO2 CLI invocation is updating the context document",
			"Retry the command.")
	}
	var lockErr lockfile.Error
	if errors.As(err, &lockErr) {
		return documentUnwritable()
	}
	return err
}

// documentUnwritable reports that the shell could not write the document, for a
// filesystem reason rather than because the document was wrong.
//
// Unlike the session lock, which reuses auth.login_required because a busy
// session recovers exactly as an expired one does, neither of these conditions
// recovers the way an existing contexts.* code does: the document here is
// readable and well formed, so contexts.document_malformed and
// contexts.document_unreadable would both send the user to correct a file that
// has nothing wrong with it.
func documentUnwritable() error {
	return contextProblem("contexts.document_unwritable",
		"the WSO2 CLI context document could not be written",
		"Check that the WSO2 CLI state directory is writable, then retry the command.")
}
