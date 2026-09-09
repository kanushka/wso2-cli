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

// Package auth is the shell's authentication broker.
//
// Authentication is shell policy. A module never holds a credential, never
// learns where one comes from, and never decides what access it has: it asks
// for an audience and scopes, and the shell answers from facts the module
// cannot influence — the module receipt it was installed with, the selected
// context, and the invocation in progress.
//
// The broker is created per invocation and is used from the one goroutine
// running the module session.
package auth

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// Request is a module's runtime access request.
type Request struct {
	// Audience is the protected audience the module intends to call.
	Audience string
	// Scopes are the permissions it needs.
	Scopes []string
	// Record names which record of the product the access is for: empty for
	// the product's own record, contexts.GatewayRecord for its gateway.
	Record string
}

// Grant is the access the shell issues.
//
// It is the whole of what crosses the boundary: access material and when it
// stops working. There is deliberately no third member, because every
// additional one would be something a module could use to obtain access the
// broker did not decide to give it.
type Grant struct {
	// Token is the access material to present to the audience.
	Token string
	// ExpiresAt is when the token stops being accepted.
	ExpiresAt time.Time
}

// ProofNamespace is the reserved non-production namespace this broker serves.
//
// The issuer behind it is a development fixture, so it answers for the
// architecture proof and nothing else. A product namespace reaching this
// broker is refused rather than quietly handed fixture access: whatever
// installed it, it is not what this release can authenticate.
const ProofNamespace = "reference"

// Denial is a refused access request.
//
// It is two statements of one refusal, because the module and the user are
// owed different things. The module is owed a typed failure it can return
// unchanged, and must not learn where a credential comes from. The user is
// usually owed exactly that: naming the variable to set is the difference
// between a refusal and an instruction.
type Denial struct {
	// Problem is the refusal as the module receives it. It crosses the module
	// contract, so it names no credential and no credential source.
	Problem problem.Problem
	// Guidance replaces the problem's recovery when the shell reports the
	// denial itself. It is empty when the module-safe recovery is already what
	// the user needs.
	Guidance string
}

// Error lets a denial travel as an ordinary error.
func (d Denial) Error() string { return d.Problem.Error() }

// Reported is the denial as the shell shows it to the user.
func (d Denial) Reported() problem.Problem {
	if d.Guidance == "" {
		return d.Problem
	}
	return d.Problem.WithRecovery(d.Guidance)
}

// Broker answers one invocation's access requests.
type Broker struct {
	// Namespace is the module asking, named in denials.
	Namespace string
	// Capabilities are the access requests the module receipt declares. They
	// are the ceiling: a module cannot ask at runtime for more than its
	// installation declared.
	Capabilities modules.Capabilities
	// Selection is the resolved invocation context and its identity. It names
	// the organization and the credential source, and holds no credential.
	Selection contexts.Selection
	// InvocationID is the invocation access is bound to.
	InvocationID string
	// Credentials reads a named environment variable. It defaults to the
	// process environment, and a test replaces it.
	Credentials func(name string) (string, bool)
	// StateRoot is the shell-owned state root. It hosts the advisory locks
	// that keep refresh-token rotation single-writer; no session material is
	// ever written under it.
	StateRoot string
	// HTTPClient serves issuer traffic. It defaults to http.DefaultClient,
	// and a test points it at an in-process issuer.
	HTTPClient *http.Client
	// Now reads the current time. It defaults to time.Now.
	Now func() time.Time
	// EstablishSession obtains a product's own session when a command finds
	// none, and again when the issuer will not renew the one it has. The shell
	// supplies it with the login flow; nil refuses with auth.session_required.
	// It is never asked for the login session itself: that is wso2 login's, and
	// a command that finds none is told to run it.
	//
	// A hook that may not open a browser returns BrowserUnavailable rather than
	// a refusal of its own, and this package states which of the two cases it
	// was asked about.
	EstablishSession func(access contexts.ProductAccess) error

	// granted records that this invocation already has access, so the module
	// cannot come back for more.
	granted bool
}

