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
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/internal/auth/oauthflow"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// identityRemoveProductUsage is the way back from every wso2 account
// remove-product usage refusal.
const identityRemoveProductUsage = "Run wso2 account remove-product <account> <namespace> " +
	"[--output table|json], where <namespace> is a product or <namespace>/gateway for its gateway record alone."

// The login-session outcomes wso2 account remove-product reports. The login
// session is never ended by this command; what varies is only whether it
// still answers for what the account logs in as.
const (
	// loginSessionKept: the login session was left in place and still covers
	// the product the account logs in as.
	loginSessionKept = "kept"
	// loginSessionNone: the account acquires access inline and holds no
	// session at all.
	loginSessionNone = "none"
)

func (s Shell) identityRemoveProductCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "remove-product <account> <namespace>",
		Short: "Stop an account reaching a product, ending the product's own session first.",
		Args: exactlyTwoArguments("an account and a product namespace",
			identityRemoveProductUsage),
		RunE: func(command *cobra.Command, args []string) error {
			return s.identityRemoveProduct(command, args[0], args[1])
		},
	}
}

// identityRemoveProduct drops one record from an account, ending and revoking
// every session the removal leaves nothing to reach, before the record goes.
//
// The order is the point of the command. A product reached by a session of
// its own keeps that session in the OS secure store under the product's own
// reference, and wso2 logout decides what to end by walking the account's
// records. Once a record is gone its session can never be ended by the shell
// again, and it cannot be found either: the secure store answers get, set and
// delete by name and offers no way to list what it holds. Ending the session
// in a later cleanup is therefore not possible, only ending it first.
//
// Which sessions go is decided by contexts.Document.SessionsUnreachedBy, not
// by the removed product's strategy alone. A product sharing the login
// session yields nothing to end, because the account still logs in through
// it; ending "its" session would log the user out of everything else. An
// exchanged product, and every product of a client-credentials account, holds
// no session, so neither does it. Removing the login product itself can make
// another product the login one, and that product's own sibling session is
// then named by nothing: it is ended too, while the login session stays.
//
// Every refusal that can be answered without the network is answered before
// any session is ended: an unknown account, a record the account does not
// hold, a document this shell does not write, and a removal the document
// would refuse. That is the ordering wso2 login follows for the same reason
// (ADR 0012): what a failure leaves behind should be nothing a user has to
// undo by hand. Ending a session is the one network call, and it is logout's
// own path, so revocation is best effort exactly as ADR 0010 decides and the
// command succeeds whatever the issuer answers.
func (s Shell) identityRemoveProduct(command *cobra.Command, account, key string) error {
	mode, err := s.shellOutputMode(command)
	if err != nil {
		return err
	}
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	// Asked before anything is ended, rather than left to Update: a version 1
	// document is one this shell reads and never writes, and learning that
	// after the sessions were gone would leave every record in place with
	// nothing behind it.
	if err := contexts.Writable(root); err != nil {
		return s.explainWriteRefusal(root, err)
	}
	document, err := contexts.Load(root)
	if err != nil {
		return err
	}
	plan, err := planRemoval(document, account, key)
	if err != nil {
		return err
	}
	// Encode validates, so a removal the document would refuse — the last
	// product of an account whose deployment binds a login to one — is
	// refused here, while every session it would have ended still stands.
	if _, err := plan.next.Encode(); err != nil {
		return explainRemovalRefusal(err)
	}

	s.log.Debug("removing a record from an account",
		"account", account, "record", key,
		"records", strings.Join(plan.records, ","),
		"sessions_to_end", len(plan.unreached),
		"document", contexts.Path(root))

	removed := productRemoved{
		Account:      account,
		Namespace:    key,
		Records:      plan.records,
		Sessions:     []removedSession{},
		LoginSession: plan.loginSession,
	}
	store := session.Store{StateRoot: root}
	ended := map[string]bool{}
	for _, access := range plan.unreached {
		// logout's own path: the refresh token is read, revoked at the issuer
		// and deleted from the secure store under the reference's lock. An
		// unusable secure store stops the command here, before the record is
		// dropped, so whatever was not ended is still named by its record.
		outcome, err := s.endSession(store, access.SessionRef, access.Issuer, access.ClientID)
		if err != nil {
			return err
		}
		ended[access.SessionRef] = true
		state := "none"
		if outcome.sessionEnded {
			state = "ended"
		}
		removed.Sessions = append(removed.Sessions, removedSession{
			Record: access.Namespace, Session: state, Revocation: string(outcome.revocation),
		})
	}

	// The plan is made again from the document as it is under the lock. The
	// sessions were ended outside it, because the document lock admits no
	// network call, so another invocation may have changed the account in
	// between. Dropping the record is safe only if every session the removal
	// now leaves unreached is one this command ended AND is still gone.
	//
	// Both halves are needed. Ending a session releases that session's lock
	// before this one is taken, so a wso2 login for the same product can store
	// a new session under the very reference just ended. Asking only whether
	// this run ended the reference would pass it and strand the new session,
	// which nothing could find again once its record is gone. Asking the store
	// is a local read, which the document lock allows.
	err = contexts.Update(root, func(current contexts.Document) (contexts.Document, error) {
		replanned, err := planRemoval(current, account, key)
		if err != nil {
			return current, err
		}
		for _, access := range replanned.unreached {
			if !ended[access.SessionRef] {
				return current, documentChangedDuringRemoval()
			}
			present, err := store.Stored(access.SessionRef)
			if err != nil {
				return current, err
			}
			if present {
				return current, documentChangedDuringRemoval()
			}
		}
		return replanned.next, nil
	})
	if err != nil {
		return s.explainWriteRefusal(root, err)
	}
	return s.reportRemoval(mode, removed, plan)
}

