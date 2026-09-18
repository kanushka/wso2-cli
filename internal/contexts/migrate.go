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

package contexts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
)

// Migration is what reading a schema version 2 or 3 document changed on its
// way to the current shape. The current shape gives every context its own
// login and its own sessions (ADR 0016), so an account several contexts
// shared can keep its sessions for one of them only.
type Migration struct {
	// From is the schema version the document was read at.
	From int
	// Relogin names the contexts whose account was shared and whose sessions
	// stayed with another context. Each has a credential reference of its own
	// now, holding nothing, so each needs wso2 login again. Sorted.
	Relogin []string
	// Adopted names the contexts made from accounts no context referenced, so
	// that no configuration is lost. Sorted.
	Adopted []string
	// Renamed maps an adopted account's name to the context name it took when
	// a context already held the account's own name.
	Renamed map[string]string
}

// Notes are the sentences a command prints about the migration, one per fact.
// A migration that moved no session and adopted nothing has none: the upgrade
// is then invisible and needs no explanation.
func (m Migration) Notes() []string {
	var notes []string
	for _, name := range m.Adopted {
		if renamed, found := m.Renamed[name]; found {
			notes = append(notes, fmt.Sprintf("The account %q had no context, so it became the context %q "+
				"(its own name was taken).", name, renamed))
			continue
		}
		notes = append(notes, fmt.Sprintf("The account %q had no context, so it became a context of the "+
			"same name.", name))
	}
	for _, name := range m.Relogin {
		notes = append(notes, fmt.Sprintf("The context %q shared its account's session with another context. "+
			"Sessions are no longer shared, so run wso2 login --context %s.", name, name))
	}
	return notes
}

// The schema version 3 shapes. They are private mirrors of what that schema
// wrote, kept only to be read and migrated.
type (
	accountsDocument struct {
		SchemaVersion  int               `json:"schemaVersion"`
		DefaultContext string            `json:"defaultContext"`
		Accounts       []accountsAccount `json:"accounts"`
		Identities     []accountsAccount `json:"identities"`
		Contexts       []accountsContext `json:"contexts"`
	}
	accountsContext struct {
		Name         string `json:"name"`
		Account      string `json:"account"`
		Identity     string `json:"identity"`
		Organization string `json:"organization,omitempty"`
		Project      string `json:"project,omitempty"`
	}
	accountsAccount struct {
		Name         string                     `json:"name"`
		Type         string                     `json:"type"`
		Auth         AccountAuth                `json:"auth"`
		Products     map[string]accountsProduct `json:"products,omitempty"`
		LoginProduct string                     `json:"loginProduct,omitempty"`
	}
	accountsProduct struct {
		Endpoint             string           `json:"endpoint"`
		Audience             string           `json:"audience,omitempty"`
		Scopes               []string         `json:"scopes,omitempty"`
		Grant                *Grant           `json:"grant,omitempty"`
		ClientIDVariable     string           `json:"clientIdVariable,omitempty"`
		ClientSecretVariable string           `json:"clientSecretVariable,omitempty"`
		Gateway              *accountsGateway `json:"gateway,omitempty"`
	}
	accountsGateway struct {
		Endpoint string   `json:"endpoint"`
		Audience string   `json:"audience,omitempty"`
		Scopes   []string `json:"scopes,omitempty"`
	}
)

// account is the schema version 3 account as the access plan reads it, with
// every endpoint carried over as the url it is now called.
func (a accountsAccount) account() Account {
	account := Account{Name: a.Name, Type: a.Type, Auth: a.Auth, LoginProduct: a.LoginProduct, noun: "account"}
	if len(a.Products) > 0 {
		account.Products = make(map[string]Product, len(a.Products))
	}
	for namespace, product := range a.Products {
		converted := Product{
			Endpoint: product.Endpoint, Audience: product.Audience, Scopes: product.Scopes,
			Grant: product.Grant, ClientIDVariable: product.ClientIDVariable,
			ClientSecretVariable: product.ClientSecretVariable,
		}
		if product.Gateway != nil {
			converted.Gateway = &Gateway{
				Endpoint: product.Gateway.Endpoint, Audience: product.Gateway.Audience,
				Scopes: product.Gateway.Scopes,
			}
		}
		account.Products[namespace] = converted
	}
	return account
}

// decodeAccounts reads a schema version 2 or 3 document, validates it by the
// rules that schema had, and migrates it to the current shape in memory. The
// next write stores the result; reading alone changes nothing on disk.
//
// Version 2 differs from version 3 in two member names only — the list of
// accounts was "identities" and each context's reference was "identity" — so
// both spellings are read here, each only at its own version.
func decodeAccounts(data []byte, version int) (Document, error) {
	var legacy accountsDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&legacy); err != nil {
		return Document{}, malformed("is not valid JSON")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return Document{}, malformed("contains more than one JSON document")
	}
	if version == SchemaVersionIdentities {
		legacy.Accounts = legacy.Identities
		for index := range legacy.Contexts {
			legacy.Contexts[index].Account = legacy.Contexts[index].Identity
		}
	}
	if err := legacy.validate(); err != nil {
		return Document{}, err
	}
	document := legacy.migrate()
	document.migration.From = version
	if err := document.validate(); err != nil {
		return Document{}, err
	}
	return document, nil
}

