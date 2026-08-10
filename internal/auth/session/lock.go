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

package session

import (
	"os"
	"path/filepath"
	"time"
)

const (
	// lockRetryInterval is how often a waiting writer re-attempts the lock.
	lockRetryInterval = 25 * time.Millisecond
	// lockDeadline is how long a writer waits before giving up. The critical
	// section spans a token refresh round trip to the issuer, so the wait has
	// to outlast a slow deployment rather than a local file operation.
	lockDeadline = 30 * time.Second
)

// WithLock runs fn while holding the per-reference advisory file lock.
// It is how refresh-token rotation stays single-writer across processes.
func (s Store) WithLock(ref string, fn func() error) error {
	path := lockPath(s.StateRoot, ref)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return lockFailed()
	}
	release, err := acquireLock(path)
	if err != nil {
		return err
	}
	defer release()
	return fn()
}

// acquireLock takes the kernel advisory lock on the file at path, retrying the
// non-blocking attempt until the deadline.
//
// The lock is held by the kernel against the open file descriptor, not by the
// existence of the file, so the file is created once and never unlinked:
// release closes the descriptor and leaves the file in place. Removing it
// would reintroduce the race it replaced — a waiter holding a descriptor to
// the old inode and a newcomer creating a fresh one at the same path would
// both lock successfully. See ADR 0005.
func acquireLock(path string) (release func(), err error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, lockFailed()
	}
	deadline := time.Now().Add(lockDeadline)
	for {
		locked, lockErr := tryLock(file)
		if lockErr != nil {
			_ = file.Close()
			return nil, lockFailed()
		}
		if locked {
			return func() { _ = file.Close() }, nil
		}
		if time.Now().After(deadline) {
			_ = file.Close()
			return nil, lockBusy()
		}
		time.Sleep(lockRetryInterval)
	}
}

// lockPath is where the advisory lock for a credential reference lives. The
// file carries no content: session material never touches the state root.
func lockPath(stateRoot, ref string) string {
	return filepath.Join(stateRoot, "cli", "locks", ref+".lock")
}

// lockBusy reports the session as busy under another invocation. The code is
// auth.login_required rather than a new busy code: the stable code list is
// closed, and the condition is recoverable the same way — by retrying.
func lockBusy() error {
	return loginRequired("another WSO2 CLI invocation is updating this session",
		"Retry the command.")
}

// lockFailed reports that the lock could not be taken at all — a filesystem
// failure, not contention — without claiming another invocation holds it.
func lockFailed() error {
	return loginRequired("the shell could not take the session update lock",
		"Check that the WSO2 CLI state directory is writable, then retry the command.")
}
