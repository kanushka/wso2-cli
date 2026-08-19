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

package acceptance_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/wso2/wso2-cli/internal/catalog"
)

// The index is the document every update check reads, so what it has to carry
// is one line per product namespace naming the latest version on each channel,
// and the location of the history to fetch when a specific version must be
// selected.
func TestCatalogIndexNamesTheLatestVersionPerChannel(t *testing.T) {
	origin := newCatalogHarness(t, catalogStable, catalogPrerelease, catalogOlderStable)

	index := origin.index(t)

	if index.SchemaVersion != catalog.SchemaVersion {
		t.Errorf("index schema version = %d, want %d", index.SchemaVersion, catalog.SchemaVersion)
	}
	if len(index.Modules) != 1 {
		t.Fatalf("index carries %d namespaces, want one:\n%s",
			len(index.Modules), origin.fetch(t, catalog.IndexPath))
	}
	entry := index.Modules[0]
	if entry.Namespace != catalogNamespace {
		t.Errorf("index namespace = %q, want %q", entry.Namespace, catalogNamespace)
	}
	if entry.Path != catalog.NamespacePath(catalogNamespace) {
		t.Errorf("index names the history at %q, want %q",
			entry.Path, catalog.NamespacePath(catalogNamespace))
	}

	// The newest release overall is the prerelease, so a stable entry naming it
	// would mean the channel was ignored, and a missing stable entry would mean
	// the newest release had been taken as the only answer.
	want := map[string]string{
		catalog.ChannelStable:     "4.5.0",
		catalog.ChannelPrerelease: "4.6.0-rc.1",
	}
	got := map[string]string{}
	for _, channel := range entry.Channels {
		got[channel.Channel] = channel.Version
	}
	if len(got) != len(want) {
		t.Fatalf("index carries channels %v, want %v", got, want)
	}
	for channel, version := range want {
		if got[channel] != version {
			t.Errorf("index %s channel = %q, want %q", channel, got[channel], version)
		}
	}
}

// The namespace file is the whole history, and every fact the shell needs to
// select and verify a version has to be in it.
func TestCatalogNamespaceFileCarriesTheFullVersionHistory(t *testing.T) {
	origin := newCatalogHarness(t, catalogStable, catalogPrerelease, catalogOlderStable)

	// Fetched by the path the index publishes, so the two documents are proven
	// to agree rather than assumed to.
	history := origin.namespaceFile(t, origin.index(t).Modules[0].Path)

	if history.Namespace != catalogNamespace {
		t.Errorf("history namespace = %q, want %q", history.Namespace, catalogNamespace)
	}
	wantVersions := []string{"4.6.0-rc.1", "4.5.0", "4.4.0"}
	if len(history.Versions) != len(wantVersions) {
		t.Fatalf("history carries %d versions, want %d", len(history.Versions), len(wantVersions))
	}
	for index, version := range history.Versions {
		if version.Version != wantVersions[index] {
			t.Errorf("history version %d = %q, want %q", index, version.Version, wantVersions[index])
		}
		if version.Channel == "" {
			t.Errorf("version %s carries no channel", version.Version)
		}
		if version.Compatibility.Shell == "" {
			t.Errorf("version %s carries no shell range", version.Version)
		}
		if len(version.Compatibility.ProtocolVersions) == 0 {
			t.Errorf("version %s carries no protocol version", version.Version)
		}
		if len(version.Artifacts) != len(catalogPlatforms) {
			t.Errorf("version %s carries %d artifacts, want %d",
				version.Version, len(version.Artifacts), len(catalogPlatforms))
		}
		for _, artifact := range version.Artifacts {
			// Downloading what the entry points at and hashing it is the only
			// way to know the size and digest describe the published archive
			// rather than something plausible. What this proves is that the
			// archive matches its manifest entry; it does not prove the
			// manifest is authentic, because nothing here is signed.
			body := origin.fetch(t, artifactPath(t, origin, artifact.URL))
			if int64(len(body)) != artifact.Size {
				t.Errorf("version %s %s/%s publishes size %d, downloaded %d",
					version.Version, artifact.OS, artifact.Arch, artifact.Size, len(body))
			}
			digest := fmt.Sprintf("%x", sha256.Sum256(body))
			if digest != artifact.SHA256 {
				t.Errorf("version %s %s/%s publishes digest %s, downloaded %s",
					version.Version, artifact.OS, artifact.Arch, artifact.SHA256, digest)
			}
		}
	}
}

// A regeneration with no new tags must produce no change, or a release would
// churn the published catalog and a reviewer could not tell a real change from
// a reordering.
func TestCatalogGenerationIsDeterministic(t *testing.T) {
	origin := newCatalogHarness(t, catalogStable, catalogPrerelease, catalogOlderStable)
	first := copyFiles(origin.files)

	// The same tags in a different order are the same tag set: a tag set has no
	// order, so nothing about the output may depend on the one it arrived in.
	second := origin.generate(catalogOlderStable, catalogStable, catalogPrerelease)

	for _, published := range []string{catalog.IndexPath, catalog.NamespacePath(catalogNamespace)} {
		if !bytes.Equal(first[published], second[published]) {
			t.Errorf("regenerating %s produced different bytes:\nfirst:\n%s\nsecond:\n%s",
				published, first[published], second[published])
		}
	}
}

// copyFiles snapshots a generation, because the harness serves only the most
// recent one.
func copyFiles(files map[string][]byte) map[string][]byte {
	copied := map[string][]byte{}
	for path, content := range files {
		copied[path] = append([]byte(nil), content...)
	}
	return copied
}

