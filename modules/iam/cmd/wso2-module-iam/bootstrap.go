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
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/modules/iam/internal/thunder"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/result"
)

// BootstrapSchema identifies the shape of the bootstrap result.
const BootstrapSchema = "iam.bootstrap/v1"

// The seeded values of a fresh ThunderID 1.0.0 deployment. Every one is a
// flag, because a deployment can be configured differently; these are the
// defaults a stock container answers to.
const (
	DefaultAdminUser        = "admin"
	DefaultPasswordVariable = "WSO2_IAM_ADMIN_PASSWORD"
	DefaultClientID         = "wso2-cli"
	DefaultConsoleClient    = "CONSOLE"
	DefaultSystemResource   = "https://localhost:8090/mcp"
	DefaultOU               = "01900000-0000-7000-8000-000000000001"
	// DefaultAuthFlow is the seeded console application's authentication
	// flow rather than the plain default flow: it carries the two SSO nodes,
	// so a user who logged in for one resource server is not asked for
	// credentials again when logging in for another (measured on 1.0.0-beta
	// and 1.0.1). Every application on the flow shares its session cookie.
	DefaultAuthFlow = "01900000-0000-7000-8000-000000000068"
	// The console flow's registration and recovery companions. ThunderID
	// refuses an application whose flows reference different families
	// ("Conflicting flow references"), so the three travel together.
	DefaultRegistrationFlow = "01900000-0000-7000-8000-000000000069"
	DefaultRecoveryFlow     = "01900000-0000-7000-8000-000000000070"
)

// loopbackRedirects are the callbacks the shell's browser login listens on.
var loopbackRedirects = []string{
	"http://127.0.0.1:10425/callback", "http://127.0.0.1:10426/callback",
	"http://127.0.0.1:10427/callback", "http://127.0.0.1:10428/callback",
}

type bootstrapFlags struct {
	url, adminUser, passwordVariable, clientID  string
	consoleClient, systemResource, ou, authFlow string
}

func bootstrapCommand() (*cobra.Command, *bootstrapFlags) {
	flags := &bootstrapFlags{}
	command := &cobra.Command{
		Use:   "bootstrap --url <issuer>",
		Short: "Register this CLI as a public client in a ThunderID deployment, once.",
		Long: "Logs in as the administrator through the seeded console client, creates the " +
			"public OAuth application the shell's browser login uses if it is absent, and prints " +
			"the wso2 iam connect line to run next. The administrator password is read from the " +
			"environment variable --password-variable names; it is never a flag value.",
	}
	f := command.Flags()
	f.StringVar(&flags.url, "url", "", "The ThunderID issuer, such as https://localhost:8090.")
	f.StringVar(&flags.adminUser, "admin-user", DefaultAdminUser, "The administrator username.")
	f.StringVar(&flags.passwordVariable, "password-variable", DefaultPasswordVariable,
		"The environment variable holding the administrator password.")
	f.StringVar(&flags.clientID, "client-id", DefaultClientID, "The client id to register for this CLI.")
	f.StringVar(&flags.consoleClient, "console-client", DefaultConsoleClient,
		"The seeded console client used for the administrator login.")
	f.StringVar(&flags.systemResource, "system-resource", DefaultSystemResource,
		"The identifier of the system resource server.")
	f.StringVar(&flags.ou, "ou", DefaultOU, "The organization unit the application is created in.")
	f.StringVar(&flags.authFlow, "auth-flow", DefaultAuthFlow, "The authentication flow the application uses.")
	return command, flags
}

