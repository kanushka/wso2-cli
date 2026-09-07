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
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/modules/apim/internal/apim"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/result"
)

const GatewaySchema = "apim.gateway-response/v1"

// gatewayBodyLimit is how much of the answer a result carries.
const gatewayBodyLimit = 4096

type gatewayFlags struct {
	method string
}

func gatewayCommands() (family, invoke *cobra.Command, flags *gatewayFlags) {
	flags = &gatewayFlags{}
	family = &cobra.Command{Use: "gateway", Short: "Call published APIs through the gateway."}
	invoke = &cobra.Command{
		Use:   "invoke </context/version/path> [--method GET]",
		Short: "Call an API through the gateway with a token the shell brokers for this identity's gateway record.",
		Long: "The token is minted for the audience and scopes the selected identity's apim gateway record " +
			"holds, the ones wso2 apim connect <gateway-url> --gateway recorded beside the management " +
			"record: the API's own resource server and permissions, issued by the identity's login " +
			"provider. That is the user's own API called with the login provider's token.",
	}
	invoke.Flags().StringVar(&flags.method, "method", http.MethodGet, "The HTTP method.")
	family.AddCommand(invoke)
	return family, invoke, flags
}

func gatewayInvoke(command *cobra.Command, flags *gatewayFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		path, err := oneArgument(command, "a gateway path such as /mockapi/1.0.0/status")
		if err != nil {
			return result.Result{}, err
		}
		if !strings.HasPrefix(path, "/") {
			return result.Result{}, problem.New(problem.CategoryUsage, "apim.missing_argument",
				fmt.Sprintf("%q is not a gateway path", path)).
				WithRecovery("Pass the path as the gateway serves it, starting with /, such as /mockapi/1.0.0/status.")
		}
		if request.Context.GatewayEndpoint == "" {
			return result.Result{}, noGateway()
		}
		// The gateway record, by name. No scopes: the shell answers with the
		// scopes the record holds, which are the user's own API's.
		access, err := request.Access.Acquire(ctx, module.AccessRequest{
			Audience: GatewayAudience, Record: module.RecordGateway,
		})
		if err != nil {
			return result.Result{}, err
		}
		call, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		target := strings.TrimRight(request.Context.GatewayEndpoint, "/") + path
		httpRequest, err := http.NewRequestWithContext(call, strings.ToUpper(flags.method), target, nil)
		if err != nil {
			return result.Result{}, problem.New(problem.CategoryUsage, "apim.missing_argument", err.Error())
		}
		httpRequest.Header.Set("Authorization", "Bearer "+access.Token)
		httpRequest.Header.Set("Accept", "application/json")
		response, err := apim.HTTPClient().Do(httpRequest)
		if err != nil {
			return result.Result{}, problem.New(problem.CategoryProductService, "apim.unavailable",
				"the gateway did not answer: "+err.Error()).
				WithRecovery("Check that the gateway is running at the endpoint this identity records.")
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(response.Body, gatewayBodyLimit))
		if response.StatusCode < 200 || response.StatusCode > 299 {
			return result.Result{}, gatewayRefusal(httpRequest.Method, target, response.StatusCode,
				strings.TrimSpace(string(body)))
		}
		return result.New(GatewaySchema).
			With("method", "Method", strings.ToUpper(flags.method)).
			With("url", "URL", target).
			With("status", "Status", fmt.Sprintf("%d", response.StatusCode)).
			With("body", "Body", strings.TrimSpace(string(body))).
			With(NextField, "Next", "(done)"), nil
	}
}

// noGateway refuses an identity that records the apim product without its
// gateway: what wso2 apim connect writes on its own. The way out is the
// second connect, not a second identity.
func noGateway() problem.Problem {
	return problem.New(problem.CategoryUsage, "apim.no_gateway",
		"the selected identity records the apim product without its gateway").
		WithRecovery("Record the gateway beside the product on this identity with " +
			"wso2 apim connect <gateway-url> --gateway --audience <api resource identifier> --scopes <list>, " +
			"then wso2 login --only apim. The gateway is https://<host>:8243 on a default deployment; " +
			"the audience is the API's own resource identifier and the scopes are its permissions.")
}

// gatewayRefusal is the problem a non-2xx answer becomes, so a script can
// tell success from refusal by the exit code while the body still shows.
func gatewayRefusal(method, target string, status int, body string) problem.Problem {
	answer := fmt.Sprintf("the gateway answered %s %s with status %d", method, target, status)
	if body != "" {
		answer += ": " + body
	}
	recovery := "Read the gateway's answer above; it names what it did not accept."
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		recovery = "The gateway refused the token: check the API is published, the application subscribed, " +
			"and this identity's client mapped onto that application with wso2 apim apps map-keys."
	}
	return problem.New(problem.CategoryProductService, "apim.refused", answer).WithRecovery(recovery)
}
