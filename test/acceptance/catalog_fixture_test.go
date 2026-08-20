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

// The catalog fixture: a set of module tags, the archives those tags published,
// and an origin serving what the real generator produced from them.
//
// What is served is the generator's own output, never hand-written JSON. A
// hand-written fixture would let the generator and the shell's reader drift
// apart while both their tests stayed green, which is the obvious failure mode
// when a producer and a consumer are tested separately.
//
// The archives are real: each carries the reference module built by this
// repository, so the size and digest a catalog entry publishes describe bytes a
// test can actually download and check rather than numbers invented to look
// plausible.
package acceptance_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/modules"
)

// The fixture tag set. The versions are deliberately far ahead of the shell's
// own 0.4.2, and the prerelease is newer than every stable release, so a
// generator that compared a module version against the shell's, or that let a
// prerelease stand in for the stable channel, would be caught.
const (
	catalogNamespace   = "reference"
	catalogOlderStable = "reference/v4.4.0"
	catalogStable      = "reference/v4.5.0"
	catalogPrerelease  = "reference/v4.6.0-rc.1"
	// catalogAddedStable extends release history without adding a namespace or
	// a channel, which is what the index's size must be insensitive to.
	catalogAddedStable = "reference/v4.7.0"
	// catalogAncientStable is far below the shell's own version rather than far
	// above it, so the two directions of a version comparison the shell must
	// never make are both represented.
	catalogAncientStable = "reference/v0.1.0"
)

// A second namespace, on its own version scheme. Several criteria are about one
// module's policy being independent of another's — a channel chosen per module,
// a pin that survives an update run that moves everything else — and none of
// them can be stated with one namespace in the store.
//
// It is a fixture namespace rather than a product module: only the reference
// module exists, and migrating apictl, amctl, and mi is separate work.
const (
	catalogOtherNamespace  = "sample"
	catalogOtherStable     = "sample/v1.0.0"
	catalogOtherNewer      = "sample/v1.1.0"
	catalogOtherPrerelease = "sample/v1.2.0-rc.1"
)

// catalogPlatforms are the platforms the fixture releases publish for. They are
// fixed rather than taken from the running machine: nothing here is executed,
// and a fixed set keeps the generated files identical on every runner.
var catalogPlatforms = []modules.Platform{
	{OS: "darwin", Arch: "arm64"},
	{OS: "linux", Arch: "amd64"},
}

// catalogOptions vary what a fixture origin publishes. The zero value is the
// fixed set of platforms and one protocol version for every tag, which is what
// the generator's own tests want; an install test varies them to reproduce a
// platform that publishes nothing and a release that outruns the shell.
type catalogOptions struct {
	// platforms are the platforms every tag publishes for.
	platforms []modules.Platform
	// protocols overrides, by tag, the protocol versions that release declares.
	protocols map[string][]int
	// carriesNoModule marks tags whose archive is a well-formed tarball that
	// carries no module executable. Its digest is the digest the catalog
	// publishes, so it reproduces the failure that happens after the download
	// has been accepted and staged.
	carriesNoModule map[string]bool
}

// hostPlatform is the platform the running test is on. An install test has to
// publish for it, because what it installs is launched.
var hostPlatform = modules.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}

// catalogHarness is one isolated catalog: a generated catalog and an origin
// serving it beside the archives its entries point at.
type catalogHarness struct {
	t       *testing.T
	options catalogOptions
	server  *httptest.Server
	catalog catalog.Catalog
	// files is what the generator produced, by published path.
	files map[string][]byte
	// archives is the archive bytes published for each tag and platform, keyed
	// by the path they are served from.
	archives map[string][]byte

	// requests counts what the origin was asked for, by path. It is how the
	// request-economy claims are proven: what a shell run costs is observable
	// here and nowhere inside the shell.
	requestMutex sync.Mutex
	requests     map[string]int
}

// newCatalogHarness generates a catalog over the given tags and serves it.
func newCatalogHarness(t *testing.T, tags ...string) *catalogHarness {
	t.Helper()
	return newCatalogOrigin(t, catalogOptions{}, tags...)
}

