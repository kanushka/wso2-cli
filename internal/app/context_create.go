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
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// contextCreateFlags are what wso2 context create takes beyond the name. None
// of it is a credential: the secret variable is a name.
type contextCreateFlags struct {
	loginProduct, url          string
	issuer, clientID, provider string
	audience                   string
	scopes                     []string
	scopesSet                  bool
	device                     bool
	clientSecretVariable       string
	organization, project      string
	use, noInstall             bool
}

func (s Shell) contextCreateCommand() *cobra.Command {
	var flags contextCreateFlags
	command := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a context that logs in through a product or an issuer. Makes no network call.",
		Long: "Create a context in one of two forms.\n\n" +
			"  wso2 context create <name> --login-product <product> --url <url>\n" +
			"      logs in through an installed product that is a login provider, such as identity.\n" +
			"      Its descriptor fills in the issuer, client, audience and scopes, and the values are\n" +
			"      written to the context file. The product is installed first when it is missing.\n\n" +
			"  wso2 context create <name> --issuer <url> --client-id <id>\n" +
			"      logs in through any OpenID provider directly. Nothing needs to be installed.\n\n" +
			"Add the products the context reaches with wso2 context product add, then run wso2 login.",
		Args: exactlyOneArgument("a name for the context", contextCreateUsage),
		RunE: func(command *cobra.Command, args []string) error {
			flags.scopesSet = command.Flags().Changed("scopes")
			return s.contextCreate(command, args[0], flags)
		},
	}
	f := command.Flags()
	f.StringVar(&flags.loginProduct, "login-product", "",
		"The installed product the context logs in through, such as identity.")
	f.StringVar(&flags.url, "url", "", "The login product's URL. Needs --login-product.")
	f.StringVar(&flags.issuer, "issuer", "", "The OpenID issuer to log in through, without a login product.")
	f.StringVar(&flags.clientID, "client-id", "",
		"The OAuth client the shell presents: required with --issuer, the descriptor's by default otherwise.")
	f.StringVar(&flags.provider, "provider", "",
		"The identity provider behind the issuer: "+strings.Join(contexts.Providers(), ", ")+".")
	f.StringVar(&flags.audience, "audience", "", "The login product's audience, when not the descriptor's.")
	f.StringSliceVar(&flags.scopes, "scopes", nil, "The login product's scopes, when not the descriptor's.")
	f.BoolVar(&flags.device, "device", false, "Log in with a device code instead of the browser.")
	f.StringVar(&flags.clientSecretVariable, "client-secret-variable", "",
		"The environment variable holding a client secret: the context logs in with client credentials.")
	f.StringVar(&flags.organization, "organization", "", "Run commands within this organization.")
	f.StringVar(&flags.project, "project", "", "Narrow the target to this project inside the organization.")
	f.BoolVar(&flags.use, "use", false, "Select the new context.")
	f.BoolVar(&flags.noInstall, "no-install", false,
		"Refuse rather than install a login product that is not installed.")
	return command
}