// Acquire applies broker policy to one request and issues access or refuses it.
//
// Every refusal is a typed problem in the authentication class, with recovery
// guidance a user can act on and no detail of the credential behind it.
func (b *Broker) Acquire(request Request) (Grant, error) {
	if b.granted {
		return Grant{}, denial("auth.already_granted",
			fmt.Sprintf("the %q module asked for access twice in one command", b.namespace()),
			"Retry the command. A module is granted access once per command and cannot renew it.")
	}
	// The record is checked against the descriptor before anything else: a
	// module may ask only for a record its installation declared.
	if err := b.checkRecord(request); err != nil {
		return Grant{}, err
	}
	// A request naming no scopes asks for the record's recorded scopes: the
	// permissions the identity's product entry already consents to, which
	// is what a module calling the user's own API through a product cannot
	// know in advance. The record stays the ceiling either way.
	if len(request.Scopes) == 0 {
		_, scopes := b.recorded(request)
		request.Scopes = slices.Clone(scopes)
	}
	if err := b.checkDeclared(request); err != nil {
		return Grant{}, err
	}
	// The receipt is checked before the identity is, so a module asking beyond
	// its installation is told so whatever context happens to be selected.
	resolved, err := b.resolveSource(request)
	if err != nil {
		return Grant{}, asDenial(err)
	}
	grant, err := resolved.mint(request, b.now())
	if err != nil {
		return Grant{}, asDenial(err)
	}

	b.granted = true
	return grant, nil
}

// checkDeclared intersects the request with the module receipt.
//
// An undeclared audience or scope is refused rather than narrowed away,
// because a module that silently receives less than it asked for would proceed
// believing it holds access it does not.
func (b *Broker) checkDeclared(request Request) error {
	if request.Audience == "" || !slices.Contains(b.Capabilities.AuthAudiences, request.Audience) {
		return denial("auth.audience_not_declared",
			fmt.Sprintf("the %q module asked for access its installation does not declare", b.namespace()),
			"Reinstall the module. The shell grants only the access a module receipt declares.")
	}
	// A scope is declared by the module receipt, or by the user: the product
	// entry an identity records for this namespace lists the permissions the
	// shell may request for it, and a module that calls the user's own API
	// through a product (a gateway, say) cannot know those in advance. Either
	// party naming the scope for this namespace is consent; a scope neither
	// named is refused.
	_, recorded := b.recorded(request)
	for _, scope := range request.Scopes {
		if !slices.Contains(b.Capabilities.AuthScopes, scope) && !slices.Contains(recorded, scope) {
			return denial("auth.scope_not_declared",
				fmt.Sprintf("the %q module asked for a permission neither its installation nor the "+
					"identity's product entry declares", b.namespace()),
				"Reinstall the module, or record the permission on this identity's product entry "+
					"with wso2 account add-product --replace. The shell grants only the permissions "+
					"a module receipt or the product entry declares.")
		}
	}
	return nil
}

// checkRecord refuses a request for a record the module's descriptor does
// not declare. The product's own record is what every module has; a gateway
// record exists only when the descriptor carries a gateway block.
func (b *Broker) checkRecord(request Request) error {
	switch request.Record {
	case "":
		return nil
	case contexts.GatewayRecord:
		if b.Capabilities.Product != nil && b.Capabilities.Product.Gateway != nil {
			return nil
		}
		return denial("auth.product_not_configured",
			fmt.Sprintf("the %q module asked for its gateway record, which its descriptor does not declare",
				b.namespace()),
			"Install a version of the module whose descriptor declares a gateway. The shell grants "+
				"only the records a module receipt declares.")
	default:
		return denial("auth.product_not_configured",
			fmt.Sprintf("the %q module asked for a %q record, which no product has", b.namespace(), request.Record),
			"Reinstall the module. A product has its own record and, when its descriptor declares one, "+
				"a gateway record.")
	}
}

// recordKey is the name Identity.Access resolves the request's record by:
// the namespace, or the product's gateway key.
func (b *Broker) recordKey(request Request) string {
	if request.Record == contexts.GatewayRecord {
		return contexts.GatewayKey(b.Namespace)
	}
	return b.Namespace
}

// recorded is what the identity's entry records for the request's record:
// its audience and its scopes. Both empty for a record the identity does not
// hold, which checkProduct refuses by name.
func (b *Broker) recorded(request Request) (string, []string) {
	product := b.Selection.Identity.Products[b.Namespace]
	if request.Record == contexts.GatewayRecord {
		if product.Gateway == nil {
			return "", nil
		}
		return product.Gateway.Audience, product.Gateway.Scopes
	}
	return product.Audience, product.Scopes
}

// credential reads the source credential the development context names.
func (b *Broker) credential() (string, error) {
	return b.namedSecret(b.Selection.Identity.Auth.CredentialVariable, "the credential")
}

