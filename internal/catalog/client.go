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
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"

	"github.com/wso2/wso2-cli/internal/preferences"
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
//
// LimitReader does two separate jobs here, and the tests below pin only one
// of them: TestDownloadRefusesAnArchiveOverTheByteLimit and
// TestDownloadAcceptsAnArchiveExactlyAtTheByteLimit prove the finite-body
// overflow check (a body of maxArtifactBytes+1 is refused, one of exactly
// maxArtifactBytes is not). Neither test, nor any other in this repository,
// proves the memory bound against an origin that never stops streaming: doing
// that honestly means actually serving an unbounded body, and a test that got
// that wrong would hang a CI job for the length of the request's context
// timeout rather than fail fast. That gap is accepted, not hidden — the
// bound below still protects a real, if untested, case.
const (
	maxDocumentBytes = 8 << 20
	maxArtifactBytes = 512 << 20
)

// Origin reports the catalog origin this invocation reads, with no trailing
// slash so a published path always joins onto it the same way.
//
// The precedence is three layers, outermost first: OriginEnvVar, which the
// acceptance suite sets so no test ever reaches the real origin and which a
// saved preference must never be able to override; the "catalog-origin"
// preference (wso2 config set catalog-origin <url>); and DefaultOrigin. A
// preferences document this shell cannot read falls back to no override
// configured, silently as far as this function is concerned — see
// preferences.Load and output.ColorEnabled's doc comment for why that
// diagnostic belongs to the shell, once per invocation, and not here.
func Origin(stateRoot string) string {
	origin, _ := resolveOrigin(stateRoot)
	return origin
}

// OriginConfigured reports whether the origin Origin would return came from
// the "catalog-origin" preference — not the environment variable, and not the
// built-in default. A failure to reach a preference-chosen origin is a
// different problem from a network outage: the fix is wso2 config, not the
// network, and the client uses this fact to say so (see originUnreachable).
func OriginConfigured(stateRoot string) bool {
	_, configured := resolveOrigin(stateRoot)
	return configured
}

// resolveOrigin applies the three-layer precedence Origin documents, and
// reports whether the middle layer — the preference — is the one that won.
func resolveOrigin(stateRoot string) (string, bool) {
	if origin := os.Getenv(OriginEnvVar); origin != "" {
		return strings.TrimRight(origin, "/"), false
	}
	if document, _ := preferences.Load(stateRoot); document.CatalogOrigin != "" {
		return strings.TrimRight(document.CatalogOrigin, "/"), true
	}
	return DefaultOrigin, false
}

// Client reads the published catalog. It is the only part of the shell that
// makes a catalog request, so what an operation costs in requests is decided by
// how many times a caller comes here.
type Client struct {
	// Origin is the catalog origin, without a trailing slash.
	Origin string
	// OriginConfigured records that Origin came from the "catalog-origin"
	// preference (see the package function of the same name). A client that
	// knows this reports a failure to reach the origin as a configuration to
	// revisit rather than a network to check, because that is the fix.
	OriginConfigured bool
	// HTTP is the transport. A nil value is the default client.
	HTTP *http.Client
	// Log is where the raw transport detail of a failed request goes. The
	// user-facing refusal deliberately carries a short cause rather than the
	// raw Go error (fix round 2, F3); the raw error still matters when the
	// short cause is not enough, and --verbose is where it lives. A nil Log
	// drops the detail, which is what --verbose being absent means.
	Log DebugLog
}

// DebugLog is the one method the client needs from a diagnostic log. It is
// satisfied by *output.Logger; declaring it here keeps this package from
// importing internal/output for a single method.
type DebugLog interface {
	Debug(message string, attributes ...any)
}

