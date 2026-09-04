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
	"strings"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/modules/apim/internal/apim"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/result"
)

const BootstrapSchema = "apim.bootstrap/v1"

const (
	DefaultAdminUser        = "admin"
	DefaultPasswordVariable = "WSO2_APIM_ADMIN_PASSWORD"
	DefaultClientName       = "wso2-cli"
	DefaultCallback         = "http://127.0.0.1:10425/callback"
	// ClientSecretVariable is the variable the identity create line names.
	ClientSecretVariable = "WSO2_APIM_CLIENT_SECRET"
	registrationPath     = "/client-registration/v0.17/register"
)

type bootstrapFlags struct {
	url, adminUser, passwordVariable, clientName, callback string
}

func bootstrapCommand() (*cobra.Command, *bootstrapFlags) {
	flags := &bootstrapFlags{}
	command := &cobra.Command{
		Use:   "bootstrap --url <base>",
		Short: "Register this CLI on API Manager's resident key manager, once.",
		Long: "Registers an OAuth client through dynamic client registration with the administrator " +
			"password read from the environment variable --password-variable names, then prints the " +
			"client secret once and the wso2 identity create line to run next. API Manager's key manager " +
			"has no public client, so the identity it creates is a client-credentials one.",
	}
	f := command.Flags()
	f.StringVar(&flags.url, "url", "", "The API Manager base URL, such as https://localhost:9443.")
	f.StringVar(&flags.adminUser, "admin-user", DefaultAdminUser, "The administrator username.")
	f.StringVar(&flags.passwordVariable, "password-variable", DefaultPasswordVariable,
		"The environment variable holding the administrator password.")
	f.StringVar(&flags.clientName, "client-name", DefaultClientName, "The name to register the client under.")
	f.StringVar(&flags.callback, "callback", DefaultCallback, "The callback URL registered for the client.")
	return command, flags
}

type registration struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	ClientName   string `json:"clientName"`
}

func bootstrap(flags *bootstrapFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		base := strings.TrimRight(flags.url, "/")
		if base == "" {
			return result.Result{}, problem.New(problem.CategoryUsage, "apim.missing_url",
				"wso2 apim bootstrap needs the base URL of the API Manager deployment").
				WithRecovery("Pass --url <base>, such as --url https://localhost:9443.")
		}
		password, err := secretFromEnvironment(flags.passwordVariable, "the administrator password")
		if err != nil {
			return result.Result{}, err
		}
		client := apim.New(base, "")
		body := map[string]any{
			"callbackUrl": flags.callback, "clientName": flags.clientName, "owner": flags.adminUser,
			"grantType": "password refresh_token client_credentials authorization_code",
			"saasApp":   true, "tokenType": "JWT",
		}
		var registered registration
		if err := client.PostBasic(ctx, registrationPath, flags.adminUser, password, body, &registered); err != nil {
			return result.Result{}, apim.Problem(err, "the client registration")
		}
		issuer := base + "/oauth2/token"
		next := fmt.Sprintf("export %s=%s (shown once); then wso2 identity create apim-admin --issuer %s "+
			"--client-id %s --client-secret-variable %s --product %s --endpoint %s --audience %s "+
			"--scope %s --scope %s --scope %s --scope %s --scope %s --scope %s",
			ClientSecretVariable, registered.ClientSecret, issuer, registered.ClientID, ClientSecretVariable,
			Namespace, base, registered.ClientID,
			ScopeAPIView, ScopeAPICreate, ScopeAPIPublish, ScopeSubscribe, ScopeAppManage, ScopeAdmin)
		return result.New(BootstrapSchema).
			With("issuer", "Issuer", issuer).
			With("clientId", "Client ID", registered.ClientID).
			With("clientSecret", "Client secret", registered.ClientSecret).
			With("tokenType", "Token type", "JWT").
			With(NextField, "Next", next), nil
	}
}
