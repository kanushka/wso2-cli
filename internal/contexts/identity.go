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

package contexts

import (
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"slices"
)

// The authentication kinds a schema version 2 document may declare.
const (
	// KindOAuthBrowser is an interactive browser Authorization Code + PKCE
	// login against the identity's issuer.
	KindOAuthBrowser = "oauth-browser"
	// KindOAuthDevice is an interactive device-code login for terminals
	// without a browser.
	KindOAuthDevice = "oauth-device"
	// KindClientCredentials is a non-interactive client-credentials grant
	// whose client secret is read from a named environment variable.
	KindClientCredentials = "client-credentials"
	// KindPAT is a personal access token held in the OS secure store.
	KindPAT = "pat"
)

// legalKinds are the authentication kinds a v2 document may declare. Which of
// them this release implements is broker policy; a legal-but-unimplemented
// kind stays readable and is refused when a command needs access.
var legalKinds = map[string]bool{
	KindOAuthBrowser: true, KindOAuthDevice: true,
	KindClientCredentials: true, KindPAT: true,
}

// The identity providers a document may name.
//
// Naming one is how a document says what it points at, which is what the person
// writing it knows. What that product requires of a token request is what this
// shell knows, and the list exists so the two do not have to be written twice.
// Omitting the member entirely is the open-world case: any conforming OpenID
// provider stays describable, and derives the way every deployment did before
// this member existed.
const (
	// ProviderAsgardeo is WSO2's identity cloud.
	ProviderAsgardeo = "asgardeo"
	// ProviderIdentityServer is a self-hosted WSO2 Identity Server.
	ProviderIdentityServer = "identity-server"
	// ProviderThunder is a ThunderID deployment.
	ProviderThunder = "thunder"
)

// The derivations a document may declare.
const (
	// DerivationScopedRefresh narrows the login session by asking the refresh
	// grant for the module's own permissions. It is what every deployment this
	// shell served before resource indicators existed, and the default.
	DerivationScopedRefresh = "scoped-refresh"
	// DerivationTokenResource binds each request to one protected resource with
	// an RFC 8707 resource indicator, and narrows permissions alongside it.
	DerivationTokenResource = "token-resource"
)

// providerDerivation is the derivation each named product requires.
//
// Asgardeo and Identity Server take no audience at authorization time, so one
// session serves every product and the scoped refresh answers for both. Thunder
// requires a resource indicator on the authorization request and accepts only
// one, so its sessions are bound to a single protected resource from the moment
// they are established.
var providerDerivation = map[string]string{
	ProviderAsgardeo:       DerivationScopedRefresh,
	ProviderIdentityServer: DerivationScopedRefresh,
	ProviderThunder:        DerivationTokenResource,
}

// legalDerivations are the derivations this shell implements.
var legalDerivations = map[string]bool{
	DerivationScopedRefresh: true, DerivationTokenResource: true,
}

// Providers are the identity providers a document may name, in a stable order.
//
// It is exported because the list has readers outside this package — the live
// runs describe a deployment before building a document from it, and a harness
// that accepted a name the shell then refused would report a configuration
// mistake as a failed deployment. One list, one place to add the next product.
func Providers() []string {
	return slices.Sorted(maps.Keys(providerDerivation))
}

// refPattern constrains a credential reference to one readable word, exactly
// as context names are constrained. A credential value pasted where a
// reference belongs — a JWT, anything with dots, equals signs, or upper-case
// runs — fails this pattern by construction and is rejected rather than stored.
//
// That is what makes the invariant checkable: a document holds a name for a
// credential and never a credential, so writing one grants the writer nothing.
// See docs/adr/0011-writing-a-context-or-identity-grants-nothing.md, which
// this pattern is the enforcement of.
var refPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// Identity is one authentication arrangement the shell can log in as. It names
// where credentials come from and never holds one.
type Identity struct {
	// Name identifies the identity; contexts reference it.
	Name string `json:"name"`
	// Type says whether the identity targets a cloud or on-premises
	// deployment. It is "cloud" or "onprem".
	Type string `json:"type"`
	// Auth says how the shell authenticates as this identity.
	Auth IdentityAuth `json:"auth"`
	// Products are the product services reachable under this identity, keyed
	// by product namespace.
	Products map[string]Product `json:"products,omitempty"`
	// synthetic marks an identity manufactured by the v1 compatibility read.
	// It is never encodable.
	synthetic bool
}

