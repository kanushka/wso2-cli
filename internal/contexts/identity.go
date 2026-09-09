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
	"strings"
)

// The deployment kinds an identity may declare in its type member.
const (
	// TypeCloud marks an identity targeting a WSO2-operated cloud deployment.
	TypeCloud = "cloud"
	// TypeOnprem marks an identity targeting a self-hosted deployment.
	TypeOnprem = "onprem"
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

// IdentityTypeForIssuer says which deployment kind an issuer URL points at:
// an issuer on a WSO2-operated cloud host is TypeCloud, and anything else is
// TypeOnprem, because a host WSO2 does not operate can only be self-hosted.
//
// The answer is descriptive today: the type member selects defaults and
// wording, never structure (docs/examples/authentication-contexts.md), and no
// logic in this repository branches on it beyond how the login report phrases
// itself. The derivation exists so the document tells the truth about the
// deployment kind — a login against Asgardeo must not record WSO2's own cloud
// as an on-premises deployment.
//
// Asgardeo issuers live on api.asgardeo.io, the one cloud host this
// repository's guides and research name; the whole asgardeo.io zone is
// recognized so a regional or future Asgardeo host is described the same way.
// A URL that does not parse yields TypeOnprem — the open-world default — but
// login refuses such a URL before this question is ever asked.
func IdentityTypeForIssuer(issuer string) string {
	parsed, err := url.Parse(issuer)
	if err != nil {
		return TypeOnprem
	}
	if asgardeoHost(parsed.Hostname()) {
		return TypeCloud
	}
	return TypeOnprem
}

// asgardeoHost reports whether a host lies in the asgardeo.io zone. It is the
// one place the zone is spelled, shared by the deployment-kind and tenant
// derivations so the two cannot disagree about what counts as Asgardeo.
//
// A fully-qualified spelling with the DNS root dot — api.asgardeo.io. — names
// the same host, so one terminal dot is trimmed before matching; without that,
// an issuer a user wrote in the fully-qualified form would be recorded as
// self-hosted (review on #161).
func asgardeoHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	return host == "asgardeo.io" || strings.HasSuffix(host, ".asgardeo.io")
}

// asgardeoTenantPath matches the tenant-qualified path an Asgardeo issuer
// carries: /t/<tenant>, followed by the rest of the issuer's path.
var asgardeoTenantPath = regexp.MustCompile(`^/t/([^/]+)(?:/|$)`)

// TenantForIssuer says which Asgardeo tenant an issuer URL belongs to, and
// answers the empty string for every other issuer.
//
// Asgardeo qualifies its issuers by tenant in the URL path —
// https://api.asgardeo.io/t/<tenant>/oauth2/token — so on that host the path,
// not the host, says whose organization a login lands in. Every tenant shares
// the host, which is why anything derived from the host alone describes the
// vendor rather than the tenant. The tenant is read here, once, so the name a
// login derives and the organization it records come from the same parse.
//
// The derivation fails closed. An issuer off the Asgardeo zone keeps its path
// to itself — a self-hosted deployment may put anything there, including a
// /t/<something> that is not a tenant claim this shell can stand behind — and
// an Asgardeo issuer without the /t/<tenant> prefix names no tenant to derive.
// Both answer empty, and empty means everything behaves as it did before this
// function existed.
func TenantForIssuer(issuer string) string {
	parsed, err := url.Parse(issuer)
	if err != nil || !asgardeoHost(parsed.Hostname()) {
		return ""
	}
	match := asgardeoTenantPath.FindStringSubmatch(parsed.Path)
	if match == nil {
		return ""
	}
	return match[1]
}

