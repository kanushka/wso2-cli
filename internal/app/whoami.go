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
	"strings"
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
	// whoamiSessionInline is what a client-credentials account reports for
	// itself and for every product it accesses: it acquires access with a
	// grant per command and holds nothing in the secure store, so "none" (a
	// state that invites "run wso2 login") would misdescribe a healthy
	// identity that never logs in at all.
	whoamiSessionInline = "inline"
	// whoamiSessionExchanged is what an exchanged product reports. It holds
	// no session of its own — its access is minted from the login session for
	// one command — so reporting "none" would read as "not logged in" and
	// send the reader to run a login that establishes nothing for it. Whether
	// the product can be reached is the login session's own state, reported
	// once, above.
	whoamiSessionExchanged = "exchanged"
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
const unconfiguredRecovery = "Run wso2 login to create an account and a context, " +
	"or wso2 context create <name> --account <account> if you already have one."

func (s Shell) whoamiCommand() *cobra.Command {
	command := &cobra.Command{
		Use:                   "whoami",
		Short:                 "Show who is signed in, and to what context, account, and session.",
		Args:                  noArguments(whoamiUsage),
		DisableFlagsInUseLine: true,
		RunE: func(command *cobra.Command, args []string) error {
			return s.whoami(command)
		},
	}
	// whoami reports ON a selected context exactly as doctor does (R5, #112),
	// so naming one with --context is meaningful for the same reason.
	declareContextFlag(command.Flags())
	declareOutputFlag(command.Flags())
	return command
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
		report.Identity = selected.Context.Account
		report.Organization = selected.Context.Organization

		store := session.Store{StateRoot: root}
		if selected.Identity.Auth.Kind == contexts.KindClientCredentials {
			// A client-credentials identity holds no bare session at all —
			// there is no credential reference to load one under — so it
			// reports the inline state rather than "none", which would
			// invite a wso2 login step this identity never takes.
			report.Session, report.Recovery = whoamiSessionInline, ""
		} else {
			stored, sessionErr := store.Load(selected.Identity.Auth.CredentialRef)
			switch {
			case sessionErr != nil && isNoSession(sessionErr):
				report.Recovery = sessionRecovery(sessionErr)
			case sessionErr != nil:
				// A secure store this command cannot even ask is not a state
				// whoami can report on; it is refused like any other command
				// that depends on the store being reachable.
				return sessionErr
			case !sessionServes(stored, selected.Identity.Auth.Issuer):
				report.Recovery = foreignSessionRecovery
			default:
				report.Subject = subjectOrUnknown(stored.Subject)
				report.Name = stored.Name
				report.Session, report.SessionExpiry, report.Recovery = sessionExpiryState(stored, time.Now())
			}
		}

		// Every record is reported, not only the ones Identity.Accesses
		// lists: Accesses skips a direct product that shares the login
		// session, and that product's state is exactly the login session's,
		// which whoami must still show under its own namespace. A product's
		// gateway record follows the product under its own key.
		for _, key := range selected.Identity.RecordKeys() {
			access, ok := selected.Identity.Access(key)
			if !ok {
				continue
			}
			entry := whoamiProduct{Namespace: access.Namespace, Strategy: access.Strategy, Session: whoamiSessionNone}
			if access.Strategy == contexts.StrategyInline {
				entry.Session = whoamiSessionInline
			} else if access.Strategy == contexts.StrategyExchanged {
				entry.Session = whoamiSessionExchanged
			} else if stored, err := store.Load(access.SessionRef); err == nil {
				if sessionServes(stored, access.Issuer) {
					entry.Session, entry.SessionExpiry, _ = sessionExpiryState(stored, time.Now())
				}
			} else if !isNoSession(err) {
				return err
			}
			report.Products = append(report.Products, entry)
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
			"Run wso2 login to create an account and a context, "+
			"or wso2 context create <name> --account <account> if you already have one.")
	return err
}

