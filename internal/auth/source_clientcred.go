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
	"net/url"
	"strings"
	"time"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/sdk/problem"
)

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
	// audience is the concrete audience the identity registers for this
	// namespace: what a resource indicator names, and what an issued token is
	// proved to be bound to.
	audience string
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
func (s clientCredentialsSource) mint(request Request, now time.Time) (Grant, error) {
	ctx, cancel := context.WithTimeout(context.Background(), grantDeadline)
	defer cancel()
	endpoint, err := tokenEndpoint(ctx, s.client, s.identity.Auth.Issuer)
	if err != nil {
		return Grant{}, err
	}

	// The scopes carried are the module's own request. A CI client is commonly
	// registered for every permission the deployment will ever need from
	// automation, and asking for that whole set would hand one module the
	// authority of all of them.
	form := url.Values{
		"grant_type": {"client_credentials"},
		"scope":      {strings.Join(request.Scopes, " ")},
	}
	// A deployment that decides the audience per request is told which one, and
	// the identity's registration for this product is what names it. There is
	// no earlier authorization for this grant to inherit a binding from, so the
	// indicator is the only thing that can bind the token at all. It carries the
	// registered value rather than the module's logical name because the
	// indicator has to be a resource this deployment knows.
	if s.identity.Auth.Derivation() == contexts.DerivationTokenResource {
		form.Set("resource", s.audience)
	}
	issued, err := requestToken(ctx, s.client, endpoint, form,
		clientAuth{id: s.identity.Auth.ClientID, secret: s.secret})
	if err != nil {
		return Grant{}, s.refusedGrant(err, form.Get("resource"))
	}
	facts, err := issued.verify(request, s.namespace, s.audience)
	if err != nil {
		return Grant{}, err
	}
	return Grant{Token: issued.AccessToken, ExpiresAt: issued.expiry(facts, now)}, nil
}

// refusedGrant reads why the deployment refused, and says so in the shell's own
// terms.
//
// Three answers are worth telling apart, because they send the user to three
// different places: a registration the deployment owns, a secret the job owns,
// and an issuer this shell cannot speak for.
// sent is the resource indicator this request carried, empty when it carried
// none. It is what tells the two halves of invalid_target apart.
func (s clientCredentialsSource) refusedGrant(err error, sent string) error {
	var refusal issuerRefusal
	switch {
	case errors.As(err, &refusal) && refusal.rejectedTarget() && sent != "":
		// The deployment was told which resource and does not know it. The
		// resource is a name the user chose and is not a secret, so the refusal
		// says which one was rejected rather than leaving them to guess.
		return denial("auth.narrowing_unavailable",
			fmt.Sprintf("the deployment does not recognize %q as a protected resource for the %q module",
				sent, s.namespace),
			unknownResourceRecovery)
	case errors.As(err, &refusal) && refusal.rejectedTarget():
		return denial("auth.narrowing_unavailable",
			fmt.Sprintf("the deployment will not issue access for the %q module without being told "+
				"which protected resource it is for", s.namespace),
			indicatorRecovery)
	case errors.As(err, &refusal) && refusal.refusedToNarrow():
		return denial("auth.narrowing_unavailable",
			fmt.Sprintf("the deployment refused to issue access limited to the permissions the %q "+
				"module asked for", s.namespace),
			narrowingRecovery)
	case errors.As(err, &refusal) && refusal.rejectedClient():
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
	case errors.As(err, &refusal):
		// The deployment answered, and its answer is not one the shell can
		// classify. Reporting it as a credential to correct or a registration
		// to narrow would send the user to change something that may be fine,
		// so it says only what happened.
		return denial("auth.discovery_failed",
			fmt.Sprintf("the identity provider refused to issue access for the %q module",
				s.namespace),
			"Check that this context's OAuth application is registered for the client-credentials "+
				"grant and carries the permissions the command needs, then retry.")
	default:
		return issuerUnreachable()
	}
}
