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

package session_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/sdk/problem"
)

func TestSaveAndLoadRoundTrip(t *testing.T) {
	keyring.MockInit()
	store := session.Store{StateRoot: t.TempDir()}
	saved := session.Session{
		Issuer:       "https://issuer.example.test",
		RefreshToken: "rt-1",
		AccessToken:  "at-1",
		ExpiresAt:    time.Now().Add(time.Hour).UTC(),
	}
	if err := store.Save("acme-cloud-login", saved); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := store.Load("acme-cloud-login")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.RefreshToken != "rt-1" || loaded.Issuer != saved.Issuer {
		t.Fatalf("round trip lost data: %+v", loaded)
	}
	if loaded.AccessToken != "at-1" || !loaded.ExpiresAt.Equal(saved.ExpiresAt) {
		t.Fatalf("round trip lost access token or expiry: %+v", loaded)
	}
}

func TestSaveReplacesPreviousEntry(t *testing.T) {
	keyring.MockInit()
	store := session.Store{StateRoot: t.TempDir()}
	first := session.Session{Issuer: "https://issuer.example.test", RefreshToken: "rt-1"}
	second := session.Session{Issuer: "https://issuer.example.test", RefreshToken: "rt-2"}
	if err := store.Save("acme-cloud-login", first); err != nil {
		t.Fatalf("save first: %v", err)
	}
	if err := store.Save("acme-cloud-login", second); err != nil {
		t.Fatalf("save second: %v", err)
	}
	loaded, err := store.Load("acme-cloud-login")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.RefreshToken != "rt-2" {
		t.Fatalf("save did not replace the previous entry: %+v", loaded)
	}
}

func TestMissingEntryIsLoginRequired(t *testing.T) {
	keyring.MockInit()
	store := session.Store{StateRoot: t.TempDir()}
	_, err := store.Load("never-logged-in")
	assertProblemCode(t, err, "auth.login_required")
}

func TestUndecodableEntryIsLoginRequired(t *testing.T) {
	keyring.MockInit()
	if err := keyring.Set(session.Service, "acme-cloud-login", "not json"); err != nil {
		t.Fatalf("seed foreign entry: %v", err)
	}
	store := session.Store{StateRoot: t.TempDir()}
	_, err := store.Load("acme-cloud-login")
	assertProblemCode(t, err, "auth.login_required")
}

func TestEntryWithoutRefreshTokenIsLoginRequired(t *testing.T) {
	keyring.MockInit()
	if err := keyring.Set(session.Service, "acme-cloud-login", `{"issuer":"https://issuer.example.test"}`); err != nil {
		t.Fatalf("seed stale entry: %v", err)
	}
	store := session.Store{StateRoot: t.TempDir()}
	_, err := store.Load("acme-cloud-login")
	assertProblemCode(t, err, "auth.login_required")
}

func TestKeyringUnavailableIsTyped(t *testing.T) {
	keyring.MockInitWithError(errors.New("no secret service"))
	store := session.Store{StateRoot: t.TempDir()}
	_, err := store.Load("acme-cloud-login")
	assertProblemCode(t, err, "auth.keyring_unavailable")
}

func TestKeyringUnavailableOnSaveIsTyped(t *testing.T) {
	keyring.MockInitWithError(errors.New("no secret service"))
	store := session.Store{StateRoot: t.TempDir()}
	err := store.Save("acme-cloud-login", session.Session{RefreshToken: "rt-1"})
	assertProblemCode(t, err, "auth.keyring_unavailable")
}

func TestProbeReportsReachableWhenTheKeyIsSimplyAbsent(t *testing.T) {
	keyring.MockInit()
	store := session.Store{StateRoot: t.TempDir()}
	if err := store.Probe(); err != nil {
		t.Fatalf("Probe: %v, want nil: a not-found answer is a reachable store", err)
	}
}

func TestProbeReportsReachableEvenIfSomethingIsStoredUnderItsKey(t *testing.T) {
	keyring.MockInit()
	if err := keyring.Set(session.Service, session.ProbeCredentialRef, "unrelated"); err != nil {
		t.Fatalf("seed the probe key: %v", err)
	}
	store := session.Store{StateRoot: t.TempDir()}
	if err := store.Probe(); err != nil {
		t.Fatalf("Probe: %v, want nil: any answer from the backend means it is reachable", err)
	}
}

func TestProbeIsTypedWhenTheKeyringIsUnavailable(t *testing.T) {
	keyring.MockInitWithError(errors.New("no secret service"))
	store := session.Store{StateRoot: t.TempDir()}
	assertProblemCode(t, store.Probe(), "auth.keyring_unavailable")
}

func TestWithLockSerializesWriters(t *testing.T) {
	keyring.MockInit()
	store := session.Store{StateRoot: t.TempDir()}
	var inside, maxInside int32
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			_ = store.WithLock("acme-cloud-login", func() error {
				now := atomic.AddInt32(&inside, 1)
				if now > atomic.LoadInt32(&maxInside) {
					atomic.StoreInt32(&maxInside, now)
				}
				time.Sleep(5 * time.Millisecond)
				atomic.AddInt32(&inside, -1)
				return nil
			})
		}()
	}
	group.Wait()
	if atomic.LoadInt32(&maxInside) != 1 {
		t.Fatalf("lock admitted %d concurrent writers", maxInside)
	}
}

