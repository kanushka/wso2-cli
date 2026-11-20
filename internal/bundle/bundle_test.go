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

package bundle_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wso2/wso2-cli/internal/bundle"
	"github.com/wso2/wso2-cli/internal/modules"
)

func TestAManifestReadsBackAsWritten(t *testing.T) {
	dir := t.TempDir()
	written := bundle.Manifest{
		Namespace: "intg", ModuleVersion: "0.1.0-test.1", ShellVersion: "0.1.3-test.1", CommandName: "ws",
		Platform: modules.Platform{OS: "darwin", Arch: "arm64"}, Archive: "wso2-module-intg.tar.gz",
		ContextFile: "wso2cloud-dev.yaml", ContextName: "wso2cloud-dev",
	}
	if err := written.Write(dir); err != nil {
		t.Fatal(err)
	}

	read, err := bundle.Read(dir)
	if err != nil {
		t.Fatalf("reading the manifest back failed: %v", err)
	}
	if read != written {
		t.Errorf("read %+v, want %+v", read, written)
	}
}

// An installer handed a manifest that leaves something out refuses it rather
// than installing half a bundle.
func TestAManifestMissingAMemberIsRefused(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, bundle.ManifestName), []byte(`{"namespace":"intg"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := bundle.Read(dir); err == nil {
		t.Error("a manifest naming only its namespace was accepted")
	}
}

func TestAWindowsExecutableIsNamedWithItsSuffix(t *testing.T) {
	if got := bundle.ExecutableName("ws", modules.Platform{OS: "windows", Arch: "amd64"}); got != "ws.exe" {
		t.Errorf("windows name = %q, want ws.exe", got)
	}
	if got := bundle.ExecutableName("ws", modules.Platform{OS: "linux", Arch: "amd64"}); got != "ws" {
		t.Errorf("linux name = %q, want ws", got)
	}
}
