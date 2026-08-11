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

// The fixture release both install scripts are driven against.
//
// It is shared rather than written twice because the two scripts implement one
// published contract: the same artifact names, the same checksum file, and the
// same tag resolution. A Windows-only copy of this would be free to drift from
// the contract the Unix script was proven against, and then only one of them
// would still be right.
//
// The archives carry the real shell, built with the version the tag names, so a
// test can install one and run what it installed. A stand-in that only claimed
// to be the shell would prove less: extraction, permissions, and replacement all
// behave differently for a real executable.
package acceptance_test

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// The fixture release. The tags differ from each other and from anything either
// script defaults to, so an assertion cannot pass by coincidence, and the
// prerelease tag is newer than the stable one so that resolving "latest" wrongly
// would pick it.
const (
	fixtureStableTag   = "v1.2.3"
	fixturePreleaseTag = "v1.3.0-rc.1"
	fixtureOlderTag    = "v1.1.0"
	installBlockMarker = "# >>> wso2 cli >>>"
)

// Building the shell three times over is worth avoiding, and every test in the
// package wants the same three builds.
var (
	standInMutex sync.Mutex
	standInCache = map[string][]byte{}
)

// installedBinaryName is what the archive holds and what the script installs.
func installedBinaryName() string {
	return "wso2" + executableSuffix()
}

// standInShell reports the bytes of a shell built to report the given tag as its
// own version, which is how a test tells which release actually landed.
func standInShell(t *testing.T, tag string) []byte {
	t.Helper()
	standInMutex.Lock()
	defer standInMutex.Unlock()
	if cached, ok := standInCache[tag]; ok {
		return cached
	}

	binary := filepath.Join(t.TempDir(), installedBinaryName())
	// The version package prefixes a "v" for display and parses the bare form, so
	// what is injected is the tag without it — exactly as the release does.
	build(t, repoRoot(t), binary,
		"-X github.com/wso2/wso2-cli/internal/version.shellVersion="+strings.TrimPrefix(tag, "v"),
		"./cmd/wso2")
	contents, err := os.ReadFile(binary)
	if err != nil {
		t.Fatalf("reading the built shell returned %v", err)
	}
	standInCache[tag] = contents
	return contents
}

// installHarness is one isolated install: a fixture release served over HTTP, a
// temporary home directory, and a state root nothing else writes to.
type installHarness struct {
	t           *testing.T
	home        string
	stateRoot   string
	environment []string
	server      *httptest.Server

	// corruptArchive serves bytes the published checksum does not describe, which
	// is what a substituted download looks like from the client's side.
	corruptArchive bool
	// precedingSiblingChecksum lists a longer artifact name ending in this
	// archive's name before the archive's own line, so a loose filename match
	// takes the wrong digest.
	precedingSiblingChecksum bool
	// omitChecksumLine publishes a checksum file that says nothing about this
	// archive.
	omitChecksumLine bool

	// The profile the script is expected to find, and whether one exists at all.
	// Windows has no profile to edit and leaves both alone.
	profilePath  string
	writeProfile bool
	// profileMode is the permission the fixture profile is written with, so a
	// test can present the script with one it cannot write to.
	profileMode os.FileMode

	// What each platform needs and the other has no use for. It is a type per
	// platform rather than a union of both, because a field only one build reads
	// is dead code in the other — which the linter is right to say.
	platform platformFields
}

func newInstallHarness(t *testing.T) *installHarness {
	t.Helper()
	home := t.TempDir()
	install := &installHarness{
		t:            t,
		home:         home,
		stateRoot:    filepath.Join(home, ".wso2"),
		profilePath:  filepath.Join(home, ".bashrc"),
		writeProfile: true,
		profileMode:  0o644,
	}
	install.server = httptest.NewServer(http.HandlerFunc(install.serve))
	t.Cleanup(install.server.Close)
	return install
}

