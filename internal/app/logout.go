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
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wso2/wso2-cli/internal/auth/oauthflow"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/result"
)

// revokeDeadline bounds the revocation round trip.
//
// It has to stay strictly shorter than the session lock's own deadline, which
// is 45 seconds in internal/auth/session/lock.go: the call happens inside the
// lock, so an issuer that merely hangs would otherwise turn a concurrent
// invocation's ordinary wait into a spurious refusal. The two constants live in
// packages that cannot import each other, so the coupling is carried by this
// comment — raising this one means raising that one. The same arrangement holds
// between that deadline and the broker's grant deadline.
var revokeDeadline = 20 * time.Second

// logoutSchema is the result shape wso2 logout reports.
const logoutSchema = "shell.logout/v1"

// logoutFlags are the flags wso2 logout owns.
type logoutFlags struct {
	contextName string
	mode        output.Mode
}

// logout ends every session the selected context's identity holds: the login
// session first, and then one per product that established a session of its
// own (internal/contexts/access.go's Identity.Accesses).
//
// A client-credentials identity is a special case with nothing to end: it
// acquires access inline, one grant per command, and never writes a session
// to the secure store, so logout reports that plainly and touches nothing —
// see logoutKindGate's own doc comment on why it is not refused instead.
//
// Ending a session is two separate acts on two separate copies of it: the
// issuer is asked to retract the refresh token, and the shell-owned entry is
// removed from the OS secure store. Only the second is guaranteed, so the
// report names what the first achieved rather than letting the command's name
// imply it. See docs/adr/0010-best-effort-revocation-on-session-end.md.
func (s Shell) logout(flags logoutFlags) error {
	selected, document, err := s.selectionAndDocument(flags.contextName)
	if err != nil {
		return err
	}
	if err := logoutKindGate.check(selected); err != nil {
		return err
	}

	if selected.Identity.Auth.Kind == contexts.KindClientCredentials {
		return s.reportLogout(flags.mode, selected, logoutOutcome{revocation: oauthflow.RevocationNotAttempted})
	}

	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	shared := document.ContextsUsingCredential(selected.Identity.Auth.CredentialRef)
	store := session.Store{StateRoot: root}

	accesses := selected.Identity.Accesses()
	login := accesses[0]
	// Recorded before the lock is taken, so a logout that then blocks on a
	// concurrent rotation still names what it was ending and against which
	// issuer the retraction was about to be attempted.
	s.log.Debug("ending a session",
		"context", selected.Context.Name,
		"issuer", login.Issuer,
		"client_id", login.ClientID,
		"credential_ref", login.SessionRef,
		"shared_contexts", len(shared))
	loginOutcome, err := s.endSession(store, login.SessionRef, login.Issuer, login.ClientID)
	if err != nil {
		return err
	}
	ended := logoutOutcome{
		sessionEnded: loginOutcome.sessionEnded,
		revocation:   loginOutcome.revocation,
		shared:       shared,
	}

	var products []string
	for _, access := range accesses[1:] {
		s.log.Debug("ending a product session",
			"context", selected.Context.Name,
			"namespace", access.Namespace,
			"issuer", access.Issuer,
			"client_id", access.ClientID,
			"credential_ref", access.SessionRef)
		outcome, err := s.endSession(store, access.SessionRef, access.Issuer, access.ClientID)
		if err != nil {
			return err
		}
		state := "none"
		if outcome.sessionEnded {
			state = "ended"
		}
		products = append(products, access.Namespace+" "+state)
	}
	ended.productSessions = products

	return s.reportLogout(flags.mode, selected, ended)
}

// sessionEndOutcome is what ending one stored session established: whether it
// removed anything, and what the issuer was told.
type sessionEndOutcome struct {
	sessionEnded bool
	revocation   oauthflow.Revocation
}

// endSession revokes and deletes one session reference against one issuer.
//
// The whole of it runs inside the store's own per-reference lock: the refresh
// token has to be read before it can be revoked and before the entry can go,
// and releasing the lock in between would leave a window where a concurrent
// wso2 login writes a fresh session that this then deletes — ending a session
// the user had just established. logout calls this once per session it ends,
// so each session's read, revoke, and delete stay under its own lock rather
// than one lock shared across every session of the identity.
func (s Shell) endSession(store session.Store, sessionRef, issuer, clientID string) (sessionEndOutcome, error) {
	outcome := sessionEndOutcome{revocation: oauthflow.RevocationNotAttempted}
	var refreshToken string
	err := store.WithLock(sessionRef, func() error {
		stored, loadErr := store.Load(sessionRef)
		switch {
		case loadErr == nil:
			refreshToken = stored.RefreshToken
		case isNoSession(loadErr):
			// Either nothing is stored, or what is stored cannot be read. Both
			// leave this command with no refresh token to revoke, and neither
			// is an error: the machine ends up in the state the user asked for,
			// and a second logout must not refuse for having succeeded the
			// first time. Which of the two it was is what Delete reports.
		default:
			// An unusable secure store is the one failure here, because the
			// shell can then neither read the session nor prove it is gone.
			return loadErr
		}
		if refreshToken != "" {
			ctx, cancel := context.WithTimeout(context.Background(), revokeDeadline)
			defer cancel()
			outcome.revocation = oauthflow.Revoke{
				Issuer:       issuer,
				ClientID:     clientID,
				RefreshToken: refreshToken,
			}.Run(ctx)
		}
		// The local entry goes whatever the issuer said. A user who asked to end
		// a session does not get to keep one because the deployment was
		// unreachable.
		//
		// What Delete removed is what decides whether a session was ended,
		// rather than what Load managed to read: an entry too stale to parse is
		// still a session on this machine, and reporting it as nothing stored
		// while removing it would describe a machine the user does not have.
		removed, deleteErr := store.Delete(sessionRef)
		outcome.sessionEnded = removed
		return deleteErr
	})
	if err != nil {
		return sessionEndOutcome{}, err
	}
	// Revocation is best effort by design, so which of its three outcomes a run
	// got is the fact a user reports and the one nothing else records: the
	// issuer's own copy of the session is not observable from this machine, and
	// "confirmed", "not attempted", and "failed" are three different states of
	// the deployment. See docs/adr/0010-best-effort-revocation-on-session-end.md.
	// The refresh token that was revoked is never logged; whether one was found
	// is.
	s.log.Debug("the session ended",
		"credential_ref", sessionRef,
		"revocation", string(outcome.revocation),
		"session_removed", outcome.sessionEnded,
		"refresh_token_found", refreshToken != "")
	return outcome, nil
}

