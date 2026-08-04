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
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const (
	// lockRetryInterval is how often a waiting writer re-attempts the lock.
	lockRetryInterval = 25 * time.Millisecond
	// lockDeadline is how long a writer waits before giving up.
	lockDeadline = 5 * time.Second
	// lockStaleAfter is the age past which a held lock is presumed abandoned
	// by a crashed process and taken over.
	lockStaleAfter = 30 * time.Second
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

// acquireLock takes the advisory lock at path, retrying until the deadline.
//
// The lock is the existence of the file: O_EXCL creation is atomic on every
// filesystem the shell targets. A lock file older than lockStaleAfter is
// treated as abandoned — removed once, then creation is retried like any
// other attempt.
func acquireLock(path string) (release func(), err error) {
	deadline := time.Now().Add(lockDeadline)
	tookOverStale := false
	for {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			file.Close()
			return func() { os.Remove(path) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, lockFailed()
		}
		if !tookOverStale {
			if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > lockStaleAfter {
				tookOverStale = true
				os.Remove(path)
				continue
			}
		}
		if time.Now().After(deadline) {
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
