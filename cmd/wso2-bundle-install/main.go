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

// Command install sets up a test bundle on the machine it runs on: it copies
// the bundle's shell into the WSO2 state root, installs the bundle's product
// module from a catalog it serves on loopback for the length of the install,
// and records the bundle's contexts. It needs nothing else on the machine: no
// repository, no Go toolchain and no published catalog.
//
// It is built for each platform by cmd/wso2-bundle and shipped inside the
// bundle, never in a released shell.
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/wso2/wso2-cli/internal/bundle"
	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/devorigin"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/semver"
	"github.com/wso2/wso2-cli/internal/state"
	"github.com/wso2/wso2-cli/sdk/protocol"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "install: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	dir, err := bundleDir()
	if err != nil {
		return err
	}
	manifest, err := bundle.Read(dir)
	if err != nil {
		return err
	}
	here := modules.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if manifest.Platform != here {
		return fmt.Errorf("this bundle is for %s/%s, and this machine is %s/%s; download the bundle for %s/%s",
			manifest.Platform.OS, manifest.Platform.Arch, here.OS, here.Arch, here.OS, here.Arch)
	}
	stateRoot, err := state.Root()
	if err != nil {
		return err
	}

	binDir := filepath.Join(stateRoot, "bin")
	shellName := bundle.ExecutableName(manifest.CommandName, here)
	shell := filepath.Join(binDir, shellName)
	if err := copyExecutable(filepath.Join(dir, shellName), shell); err != nil {
		return err
	}
	fmt.Printf("Installed %s %s at %s.\n", manifest.CommandName, manifest.ShellVersion, shell)

	// A module already installed at this version is taken off first, so a
	// bundle can be installed again over itself. --yes because removing asks,
	// and the installer's shell has no terminal to ask at: without it the
	// removal is refused and nothing is taken off.
	_ = runShell(shell, stateRoot, io.Discard, "product", "remove", manifest.Namespace, "--yes")

	declaration, err := declarationOf(dir, manifest.Namespace)
	if err != nil {
		return err
	}
	archive, err := os.ReadFile(filepath.Join(dir, manifest.Archive))
	if err != nil {
		return fmt.Errorf("reading the %s module archive: %w", manifest.Namespace, err)
	}
	shellVersion, err := semver.Parse(manifest.ShellVersion)
	if err != nil {
		return fmt.Errorf("the bundle names the shell version %q, which cannot be read: %w",
			manifest.ShellVersion, err)
	}
	result, err := devorigin.InstallPrebuilt(context.Background(), devorigin.PrebuiltRequest{
		Declaration: declaration,
		Version:     manifest.ModuleVersion,
		Archive:     archive,
		StateRoot:   stateRoot,
		Shell: modules.ShellIdentity{
			Version: shellVersion,
			// The shell was built from the same checkout as this installer,
			// so it speaks exactly the protocols this build of the SDK does.
			ProtocolVersions: protocol.Supported(),
			Platform:         here,
		},
	})
	if err != nil {
		return err
	}
	fmt.Printf("Installed the %s module %s, verified against its own digest.\n", result.Namespace, result.Version)

	if manifest.ContextFile != "" {
		if err := recordContext(shell, stateRoot, dir, manifest); err != nil {
			return err
		}
	}
	printNextSteps(manifest, stateRoot, binDir)
	return nil
}

// bundleDir is the directory the installer runs from, which is the bundle.
func bundleDir() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("finding the bundle this installer belongs to: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(self)
	if err != nil {
		return "", fmt.Errorf("finding the bundle this installer belongs to: %w", err)
	}
	return filepath.Dir(resolved), nil
}

// declarationOf reads the module's own declaration from the bundle, which
// carries it as modules/<namespace>/module.json, the way a checkout does.
func declarationOf(dir, namespace string) (catalog.Declaration, error) {
	declarations, err := catalog.Discover(dir)
	if err != nil {
		return catalog.Declaration{}, err
	}
	for _, declaration := range declarations {
		if declaration.Namespace == namespace {
			return declaration, nil
		}
	}
	return catalog.Declaration{}, fmt.Errorf("the bundle carries no declaration for the %s module", namespace)
}

// copyExecutable writes the bundle's shell into place through a temporary
// file, so an interrupted install never leaves half a binary on PATH.
func copyExecutable(source, destination string) error {
	content, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("reading the bundle's shell: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(destination), err)
	}
	staged := destination + ".installing"
	if err := os.WriteFile(staged, content, 0o755); err != nil {
		return fmt.Errorf("writing the shell: %w", err)
	}
	if err := os.Rename(staged, destination); err != nil {
		_ = os.Remove(staged)
		return fmt.Errorf("putting the shell in place at %s (close any running %s first): %w",
			destination, filepath.Base(destination), err)
	}
	return nil
}

// runShell runs the installed shell against stateRoot.
func runShell(shell, stateRoot string, out io.Writer, args ...string) error {
	command := exec.Command(shell, args...)
	command.Env = append(os.Environ(), state.RootEnvVar+"="+stateRoot)
	command.Stdout = out
	command.Stderr = out
	return command.Run()
}

// recordContext applies the bundle's contexts unless one of the name the
// bundle selects already exists, so an install over an earlier one keeps the
// contexts, and any login, the user already has.
func recordContext(shell, stateRoot, dir string, manifest bundle.Manifest) error {
	var listing bytes.Buffer
	_ = runShell(shell, stateRoot, &listing, "context", "list")
	for _, line := range strings.Split(listing.String(), "\n") {
		for _, word := range strings.Fields(line) {
			if word == manifest.ContextName {
				fmt.Printf("Kept the existing %s context.\n", manifest.ContextName)
				return nil
			}
		}
	}
	if err := runShell(shell, stateRoot, os.Stdout, "context", "apply", "-f",
		filepath.Join(dir, manifest.ContextFile), "--no-install", "--use", manifest.ContextName); err != nil {
		return fmt.Errorf("recording the %s context: %w", manifest.ContextName, err)
	}
	return nil
}

func printNextSteps(manifest bundle.Manifest, stateRoot, binDir string) {
	name := manifest.CommandName
	fmt.Printf("\nPut %s on PATH", name)
	defaultRoot := os.Getenv(state.RootEnvVar) == ""
	if runtime.GOOS == "windows" {
		fmt.Printf(" for this PowerShell session (and add %s to your user PATH to keep it):\n", binDir)
		if !defaultRoot {
			fmt.Printf("  $env:%s = \"%s\"\n", state.RootEnvVar, stateRoot)
		}
		fmt.Printf("  $env:Path = \"%s;\" + $env:Path\n", binDir)
	} else {
		fmt.Printf(" (add these lines to your shell profile to keep it):\n")
		if !defaultRoot {
			fmt.Printf("  export %s=\"%s\"\n", state.RootEnvVar, stateRoot)
		}
		fmt.Printf("  export PATH=\"%s:$PATH\"\n", binDir)
	}
	fmt.Printf("\nThen:\n  %s login\n  %s %s status\n  %s %s --help\n",
		name, name, manifest.Namespace, name, manifest.Namespace)
}
