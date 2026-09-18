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
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/install"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// This file turns what a user states about a product — on a command line or
// in an input file — into the complete record contexts.json holds.
//
// Defaults come from the installed product's descriptor and are frozen into
// the record when it is written (ADR 0016). Nothing at command time consults a
// descriptor to fill a gap, so a later wso2 product update changes no
// authentication behaviour until a context is applied again.

// productSpec is one product as a user states it. Every member but URL is
// optional; an empty one takes the descriptor's default.
type productSpec struct {
	// URL is the product's base URL.
	URL string
	// Audience overrides the audience the descriptor derives.
	Audience string
	// Scopes overrides the descriptor's scopes when non-nil.
	Scopes []string
	// Grant is a grant stated in full, which a record written without a
	// descriptor needs. It wins over the descriptor's.
	Grant *contexts.Grant
	// ClientID is the product's own public client, for a product reached by a
	// jwt-bearer or federated grant, when not the descriptor's.
	ClientID string
	// ClientIDVariable and ClientSecretVariable name a product credential of
	// its own, for a client-credentials context.
	ClientIDVariable, ClientSecretVariable string
	// Gateway is the product's gateway, when it has one.
	Gateway *gatewaySpec
	// Version pins the product version an install selects. It is an input
	// file's member only, and never reaches the record.
	Version string
}

// gatewaySpec is a product's gateway as a user states it.
type gatewaySpec struct {
	URL      string
	Audience string
	Scopes   []string
}

// descriptorLookup is how the resolver finds a product's descriptor: the
// installed one, or nil when the product is not installed or declares none.
type descriptorLookup func(namespace string) *modules.ProductDescriptor

// installedDescriptors reads the descriptor of every installed product. It is
// best effort: a store that cannot be read, or a product that cannot be
// resolved, answers nil, and the resolver then asks for a complete record.
func (s Shell) installedDescriptors() descriptorLookup {
	cache := map[string]*modules.ProductDescriptor{}
	return func(namespace string) *modules.ProductDescriptor {
		if descriptor, seen := cache[namespace]; seen {
			return descriptor
		}
		installed, _ := s.installedProduct(namespace)
		var descriptor *modules.ProductDescriptor
		if installed != nil {
			descriptor = installed.Receipt.Capabilities.Product
		}
		cache[namespace] = descriptor
		return descriptor
	}
}

// installedProduct is the installed module for a namespace, or nil when none
// is installed.
func (s Shell) installedProduct(namespace string) (*modules.Resolved, error) {
	store, err := s.store()
	if err != nil {
		return nil, err
	}
	namespaces, err := store.Namespaces()
	if err != nil {
		return nil, err
	}
	if !slices.Contains(namespaces, namespace) {
		return nil, nil
	}
	identity, err := s.identity()
	if err != nil {
		return nil, err
	}
	resolved, err := store.Resolve(namespace, identity)
	if err != nil {
		return nil, err
	}
	return &resolved, nil
}

// installNeed is one product a setup command needs installed.
type installNeed struct {
	Namespace string
	// Version is the pinned version, or empty for the channel's newest.
	Version string
	// Installed is the version installed now, or empty when none is.
	Installed string
}

// String names the install the way wso2 product install spells it.
func (n installNeed) String() string {
	if n.Version == "" {
		return n.Namespace
	}
	return n.Namespace + "@" + n.Version
}

// installPlan is what a setup command would install and what it leaves.
type installPlan struct {
	// Missing are products that are not installed.
	Missing []installNeed
	// Mismatched are products installed at a version other than the one
	// pinned: reported, and installed only under --update-products.
	Mismatched []installNeed
}

