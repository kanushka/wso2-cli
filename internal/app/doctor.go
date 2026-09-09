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
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// doctorUsage is the way back from a refused wso2 doctor invocation.
const doctorUsage = "Run wso2 doctor [--context <name>] [--online] [--output table|json]."

// doctorOnlineFlag is wso2 doctor's own flag, declared on the command rather
// than a shell-owned one: no other command has a reason to gate a network
// call this way.
const doctorOnlineFlag = "online"

// The check names doctor reports, and the names findings and tests key on.
const (
	checkContext     = "context"
	checkSecureStore = "secure-store"
	checkSession     = "session"
	checkCatalog     = "catalog"
)

// The outcomes a check reports.
//
// none is the session check's word for a configured context nobody is logged
// in to. That state needed a fourth word because the other three each say
// something untrue of it: pass claims a session exists, fail claims the
// machine is broken when being logged out is the state a confirmed
// wso2 logout deliberately leaves behind, and not-applicable claims the check
// could not be asked, when in fact it ran and established the absence.
const (
	statusPass          = "pass"
	statusFail          = "fail"
	statusNotApplicable = "not-applicable"
	statusNone          = "none"
)

// severityRank orders the checks whose failure can decide the exit status,
// most severe first, per R1 (#112, #121).
//
// This is a rank the command defines for choosing WHICH failing check decides
// the exit status. It is not the numeric order of the exit classes those
// checks carry, and reading it off that order would be wrong: secure-store and
// session both carry exit.AuthPolicy while context carries exit.Usage, so two
// of the three share a class and the third has a smaller number despite
// ranking in the middle. TestDoctorRanksTheDocumentAboveAnAbsentSession and
// TestMostSevereFailure (doctor_internal_test.go) pin this against cases where
// the numeric class of a lower-ranked failure is larger.
//
// catalog is ranked last, below every unconditional check: it is the only
// optional check (--online) and the only one whose failure may be the
// network's rather than the machine's, so a real machine problem always
// outranks it. Its own exit class, exit.ModuleProcess (70), is already
// defined and already documented in docs/reference/commands.md's exit-class
// table — Global Constraint 2's "no new exit class" bars minting a class that
// table does not already carry, not reusing one that it does.
var severityRank = []string{checkSecureStore, checkContext, checkSession, checkCatalog}

// catalogProbeTimeout bounds the --online catalog check, so a doctor run
// cannot hang on an unreachable origin.
const catalogProbeTimeout = 10 * time.Second

// doctorFinding is what one check reports. Both renderings walk the same
// slice of these, so they cannot disagree about which checks ran or what each
// one found.
type doctorFinding struct {
	Check    string `json:"check"`
	Status   string `json:"status"`
	Detail   string `json:"detail"`
	Recovery string `json:"recovery,omitempty"`
}

// doctorReport is what wso2 doctor --output json publishes.
type doctorReport struct {
	Checks []doctorFinding `json:"checks"`
}

func (s Shell) doctorCommand() *cobra.Command {
	var online bool
	command := &cobra.Command{
		Use:                   "doctor",
		Short:                 "Check the shell's context, secure-store, and session health.",
		Args:                  noArguments(doctorUsage),
		DisableFlagsInUseLine: true,
		RunE: func(command *cobra.Command, args []string) error {
			return s.doctor(command, online)
		},
	}
	command.Flags().BoolVar(&online, doctorOnlineFlag, false,
		"Also check module catalog reachability, which requires a network connection.")
	// doctor reports ON a selected context, so naming one with --context is
	// meaningful, and its findings are read by scripts as much as by a person.
	declareContextFlag(command.Flags())
	declareOutputFlag(command.Flags())
	return command
}

