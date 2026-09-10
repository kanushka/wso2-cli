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
	"maps"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/result"
)

// connectSubcommand is the word after a product namespace that the shell
// answers itself. A module never sees it: the record connect writes is the
// shell's, and writing it grants nothing (ADR 0012).
const connectSubcommand = "connect"

// connectSchema names the shape connect renders.
const connectSchema = "shell.connected/v1"

// connectFlags is what connect takes beyond the URL. None of it is a
// credential: the two variables are names.
type connectFlags struct {
	identity, loginProvider, clientID, audience string
	clientIDVariable, clientSecretVariable      string
	scopes                                      []string
	replace                                     bool
	// gateway records the product's gateway record instead of the product.
	gateway bool
	// clientIDSet reports that --client-id was written on the line, as
	// opposed to carrying the descriptor's default.
	clientIDSet bool
	// noInput is --no-input, taken off the product line before connect
	// parses it (dispatchNamespace): it stops connect asking for the name of
	// an account it creates.
	noInput bool
}

// connectVariablePattern is the shape a credential variable name has, and
// connectVariableRule states it in the words a refusal uses. The document
// enforces the same rule when it encodes the record; asking it here is what
// lets connect complain about the flag the user typed rather than about the
// file they never wrote to, whose recovery offers to remove it.
var connectVariablePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

const connectVariableRule = "upper-case letters, digits and underscores, starting with a letter, " +
	"at most 64 characters"

// connectUsage is the way back from connect's usage refusals.
func connectUsage(namespace string) string {
	return fmt.Sprintf("Run wso2 %s connect <url> [--account <name>] [--login-provider <issuer-url>] "+
		"[--client-id <id>] [--client-secret-variable <VAR>] [--audience <value>] "+
		"[--scopes <list>] [--replace], or wso2 %s connect <gateway-url> --gateway to record the "+
		"product's gateway.", namespace, namespace)
}

// connect answers wso2 <namespace> connect <url> from the product descriptor
// the module's receipt carries.
//
// It runs after the module was resolved and before it is launched, so the
// receipt it reads is the verified one, and the module contributes exactly
// the descriptor it was installed with.
func (s Shell) connect(namespace string, receipt modules.Receipt, args []string, noInput bool) error {
	descriptor := receipt.Capabilities.Product
	if descriptor == nil {
		return problem.New(problem.CategoryUsage, "shell.connect_unsupported",
			fmt.Sprintf("the %s module declares no product descriptor, so the shell cannot write its "+
				"record from a URL", namespace)).
			WithRecovery(fmt.Sprintf("Record the product with wso2 account add-product <account> %s "+
				"--endpoint <url> [--audience <value>] [--scopes <list>], or install a version of the "+
				"module that declares one.", namespace))
	}
	command := s.connectCommand(namespace, *descriptor, noInput)
	command.SetArgs(args)
	return command.Execute()
}