// refPattern constrains a credential reference to one readable word, exactly
// as context names are constrained. A credential value pasted where a
// reference belongs — a JWT, anything with dots, equals signs, or upper-case
// runs — fails this pattern by construction and is rejected rather than stored.
//
// That is what makes the invariant checkable: a document holds a name for a
// credential and never a credential, so writing one grants the writer nothing.
// See docs/adr/0012-writing-a-context-or-identity-grants-nothing.md, which
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
	// LoginProduct pins which direct product the login authorization is run
	// for. Without it the login product is the first direct product by
	// namespace, so recording a product that sorts earlier would move the
	// login session from under the sessions already stored. Optional: a
	// document written before the field existed keeps the namespace order.
	LoginProduct string `json:"loginProduct,omitempty"`
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
	// Grant says how access for this product is obtained from the identity's
	// session when the product's issuer is not the identity's. Absent, the
	// session itself is narrowed to the product, as it always was.
	Grant *Grant `json:"grant,omitempty"`
	// ClientIDVariable and ClientSecretVariable name the environment
	// variables holding a credential of the product's own, for a
	// client-credentials identity whose machine client the product cannot
	// map to its roles. Names, never values. The client id variable may be
	// omitted for a product reached through a grant, whose public client
	// the secret then belongs to.
	ClientIDVariable     string `json:"clientIdVariable,omitempty"`
	ClientSecretVariable string `json:"clientSecretVariable,omitempty"`
	// Gateway is the product's gateway record, when it has one: a second
	// record beside this management one, reached at the identity's login
	// provider for the API's own resource server. Absent for a product
	// recorded without a gateway, which a document written before the field
	// existed always is.
	Gateway *Gateway `json:"gateway,omitempty"`
}

// GatewayRecord names a product's gateway record, as a broker request and
// the session key spell it.
const GatewayRecord = "gateway"

// GatewayKey is the key a product's gateway record is reported and stored
// under: the namespace, a slash, and GatewayRecord. The slash is admitted by
// neither a namespace nor a credential reference, so the key can collide
// with neither.
func GatewayKey(namespace string) string { return namespace + "/" + GatewayRecord }

// SplitGatewayKey reports the namespace a gateway key names, and whether the
// key is one.
func SplitGatewayKey(key string) (string, bool) {
	namespace, found := strings.CutSuffix(key, "/"+GatewayRecord)
	return namespace, found && namespace != ""
}

// Gateway is a product's gateway record. Like the product it belongs to it
// names a location and what a token for it is bound to, and holds no
// credential.
type Gateway struct {
	// Endpoint is the gateway's base URL.
	Endpoint string `json:"endpoint"`
	// Audience is the API's own resource identifier, the audience a gateway
	// session is bound to.
	Audience string `json:"audience,omitempty"`
	// Scopes are the API's own permissions, the scope set a gateway session
	// is authorized for.
	Scopes []string `json:"scopes,omitempty"`
}

// GrantJWTBearer presents an identity token from the session to the product's
// own token endpoint under RFC 7523's JWT bearer grant, as a public client.
const GrantJWTBearer = "jwt-bearer"

// GrantFederated obtains the product's session at the product's own issuer,
// as the public client the grant names, through the login provider's
// sign-on. The session is the product issuer's own refresh token.
const GrantFederated = "federated"

// GrantExchange exchanges the login session's own access token at the
// identity's issuer, under RFC 8693, for one bound to the product's
// audience. It names neither an issuer nor a client because it uses the
// identity's own: the product never authorizes anything itself.
const GrantExchange = "exchange"

// legalGrants are the grant kinds this shell implements.
var legalGrants = map[string]bool{GrantJWTBearer: true, GrantFederated: true, GrantExchange: true}

// Grant is one way of deriving a product's access from the session. It names
// where the assertion goes and what the assertion must carry; like everything
// else in a document it holds no credential, and the type has nowhere to put
// one.
type Grant struct {
	// Kind names the grant. Only GrantJWTBearer is read.
	Kind string `json:"kind"`
	// Issuer is the product's own OpenID issuer, whose token endpoint takes
	// the assertion.
	Issuer string `json:"issuer"`
	// ClientID is the public client the shell presents at that issuer.
	ClientID string `json:"clientId"`
	// Scopes are what the identity's issuer is asked for when the session is
	// refreshed for the assertion: the scopes that make the identity token
	// carry the claims the product maps. openid is always among them.
	Scopes []string `json:"scopes,omitempty"`
	// Resource is the RFC 8707 resource indicator the authorization for this
	// grant's session carries, when the issuer it runs at requires one. For a
	// jwt-bearer grant that issuer is the identity's; for a federated grant it
	// is the product's own. Optional.
	Resource string `json:"resource,omitempty"`
}

