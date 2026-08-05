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
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// inlineDeadline bounds one inline acquisition: discovery plus the grant.
// An automated job has no one to notice it hanging, so an issuer that does not
// answer ends the command instead of holding the runner.
const inlineDeadline = 30 * time.Second

// clientCredentialsSource obtains access for one command, inline, from a client
// secret the environment already holds.
//
// There is no login step and nothing to store. The credential is on the machine
// before the command starts, so a stored session would be a second copy of an
// authority the job already has — one the shell would then be responsible for
// rotating and revoking. The secret is read into process memory, spent on a
// single grant, and never written to the state root, the OS secure store, or
// the module's environment.
//
// What the module receives is what the browser source hands it: a token the
// deployment minted, proved to carry exactly the permissions asked for and to
// be bound to the audience asked for. The derivation differs because CI has no
// browser; the guarantee does not, because a module cannot tell which kind of
// context it was invoked under and must not need to.
type clientCredentialsSource struct {
	// namespace is the module asking, named in refusals.
	namespace string
	// contextName is the selected context, named in refusals.
	contextName string
	// identity names the issuer and the OAuth client to present. It holds no
	// credential.
	identity contexts.Identity
	// secret is the client secret, in process memory for the length of one
	// grant.
	secret string
	// secretVariable is where the secret came from. It reaches the user and
	// never the module: the name is the answer to "where is the credential?".
	secretVariable string
	// client serves the issuer traffic.
	client *http.Client
}

// mint runs one client-credentials grant and verifies what it issued.
func (s clientCredentialsSource) mint(request Request, _ time.Time) (Grant, error) {
	ctx, cancel := context.WithTimeout(context.Background(), inlineDeadline)
	defer cancel()
	endpoint, err := tokenEndpoint(ctx, s.client, s.identity.Auth.Issuer)
	if err != nil {
		return Grant{}, err
	}

	grant := clientcredentials.Config{
		ClientID:     s.identity.Auth.ClientID,
		ClientSecret: s.secret,
		TokenURL:     endpoint,
		// The scopes carried are the module's own request. A client is
		// commonly registered for every permission the deployment will ever
		// need from CI, and asking for that whole set would hand one module
		// the authority of all of them.
		Scopes: request.Scopes,
	}
	// The client is passed through the context because that is the only way
	// oauth2 accepts one. It is the shell's client, so a deployment's proxy and
	// certificate configuration applies here as it does to every other request.
	issued, err := grant.Token(context.WithValue(ctx, oauth2.HTTPClient, s.client))
	if err != nil {
		return Grant{}, s.refusedGrant(err)
	}

	// The response's scope member, when the deployment states one, is its own
	// account of what it issued; verify prefers it over the token's claim and
	// falls back to the claim when it is absent.
	stated, _ := issued.Extra("scope").(string)
	answer := tokenResponse{AccessToken: issued.AccessToken, Scope: stated}
	facts, err := answer.verify(request, s.namespace)
	if err != nil {
		return Grant{}, err
	}
	// The library has already turned the response's lifetime into an instant.
	// The token's own claim answers for a deployment that stated no lifetime.
	expiresAt := issued.Expiry.UTC()
	if expiresAt.IsZero() {
		expiresAt = facts.ExpiresAt
	}
	return Grant{Token: issued.AccessToken, ExpiresAt: expiresAt}, nil
}

// refusedGrant reads why the deployment refused, and says so in the shell's own
// terms.
//
// Two answers are worth telling apart, because they send the user to different
// places. A refusal to narrow is a registration the deployment owns; a rejected
// credential is a secret the job owns. Everything else is an issuer this shell
// cannot speak for, and is reported as one rather than guessed at.
func (s clientCredentialsSource) refusedGrant(err error) error {
	var refusal *oauth2.RetrieveError
	if !errors.As(err, &refusal) || refusal.Response == nil {
		// No answer the shell can read: a transport failure, a timeout, or a
		// body that was not a token response. The library's own error is not
		// carried through — it may quote the request that produced it.
		return inlineUnreachable()
	}
	switch {
	case refusal.Response.StatusCode == http.StatusBadRequest && refusal.ErrorCode == "invalid_scope":
		return denial("auth.narrowing_unavailable",
			fmt.Sprintf("the deployment refused to issue access limited to the permissions the %q "+
				"module asked for", s.namespace),
			narrowingRecovery)
	case refusal.Response.StatusCode == http.StatusUnauthorized || refusal.ErrorCode == "invalid_client":
		// A rotated or mistyped secret. It is never reported as a login to
		// run: wso2 login refuses this kind of identity outright, so pointing
		// there would leave the user with nothing to do.
		return Denial{
			Problem: problem.New(problem.CategoryAuthPolicy, "auth.credential_unavailable",
				fmt.Sprintf("the identity provider did not accept the client credential the %q "+
					"context names", s.contextName)).
				WithRecovery("Set the credential source this context names to a credential the " +
					"deployment currently accepts, then retry the command."),
			Guidance: fmt.Sprintf("The identity provider rejected the client secret in %s. Set it "+
				"to a current secret for this context's OAuth client, then retry the command.",
				s.secretVariable),
		}
	default:
		return inlineUnreachable()
	}
}

// inlineUnreachable reports an identity provider that did not complete the
// grant.
//
// It is deliberately distinct from the discovery failure: by this point the
// issuer's configuration has already been read, so telling the user the shell
// could not read it would send them to look at something that worked.
func inlineUnreachable() error {
	return denial("auth.discovery_failed",
		"the shell could not obtain access from the identity provider for this command",
		"Check that this machine can reach the issuer of the selected context, then retry.")
}