// newCatalogOrigin generates a catalog over the given tags, with what the
// releases publish varied, and serves it.
func newCatalogOrigin(t *testing.T, options catalogOptions, tags ...string) *catalogHarness {
	t.Helper()
	if len(options.platforms) == 0 {
		options.platforms = catalogPlatforms
	}
	harness := &catalogHarness{
		t:        t,
		options:  options,
		archives: map[string][]byte{},
		requests: map[string]int{},
	}
	harness.server = httptest.NewServer(http.HandlerFunc(harness.serve))
	t.Cleanup(harness.server.Close)

	harness.files = harness.generate(tags...)
	return harness
}

// generate runs the real generator over a tag set and reports what it produced,
// by published path. The origin serves whatever the last call produced, so a
// test can extend release history or reorder a tag set without standing up a
// second origin — which would change every artifact URL and make two
// generations incomparable for reasons that have nothing to do with the
// generator.
func (c *catalogHarness) generate(tags ...string) map[string][]byte {
	c.t.Helper()
	generated, err := catalog.Generate(c.input(tags))
	if err != nil {
		c.t.Fatalf("generating the catalog over %v returned %v", tags, err)
	}
	c.catalog = generated
	c.files = renderCatalog(c.t, generated)
	return c.files
}

// input builds what the release job would hand the generator: the tags that
// exist, the modules this repository can build, and what each tag published.
//
// The declarations come from the real discovery over this checkout, so a tag
// naming a namespace no module in the repository declares is refused here for
// the same reason it would be refused in a release.
func (c *catalogHarness) input(tags []string) catalog.Input {
	c.t.Helper()
	declarations, err := catalog.Discover(repoRoot(c.t))
	if err != nil {
		c.t.Fatalf("discovering the buildable modules returned %v", err)
	}

	// What the module itself declares is what a release publishes, so the
	// capabilities in a fixture entry are the reference module's own rather
	// than values invented here. A module installed from a catalog that
	// published none would be denied every brokered request it makes.
	capabilities := map[string]modules.Capabilities{}
	compatibility := map[string]modules.Compatibility{}
	for _, declaration := range declarations {
		capabilities[declaration.Namespace] = declaration.Capabilities
		compatibility[declaration.Namespace] = declaration.Compatibility
	}
	// The second namespace has no module directory, because no second module
	// exists to give it one. Its declaration is the reference module's, under
	// another namespace, which is all the generator needs to accept its tags.
	declarations = append(declarations, catalog.Declaration{
		SchemaVersion: catalog.SchemaVersion,
		Namespace:     catalogOtherNamespace,
		Compatibility: compatibility[catalogNamespace],
		Capabilities:  capabilities[catalogNamespace],
	})
	capabilities[catalogOtherNamespace] = capabilities[catalogNamespace]

	published := map[string]catalog.Release{}
	for _, tag := range tags {
		namespace, _, err := catalog.ParseTag(tag)
		if err != nil {
			c.t.Fatalf("the fixture tag %q is malformed: %v", tag, err)
		}
		artifacts := make([]catalog.Artifact, 0, len(c.options.platforms))
		for _, platform := range c.options.platforms {
			body := c.archiveBytes(tag, platform)
			archivePath := c.publish(tag, platform, body)
			digest := sha256.Sum256(body)
			artifacts = append(artifacts, catalog.Artifact{
				Platform: platform,
				URL:      c.server.URL + "/" + archivePath,
				Size:     int64(len(body)),
				SHA256:   fmt.Sprintf("%x", digest),
			})
		}
		protocols, overridden := c.options.protocols[tag]
		if !overridden {
			protocols = []int{testProtocolVersionNumber}
		}
		published[tag] = catalog.Release{
			Compatibility: modules.Compatibility{
				Shell:            ">=0.1.0 <2.0.0",
				ProtocolVersions: protocols,
			},
			Capabilities: capabilities[namespace],
			Artifacts:    artifacts,
		}
	}
	return catalog.Input{Tags: tags, Modules: declarations, Published: published}
}

