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
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/auth/oauthflow"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// NoInputEnvVar declares that nothing may prompt, open a browser, or wait for a
// human. A job that sets it wants to fail fast on a misconfigured identity
// rather than hang until its own timeout.
//
// It is named for the flag it stands in for. #112 D12 renamed --non-interactive
// to --no-input, the spelling the architecture already used, on the grounds
// that one flag carrying both meanings beats two adjacent spellings for one
// idea; the variable follows so a reader who knows one knows the other.
const NoInputEnvVar = "WSO2_NO_INPUT"

// loginDeadline bounds how long a browser login waits for the user to come
// back. It is generous because a human is signing in, and it exists at all
// because without it an abandoned login waits forever holding a callback port.
var loginDeadline = 5 * time.Minute

// deviceLoginDeadline is the same bound for a device login, and is longer for a
// plain reason: the user has to reach a second device before they can even
// begin. It is a ceiling and rarely the thing that fires — the deployment
// publishes its own device code lifetime, which the flow honours and which is
// usually shorter.
var deviceLoginDeadline = 15 * time.Minute

// loginFlags are the flags wso2 login acts on. --context is the shell's own,
// read off the root's flag set; the other three are declared by loginCommand.
type loginFlags struct {
	// contextName selects the context to log in to, and on the creating path
	// names both the identity and the context that login creates. One flag
	// answers both because it answers one question — which context this login
	// is about — and #112 D6 decides it that way.
	contextName string
	// issuer is --url, and its presence is what turns the creating path on. A
	// login without it selects a configured context and behaves exactly as it
	// did before this command could write anything (#112 D5).
	issuer string
	// clientID is --client-id: the OAuth application the operator registered.
	// No WSO2-published client exists for a self-hosted deployment, so the
	// shell cannot invent one.
	clientID string
	noInput  bool
	// only is --only <namespace>: authorize this one product's access instead
	// of every access the identity records.
	only string
	// noProducts is --no-products: authorize the login session alone, and no
	// product beside it.
	noProducts bool
}

// login establishes the selected context's interactive session.
//
// What it stores is a session, not a credential the user ever sees: the refresh
// token goes straight into the OS secure store, and what reaches the terminal
// is who the login proved you are and which products that identity reaches.
func (s Shell) login(flags loginFlags) error {
	if flags.only != "" && flags.noProducts {
		// Each asks for a different subset of one identity's accesses, and
		// together they ask for two different subsets at once, which is not a
		// request this login can honour. Checked ahead of both paths below,
		// creating and configured alike: the conflict is in the flags
		// themselves, not in what they would otherwise do, and the creating
		// path must never open a browser to reach a refusal that needed no
		// issuer at all.
		return problem.New(problem.CategoryUsage, "shell.conflicting_arguments",
			"--only and --no-products both narrow which sessions this login establishes").
			WithRecovery("Pass --only <namespace> to authorize one product, " +
				"or --no-products to authorize the login session alone, not both.")
	}
	if flags.issuer != "" {
		return s.loginCreating(flags)
	}
	if flags.clientID != "" {
		// Without --url there is no issuer for this client to be registered
		// with, so the flag has nothing to act on. Refused rather than ignored:
		// the selection that follows reports a missing context document and
		// tells the user to author one, which is advice about the wrong problem
		// and the instruction #112 exists to delete.
		return problem.New(problem.CategoryUsage, "shell.missing_required_flag",
			"wso2 login takes --client-id only with --url, which names the issuer the client is registered with").
			WithRecovery(loginUsageRecovery)
	}
	selected, err := s.selection(flags.contextName)
	if err != nil {
		return err
	}
	outcome, err := s.establishAndStore(selected, flags)
	if err != nil {
		return err
	}
	return s.reportLogin(selected, outcome)
}

