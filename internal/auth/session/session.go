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

// Package session persists interactive login sessions in the OS secure store.
//
// One credential reference maps to one keychain entry. The entry is the only
// place a refresh token lives: it is never written to a file, and the state
// root hosts only the advisory lock files that keep refresh-token rotation
// single-writer across concurrent shell invocations.
package session

import (
	"encoding/json"
	"errors"
	"time"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/sdk/problem"
)

// Service is the OS secure store service name every session entry lives under.
const Service = "wso2-cli"

// Session is one identity's interactive login state, stored as a single
// keychain entry. It exists only inside the shell and the OS secure store.
type Session struct {
	Issuer       string    `json:"issuer"`
	RefreshToken string    `json:"refreshToken"`
	AccessToken  string    `json:"accessToken,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt,omitempty"`
	// Subject is the verified identity token's subject, recorded at login.
	// omitempty so a keychain entry written before this field existed decodes
	// with it empty rather than failing to decode: encoding/json leaves an
	// absent JSON member as the Go zero value. wso2 whoami is the one place
	// that reads this field, and it renders an empty Subject as unknown rather
	// than as a blank field, rather than this type asserting a guarantee it
	// cannot enforce.
	Subject string `json:"subject,omitempty"`
	// SessionExpiresAt is when the REFRESH token stops working, not the access
	// token — see ExpiresAt above for that one. It is the zero value whenever
	// the issuer has not disclosed a refresh-token lifetime, which most
	// issuers today do not; nothing in this package treats that as an error,
	// and nothing here invents a substitute for it.
	SessionExpiresAt time.Time `json:"sessionExpiresAt,omitempty"`
}

// Store reads and writes sessions in the OS secure store.
type Store struct {
	// StateRoot hosts the advisory lock files, never session content.
	StateRoot string
}

// Load returns the stored session for a credential reference.
//
// A missing entry is auth.login_required; an unavailable keyring backend is
// auth.keyring_unavailable. An unreadable or undecodable entry is
// auth.login_required as well: stale entries are re-logged-in, not repaired.
func (s Store) Load(ref string) (Session, error) {
	value, err := keyring.Get(Service, ref)
	switch {
	case errors.Is(err, keyring.ErrNotFound):
		return Session{}, loginRequired("no stored login session exists for the selected context",
			"Run wso2 login to establish a session for this context.")
	case err != nil:
		return Session{}, keyringUnavailable()
	}
	var stored Session
	if json.Unmarshal([]byte(value), &stored) != nil || stored.RefreshToken == "" {
		// A stale or foreign entry is indistinguishable from no session.
		return Session{}, loginRequired("the stored login session for the selected context cannot be read",
			"Run wso2 login to establish a fresh session for this context.")
	}
	return stored, nil
}

// ProbeCredentialRef is the reserved reference Probe reads under.
//
// It contains a period, a character the credentialRef pattern
// (^[a-z][a-z0-9-]{0,63}$) never allows, so no identity a document declares can
// ever be assigned this reference. That is what lets Probe read the secure
// store without risking a collision with, or a read of, a real session.
const ProbeCredentialRef = "probe.reachability"

// Probe reports whether the OS secure store answers a read at all, without
// touching any identity's session.
//
// It asks for the reserved reference above, which nothing ever stores a
// session under. A "not found" answer means the backend was reached and had an
// opinion about the key, which is what "reachable" means here: there is
// nothing to probe with, only whether the backend can be asked. Any other
// error means the backend itself, not the key, could not be reached.
func (s Store) Probe() error {
	_, err := keyring.Get(Service, ProbeCredentialRef)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return keyringUnavailable()
	}
	return nil
}

// Save writes the session, replacing any previous entry.
func (s Store) Save(ref string, value Session) error {
	data, err := json.Marshal(value)
	if err != nil {
		// A Session of strings and a time cannot fail to marshal; treat the
		// impossible the same as an unusable backend rather than panicking.
		return keyringUnavailable()
	}
	if err := keyring.Set(Service, ref, string(data)); err != nil {
		return keyringUnavailable()
	}
	return nil
}

// Delete removes the session for a credential reference, reporting whether
// there was one to remove.
//
// A missing entry is not an error. The caller asked for a machine with no
// session for this reference, and that is the state either way; a second logout
// would otherwise refuse for having succeeded the first time.
//
// The boolean is the only honest answer to "was a session ended", and Load
// cannot give it: a stale or foreign entry is reported by Load as
// auth.login_required exactly as a missing one is, so a caller that inferred
// existence from Load would tell a user nothing was stored while this method
// removed something.
//
// Only the shell-owned entry goes. Whether the issuer's own copy of the session
// was retracted is a separate fact the caller establishes separately, because
// nothing this store can see reveals it. See
// docs/adr/0010-best-effort-revocation-on-session-end.md.
func (s Store) Delete(ref string) (bool, error) {
	err := keyring.Delete(Service, ref)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, keyring.ErrNotFound):
		return false, nil
	default:
		return false, keyringUnavailable()
	}
}

// loginRequired reports the absence of a usable session, whatever its cause.
// Missing, stale, foreign, and lock-contended sessions all recover the same
// way, so they share one stable code.
func loginRequired(message, recovery string) problem.Problem {
	return problem.New(problem.CategoryAuthPolicy, "auth.login_required", message).
		WithRecovery(recovery)
}

// keyringUnavailable reports the secure store as unusable. The backend's own
// error is deliberately dropped: it may describe the user's desktop session in
// terms the shell cannot vouch for, and the recovery is the same regardless.
func keyringUnavailable() problem.Problem {
	return problem.New(problem.CategoryAuthPolicy, "auth.keyring_unavailable",
		"the OS secure store is not available to the shell").
		WithRecovery("Enable the OS keychain or secret service for this user, then retry. " +
			"The shell does not store credentials in files.")
}
