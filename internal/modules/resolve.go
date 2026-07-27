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

package modules

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wso2/wso2-cli/internal/semver"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// ShellIdentity is the shell-side half of a compatibility decision: the
// versions and platform an installed module must support to be launched.
type ShellIdentity struct {
	// Version is this shell's release version.
	Version semver.Version
	// ProtocolVersions are the module-contract versions this shell speaks,
	// newest first.
	ProtocolVersions []int
	// Platform is the operating system and architecture this shell runs on.
	Platform Platform
}

// Resolved is one installed module the shell may launch. Every check that does
// not require executing the module has already passed.
type Resolved struct {
	// Receipt is the validated module receipt.
	Receipt Receipt
	// ExecutablePath is the absolute path of the integrity-checked
	// executable, proven to lie inside the module's own immutable version
	// directory.
	ExecutablePath string
	// ProtocolVersion is the newest protocol version both the shell and the
	// module support.
	ProtocolVersion int
}

// Installed is one entry of receipt-backed inventory. Unlike Resolved, it is
// produced without recomputing the executable digest, because reporting
// inventory neither launches nor trusts the executable.
type Installed struct {
	Namespace string
	Version   string
	Platform  Platform
	Receipt   Receipt
}

// Resolve selects the active version of a namespace and proves it is safe to
// launch: the receipt is valid and matches the pointer, the executable lies
// inside its own version directory, the shell, protocol, and platform are
// compatible, and the executable still matches its recorded digest.
//
// The digest is recomputed on every call. A resolution that succeeded earlier
// says nothing about the executable on disk now.
func (s Store) Resolve(namespace string, shell ShellIdentity) (Resolved, error) {
	if !namespacePattern.MatchString(namespace) {
		return Resolved{}, problem.New(problem.CategoryUsage, "modules.invalid_namespace",
			fmt.Sprintf("%q is not a valid module namespace", namespace)).
			WithRecovery("Run wso2 version to see the installed modules.")
	}

	active, err := s.ReadActive(namespace)
	if err != nil {
		return Resolved{}, err
	}

	receiptPath := s.ReceiptPath(namespace, active.Version)
	receiptDigest, err := FileDigest(receiptPath)
	switch {
	case os.IsNotExist(err):
		return Resolved{}, receiptProblem("modules.receipt_missing",
			fmt.Sprintf("the active version %s of the %q module has no receipt", active.Version, namespace),
			reinstallRecovery)
	case err != nil:
		return Resolved{}, receiptProblem("modules.receipt_unreadable",
			fmt.Sprintf("the receipt of the %q module cannot be read", namespace), reinstallRecovery)
	}
	if receiptDigest != active.ReceiptSHA256 {
		return Resolved{}, receiptProblem("modules.receipt_digest_mismatch",
			fmt.Sprintf("the receipt of the %q module does not match the digest recorded in its active-version pointer", namespace),
			reinstallRecovery)
	}

	receipt, err := ReadReceipt(receiptPath)
	if err != nil {
		return Resolved{}, err
	}
	if err := checkReceiptMatchesActive(namespace, active.Version, receipt); err != nil {
		return Resolved{}, err
	}

	protocolVersion, err := negotiateProtocol(receipt, shell)
	if err != nil {
		return Resolved{}, err
	}
	if err := checkShellCompatibility(receipt, shell); err != nil {
		return Resolved{}, err
	}
	if err := checkPlatform(receipt, shell); err != nil {
		return Resolved{}, err
	}

	executablePath, err := containedExecutablePath(s.VersionDir(namespace, active.Version), receipt.Executable)
	if err != nil {
		return Resolved{}, err
	}

	executableDigest, err := FileDigest(executablePath)
	switch {
	case os.IsNotExist(err):
		return Resolved{}, receiptProblem("modules.executable_missing",
			fmt.Sprintf("the executable of the %q module is missing from the managed module store", namespace),
			reinstallRecovery)
	case err != nil:
		return Resolved{}, receiptProblem("modules.executable_unreadable",
			fmt.Sprintf("the executable of the %q module cannot be read", namespace), reinstallRecovery)
	}
	if executableDigest != receipt.ExecutableSHA256 {
		return Resolved{}, receiptProblem("modules.executable_digest_mismatch",
			fmt.Sprintf("the executable of the %q module does not match the digest recorded in its receipt", namespace),
			"Reinstall the module. The shell refuses to launch a module whose executable changed after installation.")
	}

	return Resolved{Receipt: receipt, ExecutablePath: executablePath, ProtocolVersion: protocolVersion}, nil
}

// Inventory reports every namespace in the store with its active receipt, plus
// one problem per namespace that could not be reported.
//
// Inventory never launches a module and never fails as a whole: a single broken
// installation is reported as a diagnostic so the remaining inventory still
// works offline.
func (s Store) Inventory() ([]Installed, []NamespaceProblem, error) {
	namespaces, err := s.Namespaces()
	if err != nil {
		return nil, nil, err
	}

	var installed []Installed
	var problems []NamespaceProblem
	for _, namespace := range namespaces {
		entry, err := s.inventoryEntry(namespace)
		if err != nil {
			problems = append(problems, NamespaceProblem{Namespace: namespace, Problem: asProblem(err)})
			continue
		}
		installed = append(installed, entry)
	}
	return installed, problems, nil
}

// NamespaceProblem is one namespace that could not be reported as inventory.
type NamespaceProblem struct {
	Namespace string
	Problem   problem.Problem
}