// loginOutcome is what one wso2 login established: the first authorization's
// verified identity and every access it stored a session for.
//
// It exists because Shell's methods take a value receiver: a login that
// establishes several accesses cannot leave the later ones on s for login to
// read back, so it returns them instead.
type loginOutcome struct {
	first       oauthflow.Result
	established []contexts.ProductAccess
}

// establishAndStore runs every authorization this login is asked for and
// stores the session each one produced.
//
// Both login paths share it. A login that created its identity and a login
// against a configured one differ in what reaches the context document and in
// nothing else, and a second copy of these gates would be a second place for
// them to drift.
func (s Shell) establishAndStore(selected contexts.Selection, flags loginFlags) (loginOutcome, error) {
	// The kind decides before the mode does, so a context that has no login
	// step is told so whether or not the caller asked for one interactively.
	if err := loginKindGate.check(selected); err != nil {
		return loginOutcome{}, err
	}
	// nonInteractiveControl is the same check resolveClientID's mayPrompt
	// consults, kept to one implementation (prompt.go) so a login's browser
	// gate and a text prompt's gate cannot drift into two different answers to
	// "did --no-input or WSO2_NO_INPUT ask for this". This gate stops short of
	// mayPrompt's terminal check: a device login waits on a person without
	// ever reading this process's own stdin, so whether that descriptor is a
	// terminal is not what decides it.
	if control := s.nonInteractiveControl(flags.noInput); control != "" {
		// Named for the mode actually refused. Both are interactive and both
		// are wrong in CI, but telling a device login it is a browser login
		// sends the reader looking for a browser that was never involved.
		mode := "browser login"
		if selected.Identity.Auth.Kind == contexts.KindOAuthDevice {
			mode = "device login"
		}
		return loginOutcome{}, problem.New(problem.CategoryAuthPolicy, "auth.non_interactive",
			mode+" cannot run in non-interactive mode, which "+control+" asked for").
			WithRecovery("Use a client-credentials identity for automation; it acquires access " +
				"inline without a login step.")
	}

	accesses, err := s.loginAccesses(selected, flags)
	if err != nil {
		return loginOutcome{}, err
	}
	var outcome loginOutcome
	for index, access := range accesses {
		result, err := s.establishProduct(selected, access)
		if err != nil {
			// The first access failing leaves nothing established yet, so
			// there is nothing to report beside the error itself. A later
			// access failing leaves the earlier ones' sessions stored and
			// unmentioned by that error, which is the gap this closes.
			if index > 0 {
				s.reportPartialLogin(outcome.established, access)
				err = extendIncompleteLoginRecovery(err, outcome.established, access)
			}
			return loginOutcome{}, err
		}
		if index == 0 {
			outcome.first = result
		}
		outcome.established = append(outcome.established, access)
	}
	return outcome, nil
}

// establishedLabel names one authorized access for a user-facing message: its
// namespace, or "the login session" for the bare access a namespace-less
// identity gets.
func establishedLabel(access contexts.ProductAccess) string {
	if access.Namespace == "" {
		return "the login session"
	}
	return access.Namespace
}

// establishedLabels is establishedLabel applied to every access already
// authorized, in the order they were established.
func establishedLabels(established []contexts.ProductAccess) []string {
	labels := make([]string, len(established))
	for i, access := range established {
		labels[i] = establishedLabel(access)
	}
	return labels
}

// reportPartialLogin prints what a login already established before a later
// access failed, on the diagnostic stream, so the partial state is visible
// even when the error itself renders tersely.
func (s Shell) reportPartialLogin(established []contexts.ProductAccess, failed contexts.ProductAccess) {
	fmt.Fprintf(s.Streams.Err, "Established: %s. Not established: %s.\n",
		strings.Join(establishedLabels(established), ", "), failed.Namespace)
}

