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
	"os"
	"strings"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// identityType is what a login writes into a created identity's type member.
//
// It is descriptive: the member is validated by internal/contexts and read by
// no logic in this repository, so nothing branches on it. "onprem" is what the
// one decided sample this command implements shows
// (docs/examples/login-walkthroughs.md B.1); "cloud" belongs to the bare
// zero-flag login, which is out of scope until gap 7 in those walkthroughs has
// an answer. See #112 D5.
const identityType = "onprem"

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
		if _, err := planLogin(document, name, flags.issuer, clientID); err != nil {
			return document, err
		}
		// A fresh machine yields the zero document, whose schema version is
		// zero rather than the one the shell writes.
		document.SchemaVersion = contexts.SchemaVersion
		if !declaresIdentity(document, name) {
			document.Identities = append(document.Identities, contexts.Identity{
				Name: name,
				Type: identityType,
				Auth: contexts.IdentityAuth{
					Kind:     contexts.KindOAuthBrowser,
					Issuer:   flags.issuer,
					ClientID: clientID,
					// The reference is the identity's own name, which is legal
					// by construction: a credential reference and a name are
					// held to the same pattern, so a name the document accepts
					// is a reference it accepts.
					CredentialRef: name,
				},
				// No products. A self-hosted deployment publishes no catalogue
				// of what it serves, so nothing here could fill this in, and
				// the report names the command that does.
			})
			written.CreatedIdentity = true
		}
		if !declaresContext(document, name) {
			document.Contexts = append(document.Contexts,
				contexts.Context{Name: name, Identity: name})
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
		Type: identityType,
		Auth: contexts.IdentityAuth{
			Kind:          contexts.KindOAuthBrowser,
			Issuer:        issuer,
			ClientID:      clientID,
			CredentialRef: name,
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
	for _, declared := range document.Contexts {
		if declared.Name == name && declared.Identity != name {
			return contexts.Selection{}, contextExists(name)
		}
	}
	return contexts.Selection{
		Context:  contexts.Context{Name: name, Identity: name},
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
	// Which control fired, for the reason the non-interactive refusal gives:
	// an environment variable set in a shell profile months ago is otherwise a
	// refusal with nothing in it to search for.
	if flags.noInput {
		return "", missingClientID("--no-input asked that nothing prompt")
	}
	if os.Getenv(NoInputEnvVar) != "" {
		return "", missingClientID(NoInputEnvVar + " asked that nothing prompt")
	}
	if !stdinIsTerminal() {
		return "", missingClientID("standard input is not a terminal, so nothing can be asked")
	}
	if _, err := fmt.Fprint(s.Streams.Err, "Client ID of the registered OAuth application: "); err != nil {
		return "", err
	}
	// Read from the process's own standard input rather than from a stream on
	// the Shell: the shell has streams for what it writes and none for what it
	// reads, and one prompt is not the case for inventing one. #86 is where a
	// reader belongs if a second prompt ever appears.
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return "", missingClientID("nothing was entered at the prompt")
	}
	clientID := strings.TrimSpace(scanner.Text())
	if clientID == "" {
		return "", missingClientID("nothing was entered at the prompt")
	}
	return clientID, nil
}

// stdinIsTerminal reports whether standard input is a character device, which
// is as close to "a person could answer a prompt" as the standard library gets.
//
// It is not the same question. /dev/null and /dev/zero are character devices
// too, so a login run with standard input redirected from one of them prompts
// and then refuses with "nothing was entered at the prompt" — the right code,
// the right recovery, and no wait for input that is not coming. What this does
// rule out is the case that matters, a pipe or a file, where prompting would
// consume a line of somebody's data and call it a client ID.
//
// The shell has no terminal handling by decision, so this asks the one question
// this command has rather than taking a dependency on a terminal package to
// answer it more precisely than the outcome needs.
func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
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
	_, err := fmt.Fprintf(s.Streams.Out,
		"\nNo products are configured for this identity. A self-hosted deployment is not\n"+
			"discoverable, so each product's endpoint has to be recorded:\n\n"+
			"  wso2 identity add-product %s <namespace> \\\n"+
			"      --endpoint <url> --audience <resource-id> --scopes <list>\n",
		written.Identity)
	return err
}
