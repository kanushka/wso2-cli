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
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/output"
)

// fixtureTransport answers the two small catalog documents this install needs
// and fails every other request — which is the artifact URL — so the run
// fails partway through the download rather than never starting it.
type fixtureTransport struct {
	index, namespace []byte
	failArtifact     bool
}

func (t fixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	switch request.URL.Path {
	case "/" + catalog.IndexPath:
		return jsonResponse(t.index), nil
	case "/demo.json":
		return jsonResponse(t.namespace), nil
	default:
		if t.failArtifact {
			return nil, errors.New("simulated network failure partway through the download")
		}
		return jsonResponse([]byte("archive-bytes")), nil
	}
}

func jsonResponse(body []byte) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     make(http.Header),
	}
}

// spyProgress records whether Report and Finish were called, so a test can
// prove Finish runs even when the download it was tracking failed.
type spyProgress struct {
	reported bool
	finished bool
}

func (p *spyProgress) Report(int64) { p.reported = true }
func (p *spyProgress) Finish()      { p.finished = true }

const fixtureNamespace = "demo"

func fixtureShell() modules.ShellIdentity {
	return modules.ShellIdentity{
		ProtocolVersions: []int{1},
		Platform:         modules.Platform{OS: "linux", Arch: "amd64"},
	}
}

func fixtureCatalogJSON() (index, namespace []byte) {
	index = []byte(`{
		"schemaVersion": 1,
		"modules": [
			{"namespace": "demo", "path": "demo.json",
			 "channels": [{"channel": "stable", "version": "1.0.0"}]}
		]
	}`)
	namespace = []byte(`{
		"schemaVersion": 1,
		"namespace": "demo",
		"versions": [
			{
				"version": "1.0.0",
				"channel": "stable",
				"compatibility": {"shell": ">=0.0.0", "protocolVersions": [1]},
				"artifacts": [
					{"os": "linux", "arch": "amd64",
					 "url": "https://origin.example/demo-1.0.0.tar.gz",
					 "size": 13, "sha256": "does-not-matter-for-this-test"}
				]
			}
		]
	}`)
	return index, namespace
}

// TestADownloadThatFailsPartwayStillFinishesProgress proves the wiring in
// runWithIndex: when Client.Download returns an error (a network failure
// mid-read, not a refusal before the read even starts), the Progress this run
// built still has Finish called on it. This is what stops a half-drawn
// terminal line from being left behind under the error message the run then
// prints — output_test.go's terminalProgress tests prove Finish itself clears
// a drawn line; this test proves Finish is reached at all on this path. It
// also checks the factory received the namespace being installed, which is
// what a labelled progress line (F6) needs.
func TestADownloadThatFailsPartwayStillFinishesProgress(t *testing.T) {
	index, namespace := fixtureCatalogJSON()
	spy := &spyProgress{}
	installer := Installer{
		Store: modules.NewStore(t.TempDir()),
		Client: catalog.Client{
			Origin: "https://origin.example",
			HTTP:   &http.Client{Transport: fixtureTransport{index: index, namespace: namespace, failArtifact: true}},
		},
		Shell: fixtureShell(),
		Progress: func(gotNamespace string, total int64) output.Progress {
			if gotNamespace != fixtureNamespace {
				t.Errorf("progress factory got namespace = %q, want %q", gotNamespace, fixtureNamespace)
			}
			if total != 13 {
				t.Errorf("progress factory got total = %d, want 13 (selection.Artifact.Size)", total)
			}
			return spy
		},
	}

	_, err := installer.Run(context.Background(), Request{Namespace: fixtureNamespace})
	if err == nil {
		t.Fatal("Run succeeded despite the artifact request failing, want an error")
	}
	if !spy.finished {
		t.Error("Progress.Finish was never called after a failed download")
	}
}

// TestASuccessfulDownloadAlsoFinishesProgress is the companion case: Finish
// runs on the success path too, not only when Download fails. Without it, a
// change that special-cased the error branch (finishing only on failure)
// would pass the test above and still leave a live terminal line undrawn-over
// after every successful install.
func TestASuccessfulDownloadAlsoFinishesProgress(t *testing.T) {
	index, namespace := fixtureCatalogJSON()
	spy := &spyProgress{}
	installer := Installer{
		Store: modules.NewStore(t.TempDir()),
		Client: catalog.Client{
			Origin: "https://origin.example",
			HTTP:   &http.Client{Transport: fixtureTransport{index: index, namespace: namespace, failArtifact: false}},
		},
		Shell: fixtureShell(),
		Progress: func(string, int64) output.Progress {
			return spy
		},
	}

	// The archive body this transport serves ("archive-bytes") will not match
	// the fixture's digest or size, so verify refuses it downstream — that is
	// fine here: this test is only about whether Finish runs. The actual
	// order (fixed for F7, tracked in task-2-fixes.md): Finish now runs
	// immediately after Download returns, which is BEFORE verify and activate
	// run, not after — the opposite of what an earlier version of this
	// comment, and the report, said. A stale line must not sit on screen
	// claiming to still be downloading while those two local-disk steps run.
	_, _ = installer.Run(context.Background(), Request{Namespace: fixtureNamespace})
	if !spy.reported {
		t.Error("Progress.Report was never called during a download that succeeded")
	}
	if !spy.finished {
		t.Error("Progress.Finish was never called after a successful download")
	}
}