// logoutOutcome is everything one logout established, which is what its report
// is made of.
//
// The three travel together because they are one answer to one question — what
// happened to this session — and separating them into arguments let a caller
// pass the revocation outcome of a session that was never there.
type logoutOutcome struct {
	// sessionEnded records whether there was a shell-owned session to remove.
	sessionEnded bool
	// revocation is what the issuer was established to have been told.
	revocation oauthflow.Revocation
	// shared names every context reaching this session, the selected one
	// included.
	shared []string
	// productSessions names, in namespace order, what happened to every
	// session beyond the login one: "<ns> ended" or "<ns> none" per product.
	// It is nil for a client-credentials identity and for one with no
	// product session beyond the login session.
	productSessions []string
}

// isNoSession reports whether the error is the store saying there is nothing
// stored, as opposed to saying it cannot be read at all.
//
// The distinction cannot be drawn from the problem code, because a stale entry
// and a missing one share auth.login_required by design. It does not need to
// be: both mean this command has no refresh token to revoke and nothing worth
// keeping, and only an unusable secure store is a reason to stop.
func isNoSession(err error) bool {
	var reported problem.Problem
	return errors.As(err, &reported) && reported.Code == "auth.login_required"
}

// reportLogout states what was ended and what the issuer was told.
//
// The fields are the machine-readable facts and carry the whole of what the
// command established. The prose is table-only, because a JSON caller reading a
// document must not find sentences wrapped around it, and because what the
// prose adds is an explanation of a field rather than a fact of its own.
func (s Shell) reportLogout(mode output.Mode, selected contexts.Selection,
	ended logoutOutcome) error {
	state := "none"
	if ended.sessionEnded {
		state = "ended"
	}
	productSessions := "none"
	if len(ended.productSessions) > 0 {
		productSessions = strings.Join(ended.productSessions, ", ")
	}
	reported := result.New(logoutSchema).
		With("context", "Context", selected.Context.Name).
		With("identity", "Identity", selected.Context.Identity).
		With("session", "Session", state).
		With("revocation", "Revocation", string(ended.revocation)).
		With("productSessions", "Product sessions", productSessions).
		With("sharedContexts", "Shared with", strings.Join(ended.shared, ", ")).
		// A constant today, and a field rather than prose because it is the one
		// thing users read into this command that is not true, and the table
		// note that explains it does not reach a JSON caller. It stops being
		// constant if the shell ever ends the provider's browser session too.
		With("browserSession", "Browser session", "unaffected")

	if mode == output.ModeJSON {
		return output.Report(s.Streams.Out, mode, reported)
	}
	headline := fmt.Sprintf("\nNo session was stored for the %q context.\n", selected.Context.Name)
	if ended.sessionEnded {
		headline = fmt.Sprintf("\nEnded the session for the %q context.\n", selected.Context.Name)
	}
	if _, err := fmt.Fprint(s.Streams.Out, headline); err != nil {
		return err
	}
	if err := output.Report(s.Streams.Out, mode, reported); err != nil {
		return err
	}
	for _, line := range logoutNotes(ended) {
		if _, err := fmt.Fprintf(s.Streams.Out, "\n%s\n", line); err != nil {
			return err
		}
	}
	return nil
}

// logoutNotes explains what the revocation outcome means, and what ending a
// session does not do.
func logoutNotes(ended logoutOutcome) []string {
	var notes []string
	switch {
	case !ended.sessionEnded:
		// Nothing was revoked because nothing was there to revoke. Explaining
		// the revocation outcome here would describe a request never made.
	case ended.revocation == oauthflow.RevocationConfirmed:
		notes = append(notes, "The identity provider accepted the request to revoke this "+
			"session's refresh token.")
	case ended.revocation == oauthflow.RevocationNotAttempted:
		notes = append(notes, "The identity provider publishes no revocation endpoint, so it was "+
			"not asked to retract its own copy of this session.")
	case ended.revocation == oauthflow.RevocationFailed:
		notes = append(notes, "The identity provider did not accept the request to revoke this "+
			"session's refresh token, so its own copy of the session may remain usable until it "+
			"expires.")
	}
	if ended.sessionEnded {
		// Said under every outcome, because it is true under every outcome and
		// because the command's name invites the opposite conclusion.
		notes = append(notes, "A browser single-sign-on session at the identity provider is "+
			"unaffected by this command, so a later login may not prompt for credentials.")
	}
	if len(ended.shared) > 1 {
		notes = append(notes, fmt.Sprintf("These contexts share one session and are all affected: %s.",
			strings.Join(ended.shared, ", ")))
	}
	return notes
}

// logoutUsageRecovery is the way back from every wso2 logout usage refusal.
const logoutUsageRecovery = "Run wso2 logout [--context <name>] [--output table|json]."
