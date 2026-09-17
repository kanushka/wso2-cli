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

package main

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/wso2/wso2-cli/modules/identity/internal/thunder"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// clientFor asks the shell for access and builds the management client this
// invocation may use.
//
// Access is requested per command rather than once per process, so a command
// that needs nothing never asks, and the shell's refusal reaches the user with
// its own recovery rather than one this module invented.
func clientFor(ctx context.Context, request module.Request) (thunder.Client, error) {
	if request.Context.Endpoint == "" {
		return thunder.Client{}, moduleProblem("identity.product_not_recorded",
			"the identity product manages Thunder, not Identity Server or Asgardeo, and the "+
				"selected context records no Thunder endpoint for it, so this command has nowhere to call",
			"Run wso2 context create <name> --login-product identity --url <thunder-url> --use to "+
				"create a context that logs in through Thunder, then run wso2 login.")
	}
	access, err := request.Access.Acquire(ctx, module.AccessRequest{
		Audience: ManagementAudience,
		Scopes:   []string{ManagementScope},
	})
	if err != nil {
		// The shell's own account of a refusal is returned unchanged: it knows
		// why access was denied and this module does not.
		return thunder.Client{}, err
	}
	return thunder.Client{Endpoint: request.Context.Endpoint, Token: access.Token}, nil
}

// asFailure reports whether err is a management refusal, and reads it out.
func asFailure(err error, out *thunder.Failure) bool {
	return errors.As(err, out)
}

// callFailed states a refused management call in terms an administrator can
// act on, keeping the deployment's own words rather than inventing a second
// account of them.
//
// A 400 is treated differently from every other refusal: it means the request
// this command built was what the deployment refused, not something the
// deployment did on its own, so the recovery points at the command's own
// input and flags rather than at logs the deployment holds and this refusal
// already explains.
func callFailed(err error, attempted, endpoint string) error {
	var refusal thunder.Failure
	if !asFailure(err, &refusal) {
		return moduleProblem("identity.deployment_unreachable",
			"the shell could not reach the deployment at "+endpoint+" to "+attempted,
			"Check that this machine can reach that URL, then retry.")
	}
	if refusal.Status == 401 || refusal.Status == 403 {
		return moduleProblem("identity.not_authorized",
			"the deployment refused this context's access when asked to "+attempted,
			"Ask an administrator to grant this user a role carrying the system permission on "+
				"the deployment's own resource server, then run wso2 login again.")
	}
	if refusal.Status == 400 {
		return moduleProblem("identity.call_failed",
			"the deployment would not "+attempted+": "+refusalMessage(refusal),
			"Check the command's input and flags, then retry.")
	}
	return moduleProblem("identity.call_failed",
		"the deployment would not "+attempted+": "+refusalMessage(refusal),
		"Check the deployment's own logs for the refusal, then retry.")
}

// refusalMessage states a refusal in the deployment's own words. ThunderID
// answers a validation failure with a generic Message ("Validation Failed"),
// its own longer Description, and per-field Errors; all three are folded in
// here so the detail that actually explains the refusal is never silently
// dropped in favor of the generic headline.
func refusalMessage(refusal thunder.Failure) string {
	message := refusal.Message
	if message == "" {
		message = refusal.Error()
	}
	if refusal.Description != "" && refusal.Description != message {
		message += ": " + refusal.Description
	}
	if len(refusal.FieldErrors) == 0 {
		return message
	}
	fields := make([]string, 0, len(refusal.FieldErrors))
	for field := range refusal.FieldErrors {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	details := make([]string, len(fields))
	for i, field := range fields {
		details[i] = field + ": " + refusal.FieldErrors[field]
	}
	return message + " (" + strings.Join(details, "; ") + ")"
}

// usageProblem refuses a command line this module cannot carry out. The
// category is what puts it in the shell's usage class, so it exits 64 like
// every other mistyped command rather than reading as a deployment failure.
func usageProblem(message string) error {
	return problem.New(problem.CategoryUsage, "identity.invalid_argument", message).
		WithRecovery("Run wso2 identity resource-servers create <name> --identifier <uri> " +
			"[--ou <id-or-handle>] [--permission <handle>]... [--description <text>].")
}

// moduleProblem builds this module's typed failure. The shell renders it; the
// category places it in the same class the shell uses for a deployment that
// answered and refused.
func moduleProblem(code, message, recovery string) error {
	return problem.New(problem.CategoryAuthPolicy, code, message).WithRecovery(recovery)
}