// planInstalls works out which of the wanted products are missing, and which
// are installed at a version other than their pin.
func (s Shell) planInstalls(wanted []installNeed) (installPlan, error) {
	var plan installPlan
	seen := map[string]bool{}
	for _, need := range wanted {
		if seen[need.Namespace] {
			continue
		}
		seen[need.Namespace] = true
		installed, err := s.installedProduct(need.Namespace)
		if err != nil {
			return installPlan{}, err
		}
		if installed == nil {
			plan.Missing = append(plan.Missing, need)
			continue
		}
		need.Installed = installed.Receipt.ModuleVersion
		if need.Version != "" && need.Version != need.Installed {
			plan.Mismatched = append(plan.Mismatched, need)
		}
	}
	return plan, nil
}

// install installs each product in turn, reporting each on the diagnostic
// stream so a machine-readable result on standard output stays whole. A
// failure stops the run and names what was installed before it: an installed
// product changes no configuration and installing is idempotent, so running
// the command again continues from there.
func (s Shell) install(needs []installNeed) error {
	if len(needs) == 0 {
		return nil
	}
	installer, err := s.installer()
	if err != nil {
		return err
	}
	var done []string
	for _, need := range needs {
		if _, err := fmt.Fprintf(s.Streams.Err, "Installing %s...\n", need); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), catalogTimeout)
		installed, err := installer.Run(ctx, installRequest(need))
		cancel()
		if err != nil {
			if len(done) > 0 {
				if _, werr := fmt.Fprintf(s.Streams.Err, "Installed before the failure: %s. "+
					"The context document was not changed.\n", strings.Join(done, ", ")); werr != nil {
					return werr
				}
			}
			return err
		}
		done = append(done, fmt.Sprintf("%s v%s", installed.Namespace, installed.Version))
		if _, err := fmt.Fprintf(s.Streams.Err, "Installed %s v%s.\n", installed.Namespace,
			installed.Version); err != nil {
			return err
		}
	}
	return nil
}

// resolveLoginProduct builds the login block and the login product's record
// for a context that logs in through an installed login provider.
func resolveLoginProduct(namespace string, descriptor *modules.ProductDescriptor, spec productSpec,
	login contexts.Login) (contexts.Login, contexts.Product, error) {
	if descriptor == nil || !descriptor.LoginProvider() {
		return contexts.Login{}, contexts.Product{}, problem.New(problem.CategoryUsage,
			"shell.invalid_argument",
			fmt.Sprintf("the %s product is not a login provider, so a context cannot log in through it", namespace)).
			WithRecovery("Name the product the deployment signs in with, as in --login-product identity, " +
				"or log in through an issuer with --issuer <url> --client-id <id>.")
	}
	login.Product = namespace
	if login.Issuer == "" {
		login.Issuer = descriptor.Issuer(spec.URL)
	}
	if login.ClientID == "" {
		login.ClientID = descriptor.ClientID
	}
	if login.ClientID == "" {
		return contexts.Login{}, contexts.Product{}, problem.New(problem.CategoryUsage,
			"shell.missing_required_flag",
			fmt.Sprintf("the %s product names no default client, so the context needs the client the "+
				"deployment registered for this CLI", namespace)).
			WithRecovery("Pass --client-id <id>, or set login.clientId in the input file.")
	}
	if login.Provider == "" && login.Narrowing == "" {
		login.Provider = descriptor.Provider
	}
	if login.Kind == "" {
		login.Kind = contexts.KindOAuthBrowser
	}
	if login.Kind == contexts.KindClientCredentials && !descriptor.AllowsMachine(modules.MachineInline) {
		return contexts.Login{}, contexts.Product{}, problem.New(problem.CategoryAuthPolicy,
			"auth.product_not_configured",
			fmt.Sprintf("the %s product does not accept a machine client at its own issuer, so a "+
				"client-credentials context cannot log in through it", namespace)).
			WithRecovery("Log in through a provider that accepts one, or drop --client-secret-variable.")
	}
	product := contexts.Product{Endpoint: spec.URL, Audience: spec.Audience, Scopes: descriptor.Scopes}
	if spec.Scopes != nil {
		product.Scopes = spec.Scopes
	}
	if product.Audience == "" {
		product.Audience = descriptor.AudienceFor(login.ClientID)
	}
	if product.Audience == "" {
		return contexts.Login{}, contexts.Product{}, problem.New(problem.CategoryUsage,
			"shell.missing_required_flag",
			fmt.Sprintf("the %s product names no default audience, so the context needs the resource "+
				"server the deployment binds access to", namespace)).
			WithRecovery("Pass --audience <uri>, or set the product's audience in the input file.")
	}
	return login, product, nil
}

