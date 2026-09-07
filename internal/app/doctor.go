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
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/internal/auth"
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
	checkIssuer      = "issuer"
	checkCatalog     = "catalog"
)

// The outcomes a check reports.
const (
	statusPass          = "pass"
	statusFail          = "fail"
	statusNotApplicable = "not-applicable"
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
// The two --online checks rank below every unconditional check: they are
// the optional ones and the only ones whose failure may be the network's
// rather than the machine's, so a real machine problem always outranks them.
// Between the two, issuer outranks catalog: a context whose issuer cannot be
// read fails every product command, while an unreachable catalog only stops
// an install. Neither mints an exit class: issuer carries exit.AuthPolicy
// through the same problems a product command's own discovery raises, and
// catalog's exit.ModuleProcess (70) is already documented in
// docs/reference/commands.md's exit-class table — Global Constraint 2's "no
// new exit class" bars minting a class that table does not already carry,
// not reusing one that it does.
var severityRank = []string{checkSecureStore, checkContext, checkSession, checkIssuer, checkCatalog}

// onlineProbeTimeout bounds each --online check, so a doctor run cannot hang
// on an unreachable origin or issuer.
const onlineProbeTimeout = 10 * time.Second

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
		"Also check that the selected context's issuer and the module catalog can be "+
			"reached, which requires a network connection.")
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

	// selected is the context the session and issuer checks report on. It
	// stays nil when no context can be selected, and each check says why.
	var selected *contexts.Selection

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
		chosen, selErr := document.Select(contextName)
		if selErr != nil {
			// An unresolvable --context name is the caller's argument
			// mistake, not a health finding: it is refused the way every
			// other context-selecting command refuses it, rather than
			// folded into the report. See this function's doc comment.
			return selErr
		}
		selected = &chosen
		switch {
		case selected.Identity.Auth.Kind == contexts.KindClientCredentials:
			// A client-credentials identity acquires access inline, one
			// grant per command, and holds no session at all — there is
			// nothing this check could find missing, so it is
			// not-applicable rather than a pass or a fail.
			findings = append(findings, notApplicableFinding(checkSession,
				"the selected context acquires access inline and holds no session"))
		default:
			var missing []string
			for _, access := range selected.Identity.Accesses() {
				if _, sessionErr := store.Load(access.SessionRef); sessionErr != nil {
					if !isNoSession(sessionErr) {
						// An unusable secure store is the one failure worth
						// stopping on: the loop cannot tell a genuinely
						// missing session from one it simply could not ask
						// about, so it fails the check on the store's own
						// terms instead of reporting products as missing
						// that might well have a session.
						typed := doctorProblem(sessionErr)
						failures[checkSession] = typed
						findings = append(findings, failFinding(checkSession, typed))
						missing = nil
						break
					}
					name := access.Namespace
					if name == "" {
						name = "the login session"
					}
					missing = append(missing, name)
				}
			}
			if len(missing) > 0 {
				typed := problem.New(problem.CategoryAuthPolicy, "auth.login_required",
					fmt.Sprintf("no stored session exists for %s. Run wso2 login to authorize "+
						"every product, or wso2 login --only <product> for one.",
						strings.Join(missing, ", "))).
					WithRecovery("Run wso2 login to authorize every product, or " +
						"wso2 login --only <product> for one.")
				failures[checkSession] = typed
				findings = append(findings, failFinding(checkSession, typed))
			} else if _, failed := failures[checkSession]; !failed {
				findings = append(findings, passFinding(checkSession,
					"a stored session exists for every product of the selected context"))
			}
		}
	}

	if online {
		finding, issuerErr := issuerCheck(selected)
		findings = append(findings, finding)
		if issuerErr != nil {
			failures[checkIssuer] = *issuerErr
		}
		finding, catalogErr := catalogCheck(root)
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

// issuerCheck reads the OpenID configuration of every issuer the selected
// context's identity names, reachable only with --online. It exists for the
// failure the offline checks cannot see: a deployment whose certificate this
// machine does not trust passes every one of them and then fails every
// product command at discovery. The probe is the broker's own discovery, so
// it refuses with the same problem — auth.certificate_untrusted naming the
// host and the commands that trust it, or auth.discovery_failed for anything
// else — and it dials through the process-wide client, which is the one
// WSO2_CA_FILE has widened. The second return value is non-nil exactly when
// the finding is a failure.
func issuerCheck(selected *contexts.Selection) (doctorFinding, *problem.Problem) {
	if selected == nil {
		return notApplicableFinding(checkIssuer,
			"no context is selected, so there is no issuer to reach"), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), onlineProbeTimeout)
	defer cancel()
	var issuers []string
	for _, access := range append([]contexts.ProductAccess{selected.Identity.LoginAccess()},
		selected.Identity.Accesses()...) {
		if access.Issuer != "" && !slices.Contains(issuers, access.Issuer) {
			issuers = append(issuers, access.Issuer)
		}
	}
	for _, issuer := range issuers {
		if err := auth.ProbeIssuer(ctx, http.DefaultClient, issuer); err != nil {
			typed := doctorProblem(err)
			return failFinding(checkIssuer, typed), &typed
		}
	}
	return passFinding(checkIssuer, fmt.Sprintf("the OpenID configuration of %s is readable",
		strings.Join(issuers, ", "))), nil
}

// catalogCheck is the last check, reachable only with --online. The second
// return value is non-nil exactly when the finding is a failure, so the
// caller can add it to doctor's failures map without re-deriving the outcome
// from the finding's Status string.
func catalogCheck(stateRoot string) (doctorFinding, *problem.Problem) {
	ctx, cancel := context.WithTimeout(context.Background(), onlineProbeTimeout)
	defer cancel()
	origin := catalog.Origin(stateRoot)
	client := catalog.Client{Origin: origin}
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
// path they define, and auth.ProbeIssuer returns an auth.Denial that carries
// one, so the fallback below is unreached by any call site in this file
// today. It exists so a future check that forgets to type its failure fails
// safely, as a module-process error, rather than by panicking this command.
func doctorProblem(err error) problem.Problem {
	var denied auth.Denial
	if errors.As(err, &denied) {
		return denied.Reported()
	}
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

func failFinding(check string, typed problem.Problem) doctorFinding {
	return doctorFinding{Check: check, Status: statusFail, Detail: typed.Message, Recovery: typed.Recovery}
}
