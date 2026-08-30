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
const doctorUsage = "Run wso2 doctor [--online] [--output table|json]."

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
// ranking in the middle. TestDoctorRanksTheDocumentAboveAnAbsentSession pins
// this against a case where the numeric class of the lower-ranked failure is
// larger.
//
// catalog is deliberately absent: it exists only under --online, and Global
// Constraint 2 restricts this command to the three exit classes the three
// unconditional checks already carry. A catalog failure is still reported as a
// finding; it is just never the one this command's exit status blames.
var severityRank = []string{checkSecureStore, checkContext, checkSession}

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
			if _, err := forwardShellFlags(command, nil); err != nil {
				return err
			}
			return s.doctor(command, online)
		},
	}
	command.Flags().BoolVar(&online, doctorOnlineFlag, false,
		"Also check module catalog reachability, which requires a network connection.")
	return command
}

// doctor runs every check, reports every finding, and reports the exit status
// of the most severe failing one.
//
// The report is always written before this returns, on a failing run as much
// as a passing one: a caller reading --output json must be able to read the
// findings off a failing run, and the write happens before the failure is
// decided rather than being skipped by it.
func (s Shell) doctor(command *cobra.Command, online bool) error {
	mode, err := shellOutputMode(command)
	if err != nil {
		return err
	}
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	// --context wins over WSO2_CONTEXT, which wins over the document's default
	// context, mirroring the precedence selection() applies for wso2 login and
	// wso2 logout (internal/app/invoke.go): a user who sets WSO2_CONTEXT to
	// drive those commands expects wso2 doctor to report on the same context.
	contextName := ""
	if flag := shellFlag(command, contextFlag); flag != nil {
		contextName = flag.Value.String()
	}
	if contextName == "" {
		contextName = os.Getenv("WSO2_CONTEXT")
	}

	document, loadErr := contexts.Load(root)
	// "Configured" is deliberately broader than "has a readable document with
	// contexts": a document that exists but fails to decode or validate is a
	// machine someone tried to configure, not the fresh machine R2 exempts, so
	// the other checks still run for it rather than being waved through as
	// not-applicable. Only a document that reads clean and names no context is
	// the state R2 describes.
	configured := loadErr != nil || len(document.Contexts) > 0

	failures := make(map[string]problem.Problem, len(severityRank))
	var findings []doctorFinding

	switch {
	case loadErr != nil:
		typed := doctorProblem(loadErr)
		failures[checkContext] = typed
		findings = append(findings, failFinding(checkContext, typed))
	case !configured:
		findings = append(findings, notApplicableFinding(checkContext,
			"no context document is configured"))
	default:
		findings = append(findings, passFinding(checkContext, "the context document is valid"))
	}

	store := session.Store{StateRoot: root}
	if !configured {
		findings = append(findings, notApplicableFinding(checkSecureStore,
			"no context is configured, so the secure store was not probed"))
	} else if probeErr := store.Probe(); probeErr != nil {
		typed := doctorProblem(probeErr)
		failures[checkSecureStore] = typed
		findings = append(findings, failFinding(checkSecureStore, typed))
	} else {
		findings = append(findings, passFinding(checkSecureStore, "the OS secure store is reachable"))
	}

	if !configured {
		findings = append(findings, notApplicableFinding(checkSession,
			"no context is configured, so there is no session to check"))
	} else {
		// A document that failed to load leaves no identity to read a
		// credential reference from. The store is still asked, under an empty
		// reference: nothing is ever stored under one, so the answer is the
		// same "no session" a genuinely absent session would report, which is
		// the honest answer here too — this command cannot tell whether a
		// session exists for a context it could not resolve.
		ref := ""
		if loadErr == nil {
			selected, selErr := document.Select(contextName)
			if selErr != nil {
				// An unresolvable --context name is the caller's argument
				// mistake, not a health finding: it is refused the way every
				// other context-selecting command refuses it, rather than
				// folded into the report.
				return selErr
			}
			ref = selected.Identity.Auth.CredentialRef
		}
		if _, sessionErr := store.Load(ref); sessionErr != nil {
			typed := doctorProblem(sessionErr)
			failures[checkSession] = typed
			findings = append(findings, failFinding(checkSession, typed))
		} else {
			findings = append(findings, passFinding(checkSession, "a stored session exists for the selected context"))
		}
	}

	if online {
		findings = append(findings, catalogFinding())
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

// catalogFinding is the fourth check, reachable only with --online.
//
// Its failure is never what decides the exit status: see severityRank.
func catalogFinding() doctorFinding {
	ctx, cancel := context.WithTimeout(context.Background(), catalogProbeTimeout)
	defer cancel()
	client := catalog.Client{Origin: catalog.Origin()}
	if _, err := client.Index(ctx); err != nil {
		typed := doctorProblem(err)
		return failFinding(checkCatalog, typed)
	}
	return passFinding(checkCatalog, fmt.Sprintf("the module catalog at %s is reachable", catalog.Origin()))
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
// contexts.Load and every session.Store method return a problem.Problem on
// every error path they define, so the fallback below is unreached by any
// call site in this file today. It exists so a future check that forgets to
// type its failure fails safely, as a module-process error, rather than by
// panicking this command.
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

func failFinding(check string, typed problem.Problem) doctorFinding {
	return doctorFinding{Check: check, Status: statusFail, Detail: typed.Message, Recovery: typed.Recovery}
}