// extendIncompleteLoginRecovery tells the user, on a mid-login failure, which
// sessions the earlier accesses already established and how to retry only the
// one that failed, keeping those sessions rather than repeating them.
//
// Only a problem.Problem carries recovery text a user reads, so only a
// problem.Problem gets this treatment; any other error passes through
// unchanged, and the caller has nothing further to add to it.
func extendIncompleteLoginRecovery(err error, established []contexts.ProductAccess, failed contexts.ProductAccess) error {
	var reported problem.Problem
	if !errors.As(err, &reported) {
		return err
	}
	sentence := fmt.Sprintf("Already established: %s.", strings.Join(establishedLabels(established), ", "))
	resume := fmt.Sprintf("Run wso2 login --only %s to retry just that product; "+
		"the sessions already established are kept.", failed.Namespace)
	recovery := sentence + " " + resume
	if reported.Recovery != "" {
		recovery = reported.Recovery + " " + recovery
	}
	return reported.WithRecovery(recovery)
}

// loginAccesses is what this login authorizes: every session by default, the
// login session alone under --no-products, one product under --only — both
// of its records, or one of them when named by key.
func (s Shell) loginAccesses(selected contexts.Selection, flags loginFlags) ([]contexts.ProductAccess, error) {
	switch {
	case flags.only != "":
		access, recorded := selected.Identity.Access(flags.only)
		if !recorded {
			return nil, problem.New(problem.CategoryUsage, "shell.invalid_argument",
				fmt.Sprintf("the %q identity records no %q product to authorize",
					selected.Identity.Name, flags.only)).
				WithRecovery("Name a product the identity records, or one record of it as " +
					"<namespace>/gateway; wso2 identity list shows them.")
		}
		accesses := []contexts.ProductAccess{access}
		// A product namespace names the whole product: its own record and its
		// gateway record, when it has one. A gateway key names that record
		// alone.
		if gateway, recorded := selected.Identity.Access(contexts.GatewayKey(flags.only)); recorded {
			accesses = append(accesses, gateway)
		}
		return accesses, nil
	case flags.noProducts:
		if err := checkLoginAccessBinds(selected.Identity); err != nil {
			return nil, err
		}
		return []contexts.ProductAccess{selected.Identity.LoginAccess()}, nil
	default:
		if err := checkLoginAccessBinds(selected.Identity); err != nil {
			return nil, err
		}
		return selected.Identity.Accesses(), nil
	}
}

// checkLoginAccessBinds refuses a login whose first authorization has nothing
// to bind to on a deployment that requires it.
//
// An interactive identity whose products are all reached through a grant
// records no direct product at all, so LoginAccess names no namespace and
// binds no resource. On a scoped-refresh deployment that authorization is
// still legal: it is the bare session every module narrows from. On a
// deployment that binds a login to one protected resource, RFC 8707 gives it
// nothing to name, and sending the authorization anyway would ask an issuer
// that requires a resource indicator for one this identity cannot supply.
func checkLoginAccessBinds(identity contexts.Identity) error {
	if identity.Auth.Derivation() != contexts.DerivationTokenResource ||
		len(identity.Products) == 0 || identity.LoginAccess().Namespace != "" {
		return nil
	}
	return problem.New(problem.CategoryAuthPolicy, "auth.product_not_configured",
		fmt.Sprintf("the %q identity records no product its login can bind to; every product it "+
			"records is reached by a grant", identity.Name)).
		WithRecovery("Record a direct product with wso2 identity add-product, then run wso2 login.")
}

// checkDerivedResource refuses to open a browser for a derived access this
// deployment could never carry out: a jwt-bearer grant's assertion session
// runs at the identity's own issuer, and on a deployment that binds access by
// resource that session needs one exactly as the login session does. A
// document written before this was required still decodes — see
// contexts.Identity.validateDerivation — so the refusal belongs here, at the
// one place that actually needs the resource, rather than at document load.
func checkDerivedResource(identity contexts.Identity, access contexts.ProductAccess) error {
	if access.Strategy != contexts.StrategyDerived || access.Resource != "" ||
		identity.Auth.Derivation() != contexts.DerivationTokenResource {
		return nil
	}
	return problem.New(problem.CategoryAuthPolicy, "auth.product_not_configured",
		fmt.Sprintf("the %q product's jwt-bearer grant names no resource for its assertion "+
			"session, which this deployment binds access by", access.Namespace)).
		WithRecovery(fmt.Sprintf("Record the resource with wso2 identity add-product --replace "+
			"--grant-resource <uri>, then run wso2 login --only %s.", access.Namespace))
}

