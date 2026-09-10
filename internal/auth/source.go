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

package auth

import (
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/modules"
)

// source mints access material after broker policy has admitted a request.
//
// The seam exists so that "what kind of identity is this?" is answered once per
// invocation, in one switch, rather than at every point the broker needs to
// know. What a source receives has already been admitted: the module receipt
// allows the request, the identity registers the product it names, and the
// context stays inside the identity's home tenant. A source decides only how
// access is obtained — and refuses when it cannot obtain exactly what was
// asked for, because a source that returned more than the request would make
// every check above it advisory.
type source interface {
	mint(request Request, now time.Time) (Grant, error)
}

// resolveSource applies the policy an identity kind carries and returns the
// source that answers for it.
//
// Every legal kind is named here. A kind this release does not implement is
// refused as unimplemented rather than falling through to something that
// happens to work, so a context document stays readable ahead of the release
// that serves it.
func (b *Broker) resolveSource(request Request) (source, error) {
	switch kind := b.Selection.Identity.Auth.Kind; kind {
	case "":
		return nil, denial("auth.context_not_selected",
			fmt.Sprintf("the %q module needs access, and no WSO2 CLI context is selected", b.namespace()),
			"Run wso2 context use <name> to select a configured context, or wso2 login "+
				"--url <issuer> --client-id <id> to create an account and a context. "+
				"wso2 context list shows what is configured.")
	case contexts.MethodDevelopmentCredential:
		return b.developmentSource()
	case contexts.KindOAuthBrowser, contexts.KindOAuthDevice, contexts.KindClientCredentials:
		if err := b.checkProduct(request); err != nil {
			return nil, err
		}
		if err := b.checkHomeTenant(); err != nil {
			return nil, err
		}
		product := b.Selection.Identity.Products[b.Namespace]
		if kind == contexts.KindClientCredentials {
			return b.inlineSource(request)
		}
		// Both interactive kinds land here, and deliberately on the same
		// source. How a session was established is a fact about a login that
		// already happened; what is left behind is a refresh token, and every
		// step from here — the rotation lock, the scoped refresh, the proof
		// that the narrowing held — reads that and nothing else.
		access, _ := b.Selection.Identity.Access(b.recordKey(request))
		source := sessionSource{
			namespace: access.Namespace,
			product:   b.namespace(),
			ref:       access.SessionRef,
			issuer:    access.Issuer,
			clientID:  access.ClientID,
			audience:  access.Audience,
			scopes:    access.Scopes,
			sessions:  session.Store{StateRoot: b.StateRoot},
			client:    b.httpClient(),
			strategy:  access.Strategy,
			// A direct or sibling session on a resource-bound derivation was
			// minted for the product's resource server; a derived one is an
			// assertion session, refused by the assertion source in its own
			// terms.
			contextName:   b.Selection.Context.Name,
			resourceBound: access.Resource != "" && access.Strategy != contexts.StrategyDerived,
		}
		if access.Strategy != contexts.StrategyDirect {
			// A product beside the login one, or one derived from it, may
			// have no session yet: this is the first invocation reaching it.
			// The login session itself is never established here.
			source.establish = b.establishFor(access)
		}
		if access.Strategy == contexts.StrategyExchanged {
			// An exchanged product has no session and no authorization of its
			// own, so what is renewed and locked here is the login session.
			// The source is rebuilt around that session deliberately rather
			// than reusing the one above: the access carries the product's
			// audience and no session reference at all, and a source pointed
			// at the product would have nothing to load.
			login := b.Selection.Identity.LoginAccess()
			return exchangeSource{
				login: sessionSource{
					namespace: login.Namespace,
					product:   login.Namespace,
					ref:       login.SessionRef,
					issuer:    login.Issuer,
					clientID:  login.ClientID,
					audience:  login.Audience,
					scopes:    login.Scopes,
					sessions:  session.Store{StateRoot: b.StateRoot},
					client:    b.httpClient(),
					strategy:  login.Strategy,

					contextName:   b.Selection.Context.Name,
					resourceBound: login.Resource != "",
				},
				namespace: access.Namespace,
				product:   b.namespace(),
				audience:  access.Audience,
			}, nil
		}
		if access.Strategy == contexts.StrategyDerived {
			// The product's own issuer answers, from an assertion the session
			// yields. Everything the session source knows about renewing and
			// rotating still applies; what changes is what the renewal is for.
			return assertionSource{session: source, grant: *product.Grant}, nil
		}
		return source, nil
	case contexts.KindPAT:
		return nil, denial("auth.kind_not_implemented",
			fmt.Sprintf("the %q context uses an authentication kind this release does not implement",
				b.Selection.Context.Name),
			"Select a context whose account logs in through the browser or through a device code, "+
				"or one that uses client credentials. Personal access token login is planned.")
	default:
		return nil, denial("auth.method_unsupported",
			fmt.Sprintf("the %q context uses an authentication method this shell does not implement",
				b.Selection.Context.Name),
			"Select a context with a supported authentication kind.")
	}
}

