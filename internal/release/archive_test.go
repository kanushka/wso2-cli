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

package release_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/install"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/release"
)

// The archive layout is a convention rather than a catalog field, so what the
// release job packs and what the shell unpacks have to be the same convention.
// This asserts on the shell's own expectation rather than on a literal, which
// is what keeps the two halves from drifting.
func TestArchiveCarriesTheExecutableTheShellLooksFor(t *testing.T) {
	for _, platform := range release.Platforms {
		packed, err := release.Archive("reference", platform, []byte("module bytes"),
			[]release.ArchiveFile{{Name: "LICENSE", Body: []byte("licence")}})
		if err != nil {
			t.Fatalf("packing the %s archive returned %v", platform, err)
		}
		names := archiveNames(t, platform, packed)
		wanted := install.ExecutableName("reference", platform)
		if _, found := names[wanted]; !found {
			t.Errorf("the %s archive carries %v, and the shell looks for %s", platform, names, wanted)
		}
		if _, found := names["LICENSE"]; !found {
			t.Errorf("the %s archive carries no LICENSE", platform)
		}
	}
}

func TestArchiveNameCarriesTheTagVerbatim(t *testing.T) {
	name := release.ArchiveName("reference", "4.5.0", modules.Platform{OS: "linux", Arch: "amd64"})
	if name != "wso2-module-reference-v4.5.0-linux-amd64.tar.gz" {
		t.Errorf("the archive name is %q", name)
	}
	// Gzipped tar on Windows too: the shell extracts a module archive as a
	// gzipped tarball and refuses anything else, whatever the shell's own
	// release publishes for the same platform.
	windows := release.ArchiveName("reference", "4.5.0", modules.Platform{OS: "windows", Arch: "amd64"})
	if windows != "wso2-module-reference-v4.5.0-windows-amd64.tar.gz" {
		t.Errorf("the Windows archive name is %q", windows)
	}
}

// archiveNames reports the entries an archive carries.
func archiveNames(t *testing.T, platform modules.Platform, packed []byte) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	stream, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		t.Fatalf("reading the %s gzip stream returned %v", platform, err)
	}
	reader := tar.NewReader(stream)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("reading the %s tarball returned %v", platform, err)
		}
		names[header.Name] = true
	}
	return names
}

// The targets a module publishes for are the targets the repository proves it
// can build. Declaring them a second time in Go is only safe if the two lists
// are held equal, so this reads the cross-build check's own list rather than
// trusting a comment that says they match.
func TestPlatformsAreTheTargetsEveryPullRequestCompiles(t *testing.T) {
	workflow, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", "pr-checks.yml"))
	if err != nil {
		t.Fatalf("reading the pull-request workflow returned %v", err)
	}
	loop := regexp.MustCompile(`(?s)for target in\s+(.*?);\s*do`).FindSubmatch(workflow)
	if loop == nil {
		t.Fatal("the cross-build check no longer lists its targets in a loop this test can read")
	}

	compiled := map[string]bool{}
	for _, target := range regexp.MustCompile(`[a-z0-9]+/[a-z0-9]+`).FindAllString(string(loop[1]), -1) {
		compiled[target] = true
	}
	published := map[string]bool{}
	for _, platform := range release.Platforms {
		published[platform.String()] = true
	}
	for target := range compiled {
		if !published[target] {
			t.Errorf("%s is compiled on every pull request and no module publishes for it", target)
		}
	}
	for target := range published {
		if !compiled[target] {
			t.Errorf("a module publishes for %s and no pull request compiles it", target)
		}
	}
}

// A released module announces two versions at the handshake, and both are
// build-time variables carrying development placeholders. The shell checks the
// module's own version against the receipt, so a missing injection there is
// caught at launch. Nothing checks the SDK version, so a missing injection
// there is not caught at all: the module simply tells every shell it was built
// against a development SDK that was never published, in an archive that cannot
// be changed once released.
func TestBuildFlagsInjectBothVersionsAModuleAnnounces(t *testing.T) {
	flags := release.BuildFlags("4.5.0", "0.1.0")

	for _, want := range []string{
		"-X main.moduleVersion=4.5.0",
		"-X github.com/wso2/wso2-cli/sdk/module.SDKVersion=0.1.0",
	} {
		if !strings.Contains(flags, want) {
			t.Errorf("the build flags %q do not carry %q", flags, want)
		}
	}
}

// The SDK version comes from the module being built rather than from the run,
// so it describes the build. Reading it from the module graph rather than from
// the file's text is what makes an exclude, a comment, or a versionless replace
// unable to change the answer.
func TestTheSDKVersionIsReadFromTheModuleGraph(t *testing.T) {
	directory := t.TempDir()
	writeModuleFile(t, directory, `module example.com/product

go 1.25.0

// github.com/wso2/wso2-cli/sdk v9.9.9 is a comment and not a requirement.
exclude github.com/wso2/wso2-cli/sdk v0.0.1

require github.com/wso2/wso2-cli/sdk v0.4.2
`)

	version, err := release.SDKVersion(directory)
	if err != nil {
		t.Fatalf("reading the SDK version returned %v", err)
	}
	// Announced as a version rather than as a tag, which is the shape the
	// handshake and the receipt both carry.
	if version != "0.4.2" {
		t.Errorf("SDK version = %q, want %q", version, "0.4.2")
	}
}

// A module that requires no SDK is not a module this repository can release, and
// saying so beats building one that announces nothing.
func TestAModuleRequiringNoSDKCannotBeBuilt(t *testing.T) {
	directory := t.TempDir()
	writeModuleFile(t, directory, "module example.com/product\n\ngo 1.25.0\n")

	if _, err := release.SDKVersion(directory); err == nil {
		t.Fatal("a module requiring no SDK reported an SDK version")
	}
}

func writeModuleFile(t *testing.T, directory, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte(content), 0o644); err != nil {
		t.Fatalf("cannot write go.mod: %v", err)
	}
}