// establishProduct runs one authorization at access's issuer, as its client,
// for its scopes and resource, and stores the session it produced under its
// session reference.
//
// It is what Accesses' every entry runs through, whether that entry is the
// login session or a further product: each is one authorization and one
// stored session, and nothing here needs to know which.
func (s Shell) establishProduct(selected contexts.Selection, access contexts.ProductAccess) (oauthflow.Result, error) {
	if err := checkDerivedResource(selected.Identity, access); err != nil {
		return oauthflow.Result{}, err
	}
	root, err := s.stateRoot()
	if err != nil {
		return oauthflow.Result{}, err
	}
	// Recorded before the flow starts, because the commonest login failure is a
	// deployment that answers a request the user cannot see. Who the issuer is,
	// which grant was chosen, and which application asked are the facts that
	// make a refusal from that issuer readable. The client identifier is public
	// by definition and the scopes are the identity document's, so nothing here
	// is credential material.
	s.log.Debug("starting a login",
		"context", selected.Context.Name,
		"product", access.Namespace,
		"strategy", access.Strategy,
		"grant_kind", selected.Identity.Auth.Kind,
		"issuer", access.Issuer,
		"client_id", access.ClientID,
		"scopes", strings.Join(access.Scopes, " "),
		"resource", access.Resource)
	result, err := s.establishSession(selected, access)
	if err != nil {
		return oauthflow.Result{}, err
	}
	// A session is a refresh token. A login that produced none cannot be stored
	// as one, and storing the access token alone would leave a session that
	// expires in minutes and cannot renew itself.
	if result.Token.RefreshToken == "" {
		return oauthflow.Result{}, problem.New(problem.CategoryAuthPolicy, "auth.credential_unavailable",
			"the login completed without a refresh token, so no session can be stored").
			WithRecovery("Grant the registered OAuth application the offline_access scope, " +
				"then run wso2 login again.")
	}

	// The token itself is never logged, here or anywhere: what a reader needs
	// is that the issuer answered and when the access it granted runs out.
	s.log.Debug("the login completed",
		"subject", result.Subject,
		"access_expires_at", result.Token.Expiry.UTC().Format(time.RFC3339),
		"credential_ref", access.SessionRef)

	store := session.Store{StateRoot: root}
	// The refresh token's own lifetime, when the token response discloses one
	// as refresh_token_expires_in, read through the same rule
	// internal/auth/narrowing.go's tokenResponse applies to the identical
	// member on the rotation path, so the two paths cannot drift apart on
	// what counts as stated. Any other shape (including absent) leaves this
	// at the zero value, which R7 (#112) treats as the expected case rather
	// than a reason to invent one.
	sessionExpiresAt := time.Time{}
	if seconds, ok := auth.LifetimeSeconds(result.Token.Extra("refresh_token_expires_in")); ok {
		sessionExpiresAt = time.Now().Add(time.Duration(seconds) * time.Second).UTC()
	}
	err = store.WithLock(access.SessionRef, func() error {
		return store.Save(access.SessionRef, session.Session{
			Issuer:           access.Issuer,
			RefreshToken:     result.Token.RefreshToken,
			AccessToken:      result.Token.AccessToken,
			ExpiresAt:        result.Token.Expiry.UTC(),
			Subject:          result.Subject,
			IDToken:          result.IDToken,
			SessionExpiresAt: sessionExpiresAt,
			Strategy:         access.Strategy,
			ClientID:         access.ClientID,
			Scopes:           access.Scopes,
		})
	})
	if err != nil {
		return oauthflow.Result{}, err
	}
	return result, nil
}