// establishFor is what a source calls when the product's own session is
// absent: the shell's login hook, or a refusal naming the login to run.
func (b *Broker) establishFor(access contexts.ProductAccess) func() error {
	return func() error {
		if b.EstablishSession == nil {
			return SessionRequired(access.Namespace, b.namespace())
		}
		return b.EstablishSession(access)
	}
}

// checkProduct proves the identity registers the namespace asking, for what the
// module actually asked for.
//
// The registration is the deployment's own statement of what this identity may
// reach, so a request it does not cover is refused rather than attempted: an
// issuer would answer with its own error, and a user reading it would have no
// way to tell a misregistered product from a broken one. Scope names are not
// secrets, so a refusal states both sides of the mismatch.
//
// It deliberately does not compare the module's requested audience against the
// registered one. The two name the same protected resource in different
// vocabularies: a module carries the logical name its API is known by, compiled
// in and identical for every deployment, while the registration carries the
// concrete string this deployment stamps into aud — the client ID on Asgardeo,
// the API resource identifier on Identity Server, an absolute URI on Thunder.
// Requiring them to be equal would make a module installable only where its
// compiled-in constant happened to match a deployment value, so the binding is
// proved where it is real instead: against the token the deployment issued, in
// tokenResponse.verify.
func (b *Broker) checkProduct(request Request) error {
	product, configured := b.Selection.Identity.Products[b.Namespace]
	if !configured {
		return denial("auth.product_not_configured",
			fmt.Sprintf("the account the %q context authenticates as does not configure the %q product",
				b.Selection.Context.Name, b.namespace()),
			fmt.Sprintf("Run wso2 account add-product %s %s --endpoint <url> --audience "+
				"<resource-id> --scopes <list> to register it, or select a context whose "+
				"account reaches it.", b.Selection.Identity.Name, b.namespace()))
	}
	if request.Record == contexts.GatewayRecord {
		return b.checkGateway(request, product)
	}
	if product.Audience == "" {
		return denial("auth.product_not_configured",
			fmt.Sprintf("the account the %q context authenticates as registers no audience for its "+
				"%q product, so the shell cannot prove what a token it issues is bound to",
				b.Selection.Context.Name, b.namespace()),
			"Set the audience this deployment binds access to on this account's product entry. It "+
				"is the client ID on Asgardeo, the API resource identifier on Identity Server, and "+
				"the resource server's URI on Thunder.")
	}
	if b.Selection.Identity.Auth.Derivation() == contexts.DerivationTokenResource &&
		product.Grant != nil && product.Grant.Kind == contexts.GrantJWTBearer && product.Grant.Resource == "" {
		// The identity's assertion session runs at the account's own issuer,
		// which this derivation binds by resource exactly as the login
		// session is. A document written before this field was required
		// still decodes — see contexts.Identity.validateDerivation — so the
		// refusal belongs here, at the one place that actually needs the
		// resource, rather than at document load.
		return denial("auth.product_not_configured",
			fmt.Sprintf("the %q product's jwt-bearer grant names no resource for its assertion "+
				"session, which this deployment binds access by", b.namespace()),
			fmt.Sprintf("Record the resource with wso2 account add-product --replace "+
				"--grant-resource <uri>, then run wso2 login --only %s.", b.namespace()))
	}
	if len(product.Scopes) > 0 {
		for _, scope := range request.Scopes {
			if !slices.Contains(product.Scopes, scope) {
				return denial("auth.product_not_configured",
					fmt.Sprintf("the %q module asked for the %q permission, which this account's %q "+
						"product does not carry", b.namespace(), scope, b.namespace()),
					"Add the permission to this account's product entry once the deployment grants "+
						"it, then retry the command.")
			}
		}
	}
	return nil
}

