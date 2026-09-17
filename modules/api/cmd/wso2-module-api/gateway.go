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
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/modules/api/internal/platform"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/result"
)

// The semantic shapes a registered gateway and a minted token take.
const (
	GatewaySchema      = "api.gateway/v1"
	GatewayTokenSchema = "api.gatewayToken/v1"
)

// tokenNotShownAgainWarning is printed to standard error, never standard
// output, whenever a token is minted: it is readable exactly once, in the
// result that carries it, and this says so beside it rather than in place of
// it.
const tokenNotShownAgainWarning = "This token will not be shown again: the gateway controller needs it " +
	"to connect to the control plane, so store it in the gateway's own secret store now."

// defaultGatewayFunctionalityType is what platform-api itself defaults
// functionalityType to when a request states none.
const defaultGatewayFunctionalityType = "regular"

// gatewayFunctionalityTypes are the values "--type" accepts, per
// CreateGatewayRequest.functionalityType.
var gatewayFunctionalityTypes = map[string]bool{
	"regular": true,
	"ai":      true,
	"event":   true,
}

// gatewayRegisterFlags are what "wso2 api gateway register" was asked for.
type gatewayRegisterFlags struct {
	displayName string
	endpoints   []string
	gwType      string
}

// registeredGateway is platform-api's GatewayResponse, read back after
// CreateGateway.
type registeredGateway struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

// gatewayToken is platform-api's TokenRotationResponse: the bearer credential
// a gateway controller presents to the control plane. It is readable exactly
// once, at the moment it is minted.
type gatewayToken struct {
	ID    string `json:"id"`
	Token string `json:"token"`
}

// gatewayRegister answers:
//
//	wso2 api gateway register <handle> --display-name <name> \
//	    --endpoint <url> [--endpoint <url> ...] [--type regular|ai|event]
//
// It registers the gateway and then mints its registration token in the same
// run, because the token is only ever readable at the moment
// POST /gateways/{id}/tokens answers. If the token mint fails after the
// gateway was registered, the gateway is left registered rather than rolled
// back — the deployment has no transaction to roll back into — and the
// failure names wso2 api gateway token create to finish the job, since a
// retry of register itself would meet a gateway handle that already exists.
func gatewayRegister(command *cobra.Command, flags *gatewayRegisterFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		handle, err := exactlyOneArgument(command.Flags().Args(),
			"wso2 api gateway register needs the gateway's handle",
			"Run wso2 api gateway register <handle> --display-name <name> --endpoint <url>.")
		if err != nil {
			return result.Result{}, err
		}
		if flags.displayName == "" || len(flags.endpoints) == 0 {
			return result.Result{}, usageProblem(
				"wso2 api gateway register needs a display name and at least one endpoint",
				"Run wso2 api gateway register "+handle+" --display-name <name> --endpoint <url>.")
		}
		gwType := flags.gwType
		if gwType == "" {
			gwType = defaultGatewayFunctionalityType
		}
		if !gatewayFunctionalityTypes[gwType] {
			return result.Result{}, usageProblem(
				"--type must be one of regular, ai, event; got "+gwType,
				"Run wso2 api gateway register "+handle+" --display-name <name> --endpoint <url> "+
					"--type regular|ai|event.")
		}

		// Both calls use the control-plane session: a gateway is registered
		// with the control plane, and the token it mints authenticates the
		// gateway controller back to that same control plane. Neither call
		// is the gateway's own management API.
		client, err := controlPlane(ctx, request)
		if err != nil {
			return result.Result{}, err
		}

		body := map[string]any{
			"id":                handle,
			"displayName":       flags.displayName,
			"endpoints":         flags.endpoints,
			"functionalityType": gwType,
		}
		var created registeredGateway
		if err := client.Post(ctx, controlPlanePath+"/gateways", body, &created); err != nil {
			return result.Result{}, callFailed(err, "register the gateway", request.Context.Endpoint)
		}
		if created.ID == "" {
			// A 201 that named no id still leaves the handle this command was
			// given as the only usable identifier: the gateway was created
			// with it, whether or not the deployment echoed it back.
			created.ID = handle
		}

		token, err := mintGatewayToken(ctx, client, request.Context.Endpoint, created.ID)
		if err != nil {
			return result.Result{}, gatewayRegisteredWithoutToken(created.ID, err)
		}

		fmt.Fprintln(os.Stderr, tokenNotShownAgainWarning)

		return result.New(GatewaySchema).
			With("id", "ID", created.ID).
			With("displayName", "Name", created.DisplayName).
			With("functionalityType", "Type", gwType).
			With("endpoints", "Endpoints", strings.Join(flags.endpoints, " ")).
			With("token", "Token", token.Token).
			With(NextField, "Next",
				"Configure the gateway controller with this token, then run "+
					"wso2 api gateway apis list to see what it is serving."), nil
	}
}