// connectCommand builds the one-off command connect parses its line with. It
// is a Cobra command so that --help, --output and --context are read the way
// every shell command reads them.
func (s Shell) connectCommand(namespace string, descriptor modules.ProductDescriptor,
	noInput bool) *cobra.Command {
	flags := connectFlags{noInput: noInput}
	command := &cobra.Command{
		Use:   fmt.Sprintf("wso2 %s connect <url>", namespace),
		Short: fmt.Sprintf("Record the %s product at a URL, on the selected account or a new one.", namespace),
		Long: "Writes the product record the module's descriptor describes: the issuer and audience " +
			"derived from the URL, the scopes its commands need, and the grant it is reached by when " +
			"the account's login provider is another product. Nothing is written to the secure " +
			"store and no network call is made; wso2 login is what authorizes it.",
		Args:                  exactlyOneArgument("the product's URL", connectUsage(namespace)),
		SilenceErrors:         true,
		SilenceUsage:          true,
		DisableFlagsInUseLine: true,
		RunE: func(command *cobra.Command, args []string) error {
			flags.clientIDSet = command.Flags().Changed("client-id")
			return s.connectRun(command, namespace, descriptor, args[0], flags)
		},
	}
	command.SetFlagErrorFunc(func(command *cobra.Command, err error) error {
		return usageProblemWithRecovery(err, connectUsage(namespace))
	})
	command.SetHelpTemplate(helpTemplate)
	command.SetUsageTemplate(helpTemplate)
	command.SetOut(s.Streams.Out)
	command.SetErr(s.Streams.Err)
	f := command.Flags()
	// Declared by hand, as the root declares its own: Cobra's default help
	// text names the command by the first word of Use, which here is wso2.
	f.BoolP("help", "h", false, "Show help for a command.")
	f.StringVar(&flags.identity, "account", "",
		"The account to record the product on, or to create; defaults to the selected context's, "+
			"or to the next free account-N for a new one, asked for when a prompt is allowed.")
	f.StringVar(&flags.loginProvider, "login-provider", "",
		"The issuer URL of the account to record the product on, when several exist.")
	f.StringVar(&flags.clientID, "client-id", descriptor.ClientID,
		"The OAuth client the shell presents at the product's issuer.")
	f.StringVar(&flags.clientSecretVariable, "client-secret-variable", "",
		"The environment variable holding a client secret: creates a client-credentials account "+
			"for a login provider, or records the product's own credential on one.")
	f.StringVar(&flags.clientIDVariable, "client-id-variable", "",
		"The environment variable holding the product credential's client id, when it is not --client-id.")
	f.StringVar(&flags.audience, "audience", "", "The token audience, when not the descriptor's default.")
	f.StringSliceVar(&flags.scopes, "scopes", nil,
		"The permissions to record, comma-separated, when not the descriptor's.")
	f.BoolVar(&flags.replace, "replace", false, "Replace the product's existing record instead of refusing.")
	f.BoolVar(&flags.gateway, "gateway", false,
		"Record the product's gateway at the URL, beside the product's own record on the account: "+
			"reached at the account's login provider for the API named by --audience.")
	declareContextFlag(f)
	declareOutputFlag(f)
	return command
}

// connectRun plans and writes the record.
func (s Shell) connectRun(command *cobra.Command, namespace string, descriptor modules.ProductDescriptor,
	rawURL string, flags connectFlags) error {
	mode, err := s.shellOutputMode(command)
	if err != nil {
		return err
	}
	productURL, err := connectURL(namespace, rawURL)
	if err != nil {
		return err
	}
	if flags.gateway {
		if err := checkGatewayFlags(namespace, descriptor, flags); err != nil {
			return err
		}
	} else if err := checkConnectFlags(namespace, descriptor, flags); err != nil {
		return err
	}
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	if err := contexts.Writable(root); err != nil {
		return s.explainWriteRefusal(root, err)
	}
	contextName := ""
	if flag := shellFlag(command, contextFlag); flag != nil {
		contextName = flag.Value.String()
	}
	if contextName == "" {
		contextName = os.Getenv("WSO2_CONTEXT")
	}

	// Public facts only: a namespace, a URL the user typed, scheme names.
	s.log.Debug("connecting a product",
		"namespace", namespace, "provider", descriptor.Provider, "grant", descriptor.Grant,
		"gateway", flags.gateway,
		"identity", flags.identity, "login_provider", flags.loginProvider, "replace", flags.replace,
		"document", contexts.Path(root))

	if flags.gateway {
		var plan gatewayPlan
		err = contexts.Update(root, func(document contexts.Document) (contexts.Document, error) {
			planned, err := planGateway(document, namespace, descriptor, productURL, flags, contextName)
			if err != nil {
				return document, err
			}
			plan = planned
			return plan.apply(document), nil
		})
		if err != nil {
			return s.explainWriteRefusal(root, err)
		}
		return s.reportGateway(mode, root, plan)
	}

	providers := s.loginProviderNamespaces()
	assigned, chosen, err := s.connectAccountName(root, namespace, descriptor, productURL, flags,
		contextName, providers)
	if err != nil {
		return err
	}
	// A name the user typed is held to what --account is held to: taken by
	// the time the write runs, it is refused rather than swapped for another.
	if chosen {
		flags.identity = assigned
	}
	var plan connectPlan
	err = contexts.Update(root, func(document contexts.Document) (contexts.Document, error) {
		planned, err := planConnect(document, namespace, descriptor, productURL, flags, contextName,
			providers, assigned)
		if err != nil {
			return document, err
		}
		plan = planned
		return plan.apply(document), nil
	})
	if err != nil {
		return s.explainWriteRefusal(root, err)
	}
	plan.assigned = plan.created && flags.identity == ""
	return s.reportConnect(mode, root, namespace, plan)
}

