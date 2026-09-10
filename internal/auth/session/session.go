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
// One credential reference under one state root maps to one keychain entry.
// The entry is the only place a refresh token lives: it is never written to a
// file, and the state root hosts only the advisory lock files that keep
// refresh-token rotation single-writer across concurrent shell invocations.
//
// The entry is named by the reference and a digest of the state root, so two
// state roots (two WSO2_HOMEs) whose context documents both name an identity
// "thunder" never share a session: a session is served only to the root that
// stored it. See EntryName.
package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
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
	// Name is the human-readable display name the login resolved for the
	// signed-in person — the identity token's name claim, given_name and
	// family_name joined, or email, whichever oauthflow's displayName found
	// first (internal/auth/oauthflow/login.go). omitempty, so an entry written
	// before this field existed decodes with it empty rather than failing to
	// decode, exactly as Subject's own comment explains. wso2 whoami is the one
	// place that reads it, and it falls back to the subject it already knows
	// rather than rendering a blank field or inventing a name.
	Name string `json:"name,omitempty"`
	// SessionExpiresAt is when the REFRESH token stops working, not the access
	// token — see ExpiresAt above for that one. It is the zero value whenever
	// the issuer has not disclosed a refresh-token lifetime, which most
	// issuers today do not; nothing in this package treats that as an error,
	// and nothing here invents a substitute for it.
	SessionExpiresAt time.Time `json:"sessionExpiresAt,omitempty"`
	// Strategy is how this session was obtained (a contexts.Strategy* value),
	// ClientID the client it was obtained as, and Scopes what it was
	// authorized for. wso2 whoami reports the document's own strategy for a
	// product, not this field; what these three guard is drift instead — a
	// product whose namespace sorts ahead of the current login product can
	// become the login product itself the moment it is recorded, and
	// sessionSource.renew refuses to present a session recorded here for a
	// different client or scope set as if it belonged to the product now
	// asking. omitempty, so an entry written before they existed decodes with
	// them empty rather than failing to decode: encoding/json leaves an
	// absent JSON member as the Go zero value.
	Strategy string `json:"strategy,omitempty"`
	// IDToken is the identity token the authorization returned, kept so that
	// wso2 logout can name this session to the provider's end-session
	// endpoint. It is not a credential: it grants nothing and was verified
	// before it was stored.
	IDToken  string   `json:"idToken,omitempty"`
	ClientID string   `json:"clientId,omitempty"`
	Scopes   []string `json:"scopes,omitempty"`
}

// Store reads and writes sessions in the OS secure store.
type Store struct {
	// StateRoot hosts the advisory lock files, never session content.
	StateRoot string
}

// EntryName is the secure-store name a credential reference's session lives
// under: the reference, '@', and a digest of the state root.
//
// The digest is what keeps one root from reading another's session. Its input
// is the root's cleaned path, so the same WSO2_HOME spelled two ways is one
// root, and '@' is admitted by neither a credential reference nor a product
// namespace, so the name can never be mistaken for a reference of its own.
// The digest is truncated because it identifies a root rather than protecting
// anything: the store it names is what protects the session.
func (s Store) EntryName(ref string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(s.StateRoot)))
	return ref + "@" + hex.EncodeToString(sum[:rootDigestBytes])
}

// rootDigestBytes is how much of the state root's digest the entry name keeps.
const rootDigestBytes = 8

// Load returns the stored session for a credential reference.
//
// A missing entry is auth.login_required; an unavailable keyring backend is
// auth.keyring_unavailable. An unreadable or undecodable entry is
// auth.login_required as well: stale entries are re-logged-in, not repaired.
//
// An entry a shell before this one stored under the bare reference is not
// read: nothing records which root wrote it, and a fresh root that read it
// would be reporting, and refreshing, another deployment's session. Such an
// entry is retired by the next Save or Delete under the same reference.
func (s Store) Load(ref string) (Session, error) {
	name := s.EntryName(ref)
	value, err := keyring.Get(Service, name)
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
	// The large tokens come from their side entries when the session entry
	// carries none inline; an entry written before the side entries existed
	// carries its access token inline and is read as it was written.
	if stored.AccessToken == "" {
		stored.AccessToken = s.sideEntry(name + accessTokenSuffix)
	}
	if stored.IDToken == "" {
		stored.IDToken = s.sideEntry(name + idTokenSuffix)
	}
	return stored, nil
}