// removalPlan is what removing one record from one account would do.
type removalPlan struct {
	// next is the document with the record removed.
	next contexts.Document
	// records names every record the removal drops, in the account's own
	// record order: a product and, after it, its gateway record.
	records []string
	// unreached is every session the removal leaves nothing to reach, which
	// has to be ended before next is written.
	unreached []contexts.ProductAccess
	// loginSession is one of the loginSession* outcomes.
	loginSession string
	// login is what the account logs in for once the record is gone.
}

// planRemoval works out a removal without performing any of it.
func planRemoval(document contexts.Document, account, key string) (removalPlan, error) {
	position := slices.IndexFunc(document.Accounts, func(candidate contexts.Account) bool {
		return candidate.Name == account
	})
	if position < 0 {
		return removalPlan{}, unknownIdentity(account, len(document.Accounts) > 0)
	}
	declared := document.Accounts[position]
	if login := declared.LoginAccess(); declared.Auth.Kind != contexts.KindClientCredentials &&
		login.Namespace != "" && key == login.Namespace {
		// Refused here, before any session is ended or any record dropped. The
		// login session was authorized for this product's resource and scope
		// set; with the product gone it would answer for one the account no
		// longer records, and every command against whichever product became
		// the login next would be refused until the next login. A client-
		// credentials account holds no login session, so there is nothing to
		// protect and the rule does not apply. Only the product's own record is
		// the login: its gateway key is a different record and is removable.
		return removalPlan{}, loginProductRemoval(declared, key)
	}
	remaining, removed := declared.WithoutRecord(key)
	if !removed {
		return removalPlan{}, recordNotHeld(account, key, declared.RecordKeys())
	}
	kept := remaining.RecordKeys()
	records := slices.DeleteFunc(declared.RecordKeys(), func(candidate string) bool {
		return slices.Contains(kept, candidate)
	})
	// The accounts slice is cloned before the one entry is replaced: a
	// document passed by value still shares its backing array with the
	// caller's, and the caller's is the document the unreached sessions are
	// judged against.
	next := document
	next.Accounts = slices.Clone(document.Accounts)
	next.Accounts[position] = remaining

	plan := removalPlan{
		next:         next,
		records:      records,
		unreached:    document.SessionsUnreachedBy(next),
		loginSession: loginSessionKept,
	}
	// The login session always keeps answering for what the account logs in
	// as: the login product itself is refused above, so nothing removed here
	// can change which product that is.
	if declared.Auth.Kind == contexts.KindClientCredentials {
		plan.loginSession = loginSessionNone
	}
	return plan, nil
}

// loginProductRemoval refuses to remove the product an account logs in
// through, and says what a person can do instead.
//
// The recovery names commands that do the job. No command changes an
// account's login product — it is fixed when the account first records one, so
// a product recorded later cannot displace it — so sending the reader to wso2
// login would name a command that cannot help. What does work is an account
// that logs in through the other product from the start.
func loginProductRemoval(account contexts.Account, key string) error {
	others := slices.DeleteFunc(account.RecordKeys(), func(candidate string) bool {
		return candidate == key
	})
	recovery := fmt.Sprintf("To log in through another product, create an account that records it "+
		"first: wso2 account create <name> --issuer %s --client-id %s --product <namespace> "+
		"--endpoint <url> --audience <uri>, then wso2 login --context <name>.",
		account.Auth.Issuer, account.Auth.ClientID)
	if len(others) > 0 {
		recovery += fmt.Sprintf(" The account's other records can be removed: %s.", strings.Join(others, ", "))
	}
	return problem.New(problem.CategoryUsage, "contexts.login_product",
		fmt.Sprintf("the account %q logs in through %q, so removing it would leave the login session "+
			"authorized for a product the account no longer records", account.Name, key)).
		WithRecovery(recovery)
}

