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
	"net/url"
	"os"
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
	// ClientSecretVariable is the variable the connect line names.
	ClientSecretVariable = "WSO2_APIM_CLIENT_SECRET"
	registrationPath     = "/client-registration/v0.17/register"
	// FederationSecretVariable holds the secret of the federation client,
	// the confidential client on the login provider that API Manager
	// federates through; wso2 iam apps create --type federation shows it
	// once.
	FederationSecretVariable = "WSO2_APIM_FEDERATION_CLIENT_SECRET"
	// DefaultPublicClientName is the public client the shell logs in with.
	DefaultPublicClientName = "wso2-cli-sso"
	// DefaultGroupMapping maps the login provider's administrators to the
	// role that carries the apim:* scopes.
	DefaultGroupMapping = "Administrators=admin"
	// identityProviderPrefix starts the identity provider's default name.
	identityProviderPrefix = "wso2-cli-"
)

// loopbackCallbacks are the four the shell listens on for a login.
var loopbackCallbacks = []string{"http://127.0.0.1:10425/callback", "http://127.0.0.1:10426/callback",
	"http://127.0.0.1:10427/callback", "http://127.0.0.1:10428/callback"}

type bootstrapFlags struct {
	url, adminUser, passwordVariable, clientName, callback string
	// The federation to the login provider; loginProvider empty means none.
	loginProvider, loginProviderInternalURL, federationClientID, identityProvider, publicClientName string
	groupMappings                                                                                   []string
}

func bootstrapCommand() (*cobra.Command, *bootstrapFlags) {
	flags := &bootstrapFlags{}
	command := &cobra.Command{
		Use:   "bootstrap --url <base>",
		Short: "Register this CLI on API Manager's resident key manager, once.",
		Long: "Registers an OAuth client through dynamic client registration with the administrator " +
			"password read from the environment variable --password-variable names, then prints the " +
			"client secret once and the wso2 apim connect line to run next. The client is confidential, " +
			"so it serves a client-credentials identity. With --login-provider it also registers, " +
			"idempotently, the identity provider API Manager federates to and the public client a " +
			"browser identity logs in with, and prints the wso2 apim connect line for that client; " +
			"the federation client's secret is read from " + FederationSecretVariable + ".",
	}
	f := command.Flags()
	f.StringVar(&flags.url, "url", "", "The API Manager base URL, such as https://localhost:9443.")
	f.StringVar(&flags.adminUser, "admin-user", DefaultAdminUser, "The administrator username.")
	f.StringVar(&flags.passwordVariable, "password-variable", DefaultPasswordVariable,
		"The environment variable holding the administrator password.")
	f.StringVar(&flags.clientName, "client-name", DefaultClientName, "The name to register the client under.")
	f.StringVar(&flags.callback, "callback", DefaultCallback, "The callback URL registered for the client.")
	f.StringVar(&flags.loginProvider, "login-provider", "",
		"The login provider's issuer, such as http://localhost:8492: registers an identity provider for it "+
			"and the public client the shell logs in with, federated through that identity provider.")
	f.StringVar(&flags.loginProviderInternalURL, "login-provider-internal-url", "",
		"The login provider's URL as API Manager reaches it, for the token and userinfo endpoints; "+
			"defaults to --login-provider.")
	f.StringVar(&flags.federationClientID, "federation-client-id", "",
		"The client on the login provider that API Manager federates through, from "+
			"wso2 iam apps create <id> --type federation; its secret is read from "+FederationSecretVariable+".")
	f.StringVar(&flags.identityProvider, "identity-provider", "",
		"The identity provider's name on API Manager; defaults to "+identityProviderPrefix+"<login provider host>.")
	f.StringVar(&flags.publicClientName, "public-client-name", DefaultPublicClientName,
		"The name of the public client the shell logs in with.")
	f.StringArrayVar(&flags.groupMappings, "map-group", []string{DefaultGroupMapping},
		"A <group>=<role> pair mapping a login provider group to an API Manager role; repeat for each.")
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
		federation, err := federationFrom(flags, base)
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
		if federation != nil {
			return federate(ctx, client.Admin(flags.adminUser, password), federation, base, registered)
		}
		// The client registered here is confidential, so it serves a
		// client-credentials identity: the machine client at the login
		// provider, or the product's own credential on one. A browser
		// identity reaches API Manager through a public client federated to
		// its login provider instead, which this registration does not make.
		// The secret is in the field named for it and nowhere else. This line
		// is the one an operator copies into a shell, so it names the variable
		// the connect line reads and leaves the value to be pasted in: a
		// credential belongs in a variable, and the record of it is the
		// variable's name.
		next := fmt.Sprintf("For a pipeline: export %s=<the client secret above, shown once>; then "+
			"wso2 %s connect %s --client-id %s --client-secret-variable %s, on a client-credentials "+
			"identity. A browser identity does not use this client: it needs a public client on "+
			"API Manager federated to its login provider, which wso2 %s bootstrap --url %s "+
			"--login-provider <issuer> --federation-client-id <id> registers, then wso2 %s connect %s "+
			"--client-id <that client>.",
			ClientSecretVariable, Namespace, base, registered.ClientID, ClientSecretVariable, Namespace, base, Namespace, base)
		return result.New(BootstrapSchema).
			With("issuer", "Issuer", issuer).
			With("clientId", "Client ID", registered.ClientID).
			With("clientSecret", "Client secret", registered.ClientSecret).
			With("tokenType", "Token type", "JWT").
			With(NextField, "Next", next), nil
	}
}

