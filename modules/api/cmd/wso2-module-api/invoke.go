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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/modules/api/internal/platform"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/result"
)

const (
	InvocationSchema      = "api.invocation/v1"
	InvocationTokenSchema = "api.invocationToken/v1"
)

// InvocationAudience is the logical audience this module declares for calling
// an API the way its consumer would. The concrete audience is the API's own,
// read from its jwt-auth policy and named on the access request.
const InvocationAudience = "api-invocation"

// invocationBodyLimit bounds what an invocation's result carries of the API's
// answer: enough to read, not enough to flood a terminal.
const invocationBodyLimit = 1 << 20

const invocationTokenNote = "This token is stored nowhere and cannot be revoked; it expires on its own. " +
	"Use it to test the API, not to build on."

type apisInvokeFlags struct {
	method    string
	headers   []string
	data      string
	gatewayID string
	noToken   bool
}

type apisTokenFlags struct {
	gatewayID string
}

// invokedAPI is what the control plane records about an API, as far as an
// invocation needs: where it is mounted and what a token for it is bound to.
type invokedAPI struct {
	ID       string `json:"id"`
	Context  string `json:"context"`
	Policies []struct {
		Name   string `json:"name"`
		Params struct {
			Audiences []string `json:"audiences"`
		} `json:"params"`
	} `json:"policies"`
}

// audience is the audience the API's jwt-auth policy declares, empty for an
// API that declares none and so takes no token.
func (a invokedAPI) audience() string {
	for _, policy := range a.Policies {
		if policy.Name == "jwt-auth" && len(policy.Params.Audiences) > 0 {
			return policy.Params.Audiences[0]
		}
	}
	return ""
}

type apiDeployment struct {
	GatewayID string `json:"gatewayId"`
	Status    string `json:"status"`
}

type gatewayRecord struct {
	ID        string   `json:"id"`
	Endpoints []string `json:"endpoints"`
}

func apisToken(command *cobra.Command, flags *apisTokenFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		apiID, err := exactlyOneArgument(command.Flags().Args(),
			"wso2 api apis token needs the id of the API to mint a token for",
			"Run wso2 api apis token <api-id>. wso2 api apis list --project <id> shows the ids.")
		if err != nil {
			return result.Result{}, err
		}
		client, err := controlPlane(ctx, request)
		if err != nil {
			return result.Result{}, err
		}
		api, err := readAPI(ctx, client, request.Context.Endpoint, apiID)
		if err != nil {
			return result.Result{}, err
		}
		if api.audience() == "" {
			return result.Result{}, noAudience(apiID)
		}
		access, err := request.Access.Acquire(ctx, module.AccessRequest{
			Audience: InvocationAudience, Record: module.RecordAPI, Resource: api.audience(),
		})
		if err != nil {
			return result.Result{}, err
		}
		fmt.Fprintln(os.Stderr, invocationTokenNote)
		return result.New(InvocationTokenSchema).
			With("apiId", "API", apiID).
			With("audience", "Audience", api.audience()).
			With("expiresAt", "Expires", access.ExpiresAt.UTC().Format(time.RFC3339)).
			With("token", "Token", access.Token).
			With(NextField, "Next",
				"Send it as Authorization: Bearer <token> to the API, or run wso2 api apis invoke "+
					apiID+" <path> to have this shell do that."), nil
	}
}

func apisInvoke(command *cobra.Command, flags *apisInvokeFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		apiID, path, err := invocationArguments(command.Flags().Args())
		if err != nil {
			return result.Result{}, err
		}
		headers, err := invocationHeaders(flags.headers)
		if err != nil {
			return result.Result{}, err
		}
		client, err := controlPlane(ctx, request)
		if err != nil {
			return result.Result{}, err
		}
		api, err := readAPI(ctx, client, request.Context.Endpoint, apiID)
		if err != nil {
			return result.Result{}, err
		}
		endpoint, err := deployedEndpoint(ctx, client, request.Context.Endpoint, apiID, flags.gatewayID)
		if err != nil {
			return result.Result{}, err
		}
		target := strings.TrimSuffix(endpoint, "/") + "/" + strings.Trim(api.Context, "/") + path

		token := ""
		if !flags.noToken && api.audience() != "" {
			access, err := request.Access.Acquire(ctx, module.AccessRequest{
				Audience: InvocationAudience, Record: module.RecordAPI, Resource: api.audience(),
			})
			if err != nil {
				return result.Result{}, err
			}
			token = access.Token
		}

		method := strings.ToUpper(flags.method)
		if method == "" {
			method = http.MethodGet
		}
		call, err := http.NewRequestWithContext(ctx, method, target, strings.NewReader(flags.data))
		if err != nil {
			return result.Result{}, usageProblem("the request could not be built: "+err.Error(),
				"Run wso2 api apis invoke "+apiID+" <path> [-X <method>] [-H <name: value>] [-d <body>].")
		}
		for name, value := range headers {
			call.Header.Set(name, value)
		}
		if token != "" {
			call.Header.Set("Authorization", "Bearer "+token)
		}
		started := time.Now()
		answer, err := (&http.Client{Timeout: 30 * time.Second}).Do(call)
		if err != nil {
			return result.Result{}, moduleProblem("api.gateway_unreachable",
				"the shell could not reach the API at "+target,
				"Check that this machine can reach the gateway's endpoint, then retry.")
		}
		defer func() { _ = answer.Body.Close() }()
		body, _ := io.ReadAll(io.LimitReader(answer.Body, invocationBodyLimit))

		return result.New(InvocationSchema).
			With("apiId", "API", apiID).
			With("method", "Method", method).
			With("url", "URL", target).
			With("status", "Status", strconv.Itoa(answer.StatusCode)).
			With("durationMs", "Duration (ms)", strconv.FormatInt(time.Since(started).Milliseconds(), 10)).
			With("contentType", "Content-Type", answer.Header.Get("Content-Type")).
			With("body", "Body", readableBody(body)), nil
	}
}

