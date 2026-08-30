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

// Package install installs a product module from the module catalog into the
// managed module store.
//
// The order of the steps is the point. Nothing is written into the store until
// the downloaded archive has been checked against the digest the catalog entry
// records, and the activation pointer is written last, so a failure at any
// point leaves no executable and no receipt behind and nothing to clean up.
//
// What the digest proves is that the archive is the one the catalog entry
// describes. It does not prove the entry is authentic: artifacts are unsigned,
// and integrity rests on the digest together with HTTPS.
package install

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/wso2/wso2-cli/internal/atomicfile"
	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// maxArchiveEntries and maxExtractedBytes bound what one archive may expand
// into, so a hostile or damaged archive cannot fill a user's disk.
const (
	maxArchiveEntries = 4096
	maxExtractedBytes = 512 << 20
)

// Request is one install: the module to install and the policy to select its
// version under.
type Request struct {
	Namespace string
	Policy    catalog.Policy
}

// Installer installs modules from one catalog origin into one store.
type Installer struct {
	Store  modules.Store
	Client catalog.Client
	Shell  modules.ShellIdentity
}

// Installed reports what an install activated.
type Installed struct {
	Namespace string
	Version   string
	Platform  modules.Platform
}

// Run installs one module version and activates it.
//
// It makes exactly two catalog requests: the index, which names the module and
// where its history is published, and that history, which is fetched only
// because a specific version must be selected here. No other shell operation
// fetches either.
func (i Installer) Run(ctx context.Context, request Request) (Installed, error) {
	if !modules.ValidNamespace(request.Namespace) {
		return Installed{}, problem.New(problem.CategoryUsage, "modules.invalid_namespace",
			fmt.Sprintf("%q is not a valid module namespace", request.Namespace)).
			WithRecovery("Give a module name such as reference.")
	}

	index, err := i.Client.Index(ctx)
	if err != nil {
		return Installed{}, err
	}
	return i.runWithIndex(ctx, index, request)
}

// runWithIndex installs one module version from an index already read, which is
// how an update run that moves several modules still costs one index request
// however many it moves.
func (i Installer) runWithIndex(ctx context.Context, index catalog.Index, request Request) (Installed, error) {
	entry, err := index.Module(request.Namespace)
	if err != nil {
		return Installed{}, err
	}
	history, err := i.Client.Namespace(ctx, entry)
	if err != nil {
		return Installed{}, err
	}
	selection, err := catalog.Select(history, request.Policy, i.Shell)
	if err != nil {
		return Installed{}, err
	}

	archive, err := i.Client.Download(ctx, selection.Artifact.URL)
	if err != nil {
		return Installed{}, err
	}
	if err := verify(request.Namespace, selection, archive); err != nil {
		return Installed{}, err
	}

	if err := i.activate(request.Namespace, selection, request.Policy, archive); err != nil {
		return Installed{}, err
	}
	return Installed{
		Namespace: request.Namespace,
		Version:   selection.Version.Version,
		Platform:  i.Shell.Platform,
	}, nil
}

// verify checks the downloaded archive against the entry that named it, before
// a single byte of it is written into the store.
func verify(namespace string, selection catalog.Selection, archive []byte) error {
	if int64(len(archive)) != selection.Artifact.Size {
		return problem.New(problem.CategoryModuleTrust, "modules.artifact_size_mismatch",
			fmt.Sprintf("the %s artifact of %s %s is %d bytes, and the module catalog publishes %d",
				selection.Artifact.Platform, namespace, selection.Version.Version,
				len(archive), selection.Artifact.Size)).
			WithRecovery(corruptedRecovery)
	}
	digest := sha256.Sum256(archive)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), selection.Artifact.SHA256) {
		return problem.New(problem.CategoryModuleTrust, "modules.artifact_digest_mismatch",
			fmt.Sprintf("the downloaded %s artifact of %s %s does not match the digest the module catalog publishes; "+
				"nothing was installed",
				selection.Artifact.Platform, namespace, selection.Version.Version)).
			WithRecovery(corruptedRecovery)
	}
	return nil
}

const corruptedRecovery = "The download was corrupted or substituted. Try again; if it keeps failing, report it to the module's maintainers."