// checkGateway proves the identity records a gateway for the product asking,
// for what the module asked for. The gateway record is the identity's
// statement of which API the product's gateway serves for it, so a request
// it does not cover is refused rather than sent to the gateway to refuse.
func (b *Broker) checkGateway(request Request, product contexts.Product) error {
	if product.Gateway == nil {
		return denial("auth.product_not_configured",
			fmt.Sprintf("the account the %q context authenticates as records no gateway for its %q product",
				b.Selection.Context.Name, b.namespace()),
			fmt.Sprintf("Run wso2 %s connect <gateway-url> --gateway --audience <api resource identifier> "+
				"--scopes <list> on this account, then retry the command.", b.namespace()))
	}
	if product.Gateway.Audience == "" {
		return denial("auth.product_not_configured",
			fmt.Sprintf("the account the %q context authenticates as registers no audience for its "+
				"%q product's gateway, so the shell cannot prove what a token it issues is bound to",
				b.Selection.Context.Name, b.namespace()),
			fmt.Sprintf("Record the API's resource identifier with wso2 %s connect <gateway-url> --gateway "+
				"--audience <api resource identifier> --replace.", b.namespace()))
	}
	if b.Selection.Identity.Auth.Kind == contexts.KindClientCredentials &&
		!b.Capabilities.Product.Gateway.AllowsMachine(modules.MachineInline) {
		return denial("auth.product_not_configured",
			fmt.Sprintf("the %q product's gateway does not accept the machine client this account holds",
				b.namespace()),
			"Select an account that signs in through the browser, or install a version of the module "+
				"whose descriptor says how a machine client reaches its gateway.")
	}
	for _, scope := range request.Scopes {
		if !slices.Contains(product.Gateway.Scopes, scope) {
			return denial("auth.product_not_configured",
				fmt.Sprintf("the %q module asked for the %q permission, which this account's %q "+
					"gateway record does not carry", b.namespace(), scope, b.namespace()),
				fmt.Sprintf("Record the permission with wso2 %s connect <gateway-url> --gateway "+
					"--audience <api resource identifier> --scopes <list> --replace, then retry the command.",
					b.namespace()))
		}
	}
	return nil
}

// checkHomeTenant refuses a context that points an identity at an organization
// its session does not belong to.
//
// A logged-in session is minted in one tenant. Using it against another would
// mean either silently ignoring the organization the context names or sending
// a token the target will reject, and both leave a user believing a command ran
// somewhere it did not.
func (b *Broker) checkHomeTenant() error {
	organization := b.Selection.Context.Organization
	if organization == "" || organization == b.Selection.Identity.Auth.Tenant {
		return nil
	}
	return denial("auth.organization_switch_unsupported",
		fmt.Sprintf("the %q context targets the %q organization, and this release cannot switch the "+
			"%q account's session out of its home tenant",
			b.Selection.Context.Name, organization, b.Selection.Identity.Name),
		"Select a context that stays in the account's home tenant, or add an account whose home "+
			"tenant is the organization you are targeting and log in as it.")
}