// AssertionScopes are the scopes the session is refreshed with to obtain the
// assertion: the grant's own, with openid added when absent, sorted and
// de-duplicated. An identity token is issued only under openid, so a grant
// that omits it would ask for an assertion the issuer never mints.
func (g Grant) AssertionScopes() []string {
	scopes := []string{"openid"}
	for _, scope := range g.Scopes {
		if !slices.Contains(scopes, scope) {
			scopes = append(scopes, scope)
		}
	}
	slices.Sort(scopes)
	return scopes
}

// Direct reports whether the identity's own session answers for this product.
// A product with a grant is derived instead, at another issuer.
func (p Product) Direct() bool { return p.Grant == nil }

// Synthetic reports whether this identity was manufactured by the v1
// compatibility read. A synthetic identity is readable but never written back.
func (i Identity) Synthetic() bool { return i.synthetic }

func (i Identity) validate() error {
	if !namePattern.MatchString(i.Name) {
		return malformed(fmt.Sprintf("declares an invalid identity name %q", i.Name))
	}
	if i.Type != TypeCloud && i.Type != TypeOnprem {
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
		if i.Products[namespace].ClientSecretVariable != "" && i.Auth.Kind != KindClientCredentials {
			return malformed(fmt.Sprintf(
				"declares a product credential on the interactive identity %q; a product credential "+
					"belongs to a client-credentials identity", i.Name))
		}
	}
	if i.LoginProduct != "" {
		pinned, recorded := i.Products[i.LoginProduct]
		if !recorded || !pinned.Direct() {
			return malformed(fmt.Sprintf(
				"pins the login of the identity %q to a product it does not reach directly", i.Name))
		}
	}
	return nil
}

// validateDerivation refuses a document whose derivation cannot be carried out
// as written.
//
// A resource-bound derivation names the protected resource it binds to, and
// takes that name from the product the module asks for. What follows is
// refused here rather than at the end of a browser sign-in: a product that
// names no audience leaves nothing to bind to, and an identity naming none at
// all has nothing to name either. An identity may still record several direct
// products — each becomes its own resource-bound session, the login's or a
// sibling's, since the deployments this derivation serves accept only one
// resource indicator per authorization, not one product per identity.
func (i Identity) validateDerivation() error {
	if i.Auth.Derivation() != DerivationTokenResource {
		return nil
	}
	if len(i.Products) == 0 {
		return malformed(fmt.Sprintf(
			"declares the identity %q against a deployment that binds a login to a product, "+
				"and gives it none", i.Name))
	}
	for _, namespace := range slices.Sorted(maps.Keys(i.Products)) {
		if !i.Products[namespace].Direct() {
			continue
		}
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
		if !absoluteURI(audience) {
			return malformed(fmt.Sprintf(
				"declares the %q product on the identity %q with an audience that is not an "+
					"absolute URI, which is what its deployment binds access by", namespace, i.Name))
		}
	}
	// A gateway record is always reached at the login provider, so its
	// audience is bound the way a direct product's is, grant or no grant.
	for _, namespace := range slices.Sorted(maps.Keys(i.Products)) {
		gateway := i.Products[namespace].Gateway
		if gateway == nil {
			continue
		}
		if gateway.Audience == "" {
			return malformed(fmt.Sprintf(
				"declares the %q product's gateway on the identity %q without the audience its "+
					"deployment binds access to", namespace, i.Name))
		}
		if !absoluteURI(gateway.Audience) {
			return malformed(fmt.Sprintf(
				"declares the %q product's gateway on the identity %q with an audience that is not an "+
					"absolute URI, which is what its deployment binds access by", namespace, i.Name))
		}
	}
	return nil
}

