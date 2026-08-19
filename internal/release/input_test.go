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
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/release"
)

// fakeReleases is a released world a test can have: a tag set, the declaration
// each tag carried, and the assets each release published.
type fakeReleases struct {
	tags         []string
	declarations map[string]catalog.Declaration
	published    map[string]release.Published
}

func (f fakeReleases) ModuleTags() ([]string, error) { return f.tags, nil }

func (f fakeReleases) DeclarationAt(tag, _ string) (catalog.Declaration, error) {
	declaration, found := f.declarations[tag]
	if !found {
		return catalog.Declaration{}, fmt.Errorf("no declaration at %s", tag)
	}
	return declaration, nil
}

func (f fakeReleases) PublishedAt(tag string) (release.Published, error) {
	published, found := f.published[tag]
	if !found {
		return release.Published{}, fmt.Errorf("nothing published at %s", tag)
	}
	return published, nil
}

// releasedWorld is a tag set whose releases published every platform.
func releasedWorld(tags ...string) fakeReleases {
	world := fakeReleases{
		declarations: map[string]catalog.Declaration{},
		published:    map[string]release.Published{},
	}
	for _, tag := range tags {
		namespace, version, err := catalog.ParseTag(tag)
		if err != nil {
			panic(err)
		}
		world.tags = append(world.tags, tag)
		world.declarations[tag] = catalog.Declaration{
			SchemaVersion: catalog.SchemaVersion,
			Namespace:     namespace,
			Compatibility: modules.Compatibility{Shell: ">=0.1.0 <2.0.0", ProtocolVersions: []int{2}},
			Capabilities:  modules.Capabilities{AuthAudiences: []string{"reference-status"}},
			Directory:     "examples/reference-module",
		}
		published := release.Published{Digests: map[string]string{}}
		for _, platform := range release.Platforms {
			name := release.ArchiveName(namespace, version, platform)
			published.Assets = append(published.Assets, release.Asset{
				Name: name,
				URL:  "https://releases.example.invalid/" + tag + "/" + name,
				Size: 4096,
			})
			published.Digests[name] = fmt.Sprintf("%x", sha256.Sum256([]byte(name)))
		}
		world.published[tag] = published
	}
	return world
}

func referenceDeclarations() []catalog.Declaration {
	return []catalog.Declaration{{
		SchemaVersion: catalog.SchemaVersion,
		Namespace:     "reference",
		Compatibility: modules.Compatibility{Shell: ">=0.1.0 <2.0.0", ProtocolVersions: []int{2}},
		Directory:     "examples/reference-module",
	}}
}

func TestAssembleInputGeneratesACatalogFromTheTagsThatExist(t *testing.T) {
	world := releasedWorld("reference/v4.5.0", "reference/v4.4.0")
	input, err := release.AssembleInput(world, referenceDeclarations())
	if err != nil {
		t.Fatalf("assembling the input returned %v", err)
	}
	generated, err := catalog.Generate(input)
	if err != nil {
		t.Fatalf("generating over the assembled input returned %v", err)
	}
	if len(generated.Namespaces) != 1 || len(generated.Namespaces[0].Versions) != 2 {
		t.Fatalf("the catalog carries %d namespace(s)", len(generated.Namespaces))
	}
	newest := generated.Namespaces[0].Versions[0]
	if newest.Version != "4.5.0" {
		t.Errorf("the newest published version is %q", newest.Version)
	}
	if len(newest.Artifacts) != len(release.Platforms) {
		t.Errorf("the entry publishes %d artifact(s) for %d platform(s)",
			len(newest.Artifacts), len(release.Platforms))
	}
	// A module installed from the catalog has to be able to use the broker it
	// declares, which it cannot if the entry carries no capabilities.
	if len(newest.Capabilities.AuthAudiences) != 1 {
		t.Errorf("the entry publishes no capabilities: %+v", newest.Capabilities)
	}
}

// Regenerating over an unchanged tag set may not produce a change: nothing in
// the assembly is ordered by the order the tags arrived in.
func TestAssembleInputIsDeterministic(t *testing.T) {
	first, err := release.AssembleInput(releasedWorld("reference/v4.4.0", "reference/v4.5.0"),
		referenceDeclarations())
	if err != nil {
		t.Fatalf("assembling the input returned %v", err)
	}
	second, err := release.AssembleInput(releasedWorld("reference/v4.5.0", "reference/v4.4.0"),
		referenceDeclarations())
	if err != nil {
		t.Fatalf("assembling the input returned %v", err)
	}
	if fmt.Sprint(first.Tags) != fmt.Sprint(second.Tags) {
		t.Fatalf("the tag order differs: %v against %v", first.Tags, second.Tags)
	}
}

func TestAssembleInputRefusesAReleaseMissingAPlatform(t *testing.T) {
	world := releasedWorld("reference/v4.5.0")
	published := world.published["reference/v4.5.0"]
	published.Assets = published.Assets[1:]
	world.published["reference/v4.5.0"] = published

	_, err := release.AssembleInput(world, referenceDeclarations())
	if err == nil {
		t.Fatal("a release missing a platform was assembled into a catalog")
	}
	if !strings.Contains(err.Error(), release.Platforms[0].String()) {
		t.Errorf("the refusal does not name the missing platform: %v", err)
	}
}

func TestAssembleInputRefusesAnArtifactTheChecksumFileDoesNotCover(t *testing.T) {
	world := releasedWorld("reference/v4.5.0")
	published := world.published["reference/v4.5.0"]
	delete(published.Digests, published.Assets[0].Name)
	world.published["reference/v4.5.0"] = published

	if _, err := release.AssembleInput(world, referenceDeclarations()); err == nil {
		t.Fatal("an artifact no checksum covers was published to the catalog")
	}
}

func TestAssembleInputRefusesATagNamingNoBuildableModule(t *testing.T) {
	world := releasedWorld("ghost/v1.0.0")
	if _, err := release.AssembleInput(world, referenceDeclarations()); err == nil {
		t.Fatal("a tag naming no buildable module was assembled into a catalog")
	}
}