func TestWithLockReleasesOnError(t *testing.T) {
	keyring.MockInit()
	store := session.Store{StateRoot: t.TempDir()}
	sentinel := errors.New("writer failed")
	if err := store.WithLock("acme-cloud-login", func() error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatalf("writer error not propagated: %v", err)
	}
	// The lock must be free again: a second writer succeeds immediately.
	ran := false
	if err := store.WithLock("acme-cloud-login", func() error { ran = true; return nil }); err != nil {
		t.Fatalf("lock was not released after error: %v", err)
	}
	if !ran {
		t.Fatal("second writer never ran")
	}
}

// writersPerRound is the burst size one round throws at a single reference.
const writersPerRound = 32

// TestWithLockNeverOverlapsWithAbandonedLockFile pins the property the lock
// exists for: no two writers on one credential reference run fn at the same
// time, including when a lock file left behind by a crashed process is already
// sitting at the path.
//
// Each round also asserts that every writer eventually ran. Exclusion on its
// own is a weak oracle — a lock that admits one writer and refuses the other
// thirty-one satisfies it — and that is exactly what a contended acquisition
// that stops retrying looks like.
func TestWithLockNeverOverlapsWithAbandonedLockFile(t *testing.T) {
	keyring.MockInit()
	// Each round is an independent burst of writers arriving together on a
	// reference whose lock file was left behind by a crashed process. Rounds
	// run in parallel, on their own state roots, because admitting a second
	// writer is a race rather than a certainty: one overlap in any round is a
	// failure of the property.
	for round := range 60 {
		t.Run(fmt.Sprintf("round-%02d", round), func(t *testing.T) {
			t.Parallel()
			runAbandonedLockRound(t)
		})
	}
}

func runAbandonedLockRound(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	store := session.Store{StateRoot: root}

	// Age the abandoned file well past any staleness threshold, so every
	// writer in the burst sees the same abandoned lock.
	abandoned := filepath.Join(root, "cli", "locks", "acme-cloud-login.lock")
	if err := os.MkdirAll(filepath.Dir(abandoned), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abandoned, nil, 0o600); err != nil {
		t.Fatalf("seed abandoned lock: %v", err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(abandoned, old, old); err != nil {
		t.Fatalf("age abandoned lock: %v", err)
	}

	var mu sync.Mutex
	inside, maxInside, entered := 0, 0, 0
	var group sync.WaitGroup
	start := make(chan struct{})
	for range writersPerRound {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			_ = store.WithLock("acme-cloud-login", func() error {
				mu.Lock()
				inside++
				entered++
				if inside > maxInside {
					maxInside = inside
				}
				mu.Unlock()

				time.Sleep(2 * time.Millisecond)

				mu.Lock()
				inside--
				mu.Unlock()
				return nil
			})
		}()
	}
	close(start)
	group.Wait()

	mu.Lock()
	peak, ran := maxInside, entered
	mu.Unlock()
	if peak != 1 {
		t.Fatalf("lock admitted %d concurrent writers on one reference", peak)
	}
	// Exclusion alone is satisfied by a lock that admits one writer and refuses
	// the rest, which is what a contended acquisition that never retries does.
	// Every writer must also get its turn before the deadline.
	if ran != writersPerRound {
		t.Fatalf("only %d of %d writers ran: the lock refused contention instead of waiting for it",
			ran, writersPerRound)
	}
}

// TestWithLockSurvivesKilledHolder covers the crash case end to end: a process
// killed while holding the lock must leave nothing that blocks the next
// acquisition.
func TestWithLockSurvivesKilledHolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the helper relies on POSIX process signalling")
	}
	keyring.MockInit()
	root := t.TempDir()

	held := filepath.Join(root, "held")
	helper := exec.Command(os.Args[0], "-test.run=TestHelperHoldsSessionLock", "-test.timeout=60s")
	helper.Env = append(os.Environ(), lockHelperEnv+"="+root)
	if err := helper.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	defer func() {
		_ = helper.Process.Kill()
		_, _ = helper.Process.Wait()
	}()

	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := os.Stat(held); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper never took the lock")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := helper.Process.Kill(); err != nil {
		t.Fatalf("kill helper: %v", err)
	}
	_, _ = helper.Process.Wait()

	store := session.Store{StateRoot: root}
	ran := false
	if err := store.WithLock("acme-cloud-login", func() error { ran = true; return nil }); err != nil {
		t.Fatalf("lock not recovered after holder was killed: %v", err)
	}
	if !ran {
		t.Fatal("writer never ran after holder was killed")
	}
}