// connectAccountName is the name a connect that creates an account gives it
// when --account did not name one, and whether the user typed it at the
// prompt rather than accepting the shell's.
//
// The document is read and the connect planned before the write takes its
// lock, for the reason wso2 login plans twice: nothing waits on a person while
// holding the lock, and a connect that creates nothing is asked nothing. A
// refusal here is left to the plan the write makes, which is the one that
// decides anything.
func (s Shell) connectAccountName(root, namespace string, descriptor modules.ProductDescriptor,
	productURL string, flags connectFlags, contextName string, providers []string) (string, bool, error) {
	if flags.identity != "" {
		return "", false, nil
	}
	document, err := contexts.Load(root)
	if err != nil {
		return "", false, err
	}
	planned, err := planConnect(document, namespace, descriptor, productURL, flags, contextName, providers, "")
	if err != nil || !planned.created {
		return "", false, nil
	}
	return s.askAccountName(document, flags.noInput)
}

// exchangedNext is the next line for a record reached by exchanging the
// account's login session when that session is already held: there is
// nothing left to authorize, so the line names the product rather than a
// login. Whether it is held is read from the secure store, never written; a
// store that cannot be read is reported as holding nothing, which leaves the
// caller's login line in place.
func exchangedNext(root, namespace string, identity contexts.Account, access contexts.ProductAccess) (string, bool) {
	if access.Strategy != contexts.StrategyExchanged || identity.Auth.Kind == contexts.KindClientCredentials {
		return "", false
	}
	login := identity.LoginAccess()
	if login.SessionRef == "" {
		return "", false
	}
	if _, err := (session.Store{StateRoot: root}).Load(login.SessionRef); err != nil {
		return "", false
	}
	return fmt.Sprintf("Run wso2 %s --help. The account's login session already reaches it, so "+
		"there is nothing to log in to.", namespace), true
}

// connectURL refuses a product URL that is not one. The value is not echoed:
// like an issuer, it is where a credential is likeliest to have been pasted.
func connectURL(namespace, raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", problem.New(problem.CategoryUsage, "shell.invalid_argument",
			"the URL is not an absolute http or https URL").
			WithRecovery(fmt.Sprintf("Pass the product's URL as the deployment publishes it, as in "+
				"wso2 %s connect https://host:port. A missing https:// is the usual cause. The value "+
				"is not repeated here, in case it holds a secret.", namespace))
	}
	if parsed.User != nil {
		return "", problem.New(problem.CategoryUsage, "shell.invalid_argument",
			"the URL carries a user name or password, which a product URL may not").
			WithRecovery("Pass the URL on its own. The shell authenticates through wso2 login, so a " +
				"credential in the URL is never used. The value is not repeated here.")
	}
	return strings.TrimRight(raw, "/"), nil
}