// readableBody indents a JSON answer, and returns any other exactly as the API
// sent it. The value is a string either way: the result carries what the API
// said, not this module's reading of it.
func readableBody(body []byte) string {
	if !json.Valid(body) {
		return string(body)
	}
	var indented bytes.Buffer
	if err := json.Indent(&indented, body, "", "  "); err != nil {
		return string(body)
	}
	return indented.String()
}

// invocationArguments reads the API id and the path to call under its
// context. The path is optional and is normalized to start with a slash.
func invocationArguments(positional []string) (apiID, path string, err error) {
	recovery := "Run wso2 api apis invoke <api-id> [path] [-X <method>] [-H <name: value>] [-d <body>]."
	switch len(positional) {
	case 0:
		return "", "", usageProblem("wso2 api apis invoke needs the id of the API to call", recovery)
	case 1:
		return positional[0], "", nil
	case 2:
		path = positional[1]
		if path != "" && !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		return positional[0], path, nil
	default:
		return "", "", usageProblem(
			"this command takes an API id and an optional path, and was given "+fmt.Sprint(len(positional))+
				" arguments", recovery)
	}
}

// invocationHeaders parses -H values. Authorization is refused: the token is
// the shell's to present, and a header of the caller's own would either be
// overwritten silently or replace it, and neither is what -H promises.
func invocationHeaders(raw []string) (map[string]string, error) {
	headers := map[string]string{}
	for _, entry := range raw {
		name, value, found := strings.Cut(entry, ":")
		name = strings.TrimSpace(name)
		if !found || name == "" {
			return nil, usageProblem("-H needs a header in the form name: value; got "+strconv.Quote(entry),
				"Run wso2 api apis invoke <api-id> [path] -H \"Name: value\".")
		}
		if strings.EqualFold(name, "Authorization") {
			return nil, usageProblem("-H cannot set Authorization: the shell presents the API's token itself",
				"Leave the header out, or run with --no-token to call the API anonymously.")
		}
		headers[name] = strings.TrimSpace(value)
	}
	return headers, nil
}

func readAPI(ctx context.Context, client platform.Client, endpoint, apiID string) (invokedAPI, error) {
	var api invokedAPI
	if err := client.Get(ctx, controlPlanePath+"/rest-apis/"+url.PathEscape(apiID), &api); err != nil {
		return invokedAPI{}, callFailed(err, "read the API "+apiID, endpoint)
	}
	return api, nil
}

// deployedEndpoint is the endpoint of the gateway the API is deployed on:
// the one named, or the only one it is deployed on.
func deployedEndpoint(ctx context.Context, client platform.Client, endpoint, apiID, gatewayID string) (string, error) {
	var deployed struct {
		List []apiDeployment `json:"list"`
	}
	path := controlPlanePath + "/rest-apis/" + url.PathEscape(apiID) + "/deployments"
	if err := client.Get(ctx, path, &deployed); err != nil {
		return "", callFailed(err, "read the deployments of the API "+apiID, endpoint)
	}
	var serving []string
	for _, deployment := range deployed.List {
		if deployment.Status == "DEPLOYED" && (gatewayID == "" || deployment.GatewayID == gatewayID) {
			serving = append(serving, deployment.GatewayID)
		}
	}
	switch {
	case len(serving) == 0 && gatewayID != "":
		return "", moduleProblem("api.not_deployed",
			"the API "+apiID+" is not deployed on the gateway "+gatewayID,
			"Run wso2 api apis deploy "+apiID+" --gateway-id "+gatewayID+", then retry.")
	case len(serving) == 0:
		return "", moduleProblem("api.not_deployed",
			"the API "+apiID+" is not deployed on any gateway, so there is nothing to call",
			"Run wso2 api apis deploy "+apiID+" --gateway-id <gateway-id>, then retry.")
	case len(serving) > 1:
		return "", usageProblem(
			"the API "+apiID+" is deployed on more than one gateway ("+strings.Join(serving, ", ")+")",
			"Run wso2 api apis invoke "+apiID+" --gateway-id <gateway-id> to say which one to call.")
	}
	var gateways struct {
		List []gatewayRecord `json:"list"`
	}
	if err := client.Get(ctx, controlPlanePath+"/gateways", &gateways); err != nil {
		return "", callFailed(err, "read the gateways", endpoint)
	}
	for _, gateway := range gateways.List {
		if gateway.ID == serving[0] && len(gateway.Endpoints) > 0 {
			return gateway.Endpoints[0], nil
		}
	}
	return "", moduleProblem("api.gateway_not_recorded",
		"the gateway "+serving[0]+" the API "+apiID+" is deployed on registers no endpoint to call",
		"Register the gateway's endpoint with wso2 api gateway register, then retry.")
}

func noAudience(apiID string) error {
	return moduleProblem("api.no_audience",
		"the API "+apiID+" declares no jwt-auth audience, so there is no token to mint for it",
		"Add a jwt-auth policy naming the API's audience to the API, or call it with "+
			"wso2 api apis invoke "+apiID+" <path>, which sends no token to an API that takes none.")
}
