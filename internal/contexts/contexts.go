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

// Package contexts reads the shell-owned invocation contexts.
//
// A document separates identities — how the shell authenticates and what it
// can reach — from contexts, which say what a command runs against. Neither
// ever contains a credential: they name where one comes from, and the types
// have nowhere to put a value even if a writer tried. See
// docs/examples/authentication-contexts.md.
//
// The shell reads contexts and never writes them. Creating one is a test
// fixture's job, so no shell command can write a context that grants itself
// access.
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

	"github.com/wso2/wso2-cli/sdk/problem"
)

// SchemaVersion is the current context-document schema. The shell also reads
// SchemaVersionLegacy documents through a compatibility mapping; any other
// version fails closed rather than being partly interpreted.
const SchemaVersion = 2

// FileName is the context document's fixed name inside the shell state tree.
const FileName = "contexts.json"

// DefaultName is the context a command runs against when the shell has no
// context document. It targets nothing and carries no authentication, so a
// command that needs access is refused by the broker rather than run against a
// guess.
const DefaultName = "default"

// MethodDevelopmentCredential is the architecture proof's only authentication
// method: the shell reads a development credential from a named environment
// variable and exchanges it for a short-lived fixture token.
//
// It is not a production method and not a legal v2 kind: it reaches the
// in-memory document only through the v1 compatibility read.
const MethodDevelopmentCredential = "development-credential"

// namePattern constrains a context name to one readable word.
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// variablePattern constrains a credential source to something that is
// recognizably an environment variable name. A credential value pasted where a
// variable name belongs is rejected rather than stored.
var variablePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

// Document is the shell's context store.
type Document struct {
	// SchemaVersion identifies the document format.
	SchemaVersion int `json:"schemaVersion"`
	// DefaultContext is the name of the context commands run against.
	DefaultContext string `json:"defaultContext"`
	// Identities are the authentication arrangements contexts reference.
	Identities []Identity `json:"identities"`
	// Contexts are the configured contexts.
	Contexts []Context `json:"contexts"`
}

// Context is one target a command can run against. It references an identity
// for authentication and narrows it to an organization and project.
type Context struct {
	// Name identifies the context.
	Name string `json:"name"`
	// Identity names the identity this context authenticates as.
	Identity string `json:"identity"`
	// Organization is the organization commands run within. Access is bound
	// to it, so a token minted here is refused elsewhere.
	Organization string `json:"organization,omitempty"`
	// Project further narrows the target inside the organization.
	Project string `json:"project,omitempty"`
}

// Selection is one resolved context together with its identity.
type Selection struct {
	Context  Context
	Identity Identity
}

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
			"the WSO2 CLI context document cannot be read",
			"Check that the context document is readable, or remove it to run without a context.")
	}
	return Decode(data)
}

// Decode parses and validates a context document.
//
// The schema version is probed first: the current version decodes directly, a
// legacy version decodes through the compatibility mapping, and any other
// version fails closed rather than being partly interpreted.
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
	case SchemaVersion:
		return decodeCurrent(data)
	default:
		return Document{}, contextProblem("contexts.schema_unsupported",
			fmt.Sprintf("context document schema version %d is not supported by this shell", probe.SchemaVersion),
			"Update the WSO2 CLI, or write a context document this shell owns.")
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
// A compatibility-read document refuses outright: the shell never rewrites a
// version 1 document into version 2 behind its author's back.
func (d Document) Encode() ([]byte, error) {
	if d.compatibilityRead() {
		return nil, contextProblem("contexts.document_malformed",
			"a compatibility-read context document cannot be written back",
			"Author a schema version 2 document. The shell does not rewrite version 1 documents in place.")
	}
	if err := d.validate(); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("contexts: cannot encode the context document: %w", err)
	}
	return append(data, '\n'), nil
}

// compatibilityRead reports whether this document reached memory through the
// v1 compatibility mapping.
//
// A synthetic identity marks the usual case, but a v1 document that declared no
// contexts produces no identity to mark, so the version it was read at answers
// as well. Either way the shell refuses to write it back.
func (d Document) compatibilityRead() bool {
	if d.SchemaVersion == SchemaVersionLegacy {
		return true
	}
	for _, identity := range d.Identities {
		if identity.synthetic {
			return true
		}
	}
	return false
}

// Select resolves the named context and its identity. An empty name selects
// the document's default context.
func (d Document) Select(name string) (Selection, error) {
	if len(d.Contexts) == 0 {
		if name != "" {
			return Selection{}, unknownContext(name)
		}
		return Selection{Context: Context{Name: DefaultName}}, nil
	}
	wanted := name
	if wanted == "" {
		wanted = d.DefaultContext
	}
	for _, candidate := range d.Contexts {
		if candidate.Name == wanted {
			return Selection{Context: candidate, Identity: d.identity(candidate.Identity)}, nil
		}
	}
	return Selection{}, unknownContext(wanted)
}

func (d Document) identity(name string) Identity {
	for _, candidate := range d.Identities {
		if candidate.Name == name {
			return candidate
		}
	}
	// Unreachable for a validated document: every context references a
	// declared identity.
	return Identity{}
}

func unknownContext(name string) problem.Problem {
	return contextProblem("contexts.unknown_context",
		fmt.Sprintf("no context named %q is configured", name),
		"Select a configured context, or remove the context document to run without one.")
}

// validate proves the document is internally consistent before any command
// depends on it.
func (d Document) validate() error {
	if d.SchemaVersion != SchemaVersion {
		return contextProblem("contexts.schema_unsupported",
			fmt.Sprintf("context document schema version %d is not supported by this shell", d.SchemaVersion),
			"Update the WSO2 CLI, or write a context document this shell owns.")
	}

	identities := make(map[string]struct{}, len(d.Identities))
	for _, identity := range d.Identities {
		if _, duplicate := identities[identity.Name]; duplicate {
			return malformed(fmt.Sprintf("declares the identity %q more than once", identity.Name))
		}
		identities[identity.Name] = struct{}{}
		if err := identity.validate(); err != nil {
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
		if _, found := identities[candidate.Identity]; !found {
			return malformed(fmt.Sprintf("the context %q references the identity %q, which the document does not declare",
				candidate.Name, candidate.Identity))
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

func malformed(detail string) problem.Problem {
	return contextProblem("contexts.document_malformed",
		"the WSO2 CLI context document "+detail,
		"Correct the context document, or remove it to run without a context.")
}

func contextProblem(code, message, recovery string) problem.Problem {
	return problem.New(problem.CategoryUsage, code, message).WithRecovery(recovery)
}
