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

// Command wso2-module-dev installs a module from this checkout, unpublished, so
// its author can run it under the real wso2 shell before tagging anything.
//
//	go run ./cmd/wso2-module-dev -namespace mycloud
//
// or, which is what the contributing guide points at:
//
//	make install-module NAMESPACE=mycloud
//
// The install is the real one. The module is built and packed exactly as a
// release would pack it, a catalog is generated over it by the published
// generator, and the ordinary installer reads that catalog from a loopback
// origin that lives only for the length of the run. What lands in the module
// store is therefore indistinguishable from a published install apart from its
// version, and wso2 module list, update, and remove all work on it. See
// docs/adr/0011-local-module-install-through-a-development-origin.md.
//
// The version is a prerelease and it is pinned, so a developer's own build is
// never offered to anyone following stable and is never replaced by a published
// release behind their back. Take it off again with wso2 module remove.
//
// This is contributor tooling rather than a released artifact: nothing a user
// installs contains it, and installing an unverified local build is not
// something a user's shell should be able to do.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/wso2/wso2-cli/internal/devorigin"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/semver"
	"github.com/wso2/wso2-cli/internal/state"
	"github.com/wso2/wso2-cli/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "wso2-module-dev: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	namespace := flag.String("namespace", "",
		"The namespace of the module in this checkout to install, such as mycloud.")
	moduleVersion := flag.String("version", "",
		fmt.Sprintf("The version to build and install as. Defaults to %s.", devorigin.DefaultVersion))
	shellVersion := flag.String("shell-version", "",
		"The version of the wso2 shell that will launch the module. "+
			"Defaults to what a shell built from this checkout reports.")
	// Only the closing message uses this. A caller that has just built the
	// shell it is installing for knows where that shell is, and naming "wso2"
	// there instead would send the developer to whichever one is on PATH,
	// which is the one this install was not made for.
	shellPath := flag.String("shell-path", "wso2",
		"How to name the shell that will launch the module, in the closing message.")
	shellProtocols := flag.String("shell-protocols", "",
		"The module-contract protocol versions the launching shell speaks, such as \"2,1\", "+
			"or \"checkout\" when that shell was built from this checkout.")
	flag.Parse()

	if *namespace == "" {
		flag.Usage()
		return fmt.Errorf("-namespace is required")
	}

	// The checkout is where the command is run from, as it is for
	// wso2-module-new: a flag naming another one would be a way to install a
	// module built from a repository other than the one being worked in.
	repositoryRoot, err := os.Getwd()
	if err != nil {
		return err
	}
	stateRoot, err := state.Root()
	if err != nil {
		return err
	}
	shell, err := shellIdentity(*shellVersion, *shellProtocols)
	if err != nil {
		return err
	}

	result, err := devorigin.Install(context.Background(), devorigin.Request{
		RepositoryRoot: repositoryRoot,
		Namespace:      *namespace,
		Version:        *moduleVersion,
		StateRoot:      stateRoot,
		Shell:          shell,
	})
	if err != nil {
		return err
	}

	// What was local is said out loud, because the value of this command rests
	// on how little of it was: one origin, gone again, and everything else the
	// path a user takes.
	fmt.Printf("Installed %s v%s for %s into %s.\n",
		result.Namespace, result.Version, result.Platform, result.StoreRoot)
	fmt.Printf("It was installed by the ordinary installer from a catalog served at %s for the length of this run.\n",
		result.Origin)
	fmt.Printf("The version is pinned, so wso2 module update leaves this build alone.\n")
	fmt.Printf("\nRun it:\n  %s %s --help\nTake it off again:\n  %s module remove %s\n",
		*shellPath, result.Namespace, *shellPath, result.Namespace)
	return nil
}

// shellIdentity is the shell the module is being installed for.
//
// The version defaults to what this checkout's shell reports, which for an
// uninjected build is a development version no module's declared shell range
// contains. A developer whose wso2 is a released one says so with
// -shell-version rather than being refused on the strength of what the checkout
// would have built, because the range is checked against the shell that
// launches the module and not against the one that installed it.
func shellIdentity(declared, protocols string) (modules.ShellIdentity, error) {
	shellSemver, err := version.ShellSemver()
	if err != nil {
		return modules.ShellIdentity{}, err
	}
	if declared != "" {
		shellSemver, err = semver.Parse(declared)
		if err != nil {
			return modules.ShellIdentity{}, fmt.Errorf("-shell-version %q is not a semantic version: %w",
				declared, err)
		}
	}

	// Naming another shell's version says nothing about the protocol window it
	// speaks, and selection decides over that window. Taking this checkout's
	// window on trust is the combination that installs a module a released
	// shell then refuses to launch, which is the failure this whole command
	// exists to bring forward, so it is refused rather than assumed.
	if declared != "" && protocols == "" {
		return modules.ShellIdentity{}, fmt.Errorf(
			"-shell-version names a shell other than this checkout, so its module-contract protocol " +
				"window is unknown and a module could be installed that it refuses to launch; pass " +
				"-shell-protocols with the versions it speaks, which wso2 version prints, or " +
				"-shell-protocols checkout when that shell was built from this checkout")
	}

	window, err := shellWindow(protocols)
	if err != nil {
		return modules.ShellIdentity{}, err
	}
	return modules.ShellIdentity{
		Version:          shellSemver,
		ProtocolVersions: window,
		Platform:         modules.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH},
	}, nil
}

// shellWindow reads the protocol versions the launching shell speaks.
//
// An empty value and the word "checkout" both mean this checkout's window,
// which is the truth when the shell was built here. Anything else is the
// caller stating what another shell speaks.
func shellWindow(protocols string) ([]int, error) {
	if protocols == "" || protocols == "checkout" {
		return version.ProtocolVersions(), nil
	}
	var window []int
	for _, field := range strings.Split(protocols, ",") {
		field = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(field), "v"))
		parsed, err := strconv.Atoi(field)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf(
				"-shell-protocols %q is not a comma-separated list of protocol versions such as \"2,1\"",
				protocols)
		}
		window = append(window, parsed)
	}
	// Newest first, which is the order negotiation walks.
	slices.Sort(window)
	slices.Reverse(window)
	return window, nil
}