// activate stages the verified archive, moves it into its immutable version
// directory, writes the receipt and the version policy, and points the
// active-version pointer at it.
//
// Everything before the last step happens where a failure can be swept away, so
// a refused install is indistinguishable from one that never ran.
func (i Installer) activate(namespace string, selection catalog.Selection,
	requested catalog.Policy, archive []byte) error {
	namespaceDir := i.Store.NamespaceDir(namespace)
	_, existedErr := os.Stat(namespaceDir)
	namespaceExisted := existedErr == nil

	if err := os.MkdirAll(namespaceDir, 0o755); err != nil {
		return storeFailure("creating the module directory", err)
	}
	staging, err := os.MkdirTemp(namespaceDir, ".staging-")
	if err != nil {
		return storeFailure("creating a staging directory", err)
	}
	// Containment is proven against an absolute directory, because a relative
	// one would make an escaping archive entry look contained.
	staging, err = filepath.Abs(staging)
	if err != nil {
		return storeFailure("resolving the staging directory", err)
	}
	versionDir := i.Store.VersionDir(namespace, selection.Version.Version)
	// replaced holds an installation of this same version that was moved aside,
	// so a failure after the move can put it back. Without it, reinstalling a
	// version and failing would take away the installation that was working.
	replaced := ""
	installed := false
	// The policy this install would replace, so a failure leaves the module
	// following what it followed before. A failed update must change nothing at
	// all, the channel and pin included.
	previousPolicy, previousPolicyExists, previousPolicyAbsent := readPolicyDocument(
		i.Store.PolicyPath(namespace))
	defer func() {
		_ = os.RemoveAll(staging)
		if installed {
			_ = os.RemoveAll(replaced)
			return
		}
		if previousPolicyExists {
			_ = writeAtomically("restoring the version policy",
				i.Store.PolicyPath(namespace), previousPolicy)
		} else if previousPolicyAbsent {
			_ = os.Remove(i.Store.PolicyPath(namespace))
		}
		// A failed install leaves nothing behind: the version it was writing
		// goes, whatever it displaced comes back, and a namespace this run
		// created goes with it.
		_ = os.RemoveAll(versionDir)
		if replaced != "" {
			_ = os.Rename(replaced, versionDir)
			return
		}
		if !namespaceExisted {
			_ = os.RemoveAll(namespaceDir)
		}
	}()

	executableName := ExecutableName(namespace, i.Shell.Platform)
	if err := extract(archive, staging, executableName); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(versionDir), 0o755); err != nil {
		return storeFailure("creating the versions directory", err)
	}
	if _, err := os.Stat(versionDir); err == nil {
		replaced = versionDir + ".replaced"
		if err := os.RemoveAll(replaced); err != nil {
			return storeFailure("clearing a displaced version directory", err)
		}
		if err := os.Rename(versionDir, replaced); err != nil {
			replaced = ""
			return storeFailure("moving the installed version aside", err)
		}
	}
	if err := os.Rename(staging, versionDir); err != nil {
		return storeFailure("moving the verified module into place", err)
	}

	receipt, err := i.receipt(namespace, selection, versionDir, executableName)
	if err != nil {
		return err
	}
	encoded, err := receipt.Encode()
	if err != nil {
		return err
	}
	if err := os.WriteFile(i.Store.ReceiptPath(namespace, receipt.ModuleVersion), encoded, 0o644); err != nil {
		return storeFailure("writing the module receipt", err)
	}

	if err := i.recordPolicy(namespace, selection, requested); err != nil {
		return err
	}

	active := modules.Active{
		SchemaVersion: modules.ActiveSchemaVersion,
		Namespace:     namespace,
		Version:       receipt.ModuleVersion,
		ReceiptSHA256: modules.BytesDigest(encoded),
	}
	activeDocument, err := active.Encode()
	if err != nil {
		return err
	}
	if err := writeAtomically("writing the active-version pointer",
		i.Store.ActivePath(namespace), activeDocument); err != nil {
		return err
	}
	installed = true
	return nil
}

// recordPolicy writes what this install asked for beside the installation, so a
// later update run reads the channel and the pin the module was installed under
// rather than needing to be told again.
func (i Installer) recordPolicy(namespace string, selection catalog.Selection,
	requested catalog.Policy) error {
	policy := modules.Policy{
		SchemaVersion: modules.PolicySchemaVersion,
		Namespace:     namespace,
		Channel:       requested.Channel,
	}
	if requested.Version != "" {
		// The pin records the version that was actually selected rather than
		// the string the user typed, so the policy names a version the store
		// holds.
		policy.PinnedVersion = selection.Version.Version
	}
	document, err := policy.Encode()
	if err != nil {
		return err
	}
	return writeAtomically("writing the version policy", i.Store.PolicyPath(namespace), document)
}

// readPolicyDocument reads a policy document as bytes, for putting back exactly
// as it stood. It reports absence rather than failing: a module installed
// before any policy was recorded has none.
//
// absent separates the two ways there is nothing to put back. A policy that is
// genuinely missing may be removed again on rollback; one that merely could not
// be read must be left alone, because deleting it would lose the channel and
// pin that a failed install is required to leave untouched.
func readPolicyDocument(path string) (content []byte, exists, absent bool) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, false, errors.Is(err, fs.ErrNotExist)
	}
	return content, true, false
}