// checkConnectFlags refuses a combination of flags that describes nothing the
// shell can write, before the document is opened.
func checkConnectFlags(namespace string, descriptor modules.ProductDescriptor, flags connectFlags) error {
	usage := connectUsage(namespace)
	if err := checkIdentityFlags(flags, usage); err != nil {
		return err
	}
	for _, named := range []struct{ flag, value string }{
		{"--client-secret-variable", flags.clientSecretVariable},
		{"--client-id-variable", flags.clientIDVariable},
	} {
		if named.value == "" || connectVariablePattern.MatchString(named.value) {
			continue
		}
		return problem.New(problem.CategoryUsage, "shell.invalid_argument",
			fmt.Sprintf("%s does not name an environment variable", named.flag)).
			WithRecovery(fmt.Sprintf("Pass the name of the variable holding the credential, not the "+
				"credential: a variable name is %s. The value is not repeated here, in case it is the "+
				"secret itself. %s", connectVariableRule, usage))
	}
	if flags.clientIDVariable != "" && flags.clientSecretVariable == "" {
		return problem.New(problem.CategoryUsage, "shell.missing_required_flag",
			"--client-id-variable names half a credential and needs --client-secret-variable").
			WithRecovery(usage)
	}
	if descriptor.LoginProvider() && flags.clientIDVariable != "" {
		return problem.New(problem.CategoryUsage, "shell.conflicting_arguments",
			fmt.Sprintf("the %s product is a login provider, so its credential is the account's own: "+
				"--client-id-variable belongs to a product reached through another provider", namespace)).
			WithRecovery("Pass --client-id <id> --client-secret-variable <VAR> to create a " +
				"client-credentials account for it. " + usage)
	}
	if descriptor.Exchanged() {
		// The exchange runs at the account's own issuer as the account's own
		// client, so a client named for the product is one it never presents.
		if flags.clientIDSet || flags.clientSecretVariable != "" || flags.clientIDVariable != "" {
			return problem.New(problem.CategoryUsage, "shell.conflicting_arguments",
				fmt.Sprintf("the %s product is reached by exchanging the account's own login session, so "+
					"--client-id, --client-id-variable and --client-secret-variable name a client it "+
					"never presents", namespace)).
				WithRecovery("Omit them. " + usage)
		}
		return nil
	}
	if flags.clientID == "" && flags.clientIDVariable == "" {
		return clientIDRequired(namespace, flags, usage)
	}
	if descriptor.LoginProvider() && flags.clientSecretVariable != "" &&
		!descriptor.AllowsMachine(modules.MachineInline) {
		return problem.New(problem.CategoryAuthPolicy, "auth.product_not_configured",
			fmt.Sprintf("the %s product does not accept a machine client at its own issuer, so it "+
				"cannot be a client-credentials account's login provider", namespace)).
			WithRecovery("Connect a login provider that accepts one first, then record this product " +
				"on that account.")
	}
	return nil
}

// checkIdentityFlags refuses an account name that cannot be one, and a
// login provider that is not an issuer URL. Both records connect writes
// find their identity by them.
func checkIdentityFlags(flags connectFlags, usage string) error {
	if flags.identity != "" && !contexts.ValidName(flags.identity) {
		return problem.New(problem.CategoryUsage, "shell.invalid_argument",
			fmt.Sprintf("%q cannot be used as an account name", flags.identity)).
			WithRecovery(fmt.Sprintf("An account name is %s. %s", contexts.NameRule, usage))
	}
	if flags.loginProvider != "" {
		return refuseNonIssuerURL(flags.loginProvider)
	}
	return nil
}

// connectPlan is what one connect will write: the identity it acts on, the
// product record, and whether the identity and its context are new.
type connectPlan struct {
	identity contexts.Account
	context  contexts.Context
	// created reports that the identity and context are new.
	created bool
	// selected reports that the new context became the default one.
	selected bool
	// sole reports that, once written, the identity's context is the only
	// one in the document and is selected, so the login needs no --context.
	sole bool
	// assigned reports that the shell named the new account, because neither
	// --account nor an answer at the prompt did.
	assigned bool
	// replaced reports that the product was recorded before.
	replaced  bool
	namespace string
	product   contexts.Product
}