// resolveProduct builds the complete record for a product reached from a
// context that already has its login.
//
// With no descriptor the record is taken as stated: it has to be complete,
// because there is nothing to fill it from, and the document's own validation
// says what is missing.
func resolveProduct(namespace string, descriptor *modules.ProductDescriptor, spec productSpec,
	target contexts.Context) (contexts.Product, error) {
	product := contexts.Product{
		Endpoint: spec.URL, Audience: spec.Audience, Scopes: spec.Scopes, Grant: spec.Grant,
		ClientIDVariable: spec.ClientIDVariable, ClientSecretVariable: spec.ClientSecretVariable,
	}
	if descriptor == nil {
		if spec.Gateway != nil {
			product.Gateway = &contexts.Gateway{Endpoint: spec.Gateway.URL, Audience: spec.Gateway.Audience,
				Scopes: spec.Gateway.Scopes}
		}
		return product, nil
	}
	if spec.Scopes == nil {
		product.Scopes = descriptor.Scopes
	}
	machine := target.Login.Kind == contexts.KindClientCredentials
	if descriptor.LoginProvider() {
		// A login provider serves a login at its own issuer. Recorded on a
		// context that logs in elsewhere, it would be reached as if that
		// issuer were its own, which it is not.
		if issuer := descriptor.Issuer(spec.URL); issuer != target.Login.Issuer {
			return contexts.Product{}, loginProviderElsewhere(namespace, *descriptor, spec, target)
		}
		if product.Audience == "" {
			product.Audience = descriptor.AudienceFor(target.Login.ClientID)
		}
		return product, resolveGateway(namespace, descriptor, spec, target, &product)
	}
	if machine {
		asked := modules.MachineInline
		if spec.ClientSecretVariable != "" {
			asked = modules.MachineCredential
		}
		if !descriptor.AllowsMachine(asked) {
			return contexts.Product{}, machineNotAccepted(namespace, target.Name, *descriptor, asked)
		}
	} else if spec.ClientSecretVariable != "" || spec.ClientIDVariable != "" {
		return contexts.Product{}, problem.New(problem.CategoryUsage, "shell.conflicting_arguments",
			fmt.Sprintf("--client-secret-variable records a product credential, which belongs to a "+
				"client-credentials context; %q logs in through the browser", target.Name)).
			WithRecovery("Omit the credential variables for this context.")
	}
	switch {
	case product.Grant != nil:
		// Stated in full; taken as it is.
	case descriptor.Grant == "":
		return contexts.Product{}, problem.New(problem.CategoryAuthPolicy, "auth.product_not_configured",
			fmt.Sprintf("the %s product declares no grant, so it can only be reached from its own "+
				"provider, and the %q context logs in elsewhere", namespace, target.Name)).
			WithRecovery("Add it to a context that logs in through the provider that serves it.")
	case descriptor.Grant == contexts.GrantExchange:
		// The product accepts the login provider's tokens bound to it, so
		// its audience defaults to the URL it answers at: the identifier a
		// deployment registers its resource server under at the login
		// provider. The grant names no issuer or client, because the
		// exchange runs at the context's own.
		product.Grant = &contexts.Grant{Kind: contexts.GrantExchange}
	default:
		clientID := spec.ClientID
		if clientID == "" {
			clientID = descriptor.ClientID
		}
		if clientID == "" && spec.ClientIDVariable == "" {
			return contexts.Product{}, problem.New(problem.CategoryUsage, "shell.missing_required_flag",
				fmt.Sprintf("the %s product names no default client, so it needs the client the "+
					"deployment registered for this CLI", namespace)).
				WithRecovery("Pass --client-id <id>, or set the product's grant in the input file.")
		}
		product.Grant = &contexts.Grant{Kind: descriptor.Grant, Issuer: descriptor.Issuer(spec.URL),
			ClientID: clientID}
	}
	if product.Audience == "" {
		product.Audience = descriptor.AudienceFor(spec.ClientID)
	}
	if product.Audience == "" && product.Grant != nil && product.Grant.Kind == contexts.GrantExchange {
		product.Audience = spec.URL
	}
	return product, resolveGateway(namespace, descriptor, spec, target, &product)
}