// developmentSource admits the architecture proof's fixture credential.
//
// It is deliberately the narrowest source in the shell: it answers for the
// reserved proof namespace only, because the issuer behind it is a development
// fixture and a product module reaching it must never be handed fixture access.
func (b *Broker) developmentSource() (source, error) {
	if b.Namespace != ProofNamespace {
		return nil, denial("auth.namespace_not_brokered",
			fmt.Sprintf("the %q module asked for access, and this shell brokers access for the "+
				"non-production %q proof only", b.namespace(), ProofNamespace),
			"Install a module the WSO2 CLI can authenticate, or run the command without it.")
	}
	if b.Selection.Context.Organization == "" {
		return nil, denial("auth.organization_not_selected",
			fmt.Sprintf("the %q context names no organization to act within", b.Selection.Context.Name),
			"Select a context that names the organization the command targets.")
	}
	credential, err := b.credential()
	if err != nil {
		return nil, err
	}
	return devSource{
		namespace:    b.namespace(),
		credential:   credential,
		organization: b.Selection.Context.Organization,
		invocation:   b.InvocationID,
	}, nil
}

// inlineSource admits a non-interactive identity that carries its own
// credential, or reaches a product that carries its own.
//
// A product recording clientIdVariable and clientSecretVariable is reached as
// itself, at its own issuer, rather than as the identity's machine client: the
// two are validated together, so recording one is recording both. A product
// reached through a grant but naming no credential of its own has nothing to
// present there and is refused rather than sent with a client the target
// issuer never registered. Anything else uses the account's own client and
// secret, at the account's own issuer.
//
// The secret is read here rather than at the moment of the grant, so a job that
// forgot to export it is told so before the shell reaches out to an issuer that
// was never going to be able to help.
func (b *Broker) inlineSource(request Request) (source, error) {
	access, _ := b.Selection.Identity.Access(b.recordKey(request))
	product := b.Selection.Identity.Products[b.Namespace]
	clientID := b.Selection.Identity.Auth.ClientID
	secretVariable := b.Selection.Identity.Auth.ClientSecretVariable
	// A gateway record is minted from the account's own machine client at
	// the login provider, whatever credential the product's own record
	// carries for its own issuer, so the product's credential and grant are
	// consulted only for the product's own record. There the secret belongs
	// to the client the record names for it: the one in its own variable, or
	// the grant's public client, which is then presented with a secret it
	// was registered with.
	ownRecord := request.Record != contexts.GatewayRecord
	if ownRecord && product.ClientSecretVariable != "" {
		clientID = ""
		if product.Grant != nil {
			clientID = product.Grant.ClientID
		}
		if product.ClientIDVariable != "" {
			id, err := b.namedSecret(product.ClientIDVariable, "the product's client id")
			if err != nil {
				return nil, err
			}
			clientID = id
		}
		secretVariable = product.ClientSecretVariable
	} else if ownRecord && product.Grant != nil {
		// The derivation refreshes a session for an identity token. An
		// automated identity has no session and no identity token, so it has
		// nothing to present; the product's own issuer does not accept the
		// identity's machine client, and its own credential is a different
		// registration this release does not read without being told.
		return nil, denial("auth.kind_not_implemented",
			fmt.Sprintf("the %q product is reached through its own issuer, which does not accept "+
				"this account's machine client", b.namespace()),
			"Record the product's own client credential on its entry with clientIdVariable and "+
				"clientSecretVariable, or select an interactive account.")
	}
	secret, err := b.namedSecret(secretVariable, "the client secret")
	if err != nil {
		return nil, err
	}
	return clientCredentialsSource{
		namespace:      b.namespace(),
		contextName:    b.Selection.Context.Name,
		issuer:         access.Issuer,
		clientID:       clientID,
		resource:       access.Resource,
		audience:       access.Audience,
		secret:         secret,
		secretVariable: secretVariable,
		client:         b.httpClient(),
	}, nil
}

// httpClient is what reaches an issuer. It defaults to the process-wide client
// rather than one this package builds, so a deployment's proxy and certificate
// configuration applies to shell traffic exactly as it does to everything else.
func (b *Broker) httpClient() *http.Client {
	if b.HTTPClient != nil {
		return b.HTTPClient
	}
	return http.DefaultClient
}
