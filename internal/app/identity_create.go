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
	"github.com/wso2/wso2-cli/sdk/result"
)

const identityCreateUsage = "Run wso2 account create <name> --issuer <url> --client-id <id> " +
	"[--client-secret-variable <VAR>] [--provider <name>] " +
	"[--product <namespace> --endpoint <url> [--audience <uri>] [--scope <scope>]...]."

// identityCreateSchema names the shape identity create renders.
const identityCreateSchema = "shell.identity-created/v1"

// identityCreateFlags is what the command takes. None of it is a credential:
// the secret variable is a name, and the credential reference the browser kind
// records is the account's own name (ADR 0012).
type identityCreateFlags struct {
	issuer, clientID, secretVariable, provider string
	product, endpoint, audience                string
	scopes                                     []string
}

// identityCreateCommand declares an identity and its context without a login.
//
// It is the hand-off point from a product module's bootstrap to the shell: the
// module registers the OAuth client, prints this line, and the user runs it. It
// is also how a CI identity, or one that names its provider, is written without
// editing the document by hand.
func (s Shell) identityCreateCommand() *cobra.Command {
	var flags identityCreateFlags
	command := &cobra.Command{
		Use:   "create <name> --issuer <url> --client-id <id>",
		Short: "Declare an identity and a same-named context without a login.",
		Args:  exactlyOneArgument("an account name", identityCreateUsage),
		RunE: func(command *cobra.Command, args []string) error {
			return s.identityCreate(command, args[0], flags)
		},
	}
	f := command.Flags()
	f.StringVar(&flags.issuer, "issuer", "", "The token issuer this identity authenticates against.")
	f.StringVar(&flags.clientID, "client-id", "", "The OAuth application the shell presents.")
	f.StringVar(&flags.secretVariable, "client-secret-variable", "",
		"The environment variable holding the client secret; makes this a client-credentials account.")
	f.StringVar(&flags.provider, "provider", "",
		"The identity provider: "+strings.Join(contexts.Providers(), ", ")+".")
	f.StringVar(&flags.product, "product", "", "A product namespace this identity reaches.")
	f.StringVar(&flags.endpoint, "endpoint", "", "The product service's base URL.")
	f.StringVar(&flags.audience, "audience", "", "The token audience the product's services accept.")
	f.StringArrayVar(&flags.scopes, "scope", nil,
		"A permission the shell may request for the product; repeat for each.")
	return command
}

func (s Shell) identityCreate(command *cobra.Command, name string, flags identityCreateFlags) error {
	mode, err := s.shellOutputMode(command)
	if err != nil {
		return err
	}
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	identity, err := plannedIdentity(name, flags)
	if err != nil {
		return err
	}
	if err := contexts.Writable(root); err != nil {
		return s.explainWriteRefusal(root, err)
	}

	s.log.Debug("writing an identity and context by declaration",
		"identity", name, "kind", identity.Auth.Kind, "issuer", flags.issuer,
		"client_id", flags.clientID, "provider", flags.provider, "product", flags.product,
		"document", contexts.Path(root))

	selected := false
	err = contexts.Update(root, func(document contexts.Document) (contexts.Document, error) {
		if declaresIdentity(document, name) {
			return document, identityExists(name)
		}
		if declaresContext(document, name) {
			return document, contextExists(name)
		}
		document.SchemaVersion = contexts.SchemaVersion
		document.Accounts = append(document.Accounts, identity)
		document.Contexts = append(document.Contexts, contexts.Context{Name: name, Account: name})
		if document.DefaultContext == "" {
			document.DefaultContext = name
			selected = true
		}
		return document, nil
	})
	if err != nil {
		return s.explainWriteRefusal(root, err)
	}

	created := result.New(identityCreateSchema).
		With("identity", "Identity", name).
		With("context", "Context", name).
		With("kind", "Kind", identity.Auth.Kind).
		With("issuer", "Issuer", flags.issuer).
		With("clientId", "Client ID", flags.clientID).
		With("provider", "Provider", flags.provider).
		With("product", "Product", flags.product).
		With("endpoint", "Endpoint", flags.endpoint).
		With("audience", "Audience", flags.audience).
		With("scopes", "Scopes", strings.Join(flags.scopes, ",")).
		With("selected", "Selected", fmt.Sprintf("%t", selected)).
		With(output.NextField, "Next", identityCreateNext(name, identity))
	return output.Report(s.Streams.Out, mode, created)
}

