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
	"net/url"
)

// SchemaVersionLegacy is the architecture proof's schema. Documents written in
// it stay readable through a compatibility mapping onto synthetic identities;
// they are never written back.
const SchemaVersionLegacy = 1

// The private mirror of the v1 document shape, field names and JSON tags
// exactly as the architecture proof wrote them.
type legacyDocument struct {
	SchemaVersion  int             `json:"schemaVersion"`
	DefaultContext string          `json:"defaultContext"`
	Contexts       []legacyContext `json:"contexts"`
}

type legacyContext struct {
	Name           string     `json:"name"`
	OrganizationID string     `json:"organizationId"`
	Endpoint       string     `json:"endpoint"`
	Auth           legacyAuth `json:"auth"`
}

type legacyAuth struct {
	Method             string `json:"method"`
	CredentialVariable string `json:"credentialVariable"`
}

// decodeLegacy maps a v1 document onto the v2 in-memory shape.
//
// Each v1 context becomes a synthetic identity carrying the v1 method and
// credential variable plus a context referencing it by the same name. The v1
// validation rules are enforced with the same problem codes as before.
func decodeLegacy(data []byte) (Document, error) {
	var legacy legacyDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&legacy); err != nil {
		return Document{}, malformed("is not valid JSON")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return Document{}, malformed("contains more than one JSON document")
	}
	if err := legacy.validate(); err != nil {
		return Document{}, err
	}

	document := Document{
		SchemaVersion:  legacy.SchemaVersion,
		DefaultContext: legacy.DefaultContext,
	}
	for _, candidate := range legacy.Contexts {
		identity := Identity{
			Name: candidate.Name,
			Type: "onprem",
			Auth: IdentityAuth{
				Kind:               candidate.Auth.Method,
				CredentialVariable: candidate.Auth.CredentialVariable,
			},
			synthetic: true,
		}
		if candidate.Endpoint != "" {
			identity.Products = map[string]Product{
				"reference": {Endpoint: candidate.Endpoint},
			}
		}
		document.Identities = append(document.Identities, identity)
		document.Contexts = append(document.Contexts, Context{
			Name:         candidate.Name,
			Identity:     candidate.Name,
			Organization: candidate.OrganizationID,
		})
	}
	return document, nil
}

// validate enforces the v1 rules exactly as the architecture proof did.
func (d legacyDocument) validate() error {
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

func (c legacyContext) validate() error {
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
