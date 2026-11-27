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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/auth/issuertrust"
	"github.com/wso2/wso2-cli/internal/auth/trustedhttp"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/rpc"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// platformPath is where WSO2 Cloud's platform service answers on an
// environment's product gateway.
const platformPath = "/platform-api-service-platform-api-endpoint"

const platformTimeout = 30 * time.Second

// platformClient calls WSO2 Cloud's platform service with one token.
type platformClient struct {
	endpoint string
	token    string
	client   *http.Client
}

// platform resolves the selected context's platform service and a token for
// it holding scopes, establishing the platform session when it has none.
// action names what the token is for, in a refusal: "list projects".
func (s Shell) platform(action string, scopes ...string) (platformClient, contexts.Selection, error) {
	root, err := s.stateRoot()
	if err != nil {
		return platformClient{}, contexts.Selection{}, err
	}
	selected, err := s.selection("")
	if err != nil {
		return platformClient{}, contexts.Selection{}, err
	}
	if err := requirePlatform(selected); err != nil {
		return platformClient{}, selected, err
	}
	endpoint, err := s.platformEndpoint(root, selected)
	if err != nil {
		return platformClient{}, selected, err
	}
	invocationID, err := rpc.NewInvocationID()
	if err != nil {
		return platformClient{}, selected, err
	}
	served := selected
	served.Identity = selected.Identity.WithPlatform(endpoint)
	broker := &auth.Broker{
		Namespace: contexts.PlatformKey,
		Capabilities: modules.Capabilities{
			AuthAudiences: []string{contexts.PlatformAudience},
			AuthScopes:    contexts.PlatformScopes,
		},
		Selection:    served,
		InvocationID: invocationID,
		StateRoot:    root,
	}
	broker.EstablishSession = s.sessionEstablisher(served, contexts.PlatformKey, false)
	grant, err := broker.Acquire(auth.Request{Audience: contexts.PlatformAudience, Scopes: scopes})
	if err != nil {
		var denied auth.Denial
		if errors.As(err, &denied) && denied.Problem.Code == "auth.scope_not_granted" {
			return platformClient{}, selected, problem.New(problem.CategoryAuthPolicy, "auth.platform_forbidden",
				fmt.Sprintf("your role in this organization does not allow you to %s", action)).
				WithRecovery(fmt.Sprintf("Ask an administrator of the organization for a role that carries %s, "+
					"then run wso2 login and retry.", strings.Join(scopes, " and ")))
		}
		if errors.As(err, &denied) {
			return platformClient{}, selected, denied.Reported()
		}
		return platformClient{}, selected, err
	}
	return platformClient{endpoint: endpoint, token: grant.Token, client: trustedhttp.Client(nil)}, selected, nil
}

// requirePlatform refuses a context that does not reach WSO2 Cloud's platform
// service: projects exist on WSO2 Cloud alone.
func requirePlatform(selected contexts.Selection) error {
	const recovery = "Run wso2 login to sign in to WSO2 Cloud, or wso2 context use <name> to select a " +
		"WSO2 Cloud context; wso2 context list shows them."
	switch {
	case selected.Context.Name == "":
		return problem.New(problem.CategoryUsage, "shell.not_wso2_cloud",
			"projects belong to WSO2 Cloud, and no context is selected").WithRecovery(recovery)
	case !selected.Context.WSO2Cloud():
		return problem.New(problem.CategoryUsage, "shell.not_wso2_cloud",
			fmt.Sprintf("projects belong to WSO2 Cloud, and the %q context is not a WSO2 Cloud context",
				selected.Context.Name)).WithRecovery(recovery)
	case !selected.Identity.HasPlatform():
		return problem.New(problem.CategoryUsage, "shell.not_wso2_cloud",
			fmt.Sprintf("the %q context signs in without a person, and WSO2 Cloud projects are managed "+
				"by a person's sign-in", selected.Context.Name)).
			WithRecovery("Select a WSO2 Cloud context that signs in through the browser or a device code.")
	}
	return nil
}