// Index reads the bounded index every update check reads.
//
// A failure to reach the origin is reported as an origin failure and never as
// an absent module: an outage and a mistake are different problems with
// different answers, and collapsing them leaves a user unable to tell which
// they have.
func (c Client) Index(ctx context.Context) (Index, error) {
	body, err := c.get(ctx, c.Origin+"/"+IndexPath, maxDocumentBytes, nil, c.originUnreachable)
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
	body, err := c.get(ctx, c.Origin+"/"+entry.Path, maxDocumentBytes, nil, c.originUnreachable)
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

// ProgressFunc receives the cumulative number of bytes read so far during a
// Download. It is called synchronously from the goroutine doing the read, as
// often as once per chunk the network hands back; a caller that needs to
// throttle how often that becomes visible does so itself (see
// internal/output.Progress), not by asking here for fewer calls.
type ProgressFunc func(read int64)

// Download reads one published artifact. Its digest is checked by the caller
// against the entry that named it, which proves the archive matches the entry
// and not that the entry is authentic.
//
// report is called as the archive is read, with the cumulative byte count; it
// may be nil, which reports nothing. This is the one catalog request progress
// is wired to (see internal/install.Installer): Index and Namespace fetch two
// small JSON documents that need no progress indicator.
func (c Client) Download(ctx context.Context, artifactURL string, report ProgressFunc) ([]byte, error) {
	if err := validArtifactURL(artifactURL); err != nil {
		return nil, err
	}
	return c.get(ctx, artifactURL, maxArtifactBytes, report, c.artifactUnreachable)
}

// get reads one URL, mapping every way a read can fail onto the one problem
// its caller's kind of URL deserves. An index or namespace document lives at
// the catalog origin, so its refusal may talk about the "catalog-origin"
// preference; an artifact lives wherever the catalog points, which the
// preference does not govern, so its refusal must not (review on #161).
func (c Client) get(ctx context.Context, target string, limit int64, report ProgressFunc, refuse refusal) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, c.unreachable(target, err, refuse)
	}
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, c.unreachable(target, err, refuse)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode != http.StatusOK {
		return nil, refuse(target, fmt.Sprintf("the server answered with status %d", response.StatusCode))
	}
	// The counting reader sits inside the limit reader, not outside it: what
	// is counted and reported is exactly what the limit reader lets through,
	// so wiring progress in can never change how many bytes this function
	// itself reads or when it decides the response overflowed the limit. See
	// the maxArtifactBytes doc comment above for which half of what
	// io.LimitReader does below is actually pinned by a test.
	var reader io.Reader = response.Body
	if report != nil {
		reader = &countingReader{inner: reader, report: report}
	}
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, c.unreachable(target, err, refuse)
	}
	if int64(len(body)) > limit {
		return nil, refuse(target, fmt.Sprintf("the server answered with more than %d bytes", limit))
	}
	return body, nil
}

// countingReader reports cumulative bytes read as they pass through, without
// changing what is read or how errors propagate.
type countingReader struct {
	inner  io.Reader
	report ProgressFunc
	read   int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.inner.Read(p)
	if n > 0 {
		r.read += int64(n)
		r.report(r.read)
	}
	return n, err
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

// unreachable reports a request the transport itself failed, in the user's
// terms rather than the transport's. The raw Go error — the `Get "...": dial
// tcp: ...` chain — never reaches the message (fix round 2, F3): it is logged
// to the client's diagnostic log, where --verbose surfaces it, and the message
// carries the short cause shortCause distils instead.
func (c Client) unreachable(target string, err error, refuse refusal) problem.Problem {
	if c.Log != nil {
		c.Log.Debug("a catalog request failed", "url", target, "error", err.Error())
	}
	return refuse(target, shortCause(err))
}

// shortCause names why a request failed in one short phrase, so the refusal
// reads as a sentence rather than a Go error chain. Only the causes a user can
// act on differently are told apart; everything else is a plain "the network
// request failed", with the raw detail available under --verbose.
func shortCause(err error) string {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsNotFound {
			return fmt.Sprintf("no such host %q", dnsErr.Name)
		}
		return fmt.Sprintf("the DNS lookup for %q failed", dnsErr.Name)
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "the connection was refused"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "the request timed out"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "the request timed out"
	}
	return "the network request failed"
}

// refusal renders one failed read as the typed problem for its caller's kind
// of URL. originUnreachable serves the two catalog documents; a download
// passes artifactUnreachable instead, because the advice the two give differs.
type refusal func(target, detail string) problem.Problem

// artifactUnreachable reports that a module artifact could not be downloaded.
//
// It never mentions the "catalog-origin" preference, whatever OriginConfigured
// says: the catalog answered — that is where the artifact URL came from — and
// the preference does not govern where an artifact lives, so pointing a user
// at wso2 config would send them to a knob that cannot turn this (review on
// #161).
func (c Client) artifactUnreachable(target, detail string) problem.Problem {
	return problem.New(problem.CategoryModuleProcess, "catalog.artifact_unreachable",
		fmt.Sprintf("the module artifact at %s could not be downloaded: %s", target, detail)).
		WithRecovery("Check network access to the artifact's host and try again. " +
			"The catalog itself answered; only this download failed.")
}

// originUnreachable reports that the origin could not be read, with the
// recovery chosen by where the origin came from. When it is the built-in
// default (or the environment variable, which only the test harness sets),
// the network is the thing to check; when the "catalog-origin" preference
// chose it, the preference is the thing to revisit, and blaming the network
// would send the user away from the actual fix (fix round 2, F3).
func (c Client) originUnreachable(target, detail string) problem.Problem {
	recovery := "Check network access to the catalog origin and try again. " +
		"The module may well exist; this run could not ask."
	if c.OriginConfigured {
		recovery = "The catalog origin is set by the \"catalog-origin\" preference; " +
			"run wso2 config unset catalog-origin to restore the default, or " +
			"wso2 config set catalog-origin <url> to change it."
	}
	return problem.New(problem.CategoryModuleProcess, "catalog.origin_unreachable",
		fmt.Sprintf("the module catalog at %s could not be read: %s", target, detail)).
		WithRecovery(recovery)
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
