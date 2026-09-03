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
	"bufio"
	"fmt"
	"net/url"
	"strings"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// loginCreating logs in against an issuer named on the command line and writes
// the identity and the context it authenticated as.
//
// It authenticates first and writes second, which is this wave's ruling rather
// than the ticket's. A login that fails leaves no identity and no context
// behind, so a user who mistyped an issuer re-runs the corrected command
// without first deleting a half-written context — the editor round trip #112
// exists to remove.
//
// The cost is that a session is minted before the document that names it. That
// is why everything this command can answer without the network is answered
// before the login: the issuer's shape, the name, whether an identity of that
// name is something else, and whether this shell may write the document that is
// there. What remains after those is a write that fails transiently — a lock
// held too long, a full disk — and re-running the command is the way through
// one of those. A deterministic write refusal must never get this far, because
// what it strands is a refresh token in the secure store that no identity
// names, that no command reaches, and that every retry duplicates.
func (s Shell) loginCreating(flags loginFlags) error {
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	if err := refuseNonIssuerURL(flags.issuer); err != nil {
		return err
	}
	name, err := loginIdentityName(flags)
	if err != nil {
		return err
	}
	clientID, err := s.resolveClientID(flags)
	if err != nil {
		return err
	}
	document, err := contexts.Load(root)
	if err != nil {
		return err
	}
	// Read before the login rather than only inside the write, so a mismatch
	// costs the user a refusal instead of a browser round trip they then find
	// out was pointless. The write repeats the check against the document it
	// actually holds the lock on, which is the answer that decides anything.
	selected, err := planLogin(document, name, flags.issuer, clientID)
	if err != nil {
		return err
	}
	// Asked for the same reason, and with more at stake: a document this shell
	// is not allowed to overwrite refuses the same way however long the user
	// waits, so finding out after the login would cost them a session nothing
	// can reach. Load above does not answer this — it reads a version 1
	// document quite happily, and it is the write that refuses.
	if err := contexts.Writable(root); err != nil {
		return s.explainWriteRefusal(root, err)
	}

	result, err := s.establishAndStore(selected, flags)
	if err != nil {
		return err
	}

	// Everything logged here came from a flag the user typed or from the
	// document, so none of it is credential material: the client identifier is
	// public by definition, and the record is written before the write it
	// describes so that a failure has a line above it saying what was tried.
	s.log.Debug("writing the identity and context a login created",
		"identity", name, "context", name,
		"issuer", flags.issuer, "client_id", clientID,
		"document", contexts.Path(root))

	written := loginWrite{Identity: name, Context: name}
	err = contexts.Update(root, func(document contexts.Document) (contexts.Document, error) {
		planned, err := planLogin(document, name, flags.issuer, clientID)
		if err != nil {
			return document, err
		}
		// A fresh machine yields the zero document, whose schema version is
		// zero rather than the one the shell writes.
		document.SchemaVersion = contexts.SchemaVersion
		if !declaresIdentity(document, name) {
			// The identity planLogin built is written as planned, so what the
			// report describes and what the document holds cannot disagree.
			// Its credential reference is the identity's own name, which is
			// legal by construction: a reference and a name are held to the
			// same pattern, so a name the document accepts is a reference it
			// accepts. It carries no products, because this login discovers
			// none, and the report names the command that records them.
			document.Identities = append(document.Identities, planned.Identity)
			written.CreatedIdentity = true
		}
		if !declaresContext(document, name) {
			// The context planLogin resolved, for the same reason the identity
			// is written as planned: it carries the organization the identity's
			// home tenant names, and what the report describes and what the
			// document holds cannot disagree.
			document.Contexts = append(document.Contexts, planned.Context)
			written.CreatedContext = true
		}
		if document.DefaultContext == "" {
			document.DefaultContext = name
			written.Selected = true
		}
		return document, nil
	})
	if err != nil {
		return s.explainWriteRefusal(root, err)
	}

	if err := s.reportLogin(selected, result); err != nil {
		return err
	}
	return s.reportLoginWrite(written, selected.Identity)
}

// loginWrite is what a creating login changed in the context document.
type loginWrite struct {
	Identity        string
	Context         string
	CreatedIdentity bool
	CreatedContext  bool
	// Selected reports that this context became the one commands run against,
	// which happens for the first context on a machine and no other.
	Selected bool
}

// loginIdentityName is the name a creating login assigns.
//
// --context names it when given, and the issuer host derives it otherwise
// (#112 D6). Either way one name serves the identity and the context: they are
// created together by one command, and two names for one thing would be two
// things for the user to remember about a target they named once.
func loginIdentityName(flags loginFlags) (string, error) {
	if flags.contextName == "" {
		return contexts.IdentityNameForIssuer(flags.issuer)
	}
	// Checked here rather than left to the document, for the reason
	// contextCreate states: a name that never reached the file must not be
	// reported as a malformed file.
	if !contexts.ValidName(flags.contextName) {
		return "", problem.New(problem.CategoryUsage, "shell.invalid_argument",
			fmt.Sprintf("%q cannot be used as a context name", flags.contextName)).
			WithRecovery(fmt.Sprintf("A context name is %s. %s", contexts.NameRule, loginUsageRecovery))
	}
	return flags.contextName, nil
}

