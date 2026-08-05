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
	"strings"
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
	case contexts.KindOAuthBrowser, contexts.KindClientCredentials:
		product, err := b.product(request)
		if err != nil {
			return nil, err
		}
		if err := b.checkHomeTenant(); err != nil {
			return nil, err
		}
		return b.productionSource(kind, product)
	case contexts.KindOAuthDevice, contexts.KindPAT:
		return nil, denial("auth.kind_not_implemented",
			fmt.Sprintf("the %q context uses an authentication kind this release does not implement",
				b.Selection.Context.Name),
			"Select a context whose identity logs in through the browser, or one that uses "+
				"client credentials. Device and personal access token login are planned.")
	default:
		return nil, denial("auth.method_unsupported",
			fmt.Sprintf("the %q context uses an authentication method this shell does not implement",
				b.Selection.Context.Name),
			"Select a context with a supported authentication kind.")
	}
}

// product is the identity's registration for the namespace asking, checked
// against what the module actually asked for.
//
// The registration is the deployment's own statement of what this identity may
// reach, so a request it does not cover is refused rather than attempted: an
// issuer would answer with its own error, and a user reading it would have no
// way to tell a misregistered product from a broken one. Audiences and scope
// names are not secrets, so a refusal states both sides of the mismatch.
func (b *Broker) product(request Request) (contexts.Product, error) {
	product, configured := b.Selection.Identity.Products[b.Namespace]
	if !configured {
		return contexts.Product{}, denial("auth.product_not_configured",
			fmt.Sprintf("the identity the %q context authenticates as does not configure the %q product",
				b.Selection.Context.Name, b.namespace()),
			fmt.Sprintf("Add the %q product to this identity in the context document, or select a "+
				"context whose identity reaches it.", b.namespace()))
	}
	if product.Audience != "" && request.Audience != product.Audience {
		return contexts.Product{}, denial("auth.product_not_configured",
			fmt.Sprintf("the %q module asked for the %q audience, and this identity registers its %q "+
				"product against %q", b.namespace(), request.Audience, b.namespace(), product.Audience),
			"Register the audience the module needs on this identity's product entry, or install a "+
				"module built for the deployment this identity serves.")
	}
	if len(product.Scopes) > 0 {
		for _, scope := range request.Scopes {
			if !slices.Contains(product.Scopes, scope) {
				return contexts.Product{}, denial("auth.product_not_configured",
					fmt.Sprintf("the %q module asked for the %q permission, which this identity's %q "+
						"product does not carry", b.namespace(), scope, b.namespace()),
					"Add the permission to this identity's product entry once the deployment grants "+
						"it, then retry the command.")
			}
		}
	}
	return product, nil
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

// productionSource is the derivation for an identity whose access comes from a
// real issuer. It is reached only after the checks above have admitted the
// request.
func (b *Broker) productionSource(kind string, _ contexts.Product) (source, error) {
	switch kind {
	case contexts.KindOAuthBrowser:
		return browserSource{
			namespace: b.namespace(),
			identity:  b.Selection.Identity,
			sessions:  session.Store{StateRoot: b.StateRoot},
			client:    b.httpClient(),
		}, nil
	default:
		return unavailableSource{}, nil
	}
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

// narrowingRecovery is the way back from every refusal to hand a module a grant
// the shell could not prove was narrowed to its request.
const narrowingRecovery = "Check the deployment's API resource registration and the permissions " +
	"granted to the registered OAuth application, then retry. The shell does not hand a module " +
	"broader access than it asked for."

// verifyIssued proves an issued token is exactly what the module asked for.
//
// It is the check the whole derivation exists to make. A deployment may answer
// a narrowed request with the session's full authority, or with a token bound
// to some other audience, and both look like success at the protocol level. The
// shell refuses rather than degrades: a module that receives more than it asked
// for has been handed authority nobody decided to give it, and a module that
// receives a token its audience will reject fails later for a reason no one can
// diagnose from where it fails.
func verifyIssued(request Request, namespace, accessToken, statedScopes string) (bearerFacts, error) {
	facts, err := bearerClaims(accessToken)
	if err != nil {
		// Without readable claims the shell cannot prove the audience binding,
		// and an unprovable grant is not one this broker issues.
		return bearerFacts{}, denial("auth.narrowing_unavailable",
			fmt.Sprintf("the deployment issued access for the %q module in a form the shell cannot "+
				"check against what the module asked for", namespace),
			narrowingRecovery)
	}
	// The response's own statement wins, because it is the deployment speaking
	// about what it issued. The token's claim answers for issuers that state
	// nothing.
	effective := strings.Fields(statedScopes)
	if len(effective) == 0 {
		effective = facts.Scopes
	}
	if len(effective) == 0 {
		return bearerFacts{}, denial("auth.narrowing_unavailable",
			fmt.Sprintf("the deployment did not state which permissions it issued for the %q module, "+
				"so the shell cannot prove they are the ones it asked for", namespace),
			narrowingRecovery)
	}
	if !sameScopeSet(effective, request.Scopes) {
		// Scope names are not secrets, and naming both sides is the difference
		// between a refusal and a registration a user can go and fix.
		return bearerFacts{}, denial("auth.narrowing_unavailable",
			fmt.Sprintf("the %q module asked for the permissions %s and the deployment issued %s",
				namespace, scopeList(request.Scopes), scopeList(effective)),
			narrowingRecovery)
	}
	if !slices.Contains(facts.Audiences, request.Audience) {
		return bearerFacts{}, denial("auth.narrowing_unavailable",
			fmt.Sprintf("the deployment issued access that is not bound to the %q audience the %q "+
				"module needs", request.Audience, namespace),
			narrowingRecovery)
	}
	return facts, nil
}

// sameScopeSet reports whether two permission lists carry the same members,
// whatever their order or repetition.
func sameScopeSet(issued, requested []string) bool {
	for _, scope := range issued {
		if !slices.Contains(requested, scope) {
			return false
		}
	}
	for _, scope := range requested {
		if !slices.Contains(issued, scope) {
			return false
		}
	}
	return true
}

// scopeList renders permissions for a refusal in a stable order.
func scopeList(scopes []string) string {
	sorted := slices.Sorted(slices.Values(scopes))
	if len(sorted) == 0 {
		return "none"
	}
	return strings.Join(sorted, ", ")
}

// unavailableSource stands in for an identity kind whose derivation this build
// does not carry yet.
//
// It refuses as narrowing-unavailable rather than as an internal fault: from
// where the caller stands, a shell that cannot narrow a session to the module's
// request and a shell that will not are the same answer, and both recover by
// using an identity this release can derive access for.
type unavailableSource struct{}

func (unavailableSource) mint(Request, time.Time) (Grant, error) {
	return Grant{}, denial("auth.narrowing_unavailable",
		"this build cannot derive access for the selected identity",
		"Select a context whose identity this release can authenticate as.")
}