// resolveGateway fills in a product's gateway record from its spec, when the
// spec names one.
func resolveGateway(namespace string, descriptor *modules.ProductDescriptor, spec productSpec,
	target contexts.Context, product *contexts.Product) error {
	if spec.Gateway == nil {
		return nil
	}
	if descriptor.Gateway == nil {
		return problem.New(problem.CategoryUsage, "shell.invalid_argument",
			fmt.Sprintf("the %s product declares no gateway, so it cannot record one", namespace)).
			WithRecovery("Drop the gateway, or install a version of the product that declares one.")
	}
	if target.Login.Kind == contexts.KindClientCredentials &&
		!descriptor.Gateway.AllowsMachine(modules.MachineInline) {
		return problem.New(problem.CategoryAuthPolicy, "auth.product_not_configured",
			fmt.Sprintf("the %s product's gateway does not accept the machine client the %q context "+
				"holds", namespace, target.Name)).
			WithRecovery("Record the gateway on a context that signs in through the browser.")
	}
	gateway := contexts.Gateway{Endpoint: spec.Gateway.URL, Audience: spec.Gateway.Audience,
		Scopes: spec.Gateway.Scopes}
	if gateway.Scopes == nil {
		gateway.Scopes = descriptor.Gateway.Scopes
	}
	// A deployment that binds access by resource needs the API's own
	// identifier whatever kind the descriptor names; only a scope-bound one
	// can fall back to the context's client.
	resourceBound := target.Account().Auth.Derivation() == contexts.DerivationTokenResource
	if gateway.Audience == "" && descriptor.Gateway.Audience == modules.AudienceClient && !resourceBound {
		gateway.Audience = target.Login.ClientID
	}
	// A gateway of a product reached by exchange is reached by exchange too,
	// so it takes the same default the product does: the URL it answers at.
	if gateway.Audience == "" && product.Grant != nil && product.Grant.Kind == contexts.GrantExchange {
		gateway.Audience = gateway.Endpoint
	}
	if gateway.Audience == "" {
		return problem.New(problem.CategoryUsage, "shell.missing_required_flag",
			fmt.Sprintf("the %s product's gateway is bound to a resource server, so it needs the API's "+
				"own resource identifier", namespace)).
			WithRecovery("Pass --gateway-audience <uri>, or set the gateway's audience in the input file.")
	}
	product.Gateway = &gateway
	return nil
}

// productURL refuses a product URL that is not one, and trims a trailing
// slash. The value is never echoed: like an issuer, it is where a credential
// is likeliest to have been pasted.
func productURL(flag, raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", problem.New(problem.CategoryUsage, "shell.invalid_argument",
			fmt.Sprintf("the %s value is not an absolute http or https URL", flag)).
			WithRecovery(fmt.Sprintf("Pass the URL as the deployment publishes it, as in %s "+
				"https://host:port. A missing https:// is the usual cause. The value is not repeated "+
				"here, in case it holds a secret.", flag))
	}
	if parsed.User != nil {
		return "", problem.New(problem.CategoryUsage, "shell.invalid_argument",
			fmt.Sprintf("the %s value carries a user name or password, which a URL here may not", flag)).
			WithRecovery("Pass the URL on its own. The shell authenticates through wso2 login, so a " +
				"credential in the URL is never used. The value is not repeated here.")
	}
	return strings.TrimRight(raw, "/"), nil
}

