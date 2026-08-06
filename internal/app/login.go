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

package app

import (
	"context"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/wso2/wso2-cli/internal/auth/oauthflow"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// NonInteractiveEnvVar declares that nothing may prompt, open a browser, or
// wait for a human. A job that sets it wants to fail fast on a misconfigured
// identity rather than hang until its own timeout.
const NonInteractiveEnvVar = "WSO2_NON_INTERACTIVE"

// loginDeadline bounds how long a browser login waits for the user to come
// back. It is generous because a human is signing in, and it exists at all
// because without it an abandoned login waits forever holding a callback port.
var loginDeadline = 5 * time.Minute

// loginFlags are the flags wso2 login owns. It owns all of them: unlike a
// product command, there is no module to pass an unrecognized argument on to.
type loginFlags struct {
	contextName    string
	nonInteractive bool
}

// login establishes the selected context's interactive session.
//
// What it stores is a session, not a credential the user ever sees: the refresh
// token goes straight into the OS secure store, and what reaches the terminal
// is who the login proved you are and which products that identity reaches.
func (s Shell) login(args []string) error {
	flags, err := parseLoginArgs(args)
	if err != nil {
		return err
	}
	selected, err := s.selection(flags.contextName)
	if err != nil {
		return err
	}

	// The kind decides before the mode does, so a context that has no login
	// step is told so whether or not the caller asked for one interactively.
	switch selected.Identity.Auth.Kind {
	case "":
		return problem.New(problem.CategoryAuthPolicy, "auth.context_not_selected",
			"no WSO2 CLI context is selected to log in to").
			// The pointer names a document that exists today. The login
			// walkthrough is a later slice, and recovery text that sends a
			// stuck user to a missing file helps nobody.
			WithRecovery("Author a context document and select a context, then run wso2 login. " +
				"See docs/examples/authentication-contexts.md.")
	case contexts.KindClientCredentials, contexts.MethodDevelopmentCredential:
		return problem.New(problem.CategoryAuthPolicy, "auth.login_not_required",
			fmt.Sprintf("the %q context acquires access inline and has no login step",
				selected.Context.Name)).
			WithRecovery("Run the product command directly; the shell authenticates during it.")
	case contexts.KindOAuthDevice, contexts.KindPAT:
		return problem.New(problem.CategoryAuthPolicy, "auth.kind_not_implemented",
			fmt.Sprintf("the %q context uses an authentication kind this release does not implement",
				selected.Context.Name)).
			WithRecovery("Use a browser or client-credentials identity. Device and personal access " +
				"token login are planned.")
	case contexts.KindOAuthBrowser:
		// The one kind this release logs in interactively.
	default:
		return problem.New(problem.CategoryAuthPolicy, "auth.method_unsupported",
			fmt.Sprintf("the %q context uses an authentication method this shell does not implement",
				selected.Context.Name)).
			WithRecovery("Select a context with a supported authentication kind.")
	}
	if flags.nonInteractive || os.Getenv(NonInteractiveEnvVar) != "" {
		return problem.New(problem.CategoryAuthPolicy, "auth.non_interactive",
			"browser login cannot run in non-interactive mode").
			WithRecovery("Use a client-credentials identity for automation; it acquires access " +
				"inline without a login step.")
	}

	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), loginDeadline)
	defer cancel()
	result, err := oauthflow.Login{
		Issuer:      selected.Identity.Auth.Issuer,
		ClientID:    selected.Identity.Auth.ClientID,
		Scopes:      productScopeUnion(selected.Identity),
		OpenBrowser: s.OpenBrowser,
		// The authorization URL is an instruction to act on, not this
		// command's result, so it goes to the diagnostic stream: a user who
		// redirects standard output still sees the URL the login cannot
		// finish without, and the result stream carries only the report.
		Out: s.Streams.Err,
	}.Run(ctx)
	if err != nil {
		return err
	}
	// A session is a refresh token. A login that produced none cannot be stored
	// as one, and storing the access token alone would leave a session that
	// expires in minutes and cannot renew itself.
	if result.Token.RefreshToken == "" {
		return problem.New(problem.CategoryAuthPolicy, "auth.credential_unavailable",
			"the login completed without a refresh token, so no session can be stored").
			WithRecovery("Grant the registered OAuth application the offline_access scope, " +
				"then run wso2 login again.")
	}

	reference := selected.Identity.Auth.CredentialRef
	store := session.Store{StateRoot: root}
	err = store.WithLock(reference, func() error {
		return store.Save(reference, session.Session{
			Issuer:       selected.Identity.Auth.Issuer,
			RefreshToken: result.Token.RefreshToken,
			AccessToken:  result.Token.AccessToken,
			ExpiresAt:    result.Token.Expiry.UTC(),
		})
	})
	if err != nil {
		return err
	}
	return s.reportLogin(selected, result)
}

