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

//go:build !windows

// The install script is driven exactly as a user drives it: the real script,
// from this checkout, against a release it downloads over HTTP. Nothing here
// asserts on the script's internals, because a user cannot see them; what is
// asserted is what a user is left with — a binary that runs, a profile that
// gained one block, and an aborted run that left neither behind.
//
// Two isolations make that safe to run anywhere. The home directory and the
// state root are temporary directories, so no run can reach the developer's
// real profile or state, and the release origin points at a local test server,
// so no run reaches GitHub. A test that forgot either would install onto the
// machine running it, so both are supplied by one helper rather than per test.
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
	"testing"
)

// The fixture release. The tags differ from each other and from anything the
// script defaults to, so an assertion cannot pass by coincidence, and the
// prerelease tag is newer than the stable one so that resolving "latest"
// wrongly would pick it.
const (
	fixtureStableTag     = "v1.2.3"
	fixturePreleaseTag   = "v1.3.0-rc.1"
	fixtureOlderTag      = "v1.1.0"
	installBlockMarker   = "# >>> wso2 cli >>>"
	installScriptRelPath = "scripts/install.sh"
)

func TestInstallScriptInstallsTheShellAndWiresThePath(t *testing.T) {
	install := newInstallHarness(t)

	stdout, stderr, err := install.run()
	if err != nil {
		t.Fatalf("install.sh failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	binary := filepath.Join(install.stateRoot, "bin", "wso2")
	info, statErr := os.Stat(binary)
	if statErr != nil {
		t.Fatalf("expected an installed binary at %s: %v", binary, statErr)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("installed binary has mode %v, want the execute bit set", info.Mode().Perm())
	}

	// The point of installing a binary is being able to run it. A file with the
	// right name that cannot execute would satisfy every assertion above.
	reported, runErr := exec.Command(binary, "version").CombinedOutput()
	if runErr != nil {
		t.Fatalf("the installed binary did not run: %v\noutput:\n%s", runErr, reported)
	}
	if !strings.Contains(string(reported), fixtureStableTag) {
		t.Errorf("installed binary reported %q, want it to mention %s", reported, fixtureStableTag)
	}

	profile := install.readProfile(t)
	if blocks := strings.Count(profile, installBlockMarker); blocks != 1 {
		t.Errorf("profile carries %d install blocks, want exactly 1:\n%s", blocks, profile)
	}
	if want := filepath.Join(install.stateRoot, "bin"); !strings.Contains(profile, want) {
		t.Errorf("profile does not put %s on PATH:\n%s", want, profile)
	}
	// A user who is not told which file changed cannot undo it, and cannot know
	// why their current shell still fails to find the command.
	if !strings.Contains(stdout, install.profilePath) {
		t.Errorf("output does not name the profile it edited (%s):\n%s", install.profilePath, stdout)
	}
}

func TestInstallScriptIsIdempotent(t *testing.T) {
	install := newInstallHarness(t)

	if _, stderr, err := install.run(); err != nil {
		t.Fatalf("first install failed: %v\nstderr:\n%s", err, stderr)
	}
	binary := filepath.Join(install.stateRoot, "bin", "wso2")
	if err := os.WriteFile(binary, []byte("stale"), 0o755); err != nil {
		t.Fatalf("could not stale the installed binary: %v", err)
	}

	if _, stderr, err := install.run(); err != nil {
		t.Fatalf("second install failed: %v\nstderr:\n%s", err, stderr)
	}

	// Re-running is the documented upgrade path, so the binary must be replaced
	// rather than left as whatever was there.
	replaced, err := os.ReadFile(binary)
	if err != nil {
		t.Fatalf("reading the reinstalled binary returned %v", err)
	}
	if string(replaced) == "stale" {
		t.Error("the second run did not replace the installed binary")
	}
	profile := install.readProfile(t)
	if blocks := strings.Count(profile, installBlockMarker); blocks != 1 {
		t.Errorf("profile carries %d install blocks after two runs, want exactly 1:\n%s", blocks, profile)
	}
}

func TestInstallScriptRefusesAnArchiveThatFailsItsChecksum(t *testing.T) {
	install := newInstallHarness(t)
	install.corruptArchive = true

	stdout, stderr, err := install.run()
	if err == nil {
		t.Fatalf("install.sh succeeded on a corrupted archive\nstdout:\n%s", stdout)
	}

	// The refusal has to be legible as a verification failure. A generic failure
	// would leave a user guessing whether the download merely broke.
	if !strings.Contains(strings.ToLower(stdout+stderr), "checksum") {
		t.Errorf("refusal does not mention the checksum:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	// Nothing may survive a failed verification: an executable installed from an
	// archive that failed its checksum is the whole risk this check exists for.
	if _, statErr := os.Stat(filepath.Join(install.stateRoot, "bin", "wso2")); !os.IsNotExist(statErr) {
		t.Error("a binary was installed from an archive that failed verification")
	}
	if profile := install.readProfile(t); strings.Contains(profile, installBlockMarker) {
		t.Errorf("the profile was edited by a run that failed verification:\n%s", profile)
	}
}

func TestInstallScriptInstallsAPinnedVersion(t *testing.T) {
	install := newInstallHarness(t)

	stdout, stderr, err := install.run(fixtureOlderTag)
	if err != nil {
		t.Fatalf("install.sh failed for a pinned version: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	reported, runErr := exec.Command(filepath.Join(install.stateRoot, "bin", "wso2"), "version").CombinedOutput()
	if runErr != nil {
		t.Fatalf("the installed binary did not run: %v", runErr)
	}
	if !strings.Contains(string(reported), fixtureOlderTag) {
		t.Errorf("installed binary reported %q, want the pinned %s", reported, fixtureOlderTag)
	}
}

func TestInstallScriptResolvesAPrereleaseOnlyWhenAsked(t *testing.T) {
	t.Run("the default resolves the newest stable release", func(t *testing.T) {
		install := newInstallHarness(t)

		if _, stderr, err := install.run(); err != nil {
			t.Fatalf("install.sh failed: %v\nstderr:\n%s", err, stderr)
		}

		reported, _ := exec.Command(filepath.Join(install.stateRoot, "bin", "wso2"), "version").CombinedOutput()
		if strings.Contains(string(reported), fixturePreleaseTag) {
			t.Errorf("the default install resolved the prerelease %s", fixturePreleaseTag)
		}
	})

	t.Run("the opt-in resolves the newest prerelease", func(t *testing.T) {
		install := newInstallHarness(t)
		install.environment = append(install.environment, "WSO2_CLI_PRERELEASE=true")

		if _, stderr, err := install.run(); err != nil {
			t.Fatalf("install.sh failed: %v\nstderr:\n%s", err, stderr)
		}

		reported, _ := exec.Command(filepath.Join(install.stateRoot, "bin", "wso2"), "version").CombinedOutput()
		if !strings.Contains(string(reported), fixturePreleaseTag) {
			t.Errorf("the prerelease opt-in installed %q, want %s", reported, fixturePreleaseTag)
		}
	})
}

func TestInstallScriptRefusesAnUnsupportedArchitecture(t *testing.T) {
	install := newInstallHarness(t)
	// The real detection path is exercised by answering `uname -m` with hardware
	// no release is built for, rather than by giving the script a test-only
	// override it would then carry for users.
	install.unameMachine = "sparc64"

	stdout, stderr, err := install.run()
	if err == nil {
		t.Fatalf("install.sh succeeded on an unsupported architecture\nstdout:\n%s", stdout)
	}
	if !strings.Contains(stdout+stderr, "sparc64") {
		t.Errorf("refusal does not name the architecture it detected:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestInstallScriptLeavesTheProfileAloneWhenAsked(t *testing.T) {
	install := newInstallHarness(t)
	install.environment = append(install.environment, "WSO2_CLI_NO_PROFILE=1")

	stdout, stderr, err := install.run()
	if err != nil {
		t.Fatalf("install.sh failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	// The opt-out suppresses the edit, not the install.
	if _, statErr := os.Stat(filepath.Join(install.stateRoot, "bin", "wso2")); statErr != nil {
		t.Errorf("the opt-out suppressed the install itself: %v", statErr)
	}
	if profile := install.readProfile(t); strings.Contains(profile, installBlockMarker) {
		t.Errorf("the profile was edited despite the opt-out:\n%s", profile)
	}
	// Someone who opted out still has to be told what to do to reach the binary.
	if want := filepath.Join(install.stateRoot, "bin"); !strings.Contains(stdout, want) {
		t.Errorf("output does not tell the user to add %s to PATH:\n%s", want, stdout)
	}
}

func TestInstallScriptSucceedsWhenNoProfileCanBeDetected(t *testing.T) {
	install := newInstallHarness(t)
	install.writeProfile = false

	stdout, stderr, err := install.run()
	if err != nil {
		t.Fatalf("install.sh failed with no profile present: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	if _, statErr := os.Stat(filepath.Join(install.stateRoot, "bin", "wso2")); statErr != nil {
		t.Errorf("no binary was installed: %v", statErr)
	}
	// With nowhere safe to write, the lines the user must add themselves are the
	// only way the install can be completed, so they have to be printed.
	if !strings.Contains(stdout, "export PATH=") {
		t.Errorf("output does not print the lines to add by hand:\n%s", stdout)
	}
}

func TestInstallScriptHonoursTheStateRootVariable(t *testing.T) {
	install := newInstallHarness(t)
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	install.stateRoot = elsewhere
	install.environment = append(install.environment, "WSO2_HOME="+elsewhere)

	if _, stderr, err := install.run(); err != nil {
		t.Fatalf("install.sh failed: %v\nstderr:\n%s", err, stderr)
	}

	// The shell reads WSO2_HOME to find its state. An installer that ignored it
	// would put the binary somewhere the shell it installed does not look.
	if _, statErr := os.Stat(filepath.Join(elsewhere, "bin", "wso2")); statErr != nil {
		t.Errorf("the binary was not installed under WSO2_HOME: %v", statErr)
	}
}

// installHarness is one isolated install: a fixture release served over HTTP, a
// temporary home directory with a profile in it, and a state root nothing else
// writes to.
type installHarness struct {
	t              *testing.T
	home           string
	stateRoot      string
	profilePath    string
	environment    []string
	server         *httptest.Server
	corruptArchive bool
	unameMachine   string
	writeProfile   bool
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
		unameMachine: "",
	}
	install.server = httptest.NewServer(http.HandlerFunc(install.serve))
	t.Cleanup(install.server.Close)
	return install
}

// serve answers the two shapes the script depends on: the redirect that names
// the newest stable tag, the release listing that names the newest prerelease,
// and the download paths for archives and the checksum file.
func (i *installHarness) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/releases/latest":
		http.Redirect(w, r, "/releases/tag/"+fixtureStableTag, http.StatusFound)

	case strings.HasPrefix(r.URL.Path, "/releases/tag/"):
		// The redirect target has to answer, as the real release page does: the
		// script follows the redirect and reads the tag off the URL it lands on,
		// and a failing status there would be a failed download to it.
		if _, err := fmt.Fprintf(w, "release %s\n",
			strings.TrimPrefix(r.URL.Path, "/releases/tag/")); err != nil {
			i.t.Errorf("writing the tag page returned %v", err)
		}

	case r.URL.Path == "/releases":
		// Newest first, as the GitHub API returns them, with the fields in the
		// order and nesting the real listing uses: a struct rather than a map, so
		// tag_name precedes prerelease and each release carries a nested object
		// between them. Both are what the script's parse has to cope with.
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
				// The bytes change and the published checksum does not, which is
				// what a substituted download looks like from the client's side.
				body = append(body, "tampered"...)
			}
			if _, err := w.Write(body); err != nil {
				i.t.Errorf("writing the archive returned %v", err)
			}
		case "checksums.txt":
			sum := sha256.Sum256(i.archiveBytes(tag))
			if _, err := fmt.Fprintf(w, "%x  %s\n", sum, archive); err != nil {
				i.t.Errorf("writing the checksum file returned %v", err)
			}
		default:
			http.NotFound(w, r)
		}

	default:
		http.NotFound(w, r)
	}
}

func (i *installHarness) archiveName(tag string) string {
	extension := "tar.gz"
	if runtime.GOOS == "darwin" {
		extension = "zip"
	}
	return fmt.Sprintf("wso2-cli-%s-%s-%s.%s", tag, runtime.GOOS, runtime.GOARCH, extension)
}

// archiveBytes builds the archive this platform's release would carry, holding a
// stand-in binary that reports the tag it was packaged for. It is deterministic,
// so the checksum served alongside it describes exactly these bytes.
func (i *installHarness) archiveBytes(tag string) []byte {
	i.t.Helper()
	stand := "#!/bin/sh\necho \"WSO2 CLI   " + tag + "\"\n"
	files := []struct {
		name string
		body string
		mode int64
	}{
		{"wso2", stand, 0o755},
		{"LICENSE", "Apache License 2.0\n", 0o644},
		{"NOTICE", "WSO2 CLI\n", 0o644},
	}

	var buffer strings.Builder
	if runtime.GOOS == "darwin" {
		writer := zip.NewWriter(&buffer)
		for _, file := range files {
			header := &zip.FileHeader{Name: file.name, Method: zip.Deflate}
			header.SetMode(os.FileMode(file.mode))
			entry, err := writer.CreateHeader(header)
			if err != nil {
				i.t.Fatalf("creating %s in the zip returned %v", file.name, err)
			}
			if _, err := entry.Write([]byte(file.body)); err != nil {
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
		if _, err := tarWriter.Write([]byte(file.body)); err != nil {
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

// run invokes the real script the way a user does, with everything it could
// reach outside the test redirected: home, state root, and release origin.
func (i *installHarness) run(args ...string) (string, string, error) {
	i.t.Helper()
	if i.writeProfile {
		if err := os.WriteFile(i.profilePath, []byte("# existing profile\n"), 0o644); err != nil {
			i.t.Fatalf("writing the fixture profile returned %v", err)
		}
	}

	script := filepath.Join(repoRoot(i.t), installScriptRelPath)
	command := exec.Command("bash", append([]string{script}, args...)...)

	path := os.Getenv("PATH")
	if i.unameMachine != "" {
		path = i.shimmedUname() + string(os.PathListSeparator) + path
	}
	command.Env = append([]string{
		"HOME=" + i.home,
		"PATH=" + path,
		"SHELL=/bin/bash",
		"WSO2_CLI_RELEASE_BASE_URL=" + i.server.URL + "/releases",
		"WSO2_CLI_RELEASE_API_URL=" + i.server.URL + "/releases",
	}, i.environment...)

	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout.String(), stderr.String(), err
}

// shimmedUname returns a directory holding a uname that answers -m with the
// machine under test and defers to the real one for everything else.
func (i *installHarness) shimmedUname() string {
	i.t.Helper()
	directory := i.t.TempDir()
	shim := "#!/bin/sh\nif [ \"$1\" = \"-m\" ]; then echo " + i.unameMachine +
		"; else exec /usr/bin/uname \"$@\"; fi\n"
	if err := os.WriteFile(filepath.Join(directory, "uname"), []byte(shim), 0o755); err != nil {
		i.t.Fatalf("writing the uname shim returned %v", err)
	}
	return directory
}

func (i *installHarness) readProfile(t *testing.T) string {
	t.Helper()
	contents, err := os.ReadFile(i.profilePath)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatalf("reading the profile returned %v", err)
	}
	return string(contents)
}