// reportRemoval states what was removed, what each ended session's issuer was
// told, and what became of the login session.
func (s Shell) reportRemoval(mode output.Mode, removed productRemoved, plan removalPlan) error {
	if mode == output.ModeJSON {
		return renderContext(s.Streams.Out, mode, removed)
	}
	line := fmt.Sprintf("Removed product %q from account %q.", removed.Namespace, removed.Account)
	if namespace, gateway := contexts.SplitGatewayKey(removed.Namespace); gateway {
		line = fmt.Sprintf("Removed the gateway record of product %q from account %q.", namespace, removed.Account)
	}
	if _, err := fmt.Fprintf(s.Streams.Out, "\n%s\n", line); err != nil {
		return err
	}
	if err := renderContext(s.Streams.Out, mode, removed); err != nil {
		return err
	}
	for _, note := range removalNotes(removed, plan) {
		if _, err := fmt.Fprintf(s.Streams.Out, "\n%s\n", note); err != nil {
			return err
		}
	}
	return nil
}

// removalNotes explains the revocation outcomes that claim less than a
// confirmed one, and a login session that no longer covers what the account
// logs in as. They are table-only for the reason logout's notes are: each
// explains a field, and the field is what a JSON caller reads.
func removalNotes(removed productRemoved, plan removalPlan) []string {
	var notes []string
	for _, ended := range removed.Sessions {
		if ended.Session != "ended" {
			continue
		}
		switch oauthflow.Revocation(ended.Revocation) {
		case oauthflow.RevocationNotAttempted:
			notes = append(notes, fmt.Sprintf("The identity provider publishes no revocation endpoint, "+
				"so it was not asked to retract its own copy of the %s session.", ended.Record))
		case oauthflow.RevocationFailed:
			notes = append(notes, fmt.Sprintf("The identity provider did not accept the request to "+
				"revoke the %s session's refresh token, so its own copy of that session may remain "+
				"usable until it expires.", ended.Record))
		}
	}
	return notes
}

// recordNotHeld refuses to remove a record the account does not hold, naming
// every one it does, so the correction is in the refusal rather than one
// more command away.
func recordNotHeld(account, key string, recorded []string) problem.Problem {
	holds := "It records no products."
	if len(recorded) > 0 {
		holds = "It records " + strings.Join(recorded, ", ") + "."
	}
	return problem.New(problem.CategoryUsage, "contexts.unknown_product",
		fmt.Sprintf("the account %q records nothing under %q", account, key)).
		WithRecovery(holds + " Run wso2 account list to see what each one reaches.")
}

// documentChangedDuringRemoval refuses to drop a record when the document
// changed, between ending the sessions and writing, in a way that would leave
// a session behind: either the document now names a session this run did not
// end, or a session was stored again under one it did.
func documentChangedDuringRemoval() problem.Problem {
	return problem.New(problem.CategoryUsage, "contexts.document_busy",
		"the context document changed while the product's sessions were being ended").
		WithRecovery("The record was not removed, and the sessions already ended stay ended. " +
			"Retry the command.")
}

// explainRemovalRefusal replaces a document refusal's generic recovery with
// one that says what this command did not do. The default recovery offers to
// remove the whole document, which here would destroy every account over one
// removal that was simply refused. See explainProductRefusal for why only the
// generic recovery is replaced and the message never is.
func explainRemovalRefusal(err error) error {
	var typed problem.Problem
	if !errors.As(err, &typed) || !contexts.CarriesDefaultDocumentRecovery(err) {
		return err
	}
	return problem.New(problem.CategoryUsage, "shell.invalid_argument", typed.Message).
		WithRecovery("No session was ended and the context document was not changed. " +
			"Run wso2 account list to see what the account records.")
}

// The result wso2 account remove-product reports, rendered the way the rest
// of the family renders its own.
type (
	productRemoved struct {
		Account string `json:"account"`
		// Namespace is the record that was asked for: a product namespace,
		// or a gateway key.
		Namespace string `json:"namespace"`
		// Records names every record removed, a product's gateway record
		// after the product.
		Records []string `json:"records"`
		// Sessions is every session the removal ended, in the order they
		// were ended, and empty when the removal left nothing unreached.
		Sessions []removedSession `json:"sessions"`
		// LoginSession is one of the loginSession* outcomes.
		LoginSession string `json:"loginSession"`
	}

	// removedSession is one session the removal ended: whether an entry was
	// there to remove, and what its issuer was told.
	removedSession struct {
		Record     string `json:"record"`
		Session    string `json:"session"`
		Revocation string `json:"revocation"`
	}
)

func (p productRemoved) fields() [][2]string {
	sessions := "none"
	if len(p.Sessions) > 0 {
		parts := make([]string, 0, len(p.Sessions))
		for _, ended := range p.Sessions {
			if ended.Session == "ended" {
				parts = append(parts, fmt.Sprintf("%s (ended, revocation %s)", ended.Record, ended.Revocation))
			} else {
				parts = append(parts, ended.Record+" (nothing stored)")
			}
		}
		sessions = strings.Join(parts, ", ")
	}
	return [][2]string{
		{"Account", p.Account},
		{"Product", p.Namespace},
		{"Records removed", strings.Join(p.Records, ", ")},
		{"Sessions ended", sessions},
		{"Login session", p.LoginSession},
	}
}