// publish records an archive at the path its catalog entry will name and
// reports that path.
func (c *catalogHarness) publish(tag string, platform modules.Platform, body []byte) string {
	archivePath := c.archivePath(tag, platform)
	c.archives[archivePath] = body
	return archivePath
}

// archivePath reports where one tag's archive for one platform is served from.
func (c *catalogHarness) archivePath(tag string, platform modules.Platform) string {
	c.t.Helper()
	namespace, version, err := catalog.ParseTag(tag)
	if err != nil {
		c.t.Fatalf("the fixture tag %q is malformed: %v", tag, err)
	}
	return fmt.Sprintf("download/%s/v%s/wso2-module-%s-v%s-%s-%s.tar.gz",
		namespace, version, namespace, version, platform.OS, platform.Arch)
}

// platformExecutableSuffix reports the executable suffix of a published
// platform, which is not necessarily the running one's.
func platformExecutableSuffix(platform modules.Platform) string {
	if platform.OS == "windows" {
		return ".exe"
	}
	return ""
}

// archiveBytes reports the archive one tag publishes for one platform: the
// reference module this repository builds, under its published name.
//
// One archive per tag and platform is built for the whole package. Compressing
// a real executable is the expensive part of this fixture, and a test that
// regenerates over several tag sets would otherwise pay for it again every
// time for bytes that cannot differ.
func (c *catalogHarness) archiveBytes(tag string, platform modules.Platform) []byte {
	c.t.Helper()
	key := fmt.Sprintf("%s %s %t", tag, platform, c.options.carriesNoModule[tag])
	catalogArchiveMutex.Lock()
	defer catalogArchiveMutex.Unlock()
	if cached, found := catalogArchiveCache[key]; found {
		return cached
	}
	built := c.buildArchive(tag, platform)
	catalogArchiveCache[key] = built
	return built
}