// doctor runs every check, reports every finding, and reports the exit status
// of the most severe failing one.
//
// The report is written before every check outcome is known to be final, but
// not before every return in this function: shellOutputMode and s.stateRoot
// failing (nothing to check against yet) and an unresolvable --context
// (`return selErr` below, the caller's argument mistake rather than a health
// fact — see contextUse and contextCurrent, which refuse the same way) all
// return before a single finding exists, so `wso2 doctor --context nosuch
// --output json` exits 64 with no JSON at all. Once the checks start running,
// every one of them completes and is rendered before this returns, on a
// failing run as much as a passing one, so a caller reading --output json can
// always read the findings off a run that got that far.
func (s Shell) doctor(command *cobra.Command, online bool) error {
	mode, err := s.shellOutputMode(command)
	if err != nil {
		return err
	}
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	// --context wins over WSO2_CONTEXT, which wins over the document's default
	// context — the same precedence Shell.selectionAndDocument applies for
	// wso2 login and wso2 logout (internal/app/invoke.go:152). It is
	// duplicated rather than reused because this command needs the document
	// even when selection fails (to run the context and secure-store checks
	// against it), while selectionAndDocument returns only a combined error
	// that cannot be told apart from a load failure.
	// TestDoctorHonorsContextPrecedence pins that this stays in step with
	// selectionAndDocument's own precedence.
	contextName := ""
	if flag := shellFlag(command, contextFlag); flag != nil {
		contextName = flag.Value.String()
	}
	if contextName == "" {
		contextName = os.Getenv("WSO2_CONTEXT")
	}

	document, loadErr := contexts.Load(root)

	failures := make(map[string]problem.Problem, len(severityRank))
	var findings []doctorFinding

	switch {
	case loadErr != nil:
		typed := doctorProblem(loadErr)
		failures[checkContext] = typed
		findings = append(findings, failFinding(checkContext, typed))
	case len(document.Contexts) == 0:
		findings = append(findings, notApplicableFinding(checkContext,
			"no context document is configured"))
	default:
		findings = append(findings, passFinding(checkContext, "the context document is valid"))
	}

	store := session.Store{StateRoot: root}
	// The secure-store probe never reads the context document, so its answer
	// is a real fact about the machine whether or not the document is
	// readable. It only becomes not-applicable on the fresh machine R2
	// exempts: no document, or one with no contexts declared.
	secureStoreApplicable := loadErr != nil || len(document.Contexts) > 0
	if !secureStoreApplicable {
		findings = append(findings, notApplicableFinding(checkSecureStore,
			"no context is configured, so the secure store was not probed"))
	} else if probeErr := store.Probe(); probeErr != nil {
		typed := doctorProblem(probeErr)
		failures[checkSecureStore] = typed
		findings = append(findings, failFinding(checkSecureStore, typed))
	} else {
		findings = append(findings, passFinding(checkSecureStore, "the OS secure store is reachable"))
	}

	switch {
	case loadErr != nil:
		// No identity can be read from a document that failed to decode or
		// validate, so there is no credential reference to ask the store
		// about. That is a fact this command cannot establish, not a fact
		// that it is absent: Store.Load("") would always report "no session",
		// regardless of the actual machine, because nothing is ever stored
		// under an empty reference. Reporting that fixed answer as a failure
		// would tell a user with a perfectly good session to log in again
		// over an unrelated document typo, so this is not-applicable instead.
		findings = append(findings, notApplicableFinding(checkSession,
			"the context document could not be read, so no credential reference could be resolved"))
	case len(document.Contexts) == 0:
		findings = append(findings, notApplicableFinding(checkSession,
			"no context is configured, so there is no session to check"))
	default:
		selected, selErr := document.Select(contextName)
		if selErr != nil {
			// An unresolvable --context name is the caller's argument
			// mistake, not a health finding: it is refused the way every
			// other context-selecting command refuses it, rather than
			// folded into the report. See this function's doc comment.
			return selErr
		}
		ref := selected.Identity.Auth.CredentialRef
		stored, storedErr := store.Stored(ref)
		switch {
		case storedErr != nil:
			typed := doctorProblem(storedErr)
			failures[checkSession] = typed
			findings = append(findings, failFinding(checkSession, typed))
		case !stored:
			// A context nobody is logged in to is a normal state, not a
			// health fault: a confirmed wso2 logout leaves exactly this
			// machine behind, and reporting it as fail made a wrapper
			// watching doctor alert on a deliberate action. The login
			// pointer stays in the recovery column, but the run exits 0.
			// Only absence is normal: an entry that exists but cannot be
			// used still fails below.
			findings = append(findings, noneFinding(checkSession,
				"no login session is stored for the selected context",
				"Run wso2 login to establish a session for this context."))
		default:
			if _, sessionErr := store.Load(ref); sessionErr != nil {
				typed := doctorProblem(sessionErr)
				failures[checkSession] = typed
				findings = append(findings, failFinding(checkSession, typed))
			} else {
				findings = append(findings, passFinding(checkSession, "a stored session exists for the selected context"))
			}
		}
	}

	if online {
		finding, catalogErr := catalogCheck(root, s.log)
		findings = append(findings, finding)
		if catalogErr != nil {
			failures[checkCatalog] = *catalogErr
		}
	}

	if writeErr := renderDoctorReport(s.Streams.Out, mode, findings); writeErr != nil {
		return writeErr
	}
	return mostSevereFailure(failures)
}

