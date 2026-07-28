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
// A context says what a command runs against and how the shell obtains access
// to it. It never contains a credential: it names the environment variable the
// shell reads one from, and the type has nowhere to put a value even if a
// writer tried. See docs/examples/authentication-contexts.md.
//
// The architecture proof reads contexts and never writes them. Creating one is
// a test fixture's job, so no shell command can write a context that grants
// itself access.
package contexts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"

	"github.com/wso2/wso2-cli/sdk/problem"
)

// SchemaVersion is the only context-document schema this shell reads. An
// unknown schema version fails closed rather than being partly interpreted.
const SchemaVersion = 1

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
// It is not a production method. Browser, device-code, personal-access-token,
// and client-credential methods are separate work.
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
	// Contexts are the configured contexts.
	Contexts []Context `json:"contexts"`
}

// Context is one target a command can run against.
//
// Its members are the whole of what a context records: a name, the
// organization, the service endpoint, and how the shell authenticates. No
// member holds a credential.
type Context struct {
	// Name identifies the context.
	Name string `json:"name"`
	// OrganizationID is the organization commands run within. Access is bound
	// to it, so a token minted here is refused elsewhere.
	OrganizationID string `json:"organizationId"`
	// Endpoint is the product service this context targets.
	Endpoint string `json:"endpoint"`
	// Auth says how the shell obtains access. It names a credential source and
	// never holds one.
	Auth Auth `json:"auth"`
}

// Auth is a context's authentication arrangement.
type Auth struct {
	// Method identifies how the shell obtains access.
	Method string `json:"method"`
	// CredentialVariable is the name of the environment variable the shell
	// reads the source credential from. It is a name, never a value, and only
	// the shell ever reads the variable it names.
	CredentialVariable string `json:"credentialVariable"`
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
// Unknown JSON members are tolerated so a newer shell can add non-secret
// context facts within the same schema version. A trailing document is refused,
// so a second value cannot be smuggled past a decoder that stops after the
// first.
func Decode(data []byte) (Document, error) {
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
func (d Document) Encode() ([]byte, error) {
	if err := d.validate(); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("contexts: cannot encode the context document: %w", err)
	}
	return append(data, '\n'), nil
}

// Selected reports the context commands run against.
func (d Document) Selected() (Context, error) {
	if len(d.Contexts) == 0 {
		return Context{Name: DefaultName}, nil
	}
	for _, candidate := range d.Contexts {
		if candidate.Name == d.DefaultContext {
			return candidate, nil
		}
	}
	// validate rejects a document whose default names no context, so this is
	// reachable only through a Document a caller built itself.
	return Context{}, contextProblem("contexts.unknown_context",
		fmt.Sprintf("no context named %q is configured", d.DefaultContext),
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

	seen := make(map[string]struct{}, len(d.Contexts))
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
	}

	if len(d.Contexts) == 0 {
		return nil
	}
	if _, found := seen[d.DefaultContext]; !found {
		return malformed(fmt.Sprintf("selects the context %q, which it does not declare", d.DefaultContext))
	}
	return nil
}

func (c Context) validate() error {
	if c.Endpoint != "" {
		// The endpoint is never echoed. A rejected one is the most likely
		// place for a credential to have been typed by mistake, and repeating
		// it into a problem the shell renders would publish it.
		parsed, err := url.Parse(c.Endpoint)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return malformed(fmt.Sprintf("declares an endpoint for the context %q that this shell cannot read",
				c.Name))
		}
		// A URL may carry a user and password before its host. The endpoint
		// reaches the module, so an endpoint that embeds a credential would
		// hand one to a module through the one context member nobody thinks of
		// as carrying credentials.
		if parsed.User != nil {
			return contextProblem("contexts.document_malformed",
				fmt.Sprintf("the endpoint of the context %q embeds credentials in its URL", c.Name),
				"Remove the user information from the endpoint. A context names a credential source; "+
					"it never carries a credential.")
		}
	}
	if c.Auth.CredentialVariable != "" && !variablePattern.MatchString(c.Auth.CredentialVariable) {
		// The rejected value is not echoed: a document that put a credential
		// where a variable name belongs must not have it repeated into output.
		return contextProblem("contexts.document_malformed",
			fmt.Sprintf("the context %q does not name an environment variable as its credential source", c.Name),
			"Name the environment variable holding the credential, not the credential itself.")
	}
	// The method is not checked here. Which methods a shell implements is
	// broker policy, and a context naming one this release does not implement
	// is still a readable context: it is refused when a command needs access,
	// as a typed denial, rather than making the whole document unreadable.
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
