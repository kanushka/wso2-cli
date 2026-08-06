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
			"Select a context that names the organization and credential source to use.")
	case contexts.MethodDevelopmentCredential:
		return b.developmentSource()
	case contexts.KindOAuthBrowser, contexts.KindOAuthDevice, contexts.KindClientCredentials:
		if err := b.checkProduct(request); err != nil {
			return nil, err
		}
		if err := b.checkHomeTenant(); err != nil {
			return nil, err
		}
		if kind == contexts.KindClientCredentials {
			return b.inlineSource()
		}
		// Both interactive kinds land here, and deliberately on the same
		// source. How a session was established is a fact about a login that
		// already happened; what is left behind is a refresh token, and every
		// step from here — the rotation lock, the scoped refresh, the proof
		// that the narrowing held — reads that and nothing else.
		return sessionSource{
			namespace: b.namespace(),
			identity:  b.Selection.Identity,
			sessions:  session.Store{StateRoot: b.StateRoot},
			client:    b.httpClient(),
		}, nil
	case contexts.KindPAT:
		return nil, denial("auth.kind_not_implemented",
			fmt.Sprintf("the %q context uses an authentication kind this release does not implement",
				b.Selection.Context.Name),
			"Select a context whose identity logs in through the browser or through a device code, "+
				"or one that uses client credentials. Personal access token login is planned.")
	default:
		return nil, denial("auth.method_unsupported",
			fmt.Sprintf("the %q context uses an authentication method this shell does not implement",
				b.Selection.Context.Name),
			"Select a context with a supported authentication kind.")
	}
}

// checkProduct proves the identity registers the namespace asking, for what the
// module actually asked for.
//
// The registration is the deployment's own statement of what this identity may
// reach, so a request it does not cover is refused rather than attempted: an
// issuer would answer with its own error, and a user reading it would have no
// way to tell a misregistered product from a broken one. Audiences and scope
// names are not secrets, so a refusal states both sides of the mismatch.
func (b *Broker) checkProduct(request Request) error {
	product, configured := b.Selection.Identity.Products[b.Namespace]
	if !configured {
		return denial("auth.product_not_configured",
			fmt.Sprintf("the identity the %q context authenticates as does not configure the %q product",
				b.Selection.Context.Name, b.namespace()),
			fmt.Sprintf("Add the %q product to this identity in the context document, or select a "+
				"context whose identity reaches it.", b.namespace()))
	}
	if product.Audience != "" && request.Audience != product.Audience {
		return denial("auth.product_not_configured",
			fmt.Sprintf("the %q module asked for the %q audience, and this identity registers its %q "+
				"product against %q", b.namespace(), request.Audience, b.namespace(), product.Audience),
			"Register the audience the module needs on this identity's product entry, or install a "+
				"module built for the deployment this identity serves.")
	}
	if len(product.Scopes) > 0 {
		for _, scope := range request.Scopes {
			if !slices.Contains(product.Scopes, scope) {
				return denial("auth.product_not_configured",
					fmt.Sprintf("the %q module asked for the %q permission, which this identity's %q "+
						"product does not carry", b.namespace(), scope, b.namespace()),
					"Add the permission to this identity's product entry once the deployment grants "+
						"it, then retry the command.")
			}
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
			"%q identity's session out of its home tenant",
			b.Selection.Context.Name, organization, b.Selection.Identity.Name),
		"Select a context that stays in the identity's home tenant, or add an identity whose home "+
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
// credential.
//
// The secret is read here rather than at the moment of the grant, so a job that
// forgot to export it is told so before the shell reaches out to an issuer that
// was never going to be able to help.
func (b *Broker) inlineSource() (source, error) {
	variable := b.Selection.Identity.Auth.ClientSecretVariable
	secret, err := b.namedSecret(variable, "the client secret")
	if err != nil {
		return nil, err
	}
	return clientCredentialsSource{
		namespace:      b.namespace(),
		contextName:    b.Selection.Context.Name,
		identity:       b.Selection.Identity,
		secret:         secret,
		secretVariable: variable,
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