// federation is what bootstrap --login-provider writes: the identity
// provider on API Manager that points at the login provider, and the
// public client whose one authentication step is that identity provider.
type federation struct {
	loginProvider, internalURL, clientID, clientSecret, identityProvider, publicClientName string
	groupMappings                                                                          []apim.Mapping
}

// federationFrom reads the federation flags, or returns nil when
// --login-provider is not given and bootstrap keeps its pipeline path.
// Both refusals name the iam command that makes the federation client, so
// the order of the two commands is on screen.
func federationFrom(flags *bootstrapFlags, base string) (*federation, error) {
	if flags.loginProvider == "" {
		return nil, nil
	}
	issuer := strings.TrimRight(flags.loginProvider, "/")
	if parsed, err := url.Parse(issuer); err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" || parsed.User != nil {
		return nil, problem.New(problem.CategoryUsage, "apim.invalid_flag",
			"--login-provider is not an absolute http or https URL").
			WithRecovery("Name the login provider as the browser reaches it, such as http://localhost:8492.")
	}
	if flags.federationClientID == "" {
		return nil, missingFlag("bootstrap", "--federation-client-id with --login-provider",
			fmt.Sprintf("Run wso2 iam apps create <id> --type federation --for %s first; it makes the client "+
				"API Manager federates through and shows its secret once. Then run this command with "+
				"--federation-client-id <id> and %s exported.", base, FederationSecretVariable))
	}
	secret := os.Getenv(FederationSecretVariable)
	if secret == "" {
		return nil, missingFlag("bootstrap", FederationSecretVariable+" with --login-provider",
			fmt.Sprintf("Run wso2 iam apps create %s --type federation --for %s; it shows the client's secret once. "+
				"Then export %s=<that secret> in the shell that runs wso2 and run this command again.",
				flags.federationClientID, base, FederationSecretVariable))
	}
	var mappings []apim.Mapping
	for _, pair := range flags.groupMappings {
		group, role, found := strings.Cut(pair, "=")
		if !found || group == "" || role == "" {
			return nil, problem.New(problem.CategoryUsage, "apim.invalid_flag",
				fmt.Sprintf("--map-group %q is not a <group>=<role> pair", pair)).
				WithRecovery(fmt.Sprintf("Name the login provider's group and the API Manager role it maps to, "+
					"as --map-group %s.", DefaultGroupMapping))
		}
		mappings = append(mappings, apim.Mapping{Remote: group, Local: role})
	}
	internal := strings.TrimRight(flags.loginProviderInternalURL, "/")
	if internal == "" {
		internal = issuer
	}
	name := flags.identityProvider
	if name == "" {
		name = identityProviderName(issuer)
	}
	return &federation{loginProvider: issuer, internalURL: internal, clientID: flags.federationClientID,
		clientSecret: secret, identityProvider: name, publicClientName: flags.publicClientName,
		groupMappings: mappings}, nil
}

// identityProviderName is wso2-cli- and the issuer's host and port, with
// the dots and colon a name cannot carry replaced by hyphens, so that two
// deployments do not collide.
func identityProviderName(issuer string) string {
	host := issuer
	if parsed, err := url.Parse(issuer); err == nil && parsed.Host != "" {
		host = parsed.Host
	}
	return identityProviderPrefix + strings.NewReplacer(".", "-", ":", "-").Replace(host)
}