func bootstrap(flags *bootstrapFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		issuer := strings.TrimRight(flags.url, "/")
		if issuer == "" {
			return result.Result{}, problem.New(problem.CategoryUsage, "iam.missing_url",
				"wso2 iam bootstrap needs the issuer of the ThunderID deployment").
				WithRecovery("Pass --url <issuer>, such as --url http://localhost:8490.")
		}
		password, err := secretFromEnvironment(flags.passwordVariable, "the administrator password")
		if err != nil {
			return result.Result{}, err
		}

		token, err := thunder.SystemToken(ctx, thunder.HTTPClient(), thunder.ConsoleLogin{
			Issuer: issuer, ClientID: flags.consoleClient, RedirectURI: issuer + "/console",
			SystemResource: flags.systemResource, Scope: "openid system",
			Username: flags.adminUser, Password: password,
		})
		if err != nil {
			return result.Result{}, thunder.LoginProblem(err)
		}

		client := thunder.New(issuer, token)
		app, created, err := ensurePublicApplication(ctx, client, flags)
		if err != nil {
			return result.Result{}, err
		}
		next := fmt.Sprintf("Run wso2 %s connect %s%s, then wso2 login.", Namespace, issuer,
			connectOverrides(flags))
		return result.New(BootstrapSchema).
			With("issuer", "Issuer", issuer).
			With("clientId", "Client ID", flags.clientID).
			With("applicationId", "Application", app.ID).
			With("created", "Created", fmt.Sprintf("%t", created)).
			With(NextField, "Next", next), nil
	}
}

// ensurePublicApplication finds the CLI's public client or creates it.
func ensurePublicApplication(ctx context.Context, client *thunder.Client, flags *bootstrapFlags) (
	thunder.Application, bool, error) {
	var listed thunder.ApplicationList
	if err := client.Get(ctx, "/applications", &listed); err != nil {
		return thunder.Application{}, false, thunder.Problem(err, "the application listing")
	}
	for _, app := range listed.Applications {
		if app.OAuthClientID() == flags.clientID {
			return app, false, nil
		}
	}
	body := thunder.Application{
		OUID: flags.ou, Name: "WSO2 CLI", Type: "custom",
		Description:      "The wso2 command line, registered by wso2 iam bootstrap",
		AuthFlowID:       flags.authFlow,
		RegistrationFlow: companionFlow(flags.authFlow, DefaultRegistrationFlow),
		RecoveryFlow:     companionFlow(flags.authFlow, DefaultRecoveryFlow),
		URL:              "http://127.0.0.1:10425",
		AllowedUserTypes: []string{"Person"},
		InboundAuth: []thunder.InboundAuth{{Type: "oauth2", Config: thunder.OAuthConfig{
			ClientID: flags.clientID, RedirectURIs: loopbackRedirects,
			GrantTypes: []string{"authorization_code", "refresh_token"}, ResponseTypes: []string{"code"},
			TokenEndpointAuthMethod: "none", PKCERequired: true, PublicClient: true,
		}}},
	}
	var created thunder.Application
	if err := client.Post(ctx, "/applications", body, &created); err != nil {
		return thunder.Application{}, false, thunder.Problem(err, "the application creation")
	}
	return created, true, nil
}

// secretFromEnvironment reads a secret the shell handed this module under its
// own prefix. The variable's name is the only thing a command line carries.
func secretFromEnvironment(variable, what string) (string, error) {
	value := os.Getenv(variable)
	if value == "" {
		return "", problem.New(problem.CategoryUsage, "iam.missing_secret",
			fmt.Sprintf("the environment variable %s, which should hold %s, is not set", variable, what)).
			WithRecovery(fmt.Sprintf("Export %s before running the command. The shell hands a module "+
				"only the WSO2_IAM_ variables, and never a value given as a flag.", variable))
	}
	return value, nil
}

// companionFlow pairs the seeded console authentication flow with its own
// registration and recovery flows; any other flow is left to the
// deployment's defaults.
func companionFlow(authFlow, companion string) string {
	if authFlow == DefaultAuthFlow {
		return companion
	}
	return ""
}

// connectOverrides names on the connect line whatever this bootstrap
// registered differently from the module's descriptor defaults, so the
// line printed is the one that records what was actually registered.
func connectOverrides(flags *bootstrapFlags) string {
	overrides := ""
	if flags.clientID != DefaultClientID {
		overrides += " --client-id " + flags.clientID
	}
	if flags.systemResource != DefaultSystemResource {
		overrides += " --audience " + flags.systemResource
	}
	return overrides
}
