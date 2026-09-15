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

// Package contexts reads and writes the shell-owned invocation contexts.
//
// A document is one list of contexts. Each context says how the shell logs in
// (its login block), what it can reach (its products), which secure-store
// entries hold its sessions (its credential reference), and optionally which
// organization and project commands run within. Nothing in it is a credential:
// it names where one comes from, and the types have nowhere to put a value
// even if a writer tried. See docs/examples/authentication-contexts.md and
// docs/adr/0016-a-context-owns-its-login-and-sessions.md.
//
// The shell both reads and writes this document; Save and Update in save.go are
// the only production writers. Nothing about that grants access: as above, the
// artifact cannot carry a credential, so which command writes one is not a
// security question. See docs/adr/0012-writing-a-context-or-identity-grants-nothing.md.
package contexts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/wso2/wso2-cli/sdk/problem"
)

// SchemaVersion is the current context-document schema. The shell also reads
// the earlier schemas: version 1 through a read-only compatibility mapping,
// and versions 2 and 3 through a migration that is written back as this
// version. Any other version fails closed rather than being partly
// interpreted.
const SchemaVersion = 4

// SchemaVersionAccounts is the schema that kept accounts and contexts in two
// lists, each context naming the account it authenticated as. It is migrated
// on read (migrate.go).
const SchemaVersionAccounts = 3

// SchemaVersionIdentities is the schema before the identity concept became the
// account one. It differs from SchemaVersionAccounts in two member names, and
// is migrated the same way.
const SchemaVersionIdentities = 2

// FileName is the context document's fixed name inside the shell state tree.
const FileName = "contexts.json"

// MethodDevelopmentCredential is the architecture proof's only authentication
// method: the shell reads a development credential from a named environment
// variable and exchanges it for a short-lived fixture token.
//
// It is not a production method and not a legal kind: it reaches the in-memory
// document only through the v1 compatibility read.
const MethodDevelopmentCredential = "development-credential"

// namePattern constrains a context name to one readable word.
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// NameRule states what namePattern requires, in the words a refusal uses. It
// lives beside the pattern so that changing one without the other is visible.
const NameRule = "lower-case letters, digits and hyphens, starting with a letter, " +
	"at most 64 characters"

// ValidName reports whether a name may be given to a context.
//
// It is exported so that a command can refuse a name a user typed before that
// name reaches the document. The refusal then reads as a complaint about the
// argument, which the user can retype, rather than as a complaint about the
// file, which they did not write and must not be told to remove.
func ValidName(name string) bool { return namePattern.MatchString(name) }

// variablePattern constrains a credential source to something that is
// recognizably an environment variable name. A credential value pasted where a
// variable name belongs is rejected rather than stored.
var variablePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

// ValidVariable reports whether a value is shaped like an environment variable
// name, the only thing a document accepts as a secret's source.
func ValidVariable(name string) bool { return variablePattern.MatchString(name) }

// Document is the shell's context store.
type Document struct {
	// SchemaVersion identifies the document format.
	SchemaVersion int `json:"schemaVersion"`
	// DefaultContext is the name of the context commands run against. Empty
	// when nothing is selected, which is legal: wso2 context apply adds
	// contexts without selecting one unless asked to.
	DefaultContext string `json:"defaultContext,omitempty"`
	// Contexts are the configured contexts.
	Contexts []Context `json:"contexts"`

	// migration records what reading an earlier schema changed. It is never
	// encoded: the next write produces a current document and the record has
	// done its job.
	migration *Migration
}

// Context is one target a command can run against: how the shell logs in, the
// products it reaches, and the sessions it holds.
type Context struct {
	// Name identifies the context.
	Name string `json:"name"`
	// Type says whether the context targets a cloud or on-premises deployment.
	// It is "cloud" or "onprem".
	Type string `json:"type"`
	// CredentialRef names the secure-store entries this context's sessions
	// live under: the login session under the reference itself, and each
	// product's own under ProductSessionRef. It is a stable identifier, not a
	// name: renaming the context leaves it alone, so no stored session moves.
	// No two contexts share one (see validate). Empty for a client-credentials
	// context, which stores nothing.
	CredentialRef string `json:"credentialRef,omitempty"`
	// Login says how the shell authenticates for this context.
	Login Login `json:"login"`
	// Organization is the organization commands run within. Access is bound
	// to it, so a token minted here is refused elsewhere.
	Organization string `json:"organization,omitempty"`
	// Project further narrows the target inside the organization.
	Project string `json:"project,omitempty"`
	// Products are the product services reachable from this context, keyed by
	// product namespace.
	Products map[string]Product `json:"products,omitempty"`

	// synthetic marks a context manufactured by the v1 compatibility read. It
	// is never encodable.
	synthetic bool
	// credentialVariable exists only on synthetic v1 contexts. Never encoded.
	credentialVariable string
}