// buildArchive compresses one archive. It is deterministic in its inputs, which
// is what makes caching its result sound.
func (c *catalogHarness) buildArchive(tag string, platform modules.Platform) []byte {
	c.t.Helper()
	namespace, version, err := catalog.ParseTag(tag)
	if err != nil {
		c.t.Fatalf("the fixture tag %q is malformed: %v", tag, err)
	}
	// The module inside the archive is built at the version its tag publishes.
	// A module reports its own version at the handshake and the shell checks it
	// against the receipt, so an archive carrying some other version would be
	// installable and unlaunchable.
	executable := referenceModuleBytes(c.t, version)

	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	// The tag and platform are inside the archive as well as in its name, so
	// two fixture releases never share a digest by accident.
	files := []struct {
		name string
		body []byte
	}{
		{"wso2-module-" + namespace + platformExecutableSuffix(platform), executable},
		{"RELEASE", []byte(tag + " " + platform.String() + "\n")},
	}
	if c.options.carriesNoModule[tag] {
		files = files[1:]
	}
	for _, file := range files {
		if err := tarWriter.WriteHeader(&tar.Header{
			Name: file.name,
			Mode: 0o755,
			Size: int64(len(file.body)),
		}); err != nil {
			c.t.Fatalf("writing the %s header returned %v", file.name, err)
		}
		if _, err := tarWriter.Write(file.body); err != nil {
			c.t.Fatalf("writing %s into the tarball returned %v", file.name, err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		c.t.Fatalf("closing the tarball returned %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		c.t.Fatalf("closing the gzip stream returned %v", err)
	}
	return buffer.Bytes()
}

// serve answers the two catalog paths and the archive paths their entries name.
func (c *catalogHarness) serve(w http.ResponseWriter, r *http.Request) {
	requested := strings.TrimPrefix(r.URL.Path, "/")
	c.record(requested)
	if body, found := c.files[requested]; found {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(body); err != nil {
			c.t.Errorf("writing %s returned %v", requested, err)
		}
		return
	}
	if body, found := c.archives[requested]; found {
		if _, err := w.Write(body); err != nil {
			c.t.Errorf("writing %s returned %v", requested, err)
		}
		return
	}
	http.NotFound(w, r)
}

// record counts one request the origin answered.
func (c *catalogHarness) record(requested string) {
	c.requestMutex.Lock()
	defer c.requestMutex.Unlock()
	c.requests[requested]++
}

// requestCount reports how many times one path was asked for.
func (c *catalogHarness) requestCount(requested string) int {
	c.requestMutex.Lock()
	defer c.requestMutex.Unlock()
	return c.requests[requested]
}

// totalRequests reports how many requests the origin answered in all.
func (c *catalogHarness) totalRequests() int {
	c.requestMutex.Lock()
	defer c.requestMutex.Unlock()
	total := 0
	for _, count := range c.requests {
		total += count
	}
	return total
}

// forget resets the request log, so a later run is counted on its own.
func (c *catalogHarness) forget() {
	c.requestMutex.Lock()
	defer c.requestMutex.Unlock()
	c.requests = map[string]int{}
}

// corruptArchive replaces one published archive with bytes that do not match
// the digest its catalog entry records, which is a substituted or damaged
// download as the shell would meet one.
func (c *catalogHarness) corruptArchive(tag string, platform modules.Platform) {
	c.t.Helper()
	path := c.archivePath(tag, platform)
	original, found := c.archives[path]
	if !found {
		c.t.Fatalf("no archive is published at %s", path)
	}
	// The length is preserved so the mismatch the shell reports is the digest
	// and not the size, which would prove a different check.
	corrupted := append([]byte(nil), original...)
	corrupted[len(corrupted)/2] ^= 0xff
	c.archives[path] = corrupted
}

// fetch reads one path from the origin, which is how every assertion here sees
// the catalog: over HTTP, from what was published, rather than from the value
// the generator returned.
func (c *catalogHarness) fetch(t *testing.T, published string) []byte {
	t.Helper()
	response, err := c.server.Client().Get(c.server.URL + "/" + published)
	if err != nil {
		t.Fatalf("fetching %s returned %v", published, err)
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("closing the response body for %s returned %v", published, err)
		}
	}()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("fetching %s returned status %d", published, response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("reading %s returned %v", published, err)
	}
	return body
}

// index reads the published index.
func (c *catalogHarness) index(t *testing.T) catalog.Index {
	t.Helper()
	var index catalog.Index
	if err := json.Unmarshal(c.fetch(t, catalog.IndexPath), &index); err != nil {
		t.Fatalf("the published index is not readable: %v", err)
	}
	return index
}

// namespaceFile reads the published version history for one namespace, by the
// path the index names rather than by a path the test composed.
func (c *catalogHarness) namespaceFile(t *testing.T, published string) catalog.NamespaceFile {
	t.Helper()
	var file catalog.NamespaceFile
	if err := json.Unmarshal(c.fetch(t, published), &file); err != nil {
		t.Fatalf("the published namespace file %s is not readable: %v", published, err)
	}
	return file
}

// renderCatalog is the generator's own rendering, keyed by published path.
func renderCatalog(t *testing.T, generated catalog.Catalog) map[string][]byte {
	t.Helper()
	files, err := generated.Files()
	if err != nil {
		t.Fatalf("rendering the catalog returned %v", err)
	}
	rendered := map[string][]byte{}
	for _, file := range files {
		rendered[path.Clean(file.Path)] = file.Content
	}
	return rendered
}

// The reference module is built once per version for the whole package, and
// each archive is compressed once, because building and compressing a real
// executable is the expensive part of this fixture and every test that publishes
// a given version wants the same bytes.
var (
	catalogModuleMutex sync.Mutex
	catalogModuleCache = map[string][]byte{}

	catalogArchiveMutex sync.Mutex
	catalogArchiveCache = map[string][]byte{}
)

// referenceModuleBytes is the reference module built at one module version.
func referenceModuleBytes(t *testing.T, version string) []byte {
	t.Helper()
	catalogModuleMutex.Lock()
	defer catalogModuleMutex.Unlock()
	if cached, found := catalogModuleCache[version]; found {
		return cached
	}
	built := readFile(t, buildReferenceModuleSpeaking(t, testProtocolVersion, version))
	catalogModuleCache[version] = built
	return built
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s returned %v", path, err)
	}
	return contents
}
