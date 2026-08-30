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

// Package lockfile is the shell's advisory file lock.
//
// The lock is held by the kernel against an open file description, not by the
// existence of the file, so the file is created once and never unlinked:
// release closes the descriptor and leaves the file in place. Removing it would
// reintroduce the race it replaced — a waiter holding a descriptor to the old
// inode and a newcomer creating a fresh one at the same path would both lock
// successfully. See docs/adr/0007-os-advisory-lock-for-session-rotation.md.
//
// Because the kernel keys the lock to the open file description rather than to
// the process, two callers inside one process contend exactly as two processes
// do — provided each opens the file for itself, which With always does. Locking
// twice through one description converts the existing lock instead of waiting
// for it, so no descriptor is ever shared across calls.
//
// The package classifies nothing. It reports contention as ErrBusy and its own
// failures as an Error, and leaves the typed problem to the caller, because the
// session store and the context document describe the same two conditions to a
// user in different words.
package lockfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ErrBusy reports that the deadline passed with the lock held elsewhere. It is
// deliberately distinct from a filesystem failure: one is worth retrying and
// the other is not, and only the caller knows how to say so to a user.
var ErrBusy = errors.New("lockfile: the lock is held by another process")

// Error marks a failure of the locking machinery itself, as opposed to a
// failure of the work run under the lock.
//
// With returns fn's error unchanged, so a caller cannot use "an error came
// back" to mean "the lock could not be taken" — that would report a failed
// read-modify-write as a broken state directory. Matching this type instead
// keeps the two apart. ErrBusy is not an Error: contention is the lock working.
type Error struct{ err error }

func (e Error) Error() string { return e.err.Error() }

// Unwrap exposes the underlying filesystem error, so a caller that wants the
// cause rather than the classification can still reach it.
func (e Error) Unwrap() error { return e.err }

// retryInterval is how often a waiting holder re-attempts the lock. The attempt
// is non-blocking so that the wait stays bounded by the caller's deadline; this
// is the price of that bound.
const retryInterval = 25 * time.Millisecond

// With runs fn while holding the advisory lock on the file at path, waiting up
// to deadline for a holder to release it, and returns fn's own error unwrapped.
//
// The lock file's directory is created at 0700 if it does not exist: a lock
// file carries no content, but its name is derived from state that is the
// user's own business.
//
// The file is opened here on every call rather than accepted from the caller.
// That is not an accident of the signature: a shared open file description
// would have the second acquisition convert the first one's lock rather than
// wait for it, and the mutual exclusion this package exists for would silently
// stop holding.
func With(path string, deadline time.Duration, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Error{fmt.Errorf("lockfile: cannot create the directory for %s: %w", path, err)}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return Error{fmt.Errorf("lockfile: cannot open %s: %w", path, err)}
	}
	defer func() { _ = file.Close() }()
	expiry := time.Now().Add(deadline)
	for {
		locked, lockErr := tryLock(file)
		if lockErr != nil {
			return Error{fmt.Errorf("lockfile: cannot lock %s: %w", path, lockErr)}
		}
		if locked {
			return fn()
		}
		if time.Now().After(expiry) {
			return ErrBusy
		}
		time.Sleep(retryInterval)
	}
}
