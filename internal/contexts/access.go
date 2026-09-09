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
	"maps"
	"slices"
)

// The strategies by which a product's access is obtained. They are what
// wso2 whoami reports and what a session records about itself.
const (
	// StrategyDirect: the login session itself covers the product.
	StrategyDirect = "direct"
	// StrategySibling: a second session at the login issuer, under the
	// product's own resource or scope set.
	StrategySibling = "sibling"
	// StrategyDerived: an assertion session at the login issuer, presented to
	// the product's own issuer under the jwt-bearer grant per command.
	StrategyDerived = "derived"
	// StrategyFederated: a session at the product's own issuer, obtained as
	// the product's public client through the login provider's sign-on.
	StrategyFederated = "federated"
	// StrategyExchanged: no session of the product's own; the login session's
	// access token is exchanged at the login issuer, under RFC 8693, for one
	// bound to the product's audience, per command.
	StrategyExchanged = "exchanged"
	// StrategyInline: no session; a client-credentials grant per command.
	StrategyInline = "inline"
)

// ProductAccess is how one product under an identity is reached. Everything
// in it is public configuration; nothing is a credential.
type ProductAccess struct {
	Namespace  string
	Strategy   string
	Issuer     string
	ClientID   string
	Scopes     []string
	Resource   string
	SessionRef string
	Audience   string
}

// ProductSessionRef is the secure-store entry a product's own session lives
// under. The separator is a dot, which refPattern never admits in a
// credential reference, so it can never collide with another identity's.
func ProductSessionRef(credentialRef, namespace string) string {
	return credentialRef + "." + namespace
}

// LoginAccess is the authorization wso2 login runs first: the pinned login
// product, else the first direct product by namespace, else a bare session
// when the identity records no direct product.
func (i Identity) LoginAccess() ProductAccess {
	access := ProductAccess{
		Strategy: StrategyDirect, Issuer: i.Auth.Issuer, ClientID: i.Auth.ClientID,
		SessionRef: i.Auth.CredentialRef,
	}
	candidates := slices.Sorted(maps.Keys(i.Products))
	if pinned, recorded := i.Products[i.LoginProduct]; recorded && pinned.Direct() {
		candidates = append([]string{i.LoginProduct}, candidates...)
	}
	for _, namespace := range candidates {
		product := i.Products[namespace]
		if !product.Direct() {
			continue
		}
		access.Namespace = namespace
		access.Audience = product.Audience
		access.Scopes = sortedScopes(product.Scopes)
		if i.Auth.Derivation() == DerivationTokenResource {
			access.Resource = product.Audience
		}
		return access
	}
	return access
}

// Access is how the named product is reached, and whether it is recorded.
// The name is a product namespace, or a gateway key (GatewayKey) for the
// product's gateway record.
func (i Identity) Access(namespace string) (ProductAccess, bool) {
	if product, found := SplitGatewayKey(namespace); found {
		return i.gatewayAccess(product)
	}
	product, recorded := i.Products[namespace]
	if !recorded {
		return ProductAccess{}, false
	}
	if i.Auth.Kind == KindClientCredentials {
		return i.inlineAccess(namespace, product), true
	}
	login := i.LoginAccess()
	access := ProductAccess{
		Namespace: namespace, Issuer: i.Auth.Issuer, ClientID: i.Auth.ClientID,
		Audience: product.Audience, SessionRef: ProductSessionRef(i.Auth.CredentialRef, namespace),
	}
	switch {
	case product.Direct():
		access.Scopes = sortedScopes(product.Scopes)
		if i.Auth.Derivation() == DerivationTokenResource {
			access.Resource = product.Audience
		}
		if namespace == login.Namespace ||
			(slices.Equal(access.Scopes, login.Scopes) && access.Resource == login.Resource) {
			access.Strategy = StrategyDirect
			access.SessionRef = login.SessionRef
		} else {
			access.Strategy = StrategySibling
		}
	case product.Grant.Kind == GrantExchange:
		// The exchange runs at the identity's own issuer, as its own client,
		// and asks for the product's audience: the access already carries
		// both, so only the strategy and the resource are set here. The
		// session reference is cleared because an exchanged product keeps no
		// session — the token is minted from the login session for one
		// command and never stored.
		access.Strategy = StrategyExchanged
		access.Resource = product.Audience
		access.Scopes = sortedScopes(product.Scopes)
		access.SessionRef = ""
	case product.Grant.Kind == GrantJWTBearer:
		access.Strategy = StrategyDerived
		access.Scopes = product.Grant.AssertionScopes()
		access.Resource = product.Grant.Resource
	case product.Grant.Kind == GrantFederated:
		access.Strategy = StrategyFederated
		access.Issuer = product.Grant.Issuer
		access.ClientID = product.Grant.ClientID
		access.Scopes = sortedScopes(product.Scopes)
		access.Resource = product.Grant.Resource
	}
	return access, true
}