// validate enforces the schema version 3 rules: accounts valid and uniquely
// named, contexts uniquely named and each naming a declared account, and the
// selected context declared.
func (d accountsDocument) validate() error {
	accounts := make(map[string]struct{}, len(d.Accounts))
	for _, account := range d.Accounts {
		if _, duplicate := accounts[account.Name]; duplicate {
			return malformed(fmt.Sprintf("declares the account %q more than once", account.Name))
		}
		accounts[account.Name] = struct{}{}
		if err := account.account().validate(); err != nil {
			return err
		}
	}
	seen := make(map[string]struct{}, len(d.Contexts))
	for _, candidate := range d.Contexts {
		if !namePattern.MatchString(candidate.Name) {
			return malformed(fmt.Sprintf("declares an invalid context name %q", candidate.Name))
		}
		if _, duplicate := seen[candidate.Name]; duplicate {
			return malformed(fmt.Sprintf("declares the context %q more than once", candidate.Name))
		}
		seen[candidate.Name] = struct{}{}
		if _, found := accounts[candidate.Account]; !found {
			return malformed(fmt.Sprintf("the context %q references the account %q, which the document does not declare",
				candidate.Name, candidate.Account))
		}
	}
	if len(d.Contexts) == 0 {
		return nil
	}
	if _, found := seen[d.DefaultContext]; !found {
		return malformed(fmt.Sprintf("selects the context %q, which it does not declare", d.DefaultContext))
	}
	return nil
}

// migrate folds each account into the contexts that named it.
//
// Every context gets its account's login and products. The account's
// credential reference, and with it every session stored under it, goes to
// exactly one of those contexts: the selected context when it is among them,
// else the first by name. The others get a reference of their own and log in
// again. Sessions are never copied to a second reference: a refresh token
// held twice is revoked by an issuer that detects reuse, which would end both.
//
// An account no context named becomes a context of its own, named after it,
// so that nothing configured is lost. The login product is frozen for every
// context (Context.Freeze), so a document that relied on the namespace order
// keeps the login it had.
func (d accountsDocument) migrate() Document {
	migration := &Migration{Renamed: map[string]string{}}
	document := Document{SchemaVersion: SchemaVersion, DefaultContext: d.DefaultContext, migration: migration}

	owner := map[string]string{}
	for _, account := range d.Accounts {
		var users []string
		for _, candidate := range d.Contexts {
			if candidate.Account == account.Name {
				users = append(users, candidate.Name)
			}
		}
		if len(users) == 0 {
			continue
		}
		owner[account.Name] = slices.Min(users)
		if slices.Contains(users, d.DefaultContext) {
			owner[account.Name] = d.DefaultContext
		}
	}

	// A reference two accounts declared is still one set of sessions, so it
	// too goes to one context only: the first to claim it keeps it.
	claimed := map[string]bool{}
	names := map[string]bool{}
	for _, candidate := range d.Contexts {
		names[candidate.Name] = true
	}
	accountByName := map[string]accountsAccount{}
	for _, account := range d.Accounts {
		accountByName[account.Name] = account
	}
	reserved := map[string]bool{}
	for _, account := range d.Accounts {
		if ref := account.Auth.CredentialRef; ref != "" {
			reserved[ref] = true
		}
	}
	for _, candidate := range d.Contexts {
		account := accountByName[candidate.Account]
		context := contextFromAccount(candidate.Name, account)
		context.Organization, context.Project = candidate.Organization, candidate.Project
		if ref := account.Auth.CredentialRef; ref != "" {
			if owner[account.Name] != candidate.Name || claimed[ref] {
				context.CredentialRef = freeName(candidate.Name, mergeTaken(reserved, claimed))
				migration.Relogin = append(migration.Relogin, candidate.Name)
			}
			claimed[context.CredentialRef] = true
		}
		document.Contexts = append(document.Contexts, context)
	}
	for _, account := range d.Accounts {
		if _, used := owner[account.Name]; used {
			continue
		}
		name := freeName(account.Name, names)
		names[name] = true
		context := contextFromAccount(name, account)
		if ref := account.Auth.CredentialRef; ref != "" && claimed[ref] {
			context.CredentialRef = freeName(name, mergeTaken(reserved, claimed))
		}
		if context.CredentialRef != "" {
			claimed[context.CredentialRef] = true
		}
		migration.Adopted = append(migration.Adopted, account.Name)
		if name != account.Name {
			migration.Renamed[account.Name] = name
		}
		document.Contexts = append(document.Contexts, context)
	}
	slices.Sort(migration.Relogin)
	slices.Sort(migration.Adopted)
	return document.Freeze()
}

// contextFromAccount is a context carrying the account's whole arrangement.
func contextFromAccount(name string, account accountsAccount) Context {
	view := account.account()
	return Context{
		Name:          name,
		Type:          account.Type,
		CredentialRef: account.Auth.CredentialRef,
		Login: Login{
			Kind: account.Auth.Kind, Issuer: account.Auth.Issuer, ClientID: account.Auth.ClientID,
			Tenant: account.Auth.Tenant, Provider: account.Auth.Provider, Narrowing: account.Auth.Narrowing,
			ClientSecretVariable: account.Auth.ClientSecretVariable, Product: account.LoginProduct,
		},
		Products: view.Products,
	}
}

// mergeTaken is every reference a new one must avoid: those any account
// declared, and those already given out.
func mergeTaken(reserved, claimed map[string]bool) map[string]bool {
	taken := make(map[string]bool, len(reserved)+len(claimed))
	for ref := range reserved {
		taken[ref] = true
	}
	for ref := range claimed {
		taken[ref] = true
	}
	return taken
}
