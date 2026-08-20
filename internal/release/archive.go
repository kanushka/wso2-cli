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

package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/wso2/wso2-cli/internal/install"
	"github.com/wso2/wso2-cli/internal/modules"
)

// Platforms are the targets a module release publishes for. They are the same
// eight the shell publishes for, because a module that published for fewer
// would leave a user who can install the shell unable to install the module,
// and the shell's own target list is the one the cross-build check on every
// pull request already compiles.
var Platforms = []modules.Platform{
	{OS: "darwin", Arch: "amd64"},
	{OS: "darwin", Arch: "arm64"},
	{OS: "linux", Arch: "386"},
	{OS: "linux", Arch: "amd64"},
	{OS: "linux", Arch: "arm"},
	{OS: "linux", Arch: "arm64"},
	{OS: "windows", Arch: "amd64"},
	{OS: "windows", Arch: "arm64"},
}

// ChecksumsFileName is the file listing every archive a module release
// published, in the format sha256sum reads, published beside the archives.
const ChecksumsFileName = "checksums.txt"

// ArchiveExtension is the one format a module archive is published in. It is
// gzipped tar on every platform, including the ones the shell itself publishes
// a zip for, because the shell extracts a module archive as a gzipped tarball
// and refuses anything else. The publisher follows the reader here rather than
// the shell's own convention.
const ArchiveExtension = ".tar.gz"

// ArchiveName is the file name one platform's archive is published under.
//
// The name carries the namespace and the version verbatim, so a reader who has
// a tag can build the name without transforming it.
func ArchiveName(namespace, version string, platform modules.Platform) string {
	return fmt.Sprintf("wso2-module-%s-v%s-%s-%s%s",
		namespace, version, platform.OS, platform.Arch, ArchiveExtension)
}

// MainPackage is the package a module's executable is built from, relative to
// the module directory. It is the executable's own name under cmd/, so the
// package path, the archive entry, and the name the shell looks for are one
// convention rather than three.
func MainPackage(namespace string) string {
	return "./cmd/wso2-module-" + namespace
}

// ArchiveFile is one file inside a module archive.
type ArchiveFile struct {
	Name string
	Body []byte
	// Executable marks the module binary, which has to survive extraction with
	// its execute bit on a platform that has one.
	Executable bool
}

// Archive packs one platform's archive: the module executable at the archive
// root, under the name the shell expects, beside the licence and notice that
// Apache-2.0 requires to travel with a binary.
func Archive(namespace string, platform modules.Platform, executable []byte, extra []ArchiveFile) ([]byte, error) {
	// The archive layout is a convention rather than a catalog field: the shell
	// extracts a module archive expecting exactly this name and refuses an
	// archive that does not carry it rather than searching for something
	// executable. That makes it a contract between this half, which packs the
	// archive, and the shell's half, which unpacks it, so the name comes from
	// the unpacking half rather than being written down a second time here.
	files := append([]ArchiveFile{{
		Name:       install.ExecutableName(namespace, platform),
		Body:       executable,
		Executable: true,
	}}, extra...)
	return packTarGzip(files)
}

func packTarGzip(files []ArchiveFile) ([]byte, error) {
	var packed bytes.Buffer
	compressor := gzip.NewWriter(&packed)
	writer := tar.NewWriter(compressor)
	for _, file := range files {
		if err := writer.WriteHeader(&tar.Header{
			Name: file.Name,
			Mode: fileMode(file),
			Size: int64(len(file.Body)),
		}); err != nil {
			return nil, fmt.Errorf("release: writing the %s header failed: %w", file.Name, err)
		}
		if _, err := writer.Write(file.Body); err != nil {
			return nil, fmt.Errorf("release: writing %s failed: %w", file.Name, err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("release: closing the tarball failed: %w", err)
	}
	if err := compressor.Close(); err != nil {
		return nil, fmt.Errorf("release: closing the gzip stream failed: %w", err)
	}
	return packed.Bytes(), nil
}

func fileMode(file ArchiveFile) int64 {
	if file.Executable {
		return 0o755
	}
	return 0o644
}

// ReadArchiveFiles reads the files an archive should carry beside the module
// executable from the repository checkout.
func ReadArchiveFiles(repositoryRoot string, names ...string) ([]ArchiveFile, error) {
	files := make([]ArchiveFile, 0, len(names))
	for _, name := range names {
		body, err := os.ReadFile(filepath.Join(repositoryRoot, filepath.FromSlash(name)))
		if err != nil {
			return nil, fmt.Errorf("release: reading %s failed: %w", name, err)
		}
		files = append(files, ArchiveFile{Name: baseName(name), Body: body})
	}
	return files, nil
}

// baseName reduces a checkout-relative name to the name it carries in an
// archive, which is flat.
func baseName(name string) string {
	return path.Base(name)
}