// contextCreate writes one new context.
//
// It performs no network call besides installing a missing login product,
// which it reports: an issuer typo has to surface at wso2 login, where a user
// is already waiting on the identity provider, rather than here (ADR 0011).
func (s Shell) contextCreate(command *cobra.Command, name string, flags contextCreateFlags) error {
	mode, err := s.shellOutputMode(command)
	if err != nil {
		return err
	}
	if err := checkContextCreateFlags(name, flags); err != nil {
		return err
	}
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	if err := contexts.Writable(root); err != nil {
		return s.explainWriteRefusal(root, err)
	}
	document, err := contexts.Load(root)
	if err != nil {
		return err
	}
	if declaresContext(document, name) {
		return contextExists(name)
	}

	login := contexts.Login{Kind: contexts.KindOAuthBrowser, ClientID: flags.clientID, Provider: flags.provider}
	switch {
	case flags.device:
		login.Kind = contexts.KindOAuthDevice
	case flags.clientSecretVariable != "":
		login.Kind = contexts.KindClientCredentials
		login.ClientSecretVariable = flags.clientSecretVariable
	}
	var products map[string]contexts.Product
	installed := ""
	if flags.loginProduct != "" {
		url, err := productURL("--url", flags.url)
		if err != nil {
			return err
		}
		installed, err = s.ensureInstalled(flags.loginProduct, flags.noInstall)
		if err != nil {
			return err
		}
		descriptor := s.installedDescriptors()(flags.loginProduct)
		spec := productSpec{URL: url, Audience: flags.audience}
		if flags.scopesSet {
			spec.Scopes = flags.scopes
		}
		var product contexts.Product
		login, product, err = resolveLoginProduct(flags.loginProduct, descriptor, spec, login)
		if err != nil {
			return err
		}
		products = map[string]contexts.Product{flags.loginProduct: product}
	} else {
		if err := refuseNonIssuerURL(flags.issuer); err != nil {
			return err
		}
		login.Issuer = strings.TrimRight(flags.issuer, "/")
		if login.Provider == contexts.ProviderThunder {
			return problem.New(problem.CategoryUsage, "shell.conflicting_arguments",
				"a thunder deployment binds every login to a product, and --issuer creates a context "+
					"that records none").
				WithRecovery(fmt.Sprintf("Log in through the identity product instead: wso2 context create "+
					"%s --login-product identity --url <thunder-url>.", name))
		}
	}
	login.Tenant = contexts.TenantForIssuer(login.Issuer)
	created := contexts.Context{
		Name:          name,
		Type:          contexts.IdentityTypeForIssuer(login.Issuer),
		CredentialRef: document.NewCredentialRef(name),
		Login:         login,
		Organization:  flags.organization,
		Project:       flags.project,
		Products:      products,
	}
	if login.Kind == contexts.KindClientCredentials {
		created.CredentialRef = ""
	}
	if created.Organization == "" {
		created.Organization = login.Tenant
	}

	s.log.Debug("creating a context",
		"context", name, "login_product", flags.loginProduct, "issuer", login.Issuer,
		"client_id", login.ClientID, "document", contexts.Path(root))

	selected := false
	err = contexts.Update(root, func(document contexts.Document) (contexts.Document, error) {
		if declaresContext(document, name) {
			return document, contextExists(name)
		}
		// Assigned again under the lock: another invocation may have taken
		// the reference planned above since.
		if created.CredentialRef != "" {
			created.CredentialRef = document.NewCredentialRef(name)
		}
		document = document.Put(created)
		if flags.use {
			document.DefaultContext = name
			selected = true
		}
		return document, nil
	})
	if err != nil {
		return explainChangeRefusal(s.explainWriteRefusal(root, err))
	}
	report := contextCreated{
		Context: name, Type: created.Type, Kind: login.Kind, Issuer: login.Issuer,
		ClientID: login.ClientID, LoginProduct: login.Product, Products: productList(created),
		Organization: created.Organization, Project: created.Project, Selected: selected,
		Installed: installed,
	}
	if mode == output.ModeJSON {
		return renderContext(s.Streams.Out, mode, report)
	}
	if _, err := fmt.Fprintf(s.Streams.Out, "\nCreated the %q context.\n\n", name); err != nil {
		return err
	}
	return renderContext(s.Streams.Out, mode, report)
}

// checkContextCreateFlags refuses a line that names neither form, or both,
// before anything is read. A URL alone does not say which product's
// descriptor applies, so it is never guessed at.
func checkContextCreateFlags(name string, flags contextCreateFlags) error {
	if !contexts.ValidName(name) {
		return problem.New(problem.CategoryUsage, "shell.invalid_argument",
			fmt.Sprintf("%q cannot be used as a context name", name)).
			WithRecovery(fmt.Sprintf("A context name is %s. %s", contexts.NameRule, contextCreateUsage))
	}
	switch {
	case flags.loginProduct != "" && flags.issuer != "":
		return problem.New(problem.CategoryUsage, "shell.conflicting_arguments",
			"--login-product and --issuer are two ways to say how the context logs in; pass one").
			WithRecovery(contextCreateUsage)
	case flags.url != "" && flags.loginProduct == "":
		return problem.New(problem.CategoryUsage, "shell.missing_required_flag",
			"--url names where a product runs, and needs --login-product to say which product it is").
			WithRecovery("A URL alone does not say which product's defaults apply, so the shell does not " +
				"guess. " + contextCreateUsage)
	case flags.loginProduct != "" && flags.url == "":
		return problem.New(problem.CategoryUsage, "shell.missing_required_flag",
			fmt.Sprintf("--login-product %s needs --url with the product's URL", flags.loginProduct)).
			WithRecovery(contextCreateUsage)
	case flags.loginProduct == "" && flags.issuer == "":
		return problem.New(problem.CategoryUsage, "shell.missing_required_flag",
			"wso2 context create needs --login-product <product> --url <url>, or --issuer <url> --client-id <id>").
			WithRecovery(contextCreateUsage)
	case flags.issuer != "" && flags.clientID == "":
		return problem.New(problem.CategoryUsage, "shell.missing_required_flag",
			"--issuer needs --client-id with the OAuth application the shell presents").
			WithRecovery(contextCreateUsage)
	case flags.issuer != "" && (flags.audience != "" || flags.scopesSet):
		return problem.New(problem.CategoryUsage, "shell.conflicting_arguments",
			"--audience and --scopes describe a login product, and --issuer logs in without one").
			WithRecovery("Add products with wso2 context product add once the context exists. " +
				contextCreateUsage)
	case flags.device && flags.clientSecretVariable != "":
		return problem.New(problem.CategoryUsage, "shell.conflicting_arguments",
			"--device and --client-secret-variable are two different ways to log in; pass one").
			WithRecovery(contextCreateUsage)
	}
	if flags.loginProduct != "" && !contexts.ValidName(flags.loginProduct) {
		return problem.New(problem.CategoryUsage, "shell.invalid_argument",
			fmt.Sprintf("%q cannot be a product namespace", flags.loginProduct)).
			WithRecovery(contextCreateUsage)
	}
	if flags.provider != "" && !slices.Contains(contexts.Providers(), flags.provider) {
		return problem.New(problem.CategoryUsage, "shell.invalid_argument",
			fmt.Sprintf("%q is not an identity provider this shell knows", flags.provider)).
			WithRecovery("Pass one of " + strings.Join(contexts.Providers(), ", ") +
				", or omit --provider for any other OpenID provider.")
	}
	if flags.clientSecretVariable != "" && !contexts.ValidVariable(flags.clientSecretVariable) {
		return problem.New(problem.CategoryUsage, "shell.invalid_argument",
			"--client-secret-variable does not name an environment variable").
			WithRecovery("Pass the name of the variable holding the secret, not the secret: a variable " +
				"name is upper-case letters, digits and underscores. The value is not repeated here.")
	}
	return nil
}