// plannedIdentity turns the flags into the identity the document will hold,
// refusing anything the document would refuse, before the document is opened.
func plannedIdentity(name string, flags identityCreateFlags) (contexts.Account, error) {
	if !contexts.ValidName(name) {
		return contexts.Account{}, problem.New(problem.CategoryUsage, "shell.invalid_argument",
			fmt.Sprintf("%q cannot be used as an account name", name)).
			WithRecovery(fmt.Sprintf("An account name is %s. %s", contexts.NameRule, identityCreateUsage))
	}
	if flags.issuer == "" {
		return contexts.Account{}, problem.New(problem.CategoryUsage, "shell.missing_required_flag",
			"wso2 account create needs the issuer the identity authenticates against").
			WithRecovery(identityCreateUsage)
	}
	if err := refuseNonIssuerURL(flags.issuer); err != nil {
		return contexts.Account{}, err
	}
	if flags.clientID == "" {
		return contexts.Account{}, problem.New(problem.CategoryUsage, "shell.missing_required_flag",
			"wso2 account create needs the client id of the OAuth application the shell presents").
			WithRecovery(identityCreateUsage)
	}
	if flags.provider != "" && !slices.Contains(contexts.Providers(), flags.provider) {
		return contexts.Account{}, problem.New(problem.CategoryUsage, "shell.invalid_argument",
			fmt.Sprintf("%q is not an identity provider this shell knows", flags.provider)).
			WithRecovery("Pass one of " + strings.Join(contexts.Providers(), ", ") +
				", or omit --provider for any other OpenID provider.")
	}
	productFlagsGiven := flags.endpoint != "" || flags.audience != "" || len(flags.scopes) > 0
	if flags.product == "" && productFlagsGiven {
		return contexts.Account{}, problem.New(problem.CategoryUsage, "shell.conflicting_arguments",
			"--endpoint, --audience and --scope describe a product and need --product").
			WithRecovery(identityCreateUsage)
	}
	if flags.product != "" && !contexts.ValidName(flags.product) {
		return contexts.Account{}, problem.New(problem.CategoryUsage, "shell.invalid_argument",
			fmt.Sprintf("%q cannot be used as a product namespace", flags.product)).
			WithRecovery(fmt.Sprintf("A product namespace is %s. %s", contexts.NameRule, identityCreateUsage))
	}
	if flags.product != "" && flags.endpoint == "" {
		return contexts.Account{}, problem.New(problem.CategoryUsage, "shell.missing_required_flag",
			"wso2 account create needs the endpoint the product is served at").
			WithRecovery(identityCreateUsage + " A self-hosted deployment publishes no " +
				"catalogue of what it serves, so the endpoint can only come from you.")
	}

	auth := contexts.AccountAuth{
		Kind:     contexts.KindOAuthBrowser,
		Issuer:   flags.issuer,
		ClientID: flags.clientID,
		Provider: flags.provider,
	}
	if flags.secretVariable != "" {
		auth.Kind = contexts.KindClientCredentials
		auth.ClientSecretVariable = flags.secretVariable
	} else {
		auth.CredentialRef = name
	}
	if flags.product != "" && flags.audience == "" && auth.Derivation() == contexts.DerivationTokenResource {
		return contexts.Account{}, problem.New(problem.CategoryUsage, "shell.missing_required_flag",
			fmt.Sprintf("a %q identity binds its login to one protected resource, so the product needs --audience",
				flags.provider)).
			WithRecovery("Pass --audience with the resource server's identifier as the deployment registers it. " +
				identityCreateUsage)
	}
	identity := contexts.Account{Name: name, Type: contexts.IdentityTypeForIssuer(auth.Issuer), Auth: auth}
	if flags.product != "" {
		identity.Products = map[string]contexts.Product{
			flags.product: {Endpoint: flags.endpoint, Audience: flags.audience, Scopes: flags.scopes},
		}
	}
	return identity, nil
}

// identityCreateNext is the one thing a user most likely runs after this.
func identityCreateNext(name string, identity contexts.Account) string {
	if identity.Auth.Kind == contexts.KindOAuthBrowser {
		return fmt.Sprintf("Run wso2 login --context %s.", name)
	}
	for namespace := range identity.Products {
		return fmt.Sprintf("Run wso2 %s status --context %s.", namespace, name)
	}
	return fmt.Sprintf("Run wso2 account add-product %s <namespace> --endpoint <url> to record what it reaches.", name)
}

func identityExists(name string) problem.Problem {
	return problem.New(problem.CategoryUsage, "contexts.identity_exists",
		fmt.Sprintf("an account named %q is already declared in the context document", name)).
		WithRecovery("Run wso2 account list to see it, or pick another name.")
}
