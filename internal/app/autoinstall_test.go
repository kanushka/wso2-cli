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

package app_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/install"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/version"
)

// A command for a product the catalog publishes but this machine does not
// have is offered an install rather than refused as a word nothing owns.

// autoInstallOrigin serves a catalog publishing one stable version of the
// "demo" product, backed by a real archive whose executable is not a real
// module: what is proved is that the product is installed and then launched,
// not what it does once launched. It records every path requested.
type autoInstallOrigin struct {
	server *httptest.Server

	mu        sync.Mutex
	requested []string
}

func newAutoInstallOrigin(t *testing.T) *autoInstallOrigin {
	t.Helper()
	platform := modules.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}
	archive := demoArchive(t, install.ExecutableName("demo", platform))
	digest := sha256.Sum256(archive)
	origin := &autoInstallOrigin{}
	origin.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin.mu.Lock()
		origin.requested = append(origin.requested, r.URL.Path)
		origin.mu.Unlock()
		switch r.URL.Path {
		case "/" + catalog.IndexPath:
			_, _ = fmt.Fprint(w, `{"schemaVersion": 1, "modules": [{"namespace": "demo",
				"title": "Demo Product", "path": "demo.json",
				"channels": [{"channel": "stable", "version": "1.0.0"}]}]}`)
		case "/demo.json":
			protocols := version.ProtocolVersions()
			_, _ = fmt.Fprintf(w, `{"schemaVersion": 1, "namespace": "demo", "versions": [
				{"version": "1.0.0", "channel": "stable",
				 "compatibility": {"shell": ">=0.0.0-0", "protocolVersions": [%d]},
				 "artifacts": [{"os": %q, "arch": %q, "url": %q, "size": %d, "sha256": %q}]}]}`,
				protocols[len(protocols)-1], platform.OS, platform.Arch,
				origin.server.URL+"/demo.tar.gz", len(archive), hex.EncodeToString(digest[:]))
		case "/demo.tar.gz":
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(origin.server.Close)
	t.Setenv(catalog.OriginEnvVar, origin.server.URL)
	return origin
}

// downloaded reports whether the archive was fetched, which is the line
// between asking the catalog about a product and installing it.
func (o *autoInstallOrigin) downloaded() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Contains(o.requested, "/demo.tar.gz")
}

func demoArchive(t *testing.T, executable string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	compressor := gzip.NewWriter(&buffer)
	archive := tar.NewWriter(compressor)
	content := []byte("not a real executable")
	if err := archive.WriteHeader(&tar.Header{Name: executable, Typeflag: tar.TypeReg, Mode: 0o755,
		Size: int64(len(content))}); err != nil {
		t.Fatalf("writing the archive header returned %v", err)
	}
	if _, err := archive.Write(content); err != nil {
		t.Fatalf("writing the archive entry returned %v", err)
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("closing the tar writer returned %v", err)
	}
	if err := compressor.Close(); err != nil {
		t.Fatalf("closing the gzip writer returned %v", err)
	}
	return buffer.Bytes()
}

// newAutoInstallShell is a shell whose prompts read answer.
func newAutoInstallShell(t *testing.T, answer string) app.Shell {
	t.Helper()
	t.Setenv(app.NoInputEnvVar, "")
	shell, _, _ := newShell(t)
	shell.Reader = strings.NewReader(answer)
	return shell
}

func installedNamespaces(t *testing.T, shell app.Shell) []string {
	t.Helper()
	namespaces, err := modules.NewStore(storeRoot(shell)).Namespaces()
	if err != nil {
		t.Fatalf("listing the store returned %v", err)
	}
	return namespaces
}

func TestAPublishedProductIsInstalledWhenTheOfferIsAccepted(t *testing.T) {
	origin := newAutoInstallOrigin(t)
	shell := newAutoInstallShell(t, "y\n")

	code, _, errOut := run(t, shell, "demo", "status")

	if !strings.Contains(errOut, "Demo Product (demo) is not installed. Install it now?") {
		t.Errorf("the offer does not name the product:\n%s", errOut)
	}
	if !strings.Contains(errOut, "Installed demo v1.0.0.") {
		t.Fatalf("exit %d, the product was not installed:\n%s", code, errOut)
	}
	if !origin.downloaded() || !slices.Contains(installedNamespaces(t, shell), "demo") {
		t.Fatal("the store does not hold the product the shell reported installing")
	}
	// The executable is not a real module, so the command fails once it is
	// launched: a usage refusal would mean it was never launched at all.
	if code == exit.Usage || strings.Contains(errOut, "shell.product_not_installed") {
		t.Errorf("exit %d, the command did not go on to run the installed product:\n%s", code, errOut)
	}
}

func TestPressingReturnAcceptsTheOffer(t *testing.T) {
	newAutoInstallOrigin(t)
	shell := newAutoInstallShell(t, "\n")

	_, _, errOut := run(t, shell, "demo")

	if !strings.Contains(errOut, "Installed demo v1.0.0.") {
		t.Fatalf("the default answer did not install the product:\n%s", errOut)
	}
}