// Login is a context's authentication arrangement. Every member is a name or a
// location; none holds a credential.
type Login struct {
	// Kind identifies how the shell obtains access: KindOAuthBrowser,
	// KindOAuthDevice, KindClientCredentials or KindPAT.
	Kind string `json:"kind"`
	// Issuer is the token issuer the shell authenticates against.
	Issuer string `json:"issuer,omitempty"`
	// ClientID is the OAuth client this shell presents itself as.
	ClientID string `json:"clientId,omitempty"`
	// Tenant is the context's home tenant, when the issuer is multi-tenant.
	Tenant string `json:"tenant,omitempty"`
	// Provider names the identity provider behind the issuer. It is optional,
	// and it implies a derivation rather than being one.
	Provider string `json:"provider,omitempty"`
	// Narrowing names the derivation explicitly, for a deployment that does not
	// match what its provider ordinarily requires. It wins over Provider.
	Narrowing string `json:"narrowing,omitempty"`
	// ClientSecretVariable names the environment variable holding the client
	// secret for the client-credentials kind. It is a name, never a value.
	ClientSecretVariable string `json:"clientSecretVariable,omitempty"`
	// Product names the direct product the login authorization is run for.
	// Written whenever the context reaches a direct product, so that adding a
	// product which sorts earlier never moves the login from under the
	// sessions already stored. Empty for a login through a bare issuer.
	Product string `json:"product,omitempty"`
}

// Selection is one resolved context together with the authentication view the
// broker reads it through.
type Selection struct {
	Context Context
	// Identity is the context's authentication arrangement as the broker and
	// the access plan read it (Context.Account).
	Identity Account
}

// Account is the authentication view of a context: its login block, its
// credential reference and its products, in the shape the access plan
// (access.go) and the broker read. It is not a separate record in the
// document; Context.Account builds it, and nothing writes one.
//
// Name is the context's name. Refusals and reports that name "the context"
// read it from here.
type Account struct {
	Name         string
	Type         string
	Auth         AccountAuth
	Products     map[string]Product
	LoginProduct string
	synthetic    bool
	// noun is what a refusal calls the record: empty for a context, "account"
	// while a migration validates a schema version 3 account.
	noun string
}

// AccountAuth is the authentication view's login arrangement: the context's
// login block plus its credential reference. The JSON tags are the schema
// version 3 spelling, which migrate.go decodes through this type.
type AccountAuth struct {
	Kind                 string `json:"kind"`
	Issuer               string `json:"issuer,omitempty"`
	ClientID             string `json:"clientId,omitempty"`
	Tenant               string `json:"tenant,omitempty"`
	CredentialRef        string `json:"credentialRef,omitempty"`
	ClientSecretVariable string `json:"clientSecretVariable,omitempty"`
	Provider             string `json:"provider,omitempty"`
	Narrowing            string `json:"narrowing,omitempty"`
	// CredentialVariable exists only on synthetic v1 contexts. Never encoded.
	CredentialVariable string `json:"-"`
}

// Account builds the context's authentication view.
func (c Context) Account() Account {
	return Account{
		Name: c.Name,
		Type: c.Type,
		Auth: AccountAuth{
			Kind:                 c.Login.Kind,
			Issuer:               c.Login.Issuer,
			ClientID:             c.Login.ClientID,
			Tenant:               c.Login.Tenant,
			CredentialRef:        c.CredentialRef,
			ClientSecretVariable: c.Login.ClientSecretVariable,
			Provider:             c.Login.Provider,
			Narrowing:            c.Login.Narrowing,
			CredentialVariable:   c.credentialVariable,
		},
		Products:     c.Products,
		LoginProduct: c.Login.Product,
		synthetic:    c.synthetic,
	}
}