// renderDoctorReport writes every finding, in table or JSON form.
func renderDoctorReport(w io.Writer, mode output.Mode, findings []doctorFinding) error {
	if mode == output.ModeJSON {
		return encodeContextJSON(w, doctorReport{Checks: findings})
	}
	table := output.NewTable("check", "status", "detail", "recovery")
	for _, finding := range findings {
		table.Append(finding.Check, finding.Status, finding.Detail, finding.Recovery)
	}
	return table.Render(w)
}

// catalogCheck is the fourth check, reachable only with --online. The second
// return value is non-nil exactly when the finding is a failure, so the
// caller can add it to doctor's failures map without re-deriving the outcome
// from the finding's Status string.
func catalogCheck(stateRoot string, log catalog.DebugLog) (doctorFinding, *problem.Problem) {
	ctx, cancel := context.WithTimeout(context.Background(), catalogProbeTimeout)
	defer cancel()
	origin := catalog.Origin(stateRoot)
	// The log is the same one --verbose turns on for module commands, so a
	// probe that fails for transport reasons surfaces the raw detail there
	// exactly as wso2 module list would (review on #161).
	client := catalog.Client{Origin: origin, OriginConfigured: catalog.OriginConfigured(stateRoot), Log: log}
	if _, err := client.Index(ctx); err != nil {
		typed := doctorProblem(err)
		return failFinding(checkCatalog, typed), &typed
	}
	return passFinding(checkCatalog, fmt.Sprintf("the module catalog at %s is reachable", origin)), nil
}

// mostSevereFailure reports the exit-deciding problem, per R1's rank. A check
// this command did not run, or ran and passed or found not-applicable, never
// appears in failures and cannot be returned.
func mostSevereFailure(failures map[string]problem.Problem) error {
	for _, name := range severityRank {
		if typed, failed := failures[name]; failed {
			return typed
		}
	}
	return nil
}

// doctorProblem recovers the typed problem a doctor check's error always carries.
//
// contexts.Load, every session.Store method, and catalog.Client.Index
// (internal/catalog/client.go:203-220, every one of originUnreachable,
// unreadable, and schemaUnsupported) return a problem.Problem on every error
// path they define, so the fallback below is unreached by any call site in
// this file today. It exists so a future check that forgets to type its
// failure fails safely, as a module-process error, rather than by panicking
// this command.
func doctorProblem(err error) problem.Problem {
	var typed problem.Problem
	if errors.As(err, &typed) {
		return typed
	}
	return problem.New(problem.CategoryModuleProcess, "shell.unexpected_failure", err.Error())
}

func passFinding(check, detail string) doctorFinding {
	return doctorFinding{Check: check, Status: statusPass, Detail: detail}
}

func notApplicableFinding(check, detail string) doctorFinding {
	return doctorFinding{Check: check, Status: statusNotApplicable, Detail: detail}
}

func noneFinding(check, detail, recovery string) doctorFinding {
	return doctorFinding{Check: check, Status: statusNone, Detail: detail, Recovery: recovery}
}

func failFinding(check string, typed problem.Problem) doctorFinding {
	return doctorFinding{Check: check, Status: statusFail, Detail: typed.Message, Recovery: typed.Recovery}
}