// reportLogin states who the login proved you are and what that identity
// reaches.
//
// Every value here came out of a verified identity token or the context
// document. None of it is token material, and there is deliberately nothing in
// the report a caller could authenticate with.
func (s Shell) reportLogin(selected contexts.Selection, result oauthflow.Result) error {
	if _, err := fmt.Fprintf(s.Streams.Out, "\nLogged in to the %q context.\n",
		selected.Context.Name); err != nil {
		return err
	}
	fields := [][2]string{{"Subject", result.Subject}}
	if result.Email != "" {
		fields = append(fields, [2]string{"Email", result.Email})
	}
	if selected.Context.Organization != "" {
		fields = append(fields, [2]string{"Organization", selected.Context.Organization})
	}
	fields = append(fields, [2]string{"Products", productNamespaces(selected.Identity)})
	return output.Fields(s.Streams.Out, fields)
}

// productNamespaces names the product namespaces this identity claims to reach,
// in a stable order.
func productNamespaces(identity contexts.Identity) string {
	namespaces := slices.Sorted(maps.Keys(identity.Products))
	if len(namespaces) == 0 {
		return "none configured"
	}
	return strings.Join(namespaces, ", ")
}

// productScopeUnion is every permission the identity's products declare, sorted
// and de-duplicated.
//
// The login asks for the union once, because the session it establishes is what
// a later per-product request narrows down from. Asking per product instead
// would mean one browser login per product.
func productScopeUnion(identity contexts.Identity) []string {
	var union []string
	for _, namespace := range slices.Sorted(maps.Keys(identity.Products)) {
		for _, scope := range identity.Products[namespace].Scopes {
			if !slices.Contains(union, scope) {
				union = append(union, scope)
			}
		}
	}
	slices.Sort(union)
	return union
}

// parseLoginArgs reads the flags wso2 login owns and refuses everything else.
func parseLoginArgs(args []string) (loginFlags, error) {
	var flags loginFlags
	remaining := args
	for len(remaining) > 0 {
		argument := remaining[0]
		switch {
		case argument == "--context" || strings.HasPrefix(argument, "--context="):
			name, consumed := contextFlagValue(remaining)
			if name == "" {
				return loginFlags{}, missingContextValue(loginUsageRecovery)
			}
			flags.contextName = name
			remaining = remaining[consumed:]
		case argument == "--non-interactive":
			flags.nonInteractive = true
			remaining = remaining[1:]
		case strings.HasPrefix(argument, "-"):
			return loginFlags{}, loginUsage("shell.unknown_flag",
				fmt.Sprintf("wso2 login does not take the flag %q", argument))
		default:
			return loginFlags{}, loginUsage("shell.unexpected_argument",
				fmt.Sprintf("wso2 login does not take the argument %q", argument))
		}
	}
	return flags, nil
}

// loginUsageRecovery is the way back from every wso2 login usage refusal.
const loginUsageRecovery = "Run wso2 login [--context <name>] [--non-interactive]."

func loginUsage(code, message string) problem.Problem {
	return problem.New(problem.CategoryUsage, code, message).WithRecovery(loginUsageRecovery)
}