// FromAccount is the context an authentication view describes, named name:
// the inverse of Context.Account. It exists for code that builds the view
// first, which the access plan's own tests do.
func FromAccount(name string, account Account) Context {
	return Context{
		Name:          name,
		Type:          account.Type,
		CredentialRef: account.Auth.CredentialRef,
		Login: Login{
			Kind: account.Auth.Kind, Issuer: account.Auth.Issuer, ClientID: account.Auth.ClientID,
			Tenant: account.Auth.Tenant, Provider: account.Auth.Provider, Narrowing: account.Auth.Narrowing,
			ClientSecretVariable: account.Auth.ClientSecretVariable, Product: account.LoginProduct,
		},
		Products: account.Products,
	}
}

// Synthetic reports whether this context was manufactured by the v1
// compatibility read. A synthetic context is readable but never written back.
func (c Context) Synthetic() bool { return c.synthetic }

// CredentialVariable is the environment variable a v1 context reads its
// development credential from, and empty for every other context.
func (c Context) CredentialVariable() string { return c.credentialVariable }

// Path reports the context document's location inside a state root.
func Path(stateRoot string) string {
	return filepath.Join(stateRoot, "cli", FileName)
}

// Load reads the context document from a state root.
//
// A shell with no context document is a shell with no contexts, not a failure:
// the command still runs, and a module that needs access is refused by the
// broker with guidance. Anything else that cannot be read fails closed.
func Load(stateRoot string) (Document, error) {
	data, err := os.ReadFile(Path(stateRoot))
	switch {
	case os.IsNotExist(err):
		return Document{}, nil
	case err != nil:
		return Document{}, contextProblem("contexts.document_unreadable",
			fmt.Sprintf("the WSO2 CLI context document at %s cannot be read", Path(stateRoot)),
			"Check that the file is readable, or remove it to run without a context.")
	}
	return Decode(data)
}

// Decode parses and validates a context document.
//
// The schema version is probed first: the current version decodes directly,
// versions 2 and 3 decode through the migration, version 1 through the
// read-only compatibility mapping, and any other version fails closed.
func Decode(data []byte) (Document, error) {
	var probe struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&probe); err != nil {
		return Document{}, malformed("is not valid JSON")
	}
	switch probe.SchemaVersion {
	case SchemaVersionLegacy:
		return decodeLegacy(data)
	case SchemaVersionIdentities, SchemaVersionAccounts:
		return decodeAccounts(data, probe.SchemaVersion)
	case SchemaVersion:
		return decodeCurrent(data)
	default:
		return Document{}, contextProblem("contexts.schema_unsupported",
			fmt.Sprintf("context document schema version %d is not supported by this shell", probe.SchemaVersion),
			"Update the WSO2 CLI, or run the WSO2 CLI version that manages this document.")
	}
}

// decodeCurrent is the strict single-document decode of the current schema.
//
// Unknown JSON members are tolerated so a newer shell can add non-secret
// context facts within the same schema version. A trailing document is refused,
// so a second value cannot be smuggled past a decoder that stops after the
// first.
func decodeCurrent(data []byte) (Document, error) {
	var document Document
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&document); err != nil {
		return Document{}, malformed("is not valid JSON")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return Document{}, malformed("contains more than one JSON document")
	}
	if err := document.validate(); err != nil {
		return Document{}, err
	}
	return document, nil
}

// Encode renders the document as the canonical on-disk form, refusing a
// document this shell would not read back.
//
// The login product of every context is frozen first (Freeze), so a document
// assembled in memory is written complete. A compatibility-read document
// refuses outright: the shell never rewrites a version 1 document behind its
// author's back.
func (d Document) Encode() ([]byte, error) {
	if d.compatibilityRead() {
		return nil, contextProblem("contexts.document_malformed",
			"a compatibility-read context document cannot be written back",
			fmt.Sprintf("Author a schema version %d document. The shell does not rewrite version 1 "+
				"documents in place.", SchemaVersion))
	}
	d = d.Freeze()
	d.SchemaVersion = SchemaVersion
	if err := d.validate(); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("contexts: cannot encode the context document: %w", err)
	}
	return append(data, '\n'), nil
}

// Freeze returns the document with every context's login product written out:
// the product its login already runs for, pinned so that recording another
// product can never move it. A context that already names one, reaches no
// direct product, or logs in through nothing is left as it is.
func (d Document) Freeze() Document {
	frozen := slices.Clone(d.Contexts)
	for index, candidate := range frozen {
		frozen[index] = candidate.Freeze()
	}
	d.Contexts = frozen
	return d
}

