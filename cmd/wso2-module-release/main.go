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

// Command wso2-module-release publishes one product module's tag: it gates the
// release, builds the module for every supported platform, and packs the
// archives and the checksum file the catalog will point at.
//
//	go run ./cmd/wso2-module-release -tag reference/v4.5.0 -out dist
//
// The gate runs first and on its own, so a module no released shell can launch
// is refused before anything is built or uploaded. Run it alone with
// -gate-only, which is what the release workflow's first job does.
//
// This is a build tool rather than a released artifact: nothing a user installs
// contains it.
package main

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/release"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "wso2-module-release: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	tag := flag.String("tag", "", "The module tag being released, such as reference/v4.5.0.")
	repositoryRoot := flag.String("repo", ".", "Repository checkout to build the module from.")
	outputDir := flag.String("out", "dist", "Directory to write the archives and the checksum file into.")
	gateOnly := flag.Bool("gate-only", false, "Decide whether the release may publish, and build nothing.")
	flag.Parse()

	if *tag == "" {
		return fmt.Errorf("no -tag was given")
	}
	namespace, version, err := catalog.ParseTag(*tag)
	if err != nil {
		return err
	}

	declaration, err := declarationFor(*repositoryRoot, namespace)
	if err != nil {
		return err
	}

	// The gate. Its decision is a pure function of what the module declares and
	// what the released shell supports, and it is proven in
	// internal/release/gate_test.go rather than by pushing a tag.
	if err := release.Gate(namespace, version, declaration.Compatibility, release.ShellWindow()); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "%s speaks module-contract protocol %s and the released shell speaks %s\n",
		*tag, release.FormatProtocols(declaration.Compatibility.ProtocolVersions),
		release.FormatProtocols(release.ShellWindow()))
	if *gateOnly {
		return nil
	}

	extra, err := release.ReadArchiveFiles(*repositoryRoot, "LICENSE", "NOTICE")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*outputDir, 0o755); err != nil {
		return fmt.Errorf("creating %s failed: %w", *outputDir, err)
	}

	digests := map[string]string{}
	for _, platform := range release.Platforms {
		executable, err := build(*repositoryRoot, declaration, version, platform)
		if err != nil {
			return err
		}
		archive, err := release.Archive(namespace, platform, executable, extra)
		if err != nil {
			return err
		}
		name := release.ArchiveName(namespace, version, platform)
		if err := os.WriteFile(filepath.Join(*outputDir, name), archive, 0o644); err != nil {
			return fmt.Errorf("writing %s failed: %w", name, err)
		}
		digests[name] = fmt.Sprintf("%x", sha256.Sum256(archive))
		fmt.Fprintf(os.Stdout, "built %s\n", name)
	}

	if err := writeChecksums(*outputDir, digests); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "%d archives and %s written into %s\n",
		len(digests), release.ChecksumsFileName, *outputDir)
	return nil
}

// declarationFor finds the module a tag names. A tag naming a namespace this
// repository declares no module for names nothing that can be built, and is
// refused rather than releasing an empty archive.
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
		"the tag names the namespace %q and this repository declares no module for it", namespace)
}

// build compiles the module for one platform.
//
// The main package is at cmd/<executable name> inside the module directory,
// which is the same name the archive carries and the shell looks for, so the
// convention is one name rather than three.
func build(repositoryRoot string, declaration catalog.Declaration, version string,
	platform modules.Platform) ([]byte, error) {
	output := filepath.Join(os.TempDir(), release.ExecutableName(declaration.Namespace, platform))
	packagePath := release.MainPackage(declaration.Namespace)

	// The module reports its own version at the handshake and the shell checks
	// it against the receipt, so a build that left the development placeholder
	// in would install and refuse to launch.
	ldflags := "-s -w -X main.moduleVersion=" + version
	command := exec.Command("go", "build", "-trimpath", "-ldflags", ldflags, "-o", output, packagePath)
	command.Dir = filepath.Join(repositoryRoot, filepath.FromSlash(declaration.Directory))
	command.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOOS="+platform.OS,
		"GOARCH="+platform.Arch,
		"GOARM=6",
	)
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("building the %s module for %s failed: %w",
			declaration.Namespace, platform, err)
	}
	built, err := os.ReadFile(output)
	if err != nil {
		return nil, fmt.Errorf("reading the built %s module failed: %w", platform, err)
	}
	if err := os.Remove(output); err != nil {
		return nil, fmt.Errorf("removing the built %s module failed: %w", platform, err)
	}
	return built, nil
}

// writeChecksums publishes one file covering every archive, in the format
// sha256sum reads, so a download can be verified with the tool already on the
// machine as well as against the catalog entry.
func writeChecksums(outputDir string, digests map[string]string) error {
	names := make([]string, 0, len(digests))
	for name := range digests {
		names = append(names, name)
	}
	sort.Strings(names)

	var listing strings.Builder
	for _, name := range names {
		fmt.Fprintf(&listing, "%s  %s\n", digests[name], name)
	}
	path := filepath.Join(outputDir, release.ChecksumsFileName)
	if err := os.WriteFile(path, []byte(listing.String()), 0o644); err != nil {
		return fmt.Errorf("writing %s failed: %w", path, err)
	}
	return nil
}
