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

// Package devorigin installs a module that has not been published yet, by
// standing the real catalog up in front of the real installer.
//
// A module author could not run their own module until it was tagged: the SDK's
// test kit drives a module through the contract in process, but it is a
// conforming peer rather than the shell, so a module that satisfies it is not
// thereby proven installable or launchable. This closes that gap without
// inventing a second install path. The module is built, packed into the archive
// a release would publish, described by a catalog the real generator produced,
// served over a short-lived loopback origin, and then installed by
// install.Installer with nothing about the installation made easier. What the
// developer ends up with passes the same receipt validation, records the same
// version policy, and is rolled back the same way as a published module, so the
// problems worth meeting early are met here rather than by their users.
// docs/adr/0011-local-module-install-through-a-development-origin.md records
// why the shortcut of writing the store entry directly was rejected.
//
// This is contributor tooling. Nothing a user installs contains it, and no
// shell command reaches it.
package devorigin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/install"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/release"
	"github.com/wso2/wso2-cli/internal/semver"
	"github.com/wso2/wso2-cli/internal/state"
)

// DefaultVersion is what a local build is installed as when the developer names
// no version.
//
// It is a prerelease so the module lands on the prerelease channel: a developer
// build published on stable would be the version a user following stable is
// offered, and the local catalog and the real one are the same shape. It is
// also below every version a release would carry, so an ordinary install of a
// published version moves the store forwards rather than appearing to go back.
const DefaultVersion = "0.0.0-dev"

// readHeaderTimeout bounds how long the local origin waits on a request header.
// Nothing but this run's installer connects to it, so the value only has to
// stop a stuck connection from outliving the install.
const readHeaderTimeout = 10 * time.Second

// Request is one local install.
type Request struct {
	// RepositoryRoot is the checkout the module is built from.
	RepositoryRoot string
	// Namespace is the module to install, which the checkout must declare.
	Namespace string
	// Version is the version to build and install as. Empty is DefaultVersion.
	Version string
	// StateRoot is the WSO2 state root to install into. It is passed rather
	// than resolved here so a caller can install into an isolated root.
	StateRoot string
	// Shell is the identity of the shell that will launch the module. The
	// install is gated on it exactly as a published install is, and the module
	// is built for its platform.
	Shell modules.ShellIdentity
}

// Result reports what a local install put in the store.
type Result struct {
	Namespace string
	Version   string
	Platform  modules.Platform
	// Origin is the loopback origin the install read, which no longer exists by
	// the time this is returned. It is reported because it is the one part of
	// the run that was not the real path, and a developer reading the output
	// should be able to see exactly how much was local.
	Origin string
	// StoreRoot is the managed module store the module was installed into.
	StoreRoot string
}

// Install builds one module, publishes it to a catalog served on loopback, and
// installs it from there.
//
// The version is pinned, so the policy the installer records holds the module
// at the developer's own build. Without the pin an update run would treat a
// published release as an upgrade and silently replace it.
func Install(ctx context.Context, request Request) (Result, error) {
	if request.StateRoot == "" {
		return Result{}, fmt.Errorf("devorigin: no state root was given to install into")
	}
	version, err := normalizeVersion(request.Version)
	if err != nil {
		return Result{}, err
	}
	declaration, err := declarationFor(request.RepositoryRoot, request.Namespace)
	if err != nil {
		return Result{}, err
	}

	// Asked before anything is built, because the answer does not depend on the
	// build and a refusal after one wastes a compile the developer is waiting
	// on.
	if err := launchable(declaration, request.Shell); err != nil {
		return Result{}, err
	}

	archive, err := pack(request.RepositoryRoot, declaration, version, request.Shell.Platform)
	if err != nil {
		return Result{}, err
	}

	// The listener is bound before the catalog is generated because a published
	// artifact URL is absolute and therefore carries the port, and the port is
	// only known once something is listening. Serving does not begin here: the
	// documents the handler answers with are built first and never written
	// afterwards, so no request can arrive while they are still being assembled.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Result{}, fmt.Errorf("devorigin: cannot listen on loopback to serve the local catalog: %w", err)
	}
	origin := "http://" + listener.Addr().String()

	handler, err := documents(declaration, version, origin, archive, request.Shell.Platform)
	if err != nil {
		_ = listener.Close()
		return Result{}, err
	}

	server := &http.Server{Handler: handler, ReadHeaderTimeout: readHeaderTimeout}
	go func() {
		// The only expected end is the Close below, and Serve reports that as
		// an error. Nothing else can act on a failure here: the install is
		// already reading from this origin and reports the failure it sees.
		_ = server.Serve(listener)
	}()
	defer func() {
		_ = server.Close()
	}()

	store := modules.NewStore(state.ModuleStore(request.StateRoot))
	installer := install.Installer{
		Store:  store,
		Client: catalog.Client{Origin: origin, HTTP: &http.Client{}},
		Shell:  request.Shell,
	}
	installed, err := installer.Run(ctx, install.Request{
		Namespace: declaration.Namespace,
		// The pin is the point: it is what recordPolicy writes into policy.json
		// and what a later update run reads to leave this build alone.
		Policy: catalog.Policy{Version: version},
	})
	if err != nil {
		return Result{}, err
	}
	return Result{
		Namespace: installed.Namespace,
		Version:   installed.Version,
		Platform:  installed.Platform,
		Origin:    origin,
		StoreRoot: store.Root(),
	}, nil
}