// namedSecret reads the environment variable an account names, into process
// memory and nowhere else.
//
// It is the one door a secret comes through, so the shape of its refusal is
// decided here for every kind that uses one. The value is never written to
// state, passed to the module, or included in a problem. Neither is the name
// of the variable holding it, which is the module's own answer to "where would
// I look?" and therefore travels only to the user — as guidance, on the side of
// the refusal the module never sees.
//
// description says what the variable holds, in the user's terms; it is the
// difference between "set this" and an instruction someone can follow.
func (b *Broker) namedSecret(variable, description string) (string, error) {
	if variable == "" {
		return "", denial("auth.credential_unavailable",
			fmt.Sprintf("the %q context names no credential source", b.Selection.Context.Name),
			fmt.Sprintf("Select a context that names the environment variable holding %s.",
				description))
	}
	lookup := b.Credentials
	if lookup == nil {
		lookup = os.LookupEnv
	}
	value, present := lookup(variable)
	// A variable set to whitespace is treated as unset. A CI job that exports
	// an empty secret has the same problem as one that exports none, and
	// sending the blank to the issuer would report it as a rejected credential
	// instead of a missing one.
	if !present || strings.TrimSpace(value) == "" {
		return "", Denial{
			Problem: problem.New(problem.CategoryAuthPolicy, "auth.credential_unavailable",
				fmt.Sprintf("the credential source the %q context names is not set", b.Selection.Context.Name)).
				WithRecovery("Set the credential source this context names, then retry the command."),
			Guidance: fmt.Sprintf("Set %s to %s for this context, then retry the command.",
				variable, description),
		}
	}
	return value, nil
}

func (b *Broker) now() time.Time {
	if b.Now == nil {
		return time.Now().UTC()
	}
	return b.Now().UTC()
}

func (b *Broker) namespace() string {
	if b.Namespace == "" {
		return "product"
	}
	return b.Namespace
}

// asDenial restates any typed problem a source raised as a broker denial.
//
// A source may borrow a problem from a package that knows nothing about this
// broker — the session store's own auth.login_required, for one. Everything
// Acquire refuses with is a Denial, so one type answers for every refusal and
// the shell has a single place to decide what a module is told versus what the
// user is told.
func asDenial(err error) error {
	var refusal Denial
	if errors.As(err, &refusal) {
		return refusal
	}
	var typed problem.Problem
	if errors.As(err, &typed) {
		return Denial{Problem: typed}
	}
	return err
}

// BrowserUnavailable is what an EstablishSession hook returns when this
// invocation may not authorize a product, because nothing may open a browser
// or wait for a person. Control names what asked for that — the shell's
// --no-input flag or the environment variable behind it — and is public.
//
// It is a marker and not a refusal on its own, deliberately. Whether a product
// cannot be authorized is the shell's to decide, but what that means for the
// user depends on something only this package knows: whether a session for the
// product is already stored, and if it is, why it could not serve the request.
// The shell has neither fact, so a hook that built the message itself could
// only ever write one of them, and wrote the wrong one whenever a stored
// session was the thing that failed. Every path that calls the hook restates
// this into a Denial that says which case it was; see sessionSource.
type BrowserUnavailable struct {
	// Control names the flag or environment variable that asked that nothing
	// prompt. It reaches the user in guidance, so it is a name, never a value.
	Control string
}

// Error lets the marker travel as an ordinary error. It is never what a user
// reads: every caller restates it, and this text exists for a log line or a
// wrapped error a test prints.
func (b BrowserUnavailable) Error() string {
	return fmt.Sprintf("no browser may be opened in this invocation (%s)", b.Control)
}

// SessionRequired refuses a record whose own session is absent and cannot
// be established in this invocation. record is the key the session is
// reported under, a namespace or a gateway key; product is the namespace the
// login is narrowed to, which establishes every record of the product.
func SessionRequired(record, product string) Denial {
	return denial("auth.session_required",
		fmt.Sprintf("the %q product has no session under this identity yet", record),
		fmt.Sprintf("Run wso2 login --only %s to authorize it, or wso2 login to authorize every product.",
			product))
}

// denial reports a broker refusal the module and the user can both be told in
// full. Every refusal is in the authentication class, so automation can tell an
// access failure from a product failure by exit code alone.
func denial(code, message, recovery string) Denial {
	return Denial{
		Problem: problem.New(problem.CategoryAuthPolicy, code, message).WithRecovery(recovery),
	}
}
