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

package modules

import (
	"fmt"
	"slices"
	"strings"

	"github.com/wso2/wso2-cli/internal/contexts"
)

// The two shapes a product's token audience takes.
const (
	// AudienceResource: the audience is a resource server URI, sent as an
	// RFC 8707 resource indicator. ThunderID.
	AudienceResource = "resource"
	// AudienceClient: the audience is the client id the shell presents.
	// API Manager, Asgardeo.
	AudienceClient = "client"
)

// The strategies a product may allow a client-credentials identity.
const (
	// MachineInline: the identity's machine client, registered at the login
	// provider, is minted per product.
	MachineInline = "inline"
	// MachineCredential: the product does not accept the machine client and
	// carries a credential of its own on the record.
	MachineCredential = "credential"
)

// ProductDescriptor is what a module declares about reaching its product,
// so wso2 <namespace> connect <url> can write the product record from the
// URL alone. It is published in the module manifest, travels through the
// catalog and the release record, and lands in the receipt beside the
// access requests, because it is the same kind of fact: what this module
// needs of the shell's authentication, stated before the module runs.
//
// Everything in it is public configuration. It names no credential.
type ProductDescriptor struct {
	// Provider names the identity provider this product is, when a login
	// can run against it: thunder, identity-server or asgardeo. Empty for a
	// product that is not a login provider.
	Provider string `json:"provider,omitempty"`
	// IssuerPath is appended to the connect URL to name the issuer. Empty
	// when the URL is the issuer.
	IssuerPath string `json:"issuerPath,omitempty"`
	// ClientID is the public client the shell presents at the issuer, as
	// the product's bootstrap registers it. Empty when the deployment
	// assigns one and connect must be told it.
	ClientID string `json:"clientId,omitempty"`
	// Audience is AudienceResource or AudienceClient.
	Audience string `json:"audience"`
	// DefaultAudience is the resource server URI a resource audience
	// defaults to, when the deployment seeds one.
	DefaultAudience string `json:"defaultAudience,omitempty"`
	// Scopes are the scopes the product's commands need; a session is
	// authorized for exactly these.
	Scopes []string `json:"scopes"`
	// Grant is the grant kind the product needs when it is not the login
	// provider: federated or jwt-bearer. Empty for a product only its own
	// provider serves.
	Grant string `json:"grant,omitempty"`
	// Machine lists the strategies a client-credentials identity may use:
	// MachineInline, MachineCredential, or both.
	Machine []string `json:"machine,omitempty"`
	// Gateway is the shape of the product's gateway record, when the product
	// has one: a second record beside the management one, reached at the
	// identity's login provider for the API's own resource server. Absent
	// for a product without a gateway, which has connect --gateway refused.
	Gateway *GatewayDescriptor `json:"gateway,omitempty"`
}

// GatewayDescriptor is what a module declares about its product's gateway
// record. It names no grant: a gateway validates tokens from the login
// provider, so the record's session is always obtained there, and its
// strategy follows from the identity as direct or sibling for a browser
// identity and inline for a machine identity.
type GatewayDescriptor struct {
	// Audience is AudienceResource or AudienceClient, the kind of value the
	// gateway record's audience is.
	Audience string `json:"audience"`
	// Scopes are the default scope set a gateway record carries. Normally
	// empty: the permissions are the API's own, passed to connect.
	Scopes []string `json:"scopes,omitempty"`
	// Machine lists the strategies a client-credentials identity may use
	// for the gateway record: MachineInline, MachineCredential, or both.
	Machine []string `json:"machine,omitempty"`
}

// AllowsMachine reports whether a client-credentials identity may reach the
// gateway by the named strategy.
func (g GatewayDescriptor) AllowsMachine(strategy string) bool {
	return slices.Contains(g.Machine, strategy)
}

// Issuer names the product's issuer for a connect URL.
func (d ProductDescriptor) Issuer(url string) string {
	return strings.TrimRight(url, "/") + d.IssuerPath
}

// AudienceFor is the token audience for the client the shell presents:
// the client id itself, or the default resource server.
func (d ProductDescriptor) AudienceFor(clientID string) string {
	if d.Audience == AudienceClient {
		return clientID
	}
	return d.DefaultAudience
}

// LoginProvider reports whether a login can run against this product.
func (d ProductDescriptor) LoginProvider() bool { return d.Provider != "" }

// AllowsMachine reports whether a client-credentials identity may reach
// the product by the named strategy.
func (d ProductDescriptor) AllowsMachine(strategy string) bool {
	return slices.Contains(d.Machine, strategy)
}

var legalDescriptorGrants = []string{contexts.GrantFederated, contexts.GrantJWTBearer}

// validate refuses a descriptor the shell could not write a record from.
func (d ProductDescriptor) validate() error {
	refuse := func(what string) error {
		return receiptProblem("modules.receipt_malformed",
			"module receipt declares a product descriptor "+what, reinstallRecovery)
	}
	if d.Audience != AudienceResource && d.Audience != AudienceClient {
		return refuse(fmt.Sprintf("with an audience kind %q that is neither %s nor %s",
			d.Audience, AudienceResource, AudienceClient))
	}
	if d.Provider != "" && !slices.Contains(contexts.Providers(), d.Provider) {
		return refuse(fmt.Sprintf("for an identity provider %q this shell does not read", d.Provider))
	}
	if d.Grant != "" && !slices.Contains(legalDescriptorGrants, d.Grant) {
		return refuse(fmt.Sprintf("with a grant %q this shell does not implement", d.Grant))
	}
	for _, strategy := range d.Machine {
		if strategy != MachineInline && strategy != MachineCredential {
			return refuse(fmt.Sprintf("with a machine strategy %q this shell does not implement", strategy))
		}
	}
	if len(d.Scopes) == 0 {
		return refuse("with no scopes")
	}
	if d.IssuerPath != "" && !strings.HasPrefix(d.IssuerPath, "/") {
		return refuse(fmt.Sprintf("with an issuer path %q that does not start with /", d.IssuerPath))
	}
	if d.Gateway != nil {
		if d.Gateway.Audience != AudienceResource && d.Gateway.Audience != AudienceClient {
			return refuse(fmt.Sprintf("whose gateway block has an audience kind %q that is neither %s nor %s",
				d.Gateway.Audience, AudienceResource, AudienceClient))
		}
		// A gateway is reached at the identity's login provider, so the
		// only machine strategy it can have is the identity's own client.
		for _, strategy := range d.Gateway.Machine {
			if strategy != MachineInline {
				return refuse(fmt.Sprintf("whose gateway block has a machine strategy %q this shell does not implement "+
					"for a gateway, which is reached only from the identity's own client (%s)", strategy, MachineInline))
			}
		}
	}
	return nil
}
