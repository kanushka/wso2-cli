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
	"os"
	"path/filepath"
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

func TestWithLockTakesOverStaleLock(t *testing.T) {
	keyring.MockInit()
	root := t.TempDir()
	store := session.Store{StateRoot: root}
	// Leave a lock file behind, as a crashed process would, with an old mtime.
	stale := filepath.Join(root, "cli", "locks", "acme-cloud-login.lock")
	if err := os.MkdirAll(filepath.Dir(stale), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(stale, nil, 0o600); err != nil {
		t.Fatalf("seed stale lock: %v", err)
	}
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatalf("age stale lock: %v", err)
	}
	ran := false
	if err := store.WithLock("acme-cloud-login", func() error { ran = true; return nil }); err != nil {
		t.Fatalf("stale lock was not taken over: %v", err)
	}
	if !ran {
		t.Fatal("writer never ran after takeover")
	}
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
