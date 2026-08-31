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
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// whoamiUsage is the way back from a refused wso2 whoami invocation.
const whoamiUsage = "Run wso2 whoami [--context <name>] [--output table|json]."

// The session states wso2 whoami reports. They are the whole of what a caller
// can distinguish about a stored session without making a network call: none
// of them require asking the issuer anything.
const (
	// whoamiSessionNone is a selected context with no stored session for it,
	// or one whose stored entry is stale or foreign — Store.Load reports both
	// the same way, and neither leaves anything for this command to describe
	// beyond "log in".
	whoamiSessionNone = "none"
	// whoamiSessionPresent is a stored session whose refresh token has not
	// been disclosed to have lapsed: either the issuer named no lifetime for
	// it, or it named one still in the future. A session in this state may
	// still renew on the next command that needs it, whether or not its much
	// shorter-lived access token has itself expired — R7 (#112) is why that
	// quantity (session.Session.ExpiresAt) plays no part here: it carries no
	// doc comment of its own, and this package is where the reasoning is
	// recorded.
	whoamiSessionPresent = "present"
	// whoamiSessionExpired is a stored session whose issuer-disclosed
	// refresh-token lifetime has passed. Unlike whoamiSessionPresent, this
	// session cannot renew itself: whatever it could do expired along with it.
	whoamiSessionExpired = "expired"
)

// unknownSubject is what wso2 whoami reports for a session predating R6
// (#112), whose Subject field decodes to the empty string. It renders as this
// word in both table and JSON, never as a blank field:
// TestWhoamiRendersAPreR6SessionAsUnknownAndNotStated pins both renderings
// against a session written as raw JSON that never carries a subject member
// at all.
const unknownSubject = "unknown"

// sessionExpiryNotStated is what wso2 whoami reports when a stored session
// carries no SessionExpiresAt: the expected case per R7 (#112), not an error,
// and not a reason to substitute the access token's own, much shorter, expiry.
const sessionExpiryNotStated = "not stated by the issuer"

// unconfiguredRecovery is the way back from a machine with no context
// configured at all — the second half of the sentence table mode prints for
// that state below — used as whoamiReport.Recovery's initial value so a JSON
// caller reading Session == whoamiSessionNone always finds a Recovery,
// whichever of "nothing is configured" or "a context is configured but has no
// session" produced it. TestWhoamiOnAnUnconfiguredMachineReportsPlainly pins
// this for the unconfigured case specifically.
const unconfiguredRecovery = "Run wso2 login to create an identity and a context, " +
	"or wso2 context create <name> --identity <identity> if you already have one."

func (s Shell) whoamiCommand() *cobra.Command {
	return &cobra.Command{
		Use:                   "whoami",
		Short:                 "Show who is signed in, and to what context, identity, and session.",
		Args:                  noArguments(whoamiUsage),
		DisableFlagsInUseLine: true,
		RunE: func(command *cobra.Command, args []string) error {
			if _, err := forwardShellFlags(command, nil); err != nil {
				return err
			}
			return s.whoami(command)
		},
	}
}

// whoami reports the selected context, the identity it authenticates as, and
// what the stored session says about who is signed in — all read from local
// state, never from a network call.
func (s Shell) whoami(command *cobra.Command) error {
	mode, err := s.shellOutputMode(command)
	if err != nil {
		return err
	}
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	// Precedence duplicated from doctor.go rather than shared with it, for
	// whoami's own reason, distinct from doctor's: whoami needs to tell "no
	// context configured" (a state to report, exit 0) apart from "an
	// unresolvable --context name" (the caller's argument mistake, refused as
	// usage), and Shell.selectionAndDocument (internal/app/invoke.go:152)
	// returns only a combined error that cannot be told apart after the fact.
	// doctor.go duplicates the identical precedence for a different reason of
	// its own — it needs the document even when selection fails, to run its
	// context and secure-store checks against it (see doctor.go's doc
	// comment on doctor) — so the two commands share the code, not the
	// justification.
	contextName := ""
	if flag := shellFlag(command, contextFlag); flag != nil {
		contextName = flag.Value.String()
	}
	if contextName == "" {
		contextName = os.Getenv("WSO2_CONTEXT")
	}

	document, err := contexts.Load(root)
	if err != nil {
		return err
	}

	report := whoamiReport{Session: whoamiSessionNone, Recovery: unconfiguredRecovery}
	if len(document.Contexts) > 0 {
		selected, selErr := document.Select(contextName)
		if selErr != nil {
			// An unresolvable --context name is the caller's argument
			// mistake, refused the way every other context-selecting command
			// refuses it, rather than folded into the report as a state.
			return selErr
		}
		report.Configured = true
		report.Context = selected.Context.Name
		report.Identity = selected.Context.Identity
		report.Organization = selected.Context.Organization

		store := session.Store{StateRoot: root}
		stored, sessionErr := store.Load(selected.Identity.Auth.CredentialRef)
		switch {
		case sessionErr != nil && isNoSession(sessionErr):
			report.Recovery = sessionRecovery(sessionErr)
		case sessionErr != nil:
			// A secure store this command cannot even ask is not a state
			// whoami can report on; it is refused like any other command that
			// depends on the store being reachable.
			return sessionErr
		default:
			report.Subject = subjectOrUnknown(stored.Subject)
			report.Session, report.SessionExpiry, report.Recovery = sessionExpiryState(stored, time.Now())
		}
	}

	if mode == output.ModeJSON || report.Configured {
		return renderContext(s.Streams.Out, mode, report)
	}
	// Reported as a state rather than refused, and worded exactly as wso2
	// context current words it (context.go's contextCurrent): a machine
	// nobody has configured yet has done nothing wrong, and the two commands
	// must not invent two sentences for the one fact.
	_, err = fmt.Fprintln(s.Streams.Out,
		"No context is configured, so commands run against nothing.\n\n"+
			"Run wso2 login to create an identity and a context, "+
			"or wso2 context create <name> --identity <identity> if you already have one.")
	return err
}