// The index exists so an update check costs one request whose size does not
// grow with release history. Extending the history must therefore leave it
// untouched.
func TestCatalogIndexDoesNotGrowWhenReleaseHistoryIsExtended(t *testing.T) {
	origin := newCatalogHarness(t, catalogStable, catalogPrerelease)
	short := copyFiles(origin.files)
	shortHistory := len(origin.namespaceFile(t, catalog.NamespacePath(catalogNamespace)).Versions)

	// An older release extends the history and changes no latest version, so
	// the index is not merely the same size but the same bytes.
	long := copyFiles(origin.generate(catalogStable, catalogPrerelease, catalogOlderStable))
	if !bytes.Equal(short[catalog.IndexPath], long[catalog.IndexPath]) {
		t.Errorf("adding an older release changed the index:\nbefore:\n%s\nafter:\n%s",
			short[catalog.IndexPath], long[catalog.IndexPath])
	}

	// A newer release on an existing channel moves one version string and adds
	// no line, so the index stays the same size while the history grows again.
	newer := origin.generate(catalogStable, catalogPrerelease, catalogOlderStable, catalogAddedStable)
	index := origin.index(t)
	if len(index.Modules) != 1 || len(index.Modules[0].Channels) != 2 {
		t.Errorf("the index carries %d namespaces and %d channels, want one and two",
			len(index.Modules), len(index.Modules[0].Channels))
	}
	// One byte of slack: a version string may be one character longer.
	if got, want := len(newer[catalog.IndexPath]), len(long[catalog.IndexPath]); got > want+1 {
		t.Errorf("the index grew from %d bytes to %d when release history was extended", want, got)
	}
	if history := len(origin.namespaceFile(t, catalog.NamespacePath(catalogNamespace)).Versions); history <= shortHistory {
		t.Errorf("the history went from %d versions to %d, so the index's flatness proves nothing",
			shortHistory, history)
	}
}

// A tag naming no buildable module would publish an entry pointing at an
// artifact that was never built, so it fails generation instead.
func TestCatalogGenerationRefusesATagNamingNoBuildableModule(t *testing.T) {
	declarations, err := catalog.Discover(repoRoot(t))
	if err != nil {
		t.Fatalf("discovering the buildable modules returned %v", err)
	}

	const orphan = "nosuchproduct/v1.0.0"
	_, err = catalog.Generate(catalog.Input{
		Tags:    []string{orphan},
		Modules: declarations,
		Published: map[string]catalog.Release{
			orphan: {},
		},
	})
	if err == nil {
		t.Fatal("generating over a tag naming no buildable module succeeded")
	}
	for _, want := range []string{orphan, "nosuchproduct"} {
		if !bytes.Contains([]byte(err.Error()), []byte(want)) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
}

// The manifest carries no trust fields. Empty or fabricated ones would suggest
// a trust chain that does not exist: nothing published here is signed, and the
// digest proves only that an archive matches its entry.
func TestCatalogManifestCarriesNoTrustFields(t *testing.T) {
	origin := newCatalogHarness(t, catalogStable, catalogPrerelease, catalogOlderStable)

	forbidden := []string{"publisher", "signature", "provenance", "sbom", "revocation", "revoked"}
	for _, published := range []string{catalog.IndexPath, catalog.NamespacePath(catalogNamespace)} {
		var document any
		if err := json.Unmarshal(origin.fetch(t, published), &document); err != nil {
			t.Fatalf("the published %s is not readable: %v", published, err)
		}
		for _, key := range jsonKeys(document) {
			for _, banned := range forbidden {
				if key == banned {
					t.Errorf("%s carries the trust field %q", published, key)
				}
			}
		}
	}
}

// A module tag carries the product's own version scheme, which the shell never
// compares against its own. A version far ahead of the shell's 0.4.2 has to be
// read and published exactly as tagged.
func TestCatalogReadsAProductFlavouredVersionScheme(t *testing.T) {
	origin := newCatalogHarness(t, catalogStable)

	history := origin.namespaceFile(t, catalog.NamespacePath(catalogNamespace))
	if len(history.Versions) != 1 {
		t.Fatalf("history carries %d versions, want one", len(history.Versions))
	}
	if history.Versions[0].Version != "4.5.0" {
		t.Errorf("history version = %q, want the tagged 4.5.0", history.Versions[0].Version)
	}
	if history.Versions[0].Channel != catalog.ChannelStable {
		t.Errorf("history channel = %q, want %q", history.Versions[0].Channel, catalog.ChannelStable)
	}
	if history.Versions[0].Version == testShellVersion {
		t.Error("the fixture module version equals the shell version, so it proves nothing")
	}
}

// artifactPath turns a published artifact URL back into the origin path it
// names, so a test downloads what the catalog points at rather than a path it
// composed for itself.
func artifactPath(t *testing.T, origin *catalogHarness, url string) string {
	t.Helper()
	prefix := origin.server.URL + "/"
	if len(url) <= len(prefix) || url[:len(prefix)] != prefix {
		t.Fatalf("the artifact URL %q does not point at the catalog origin", url)
	}
	return url[len(prefix):]
}

// jsonKeys reports every object key in a decoded document, at any depth.
func jsonKeys(document any) []string {
	switch value := document.(type) {
	case map[string]any:
		keys := []string{}
		for key, nested := range value {
			keys = append(keys, key)
			keys = append(keys, jsonKeys(nested)...)
		}
		return keys
	case []any:
		keys := []string{}
		for _, nested := range value {
			keys = append(keys, jsonKeys(nested)...)
		}
		return keys
	default:
		return nil
	}
}
