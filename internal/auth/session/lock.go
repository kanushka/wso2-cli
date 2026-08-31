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
	"path/filepath"
	"time"

	"github.com/wso2/wso2-cli/internal/lockfile"
)

const (
	// lockDeadline is how long a writer waits before giving up. The critical
	// section spans a token refresh round trip to the issuer, so the wait has
	// to outlast a slow deployment rather than a local file operation.
	//
	// It must stay strictly greater than auth.grantDeadline, which bounds that
	// round trip: at equal values a holder that is merely slow outlives the
	// waiter's patience, and an ordinary wait becomes a spurious refusal. The
	// constant is not derived from auth.grantDeadline because auth imports this
	// package, so the coupling is stated here instead — raising one means
	// raising the other. The margin covers the keyring read and write that
	// bracket the round trip inside the same critical section.
	lockDeadline = 45 * time.Second
)

// WithLock runs fn while holding the per-reference advisory file lock.
// It is how refresh-token rotation stays single-writer across processes.
//
// The lock itself lives in internal/lockfile, which classifies nothing; this is
// where its two failure modes become the session package's typed problems. A
// failure inside fn is neither of them and passes through untouched, which is
// why the two conditions are matched by type rather than by "err != nil".
func (s Store) WithLock(ref string, fn func() error) error {
	err := lockfile.With(lockPath(s.StateRoot, ref), lockDeadline, fn)
	if errors.Is(err, lockfile.ErrBusy) {
		return lockBusy()
	}
	var lockErr lockfile.Error
	if errors.As(err, &lockErr) {
		return lockFailed()
	}
	return err
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