// absoluteURI reports whether value is an absolute URI carrying no fragment,
// which is what RFC 8707 section 2 requires of a resource indicator.
func absoluteURI(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme != "" && parsed.Fragment == ""
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
	// A product credential is a secret variable and the client it belongs
	// to: the client id from its own variable, or, for a product reached
	// through a grant, the grant's public client. A client id variable
	// without a secret names half of nothing.
	if p.ClientIDVariable != "" && p.ClientSecretVariable == "" {
		return malformed(fmt.Sprintf(
			"declares a product credential on the identity %q with a client id variable and no secret", identity))
	}
	if p.ClientSecretVariable != "" && p.ClientIDVariable == "" && p.Grant == nil {
		return malformed(fmt.Sprintf(
			"declares a product credential on the identity %q with no client id for it: name one with "+
				"clientIdVariable, or reach the product through a grant naming its client", identity))
	}
	if p.ClientSecretVariable != "" {
		if !variablePattern.MatchString(p.ClientSecretVariable) ||
			(p.ClientIDVariable != "" && !variablePattern.MatchString(p.ClientIDVariable)) {
			return contextProblem("contexts.document_malformed",
				fmt.Sprintf("a product on the identity %q does not name environment variables as its credential source", identity),
				"Name the environment variables holding the product's client id and secret, not the values.")
		}
	}
	if p.Gateway != nil {
		if err := p.Gateway.validate(identity); err != nil {
			return err
		}
	}
	if p.Grant != nil {
		// A derived token is proved bound to the product's audience, exactly as
		// a narrowed one is. Without an audience there is nothing to prove it
		// against, and the refusal belongs here rather than at the first
		// command that needs the product.
		if p.Audience == "" {
			return malformed(fmt.Sprintf(
				"declares a product with a grant on the identity %q without the audience the "+
					"derived access is proved against", identity))
		}
		if p.Grant.Kind == GrantExchange && !absoluteURI(p.Audience) {
			// An exchange sends the audience as an RFC 8707 resource
			// indicator, which must be an absolute URI. A deployment answers a
			// bare name with invalid_target, which reads as a resource server
			// nobody registered and sends the reader to create one that is
			// already there. The true cause is stated once, here.
			return malformed(fmt.Sprintf(
				"declares an exchange product on the identity %q whose audience is not an absolute "+
					"URI, and an exchange asks for it as a resource indicator", identity))
		}
		return p.Grant.validate(identity)
	}
	return nil
}

// validate refuses a gateway record this shell could not reach as written.
// The endpoint follows the product endpoint's rules and, like it, is never
// echoed.
func (g Gateway) validate(identity string) error {
	if g.Endpoint == "" {
		return malformed(fmt.Sprintf("declares a product gateway without an endpoint on the identity %q", identity))
	}
	parsed, err := url.Parse(g.Endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return malformed(fmt.Sprintf("declares a product gateway endpoint on the identity %q that this shell cannot read", identity))
	}
	if parsed.User != nil {
		return contextProblem("contexts.document_malformed",
			fmt.Sprintf("a product gateway endpoint on the identity %q embeds credentials in its URL", identity),
			"Remove the user information from the endpoint. A context names a credential source; "+
				"it never carries a credential.")
	}
	return nil
}

// validate refuses a grant this shell could not carry out as written. The
// issuer URL is never echoed: like an endpoint, it is where a credential is
// likeliest to have been typed by mistake.
func (g Grant) validate(identity string) error {
	if !legalGrants[g.Kind] {
		return malformed(fmt.Sprintf(
			"declares a product grant on the identity %q of a kind this shell does not implement", identity))
	}
	if g.Kind == GrantExchange {
		// An exchange is run at the identity's own issuer, as its own client,
		// for the product's own audience. A document that named an issuer or
		// a client here would be stating something the shell already holds and
		// could contradict, so naming either is refused rather than ignored.
		if g.Issuer != "" || g.ClientID != "" {
			return malformed(fmt.Sprintf(
				"declares an exchange grant on the identity %q that names its own issuer or client, "+
					"which an exchange never uses", identity))
		}
		return nil
	}
	if g.ClientID == "" {
		return malformed(fmt.Sprintf(
			"declares a product grant on the identity %q without the client it presents", identity))
	}
	parsed, err := url.Parse(g.Issuer)
	if g.Issuer == "" || err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return malformed(fmt.Sprintf(
			"declares a product grant on the identity %q whose issuer this shell cannot read", identity))
	}
	if parsed.User != nil {
		return contextProblem("contexts.document_malformed",
			fmt.Sprintf("a product grant on the identity %q embeds credentials in its issuer URL", identity),
			"Remove the user information from the issuer. A context names a credential source; "+
				"it never carries a credential.")
	}
	if g.Resource != "" && !absoluteURI(g.Resource) {
		return malformed(fmt.Sprintf(
			"declares a product grant on the identity %q with a resource that is not an absolute URI",
			identity))
	}
	return nil
}
