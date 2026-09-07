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
	"net/url"
	"strings"

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
	kind, name, ou, authFlow, product string
}

func appCommands() (*cobra.Command, *cobra.Command, *cobra.Command, *appCreateFlags) {
	family := &cobra.Command{Use: "apps", Short: "Manage ThunderID applications (OAuth clients)."}
	list := &cobra.Command{Use: "list", Short: "List the applications."}
	flags := &appCreateFlags{}
	create := &cobra.Command{
		Use: "create <client-id> --type m2m|public|federation [--for <url>]",
		Short: "Register an OAuth client: m2m gets a generated secret, public gets the CLI's loopback " +
			"callbacks, federation gets a secret and the callback of the product that signs in through it.",
	}
	create.Flags().StringVar(&flags.kind, "type", "",
		"m2m for client credentials, public for a person's login, federation for another product's login.")
	create.Flags().StringVar(&flags.product, "for", "",
		"The base URL of the product a federation client signs in for; its callback is <url>/commonauth.")
	create.Flags().StringVar(&flags.name, "name", "", "The application's display name; defaults to the client id.")
	create.Flags().StringVar(&flags.ou, "ou", DefaultOU, "The organization unit that owns it.")
	create.Flags().StringVar(&flags.authFlow, "auth-flow", DefaultAuthFlow,
		"The authentication flow a public or federation app uses.")
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
		if flags.kind != "m2m" && flags.kind != "public" && flags.kind != "federation" {
			return result.Result{}, problem.New(problem.CategoryUsage, "iam.missing_flag",
				"wso2 iam apps create needs --type m2m, --type public or --type federation").
				WithRecovery("m2m is a confidential client for client credentials; public is a browser-login " +
					"client; federation is the confidential client another product signs in through.")
		}
		product, err := federationProduct(flags)
		if err != nil {
			return result.Result{}, err
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
				return appResult(request, app, clientID, flags.kind, product, false, ""), nil
			}
		}
		name := flags.name
		if name == "" {
			name = clientID
		}
		body := thunder.Application{OUID: flags.ou, Name: name, Description: "Registered by wso2 iam apps create",
			AllowedUserTypes: []string{"Person"}}
		secret := ""
		switch flags.kind {
		case "public":
			body.Type, body.AuthFlowID, body.URL = "custom", flags.authFlow, "http://127.0.0.1:10425"
			body.RegistrationFlow = companionFlow(flags.authFlow, DefaultRegistrationFlow)
			body.RecoveryFlow = companionFlow(flags.authFlow, DefaultRecoveryFlow)
			body.InboundAuth = []thunder.InboundAuth{{Type: "oauth2", Config: thunder.OAuthConfig{
				ClientID: clientID, RedirectURIs: loopbackRedirects,
				GrantTypes: []string{"authorization_code", "refresh_token"}, ResponseTypes: []string{"code"},
				TokenEndpointAuthMethod: "none", PKCERequired: true, PublicClient: true,
			}}}
		case "federation":
			secret, err = generatedSecret()
			if err != nil {
				return result.Result{}, err
			}
			// The product federating through this client reads the user's
			// email, groups and name from the ID token, and a scope for
			// email and groups so that the token carries them when asked.
			body.Type, body.AuthFlowID, body.URL = "custom", flags.authFlow, product
			body.RegistrationFlow = companionFlow(flags.authFlow, DefaultRegistrationFlow)
			body.RecoveryFlow = companionFlow(flags.authFlow, DefaultRecoveryFlow)
			body.InboundAuth = []thunder.InboundAuth{{Type: "oauth2", Config: thunder.OAuthConfig{
				ClientID: clientID, ClientSecret: secret, RedirectURIs: []string{product + "/commonauth"},
				GrantTypes: []string{"authorization_code", "refresh_token"}, ResponseTypes: []string{"code"},
				TokenEndpointAuthMethod: "client_secret_post",
				Token:                   &thunder.TokenConfig{IDToken: &thunder.IDTokenConfig{UserAttributes: []string{"email", "groups", "name"}}},
				ScopeClaims:             map[string][]string{"email": {"email"}, "groups": {"groups"}},
			}}}
		default:
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
		return appResult(request, created, clientID, flags.kind, product, true, secret), nil
	}
}

// federationProduct is the base URL a federation client signs in for, with
// no trailing slash so that the callback and the next line read cleanly.
// Other types have no product and get an empty string. The URL is never
// guessed: a redirect pointing at the wrong place is the kind of mistake
// that only shows up at the product's login as a refused callback.
func federationProduct(flags *appCreateFlags) (string, error) {
	if flags.kind != "federation" {
		return "", nil
	}
	if flags.product == "" {
		return "", problem.New(problem.CategoryUsage, "iam.missing_flag",
			"wso2 iam apps create --type federation needs --for <url>, the product that signs in through this client").
			WithRecovery("Pass --for <url>, such as --for https://localhost:9443; the client's callback is <url>/commonauth.")
	}
	parsed, err := url.Parse(flags.product)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil {
		return "", problem.New(problem.CategoryUsage, "iam.invalid_flag",
			fmt.Sprintf("--for %s is not an absolute http or https URL without user information", flags.product)).
			WithRecovery("Pass the product's base URL, such as --for https://localhost:9443.")
	}
	return strings.TrimRight(flags.product, "/"), nil
}

func appResult(request module.Request, app thunder.Application, clientID, kind, product string, created bool,
	secret string) result.Result {
	shown := "(not shown again)"
	next := fmt.Sprintf("Run wso2 iam roles create <role> --resource-server <name> --permission <a:b:c> "+
		"--assign-app %s to grant this client access.", clientID)
	if secret != "" {
		shown = secret
		// The secret is in the field named for it and nowhere else. This line
		// is the one an operator copies into a shell, so it names the variable
		// the identity reads and leaves the value to be pasted in: a
		// credential belongs in a variable, and the record of it is the
		// variable's name.
		next = fmt.Sprintf("export WSO2_THUNDER_CI_SECRET=<the client secret above, shown once>, then "+
			"wso2 identity create <name> --issuer <issuer> --client-id %s "+
			"--client-secret-variable WSO2_THUNDER_CI_SECRET --provider thunder "+
			"--product <namespace> --endpoint <url> --audience <resource identifier> --scope <permission>.",
			clientID)
	}
	if kind == "federation" {
		// The product's bootstrap reads the secret from this variable and
		// nothing else; the issuer is the deployment this command ran
		// against, which is the login provider the product federates to.
		next = fmt.Sprintf("Run wso2 apim bootstrap --url %s --login-provider %s --federation-client-id %s "+
			"with WSO2_APIM_FEDERATION_CLIENT_SECRET exported.", product, request.Context.Endpoint, clientID)
		if secret != "" {
			next = fmt.Sprintf("export WSO2_APIM_FEDERATION_CLIENT_SECRET=<the client secret above, shown once>; then "+
				"wso2 apim bootstrap --url %s --login-provider %s --federation-client-id %s",
				product, request.Context.Endpoint, clientID)
		}
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