// IdentityAuth is an identity's authentication arrangement. Every member is a
// name or a location; none holds a credential.
type IdentityAuth struct {
	// Kind identifies how the shell obtains access.
	Kind string `json:"kind"`
	// Issuer is the token issuer the shell authenticates against.
	Issuer string `json:"issuer,omitempty"`
	// ClientID is the OAuth client this shell presents itself as.
	ClientID string `json:"clientId,omitempty"`
	// Tenant is the identity's home tenant, when the issuer is multi-tenant.
	Tenant string `json:"tenant,omitempty"`
	// CredentialRef names the secure-store entry holding the identity's
	// session. It is a reference, never a value.
	CredentialRef string `json:"credentialRef,omitempty"`
	// ClientSecretVariable names the environment variable holding the client
	// secret for the client-credentials kind. It is a name, never a value.
	ClientSecretVariable string `json:"clientSecretVariable,omitempty"`
	// Provider names the identity provider behind the issuer. It is optional,
	// and it implies a derivation rather than being one.
	Provider string `json:"provider,omitempty"`
	// Narrowing names the derivation explicitly, for a deployment that does not
	// match what its provider ordinarily requires. It is optional and wins over
	// what Provider implies.
	Narrowing string `json:"narrowing,omitempty"`
	// CredentialVariable exists only on synthetic v1 identities. Never encoded.
	CredentialVariable string `json:"-"`
}

// Derivation is how access for one module is derived under this identity.
//
// It is decided in one place because everything downstream — the login that
// establishes a session, and the grant that narrows it — has to agree, and a
// disagreement between them is a token bound to the wrong thing rather than a
// failure anyone can read.
//
// The order is a default and an override, not two assertions that could
// contradict each other: a provider states what its product ordinarily
// requires, and an explicit derivation states what this deployment actually
// does. Saying both is legal, because a deployment that has not registered a
// resource server is a real state and the document has to be able to say so.
func (a IdentityAuth) Derivation() string {
	if a.Narrowing != "" {
		return a.Narrowing
	}
	if derivation, named := providerDerivation[a.Provider]; named {
		return derivation
	}
	return DerivationScopedRefresh
}

// Product is one product service reachable under an identity.
type Product struct {
	// Endpoint is the product service's base URL.
	Endpoint string `json:"endpoint"`
	// Audience is the token audience the product's services accept.
	Audience string `json:"audience,omitempty"`
	// Scopes are the permissions the shell may request for this product.
	Scopes []string `json:"scopes,omitempty"`
}

// Synthetic reports whether this identity was manufactured by the v1
// compatibility read. A synthetic identity is readable but never written back.
func (i Identity) Synthetic() bool { return i.synthetic }

func (i Identity) validate() error {
	if !namePattern.MatchString(i.Name) {
		return malformed(fmt.Sprintf("declares an invalid identity name %q", i.Name))
	}
	if i.Type != "cloud" && i.Type != "onprem" {
		return malformed(fmt.Sprintf("declares an identity type for %q that is neither cloud nor onprem", i.Name))
	}
	if err := i.Auth.validate(i.Name); err != nil {
		return err
	}
	if err := i.validateDerivation(); err != nil {
		return err
	}
	// The namespaces are walked in sorted order so a document with more than
	// one unreadable product is refused for the same reason on every run.
	for _, namespace := range slices.Sorted(maps.Keys(i.Products)) {
		if !namePattern.MatchString(namespace) {
			return malformed(fmt.Sprintf("declares an invalid product namespace on the identity %q", i.Name))
		}
		if err := i.Products[namespace].validate(i.Name); err != nil {
			return err
		}
	}
	return nil
}

// validateDerivation refuses a document whose derivation cannot be carried out
// as written.
//
// A resource-bound derivation names the protected resource it binds to, and
// takes that name from the product the module asks for. Two consequences
// follow, and both are refused here rather than at the end of a browser
// sign-in: a product that names no audience leaves nothing to bind to, and an
// identity serving several products cannot be served by one session at all,
// because the deployments that require a resource indicator accept only one per
// authorization.
func (i Identity) validateDerivation() error {
	if i.Auth.Derivation() != DerivationTokenResource {
		return nil
	}
	// Exactly one, not at most one. A deployment that binds by resource takes
	// the resource from the identity's product, so an identity with none has
	// nothing to name: login would send no indicator and be refused, which is
	// the failure this whole validation exists to move earlier.
	if len(i.Products) != 1 {
		return malformed(fmt.Sprintf(
			"declares the identity %q against a deployment that binds one login to one product, "+
				"and gives it %d", i.Name, len(i.Products)))
	}
	for _, namespace := range slices.Sorted(maps.Keys(i.Products)) {
		audience := i.Products[namespace].Audience
		if audience == "" {
			return malformed(fmt.Sprintf(
				"declares the %q product on the identity %q without the audience its deployment "+
					"binds access to", namespace, i.Name))
		}
		// The audience travels as an RFC 8707 resource indicator, which section
		// 2 of that specification requires to be an absolute URI carrying no
		// fragment. A bare identifier is the shape the other two products use
		// and is accepted by neither the specification nor a deployment reading
		// it, so it is refused here rather than at the end of a browser sign-in
		// that ends in invalid_target.
		//
		// The rule is the specification's and stops there. Requiring a
		// particular scheme, or a host, would refuse identifiers RFC 8707
		// permits — a URN names a resource server perfectly well — and this
		// shell never dereferences the value, so it has no reason to hold an
		// opinion the specification does not.
		parsed, err := url.Parse(audience)
		if err != nil || parsed.Scheme == "" || parsed.Fragment != "" {
			return malformed(fmt.Sprintf(
				"declares the %q product on the identity %q with an audience that is not an "+
					"absolute URI, which is what its deployment binds access by", namespace, i.Name))
		}
	}
	return nil
}