// sessionServes reports whether a stored session was established against the
// issuer the identity now names.
//
// One that was not is another deployment's session: the context's issuer
// changed after the login, or the entry was written for a different
// deployment under the same name. It is reported as no session at all, with
// the recovery below, the way sessionSource.renew refuses to present it to a
// module with auth.session_issuer_mismatch; whoami must not show a subject
// that the selected context cannot act as.
func sessionServes(stored session.Session, issuer string) bool {
	return stored.Issuer == issuer
}

// foreignSessionRecovery is what whoami advises when the stored session was
// established against a different issuer than the one the identity names.
const foreignSessionRecovery = "The stored session was established against a different identity provider " +
	"than the context now names. Run wso2 login to establish a session against it."

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
	Identity     string `json:"account"`
	Organization string `json:"organization"`
	// Subject is unknownSubject for a pre-R6 session, and empty when there is
	// no session at all — see whoamiSessionNone.
	Subject string `json:"subject"`
	// Name is the human-readable display name the login resolved and stored
	// (#168) — see session.Session.Name. It is empty, and absent from JSON,
	// when the session carries none: one stored before the field existed, or
	// one whose login learned no name claim. Nothing is put in its place,
	// because the subject is an identifier and not a name, and a reader of
	// name would take whatever it holds for the person.
	Name string `json:"name,omitempty"`
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
	// Products is every record the selected account declares — each product
	// and, under its gateway key, its gateway record — with what
	// Identity.Access says about how it is reached and what the secure store
	// says about its session. It is nil for an unconfigured machine or an
	// identity that declares no products.
	Products []whoamiProduct `json:"products,omitempty"`
}

// whoamiProduct is one product's access strategy and session state, as
// wso2 whoami reports it.
type whoamiProduct struct {
	Namespace string `json:"namespace"`
	Strategy  string `json:"strategy"`
	// Session is one of whoamiSessionNone, whoamiSessionPresent,
	// whoamiSessionExpired, or whoamiSessionInline.
	Session string `json:"session"`
	// SessionExpiry mirrors whoamiReport.SessionExpiry's own rules, for this
	// product's own session.
	SessionExpiry string `json:"sessionExpiry"`
}

func (w whoamiReport) fields() [][2]string {
	pairs := [][2]string{
		{"Context", w.Context},
		{"Account", w.Identity},
	}
	// Organization is left out when the context names none, for the reason
	// the Name row is below: a blank row reads as a value that failed to load.
	if w.Organization != "" {
		pairs = append(pairs, [2]string{"Organization", w.Organization})
	}
	// The Name row is left out, rather than shown blank, when the session
	// carries no name: the Subject row below already identifies who signed
	// in, and a blank Name would read as a value the shell failed to load.
	if w.Name != "" {
		pairs = append(pairs, [2]string{"Name", w.Name})
	}
	pairs = append(pairs, [][2]string{
		{"Subject", w.Subject},
		{"Session", w.Session},
		{"Session expiry", w.SessionExpiry},
		{"Products", w.productsField()},
	}...)
	if w.Recovery != "" {
		pairs = append(pairs, [2]string{"Recovery", w.Recovery})
	}
	return pairs
}

// productsField renders every record on one line, namespace order:
// "apim: federated, none; apim/gateway: sibling, present; iam: direct, present".
// A product that holds no session of its own, exchanged or inline, has no
// session state to add to its strategy, and is rendered "api: exchanged"
// rather than "api: exchanged, exchanged".
func (w whoamiReport) productsField() string {
	if len(w.Products) == 0 {
		return "none configured"
	}
	parts := make([]string, 0, len(w.Products))
	for _, product := range w.Products {
		if product.Session == whoamiSessionExchanged || product.Session == whoamiSessionInline {
			parts = append(parts, fmt.Sprintf("%s: %s", product.Namespace, product.Strategy))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s, %s", product.Namespace, product.Strategy, product.Session))
	}
	return strings.Join(parts, "; ")
}