// gatewayAccess is how a product's gateway record is reached, and whether
// the product records one. A gateway validates tokens from the login
// provider, so the record is always reached there, under the API's own
// resource and scope set: direct when those happen to be the login
// session's, else sibling; inline for a client-credentials identity, minted
// from its machine client with the gateway audience as the resource.
func (i Identity) gatewayAccess(namespace string) (ProductAccess, bool) {
	product, recorded := i.Products[namespace]
	if !recorded || product.Gateway == nil {
		return ProductAccess{}, false
	}
	gateway := product.Gateway
	key := GatewayKey(namespace)
	access := ProductAccess{
		Namespace: key, Issuer: i.Auth.Issuer, ClientID: i.Auth.ClientID,
		Audience: gateway.Audience, Scopes: sortedScopes(gateway.Scopes),
	}
	if i.Auth.Derivation() == DerivationTokenResource {
		access.Resource = gateway.Audience
	}
	if i.Auth.Kind == KindClientCredentials {
		access.Strategy = StrategyInline
		return access, true
	}
	login := i.LoginAccess()
	access.SessionRef = ProductSessionRef(i.Auth.CredentialRef, key)
	access.Strategy = StrategySibling
	if slices.Equal(access.Scopes, login.Scopes) && access.Resource == login.Resource {
		access.Strategy = StrategyDirect
		access.SessionRef = login.SessionRef
	}
	return access, true
}

// RecordKeys names every record the identity holds, in namespace order: each
// product's own namespace, followed by its gateway key when it records a
// gateway. Each is a name Access resolves.
func (i Identity) RecordKeys() []string {
	var keys []string
	for _, namespace := range slices.Sorted(maps.Keys(i.Products)) {
		keys = append(keys, namespace)
		if i.Products[namespace].Gateway != nil {
			keys = append(keys, GatewayKey(namespace))
		}
	}
	return keys
}

// inlineAccess is a client-credentials identity's plan for one product: no
// session, one grant per command, at the product's own issuer when it names
// one.
func (i Identity) inlineAccess(namespace string, product Product) ProductAccess {
	access := ProductAccess{
		Namespace: namespace, Strategy: StrategyInline, Issuer: i.Auth.Issuer,
		ClientID: i.Auth.ClientID, Audience: product.Audience, Scopes: sortedScopes(product.Scopes),
	}
	if product.Grant != nil {
		access.Issuer = product.Grant.Issuer
		access.Resource = product.Grant.Resource
	} else if i.Auth.Derivation() == DerivationTokenResource {
		access.Resource = product.Audience
	}
	return access
}

// Accesses is every authorization the identity needs, the login one first
// and then one per further session, in namespace order, a product's gateway
// record right after the product's own. A client-credentials identity lists
// every record, each inline.
func (i Identity) Accesses() []ProductAccess {
	if i.Auth.Kind == KindClientCredentials {
		var all []ProductAccess
		for _, key := range i.RecordKeys() {
			access, _ := i.Access(key)
			all = append(all, access)
		}
		return all
	}
	all := []ProductAccess{i.LoginAccess()}
	for _, key := range i.RecordKeys() {
		access, _ := i.Access(key)
		// An exchanged product holds no session, so there is no authorization
		// to run for it and nothing a login could leave behind. Its access is
		// minted from the login session at the moment a command needs it.
		if access.Strategy == StrategyExchanged {
			continue
		}
		if access.SessionRef != all[0].SessionRef {
			all = append(all, access)
		}
	}
	return all
}

func sortedScopes(scopes []string) []string {
	sorted := slices.Clone(scopes)
	slices.Sort(sorted)
	return slices.Compact(sorted)
}