// planConnect decides what connect writes, from the document as it is.
//
// providers are the installed namespaces whose product a login can run
// against, named by the refusal when there is no account to record on.
// assigned is the name the shell offered for an account connect creates when
// --account named none; when it is empty, or was taken since it was offered,
// the next free account-N is used instead, so a name nobody typed is never
// refused.
func planConnect(document contexts.Document, namespace string, descriptor modules.ProductDescriptor,
	productURL string, flags connectFlags, contextName string, providers []string,
	assigned string) (connectPlan, error) {
	issuer := descriptor.Issuer(productURL)
	plan := connectPlan{namespace: namespace}
	target, found, err := connectTarget(document, flags, contextName)
	if err != nil {
		return connectPlan{}, err
	}
	switch {
	case descriptor.LoginProvider() && found && target.Auth.Issuer == issuer:
		if (flags.clientSecretVariable != "") != (target.Auth.Kind == contexts.KindClientCredentials) {
			return connectPlan{}, identityDiffers(target.Name, "kind", target.Auth.Kind,
				connectKind(flags))
		}
		plan.identity = target
	case descriptor.LoginProvider():
		// The flag named an issuer no identity authenticates against, so
		// creating one here would answer a line that asked for another
		// identity by silently starting a session somewhere else.
		if flags.loginProvider != "" && !found {
			return connectPlan{}, loginProviderRequired(namespace, flags.loginProvider, providers)
		}
		name := flags.identity
		switch {
		case name == "":
			name = assigned
			if name == "" || accountNameTaken(document, name) {
				name = nextFreeAccountName(document)
			}
		case declaresIdentity(document, name):
			return connectPlan{}, problem.New(problem.CategoryUsage, "contexts.identity_exists",
				fmt.Sprintf("an account named %q already exists and authenticates against another issuer", name)).
				WithRecovery("Pass --account <name> to create this deployment's account under another " +
					"name. Connecting never replaces an account.")
		case declaresContext(document, name):
			return connectPlan{}, contextExists(name)
		}
		plan.identity = newConnectIdentity(name, descriptor, issuer, flags)
		plan.context = contexts.Context{Name: name, Account: name}
		plan.created = true
		plan.selected = document.DefaultContext == ""
	case !found:
		return connectPlan{}, loginProviderRequired(namespace, flags.loginProvider, providers)
	default:
		plan.identity = target
	}
	if plan.identity.Auth.Kind == contexts.KindClientCredentials && !descriptor.LoginProvider() {
		// The flags say which machine strategy is being asked for: the client
		// the identity already holds, or a credential the record carries. The
		// descriptor says which the product serves, and only it knows.
		asked := modules.MachineInline
		if flags.clientSecretVariable != "" {
			asked = modules.MachineCredential
		}
		if !descriptor.AllowsMachine(asked) {
			return connectPlan{}, machineNotAccepted(namespace, plan.identity.Name, descriptor, asked)
		}
	}
	if plan.identity.Auth.Kind != contexts.KindClientCredentials && !descriptor.LoginProvider() &&
		flags.clientSecretVariable != "" {
		return connectPlan{}, problem.New(problem.CategoryUsage, "shell.conflicting_arguments",
			fmt.Sprintf("--client-secret-variable records a product credential, which belongs to a "+
				"client-credentials account; %q logs in through the browser", plan.identity.Name)).
			WithRecovery("Omit the secret variable, or connect the login provider with --client-id and " +
				"--client-secret-variable first to create a client-credentials account.")
	}
	if _, recorded := plan.identity.Products[namespace]; recorded {
		if !flags.replace {
			return connectPlan{}, productExists(plan.identity.Name, namespace)
		}
		plan.replaced = true
	}
	product, err := connectProduct(namespace, descriptor, productURL, issuer, flags, plan.identity)
	if err != nil {
		return connectPlan{}, err
	}
	plan.product = product
	plan.sole = soleContext(document, plan)
	return plan, nil
}

// soleContext reports whether the document, once the plan is applied, holds
// exactly one context, it is the plan's identity's, and it is selected.
func soleContext(document contexts.Document, plan connectPlan) bool {
	if plan.created {
		return plan.selected && len(document.Contexts) == 0
	}
	return soleIdentityContext(document, plan.identity.Name)
}

// soleIdentityContext reports whether the document holds exactly one
// context, it is the named identity's, and it is selected.
func soleIdentityContext(document contexts.Document, identity string) bool {
	if len(document.Contexts) != 1 {
		return false
	}
	only := document.Contexts[0]
	return only.Account == identity && document.DefaultContext == only.Name
}