func TestADeclinedOfferInstallsNothingAndNamesTheInstallCommand(t *testing.T) {
	origin := newAutoInstallOrigin(t)
	shell := newAutoInstallShell(t, "n\n")

	code, _, errOut := run(t, shell, "demo", "status")

	if code != exit.Usage || !strings.Contains(errOut, "shell.product_not_installed") {
		t.Fatalf("exit %d, want the not-installed refusal:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "wso2 product install demo") {
		t.Errorf("the refusal does not name the install command:\n%s", errOut)
	}
	if origin.downloaded() || len(installedNamespaces(t, shell)) != 0 {
		t.Error("a declined offer installed the product")
	}
}

func TestNoInputRefusesWithoutAskingOrInstalling(t *testing.T) {
	for name, setup := range map[string]struct {
		args []string
		env  string
	}{
		"the flag":     {args: []string{"demo", "status", "--no-input"}},
		"the variable": {args: []string{"demo", "status"}, env: "1"},
	} {
		t.Run(name, func(t *testing.T) {
			origin := newAutoInstallOrigin(t)
			shell := newAutoInstallShell(t, "y\n")
			t.Setenv(app.NoInputEnvVar, setup.env)

			code, _, errOut := run(t, shell, setup.args...)

			if code != exit.Usage || !strings.Contains(errOut, "shell.product_not_installed") {
				t.Fatalf("exit %d, want the not-installed refusal:\n%s", code, errOut)
			}
			if strings.Contains(errOut, "Install it now?") {
				t.Errorf("the shell asked although nothing may be asked:\n%s", errOut)
			}
			if origin.downloaded() || len(installedNamespaces(t, shell)) != 0 {
				t.Error("the product was installed without being asked")
			}
		})
	}
}

func TestAWordTheCatalogDoesNotPublishIsStillUnknown(t *testing.T) {
	origin := newAutoInstallOrigin(t)
	shell := newAutoInstallShell(t, "y\n")

	code, _, errOut := run(t, shell, "nosuch")

	if code != exit.Usage || !strings.Contains(errOut, "shell.unknown_command") {
		t.Fatalf("exit %d, want the unknown-command refusal:\n%s", code, errOut)
	}
	if strings.Contains(errOut, "Install it now?") || origin.downloaded() {
		t.Errorf("a word no product owns was offered an install:\n%s", errOut)
	}
}

func TestAnUnreachableCatalogLeavesTheWordUnknown(t *testing.T) {
	t.Setenv(catalog.OriginEnvVar, "http://127.0.0.1:1")
	shell := newAutoInstallShell(t, "y\n")

	code, _, errOut := run(t, shell, "demo")

	if code != exit.Usage || !strings.Contains(errOut, "shell.unknown_command") {
		t.Fatalf("exit %d, want the unknown-command refusal:\n%s", code, errOut)
	}
}

// Against the built-in origin, the release's own copy of the index decides,
// so the offer costs no request until it is accepted.
func TestTheReleasedIndexDecidesAgainstTheBuiltInOrigin(t *testing.T) {
	t.Setenv(catalog.OriginEnvVar, "")
	shell := newAutoInstallShell(t, "")
	shell.ReleasedIndex = &catalog.Index{SchemaVersion: catalog.SchemaVersion, Modules: []catalog.IndexModule{
		{Namespace: "demo", Title: "Demo Product", Path: "demo.json",
			Channels: []catalog.IndexChannel{{Channel: catalog.ChannelStable, Version: "1.0.0"}}},
		{Namespace: "early", Path: "early.json",
			Channels: []catalog.IndexChannel{{Channel: catalog.ChannelPrerelease, Version: "0.1.0-rc.1"}}},
	}}

	code, _, errOut := run(t, shell, "demo", "--no-input")
	if code != exit.Usage || !strings.Contains(errOut, "shell.product_not_installed") {
		t.Fatalf("exit %d, want the not-installed refusal for a released product:\n%s", code, errOut)
	}

	// wso2 product install selects the stable channel, so a product with only
	// a prerelease is not one an install offer could keep.
	code, _, errOut = run(t, shell, "early", "--no-input")
	if code != exit.Usage || !strings.Contains(errOut, "shell.unknown_command") {
		t.Fatalf("exit %d, want the unknown-command refusal for a prerelease-only product:\n%s", code, errOut)
	}
}

func TestHelpForAPublishedProductNamesTheInstallWithoutOfferingIt(t *testing.T) {
	origin := newAutoInstallOrigin(t)
	shell := newAutoInstallShell(t, "y\n")

	code, _, errOut := run(t, shell, "help", "demo")

	if code != exit.Usage || !strings.Contains(errOut, "shell.product_not_installed") ||
		!strings.Contains(errOut, "wso2 product install demo") {
		t.Fatalf("exit %d, want the not-installed refusal naming the install:\n%s", code, errOut)
	}
	if strings.Contains(errOut, "Install it now?") || origin.downloaded() {
		t.Errorf("help offered or performed an install:\n%s", errOut)
	}
}