// Freeze returns the context with its login product written out; see
// Document.Freeze.
func (c Context) Freeze() Context {
	if c.Login.Product != "" || c.Login.Kind == KindClientCredentials || c.synthetic {
		return c
	}
	c.Login.Product = c.Account().LoginAccess().Namespace
	return c
}

// compatibilityRead reports whether this document reached memory through the
// v1 compatibility mapping.
func (d Document) compatibilityRead() bool {
	if d.SchemaVersion == SchemaVersionLegacy {
		return true
	}
	for _, candidate := range d.Contexts {
		if candidate.synthetic {
			return true
		}
	}
	return false
}

// Select resolves the named context. An empty name selects the document's
// default context.
//
// A shell with no contexts configured runs against the empty selection: no
// name, no target, no authentication. Its empty name is deliberate — a context
// name must match namePattern, which admits nothing shorter than one letter —
// so nothing downstream can mistake the fallback for a context a user could
// list, select, or be told to use. A command that needs access is refused by
// the broker rather than run against a guess.
func (d Document) Select(name string) (Selection, error) {
	if len(d.Contexts) == 0 {
		if name != "" {
			return Selection{}, noContextConfigured(name)
		}
		return Selection{}, nil
	}
	wanted := name
	if wanted == "" {
		wanted = d.DefaultContext
	}
	if wanted == "" {
		return Selection{}, noContextSelected()
	}
	for _, candidate := range d.Contexts {
		if candidate.Name == wanted {
			return Selection{Context: candidate, Identity: candidate.Account()}, nil
		}
	}
	return Selection{}, unknownContext(wanted)
}

// Find returns the named context and whether the document declares it.
func (d Document) Find(name string) (Context, bool) {
	for _, candidate := range d.Contexts {
		if candidate.Name == name {
			return candidate, true
		}
	}
	return Context{}, false
}

// Put returns the document with the context added, or replacing the one of
// the same name in place. The receiver's list is not modified.
func (d Document) Put(context Context) Document {
	contexts := slices.Clone(d.Contexts)
	position := slices.IndexFunc(contexts, func(candidate Context) bool { return candidate.Name == context.Name })
	if position < 0 {
		contexts = append(contexts, context)
	} else {
		contexts[position] = context
	}
	d.Contexts = contexts
	return d
}

// Without returns the document with the named context removed, clearing the
// selection when it named that context. The receiver's list is not modified.
func (d Document) Without(name string) Document {
	d.Contexts = slices.DeleteFunc(slices.Clone(d.Contexts), func(candidate Context) bool {
		return candidate.Name == name
	})
	if d.DefaultContext == name {
		d.DefaultContext = ""
	}
	return d
}

// NewCredentialRef is a credential reference no context in the document holds:
// the base itself when it is free, else the base with the lowest free numeric
// suffix. The base is a context name, so the result is legal by construction.
func (d Document) NewCredentialRef(base string) string {
	taken := map[string]bool{}
	for _, candidate := range d.Contexts {
		taken[candidate.CredentialRef] = true
	}
	return freeName(base, taken)
}

// freeName is base when taken does not hold it, else base-N for the lowest N
// that it does not. The base is shortened when the suffix would take it past
// the 64 characters a name or reference may have.
func freeName(base string, taken map[string]bool) string {
	if !taken[base] {
		return base
	}
	for number := 2; ; number++ {
		suffix := fmt.Sprintf("-%d", number)
		stem := base
		if len(stem)+len(suffix) > 64 {
			stem = stem[:64-len(suffix)]
		}
		if candidate := stem + suffix; !taken[candidate] {
			return candidate
		}
	}
}

// Migrated reports what reading an earlier schema changed, and whether the
// document was read from one at all. A current document reports nothing.
func (d Document) Migrated() (Migration, bool) {
	if d.migration == nil {
		return Migration{}, false
	}
	return *d.migration, true
}

func unknownContext(name string) problem.Problem {
	return contextProblem("contexts.unknown_context",
		fmt.Sprintf("no context named %q is configured", name),
		"Run wso2 context list to see the configured contexts, then wso2 context use <name> "+
			"to select one.")
}

// noContextSelected refuses a selection on a document that has contexts and
// selects none of them, which wso2 context apply leaves behind by design.
func noContextSelected() problem.Problem {
	return contextProblem("contexts.no_context_selected",
		"no context is selected",
		"Run wso2 context list to see the configured contexts, then wso2 context use <name> "+
			"to select one, or pass --context <name>.")
}

