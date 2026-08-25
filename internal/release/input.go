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
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/wso2/wso2-cli/internal/catalog"
)

// ErrDeclarationMissing indicates a tag names a buildable module but the
// module's declaration does not exist at that ref — most often a tag cut from
// the wrong commit. Such a tag is not a valid release, so it is excluded from
// the catalog rather than failing the assembly for every other tag.
var ErrDeclarationMissing = errors.New("module declaration missing at tag")

// Asset is one file a tag's release published.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size int64  `json:"size"`
}

// Published is what one tag actually put on the release page: the assets and
// the digests published beside them.
type Published struct {
	Assets []Asset
	// Digests are the SHA-256 digests from the checksum file the release
	// published, by asset name. They come from that file rather than from the
	// release API, which reports no digest.
	Digests map[string]string
}

// Releases is the released world the catalog input is assembled from. It is an
// interface because the real implementation reads git and the release page,
// neither of which a test may have, and because the assembly is the part worth
// proving.
type Releases interface {
	// ModuleTags reports every module tag that exists, in any order.
	ModuleTags() ([]string, error)
	// DeclarationAt reports the module declaration as it stood at one tag. It
	// is read at the tag rather than from the checkout, because the checkout
	// describes the version being released and not the versions already
	// released: a module that widened its protocol range last week did not
	// widen the entry it published last year.
	DeclarationAt(tag, directory string) (catalog.Declaration, error)
	// PublishedAt reports what one tag's release published.
	PublishedAt(tag string) (Published, error)
}

// AssembleInput builds the catalog generator's input from the tags that exist
// and what each one published.
//
// This is the wiring between a release and the catalog: nothing is curated and
// nothing is carried by hand, so a regeneration over an unchanged tag set
// reproduces the same input and therefore the same files.
func AssembleInput(releases Releases, declarations []catalog.Declaration) (catalog.Input, error) {
	byNamespace := map[string]catalog.Declaration{}
	for _, declaration := range declarations {
		byNamespace[declaration.Namespace] = declaration
	}

	tags, err := releases.ModuleTags()
	if err != nil {
		return catalog.Input{}, err
	}
	sort.Strings(tags)

	input := catalog.Input{Modules: declarations, Published: map[string]catalog.Release{}}
	for _, tag := range tags {
		namespace, version, err := catalog.ParseTag(tag)
		if err != nil {
			return catalog.Input{}, err
		}
		declaration, buildable := byNamespace[namespace]
		if !buildable {
			return catalog.Input{}, fmt.Errorf(
				"release: the tag %q names no buildable module in namespace %q", tag, namespace)
		}
		declaredAtTag, err := releases.DeclarationAt(tag, declaration.Directory)
		if errors.Is(err, ErrDeclarationMissing) {
			continue
		}
		if err != nil {
			return catalog.Input{}, err
		}
		published, err := releases.PublishedAt(tag)
		if err != nil {
			return catalog.Input{}, err
		}
		artifacts, err := artifactsFor(tag, namespace, version, published)
		if err != nil {
			return catalog.Input{}, err
		}
		input.Tags = append(input.Tags, tag)
		input.Published[tag] = catalog.Release{
			Compatibility: declaredAtTag.Compatibility,
			Capabilities:  declaredAtTag.Capabilities,
			Artifacts:     artifacts,
		}
	}
	return input, nil
}

// artifactsFor matches a release's assets to the platforms a module publishes
// for. Every supported platform has to be there: a release missing one would
// publish a catalog that silently stops serving a platform, which a user reads
// as their platform being unsupported rather than as a broken release.
func artifactsFor(tag, namespace, version string, published Published) ([]catalog.Artifact, error) {
	assets := map[string]Asset{}
	for _, asset := range published.Assets {
		assets[asset.Name] = asset
	}

	artifacts := make([]catalog.Artifact, 0, len(Platforms))
	for _, platform := range Platforms {
		name := ArchiveName(namespace, version, platform)
		asset, uploaded := assets[name]
		if !uploaded {
			return nil, fmt.Errorf("release: the release for %q published no %s, "+
				"so it published nothing for %s", tag, name, platform)
		}
		if asset.URL == "" {
			return nil, fmt.Errorf("release: the release for %q published %s at no URL", tag, name)
		}
		if asset.Size <= 0 {
			return nil, fmt.Errorf("release: the release for %q published %s with no size", tag, name)
		}
		digest, listed := published.Digests[name]
		if !listed {
			return nil, fmt.Errorf("release: the checksum file for %q does not cover %s, "+
				"so nothing could verify a download of it", tag, name)
		}
		artifacts = append(artifacts, catalog.Artifact{
			Platform: platform,
			URL:      asset.URL,
			Size:     asset.Size,
			SHA256:   strings.ToLower(digest),
		})
	}
	return artifacts, nil
}
