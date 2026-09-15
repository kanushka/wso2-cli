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
	"strings"

	"github.com/wso2/wso2-cli/internal/auth/oauthflow"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// documentChange is one change to the context document, as a function of the
// document it is made to. It is called once, to plan; the write replays the
// planned result, and refuses when the document changed in between.
type documentChange func(contexts.Document) (contexts.Document, error)

// endedSession is one session a change ended: which record held it, whether
// an entry was there to remove, and what its issuer was told.
type endedSession struct {
	Context    string `json:"context"`
	Record     string `json:"record"`
	Session    string `json:"session"`
	Revocation string `json:"revocation"`
}

// plannedChange is what a change would do, worked out without doing any of it.
type plannedChange struct {
	// before and after are the document as it is and as the change leaves it.
	before, after contexts.Document
	// ending is every session the change leaves unreached, or reached for
	// something it was not authorized for (contexts.SessionsUnreachedBy).
	ending []contexts.ProductAccess
}

// planChange applies the change to the document as it is now and works out
// which sessions it ends. Nothing is written and nothing is ended.
//
// The result is encoded, which validates it, so a change the document would
// refuse is refused here, before any session is touched.
func (s Shell) planChange(root string, change documentChange) (plannedChange, error) {
	// Asked first: a document this shell reads and never writes must be
	// refused before anything is planned against it, let alone ended.
	if err := contexts.Writable(root); err != nil {
		return plannedChange{}, s.explainWriteRefusal(root, err)
	}
	before, err := contexts.Load(root)
	if err != nil {
		return plannedChange{}, err
	}
	after, err := change(before)
	if err != nil {
		return plannedChange{}, err
	}
	if _, err := after.Encode(); err != nil {
		return plannedChange{}, explainChangeRefusal(err)
	}
	return plannedChange{before: before, after: after, ending: before.SessionsUnreachedBy(after)}, nil
}

// writeChange ends every session the change unbinds and then writes the
// change, in that order.
//
// The order is the point. The secure store offers no way to list what it
// holds, so a session whose record is gone can never be found, ended or
// revoked again; and a session whose record now asks for another URL,
// audience or grant would be presented for something it was never authorized
// for. Ending the sessions first leaves, at worst, a context with no session,
// which a login fixes. The reverse order would leave secrets no document names.
//
// Sessions are ended outside the document lock, because that lock admits no
// network call and revocation is one (ADR 0010, best effort). The change is
// then planned again under the lock, and written only if every session it
// now ends is one this run ended and is still gone: another invocation may
// have logged in or changed the document in between.
func (s Shell) writeChange(root string, plan plannedChange) ([]endedSession, error) {
	store := session.Store{StateRoot: root}
	ended := map[string]bool{}
	var report []endedSession
	for _, access := range plan.ending {
		outcome, err := s.endSession(store, access.SessionRef, access.Issuer, access.ClientID)
		if err != nil {
			return report, err
		}
		ended[access.SessionRef] = true
		state := "none"
		if outcome.sessionEnded {
			state = "ended"
		}
		report = append(report, endedSession{
			Context: owningContext(plan.before, access.SessionRef), Record: access.Namespace,
			Session: state, Revocation: string(outcome.revocation),
		})
	}
	err := contexts.Update(root, func(current contexts.Document) (contexts.Document, error) {
		next, err := replay(plan, current)
		if err != nil {
			return current, err
		}
		for _, access := range current.SessionsUnreachedBy(next) {
			if !ended[access.SessionRef] {
				return current, documentChangedDuringChange()
			}
			present, err := store.Stored(access.SessionRef)
			if err != nil {
				return current, err
			}
			if present {
				return current, documentChangedDuringChange()
			}
		}
		return next, nil
	})
	if err != nil {
		return report, s.explainWriteRefusal(root, err)
	}
	return report, nil
}

// replay is the change as planned, made again to the document under the lock.
// A document that did not change since the plan takes the planned result as it
// is; one that did is refused rather than merged, because the plan's sessions
// were ended against the document as it was.
func replay(plan plannedChange, current contexts.Document) (contexts.Document, error) {
	before, err := plan.before.Encode()
	if err != nil {
		return contexts.Document{}, err
	}
	now, err := current.Encode()
	if err != nil {
		return contexts.Document{}, err
	}
	if string(before) != string(now) {
		return contexts.Document{}, documentChangedDuringChange()
	}
	return plan.after, nil
}

// owningContext names the context whose credential reference a session ref
// lives under.
func owningContext(document contexts.Document, sessionRef string) string {
	for _, candidate := range document.Contexts {
		ref := candidate.CredentialRef
		if ref != "" && (sessionRef == ref || strings.HasPrefix(sessionRef, ref+".")) {
			return candidate.Name
		}
	}
	return ""
}

// endingLines describes, one line per session, what a change will end. It is
// what --dry-run prints before anything is ended.
func endingLines(document contexts.Document, ending []contexts.ProductAccess) []string {
	lines := make([]string, 0, len(ending))
	for _, access := range ending {
		record := access.Namespace
		if record == "" {
			record = "login"
		}
		lines = append(lines, fmt.Sprintf("%s: %s session at %s", owningContext(document, access.SessionRef),
			record, access.Issuer))
	}
	return lines
}

// endedNotes explains the revocation outcomes that claim less than a
// confirmed one.
func endedNotes(ended []endedSession) []string {
	var notes []string
	for _, session := range ended {
		if session.Session != "ended" {
			continue
		}
		record := session.Record
		if record == "" {
			record = "login"
		}
		switch oauthflow.Revocation(session.Revocation) {
		case oauthflow.RevocationNotAttempted:
			notes = append(notes, fmt.Sprintf("The identity provider publishes no revocation endpoint, "+
				"so it was not asked to retract its own copy of the %s session.", record))
		case oauthflow.RevocationFailed:
			notes = append(notes, fmt.Sprintf("The identity provider did not accept the request to "+
				"revoke the %s session's refresh token, so its own copy of that session may remain "+
				"usable until it expires.", record))
		}
	}
	return notes
}

// documentChangedDuringChange refuses to write when the document changed
// between ending the sessions and writing.
func documentChangedDuringChange() problem.Problem {
	return problem.New(problem.CategoryUsage, "contexts.document_busy",
		"the context document changed while the change's sessions were being ended").
		WithRecovery("Nothing was written, and the sessions already ended stay ended. Retry the command.")
}

// explainChangeRefusal replaces a document refusal's generic recovery with one
// that says what this command did not do. The default recovery offers to
// remove the whole document, which would destroy every context over one change
// that was simply refused. A refusal carrying advice of its own keeps it.
func explainChangeRefusal(err error) error {
	var typed problem.Problem
	if !errors.As(err, &typed) || !contexts.CarriesDefaultDocumentRecovery(err) {
		return err
	}
	return problem.New(problem.CategoryUsage, "shell.invalid_argument", typed.Message).
		WithRecovery("No session was ended and the context document was not changed. " +
			"Run wso2 context show to see what the context records.")
}