// connectTarget is the identity a connect records on, when one can be
// named: by --login-provider, by --identity, or by the selected context.
func connectTarget(document contexts.Document, flags connectFlags, contextName string) (
	contexts.Account, bool, error) {
	switch {
	case flags.loginProvider != "":
		for _, identity := range document.Accounts {
			if identity.Auth.Issuer == strings.TrimRight(flags.loginProvider, "/") {
				return identity, true, nil
			}
		}
		return contexts.Account{}, false, nil
	case flags.identity != "":
		for _, identity := range document.Accounts {
			if identity.Name == flags.identity {
				return identity, true, nil
			}
		}
		return contexts.Account{}, false, nil
	case len(document.Contexts) == 0:
		return contexts.Account{}, false, nil
	}
	selected, err := document.Select(contextName)
	if err != nil {
		return contexts.Account{}, false, err
	}
	return selected.Identity, true, nil
}

// newConnectIdentity is the identity a login provider's connect creates.
func newConnectIdentity(name string, descriptor modules.ProductDescriptor, issuer string,
	flags connectFlags) contexts.Account {
	auth := contexts.AccountAuth{
		Kind: contexts.KindOAuthBrowser, Issuer: issuer, ClientID: flags.clientID,
		Provider: descriptor.Provider, CredentialRef: name,
	}
	if flags.clientSecretVariable != "" {
		auth.Kind = contexts.KindClientCredentials
		auth.ClientSecretVariable = flags.clientSecretVariable
		auth.CredentialRef = ""
	}
	return contexts.Account{Name: name, Type: contexts.IdentityTypeForIssuer(issuer), Auth: auth}
}

// connectKind names the identity kind the flags ask for, for a refusal.
func connectKind(flags connectFlags) string {
	if flags.clientSecretVariable != "" {
		return contexts.KindClientCredentials
	}
	return contexts.KindOAuthBrowser
}

// connectProduct is the product record connect writes.
func connectProduct(namespace string, descriptor modules.ProductDescriptor, productURL, issuer string,
	flags connectFlags, identity contexts.Account) (contexts.Product, error) {
	product := contexts.Product{Endpoint: productURL, Scopes: descriptor.Scopes}
	if len(flags.scopes) > 0 {
		product.Scopes = flags.scopes
	}
	product.Audience = descriptor.AudienceFor(flags.clientID)
	if flags.audience != "" {
		product.Audience = flags.audience
	}
	if descriptor.LoginProvider() {
		if product.Audience == "" {
			return contexts.Product{}, problem.New(problem.CategoryUsage, "shell.missing_required_flag",
				fmt.Sprintf("the %s module's descriptor names no default audience, so connect needs "+
					"--audience with the resource server the deployment binds access to", namespace)).
				WithRecovery(connectUsage(namespace))
		}
		return product, nil
	}
	if descriptor.Grant == "" {
		return contexts.Product{}, problem.New(problem.CategoryAuthPolicy, "auth.product_not_configured",
			fmt.Sprintf("the %s product declares no grant, so it can only be reached from its own "+
				"provider, and the %q account logs in elsewhere", namespace, identity.Name)).
			WithRecovery("Select an account whose login provider serves this product, or record the " +
				"product with wso2 account add-product.")
	}
	if descriptor.Grant == contexts.GrantExchange {
		// The product accepts the login provider's tokens bound to it, so its
		// audience defaults to the URL it answers at: the identifier a
		// deployment registers its resource server under at the login
		// provider. The grant names no issuer or client, because the exchange
		// runs at the account's own.
		if product.Audience == "" {
			product.Audience = productURL
		}
		product.Grant = &contexts.Grant{Kind: contexts.GrantExchange}
		return product, nil
	}
	product.Grant = &contexts.Grant{Kind: descriptor.Grant, Issuer: issuer, ClientID: flags.clientID}
	if flags.clientSecretVariable != "" {
		product.ClientIDVariable = flags.clientIDVariable
		product.ClientSecretVariable = flags.clientSecretVariable
	}
	return product, nil
}