// gatewayTokenCreate answers "wso2 api gateway token create <gateway-id>": it
// mints a new registration token for a gateway that already exists, the way
// out of a gateway left registered without one, or a way to rotate one later.
func gatewayTokenCreate(command *cobra.Command) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		gatewayID, err := exactlyOneArgument(command.Flags().Args(),
			"wso2 api gateway token create needs the gateway's id",
			"Run wso2 api gateway token create <gateway-id>.")
		if err != nil {
			return result.Result{}, err
		}

		client, err := controlPlane(ctx, request)
		if err != nil {
			return result.Result{}, err
		}

		token, err := mintGatewayToken(ctx, client, request.Context.Endpoint, gatewayID)
		if err != nil {
			return result.Result{}, err
		}

		fmt.Fprintln(os.Stderr, tokenNotShownAgainWarning)

		return result.New(GatewayTokenSchema).
			With("gatewayId", "Gateway", gatewayID).
			With("token", "Token", token.Token).
			With(NextField, "Next", "Configure the gateway controller with this token."), nil
	}
}

// gatewayTokenLimitNote is appended to a token-mint recovery, because
// platform-api allows only 2 active tokens per gateway: a retry that keeps
// meeting a conflict may need an old token revoked first, not just repeating.
const gatewayTokenLimitNote = " platform-api allows at most 2 active tokens per gateway; revoke an " +
	"old one first if this keeps being refused."

// mintGatewayToken calls POST /gateways/{id}/tokens and refuses a 201 whose
// token is empty the same way it refuses a failed call: a token nobody can
// read back is exactly as useless as one that was never minted, and the same
// recovery — run this command again — applies to both.
func mintGatewayToken(ctx context.Context, client platform.Client, endpoint, gatewayID string) (gatewayToken, error) {
	var token gatewayToken
	path := controlPlanePath + "/gateways/" + url.PathEscape(gatewayID) + "/tokens"
	if err := client.Post(ctx, path, nil, &token); err != nil {
		return gatewayToken{}, withTokenLimitNoteOnGenericFailure(
			callFailed(err, "mint a token for gateway "+gatewayID, endpoint))
	}
	if token.Token == "" {
		return gatewayToken{}, moduleProblem("api.gateway_token_empty",
			"the deployment minted a token for gateway "+gatewayID+" but it carried no token value",
			"Run wso2 api gateway token create "+gatewayID+" to try again."+gatewayTokenLimitNote)
	}
	return token, nil
}

// withTokenLimitNoteOnGenericFailure appends gatewayTokenLimitNote to a
// callFailed problem's recovery, but only its generic api.call_failed shape
// (a 409, most likely) — not api.not_authorized or api.deployment_unreachable,
// where the token limit is not what a retry needs to fix.
func withTokenLimitNoteOnGenericFailure(err error) error {
	var refused problem.Problem
	if errors.As(err, &refused) && refused.Code == "api.call_failed" {
		return refused.WithRecovery(refused.Recovery + gatewayTokenLimitNote)
	}
	return err
}

// gatewayRegisteredWithoutToken reports that a gateway was registered but its
// token could not be minted, carrying the underlying failure's own message so
// the user knows why, and pointing at wso2 api gateway token create to finish
// the job. A 403 is reported as api.not_authorized — the code that already
// tells an administrator what to fix — rather than masked behind a generic
// code, since fixing permissions is what a retry actually needs.
func gatewayRegisteredWithoutToken(gatewayID string, mintErr error) error {
	recovery := "Run wso2 api gateway token create " + gatewayID + " to mint one." + gatewayTokenLimitNote
	var underlying problem.Problem
	if errors.As(mintErr, &underlying) && underlying.Code == "api.not_authorized" {
		return problem.New(problem.CategoryAuthPolicy, "api.not_authorized",
			"gateway "+gatewayID+" is registered, but minting its token was refused: "+underlying.Message).
			WithRecovery("Ask an administrator to map this user's group to a role carrying the gateway " +
				"token permissions, then run wso2 api gateway token create " + gatewayID + ".")
	}
	return moduleProblem("api.gateway_registered_without_token",
		"gateway "+gatewayID+" is registered, but no token was minted: "+mintFailureReason(mintErr),
		recovery)
}

// mintFailureReason states a token-mint failure in its own words rather than
// problem.Problem.Error()'s "category: code: message" form, which reads like
// an internal log line rather than something to show a user.
func mintFailureReason(err error) string {
	var underlying problem.Problem
	if errors.As(err, &underlying) {
		return underlying.Message
	}
	return err.Error()
}