// refuseNonIssuerURL refuses a --url that is not one.
//
// A missing scheme is one of the two commonest first-run mistakes, and nothing
// downstream reports it as one: url.Parse accepts "idp.customer.example" with
// an empty host, so the derivation refuses a name it cannot make and never
// mentions --url, and a --context that supplies the name sends the malformed
// issuer to the OIDC client, which reports a discovery failure against the
// issuer "of the selected context" when no context is selected. Two wrong
// messages in a row for one typo, so it is caught where the value arrives.
//
// Userinfo is refused here for a harder reason. The only other check on it is
// the document's, which runs inside contexts.Update — after the login — so a
// URL carrying a password would authenticate, store a session, and only then
// be refused, stranding a token no identity names. Every condition this
// function knows has to be one the shell can answer before it mints anything.
//
// Neither branch echoes the value. internal/contexts refuses an endpoint or a
// reference without repeating it, because what was pasted where one belongs may
// be a credential; a --url is exactly as plausible a place for that, and with
// the userinfo branch it is the likeliest one in the shell.
func refuseNonIssuerURL(issuer string) error {
	parsed, err := url.Parse(issuer)
	if err != nil || parsed.Host == "" ||
		(parsed.Scheme != "https" && parsed.Scheme != "http") {
		return problem.New(problem.CategoryUsage, "shell.invalid_argument",
			"the --url value is not an issuer URL").
			WithRecovery("Pass the issuer as an absolute URL, as in " +
				"--url https://idp.example. A missing https:// is the usual cause. " +
				"The value is not repeated here, in case it holds a secret.")
	}
	if parsed.User != nil {
		return problem.New(problem.CategoryUsage, "shell.invalid_argument",
			"the --url value carries a user name or password in the URL, which an issuer may not").
			WithRecovery("Pass the issuer on its own, as in --url https://idp.example. " +
				"The shell authenticates through the browser login, so a credential in the " +
				"URL is never used. The value is not repeated here, in case it holds a secret.")
	}
	return nil
}

// planLogin resolves what this login authenticates as, and refuses a name that
// is already something else.
//
// An identity whose issuer and client identifier both match is reused, because
// re-running the same login is the ordinary case: a session expired and someone
// repeated the command out of shell history. An identity that differs in either
// is a different identity wearing a taken name, and is refused rather than
// replaced — the issuer and client it names are not recorded anywhere else, so
// overwriting them is the one thing the user could not undo (#112 D7).
func planLogin(document contexts.Document, name, issuer, clientID string) (contexts.Selection, error) {
	identity := contexts.Identity{
		Name: name,
		// Derived from the issuer, not asserted: an issuer on a WSO2-operated
		// cloud host records "cloud" and anything else records "onprem", so the
		// document says what kind of deployment was actually logged in to.
		// contexts.IdentityTypeForIssuer explains why the member is descriptive.
		Type: contexts.IdentityTypeForIssuer(issuer),
		Auth: contexts.IdentityAuth{
			Kind:          contexts.KindOAuthBrowser,
			Issuer:        issuer,
			ClientID:      clientID,
			CredentialRef: name,
			// The tenant an Asgardeo issuer carries in its path is the home
			// tenant of the session this login establishes, so it is recorded
			// rather than discarded. Empty for every other issuer, which is the
			// derivation failing closed (contexts.TenantForIssuer).
			Tenant: contexts.TenantForIssuer(issuer),
		},
	}
	for _, declared := range document.Identities {
		if declared.Name != name {
			continue
		}
		if declared.Auth.Issuer != issuer {
			return contexts.Selection{}, identityDiffers(name, "issuer", declared.Auth.Issuer, issuer)
		}
		if declared.Auth.ClientID != clientID {
			return contexts.Selection{}, identityDiffers(name, "client", declared.Auth.ClientID, clientID)
		}
		// The declared identity rather than the one built above: it carries the
		// products whose permissions the login has to ask for, and its kind is
		// what decides whether this login has a step at all.
		identity = declared
	}
	// The context runs within the identity's home tenant, which is the one
	// organization its session can already act in — the broker refuses a
	// context that names any other (internal/auth/source.go's checkHomeTenant).
	// Taken from the identity after the loop above, so a reused identity that
	// recorded no tenant yields a context naming no organization, exactly as
	// its earlier logins did.
	selected := contexts.Context{Name: name, Identity: name,
		Organization: identity.Auth.Tenant}
	for _, declared := range document.Contexts {
		if declared.Name != name {
			continue
		}
		if declared.Identity != name {
			return contexts.Selection{}, contextExists(name)
		}
		// The declared context, because what it says stands: a login refreshes
		// a session, and must not undo an organization wso2 org use recorded.
		selected = declared
	}
	return contexts.Selection{
		Context:  selected,
		Identity: identity,
	}, nil
}