// subjectOrUnknown reports a stored session's subject, or unknownSubject for
// one written before R6 (#112) added the field.
func subjectOrUnknown(subject string) string {
	if subject == "" {
		return unknownSubject
	}
	return subject
}

// sessionExpiryState reports what a present session's stored expiry means:
// whether it is not stated, still ahead of now, or already passed.
//
// A zero SessionExpiresAt is the expected case per R7 and is never read as
// "expired": an issuer that discloses nothing about a refresh token's
// lifetime has not said it lapsed, and treating silence as expiry would tell
// most users their healthy session is dead.
func sessionExpiryState(stored session.Session, now time.Time) (state, expiry, recovery string) {
	if stored.SessionExpiresAt.IsZero() {
		return whoamiSessionPresent, sessionExpiryNotStated, ""
	}
	formatted := stored.SessionExpiresAt.UTC().Format(time.RFC3339)
	if stored.SessionExpiresAt.Before(now) {
		return whoamiSessionExpired, formatted,
			"The issuer's disclosed refresh-token lifetime has passed. Run wso2 login to establish a fresh session."
	}
	return whoamiSessionPresent, formatted, ""
}

// sessionRecovery reads the way back a no-session error already carries,
// rather than restating it in a second sentence that could drift from the one
// session.Store itself gives.
func sessionRecovery(err error) string {
	var typed problem.Problem
	if errors.As(err, &typed) && typed.Recovery != "" {
		return typed.Recovery
	}
	return "Run wso2 login to establish a session for this context."
}

// whoamiReport is what wso2 whoami reports, in either rendering.
type whoamiReport struct {
	// Configured says whether a context is selected at all, exactly as
	// contextCurrent's own field does: a caller cannot read the difference
	// between "nothing is configured" and every other field's zero value out
	// of empty strings.
	Configured   bool   `json:"configured"`
	Context      string `json:"context"`
	Identity     string `json:"identity"`
	Organization string `json:"organization"`
	// Subject is unknownSubject for a pre-R6 session, and empty when there is
	// no session at all — see whoamiSessionNone.
	Subject string `json:"subject"`
	// Session is one of whoamiSessionNone, whoamiSessionPresent, or
	// whoamiSessionExpired.
	Session string `json:"session"`
	// SessionExpiry is an RFC 3339 timestamp when the issuer disclosed one,
	// sessionExpiryNotStated when it did not, or empty when Session is
	// whoamiSessionNone.
	SessionExpiry string `json:"sessionExpiry"`
	// Recovery is the way back, present exactly when Session is not
	// whoamiSessionPresent: whoamiSessionNone always sets it (to
	// unconfiguredRecovery when nothing is configured, or to the store's own
	// auth.login_required recovery via sessionRecovery when a context is
	// configured but has no session), and whoamiSessionExpired always sets it
	// via sessionExpiryState. TestWhoamiOnAnUnconfiguredMachineReportsPlainly
	// and TestWhoamiWithNoSessionNamesLogin each pin one of the two
	// whoamiSessionNone causes; TestWhoamiReportsAnExpiredSession pins the
	// expired case; TestWhoamiReportsAPresentSessionWithUndisclosedExpiry
	// pins the one case where it must be empty.
	Recovery string `json:"recovery,omitempty"`
}

func (w whoamiReport) fields() [][2]string {
	pairs := [][2]string{
		{"Context", w.Context},
		{"Identity", w.Identity},
		{"Organization", w.Organization},
		{"Subject", w.Subject},
		{"Session", w.Session},
		{"Session expiry", w.SessionExpiry},
	}
	if w.Recovery != "" {
		pairs = append(pairs, [2]string{"Recovery", w.Recovery})
	}
	return pairs
}