// machineNotAccepted refuses a product a client-credentials account cannot
// reach the way the flags ask. The descriptor's machine list is what the
// deployment declared, and each of the three answers it can give has its own
// way out: another identity, fewer flags, or the product's own credential.
func machineNotAccepted(namespace, identity string, descriptor modules.ProductDescriptor,
	asked string) problem.Problem {
	switch {
	case len(descriptor.Machine) == 0:
		return problem.New(problem.CategoryAuthPolicy, "auth.product_not_configured",
			fmt.Sprintf("the %s product declares no way for a client-credentials account to reach it, "+
				"and %q holds a machine client", namespace, identity)).
			WithRecovery("Record the product on an account that signs in through the browser, or " +
				"install a version of the module whose descriptor says how a machine client reaches it.")
	case asked == modules.MachineCredential:
		return problem.New(problem.CategoryAuthPolicy, "auth.product_not_configured",
			fmt.Sprintf("the %s product accepts the machine client %q already holds, so it carries no "+
				"credential of its own", namespace, identity)).
			WithRecovery("Omit --client-secret-variable and --client-id-variable: the account's own " +
				"client is what reaches this product.")
	}
	return problem.New(problem.CategoryAuthPolicy, "auth.product_not_configured",
		fmt.Sprintf("the %s product does not accept the machine client the %q account holds, "+
			"so it needs a credential of its own", namespace, identity)).
		WithRecovery(fmt.Sprintf("Register a client for this CLI on the product (wso2 %s bootstrap "+
			"does) and pass --client-id <id> --client-secret-variable <VAR> naming its credential, "+
			"adding --client-id-variable <VAR> when the client id is held in the environment too; the "+
			"values stay there.", namespace))
}

// clientIDRequired refuses a product whose descriptor names no client when
// the line names none either. Which client is wanted depends on the identity
// the product lands on, and the document is not open yet, so the flags
// decide: a secret variable on the line means a pipeline, which uses the
// confidential client the product's bootstrap registers; none means a
// browser identity, which needs the product's public client federated to the
// login provider, a different registration that the bootstrap does not make.
func clientIDRequired(namespace string, flags connectFlags, usage string) problem.Problem {
	if flags.clientSecretVariable != "" {
		return problem.New(problem.CategoryUsage, "shell.missing_required_flag",
			fmt.Sprintf("the %s module's descriptor names no default client, so connect needs "+
				"--client-id with the client the deployment registered for this CLI", namespace)).
			WithRecovery(fmt.Sprintf("Run wso2 %s bootstrap to register one, then this command with "+
				"the --client-id it prints. %s", namespace, usage))
	}
	return problem.New(problem.CategoryUsage, "shell.missing_required_flag",
		fmt.Sprintf("the %s module's descriptor names no default client, so connect needs "+
			"--client-id with the public client the deployment holds for this CLI", namespace)).
		WithRecovery(fmt.Sprintf("On a browser account that is a public client registered on the "+
			"product itself and federated to the account's login provider, not the confidential "+
			"client wso2 %s bootstrap registers. A pipeline passes the bootstrap's client instead, "+
			"as --client-id <id> --client-secret-variable <VAR>. %s", namespace, usage))
}

// loginProviderRequired refuses a product with no identity to attach to.
//
// The way out names the connect of every installed module whose product is a
// login provider, since that is what creates an account from a URL; with none
// installed it names the command that declares one by hand.
func loginProviderRequired(namespace, loginProvider string, providers []string) problem.Problem {
	message := fmt.Sprintf("the %s product is reached through a login provider, and no account exists "+
		"to record it on", namespace)
	if loginProvider != "" {
		message = fmt.Sprintf("no account authenticates against the issuer --login-provider names, "+
			"so the %s product has nowhere to be recorded", namespace)
	}
	create := "wso2 account create <name> --issuer <issuer-url> --client-id <id>"
	if len(providers) > 0 {
		connects := make([]string, 0, len(providers))
		for _, provider := range providers {
			connects = append(connects, fmt.Sprintf("wso2 %s connect <login-provider-url>", provider))
		}
		create = strings.Join(connects, " or ") + " [--account <name>]"
	}
	return problem.New(problem.CategoryUsage, "shell.login_provider_required", message).
		WithRecovery(fmt.Sprintf("Create the account first with %s, then run this command again; or "+
			"pass --login-provider <issuer-url> naming an account that exists. wso2 account list shows "+
			"them.", create))
}

