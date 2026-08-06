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

// Package fixture writes context documents into an isolated state root for
// tests and the acceptance harness.
//
// The shell reads contexts and never writes them, so this is the only writer in
// the repository. It is deliberately test infrastructure: it requires an
// explicit state root and refuses to write into a developer's real WSO2 state.
package fixture

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/state"
)

// LegacyDocument is the schema version 1 document shape, kept here so the
// acceptance harness can still install v1 documents and exercise the shell's
// compatibility read. The shell itself never writes this shape.
type LegacyDocument struct {
	SchemaVersion  int             `json:"schemaVersion"`
	DefaultContext string          `json:"defaultContext"`
	Contexts       []LegacyContext `json:"contexts"`
}

// LegacyContext is one v1 context.
type LegacyContext struct {
	Name           string     `json:"name"`
	OrganizationID string     `json:"organizationId"`
	Endpoint       string     `json:"endpoint"`
	Auth           LegacyAuth `json:"auth"`
}

// LegacyAuth is a v1 context's authentication arrangement.
type LegacyAuth struct {
	Method             string `json:"method"`
	CredentialVariable string `json:"credentialVariable"`
}

// Install writes a schema version 1 document into the given state root.
//
// The written bytes are decoded back through the shell's own reader, so a
// fixture cannot install a document the shell would refuse to read.
func Install(stateRoot string, document LegacyDocument) error {
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("fixture: cannot encode the legacy context document: %w", err)
	}
	data = append(data, '\n')
	if _, err := contexts.Decode(data); err != nil {
		return fmt.Errorf("fixture: %w", err)
	}
	return write(stateRoot, data)
}

// WriteV2 writes a schema version 2 document into the state root.
//
// The document is validated on the way out, so a fixture cannot install a
// context the shell would refuse to read.
func WriteV2(stateRoot string, document contexts.Document) error {
	data, err := document.Encode()
	if err != nil {
		return fmt.Errorf("fixture: %w", err)
	}
	return write(stateRoot, data)
}

func write(stateRoot string, data []byte) error {
	if stateRoot == "" {
		return fmt.Errorf("fixture: a state root is required")
	}
	if err := state.GuardIsolated("write", stateRoot); err != nil {
		return fmt.Errorf("fixture: %w", err)
	}
	path := contexts.Path(stateRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("fixture: cannot create the context directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("fixture: cannot write the context document: %w", err)
	}
	return nil
}
