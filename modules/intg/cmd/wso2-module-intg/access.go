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
	"fmt"
	"net/http"

	"github.com/wso2/wso2-cli/modules/intg/internal/platform"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// clientFor asks the shell for access to the Integration Platform API and
// builds the client that calls it.
//
// No scopes are named. On WSO2 Cloud the shell reaches this product by
// exchanging the login session's token at ThunderID for one bound to the
// API's resource server, and the API authorizes from the organization claims
// that token carries rather than from scopes. Leaving them empty asks for
// exactly what the context's product record consents to.
func clientFor(ctx context.Context, request module.Request) (platform.Client, error) {
	if request.Context.Endpoint == "" {
		return platform.Client{}, problem.New(problem.CategoryUsage, "intg.product_not_recorded",
			"the selected context records no URL for the intg product, so this command has nowhere to call").
			WithRecovery("Run wso2 context product add intg --url <integration platform API URL> to record it " +
				"on the selected context, then run this command again.")
	}
	access, err := request.Access.Acquire(ctx, module.AccessRequest{Audience: IntegrationAudience})
	if err != nil {
		// The shell's denial already says why and what to do; restating it
		// here could only lose detail.
		return platform.Client{}, err
	}
	return platform.Client{Endpoint: request.Context.Endpoint, Token: access.Token}, nil
}

// callFailed states a refused call in terms a user can act on, keeping the
// API's own words rather than inventing a second account of what happened.
// attempted is a verb phrase such as "list the projects".
func callFailed(err error, attempted, endpoint string) error {
	return callFailedNaming(err, attempted, endpoint, "Check the names given on the command line, and that "+
		"the selected context's URL is the Integration Platform API.")
}

// callFailedNaming is callFailed for a command that names one resource, whose
// 404 recovery points at where to find the right name.
func callFailedNaming(err error, attempted, endpoint, notFoundRecovery string) error {
	var refusal platform.Failure
	if !errors.As(err, &refusal) {
		return problem.New(problem.CategoryProductService, "intg.deployment_unreachable",
			"the Integration Platform API at "+endpoint+" could not be reached to "+attempted).
			WithRecovery("Check that this machine can reach that URL and that it is the one the selected " +
				"context should use, then retry.")
	}
	words := refusalWords(refusal)
	switch refusal.Status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return problem.New(problem.CategoryAuthPolicy, "intg.not_authorized",
			"the Integration Platform refused this context's access when asked to "+attempted+": "+words).
			WithRecovery("Run wso2 login to sign in again. If it persists, ask an administrator of the " +
				"organization for a role that allows it.")
	case http.StatusPaymentRequired:
		return problem.New(problem.CategoryProductService, "intg.plan_limit",
			"the organization's plan does not allow this: could not "+attempted+": "+words).
			WithRecovery("Check the organization's subscription in the WSO2 Cloud console.")
	case http.StatusBadRequest:
		// The API judged the request itself, so retrying it unchanged would be
		// refused the same way.
		return problem.New(problem.CategoryUsage, "intg.call_failed",
			"the Integration Platform refused the request to "+attempted+": "+words).
			WithRecovery("Check the arguments and flags given on the command line against the message above, " +
				"then run the command again.")
	case http.StatusNotFound:
		return problem.New(problem.CategoryProductService, "intg.not_found",
			"the Integration Platform found nothing to "+attempted+": "+words).
			WithRecovery(notFoundRecovery)
	default:
		return problem.New(problem.CategoryProductService, "intg.call_failed",
			"the Integration Platform could not "+attempted+": "+words).
			WithRecovery("Retry in a moment. If it persists, report the message above to the " +
				"Integration Platform team.")
	}
}

// refusalWords is the API's own message, falling back to its status line when
// it stated none.
func refusalWords(refusal platform.Failure) string {
	if refusal.Message != "" {
		return refusal.Message
	}
	return refusal.Error()
}

// refusal reports whether err is the API refusing with status, and with what.
func refusal(err error, status int) (platform.Failure, bool) {
	var refused platform.Failure
	if errors.As(err, &refused) && refused.Status == status {
		return refused, true
	}
	return platform.Failure{}, false
}

// exactlyOneArgument proves a command line names exactly one positional
// argument, refusing none with message and more than one by count.
func exactlyOneArgument(positional []string, message, recovery string) (string, error) {
	switch len(positional) {
	case 0:
		return "", usageProblem(message, recovery)
	case 1:
		return positional[0], nil
	default:
		return "", usageProblem(fmt.Sprintf("this command takes one argument, and was given %d",
			len(positional)), recovery)
	}
}

// exactlyTwoArguments proves a command line names exactly two positional
// arguments, refusing any other count with message.
func exactlyTwoArguments(positional []string, message, recovery string) (string, string, error) {
	if len(positional) != 2 {
		return "", "", usageProblem(fmt.Sprintf("%s: this command takes two arguments, and was given %d",
			message, len(positional)), recovery)
	}
	return positional[0], positional[1], nil
}

// projectOf is the project a command acts within: the one --project names, or
// the selected context's when the flag is not given. Either may be empty.
func projectOf(flag string, request module.Request) string {
	if flag != "" {
		return flag
	}
	return request.Context.Project
}

// noArguments refuses a command line that names positional arguments the
// command does not take.
func noArguments(positional []string, recovery string) error {
	if len(positional) == 0 {
		return nil
	}
	return usageProblem(fmt.Sprintf("this command takes no arguments, and was given %d", len(positional)),
		recovery)
}

// usageProblem refuses a command line this module cannot carry out.
func usageProblem(message, recovery string) error {
	return problem.New(problem.CategoryUsage, "intg.invalid_argument", message).WithRecovery(recovery)
}