// ensureInstalled installs a product that is not installed yet, saying so, and
// reports the version it installed, empty when it was already there. Under
// noInstall a missing product is refused instead.
func (s Shell) ensureInstalled(namespace string, noInstall bool) (string, error) {
	installed, err := s.installedProduct(namespace)
	if err != nil {
		return "", err
	}
	if installed != nil {
		return "", nil
	}
	if noInstall {
		return "", problem.New(problem.CategoryUsage, "shell.product_not_installed",
			fmt.Sprintf("the %s product is not installed, and --no-install forbids installing it", namespace)).
			WithRecovery(fmt.Sprintf("Run wso2 product install %s first, or drop --no-install.", namespace))
	}
	if _, err := fmt.Fprintf(s.Streams.Err, "The %s product is not installed; its descriptor supplies "+
		"this context's defaults, so it is installed first.\n", namespace); err != nil {
		return "", err
	}
	if err := s.install([]installNeed{{Namespace: namespace}}); err != nil {
		return "", err
	}
	after, err := s.installedProduct(namespace)
	if err != nil || after == nil {
		return "", err
	}
	return after.Receipt.ModuleVersion, nil
}

// contextCreated is what wso2 context create reports.
type contextCreated struct {
	Context      string   `json:"context"`
	Type         string   `json:"type"`
	Kind         string   `json:"kind"`
	Issuer       string   `json:"issuer"`
	ClientID     string   `json:"clientId"`
	LoginProduct string   `json:"loginProduct"`
	Products     []string `json:"products"`
	Organization string   `json:"organization"`
	Project      string   `json:"project"`
	Selected     bool     `json:"selected"`
	// Installed is the version of the login product this command installed,
	// empty when it was installed already or there is none.
	Installed string `json:"installed"`
}

func (c contextCreated) fields() [][2]string {
	return [][2]string{
		{"Context", c.Context},
		{"Type", c.Type},
		{"Login", c.Kind},
		{"Issuer", c.Issuer},
		{"Client ID", c.ClientID},
		{"Login product", c.LoginProduct},
		{"Products", strings.Join(c.Products, ", ")},
		{"Organization", c.Organization},
		{"Project", c.Project},
		{"Selected", yesNo(c.Selected)},
	}
}

func (c contextCreated) next() string {
	context := " --context " + c.Context
	if c.Selected {
		context = ""
	}
	switch {
	case c.Kind == contexts.KindClientCredentials:
		return fmt.Sprintf("Run wso2 context product add <product> --url <url>%s to add what it reaches.", context)
	case c.LoginProduct == "":
		return fmt.Sprintf("Run wso2 context product add <product> --url <url>%s, then wso2 login%s.",
			context, context)
	case !c.Selected:
		return fmt.Sprintf("Run wso2 context use %s, then wso2 login. Add more products with wso2 context "+
			"product add <product> --url <url>.", c.Context)
	}
	return "Run wso2 login. Add more products with wso2 context product add <product> --url <url>."
}
