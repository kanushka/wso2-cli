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

// Package fixture installs modules into an isolated managed module store for
// tests and the acceptance harness.
//
// This package is deliberately test infrastructure and is not reachable from
// any shell command: the architecture proof has no public unverified
// local-install command. Every function requires an explicit store root and
// refuses to write to a developer's real WSO2 state.
package fixture

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/state"
)

// Module describes one module installation to create.
type Module struct {
	// Namespace is the top-level command name the module owns.
	Namespace string
	// Version is the module's release version and its version directory name.
	Version string
	// ExecutableName is the executable's name inside the version directory.
	// It defaults to "wso2-module-<namespace>".
	ExecutableName string
	// SourcePath is a built executable to copy. When empty, Contents is
	// written instead.
	SourcePath string
	// Contents is the executable's content when SourcePath is empty.
	Contents []byte
	// ShellRange is the supported shell range. It defaults to a range that
	// accepts every shell version, including a prerelease development build.
	ShellRange string
	// ProtocolVersions are the supported protocol versions. They default to
	// protocol 1, which is the older end of the shell's window. An
	// installation whose executable is a real module built against the SDK
	// must set what that build speaks instead, because the receipt has to
	// agree with the executable at the handshake.
	ProtocolVersions []int
	// OS and Arch default to the running platform.
	OS   string
	Arch string
	// AuthAudiences and AuthScopes are the declared authentication
	// capabilities.
	AuthAudiences []string
	AuthScopes    []string
	// ExecutablePathOverride replaces the receipt's executable path without
	// changing where the executable is written. Tests use it to prove that a
	// receipt cannot redirect execution outside its version directory.
	ExecutablePathOverride string
	// Inactive installs the version without activating it, leaving the
	// namespace with no active-version pointer.
	Inactive bool
}

// Install writes one module version and, unless the module is inactive, the
// active-version pointer selecting it. It returns the written receipt.
func Install(storeRoot string, module Module) (modules.Receipt, error) {
	store, err := guardedStore(storeRoot)
	if err != nil {
		return modules.Receipt{}, err
	}
	module = module.withDefaults()

	versionDir := store.VersionDir(module.Namespace, module.Version)
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		return modules.Receipt{}, fmt.Errorf("fixture: cannot create the version directory: %w", err)
	}

	contents := module.Contents
	if module.SourcePath != "" {
		read, readErr := os.ReadFile(module.SourcePath)
		if readErr != nil {
			return modules.Receipt{}, fmt.Errorf("fixture: cannot read the source executable: %w", readErr)
		}
		contents = read
	}
	executablePath := filepath.Join(versionDir, module.ExecutableName)
	if err := os.WriteFile(executablePath, contents, 0o755); err != nil {
		return modules.Receipt{}, fmt.Errorf("fixture: cannot write the module executable: %w", err)
	}

	receiptExecutable := module.ExecutableName
	if module.ExecutablePathOverride != "" {
		receiptExecutable = module.ExecutablePathOverride
	}
	receipt := modules.Receipt{
		SchemaVersion: modules.ReceiptSchemaVersion,
		Namespace:     module.Namespace,
		ModuleVersion: module.Version,
		Executable:    receiptExecutable,
		Compatibility: modules.Compatibility{
			Shell:            module.ShellRange,
			ProtocolVersions: module.ProtocolVersions,
		},
		Platform: modules.Platform{OS: module.OS, Arch: module.Arch},
		Capabilities: modules.Capabilities{
			AuthAudiences: module.AuthAudiences,
			AuthScopes:    module.AuthScopes,
		},
		ExecutableSHA256: modules.BytesDigest(contents),
	}
	if err := WriteReceipt(storeRoot, receipt); err != nil {
		return modules.Receipt{}, err
	}
	if module.Inactive {
		return receipt, nil
	}
	if err := Activate(storeRoot, module.Namespace, module.Version); err != nil {
		return modules.Receipt{}, err
	}
	return receipt, nil
}

// WriteReceipt writes a receipt for an already installed version. Tests use it
// to install a receipt that disagrees with the executable on disk.
func WriteReceipt(storeRoot string, receipt modules.Receipt) error {
	if _, err := guardedStore(storeRoot); err != nil {
		return err
	}
	data, err := receipt.Encode()
	if err != nil {
		return err
	}
	return WriteRawReceipt(storeRoot, receipt.Namespace, receipt.ModuleVersion, data)
}

