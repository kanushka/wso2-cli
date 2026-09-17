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
	"strings"

	"github.com/wso2/wso2-cli/modules/api/internal/platform"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// controlPlane asks the shell for access to the product's own record and
// builds the client for the control plane.
//
// No scopes are named. On this product the shell's access is obtained by
// exchanging the login session for one bound to the product's audience, and an
// exchanged token carries no resource-server permissions: the deployment
// authorizes the call from the group the token carries, mapped to a role at
// the product. Naming scopes here would ask the shell to prove a narrowing
// this deployment never performs. Leaving them empty asks for exactly what the
// context's product record consents to, which is the honest request.
func controlPlane(ctx context.Context, request module.Request) (platform.Client, error) {
	if request.Context.Endpoint == "" {
		return platform.Client{}, moduleProblem("api.product_not_recorded",
			"the selected context records no endpoint for the api product, so this command has "+
				"nowhere to call",
			"Run wso2 context product add api --url <url> to record it on the selected context, then "+
				"run this command again.")
	}
	access, err := request.Access.Acquire(ctx, module.AccessRequest{Audience: ManagementAudience})
	if err != nil {
		return platform.Client{}, err
	}
	return platform.Client{Endpoint: request.Context.Endpoint, Token: access.Token}, nil
}

// gateway asks for access to the product's gateway record and builds the
// client for the gateway's own management API.
//
// It is a second record on the same product rather than a second product: the
// gateway validates the same login provider's tokens, under the gateway's own
// audience, so the shell holds one record for each and a module names which it
// wants.
func gateway(ctx context.Context, request module.Request) (platform.Client, error) {
	if request.Context.GatewayEndpoint == "" {
		return platform.Client{}, moduleProblem("api.gateway_not_recorded",
			"the selected context records no gateway for the api product, so this command has "+
				"nowhere to call",
			"Run wso2 context product add api --url <url> --gateway <gateway-url> --replace, "+
				"then run wso2 login.")
	}
	access, err := request.Access.Acquire(ctx, module.AccessRequest{
		Audience: GatewayAudience,
		Record:   module.RecordGateway,
	})
	if err != nil {
		return platform.Client{}, err
	}
	return platform.Client{Endpoint: request.Context.GatewayEndpoint, Token: access.Token}, nil
}

// callFailed states a refused call in terms an administrator can act on,
// keeping the deployment's own words rather than inventing a second account.
func callFailed(err error, attempted, endpoint string) error {
	var refusal platform.Failure
	if !errors.As(err, &refusal) {
		return moduleProblem("api.deployment_unreachable",
			"the shell could not reach the API Platform at "+endpoint+" to "+attempted,
			"Check that this machine can reach that URL, then retry.")
	}
	if refusal.Status == 401 || refusal.Status == 403 {
		return moduleProblem("api.not_authorized",
			"the deployment refused this context's access when asked to "+attempted,
			"Ask an administrator to map this user's group to a role carrying the permissions "+
				"the operation needs, then run wso2 login again.")
	}
	return moduleProblem("api.call_failed",
		"the deployment would not "+attempted+": "+refusalMessage(refusal),
		"Check the deployment's own logs for the refusal, then retry.")
}

// createCallFailed refuses "wso2 api apis create"'s own call to POST
// /rest-apis.
//
// A 400 there means the file this command built the request from was what the
// deployment refused, never something the deployment did on its own, so the
// recovery points at the file instead of callFailed's usual "check the
// deployment's logs" — there are no deployment logs to check for a request
// this command composed.
func createCallFailed(err error, endpoint, file string) error {
	var refusal platform.Failure
	if errors.As(err, &refusal) && refusal.Status == 400 {
		return moduleProblem("api.call_failed",
			"the deployment refused the API built from "+file+": "+refusalMessage(refusal),
			"Fix "+file+" and run wso2 api apis create again.")
	}
	return callFailed(err, "create the API", endpoint)
}

// refusalMessage states a refusal in the deployment's own words, with any
// per-field validation failures it named appended as "field: message" so a
// validation failure is never reported as an opaque generic message with the
// detail that would explain it silently dropped.
func refusalMessage(refusal platform.Failure) string {
	message := refusal.Message
	if message == "" {
		message = refusal.Error()
	}
	if len(refusal.FieldErrors) == 0 {
		return message
	}
	details := make([]string, len(refusal.FieldErrors))
	for i, fieldError := range refusal.FieldErrors {
		details[i] = fieldError.Field + ": " + fieldError.Message
	}
	return message + " (" + strings.Join(details, "; ") + ")"
}

// exactlyOneArgument proves a command line names exactly one positional
// argument, refusing zero with message and recovery, and more than one by
// naming how many were given.
func exactlyOneArgument(positional []string, message, recovery string) (string, error) {
	switch len(positional) {
	case 0:
		return "", usageProblem(message, recovery)
	case 1:
		return positional[0], nil
	default:
		return "", usageProblem(
			"this command takes one argument, and was given "+fmt.Sprint(len(positional)), recovery)
	}
}

// usageProblem refuses a command line this module cannot carry out.
func usageProblem(message, recovery string) error {
	return problem.New(problem.CategoryUsage, "api.invalid_argument", message).WithRecovery(recovery)
}

// moduleProblem builds this module's typed failure.
func moduleProblem(code, message, recovery string) error {
	return problem.New(problem.CategoryAuthPolicy, code, message).WithRecovery(recovery)
}