// normalizeVersion settles the version the module is built and installed as.
//
// It has to parse as a semantic version because the catalog, the store's
// directory names, and the receipt all require one, and because the version is
// injected into the binary, which announces it back at the handshake.
func normalizeVersion(version string) (string, error) {
	if version == "" {
		version = DefaultVersion
	}
	parsed, err := semver.Parse(version)
	if err != nil {
		return "", fmt.Errorf("devorigin: %q is not a semantic version a module can be published as: %w",
			version, err)
	}
	return parsed.String(), nil
}

// declarationFor finds the module a namespace names in a checkout.
func declarationFor(repositoryRoot, namespace string) (catalog.Declaration, error) {
	declarations, err := catalog.Discover(repositoryRoot)
	if err != nil {
		return catalog.Declaration{}, err
	}
	for _, declaration := range declarations {
		if declaration.Namespace == namespace {
			return declaration, nil
		}
	}
	return catalog.Declaration{}, fmt.Errorf(
		"devorigin: this repository declares no module for the namespace %q; "+
			"create one with make new-module NAMESPACE=%s", namespace, namespace)
}

// launchable refuses an install the shell would not be able to launch.
//
// The version of the shell is deliberately not part of what catalog.Select
// gates on, because a module's version scheme is its product's and says nothing
// about compatibility. The shell range is a different claim, and it is checked
// only when the module is launched. For a published install the two coincide in
// practice, but a shell built from a checkout reports 0.0.0-dev, which no
// scaffolded module's range contains, so the install would succeed and every
// launch afterwards would fail with modules.incompatible_shell.
//
// That is refused rather than warned about. A warning would leave a module in
// the store that cannot run, and the developer would meet the failure later as
// a trust-category refusal that reads like a damaged installation; here the two
// versions are in front of them along with what to change. Nothing is built or
// written, so the refusal costs the developer only the time to read it.
func launchable(declaration catalog.Declaration, shell modules.ShellIdentity) error {
	supported, err := semver.ParseRange(declaration.Compatibility.Shell)
	if err != nil {
		return fmt.Errorf("devorigin: the %s module declares the shell range %q, which cannot be read: %w",
			declaration.Namespace, declaration.Compatibility.Shell, err)
	}
	if supported.Contains(shell.Version) {
		return nil
	}
	return fmt.Errorf("devorigin: the %s module declares that it runs under a shell matching %q, "+
		"and the shell this install is for is %s, so it would install and then refuse to launch. "+
		"Nothing was built or installed. Either name the released wso2 that will launch it with -shell-version, "+
		"or widen the shell range in %s/%s, or build the shell with a version injected: "+
		"go build -ldflags \"-X github.com/wso2/wso2-cli/internal/version.shellVersion=1.0.0\" ./cmd/wso2",
		declaration.Namespace, supported.String(), shell.Version,
		declaration.Directory, catalog.DeclarationFileName)
}