// serve answers the shapes both scripts depend on: the redirect that names the
// newest stable tag, the listing that names the newest prerelease, and the
// download paths for archives and the checksum file.
func (i *installHarness) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/releases/latest":
		http.Redirect(w, r, "/releases/tag/"+fixtureStableTag, http.StatusFound)

	case strings.HasPrefix(r.URL.Path, "/releases/tag/"):
		// The redirect target has to answer, as the real release page does: both
		// scripts follow the redirect and read the tag off the URL they land on.
		if _, err := fmt.Fprintf(w, "release %s\n",
			strings.TrimPrefix(r.URL.Path, "/releases/tag/")); err != nil {
			i.t.Errorf("writing the tag page returned %v", err)
		}

	case r.URL.Path == "/releases":
		// Newest first, as the GitHub API returns them, with the fields in the
		// order and nesting the real listing uses: tag_name precedes prerelease and
		// a nested object sits between them. Both are what the parses cope with.
		type author struct {
			Login string `json:"login"`
			ID    int    `json:"id"`
		}
		type release struct {
			TagName    string `json:"tag_name"`
			Author     author `json:"author"`
			Prerelease bool   `json:"prerelease"`
		}
		releases := []release{
			{TagName: fixturePreleaseTag, Author: author{Login: "release-bot", ID: 1}, Prerelease: true},
			{TagName: fixtureStableTag, Author: author{Login: "release-bot", ID: 1}, Prerelease: false},
			{TagName: fixtureOlderTag, Author: author{Login: "release-bot", ID: 1}, Prerelease: false},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(releases); err != nil {
			i.t.Errorf("encoding the release listing returned %v", err)
		}

	case strings.HasPrefix(r.URL.Path, "/releases/download/"):
		rest := strings.TrimPrefix(r.URL.Path, "/releases/download/")
		tag, name, found := strings.Cut(rest, "/")
		if !found {
			http.NotFound(w, r)
			return
		}
		archive := i.archiveName(tag)
		switch name {
		case archive:
			body := i.archiveBytes(tag)
			if i.corruptArchive {
				body = append(body, "tampered"...)
			}
			if _, err := w.Write(body); err != nil {
				i.t.Errorf("writing the archive returned %v", err)
			}
		case "checksums.txt":
			sum := sha256.Sum256(i.archiveBytes(tag))
			line := fmt.Sprintf("%x  %s\n", sum, archive)
			switch {
			case i.omitChecksumLine:
				line = fmt.Sprintf("%064d  some-other-artifact.tar.gz\n", 0)
			case i.precedingSiblingChecksum:
				// A hash that is deliberately not the archive's, on a line whose name
				// ends with the archive's name, listed first.
				line = fmt.Sprintf("%064d  %s.sig\n%s", 0, archive, line)
			}
			if _, err := fmt.Fprint(w, line); err != nil {
				i.t.Errorf("writing the checksum file returned %v", err)
			}
		default:
			http.NotFound(w, r)
		}

	default:
		http.NotFound(w, r)
	}
}

// archiveName is the published name for this platform, built from the same
// convention the scripts build their download URL from.
func (i *installHarness) archiveName(tag string) string {
	extension := "tar.gz"
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		extension = "zip"
	}
	return fmt.Sprintf("wso2-cli-%s-%s-%s.%s", tag, runtime.GOOS, runtime.GOARCH, extension)
}

// archiveBytes builds the archive this platform's release would carry: the shell
// at the archive root, beside the licence and notice a real release ships. It is
// deterministic, so the checksum served alongside describes exactly these bytes.
func (i *installHarness) archiveBytes(tag string) []byte {
	i.t.Helper()
	files := []struct {
		name string
		body []byte
		mode int64
	}{
		{installedBinaryName(), standInShell(i.t, tag), 0o755},
		{"LICENSE", []byte("Apache License 2.0\n"), 0o644},
		{"NOTICE", []byte("WSO2 CLI\n"), 0o644},
	}

	var buffer strings.Builder
	if strings.HasSuffix(i.archiveName(tag), ".zip") {
		writer := zip.NewWriter(&buffer)
		for _, file := range files {
			header := &zip.FileHeader{Name: file.name, Method: zip.Deflate}
			header.SetMode(os.FileMode(file.mode))
			entry, err := writer.CreateHeader(header)
			if err != nil {
				i.t.Fatalf("creating %s in the zip returned %v", file.name, err)
			}
			if _, err := entry.Write(file.body); err != nil {
				i.t.Fatalf("writing %s into the zip returned %v", file.name, err)
			}
		}
		if err := writer.Close(); err != nil {
			i.t.Fatalf("closing the zip returned %v", err)
		}
		return []byte(buffer.String())
	}

	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, file := range files {
		if err := tarWriter.WriteHeader(&tar.Header{
			Name: file.name,
			Mode: file.mode,
			Size: int64(len(file.body)),
		}); err != nil {
			i.t.Fatalf("writing the %s header returned %v", file.name, err)
		}
		if _, err := tarWriter.Write(file.body); err != nil {
			i.t.Fatalf("writing %s into the tarball returned %v", file.name, err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		i.t.Fatalf("closing the tarball returned %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		i.t.Fatalf("closing the gzip stream returned %v", err)
	}
	return []byte(buffer.String())
}

// installedBinary is where the script is expected to have put the shell.
func (i *installHarness) installedBinary() string {
	return filepath.Join(i.stateRoot, "bin", installedBinaryName())
}

// reportedVersion runs what was installed and reports the release tag it names.
// Running it is the point: a file of the right name that cannot execute would
// satisfy an existence check and nothing a user cares about.
func (i *installHarness) reportedVersion(t *testing.T) string {
	t.Helper()
	// Run against a state root of its own: reporting a version reads the module
	// inventory, and it must not read what the install under test just created.
	command := exec.Command(i.installedBinary(), "version")
	command.Env = shellEnvironment(filepath.Join(i.t.TempDir(), "inventory"))
	raw, err := command.CombinedOutput()
	output := string(raw)
	if err != nil {
		t.Fatalf("the installed binary at %s did not run: %v\noutput:\n%s",
			i.installedBinary(), err, output)
	}
	for _, tag := range []string{fixturePreleaseTag, fixtureStableTag, fixtureOlderTag} {
		if strings.Contains(output, tag) {
			return tag
		}
	}
	t.Fatalf("the installed binary reported no known fixture tag:\n%s", output)
	return ""
}