func (s Store) inventoryEntry(namespace string) (Installed, error) {
	active, err := s.ReadActive(namespace)
	if err != nil {
		return Installed{}, err
	}
	receipt, err := ReadReceipt(s.ReceiptPath(namespace, active.Version))
	if err != nil {
		return Installed{}, err
	}
	if err := checkReceiptMatchesActive(namespace, active.Version, receipt); err != nil {
		return Installed{}, err
	}
	return Installed{
		Namespace: namespace,
		Version:   receipt.ModuleVersion,
		Platform:  receipt.Platform,
		Receipt:   receipt,
	}, nil
}

// checkReceiptMatchesActive proves the receipt describes the installation the
// active-version pointer selected, so one executable cannot be resolved under
// another module's namespace or version.
func checkReceiptMatchesActive(namespace, activeVersion string, receipt Receipt) error {
	if receipt.Namespace != namespace {
		return receiptProblem("modules.receipt_namespace_mismatch",
			fmt.Sprintf("the receipt installed for the %q module declares namespace %q", namespace, receipt.Namespace),
			reinstallRecovery)
	}
	if receipt.ModuleVersion != activeVersion {
		return receiptProblem("modules.receipt_version_mismatch",
			fmt.Sprintf("the receipt of the active %q module declares version %s, but version %s is active",
				namespace, receipt.ModuleVersion, activeVersion),
			reinstallRecovery)
	}
	return nil
}

// negotiateProtocol selects the newest protocol version both sides support.
func negotiateProtocol(receipt Receipt, shell ShellIdentity) (int, error) {
	for _, candidate := range shell.ProtocolVersions {
		if receipt.SupportsProtocol(candidate) {
			return candidate, nil
		}
	}
	return 0, problem.New(problem.CategoryModuleTrust, "modules.incompatible_protocol",
		fmt.Sprintf("the %q module supports module-contract protocol %s, and this shell speaks %s",
			receipt.Namespace, formatVersions(receipt.Compatibility.ProtocolVersions), formatVersions(shell.ProtocolVersions))).
		WithRecovery("Update the module or the WSO2 CLI so both support one protocol version.")
}

func checkShellCompatibility(receipt Receipt, shell ShellIdentity) error {
	supported, err := receipt.ShellRange()
	if err != nil {
		return receiptProblem("modules.receipt_malformed",
			fmt.Sprintf("module receipt declares an unreadable shell compatibility range %q", receipt.Compatibility.Shell),
			reinstallRecovery)
	}
	if !supported.Contains(shell.Version) {
		return problem.New(problem.CategoryModuleTrust, "modules.incompatible_shell",
			fmt.Sprintf("the %q module requires a WSO2 CLI shell matching %q, and this shell is %s",
				receipt.Namespace, supported.String(), shell.Version)).
			WithRecovery("Update the module or the WSO2 CLI so the shell version is supported.")
	}
	return nil
}

func checkPlatform(receipt Receipt, shell ShellIdentity) error {
	if receipt.Platform != shell.Platform {
		return problem.New(problem.CategoryModuleTrust, "modules.incompatible_platform",
			fmt.Sprintf("the installed %q module targets %s, and this shell runs on %s",
				receipt.Namespace, receipt.Platform, shell.Platform)).
			WithRecovery("Reinstall the module for this operating system and architecture.")
	}
	return nil
}

// containedExecutablePath joins a receipt's relative executable path to its
// version directory and proves the result stays inside that directory, after
// symbolic links are resolved.
func containedExecutablePath(versionDir, executable string) (string, error) {
	absoluteVersionDir, err := filepath.Abs(versionDir)
	if err != nil {
		return "", receiptProblem("modules.store_unreadable",
			"the managed module store path cannot be resolved", reinstallRecovery)
	}
	candidate := filepath.Join(absoluteVersionDir, filepath.FromSlash(executable))
	if !WithinDir(absoluteVersionDir, candidate) {
		return "", receiptProblem("modules.receipt_path_escape",
			fmt.Sprintf("the module receipt points at %q, which is outside the module's version directory", executable),
			pathEscapeRecovery)
	}

	// A symbolic link inside the version directory could still point out of
	// it, so containment is re-proven against the real path.
	realCandidate, err := filepath.EvalSymlinks(candidate)
	if os.IsNotExist(err) {
		return candidate, nil
	}
	if err != nil {
		return "", receiptProblem("modules.executable_unreadable",
			"the module executable path cannot be resolved", reinstallRecovery)
	}
	realVersionDir, err := filepath.EvalSymlinks(absoluteVersionDir)
	if err != nil {
		return "", receiptProblem("modules.store_unreadable",
			"the managed module store path cannot be resolved", reinstallRecovery)
	}
	if !WithinDir(realVersionDir, realCandidate) {
		return "", receiptProblem("modules.receipt_path_escape",
			fmt.Sprintf("the module receipt points at %q, which resolves outside the module's version directory", executable),
			pathEscapeRecovery)
	}
	return realCandidate, nil
}

// WithinDir reports whether candidate is a path strictly inside dir. Both paths
// must already be absolute and lexically clean.
func WithinDir(dir, candidate string) bool {
	relative, err := filepath.Rel(dir, candidate)
	if err != nil {
		return false
	}
	if relative == "." || relative == ".." {
		return false
	}
	return !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func formatVersions(versions []int) string {
	if len(versions) == 0 {
		return "no version"
	}
	rendered := make([]string, 0, len(versions))
	for _, version := range versions {
		rendered = append(rendered, fmt.Sprintf("v%d", version))
	}
	return strings.Join(rendered, ", ")
}

// asProblem converts an error into a problem, so shell-owned rendering always
// has a typed failure to work with.
func asProblem(err error) problem.Problem {
	var typed problem.Problem
	if errors.As(err, &typed) {
		return typed
	}
	return problem.New(problem.CategoryModuleProcess, "modules.unexpected_failure", err.Error())
}
