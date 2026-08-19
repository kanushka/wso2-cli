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

package catalog_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/modules"
)

const validDigest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestParseTagReadsANamespacedProductVersion(t *testing.T) {
	for tag, want := range map[string]string{
		"reference/v4.5.0":      "4.5.0",
		"reference/v4.6.0-rc.1": "4.6.0-rc.1",
		// A product version scheme far from the shell's own is read as tagged.
		"apim/v4.2.0": "4.2.0",
	} {
		namespace, version, err := catalog.ParseTag(tag)
		if err != nil {
			t.Errorf("parsing %q returned %v", tag, err)
			continue
		}
		if version != want {
			t.Errorf("parsing %q gave version %q, want %q", tag, version, want)
		}
		if !strings.HasPrefix(tag, namespace+"/") {
			t.Errorf("parsing %q gave namespace %q", tag, namespace)
		}
	}

	// Build metadata is refused rather than dropped: the shell's version parse
	// discards it, so publishing the discarded form would collapse two tags into
	// one catalog entry.
	for _, tag := range []string{
		"v1.0.0", "reference/1.0.0", "reference/vnot-a-version", "REF/v1.0.0", "",
		"reference/v1.0.0+ci.4",
		// A product scheme with a fourth component is not a semantic version,
		// which is the constraint the module receipt already imposes.
		"reference/v4.2.0.1",
	} {
		if _, _, err := catalog.ParseTag(tag); err == nil {
			t.Errorf("parsing the malformed tag %q succeeded", tag)
		}
	}
}

// Every refusal that keeps the catalog from advertising something that was not
// published. Each names the tag, because a release log that says only that
// generation failed does not tell a product team what to fix.
func TestGenerateRefusesWhatCannotBePublished(t *testing.T) {
	declaration := catalog.Declaration{SchemaVersion: catalog.SchemaVersion, Namespace: "reference"}
	sound := catalog.Release{
		Compatibility: modules.Compatibility{Shell: ">=0.1.0 <2.0.0", ProtocolVersions: []int{1}},
		Artifacts: []catalog.Artifact{{
			Platform: modules.Platform{OS: "linux", Arch: "amd64"},
			URL:      "https://downloads.example.invalid/reference-1.0.0-linux-amd64.tar.gz",
			Size:     1024,
			SHA256:   validDigest,
		}},
	}
	const tag = "reference/v1.0.0"

	withArtifacts := func(artifacts ...catalog.Artifact) catalog.Release {
		release := sound
		release.Artifacts = artifacts
		return release
	}

	cases := map[string]catalog.Input{
		"a tag naming no buildable module": {
			Tags:      []string{"nosuchproduct/v1.0.0"},
			Modules:   []catalog.Declaration{declaration},
			Published: map[string]catalog.Release{"nosuchproduct/v1.0.0": sound},
		},
		"a tag that published nothing": {
			Tags:      []string{tag},
			Modules:   []catalog.Declaration{declaration},
			Published: map[string]catalog.Release{},
		},
		"a release with no artifact": {
			Tags:      []string{tag},
			Modules:   []catalog.Declaration{declaration},
			Published: map[string]catalog.Release{tag: withArtifacts()},
		},
		"an artifact with an invalid digest": {
			Tags:    []string{tag},
			Modules: []catalog.Declaration{declaration},
			Published: map[string]catalog.Release{tag: withArtifacts(catalog.Artifact{
				Platform: modules.Platform{OS: "linux", Arch: "amd64"},
				URL:      "https://downloads.example.invalid/reference.tar.gz",
				Size:     1024,
				SHA256:   "not-a-digest",
			})},
		},
		"an artifact with no size": {
			Tags:    []string{tag},
			Modules: []catalog.Declaration{declaration},
			Published: map[string]catalog.Release{tag: withArtifacts(catalog.Artifact{
				Platform: modules.Platform{OS: "linux", Arch: "amd64"},
				URL:      "https://downloads.example.invalid/reference.tar.gz",
				SHA256:   validDigest,
			})},
		},
		"a tag listed twice": {
			Tags:      []string{tag, tag},
			Modules:   []catalog.Declaration{declaration},
			Published: map[string]catalog.Release{tag: sound},
		},
		"two modules claiming one namespace": {
			Tags:      []string{tag},
			Modules:   []catalog.Declaration{declaration, declaration},
			Published: map[string]catalog.Release{tag: sound},
		},
		"a declaration from an unknown schema": {
			Tags:      []string{tag},
			Modules:   []catalog.Declaration{{SchemaVersion: 99, Namespace: "reference"}},
			Published: map[string]catalog.Release{tag: sound},
		},
	}

	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := catalog.Generate(input); err == nil {
				t.Fatal("generation succeeded")
			}
		})
	}

	// The same input without the defect generates, so the refusals above are
	// caused by what they name and not by the fixture being unusable.
	if _, err := catalog.Generate(catalog.Input{
		Tags:      []string{tag},
		Modules:   []catalog.Declaration{declaration},
		Published: map[string]catalog.Release{tag: sound},
	}); err != nil {
		t.Fatalf("generating over a sound input returned %v", err)
	}
}