// The suffixes of a session's side entries. '#' is admitted by neither a
// credential reference nor a product namespace, so a side entry can never be
// mistaken for, or collide with, a session entry.
const (
	accessTokenSuffix = "#access"
	idTokenSuffix     = "#id"
)

// sideEntry reads one side entry, empty when there is none. A backend that
// cannot be asked shows up on the session entry itself, which was read first.
func (s Store) sideEntry(key string) string {
	value, err := keyring.Get(Service, key)
	if err != nil {
		return ""
	}
	return value
}

// Stored reports whether any entry exists for a credential reference, without
// judging whether the entry is usable.
//
// Load cannot answer this: it reports a stale or foreign entry with the same
// auth.login_required a missing one gets, because both recover by logging in
// again. wso2 doctor needs the two told apart anyway — a machine with no entry
// is the state wso2 logout deliberately leaves behind, while an entry that
// exists but cannot be used is a fault — so existence is its own question,
// asked before Load judges usability.
func (s Store) Stored(ref string) (bool, error) {
	_, err := keyring.Get(Service, s.EntryName(ref))
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, keyring.ErrNotFound):
		return false, nil
	default:
		return false, keyringUnavailable()
	}
}

// ProbeCredentialRef is the reserved reference Probe reads under.
//
// The key is reserved by convention, not by a pattern this package enforces:
// a credential reference of "probe" paired with a product namespace of
// "reachability" would form this same string through ProductSessionRef. That
// collision is harmless, because Probe never reads what a real session would
// have written there — it only asks the backend whether the key is known,
// and treats "not found" and "found" identically. See Probe.
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
//
// The access token and the identity token go to side entries of their own,
// and the session entry holds the rest. macOS's secure-store tool refuses a
// command over 4096 bytes, and a session carrying three JSON web tokens is
// larger than that once encoded; split three ways every piece stays well
// under. A side entry a session no longer has a value for is removed, so a
// later read cannot resurrect a token from an earlier session.
func (s Store) Save(ref string, value Session) error {
	entry := value
	entry.AccessToken = ""
	entry.IDToken = ""
	data, err := json.Marshal(entry)
	if err != nil {
		// A Session of strings and a time cannot fail to marshal; treat the
		// impossible the same as an unusable backend rather than panicking.
		return keyringUnavailable()
	}
	name := s.EntryName(ref)
	if err := keyring.Set(Service, name, string(data)); err != nil {
		return keyringUnavailable()
	}
	for suffix, token := range map[string]string{accessTokenSuffix: value.AccessToken, idTokenSuffix: value.IDToken} {
		if token == "" {
			if err := keyring.Delete(Service, name+suffix); err != nil && !errors.Is(err, keyring.ErrNotFound) {
				return keyringUnavailable()
			}
			continue
		}
		if err := keyring.Set(Service, name+suffix, token); err != nil {
			return keyringUnavailable()
		}
	}
	retireLegacyEntry(ref)
	return nil
}

// retireLegacyEntry removes what a shell before this one stored under the bare
// reference. No shell reads such an entry any more (see Load), so leaving it
// would keep a refresh token on the machine that nothing can reach, and
// nothing can end. Best effort: the entry this store owns has already been
// written or removed, and that is the answer the caller gets.
func retireLegacyEntry(ref string) {
	for _, name := range []string{ref + accessTokenSuffix, ref + idTokenSuffix, ref} {
		_ = keyring.Delete(Service, name)
	}
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
	// The side entries go first and unconditionally: whether a session was
	// ended is the session entry's answer, and a side entry left behind
	// would be a token on the machine that nothing can reach any more.
	name := s.EntryName(ref)
	for _, suffix := range []string{accessTokenSuffix, idTokenSuffix} {
		if err := keyring.Delete(Service, name+suffix); err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return false, keyringUnavailable()
		}
	}
	retireLegacyEntry(ref)
	err := keyring.Delete(Service, name)
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
