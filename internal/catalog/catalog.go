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

// Package catalog generates the module catalog from the tags that exist.
//
// The catalog is a build output rather than a curated artifact: there is no
// reviewed metadata and no submission step, so nothing can be forgotten and
// nothing can disagree with what was actually released. Two files are produced
// and served from the origin that already serves the install scripts:
//
//   - index.json holds one entry per product namespace with the latest version
//     on each channel. Its size is bounded by namespaces and channels rather
//     than by release history, so one request serves an update check however
//     long the project has been releasing.
//   - modules/<namespace>.json holds the full version history for one
//     namespace: per version, its channel, the protocol versions it supports,
//     the shell range it declares, and for each platform an artifact URL, size,
//     and digest.
//
// The digest proves that a downloaded archive is the one the manifest entry
// describes. It does not prove that the manifest is authentic: artifacts are
// unsigned, and integrity rests on the digest together with HTTPS. The manifest
// carries no publisher, signature, provenance, SBOM, or revocation field,
// because with one repository and one CODEOWNERS file the question those fields
// existed to answer is not asked, and empty values would suggest a trust chain
// that does not exist.
package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/semver"
)

// SchemaVersion is the only catalog format this generator writes and the shell
// reads. An unknown schema version fails closed rather than being partially
// interpreted, exactly as a module receipt does.
const SchemaVersion = 1

// The channels a version can fall on. A channel is derived from the version
// rather than declared, so no release can land on a channel by mistake: a
// version carrying a prerelease identifier is a prerelease and every other
// version is stable.
const (
	ChannelStable     = "stable"
	ChannelPrerelease = "prerelease"
)

// IndexPath and namespacePathFormat are the published locations of the two
// files, relative to the catalog origin. They are part of the contract with the
// shell and with whatever serves them.
const (
	IndexPath           = "index.json"
	namespacePathFormat = "modules/%s.json"
)

// NamespacePath is where the version history for one namespace is published.
func NamespacePath(namespace string) string {
	return fmt.Sprintf(namespacePathFormat, namespace)
}

// Declaration names a module the repository can build. It is what makes a tag
// legitimate: a tag whose namespace no declaration answers to names nothing
// that was ever built, and generation fails rather than publishing an entry
// pointing at an artifact that does not exist.
type Declaration struct {
	SchemaVersion int    `json:"schemaVersion"`
	Namespace     string `json:"namespace"`
}

// Artifact is one platform's published archive for one module version.
type Artifact struct {
	Platform modules.Platform `json:"platform"`
	URL      string           `json:"url"`
	Size     int64            `json:"size"`
	SHA256   string           `json:"sha256"`
}

// Release is what one module tag actually published: the compatibility that
// build declared and the archives that were uploaded for it. It is recorded per
// tag rather than read from the checkout, because the checkout describes the
// version being released and not the versions already released.
type Release struct {
	Compatibility modules.Compatibility `json:"compatibility"`
	Artifacts     []Artifact            `json:"artifacts"`
}

// Input is one generation's whole world: the module tags that exist, the
// modules the repository can build, and what each tag published.
type Input struct {
	// Tags are module tags of the form "<namespace>/vX.Y.Z", in any order.
	Tags []string `json:"tags"`
	// Modules are the buildable modules, one declaration per namespace. The
	// command reads them from the checkout rather than from the input file.
	Modules []Declaration `json:"-"`
	// Published records, by tag, what that tag's release put on the origin.
	Published map[string]Release `json:"published"`
}

// File is one generated catalog document and the path it is published at.
type File struct {
	Path    string
	Content []byte
}

// Catalog is a generated catalog, ready to be written to a site directory.
type Catalog struct {
	Index      Index
	Namespaces []NamespaceFile
}

// Index is the bounded document every update check reads.
type Index struct {
	SchemaVersion int           `json:"schemaVersion"`
	Modules       []IndexModule `json:"modules"`
}

// IndexModule is one product namespace's line in the index.
type IndexModule struct {
	Namespace string `json:"namespace"`
	// Path is where this namespace's full version history is published, so a
	// shell that must select a specific version knows what to fetch without
	// reconstructing the convention.
	Path     string         `json:"path"`
	Channels []IndexChannel `json:"channels"`
}

// IndexChannel is the latest version on one channel.
type IndexChannel struct {
	Channel string `json:"channel"`
	Version string `json:"version"`
}

// NamespaceFile is the full version history for one product namespace.
type NamespaceFile struct {
	SchemaVersion int       `json:"schemaVersion"`
	Namespace     string    `json:"namespace"`
	Versions      []Version `json:"versions"`
}

// Version is one released module version, newest first within its file.
type Version struct {
	Version       string                `json:"version"`
	Channel       string                `json:"channel"`
	Compatibility modules.Compatibility `json:"compatibility"`
	Artifacts     []VersionArtifact     `json:"artifacts"`
}