// establishSession runs the login mode the selected identity's kind names, for
// one access.
//
// The two modes differ in how a person proves who they are and in nothing else:
// each returns the same result, and each is given the diagnostic stream to
// print on. What they print is an instruction to act on, not this command's
// result, so a user who redirects standard output still sees the URL or the
// code the login cannot finish without, and the result stream carries only the
// report.
func (s Shell) establishSession(selected contexts.Selection, access contexts.ProductAccess) (oauthflow.Result, error) {
	if selected.Identity.Auth.Kind == contexts.KindOAuthDevice {
		// A longer deadline than the browser login's, because a longer errand:
		// the person has to reach another device, open a browser on it, and
		// type a code, where a browser login's user is already looking at the
		// page. The deployment's own code lifetime bounds this further, and
		// almost always to something shorter.
		ctx, cancel := context.WithTimeout(context.Background(), deviceLoginDeadline)
		defer cancel()
		// No resource indicator here, and that is not an oversight. The only
		// deployment this shell knows that decides the audience at
		// authorization time is Thunder, and Thunder registers no device grant
		// at all — so a device identity against such a deployment is refused at
		// discovery, before an indicator could matter. A deployment that
		// requires one and offers a device grant would need this branch to
		// carry it too.
		return oauthflow.DeviceLogin{
			Issuer:   access.Issuer,
			ClientID: access.ClientID,
			Scopes:   access.Scopes,
			Out:      s.Streams.Err,
		}.Run(ctx)
	}
	ctx, cancel := context.WithTimeout(context.Background(), loginDeadline)
	defer cancel()
	return oauthflow.Login{
		Issuer:      access.Issuer,
		ClientID:    access.ClientID,
		Scopes:      access.Scopes,
		Resource:    access.Resource,
		Label:       access.Namespace,
		OpenBrowser: s.OpenBrowser,
		Out:         s.Streams.Err,
	}.Run(ctx)
}

// reportLogin states who the login proved you are and what that identity
// reaches.
//
// Every value here came out of a verified identity token or the context
// document. None of it is token material, and there is deliberately nothing in
// the report a caller could authenticate with.
func (s Shell) reportLogin(selected contexts.Selection, outcome loginOutcome) error {
	if _, err := fmt.Fprintf(s.Streams.Out, "\nLogged in to the %q context.\n",
		selected.Context.Name); err != nil {
		return err
	}
	var fields [][2]string
	// Both are reported only when the login actually verified them. A browser
	// login always has a subject, because it refuses without a verified
	// identity token; a device login may not, because RFC 8628's grant is not
	// defined to carry one and the session does not depend on it. An empty
	// label would claim the shell knows something it does not.
	if outcome.first.Subject != "" {
		fields = append(fields, [2]string{"Subject", outcome.first.Subject})
	}
	if outcome.first.Email != "" {
		fields = append(fields, [2]string{"Email", outcome.first.Email})
	}
	if selected.Context.Organization != "" {
		fields = append(fields, [2]string{"Organization", selected.Context.Organization})
	}
	fields = append(fields, [2]string{"Products", productNamespaces(selected.Identity)})
	// One field per access this login actually established, naming the
	// strategy that reached it: a reader who sees "apim, federated" knows both
	// that the product is up and how its session differs from the login's own.
	for _, access := range outcome.established {
		label := "Session"
		if access.Namespace != "" {
			label = access.Namespace
		}
		fields = append(fields, [2]string{label, access.Strategy + ", established"})
	}
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

// loginUsageRecovery is the way back from every wso2 login usage refusal.
const loginUsageRecovery = "Run wso2 login [--url <issuer> --client-id <id>] " +
	"[--context <name>] [--no-input]."