// platformEndpoint is where the context's environment serves the platform
// service: on the product gateway every Cloud product shares.
func (s Shell) platformEndpoint(root string, selected contexts.Selection) (string, error) {
	if s.PlatformURL != "" {
		return strings.TrimSuffix(s.PlatformURL, "/"), nil
	}
	env := selected.Context.CloudEnvironment
	if env == "" {
		return "", problem.New(problem.CategoryUsage, "shell.cloud_environment_unknown",
			fmt.Sprintf("the %q context records no WSO2 Cloud environment", selected.Context.Name)).
			WithRecovery(fmt.Sprintf("Run wso2 login with %s set to the environment the context signs in "+
				"to, such as dev or prod-us, so the context records it.", CloudEnvVar))
	}
	environment, err := readCloudEnvironment(root, env)
	if err != nil {
		return "", err
	}
	return cloudProductGateway(environment) + platformPath, nil
}

// call sends one request and decodes a 2xx answer into answer, when given.
// action names what was asked, for a refusal: "list projects".
func (c platformClient) call(method, path string, body, answer any, action string) error {
	target, err := url.Parse(c.endpoint + path)
	if err != nil || !issuertrust.Secure(target) {
		return problem.New(problem.CategoryUsage, "shell.platform_url_insecure",
			"the WSO2 Cloud platform service address is not served over HTTPS").
			WithRecovery("Check the environment file's baseDomain and environment.")
	}
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(encoded)
	}
	ctx, cancel := context.WithTimeout(context.Background(), platformTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, target.String(), payload)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.client.Do(request)
	if err != nil {
		return problem.New(problem.CategoryUnreachable, "shell.platform_unreachable",
			fmt.Sprintf("WSO2 Cloud's platform service could not be reached to %s", action)).
			WithRecovery("Check the network connection and any VPN the environment needs, then retry.")
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return problem.New(problem.CategoryUnreachable, "shell.platform_unreachable",
			fmt.Sprintf("WSO2 Cloud's platform service stopped answering while asked to %s", action)).
			WithRecovery("Retry the command.")
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return platformRefusal(response.StatusCode, serviceReason(data), action)
	}
	if answer == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, answer); err != nil {
		return problem.New(problem.CategoryProductService, "shell.platform_answer_malformed",
			fmt.Sprintf("WSO2 Cloud's platform service answered the request to %s with something that is "+
				"not what it documents", action)).
			WithRecovery("Retry the command. Report it if it keeps happening.")
	}
	return nil
}

// serviceReason is the reason a refusal's body gives, in its error or
// message member, cut to one line.
func serviceReason(data []byte) string {
	var body struct {
		Error   any    `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &body) != nil {
		return ""
	}
	reason, _ := body.Error.(string)
	if body.Message != "" && (reason == "" || len(reason) < 24) {
		reason = body.Message
	}
	reason, _, _ = strings.Cut(strings.TrimSpace(reason), "\n")
	if len(reason) > 300 {
		reason = reason[:300]
	}
	return reason
}

func platformRefusal(status int, reason, action string) error {
	because := ""
	if reason != "" {
		because = ": " + reason
	}
	switch {
	case status == http.StatusUnauthorized:
		return problem.New(problem.CategoryAuthPolicy, "auth.platform_unauthorized",
			fmt.Sprintf("WSO2 Cloud's platform service did not accept this sign-in to %s%s", action, because)).
			WithRecovery("Run wso2 login to sign in again, then retry.")
	case status == http.StatusForbidden:
		return problem.New(problem.CategoryAuthPolicy, "auth.platform_forbidden",
			fmt.Sprintf("your role in this organization does not allow you to %s%s", action, because)).
			WithRecovery("Ask an administrator of the organization for a role that allows it.")
	case status == http.StatusNotFound:
		return problem.New(problem.CategoryNotFound, "shell.project_not_found",
			fmt.Sprintf("WSO2 Cloud has no such project to %s%s", action, because)).
			WithRecovery("Run wso2 project list to see the projects.")
	case status == http.StatusConflict:
		return problem.New(problem.CategoryConflict, "shell.project_conflict",
			fmt.Sprintf("WSO2 Cloud refused to %s%s", action, because)).
			WithRecovery("Resolve what the reason names, then retry.")
	case status >= 400 && status < 500:
		return problem.New(problem.CategoryUsage, "shell.platform_refused",
			fmt.Sprintf("WSO2 Cloud refused to %s%s", action, because)).
			WithRecovery("Correct what the reason names, then retry.")
	}
	return problem.New(problem.CategoryProductService, "shell.platform_failed",
		fmt.Sprintf("WSO2 Cloud's platform service failed to %s (HTTP %d)%s", action, status, because)).
		WithRecovery("Retry the command. Report it if it keeps happening.")
}