// pack builds the module for one platform and packs the archive a release would
// publish for it.
//
// The build is the release tooling's, flags included, so a module that only
// installs locally because it was built differently is not a thing that can
// happen.
func pack(repositoryRoot string, declaration catalog.Declaration, version string,
	platform modules.Platform) ([]byte, error) {
	moduleDir := filepath.Join(repositoryRoot, filepath.FromSlash(declaration.Directory))

	// The module announces its own version at the handshake and the shell
	// compares it against the receipt, so a build without this injected would
	// install and then refuse to launch on the placeholder. The SDK version is
	// injected for the same reason a release injects it: it describes the build
	// rather than a development placeholder.
	sdkVersion, err := release.SDKVersion(moduleDir)
	if err != nil {
		return nil, err
	}

	// A private directory rather than a predictable name under os.TempDir(),
	// so two runs on one machine cannot build over each other and no
	// pre-existing file or symlink at that path is followed.
	workDir, err := os.MkdirTemp("", "wso2-module-dev-")
	if err != nil {
		return nil, fmt.Errorf("devorigin: creating a build directory failed: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(workDir)
	}()

	output := filepath.Join(workDir, install.ExecutableName(declaration.Namespace, platform))
	var failure bytes.Buffer
	command := exec.Command("go", "build", "-trimpath",
		"-ldflags", release.BuildFlags(version, sdkVersion), "-o", output,
		release.MainPackage(declaration.Namespace))
	command.Dir = moduleDir
	command.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOOS="+platform.OS,
		"GOARCH="+platform.Arch,
		"GOARM=6",
	)
	// The compiler's own output is what says which line failed, so it is
	// carried into the error rather than left on a stream the caller may not be
	// showing.
	command.Stderr = &failure
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("devorigin: building the %s module for %s failed: %w\n%s",
			declaration.Namespace, platform, err, failure.String())
	}
	executable, err := os.ReadFile(output)
	if err != nil {
		return nil, fmt.Errorf("devorigin: reading the built %s module failed: %w", declaration.Namespace, err)
	}

	// The licence and notice travel with the binary in a published archive, so
	// they travel here too: an archive missing them would be a different
	// archive from the one the install path is meant to be exercised with.
	extra, err := release.ReadArchiveFiles(repositoryRoot, "LICENSE", "NOTICE")
	if err != nil {
		return nil, err
	}
	return release.Archive(declaration.Namespace, platform, executable, extra)
}

// documents generates the catalog for the one tag this install publishes and
// returns a handler serving it together with the archive.
//
// Everything is held in memory rather than written to a directory and served
// from disk: there is nothing to clean up afterwards, and no window in which a
// document exists as a partly written file.
func documents(declaration catalog.Declaration, version, origin string, archive []byte,
	platform modules.Platform) (http.Handler, error) {
	archiveName := release.ArchiveName(declaration.Namespace, version, platform)
	tag := declaration.Namespace + "/v" + version

	// The generator is the published one, so a catalog this install can read
	// and the real one cannot are the same catalog. The size and digest are the
	// archive's own, which is what makes the installer's integrity check a real
	// check here rather than a formality.
	digest := sha256.Sum256(archive)
	generated, err := catalog.Generate(catalog.Input{
		Tags:    []string{tag},
		Modules: []catalog.Declaration{declaration},
		Published: map[string]catalog.Release{tag: {
			Compatibility: declaration.Compatibility,
			Capabilities:  declaration.Capabilities,
			Artifacts: []catalog.Artifact{{
				Platform: platform,
				URL:      origin + "/" + archiveName,
				Size:     int64(len(archive)),
				SHA256:   fmt.Sprintf("%x", digest),
			}},
		}},
	})
	if err != nil {
		return nil, err
	}
	files, err := generated.Files()
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	for _, file := range files {
		mux.Handle("/"+file.Path, bodyHandler("application/json", file.Content))
	}
	mux.Handle("/"+archiveName, bodyHandler("application/gzip", archive))
	return mux, nil
}

// bodyHandler answers one fixed body. The bytes are captured before the server
// starts and are never written again, so no request can observe them half
// assembled.
func bodyHandler(contentType string, body []byte) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", contentType)
		_, _ = writer.Write(body)
	})
}
