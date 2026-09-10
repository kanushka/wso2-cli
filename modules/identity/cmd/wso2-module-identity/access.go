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
			"the selected context records no endpoint for the account product, so this command "+
				"has nowhere to call",
			"Run wso2 account add-product <account> account --endpoint <url> --audience <uri> "+
				"--scopes "+ManagementScope+", then run wso2 login.")
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

// usageProblem refuses a command line this module cannot carry out. The
// category is what puts it in the shell's usage class, so it exits 64 like
// every other mistyped command rather than reading as a deployment failure.
func usageProblem(message string) error {
	return problem.New(problem.CategoryUsage, "identity.invalid_argument", message).
		WithRecovery("Run wso2 account resource-servers create <name> --identifier <uri> " +
			"[--permission <handle>]... [--description <text>].")
}

// moduleProblem builds this module's typed failure. The shell renders it; the
// category places it in the same class the shell uses for a deployment that
// answered and refused.
func moduleProblem(code, message, recovery string) error {
	return problem.New(problem.CategoryAuthPolicy, code, message).WithRecovery(recovery)
}
