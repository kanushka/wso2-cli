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

package lockfile_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/wso2/wso2-cli/internal/lockfile"
)

func TestWithRunsTheFunctionWhileHoldingTheLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "document.lock")
	ran := false
	if err := lockfile.With(path, time.Second, func() error {
		ran = true
		return nil
	}); err != nil {
		t.Fatalf("With: %v", err)
	}
	if !ran {
		t.Error("the function did not run")
	}
}

func TestWithReturnsTheFunctionsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "document.lock")
	sentinel := errors.New("sentinel")
	if err := lockfile.With(path, time.Second, func() error { return sentinel }); !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want the function's own error", err)
	}
}

func TestWithDistinguishesItsOwnFailureFromTheFunctionsError(t *testing.T) {
	// A caller maps a lock failure to one typed problem and lets the work's own
	// failure through untouched. It can only do that if the two are told apart,
	// and "err != nil" cannot tell them apart.
	path := filepath.Join(t.TempDir(), "document.lock")
	sentinel := errors.New("sentinel")
	err := lockfile.With(path, time.Second, func() error { return sentinel })
	var lockErr lockfile.Error
	if errors.As(err, &lockErr) {
		t.Errorf("the function's own error was reported as a lock failure: %v", err)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want the sentinel", err)
	}
}

func TestWithReportsItsOwnFailureAsALockError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("directory permissions do not refuse a write here")
	}
	// A lock file that cannot be opened is the shell's failure, not the work's.
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	err := lockfile.With(filepath.Join(dir, "nested", "document.lock"), time.Second, func() error {
		t.Error("the function ran without the lock")
		return nil
	})
	var lockErr lockfile.Error
	if !errors.As(err, &lockErr) {
		t.Errorf("err = %v, want a lockfile.Error", err)
	}
}

func TestASecondHolderInTheSameProcessWaitsRatherThanRunningConcurrently(t *testing.T) {
	// The lock is a kernel advisory lock held against an open file description,
	// so two goroutines in one process each opening the file contend the same
	// way two processes do. This is the property every writer in the shell
	// depends on: a read-modify-write cannot interleave with another one.
	path := filepath.Join(t.TempDir(), "document.lock")
	var mu sync.Mutex
	var order []string
	release := make(chan struct{})
	entered := make(chan struct{})
	done := make(chan error, 1)

	go func() {
		done <- lockfile.With(path, 5*time.Second, func() error {
			mu.Lock()
			order = append(order, "first-in")
			mu.Unlock()
			close(entered)
			<-release
			mu.Lock()
			order = append(order, "first-out")
			mu.Unlock()
			return nil
		})
	}()
	<-entered
	second := make(chan error, 1)
	go func() {
		second <- lockfile.With(path, 5*time.Second, func() error {
			mu.Lock()
			order = append(order, "second-in")
			mu.Unlock()
			return nil
		})
	}()
	// Give the second holder time to block on the lock rather than to be
	// merely unscheduled.
	time.Sleep(200 * time.Millisecond)
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("first holder: %v", err)
	}
	if err := <-second; err != nil {
		t.Fatalf("second holder: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"first-in", "first-out", "second-in"}
	if !slices.Equal(order, want) {
		t.Errorf("order = %v, want %v; the second holder ran inside the first", order, want)
	}
}

func TestWithReportsBusyWhenTheDeadlinePasses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "document.lock")
	held := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = lockfile.With(path, 5*time.Second, func() error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	defer close(release)
	err := lockfile.With(path, 50*time.Millisecond, func() error {
		t.Error("the function ran while the lock was held elsewhere")
		return nil
	})
	if !errors.Is(err, lockfile.ErrBusy) {
		t.Errorf("err = %v, want ErrBusy", err)
	}
}

func TestBusyIsNotReportedAsALockError(t *testing.T) {
	// Contention and a filesystem failure recover differently: one is worth
	// retrying and the other is not. A caller matching lockfile.Error first
	// must not swallow the busy case.
	path := filepath.Join(t.TempDir(), "document.lock")
	held := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = lockfile.With(path, 5*time.Second, func() error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	defer close(release)
	err := lockfile.With(path, 50*time.Millisecond, func() error { return nil })
	var lockErr lockfile.Error
	if errors.As(err, &lockErr) {
		t.Errorf("busy was reported as a lock failure: %v", err)
	}
}

func TestTheLockFileIsNotUnlinkedOnRelease(t *testing.T) {
	// ADR 0007: release closes the descriptor and leaves the file. Removing it
	// would let a waiter holding the old inode and a newcomer creating a fresh
	// one both lock successfully.
	path := filepath.Join(t.TempDir(), "document.lock")
	if err := lockfile.With(path, time.Second, func() error { return nil }); err != nil {
		t.Fatalf("With: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the lock file was removed on release: %v", err)
	}
}

func TestWithCreatesTheLockDirectory(t *testing.T) {
	// The session store keys its locks by credential reference under a
	// directory that need not exist yet, and so does the first write on a
	// fresh machine.
	path := filepath.Join(t.TempDir(), "locks", "acme.lock")
	if err := lockfile.With(path, time.Second, func() error { return nil }); err != nil {
		t.Fatalf("With: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("Stat: %v", err)
	}
}
