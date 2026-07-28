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
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/wso2/wso2-cli/internal/auth/devtoken"
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
	// Context is the selected invocation context. It names the organization
	// and the credential source, and holds no credential.
	Context contexts.Context
	// InvocationID is the invocation access is bound to.
	InvocationID string
	// Credentials reads a named environment variable. It defaults to the
	// process environment, and a test replaces it.
	Credentials func(name string) (string, bool)
	// Now reads the current time. It defaults to time.Now.
	Now func() time.Time

	// granted records that this invocation already has access, so the module
	// cannot come back for more.
	granted bool
}

// Acquire applies broker policy to one request and issues access or refuses it.
//
// Every refusal is a typed problem in the authentication class, with recovery
// guidance a user can act on and no detail of the credential behind it.
func (b *Broker) Acquire(request Request) (Grant, error) {
	if b.Namespace != ProofNamespace {
		return Grant{}, denial("auth.namespace_not_brokered",
			fmt.Sprintf("the %q module asked for access, and this shell brokers access for the "+
				"non-production %q proof only", b.namespace(), ProofNamespace),
			"Install a module the WSO2 CLI can authenticate, or run the command without it.")
	}
	if b.granted {
		return Grant{}, denial("auth.already_granted",
			fmt.Sprintf("the %q module asked for access twice in one command", b.namespace()),
			"Retry the command. A module is granted access once per command and cannot renew it.")
	}
	if err := b.checkDeclared(request); err != nil {
		return Grant{}, err
	}
	if err := b.checkContext(); err != nil {
		return Grant{}, err
	}

	credential, err := b.credential()
	if err != nil {
		return Grant{}, err
	}

	now := b.now()
	token, mintErr := devtoken.Mint(credential, devtoken.Claims{
		Audience:     request.Audience,
		Scopes:       request.Scopes,
		Organization: b.Context.OrganizationID,
		Invocation:   b.InvocationID,
	}, now)
	if mintErr != nil {
		// The issuer's own error may name what it was given, so it is not
		// carried into a problem the shell renders.
		return Grant{}, denial("auth.access_not_issued",
			fmt.Sprintf("the shell could not issue access for the %q module", b.namespace()),
			"Retry the command. Report the failure if it persists.")
	}

	b.granted = true
	return Grant{Token: token, ExpiresAt: now.Add(devtoken.Lifetime).UTC()}, nil
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
	for _, scope := range request.Scopes {
		if !slices.Contains(b.Capabilities.AuthScopes, scope) {
			return denial("auth.scope_not_declared",
				fmt.Sprintf("the %q module asked for a permission its installation does not declare",
					b.namespace()),
				"Reinstall the module. The shell grants only the permissions a module receipt declares.")
		}
	}
	return nil
}

// checkContext proves the selected context can be authenticated against at all.
func (b *Broker) checkContext() error {
	if b.Context.Auth.Method == "" && b.Context.Auth.CredentialVariable == "" {
		return denial("auth.context_not_selected",
			fmt.Sprintf("the %q module needs access, and no WSO2 CLI context is selected", b.namespace()),
			"Select a context that names the organization and credential source to use.")
	}
	if b.Context.Auth.Method != contexts.MethodDevelopmentCredential {
		return denial("auth.method_unsupported",
			fmt.Sprintf("the %q context uses an authentication method this shell does not implement",
				b.Context.Name),
			fmt.Sprintf("Select a context whose authentication method is %q.",
				contexts.MethodDevelopmentCredential))
	}
	if b.Context.OrganizationID == "" {
		return denial("auth.organization_not_selected",
			fmt.Sprintf("the %q context names no organization to act within", b.Context.Name),
			"Select a context that names the organization the command targets.")
	}
	return nil
}

// credential reads the source credential the context names.
//
// The value stays in this process: it is the issuer's signing key and is never
// written to state, passed to the module, or included in a problem. Neither is
// the name of the variable holding it, which is the module's own answer to
// "where would I look?" and therefore travels only to the user.
func (b *Broker) credential() (string, error) {
	name := b.Context.Auth.CredentialVariable
	if name == "" {
		return "", denial("auth.credential_unavailable",
			fmt.Sprintf("the %q context names no credential source", b.Context.Name),
			"Select a context that names the environment variable holding the credential.")
	}
	lookup := b.Credentials
	if lookup == nil {
		lookup = os.LookupEnv
	}
	value, present := lookup(name)
	if !present || strings.TrimSpace(value) == "" {
		return "", Denial{
			Problem: problem.New(problem.CategoryAuthPolicy, "auth.credential_unavailable",
				fmt.Sprintf("the credential source the %q context names is not set", b.Context.Name)).
				WithRecovery("Set the credential source this context names, then retry the command."),
			Guidance: fmt.Sprintf("Set %s to the credential for this context, then retry the command.", name),
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

// denial reports a broker refusal the module and the user can both be told in
// full. Every refusal is in the authentication class, so automation can tell an
// access failure from a product failure by exit code alone.
func denial(code, message, recovery string) Denial {
	return Denial{
		Problem: problem.New(problem.CategoryAuthPolicy, code, message).WithRecovery(recovery),
	}
}