// noContextConfigured refuses a named selection on a shell that has no
// contexts at all. It is a separate refusal from unknownContext because the
// recovery differs: there is no list to consult and nothing to select, so the
// only way forward is to create a context.
func noContextConfigured(name string) problem.Problem {
	return contextProblem("contexts.unknown_context",
		fmt.Sprintf("no context named %q is configured, and no contexts exist", name),
		"Run wso2 context create <name> --login-product <product> --url <url>, "+
			"wso2 context apply -f <file>, or wso2 login --url <issuer> --client-id <id> to create one.")
}

// validate proves the document is internally consistent before any command
// depends on it.
func (d Document) validate() error {
	if d.SchemaVersion != SchemaVersion {
		return contextProblem("contexts.schema_unsupported",
			fmt.Sprintf("context document schema version %d is not supported by this shell", d.SchemaVersion),
			"Update the WSO2 CLI, or run the WSO2 CLI version that manages this document.")
	}
	seen := make(map[string]struct{}, len(d.Contexts))
	refs := make(map[string]string, len(d.Contexts))
	for _, candidate := range d.Contexts {
		if !namePattern.MatchString(candidate.Name) {
			return malformed(fmt.Sprintf("declares an invalid context name %q", candidate.Name))
		}
		if _, duplicate := seen[candidate.Name]; duplicate {
			return malformed(fmt.Sprintf("declares the context %q more than once", candidate.Name))
		}
		seen[candidate.Name] = struct{}{}
		if err := candidate.validate(); err != nil {
			return err
		}
		// One reference, one owner. Two contexts sharing a reference would
		// each present the other's sessions the moment either changed a
		// product's URL or grant, and ending one context's sessions would end
		// the other's. See ADR 0016.
		if ref := candidate.CredentialRef; ref != "" {
			if owner, taken := refs[ref]; taken {
				return contextProblem("contexts.document_malformed",
					fmt.Sprintf("the contexts %q and %q declare the same credential reference, and each "+
						"context owns its own sessions", owner, candidate.Name),
					"Give one of them another credentialRef, then run wso2 login for it. Sessions are "+
						"never shared between contexts.")
			}
			refs[ref] = candidate.Name
		}
	}
	if d.DefaultContext == "" {
		return nil
	}
	if _, found := seen[d.DefaultContext]; !found {
		return malformed(fmt.Sprintf("selects the context %q, which it does not declare", d.DefaultContext))
	}
	return nil
}

// validate proves one context is complete and consistent.
func (c Context) validate() error {
	if err := c.Account().validate(); err != nil {
		return err
	}
	// Frozen defaults (ADR 0016): an interactive context that reaches a direct
	// product names the one its login runs for. Without it the login would
	// follow the namespace order, and recording a product that sorts earlier
	// would move the login session from under the sessions already stored.
	// Every writer freezes it (Freeze), so only a hand edit can leave it out.
	if c.Login.Product == "" && c.Login.Kind != KindClientCredentials &&
		c.Account().LoginAccess().Namespace != "" {
		return contextProblem("contexts.document_malformed",
			fmt.Sprintf("the context %q reaches a direct product and does not name its login product", c.Name),
			"Set login.product to the product the context logs in through, or re-apply the context "+
				"with wso2 context apply -f <file>.")
	}
	return nil
}

// DefaultDocumentRecovery is the way out of a document that is wrong as it sits
// on disk. It is the right advice for a reader, which found the fault already
// written, and the wrong advice for a writer, which was refused before writing
// anything: following it there would destroy contexts the fault never reached.
//
// It is exported so a writer can tell this generic advice from a refusal that
// carries specific advice of its own, which several do. See
// CarriesDefaultDocumentRecovery.
const DefaultDocumentRecovery = "Correct the context document (the cli/contexts.json file under " +
	"the WSO2 CLI state directory; wso2 context edit opens it), or remove it to run without a context."

// CarriesDefaultDocumentRecovery reports whether err offers only the generic
// document recovery above, rather than advice specific to what was wrong.
func CarriesDefaultDocumentRecovery(err error) bool {
	var typed problem.Problem
	return errors.As(err, &typed) && typed.Recovery == DefaultDocumentRecovery
}

func malformed(detail string) problem.Problem {
	return contextProblem("contexts.document_malformed",
		"the WSO2 CLI context document "+detail,
		DefaultDocumentRecovery)
}

func contextProblem(code, message, recovery string) problem.Problem {
	return problem.New(problem.CategoryUsage, code, message).WithRecovery(recovery)
}