// lockHelperEnv carries the state root into the helper subprocess and is what
// makes TestHelperHoldsSessionLock do anything at all.
const lockHelperEnv = "WSO2_CLI_TEST_LOCK_HELPER_ROOT"

// TestHelperHoldsSessionLock is not a test: it is the subprocess body for
// TestWithLockSurvivesKilledHolder. It takes the lock, signals that it holds
// it, and blocks until it is killed.
func TestHelperHoldsSessionLock(t *testing.T) {
	root := os.Getenv(lockHelperEnv)
	if root == "" {
		t.Skip("helper process only")
	}
	keyring.MockInit()
	store := session.Store{StateRoot: root}
	_ = store.WithLock("acme-cloud-login", func() error {
		if err := os.WriteFile(filepath.Join(root, "held"), nil, 0o600); err != nil {
			return err
		}
		select {}
	})
}

func assertProblemCode(t *testing.T, err error, code string) {
	t.Helper()
	var typed problem.Problem
	if !errors.As(err, &typed) || typed.Code != code {
		t.Fatalf("expected problem code %q, got %v", code, err)
	}
	if typed.Category != problem.CategoryAuthPolicy {
		t.Fatalf("expected auth_policy category, got %q", typed.Category)
	}
}

func TestDeleteRemovesTheEntry(t *testing.T) {
	keyring.MockInit()
	store := session.Store{StateRoot: t.TempDir()}
	if err := store.Save("acme-cloud-login",
		session.Session{Issuer: "https://issuer.example.test", RefreshToken: "rt-1"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	removed, err := store.Delete("acme-cloud-login")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !removed {
		t.Error("delete did not report removing the entry it removed")
	}
	_, err = store.Load("acme-cloud-login")
	assertProblemCode(t, err, "auth.login_required")
}

// Deleting what is not there is the state the caller asked for, so it is not a
// failure. Logging out twice must not turn the second attempt into an error a
// user has to interpret.
func TestDeleteMissingEntrySucceeds(t *testing.T) {
	keyring.MockInit()
	store := session.Store{StateRoot: t.TempDir()}
	removed, err := store.Delete("never-logged-in")
	if err != nil {
		t.Fatalf("delete of a missing entry: %v", err)
	}
	if removed {
		t.Error("delete reported removing an entry that was never there")
	}
}

// A stale entry Load refuses to read is still an entry, and deleting it reports
// that something was removed. Load cannot distinguish it from a missing one, so
// this is the only way a caller learns a session was really ended.
func TestDeleteReportsRemovingAnUnreadableEntry(t *testing.T) {
	keyring.MockInit()
	if err := keyring.Set(session.Service, "acme-cloud-login", "not json"); err != nil {
		t.Fatalf("seed foreign entry: %v", err)
	}
	store := session.Store{StateRoot: t.TempDir()}
	removed, err := store.Delete("acme-cloud-login")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !removed {
		t.Error("delete did not report removing an unreadable entry")
	}
}

// Stored answers existence, not usability: an entry Load would refuse as
// unreadable is still stored, and a missing one is not, without either answer
// being an error.
func TestStoredDistinguishesAbsenceFromPresence(t *testing.T) {
	keyring.MockInit()
	store := session.Store{StateRoot: t.TempDir()}
	stored, err := store.Stored("never-logged-in")
	if err != nil {
		t.Fatalf("stored of a missing entry: %v", err)
	}
	if stored {
		t.Error("an entry that was never written is reported as stored")
	}
	if err := keyring.Set(session.Service, "acme-cloud-login", "not json"); err != nil {
		t.Fatalf("seed foreign entry: %v", err)
	}
	stored, err = store.Stored("acme-cloud-login")
	if err != nil {
		t.Fatalf("stored of an unreadable entry: %v", err)
	}
	if !stored {
		t.Error("an unreadable entry is still an entry, and is not reported as stored")
	}
}

func TestKeyringUnavailableOnStoredIsTyped(t *testing.T) {
	keyring.MockInitWithError(errors.New("no secret service"))
	store := session.Store{StateRoot: t.TempDir()}
	_, err := store.Stored("acme-cloud-login")
	assertProblemCode(t, err, "auth.keyring_unavailable")
}

// Deleting one reference leaves every other session alone. Two identities on
// one machine share the service name and are separated only by the reference.
func TestDeleteLeavesOtherReferences(t *testing.T) {
	keyring.MockInit()
	store := session.Store{StateRoot: t.TempDir()}
	for _, ref := range []string{"acme-cloud-login", "acme-staging-login"} {
		if err := store.Save(ref,
			session.Session{Issuer: "https://issuer.example.test", RefreshToken: "rt-" + ref}); err != nil {
			t.Fatalf("save %s: %v", ref, err)
		}
	}
	if _, err := store.Delete("acme-cloud-login"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	loaded, err := store.Load("acme-staging-login")
	if err != nil {
		t.Fatalf("load the untouched reference: %v", err)
	}
	if loaded.RefreshToken != "rt-acme-staging-login" {
		t.Fatalf("the untouched reference changed: %+v", loaded)
	}
}
