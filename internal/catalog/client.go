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

package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/wso2/wso2-cli/sdk/problem"
)

// DefaultOrigin is where the catalog is published: the same origin that already
// serves the install scripts.
const DefaultOrigin = "https://wso2.github.io/wso2-cli"

// OriginEnvVar overrides the catalog origin. It exists so the acceptance suite
// can drive the shell against a local origin serving a generated catalog; no
// test may ever reach the real one.
const OriginEnvVar = "WSO2_CLI_CATALOG_ORIGIN"

// The read limits. A catalog document is small by construction, and an archive
// is bounded so a hostile or broken origin cannot make the shell read forever.
const (
	maxDocumentBytes = 8 << 20
	maxArtifactBytes = 512 << 20
)

// Origin reports the catalog origin this invocation reads, with no trailing
// slash so a published path always joins onto it the same way.
func Origin() string {
	origin := os.Getenv(OriginEnvVar)
	if origin == "" {
		origin = DefaultOrigin
	}
	return strings.TrimRight(origin, "/")
}

// Client reads the published catalog. It is the only part of the shell that
// makes a catalog request, so what an operation costs in requests is decided by
// how many times a caller comes here.
type Client struct {
	// Origin is the catalog origin, without a trailing slash.
	Origin string
	// HTTP is the transport. A nil value is the default client.
	HTTP *http.Client
}

// Index reads the bounded index every update check reads.
//
// A failure to reach the origin is reported as an origin failure and never as
// an absent module: an outage and a mistake are different problems with
// different answers, and collapsing them leaves a user unable to tell which
// they have.
func (c Client) Index(ctx context.Context) (Index, error) {
	body, err := c.get(ctx, c.Origin+"/"+IndexPath, maxDocumentBytes)
	if err != nil {
		return Index{}, err
	}
	var index Index
	if err := json.Unmarshal(body, &index); err != nil {
		return Index{}, unreadable(IndexPath, err)
	}
	if index.SchemaVersion != SchemaVersion {
		return Index{}, schemaUnsupported(IndexPath, index.SchemaVersion)
	}
	return index, nil
}

// Module reports the index entry for one namespace, or the refusal that names
// it as unpublished.
func (i Index) Module(namespace string) (IndexModule, error) {
	for _, entry := range i.Modules {
		if entry.Namespace == namespace {
			return entry, nil
		}
	}
	return IndexModule{}, problem.New(problem.CategoryUsage, "catalog.unknown_module",
		fmt.Sprintf("no module named %q is published in the module catalog", namespace)).
		WithRecovery("Check the module name. This is not a network failure: the catalog was read and names no such module.")
}

// Namespace reads one namespace's full version history, at the path the index
// publishes for it.
func (c Client) Namespace(ctx context.Context, entry IndexModule) (NamespaceFile, error) {
	if err := validPublishedPath(entry.Path); err != nil {
		return NamespaceFile{}, err
	}
	body, err := c.get(ctx, c.Origin+"/"+entry.Path, maxDocumentBytes)
	if err != nil {
		return NamespaceFile{}, err
	}
	var file NamespaceFile
	if err := json.Unmarshal(body, &file); err != nil {
		return NamespaceFile{}, unreadable(entry.Path, err)
	}
	if file.SchemaVersion != SchemaVersion {
		return NamespaceFile{}, schemaUnsupported(entry.Path, file.SchemaVersion)
	}
	if file.Namespace != entry.Namespace {
		return NamespaceFile{}, problem.New(problem.CategoryModuleTrust, "catalog.namespace_mismatch",
			fmt.Sprintf("the version history at %s declares the namespace %q, and the index names it %q",
				entry.Path, file.Namespace, entry.Namespace)).
			WithRecovery("Report this to the module catalog's maintainers; the published catalog disagrees with itself.")
	}
	return file, nil
}

// Download reads one published artifact. Its digest is checked by the caller
// against the entry that named it, which proves the archive matches the entry
// and not that the entry is authentic.
func (c Client) Download(ctx context.Context, artifactURL string) ([]byte, error) {
	if err := validArtifactURL(artifactURL); err != nil {
		return nil, err
	}
	return c.get(ctx, artifactURL, maxArtifactBytes)
}

// get reads one URL, mapping every way a read can fail onto the one problem
// that says the origin could not be read.
func (c Client) get(ctx context.Context, target string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, originUnreachable(target, err.Error())
	}
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, originUnreachable(target, err.Error())
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode != http.StatusOK {
		return nil, originUnreachable(target, fmt.Sprintf("the origin answered with status %d", response.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, originUnreachable(target, err.Error())
	}
	if int64(len(body)) > limit {
		return nil, originUnreachable(target, fmt.Sprintf("the origin answered with more than %d bytes", limit))
	}
	return body, nil
}

// validPublishedPath proves a path the index named is a path on this origin and
// not somewhere else: a published document may only redirect the shell within
// the origin it was itself read from.
func validPublishedPath(published string) error {
	if published == "" || strings.HasPrefix(published, "/") ||
		strings.Contains(published, "://") || strings.Contains(published, "\\") {
		return malformedPath(published)
	}
	for _, element := range strings.Split(published, "/") {
		if element == "" || element == "." || element == ".." {
			return malformedPath(published)
		}
	}
	return nil
}

func malformedPath(published string) error {
	return problem.New(problem.CategoryModuleTrust, "catalog.malformed_path",
		fmt.Sprintf("the module catalog names a version history at %q, which is not a path on the catalog origin", published)).
		WithRecovery("Report this to the module catalog's maintainers.")
}

// validArtifactURL proves an artifact is fetched over HTTP, so a published
// entry cannot make the shell read a local file or an unknown scheme.
func validArtifactURL(artifactURL string) error {
	parsed, err := url.Parse(artifactURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return problem.New(problem.CategoryModuleTrust, "catalog.malformed_artifact_url",
			fmt.Sprintf("the module catalog names an artifact at %q, which is not an HTTP URL", artifactURL)).
			WithRecovery("Report this to the module catalog's maintainers.")
	}
	return nil
}

func originUnreachable(target, detail string) problem.Problem {
	return problem.New(problem.CategoryModuleProcess, "catalog.origin_unreachable",
		fmt.Sprintf("the module catalog at %s could not be read: %s", target, detail)).
		WithRecovery("Check network access to the catalog origin and try again. The module may well exist; this run could not ask.")
}

func unreadable(published string, err error) problem.Problem {
	return problem.New(problem.CategoryModuleTrust, "catalog.unreadable",
		fmt.Sprintf("the module catalog at %s is not a readable catalog document: %v", published, err)).
		WithRecovery("Report this to the module catalog's maintainers.")
}

func schemaUnsupported(published string, schemaVersion int) problem.Problem {
	return problem.New(problem.CategoryModuleTrust, "catalog.schema_unsupported",
		fmt.Sprintf("the module catalog at %s uses schema version %d, and this shell reads version %d",
			published, schemaVersion, SchemaVersion)).
		WithRecovery("Update the WSO2 CLI so it reads the published catalog schema.")
}