// The reference module is the catalog's first inhabitant, and discovery has to
// find it by what it declares rather than by the directory it sits in: its
// directory is named for the module and its namespace is not.
func TestDiscoverFindsTheReferenceModuleByItsDeclaredNamespace(t *testing.T) {
	root := filepath.Join("..", "..")

	declarations, err := catalog.Discover(root)
	if err != nil {
		t.Fatalf("discovering the buildable modules returned %v", err)
	}

	found := false
	for _, declaration := range declarations {
		if declaration.Namespace == "reference" {
			found = true
			if declaration.SchemaVersion != catalog.SchemaVersion {
				t.Errorf("the reference declaration uses schema version %d, want %d",
					declaration.SchemaVersion, catalog.SchemaVersion)
			}
		}
	}
	if !found {
		t.Errorf("discovery found %v, none of which declares the reference namespace", declarations)
	}
}

// Writing the catalog twice over the same tags leaves the same bytes on disk,
// which is what makes a regeneration with no new tags a no-op.
func TestWriteIsRepeatable(t *testing.T) {
	const tag = "reference/v1.0.0"
	input := catalog.Input{
		Tags:    []string{tag},
		Modules: []catalog.Declaration{{SchemaVersion: catalog.SchemaVersion, Namespace: "reference"}},
		Published: map[string]catalog.Release{tag: {
			Compatibility: modules.Compatibility{Shell: ">=0.1.0 <2.0.0", ProtocolVersions: []int{1}},
			Artifacts: []catalog.Artifact{{
				Platform: modules.Platform{OS: "linux", Arch: "amd64"},
				URL:      "https://downloads.example.invalid/reference.tar.gz",
				Size:     1024,
				SHA256:   validDigest,
			}},
		}},
	}

	generated, err := catalog.Generate(input)
	if err != nil {
		t.Fatalf("generating returned %v", err)
	}
	directory := t.TempDir()
	for attempt := 0; attempt < 2; attempt++ {
		if err := catalog.Write(directory, generated); err != nil {
			t.Fatalf("writing the catalog returned %v", err)
		}
	}

	// A namespace file that no tag answers to any more must not go on being
	// served: a stale file would disagree with what was released.
	stale := filepath.Join(directory, "modules", "gone.json")
	if err := os.WriteFile(stale, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("writing the stale file returned %v", err)
	}
	if err := catalog.Write(directory, generated); err != nil {
		t.Fatalf("writing the catalog returned %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("the stale namespace file survived regeneration: %v", err)
	}

	files, err := generated.Files()
	if err != nil {
		t.Fatalf("rendering the catalog returned %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("the catalog rendered %d files, want the index and one namespace file", len(files))
	}
	for _, file := range files {
		written, err := os.ReadFile(filepath.Join(directory, filepath.FromSlash(file.Path)))
		if err != nil {
			t.Fatalf("reading the written %s returned %v", file.Path, err)
		}
		if !bytes.Equal(written, file.Content) {
			t.Errorf("the written %s is not what was rendered:\n%s", file.Path, written)
		}
	}
}