// loginProviderNamespaces are the installed namespaces whose product a login
// can run against, in namespace order. It is best effort, because it only
// words a refusal: a store that cannot be read names none.
func (s Shell) loginProviderNamespaces() []string {
	store, err := s.store()
	if err != nil {
		return nil
	}
	installed, _, err := store.Inventory()
	if err != nil {
		return nil
	}
	var providers []string
	for _, entry := range installed {
		if product := entry.Receipt.Capabilities.Product; product != nil && product.LoginProvider() {
			providers = append(providers, entry.Namespace)
		}
	}
	return providers
}

// apply writes the plan into the document.
func (p connectPlan) apply(document contexts.Document) contexts.Document {
	identity := p.identity
	// Read before the insert: the login product is the one the namespace
	// order chose out of the products the identity already had. Reading it
	// afterwards would let a namespace that sorts earlier become the answer,
	// which is the move the pin exists to prevent.
	login := identity.LoginAccess().Namespace
	products := maps.Clone(identity.Products)
	if products == nil {
		products = map[string]contexts.Product{}
	}
	products[p.namespace] = p.product
	identity.Products = products
	// The login product is pinned the first time an identity has one to
	// pin: to the product the namespace order already chose, so pinning
	// never moves a login, and to this product when it is the first.
	if identity.LoginProduct == "" && identity.Auth.Kind != contexts.KindClientCredentials {
		if login == "" {
			login = identity.LoginAccess().Namespace
		}
		identity.LoginProduct = login
	}
	if p.created {
		document.SchemaVersion = contexts.SchemaVersion
		document.Accounts = append(document.Accounts, identity)
		document.Contexts = append(document.Contexts, p.context)
		if p.selected {
			document.DefaultContext = p.context.Name
		}
		return document
	}
	position := slices.IndexFunc(document.Accounts, func(candidate contexts.Account) bool {
		return candidate.Name == identity.Name
	})
	document.Accounts[position] = identity
	return document
}

// reportConnect states what was recorded and what to run next.
func (s Shell) reportConnect(mode output.Mode, root, namespace string, plan connectPlan) error {
	identity := plan.identity
	identity.Products = map[string]contexts.Product{namespace: plan.product}
	access, _ := identity.Access(namespace)
	verb := "Recorded"
	if plan.replaced {
		verb = "Replaced"
	}
	next := fmt.Sprintf("Run wso2 login --context %s.", identity.Name)
	if plan.sole {
		next = "Run wso2 login."
	}
	if identity.Auth.Kind == contexts.KindClientCredentials {
		next = fmt.Sprintf("Run wso2 %s status --context %s.", namespace, identity.Name)
	}
	// The account as it stood, not the one-product copy above: which session
	// is the login one is decided by the products it already recorded.
	if held, ok := exchangedNext(root, namespace, plan.identity, access); ok {
		next = held
	}
	reported := result.New(connectSchema).
		With("product", "Product", namespace).
		With("record", "Record", recordManagement).
		With("account", "Account", identity.Name).
		With("created", "Account created", fmt.Sprintf("%t", plan.created)).
		With("endpoint", "Endpoint", plan.product.Endpoint).
		With("issuer", "Issuer", access.Issuer).
		With("clientId", "Client ID", access.ClientID).
		With("audience", "Audience", plan.product.Audience).
		With("scopes", "Scopes", strings.Join(plan.product.Scopes, ",")).
		With("strategy", "Strategy", access.Strategy).
		With("replaced", "Replaced", fmt.Sprintf("%t", plan.replaced)).
		With(output.NextField, "Next", next)
	if mode == output.ModeJSON {
		return output.Report(s.Streams.Out, mode, reported)
	}
	if _, err := fmt.Fprintf(s.Streams.Out, "\n%s the %q product on the %q account.\n",
		verb, namespace, identity.Name); err != nil {
		return err
	}
	if plan.assigned {
		if _, err := fmt.Fprintln(s.Streams.Out, assignedNameNote("--account", identity.Name)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(s.Streams.Out); err != nil {
		return err
	}
	return output.Report(s.Streams.Out, mode, reported)
}
