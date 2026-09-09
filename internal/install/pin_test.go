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

package install

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"testing"

	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/modules"
)

// A pin is created and cleared as a side effect of how the module is named on
// the command line, so Installed must carry both facts out of Run: without
// them, the caller can only say "Installed", and the user first learns about
// the pin when an update run passes the module over (F7).

// pinFixtureArchive is a real, extractable module archive: a gzipped tar
// carrying the executable name the installer requires. The executable's
// content is not a real program — declaredTree tolerates an executable that
// cannot declare itself, and nothing else here runs it.
func pinFixtureArchive(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	compressor := gzip.NewWriter(&buffer)
	archive := tar.NewWriter(compressor)
	content := []byte("not a real executable")
	if err := archive.WriteHeader(&tar.Header{
		Name:     ExecutableName(fixtureNamespace, fixtureShell().Platform),
		Typeflag: tar.TypeReg,
		Mode:     0o755,
		Size:     int64(len(content)),
	}); err != nil {
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

// pinFixtureInstaller is an installer whose catalog publishes two stable
// versions of the fixture module, both backed by the same real archive, so a
// pinned install can select 1.0.0 while a plain one selects the newer 1.1.0.
type pinFixtureTransport struct {
	index, namespace, archive []byte
}

func (t pinFixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	switch request.URL.Path {
	case "/" + catalog.IndexPath:
		return jsonResponse(t.index), nil
	case "/demo.json":
		return jsonResponse(t.namespace), nil
	default:
		return jsonResponse(t.archive), nil
	}
}

func pinFixtureInstaller(t *testing.T) Installer {
	t.Helper()
	archive := pinFixtureArchive(t)
	digest := sha256.Sum256(archive)
	artifact := fmt.Sprintf(`{"os": "linux", "arch": "amd64",
		"url": "https://origin.example/demo.tar.gz",
		"size": %d, "sha256": %q}`, len(archive), hex.EncodeToString(digest[:]))
	index := []byte(`{
		"schemaVersion": 1,
		"modules": [
			{"namespace": "demo", "path": "demo.json",
			 "channels": [{"channel": "stable", "version": "1.1.0"}]}
		]
	}`)
	namespace := []byte(fmt.Sprintf(`{
		"schemaVersion": 1,
		"namespace": "demo",
		"versions": [
			{"version": "1.0.0", "channel": "stable",
			 "compatibility": {"shell": ">=0.0.0", "protocolVersions": [1]},
			 "artifacts": [%s]},
			{"version": "1.1.0", "channel": "stable",
			 "compatibility": {"shell": ">=0.0.0", "protocolVersions": [1]},
			 "artifacts": [%s]}
		]
	}`, artifact, artifact))
	return Installer{
		Store: modules.NewStore(t.TempDir()),
		Client: catalog.Client{
			Origin: "https://origin.example",
			HTTP: &http.Client{Transport: pinFixtureTransport{
				index: index, namespace: namespace, archive: archive}},
		},
		Shell: fixtureShell(),
	}
}

// TestAPinnedInstallReportsThePinItCreates pins that Run says a pinned
// install pinned, and that a plain install of a never-pinned module claims
// neither a pin nor a clearing it did not perform.
func TestAPinnedInstallReportsThePinItCreates(t *testing.T) {
	installer := pinFixtureInstaller(t)

	installed, err := installer.Run(context.Background(),
		Request{Namespace: fixtureNamespace, Policy: catalog.Policy{Version: "1.0.0"}})
	if err != nil {
		t.Fatalf("Run returned %v", err)
	}
	if installed.PinnedVersion != "1.0.0" {
		t.Errorf("PinnedVersion = %q, want the pinned 1.0.0", installed.PinnedVersion)
	}
	if installed.ClearedPinnedVersion != "" {
		t.Errorf("ClearedPinnedVersion = %q, want empty: this install created a pin, it cleared none",
			installed.ClearedPinnedVersion)
	}

	policy, err := installer.Store.ReadPolicy(fixtureNamespace)
	if err != nil {
		t.Fatalf("ReadPolicy returned %v", err)
	}
	if !policy.Pinned() || policy.PinnedVersion != "1.0.0" {
		t.Errorf("the recorded policy pins %q, want 1.0.0", policy.PinnedVersion)
	}
}

// TestAPlainInstallReportsThePinItCleared pins the other half of the escape
// hatch: reinstalling without a version clears the pin, and Run names the pin
// it cleared so the caller can say so.
func TestAPlainInstallReportsThePinItCleared(t *testing.T) {
	installer := pinFixtureInstaller(t)
	if _, err := installer.Run(context.Background(),
		Request{Namespace: fixtureNamespace, Policy: catalog.Policy{Version: "1.0.0"}}); err != nil {
		t.Fatalf("the pinning install returned %v", err)
	}

	installed, err := installer.Run(context.Background(), Request{Namespace: fixtureNamespace})
	if err != nil {
		t.Fatalf("the clearing install returned %v", err)
	}
	if installed.ClearedPinnedVersion != "1.0.0" {
		t.Errorf("ClearedPinnedVersion = %q, want the cleared 1.0.0", installed.ClearedPinnedVersion)
	}
	if installed.PinnedVersion != "" {
		t.Errorf("PinnedVersion = %q, want empty: a plain install pins nothing", installed.PinnedVersion)
	}
	if installed.Version != "1.1.0" {
		t.Errorf("Version = %q, want the newest stable 1.1.0 once the pin no longer holds", installed.Version)
	}

	policy, err := installer.Store.ReadPolicy(fixtureNamespace)
	if err != nil {
		t.Fatalf("ReadPolicy returned %v", err)
	}
	if policy.Pinned() {
		t.Errorf("the recorded policy still pins %q, want no pin", policy.PinnedVersion)
	}

	// A second plain install has no pin to clear, so reporting one again
	// would claim a change this run did not make.
	repeated, err := installer.Run(context.Background(), Request{Namespace: fixtureNamespace})
	if err != nil {
		t.Fatalf("the repeated install returned %v", err)
	}
	if repeated.ClearedPinnedVersion != "" {
		t.Errorf("ClearedPinnedVersion = %q on a module that was not pinned, want empty",
			repeated.ClearedPinnedVersion)
	}
}
