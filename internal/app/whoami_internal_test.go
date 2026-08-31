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

package app

import (
	"testing"
	"time"

	"github.com/wso2/wso2-cli/internal/auth/session"
)

// TestSessionExpiryStateAtTheBoundary pins sessionExpiryState directly against
// its injectable now, rather than through shell.Run, because every
// package-level test sits roughly 30 days from the boundary and would still
// pass if Before(now) drifted to Before(now.Add(72*time.Hour)) or similar:
// only a test built around a fixed reference instant can catch that kind of
// off-by-something. internal/app already carries in-package test files
// (invoke_test.go, doctor_internal_test.go), so this needs no export seam.
func TestSessionExpiryStateAtTheBoundary(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

	for name, testCase := range map[string]struct {
		expiresAt   time.Time
		wantState   string
		wantExpired bool
	}{
		"the zero value: not stated, never expired": {
			expiresAt: time.Time{}, wantState: whoamiSessionPresent,
		},
		"one second before now: expired": {
			expiresAt: now.Add(-time.Second), wantState: whoamiSessionExpired, wantExpired: true,
		},
		"exactly now: not expired — Before(now) is false when equal": {
			expiresAt: now, wantState: whoamiSessionPresent,
		},
		"one second after now: not expired": {
			expiresAt: now.Add(time.Second), wantState: whoamiSessionPresent,
		},
		"far in the past: expired": {
			expiresAt: now.Add(-24 * time.Hour), wantState: whoamiSessionExpired, wantExpired: true,
		},
		"far in the future: not expired": {
			expiresAt: now.Add(24 * time.Hour), wantState: whoamiSessionPresent,
		},
	} {
		t.Run(name, func(t *testing.T) {
			stored := session.Session{SessionExpiresAt: testCase.expiresAt}
			state, expiry, recovery := sessionExpiryState(stored, now)
			if state != testCase.wantState {
				t.Errorf("state = %q, want %q", state, testCase.wantState)
			}
			if testCase.expiresAt.IsZero() {
				if expiry != sessionExpiryNotStated {
					t.Errorf("expiry = %q, want the not-stated wording", expiry)
				}
				if recovery != "" {
					t.Errorf("recovery = %q, want empty for an undisclosed expiry", recovery)
				}
				return
			}
			if want := testCase.expiresAt.UTC().Format(time.RFC3339); expiry != want {
				t.Errorf("expiry = %q, want %q", expiry, want)
			}
			if testCase.wantExpired && recovery == "" {
				t.Error("recovery = \"\", want a way back for an expired session")
			}
			if !testCase.wantExpired && recovery != "" {
				t.Errorf("recovery = %q, want empty for a present, unexpired session", recovery)
			}
		})
	}
}
