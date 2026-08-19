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