// VersionArtifact is one platform's archive. The digest proves the archive
// matches this entry; it does not prove this entry is authentic.
//
// The platform is embedded rather than restated, so the published os and arch
// keys are the shell's own platform type and cannot drift from it.
type VersionArtifact struct {
	modules.Platform
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// ParseTag splits a module tag into its namespace and version.
//
// A product module's tags are prefixed by its namespace and are free to carry
// the product's own version scheme: the version here is only required to be a
// semantic version, never to resemble the shell's.
func ParseTag(tag string) (namespace, version string, err error) {
	namespace, rest, found := strings.Cut(tag, "/")
	if !found {
		return "", "", fmt.Errorf("catalog: tag %q is not of the form <namespace>/vX.Y.Z", tag)
	}
	if !modules.ValidNamespace(namespace) {
		return "", "", fmt.Errorf("catalog: tag %q names an invalid namespace %q", tag, namespace)
	}
	if !strings.HasPrefix(rest, "v") {
		return "", "", fmt.Errorf("catalog: tag %q has a version without a leading \"v\"", tag)
	}
	if strings.Contains(rest, "+") {
		// Build metadata never affects a compatibility decision, so the shell's
		// version parse discards it. Publishing the discarded form would let two
		// tags collapse into one catalog entry, so such a tag is refused instead.
		return "", "", fmt.Errorf("catalog: tag %q carries build metadata, which a published version may not", tag)
	}
	parsed, parseErr := semver.Parse(rest)
	if parseErr != nil {
		return "", "", fmt.Errorf("catalog: tag %q has an unreadable version: %w", tag, parseErr)
	}
	return namespace, parsed.String(), nil
}

// Channel reports the channel a version falls on.
func Channel(version semver.Version) string {
	if version.Prerelease == "" {
		return ChannelStable
	}
	return ChannelPrerelease
}

// Generate turns the tags that exist into the catalog. It is a pure function of
// its input and imposes a total order on everything it emits, so regenerating
// over an unchanged tag set produces byte-identical files.
func Generate(input Input) (Catalog, error) {
	declared := map[string]bool{}
	for _, declaration := range input.Modules {
		if declaration.SchemaVersion != SchemaVersion {
			return Catalog{}, fmt.Errorf("catalog: module declaration for %q uses unsupported schema version %d",
				declaration.Namespace, declaration.SchemaVersion)
		}
		if !modules.ValidNamespace(declaration.Namespace) {
			return Catalog{}, fmt.Errorf("catalog: module declaration has an invalid namespace %q",
				declaration.Namespace)
		}
		if declared[declaration.Namespace] {
			return Catalog{}, fmt.Errorf("catalog: two modules declare the namespace %q", declaration.Namespace)
		}
		declared[declaration.Namespace] = true
	}

	histories := map[string][]Version{}
	seen := map[string]bool{}
	for _, tag := range input.Tags {
		namespace, version, err := ParseTag(tag)
		if err != nil {
			return Catalog{}, err
		}
		if !declared[namespace] {
			return Catalog{}, fmt.Errorf("catalog: tag %q names no buildable module in namespace %q",
				tag, namespace)
		}
		if seen[tag] {
			return Catalog{}, fmt.Errorf("catalog: tag %q appears twice", tag)
		}
		seen[tag] = true

		release, published := input.Published[tag]
		if !published {
			return Catalog{}, fmt.Errorf("catalog: tag %q published no release", tag)
		}
		entry, err := versionEntry(tag, version, release)
		if err != nil {
			return Catalog{}, err
		}
		histories[namespace] = append(histories[namespace], entry)
	}

	catalog := Catalog{Index: Index{SchemaVersion: SchemaVersion}}
	for _, namespace := range sortedKeys(histories) {
		versions := histories[namespace]
		if err := sortVersions(versions); err != nil {
			return Catalog{}, err
		}
		catalog.Namespaces = append(catalog.Namespaces, NamespaceFile{
			SchemaVersion: SchemaVersion,
			Namespace:     namespace,
			Versions:      versions,
		})
		catalog.Index.Modules = append(catalog.Index.Modules, IndexModule{
			Namespace: namespace,
			Path:      NamespacePath(namespace),
			Channels:  latestPerChannel(versions),
		})
	}
	return catalog, nil
}

// versionEntry validates and normalizes what one tag published.
func versionEntry(tag, version string, release Release) (Version, error) {
	parsed, err := semver.Parse(version)
	if err != nil {
		return Version{}, fmt.Errorf("catalog: tag %q has an unreadable version: %w", tag, err)
	}
	if _, err := semver.ParseRange(release.Compatibility.Shell); err != nil {
		return Version{}, fmt.Errorf("catalog: tag %q declares an unreadable shell range: %w", tag, err)
	}
	if len(release.Compatibility.ProtocolVersions) == 0 {
		return Version{}, fmt.Errorf("catalog: tag %q declares no protocol version", tag)
	}
	if len(release.Artifacts) == 0 {
		return Version{}, fmt.Errorf("catalog: tag %q published no artifact", tag)
	}

	protocols := append([]int(nil), release.Compatibility.ProtocolVersions...)
	sort.Ints(protocols)

	platforms := map[string]bool{}
	artifacts := make([]VersionArtifact, 0, len(release.Artifacts))
	for _, artifact := range release.Artifacts {
		switch {
		case artifact.Platform.OS == "" || artifact.Platform.Arch == "":
			return Version{}, fmt.Errorf("catalog: tag %q published an artifact for no platform", tag)
		case artifact.URL == "":
			return Version{}, fmt.Errorf("catalog: tag %q published a %s artifact with no URL",
				tag, artifact.Platform)
		case artifact.Size <= 0:
			return Version{}, fmt.Errorf("catalog: tag %q published a %s artifact with no size",
				tag, artifact.Platform)
		case !validDigest(artifact.SHA256):
			return Version{}, fmt.Errorf("catalog: tag %q published a %s artifact with an invalid digest %q",
				tag, artifact.Platform, artifact.SHA256)
		case platforms[artifact.Platform.String()]:
			return Version{}, fmt.Errorf("catalog: tag %q published two %s artifacts",
				tag, artifact.Platform)
		}
		platforms[artifact.Platform.String()] = true
		artifacts = append(artifacts, VersionArtifact{
			Platform: artifact.Platform,
			URL:      artifact.URL,
			Size:     artifact.Size,
			SHA256:   strings.ToLower(artifact.SHA256),
		})
	}
	sort.Slice(artifacts, func(left, right int) bool {
		return artifacts[left].String() < artifacts[right].String()
	})

	return Version{
		Version: parsed.String(),
		Channel: Channel(parsed),
		Compatibility: modules.Compatibility{
			Shell:            release.Compatibility.Shell,
			ProtocolVersions: protocols,
		},
		Artifacts: artifacts,
	}, nil
}

// validDigest reports whether a string is a lowercase or uppercase hex SHA-256.
func validDigest(digest string) bool {
	if len(digest) != 64 {
		return false
	}
	for _, character := range digest {
		switch {
		case character >= '0' && character <= '9':
		case character >= 'a' && character <= 'f':
		case character >= 'A' && character <= 'F':
		default:
			return false
		}
	}
	return true
}

// sortVersions orders a namespace's history newest first. Ordering the file is
// a presentation choice and not a selection one: the shell still selects by
// channel and pin policy, and never assumes the newest release is usable.
func sortVersions(versions []Version) error {
	var failure error
	sort.SliceStable(versions, func(left, right int) bool {
		leftVersion, leftErr := semver.Parse(versions[left].Version)
		rightVersion, rightErr := semver.Parse(versions[right].Version)
		if leftErr != nil || rightErr != nil {
			// Unreachable: every entry parsed on the way in. Recorded rather
			// than ignored so a future change cannot make it silent.
			failure = fmt.Errorf("catalog: unreadable version while ordering %q and %q",
				versions[left].Version, versions[right].Version)
			return false
		}
		return semver.Compare(leftVersion, rightVersion) > 0
	})
	return failure
}

// latestPerChannel reduces an ordered history to the index's bounded shape: one
// line per channel that has a release, and nothing that grows with history.
func latestPerChannel(versions []Version) []IndexChannel {
	latest := map[string]string{}
	for _, version := range versions {
		if _, found := latest[version.Channel]; !found {
			latest[version.Channel] = version.Version
		}
	}
	channels := make([]IndexChannel, 0, len(latest))
	for _, channel := range sortedKeys(latest) {
		channels = append(channels, IndexChannel{Channel: channel, Version: latest[channel]})
	}
	return channels
}

func sortedKeys[Value any](values map[string]Value) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Files renders the catalog as the documents to publish, ordered by path.
func (c Catalog) Files() ([]File, error) {
	index, err := render(c.Index)
	if err != nil {
		return nil, err
	}
	files := []File{{Path: IndexPath, Content: index}}
	for _, namespace := range c.Namespaces {
		content, err := render(namespace)
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: NamespacePath(namespace.Namespace), Content: content})
	}
	return files, nil
}

// render serializes one document. Indentation and the trailing newline are
// fixed so that two generations over the same tags produce identical bytes and
// a published file diffs readably. HTML escaping is off because these documents
// are read by a shell and by people, and a shell range would otherwise be
// published as an unreadable escape sequence.
func render(document any) ([]byte, error) {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(document); err != nil {
		return nil, fmt.Errorf("catalog: rendering the catalog failed: %w", err)
	}
	return encoded.Bytes(), nil
}