// federate writes the identity provider and the public client, each read
// first and written only when the read differs, and builds the result with
// the confidential client's rows kept.
func federate(ctx context.Context, admin *apim.Admin, federation *federation, base string,
	registered registration) (result.Result, error) {
	want := &apim.IdentityProvider{
		Name:        federation.identityProvider,
		Description: "Registered by wso2 apim bootstrap for " + federation.loginProvider,
		Authenticator: apim.OpenIDConnect{
			ClientID: federation.clientID, ClientSecret: federation.clientSecret,
			AuthorizationEndpoint: federation.loginProvider + "/oauth2/authorize",
			TokenEndpoint:         federation.internalURL + "/oauth2/token",
			UserInfoEndpoint:      federation.internalURL + "/oauth2/userinfo",
			Callback:              base + "/commonauth",
			// The resource parameter is required: the login provider refuses
			// an authorization that names none, and the groups scope is what
			// the role mapping reads.
			QueryParameters: "scope=openid email groups&resource=" + base + "/oauth2/token",
			Scopes:          "openid email groups",
		},
		ClaimMappings:   []apim.Mapping{{Remote: "groups", Local: apim.RoleClaim}, {Remote: "email", Local: apim.EmailClaim}},
		UserClaim:       "sub",
		RoleMappings:    federation.groupMappings,
		JITProvisioning: true,
	}
	existing, found, err := admin.IdentityProvider(ctx, want.Name)
	if err != nil {
		return result.Result{}, apim.Problem(err, "the identity provider lookup")
	}
	identityProviderCreated := !found
	switch {
	case !found:
		if err := admin.AddIdentityProvider(ctx, want); err != nil {
			return result.Result{}, apim.Problem(err, "the identity provider registration")
		}
	case !existing.Federates(want):
		// What the provider carried and bootstrap does not set stays,
		// its description among them.
		want.Alias, want.Properties = existing.Alias, existing.Properties
		if existing.Description != "" {
			want.Description = existing.Description
		}
		if err := admin.UpdateIdentityProvider(ctx, want); err != nil {
			return result.Result{}, apim.Problem(err, "the identity provider update")
		}
	}

	public, found, err := admin.FindClient(ctx, federation.publicClientName)
	if err != nil {
		return result.Result{}, apim.Problem(err, "the public client lookup")
	}
	publicClientCreated := !found
	if !found {
		public, err = admin.RegisterClient(ctx, federation.publicClientName, loopbackCallbacks,
			[]string{"authorization_code", "refresh_token"})
		if err != nil {
			return result.Result{}, apim.Problem(err, "the public client registration")
		}
	}
	app, err := admin.OAuthApplication(ctx, public.ID)
	if err != nil {
		return result.Result{}, apim.Problem(err, "the public client's settings")
	}
	if !app.Public || !app.PKCEMandatory || app.TokenType != "JWT" {
		app.Public, app.PKCEMandatory, app.TokenType = true, true, "JWT"
		if err := admin.UpdateOAuthApplication(ctx, app); err != nil {
			return result.Result{}, apim.Problem(err, "the public client update")
		}
	}
	provider, found, err := admin.ServiceProvider(ctx, federation.publicClientName)
	if err != nil {
		return result.Result{}, apim.Problem(err, "the public client's authentication lookup")
	}
	if !found {
		return result.Result{}, problem.New(problem.CategoryProductService, "apim.unreadable",
			fmt.Sprintf("API Manager registered the public client %q but has no application of that name", federation.publicClientName)).
			WithRecovery("Check the client under Service Providers in the API Manager console, then run the command again.")
	}
	if !provider.Federates(want.Name) {
		provider.FederatedThrough = want.Name
		if err := admin.UpdateServiceProvider(ctx, provider); err != nil {
			return result.Result{}, apim.Problem(err, "the public client's authentication update")
		}
	}

	// The public client's id is what connect needs, so it leads. The
	// pipeline sentence names the variable that carries the confidential
	// client's secret, never the value.
	next := fmt.Sprintf("Run wso2 %s connect %s --client-id %s. For a pipeline: export %s=<the client secret "+
		"above, shown once>; then wso2 %s connect %s --client-id %s --client-secret-variable %s, on a "+
		"client-credentials identity.",
		Namespace, base, public.ID, ClientSecretVariable, Namespace, base, registered.ClientID, ClientSecretVariable)
	return result.New(BootstrapSchema).
		With("issuer", "Issuer", base+"/oauth2/token").
		With("clientId", "Client ID", registered.ClientID).
		With("clientSecret", "Client secret", registered.ClientSecret).
		With("tokenType", "Token type", "JWT").
		With("identityProvider", "Identity provider", want.Name+" "+presence(identityProviderCreated)).
		With("publicClient", "Public client", public.ID+" "+presence(publicClientCreated)).
		With(NextField, "Next", next), nil
}

func presence(created bool) string {
	if created {
		return "(created)"
	}
	return "(present)"
}