// identityDiffers refuses a login that would change an identity rather than use
// it.
//
// It has its own code rather than sharing contexts.context_exists: the thing
// already there is an identity, the field that disagrees is named in the
// message, and the way out is a different --context name rather than a
// different argument. The message names both values because a user who typed
// one of them is looking at half the answer.
func identityDiffers(name, field, declared, asked string) problem.Problem {
	return problem.New(problem.CategoryUsage, "contexts.identity_exists",
		fmt.Sprintf("the identity %q already authenticates against the %s %q, not %q",
			name, field, declared, asked)).
		WithRecovery("Log in under another name with --context <name>. " +
			"Logging in never replaces an identity that is already configured.")
}

// resolveClientID answers which OAuth application this login presents itself
// as.
//
// There is no default and the shell invents none: no WSO2-published client
// exists for a self-hosted deployment, so the value can only come from the
// operator who registered the application.
func (s Shell) resolveClientID(flags loginFlags) (string, error) {
	if flags.clientID != "" {
		return flags.clientID, nil
	}
	// mayPrompt (prompt.go) is the shared answer to "may this ask standard
	// input a question": the flag, WSO2_NO_INPUT, then whether standard input
	// is a terminal, in that order, naming which one refused.
	if may, reason := s.mayPrompt(flags.noInput); !may {
		return "", missingClientID(reason)
	}
	if _, err := fmt.Fprint(s.Streams.Err, "Client ID of the registered OAuth application: "); err != nil {
		return "", err
	}
	// s.reader() is this Shell's own input stream (#86, prompt.go): it
	// defaults to the process's real standard input, and is what a test
	// overrides to answer this prompt without a real terminal to hand it.
	scanner := bufio.NewScanner(s.reader())
	if !scanner.Scan() {
		return "", missingClientID("nothing was entered at the prompt")
	}
	clientID := strings.TrimSpace(scanner.Text())
	if clientID == "" {
		return "", missingClientID("nothing was entered at the prompt")
	}
	return clientID, nil
}

// missingClientID refuses a login that has no application to present.
//
// It shares shell.missing_required_flag with the other commands that refuse an
// absent flag, because it recovers the same way they do: the user supplies the
// flag. The clause says which of the several reasons nothing was asked
// interactively.
func missingClientID(because string) problem.Problem {
	return problem.New(problem.CategoryUsage, "shell.missing_required_flag",
		"wso2 login needs the client ID of a registered OAuth application, and "+because).
		WithRecovery("Pass --client-id <id>. A self-hosted deployment has no WSO2-published " +
			"client, so the operator registers an application and supplies its ID.")
}

// reportLoginWrite says what the login assigned, and what is still missing.
//
// A name the user is not told is a name they have to go and read out of a JSON
// file, and an identity that reaches no product is a first run that stops here
// unless the command that fixes it is named where the user is standing (#118
// acceptance criterion 9).
func (s Shell) reportLoginWrite(written loginWrite, identity contexts.Identity) error {
	switch {
	case written.CreatedIdentity && written.CreatedContext:
		if _, err := fmt.Fprintf(s.Streams.Out, "\nCreated identity %q and context %q.\n",
			written.Identity, written.Context); err != nil {
			return err
		}
	// Reached only if an identity of this name exists and a context of it does
	// not. The document cannot be in that state today — validation refuses a
	// context naming an undeclared identity, and this command creates the two
	// together — but a hand-authored document that declares an identity and no
	// context for it is legal, and this is what that user is told.
	case written.CreatedContext:
		if _, err := fmt.Fprintf(s.Streams.Out,
			"\nCreated context %q for the existing identity %q.\n",
			written.Context, written.Identity); err != nil {
			return err
		}
	}
	if written.Selected {
		if _, err := fmt.Fprint(s.Streams.Out,
			"It is the first context, so it is now the selected one.\n"); err != nil {
			return err
		}
	}
	if len(identity.Products) > 0 {
		return nil
	}
	// The justification is picked by the identity's own deployment kind,
	// because it is only true of one of them: a self-hosted deployment
	// publishes no catalogue of what it serves, so "not discoverable" is its
	// honest explanation, while a user who just authenticated against WSO2's
	// cloud must not be told their deployment is self-hosted. The instruction
	// is the same either way — this login discovers no products for anyone.
	if identity.Type == contexts.TypeOnprem {
		_, err := fmt.Fprintf(s.Streams.Out,
			"\nNo products are configured for this identity. A self-hosted deployment is not\n"+
				"discoverable, so each product's endpoint has to be recorded:\n\n"+
				"  wso2 identity add-product %s <namespace> \\\n"+
				"      --endpoint <url> --audience <resource-id> --scopes <list>\n",
			written.Identity)
		return err
	}
	_, err := fmt.Fprintf(s.Streams.Out,
		"\nNo products are configured for this identity yet. Product endpoints are not\n"+
			"discovered automatically, so each product's endpoint has to be recorded:\n\n"+
			"  wso2 identity add-product %s <namespace> \\\n"+
			"      --endpoint <url> --audience <resource-id> --scopes <list>\n",
		written.Identity)
	return err
}
