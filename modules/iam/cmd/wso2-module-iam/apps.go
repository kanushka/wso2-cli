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
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/modules/iam/internal/thunder"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/result"
)

const (
	AppsSchema = "iam.apps/v1"
	AppSchema  = "iam.app/v1"
)

type appCreateFlags struct {
	kind, name, ou, authFlow string
}

func appCommands() (*cobra.Command, *cobra.Command, *cobra.Command, *appCreateFlags) {
	family := &cobra.Command{Use: "apps", Short: "Manage ThunderID applications (OAuth clients)."}
	list := &cobra.Command{Use: "list", Short: "List the applications."}
	flags := &appCreateFlags{}
	create := &cobra.Command{
		Use:   "create <client-id> --type m2m|public",
		Short: "Register an OAuth client: m2m gets a generated secret, public gets the CLI's loopback callbacks.",
	}
	create.Flags().StringVar(&flags.kind, "type", "", "m2m for client credentials, public for a browser login.")
	create.Flags().StringVar(&flags.name, "name", "", "The application's display name; defaults to the client id.")
	create.Flags().StringVar(&flags.ou, "ou", DefaultOU, "The organization unit that owns it.")
	create.Flags().StringVar(&flags.authFlow, "auth-flow", DefaultAuthFlow, "The authentication flow a public app uses.")
	family.AddCommand(list, create)
	return family, list, create, flags
}

func appsList(ctx context.Context, request module.Request) (result.Result, error) {
	client, err := managementClient(ctx, request)
	if err != nil {
		return result.Result{}, err
	}
	var listed thunder.ApplicationList
	if err := client.Get(ctx, "/applications", &listed); err != nil {
		return result.Result{}, thunder.Problem(err, "the application listing")
	}
	names := make([]string, 0, len(listed.Applications))
	for _, app := range listed.Applications {
		entry := app.Name
		if id := app.OAuthClientID(); id != "" {
			entry += " (" + id + ")"
		}
		names = append(names, entry)
	}
	return result.New(AppsSchema).
		With("total", "Total", fmt.Sprintf("%d", listed.TotalResults)).
		With("apps", "Applications", joined(names)).
		With(NextField, "Next", "Run wso2 iam apps create <client-id> --type m2m to register a CI client."), nil
}

func appsCreate(command *cobra.Command, flags *appCreateFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		clientID, err := oneArgument(command, "a client id")
		if err != nil {
			return result.Result{}, err
		}
		if flags.kind != "m2m" && flags.kind != "public" {
			return result.Result{}, problem.New(problem.CategoryUsage, "iam.missing_flag",
				"wso2 iam apps create needs --type m2m or --type public").
				WithRecovery("m2m is a confidential client for client credentials; public is a browser-login client.")
		}
		client, err := managementClient(ctx, request)
		if err != nil {
			return result.Result{}, err
		}
		var listed thunder.ApplicationList
		if err := client.Get(ctx, "/applications", &listed); err != nil {
			return result.Result{}, thunder.Problem(err, "the application listing")
		}
		for _, app := range listed.Applications {
			if app.OAuthClientID() == clientID {
				return appResult(app, clientID, flags.kind, false, ""), nil
			}
		}
		name := flags.name
		if name == "" {
			name = clientID
		}
		body := thunder.Application{OUID: flags.ou, Name: name, Description: "Registered by wso2 iam apps create",
			AllowedUserTypes: []string{"Person"}}
		secret := ""
		if flags.kind == "public" {
			body.Type, body.AuthFlowID, body.URL = "custom", flags.authFlow, "http://127.0.0.1:10425"
			body.InboundAuth = []thunder.InboundAuth{{Type: "oauth2", Config: thunder.OAuthConfig{
				ClientID: clientID, RedirectURIs: loopbackRedirects,
				GrantTypes: []string{"authorization_code", "refresh_token"}, ResponseTypes: []string{"code"},
				TokenEndpointAuthMethod: "none", PKCERequired: true, PublicClient: true,
			}}}
		} else {
			secret, err = generatedSecret()
			if err != nil {
				return result.Result{}, err
			}
			body.Type = "m2m"
			body.InboundAuth = []thunder.InboundAuth{{Type: "oauth2", Config: thunder.OAuthConfig{
				ClientID: clientID, ClientSecret: secret, GrantTypes: []string{"client_credentials"},
				TokenEndpointAuthMethod: "client_secret_basic",
			}}}
		}
		var created thunder.Application
		if err := client.Post(ctx, "/applications", body, &created); err != nil {
			return result.Result{}, thunder.Problem(err, "the application creation")
		}
		return appResult(created, clientID, flags.kind, true, secret), nil
	}
}

func appResult(app thunder.Application, clientID, kind string, created bool, secret string) result.Result {
	shown := "(not shown again)"
	next := fmt.Sprintf("Run wso2 iam roles create <role> --resource-server <name> --permission <a:b:c> "+
		"--assign-app %s to grant this client access.", clientID)
	if secret != "" {
		shown = secret
		next = fmt.Sprintf("export WSO2_THUNDER_CI_SECRET=%s (shown once), then wso2 identity create <name> "+
			"--issuer <issuer> --client-id %s --client-secret-variable WSO2_THUNDER_CI_SECRET --provider thunder "+
			"--product <namespace> --endpoint <url> --audience <resource identifier> --scope <permission>.",
			secret, clientID)
	}
	if kind == "public" {
		shown = "(none: public client)"
		next = fmt.Sprintf("Run wso2 identity create <name> --issuer <issuer> --client-id %s --provider thunder "+
			"--product <namespace> --endpoint <url> --audience <resource identifier> --scope <permission>, "+
			"then wso2 login --context <name>.", clientID)
	}
	return result.New(AppSchema).
		With("id", "ID", app.ID).
		With("clientId", "Client ID", clientID).
		With("type", "Type", kind).
		With("created", "Created", createdWord(created)).
		With("clientSecret", "Client secret", shown).
		With(NextField, "Next", next)
}

func generatedSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
