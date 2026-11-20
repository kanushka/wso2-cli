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

// Package bundle describes a test bundle: a shell and one product module,
// prebuilt for one platform, with the installer that sets them up on a
// machine that has neither this repository nor a Go toolchain.
//
// A bundle is contributor tooling for trying a module before it is released.
// It installs the module the way a release does, through a catalog, but that
// catalog is served on loopback by the bundle's own installer for the length
// of the install, so nothing has to be published.
package bundle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wso2/wso2-cli/internal/modules"
)

// ManifestName is the manifest's file name at the root of a bundle.
const ManifestName = "bundle.json"

// InstallerName is the bundle installer's program name, without a platform
// suffix.
const InstallerName = "install"

// Manifest is what a bundle holds, written by the bundle builder and read by
// the installer it ships with.
type Manifest struct {
	// Namespace is the product module the bundle carries.
	Namespace string `json:"namespace"`
	// ModuleVersion is the version the module archive was built as.
	ModuleVersion string `json:"moduleVersion"`
	// ShellVersion is the version the shell was built as.
	ShellVersion string `json:"shellVersion"`
	// CommandName is the shell's program name, such as ws.
	CommandName string `json:"commandName"`
	// Platform is the one platform every program in the bundle was built for.
	Platform modules.Platform `json:"platform"`
	// Archive is the module archive's file name at the bundle's root.
	Archive string `json:"archive"`
	// ContextFile is an input file of contexts at the bundle's root, applied
	// at install when no context named ContextName exists yet; empty for none.
	ContextFile string `json:"contextFile,omitempty"`
	// ContextName is the context ContextFile declares and the install selects.
	ContextName string `json:"contextName,omitempty"`
}

// ExecutableName is name as an executable file on platform.
func ExecutableName(name string, platform modules.Platform) string {
	if platform.OS == "windows" {
		return name + ".exe"
	}
	return name
}

// Read reads the manifest of the bundle at dir.
func Read(dir string) (Manifest, error) {
	content, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		return Manifest{}, fmt.Errorf("bundle: reading %s: %w", ManifestName, err)
	}
	var manifest Manifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("bundle: %s is not a bundle manifest: %w", ManifestName, err)
	}
	if manifest.Namespace == "" || manifest.ModuleVersion == "" || manifest.ShellVersion == "" ||
		manifest.CommandName == "" || manifest.Archive == "" || manifest.Platform.OS == "" {
		return Manifest{}, fmt.Errorf("bundle: %s leaves out a required member", ManifestName)
	}
	return manifest, nil
}

// Write writes the manifest into the bundle at dir.
func (m Manifest) Write(dir string) error {
	content, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ManifestName), append(content, '\n'), 0o644)
}