func (a IdentityAuth) validate(identity string) error {
	if a.Provider != "" {
		if _, known := providerDerivation[a.Provider]; !known {
			return malformed(fmt.Sprintf(
				"declares an identity provider for %q that this shell does not read", identity))
		}
	}
	if a.Narrowing != "" && !legalDerivations[a.Narrowing] {
		return malformed(fmt.Sprintf(
			"declares a derivation for the identity %q that this shell does not implement", identity))
	}
	if !legalKinds[a.Kind] {
		return malformed(fmt.Sprintf("declares an authentication kind for the identity %q that this shell does not read", identity))
	}
	switch a.Kind {
	case KindOAuthBrowser, KindOAuthDevice:
		if a.Issuer == "" || a.ClientID == "" {
			return malformed(fmt.Sprintf("declares the interactive identity %q without an issuer and client identifier", identity))
		}
		if a.CredentialRef == "" || !refPattern.MatchString(a.CredentialRef) {
			// The rejected value is not echoed: what was pasted where a
			// reference belongs may be a credential.
			return contextProblem("contexts.document_malformed",
				fmt.Sprintf("the identity %q does not name a secure-store reference as its credential source", identity),
				"Name the secure-store entry, not a credential value. A reference is one lower-case word.")
		}
		if a.ClientSecretVariable != "" {
			return malformed(fmt.Sprintf("declares a client secret source on the interactive identity %q", identity))
		}
	case KindClientCredentials:
		if a.Issuer == "" || a.ClientID == "" {
			return malformed(fmt.Sprintf("declares the identity %q without an issuer and client identifier", identity))
		}
		if a.ClientSecretVariable == "" || !variablePattern.MatchString(a.ClientSecretVariable) {
			return contextProblem("contexts.document_malformed",
				fmt.Sprintf("the identity %q does not name an environment variable as its client secret source", identity),
				"Name the environment variable holding the client secret, not the secret itself.")
		}
		if a.CredentialRef != "" {
			return malformed(fmt.Sprintf("declares a secure-store reference on the non-interactive identity %q", identity))
		}
	case KindPAT:
		if a.CredentialRef == "" || !refPattern.MatchString(a.CredentialRef) {
			return contextProblem("contexts.document_malformed",
				fmt.Sprintf("the identity %q does not name a secure-store reference as its credential source", identity),
				"Name the secure-store entry, not a credential value. A reference is one lower-case word.")
		}
		// A personal access token has no client secret. Accepting the member
		// anyway would leave the one field this shell pattern-checks for a
		// pasted value unchecked on the kind whose users are most likely to be
		// holding a raw token, so it is refused rather than ignored.
		if a.ClientSecretVariable != "" {
			return malformed(fmt.Sprintf("declares a client secret source on the token identity %q", identity))
		}
	}
	// The issuer URL, like an endpoint, may not embed user information.
	if a.Issuer != "" {
		parsed, err := url.Parse(a.Issuer)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return malformed(fmt.Sprintf("declares an issuer for the identity %q that this shell cannot read", identity))
		}
		if parsed.User != nil {
			return malformed(fmt.Sprintf("declares an issuer for the identity %q that embeds credentials in its URL", identity))
		}
	}
	return nil
}

func (p Product) validate(identity string) error {
	// The same endpoint rules the v1 context enforced, including the
	// credentials-in-URL rejection. The endpoint is never echoed: a rejected
	// one is the most likely place for a credential to have been typed by
	// mistake.
	if p.Endpoint == "" {
		return malformed(fmt.Sprintf("declares a product without an endpoint on the identity %q", identity))
	}
	parsed, err := url.Parse(p.Endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return malformed(fmt.Sprintf("declares a product endpoint on the identity %q that this shell cannot read", identity))
	}
	if parsed.User != nil {
		return contextProblem("contexts.document_malformed",
			fmt.Sprintf("a product endpoint on the identity %q embeds credentials in its URL", identity),
			"Remove the user information from the endpoint. A context names a credential source; "+
				"it never carries a credential.")
	}
	return nil
}