// providerLabels is how each identity provider is named to a person, as its
// own product names itself. A provider with no label here is shown by its
// recorded name, which is the next best thing and never wrong.
var providerLabels = map[string]string{
	contexts.ProviderThunder:        "Thunder",
	contexts.ProviderAsgardeo:       "Asgardeo",
	contexts.ProviderIdentityServer: "WSO2 Identity Server",
}

// providerLabel names a provider to a person.
func providerLabel(provider string) string {
	if label := providerLabels[provider]; label != "" {
		return label
	}
	if provider == "" {
		return "another provider"
	}
	return provider
}

// providerServed reports whether any login-provider module this shell knows
// of signs in at the named provider. Only Thunder does today, so a context
// that signs in at Asgardeo or an Identity Server logs in and then finds no
// product of that provider to reach. A product reached by its own grant is
// a separate question, and this does not answer it. Extend this when a
// module ships that fronts another provider, and the refusal below stops
// naming it.
func providerServed(provider string) bool {
	return provider == contexts.ProviderThunder
}

// loginProviderElsewhere refuses a login provider recorded on a context that
// signs in somewhere else. Naming both providers is what separates the two
// ways to arrive here: a URL typed wrong, which the recovery's create line
// fixes, and a context whose provider has no module at all, where there is no
// URL to correct and the search should stop.
func loginProviderElsewhere(namespace string, descriptor modules.ProductDescriptor, spec productSpec,
	target contexts.Context) problem.Problem {
	recovery := fmt.Sprintf("Create a context that signs in through %s: wso2 context create <name> "+
		"--login-product %s --url %s.", providerLabel(descriptor.Provider), namespace, spec.URL)
	if !providerServed(target.Login.Provider) {
		recovery = fmt.Sprintf("No module serves %s yet. ", providerLabel(target.Login.Provider)) + recovery
	}
	return problem.New(problem.CategoryUsage, "shell.conflicting_arguments",
		fmt.Sprintf("the %s product signs in at its own %s issuer, and the %q context signs in at %s",
			namespace, providerLabel(descriptor.Provider), target.Name,
			providerLabel(target.Login.Provider))).
		WithRecovery(recovery)
}

// machineNotAccepted refuses a product a client-credentials context cannot
// reach the way it asks. The descriptor's machine list is what the deployment
// declared, and each of the three answers it can give has its own way out.
func machineNotAccepted(namespace, name string, descriptor modules.ProductDescriptor,
	asked string) problem.Problem {
	switch {
	case len(descriptor.Machine) == 0:
		return problem.New(problem.CategoryAuthPolicy, "auth.product_not_configured",
			fmt.Sprintf("the %s product declares no way for a client-credentials context to reach it, "+
				"and %q holds a machine client", namespace, name)).
			WithRecovery("Add the product to a context that signs in through the browser, or install a " +
				"version of the product whose descriptor says how a machine client reaches it.")
	case asked == modules.MachineCredential:
		return problem.New(problem.CategoryAuthPolicy, "auth.product_not_configured",
			fmt.Sprintf("the %s product accepts the machine client %q already holds, so it carries no "+
				"credential of its own", namespace, name)).
			WithRecovery("Omit --client-secret-variable and --client-id-variable: the context's own " +
				"client is what reaches this product.")
	}
	return problem.New(problem.CategoryAuthPolicy, "auth.product_not_configured",
		fmt.Sprintf("the %s product does not accept the machine client the %q context holds, "+
			"so it needs a credential of its own", namespace, name)).
		WithRecovery(fmt.Sprintf("Register a client for this CLI on the product (wso2 %s bootstrap "+
			"does) and pass --client-id <id> --client-secret-variable <VAR> naming its credential; "+
			"the values stay in the environment.", namespace))
}

// installRequest is the install a need asks for.
func installRequest(need installNeed) install.Request {
	return install.Request{Namespace: need.Namespace, Policy: catalog.Policy{Version: need.Version}}
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