// receipt records what the catalog published, so what the shell later gates a
// launch on is what was published rather than what the archive contained.
func (i Installer) receipt(namespace string, selection catalog.Selection,
	versionDir, executableName string) (modules.Receipt, error) {
	digest, err := modules.FileDigest(filepath.Join(versionDir, executableName))
	if err != nil {
		return modules.Receipt{}, storeFailure("reading the installed executable", err)
	}
	receipt := modules.Receipt{
		SchemaVersion: modules.ReceiptSchemaVersion,
		Namespace:     namespace,
		ModuleVersion: selection.Version.Version,
		Executable:    executableName,
		Compatibility: modules.Compatibility{
			Shell:            selection.Version.Compatibility.Shell,
			ProtocolVersions: selection.Version.Compatibility.ProtocolVersions,
		},
		Capabilities:     selection.Version.Capabilities,
		Platform:         i.Shell.Platform,
		ExecutableSHA256: digest,
	}
	if err := receipt.Validate(); err != nil {
		return modules.Receipt{}, err
	}
	return receipt, nil
}

// ExecutableName is the name a module's executable carries inside its published
// archive and inside its version directory. It is a convention rather than a
// catalog field, so an archive that does not follow it is refused rather than
// searched.
func ExecutableName(namespace string, platform modules.Platform) string {
	name := "wso2-module-" + namespace
	if platform.OS == "windows" {
		return name + ".exe"
	}
	return name
}

// extract unpacks a verified archive into a directory, refusing anything that
// is not a plain file or directory under it. An entry naming an absolute path,
// climbing out of the destination, or carrying a link is refused rather than
// sanitized: the archives the catalog publishes carry none of them.
func extract(archive []byte, destination, executableName string) error {
	stream, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return malformedArtifact(fmt.Sprintf("it is not a gzip stream: %v", err))
	}
	defer func() {
		_ = stream.Close()
	}()

	reader := tar.NewReader(stream)
	entries, written := 0, int64(0)
	foundExecutable := false
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return malformedArtifact(fmt.Sprintf("it is not a readable tar archive: %v", err))
		}
		entries++
		if entries > maxArchiveEntries {
			return malformedArtifact(fmt.Sprintf("it carries more than %d entries", maxArchiveEntries))
		}
		target, err := safePath(destination, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return storeFailure("creating an extracted directory", err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return storeFailure("creating an extracted directory", err)
			}
			size, err := writeEntry(target, reader, maxExtractedBytes-written, header.Name == executableName)
			if err != nil {
				return err
			}
			written += size
			if header.Name == executableName {
				foundExecutable = true
			}
		default:
			return malformedArtifact(fmt.Sprintf("it carries %q, which is neither a file nor a directory",
				header.Name))
		}
	}
	if !foundExecutable {
		return malformedArtifact(fmt.Sprintf("it carries no %s", executableName))
	}
	return nil
}

// writeEntry copies one archived file, refusing an archive that expands past
// what the shell is willing to write.
func writeEntry(target string, reader io.Reader, remaining int64, executable bool) (int64, error) {
	mode := os.FileMode(0o644)
	if executable {
		mode = 0o755
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return 0, storeFailure("writing an extracted file", err)
	}
	written, err := io.Copy(file, io.LimitReader(reader, remaining+1))
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return 0, storeFailure("writing an extracted file", err)
	}
	if written > remaining {
		// The budget is the whole archive's, so exceeding what is left of it is
		// reported as the total a module archive may occupy rather than as this
		// entry's share of it.
		return 0, malformedArtifact(fmt.Sprintf("it expands past the %d bytes a module archive may occupy",
			int64(maxExtractedBytes)))
	}
	return written, nil
}

// safePath resolves one archive entry's name inside the destination.
func safePath(destination, name string) (string, error) {
	if name == "" || path.IsAbs(name) || filepath.IsAbs(name) || strings.Contains(name, "\\") {
		return "", malformedArtifact(fmt.Sprintf("it carries the unsafe path %q", name))
	}
	target := filepath.Join(destination, filepath.FromSlash(path.Clean(name)))
	if !modules.WithinDir(destination, target) {
		return "", malformedArtifact(fmt.Sprintf("it carries %q, which is outside the module's directory", name))
	}
	return target, nil
}

// writeAtomically replaces a store document in one step, so a reader never
// sees a half written document. action names what is being written and reaches
// the user in the failure, so the store's two documents are told apart when one
// cannot be written.
//
// The store's documents are 0644: every other file beside them is, and neither
// document written here is more secret than the receipts they sit next to.
func writeAtomically(action, target string, content []byte) error {
	if err := atomicfile.Write(target, content, 0o644); err != nil {
		return storeFailure(action, err)
	}
	return nil
}

func malformedArtifact(detail string) problem.Problem {
	return problem.New(problem.CategoryModuleTrust, "modules.artifact_malformed",
		"the downloaded module artifact cannot be installed: "+detail).
		WithRecovery("Report this to the module's maintainers. The archive was the one the catalog describes, but it is not the shape a module archive has.")
}

func storeFailure(action string, err error) problem.Problem {
	return problem.New(problem.CategoryModuleProcess, "modules.install_failed",
		fmt.Sprintf("%s failed: %v", action, err)).
		WithRecovery("Check the permissions on the WSO2 CLI state directory and try again.")
}