// WriteRawReceipt writes arbitrary receipt bytes. Tests use it to install a
// malformed receipt that the shell must reject rather than interpret.
func WriteRawReceipt(storeRoot, namespace, version string, data []byte) error {
	store, err := guardedStore(storeRoot)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(store.VersionDir(namespace, version), 0o755); err != nil {
		return fmt.Errorf("fixture: cannot create the version directory: %w", err)
	}
	if err := os.WriteFile(store.ReceiptPath(namespace, version), data, 0o644); err != nil {
		return fmt.Errorf("fixture: cannot write the module receipt: %w", err)
	}
	return nil
}

// Activate writes the active-version pointer for an installed version,
// recording the digest of the receipt as installed.
func Activate(storeRoot, namespace, version string) error {
	store, err := guardedStore(storeRoot)
	if err != nil {
		return err
	}
	receiptDigest, err := modules.FileDigest(store.ReceiptPath(namespace, version))
	if err != nil {
		return fmt.Errorf("fixture: cannot digest the module receipt: %w", err)
	}
	active := modules.Active{
		SchemaVersion: modules.ActiveSchemaVersion,
		Namespace:     namespace,
		Version:       version,
		ReceiptSHA256: receiptDigest,
	}
	data, err := active.Encode()
	if err != nil {
		return err
	}
	if err := os.WriteFile(store.ActivePath(namespace), data, 0o644); err != nil {
		return fmt.Errorf("fixture: cannot write the active-version pointer: %w", err)
	}
	return nil
}

// WriteRawActive writes arbitrary active-version pointer bytes. Tests use it to
// install a malformed pointer.
func WriteRawActive(storeRoot, namespace string, data []byte) error {
	store, err := guardedStore(storeRoot)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(store.NamespaceDir(namespace), 0o755); err != nil {
		return fmt.Errorf("fixture: cannot create the namespace directory: %w", err)
	}
	if err := os.WriteFile(store.ActivePath(namespace), data, 0o644); err != nil {
		return fmt.Errorf("fixture: cannot write the active-version pointer: %w", err)
	}
	return nil
}

// TamperExecutable rewrites an installed executable so its content no longer
// matches the digest recorded in its receipt.
func TamperExecutable(storeRoot, namespace, version, executableName string, contents []byte) error {
	store, err := guardedStore(storeRoot)
	if err != nil {
		return err
	}
	path := filepath.Join(store.VersionDir(namespace, version), executableName)
	if err := os.WriteFile(path, contents, 0o755); err != nil {
		return fmt.Errorf("fixture: cannot tamper with the module executable: %w", err)
	}
	return nil
}

func (m Module) withDefaults() Module {
	if m.ExecutableName == "" {
		m.ExecutableName = "wso2-module-" + m.Namespace
	}
	if m.ShellRange == "" {
		// A prerelease sorts below its own release, so ">=0.0.0" would reject
		// the default 0.0.0-dev shell build.
		m.ShellRange = ">=0.0.0-0"
	}
	if len(m.ProtocolVersions) == 0 {
		m.ProtocolVersions = []int{1}
	}
	if m.OS == "" {
		m.OS = runtime.GOOS
	}
	if m.Arch == "" {
		m.Arch = runtime.GOARCH
	}
	if m.Contents == nil && m.SourcePath == "" {
		m.Contents = []byte("#!/bin/sh\nexit 0\n")
	}
	return m
}

// guardedStore proves the root is isolated, then opens the store there.
func guardedStore(storeRoot string) (modules.Store, error) {
	if err := guardIsolatedRoot(storeRoot); err != nil {
		return modules.Store{}, err
	}
	return modules.NewStore(storeRoot), nil
}

// guardIsolatedRoot refuses to install anywhere that could be real WSO2 state,
// so a mistaken test can never modify a developer's configuration.
func guardIsolatedRoot(storeRoot string) error {
	if storeRoot == "" {
		return fmt.Errorf("fixture: a managed module store root is required")
	}
	if err := state.GuardIsolated("install", storeRoot); err != nil {
		return fmt.Errorf("fixture: %w", err)
	}
	return nil
}
