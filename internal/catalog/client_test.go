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
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/sdk/problem"
)

// zeroReader produces an endless stream of zero bytes without materializing a
// buffer the size of what it hands out, so a test can serve a body hundreds of
// megabytes long without holding that much in an intermediate slice.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// TestDownloadRefusesAnArchiveOverTheByteLimit pins the exact bound Download
// enforces: maxArtifactBytes, the same 512 MiB constant that guards a hostile
// or broken origin from making the shell read forever. It serves
// maxArtifactBytes+1 bytes — the smallest body that must be refused — rather
// than an arbitrarily larger one, so the test also pins that the boundary
// itself, and not merely "very large", is what triggers the refusal.
//
// Mutation-proof: with the "+1" on the limit reader removed (so get read
// exactly `limit` bytes and could never observe an overflow) or with the
// length check changed to ">=", this test's exact-boundary body would flip
// which way it fails, either wrongly succeeding or wrongly refusing a body
// exactly at the limit (see TestDownloadAcceptsAnArchiveExactlyAtTheByteLimit
// below, which pins the other side of the same boundary).
func TestDownloadRefusesAnArchiveOverTheByteLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := io.CopyN(w, zeroReader{}, maxArtifactBytes+1); err != nil {
			t.Logf("serving the oversized body: %v", err)
		}
	}))
	defer server.Close()

	client := Client{Origin: server.URL, HTTP: server.Client()}
	_, err := client.Download(context.Background(), server.URL+"/archive.tar.gz", nil)
	if err == nil {
		t.Fatal("Download succeeded for a body one byte over maxArtifactBytes, want a refusal")
	}
	var typed problem.Problem
	if !errors.As(err, &typed) || typed.Code != "catalog.origin_unreachable" {
		t.Fatalf("err = %v, want a catalog.origin_unreachable problem", err)
	}
}

// TestDownloadAcceptsAnArchiveExactlyAtTheByteLimit pins the other side of the
// same boundary: a body of exactly maxArtifactBytes bytes is not refused.
// Without this, a mutant that shrank the limit reader's allowance (so it read
// only `limit-1` bytes and always reported an "overflow" for anything at the
// true limit) would still pass the refusal test above.
func TestDownloadAcceptsAnArchiveExactlyAtTheByteLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := io.CopyN(w, zeroReader{}, maxArtifactBytes); err != nil {
			t.Logf("serving the body: %v", err)
		}
	}))
	defer server.Close()

	client := Client{Origin: server.URL, HTTP: server.Client()}
	body, err := client.Download(context.Background(), server.URL+"/archive.tar.gz", nil)
	if err != nil {
		t.Fatalf("Download refused a body exactly at maxArtifactBytes: %v", err)
	}
	if int64(len(body)) != maxArtifactBytes {
		t.Fatalf("got %d bytes, want exactly %d", len(body), maxArtifactBytes)
	}
}

// TestDownloadReportsProgressAsBytesArrive proves progress is wired at
// Download, per R4: the report function is called with a strictly increasing
// cumulative count that ends at the body's full length. Index and Namespace
// deliberately do not thread a report function through at all (they pass nil
// to get), which this test does not exercise — there is nothing to wire
// progress into for two small JSON documents.
func TestDownloadReportsProgressAsBytesArrive(t *testing.T) {
	const size = 256 * 1024
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := io.CopyN(w, zeroReader{}, size); err != nil {
			t.Logf("serving the body: %v", err)
		}
	}))
	defer server.Close()

	var reports []int64
	client := Client{Origin: server.URL, HTTP: server.Client()}
	body, err := client.Download(context.Background(), server.URL+"/archive.tar.gz", func(read int64) {
		reports = append(reports, read)
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if len(reports) == 0 {
		t.Fatal("Download never called the report function")
	}
	last := int64(0)
	for _, read := range reports {
		if read < last {
			t.Fatalf("report went backwards: %d after %d", read, last)
		}
		last = read
	}
	if last != int64(len(body)) {
		t.Fatalf("final report = %d, want %d (the full body length)", last, len(body))
	}
}

// TestGetsSharedByIndexAndNamespaceIgnoreANilReport proves the small-document
// path (get called with a nil ProgressFunc, exactly what Index and Namespace
// do) tolerates a nil report rather than panicking on the first byte read —
// which is what a countingReader unconditionally wrapping the body regardless
// of whether report is nil would do.
func TestGetsSharedByIndexAndNamespaceIgnoreANilReport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"schemaVersion":1}`))
	}))
	defer server.Close()

	client := Client{Origin: server.URL, HTTP: server.Client()}
	body, err := client.get(context.Background(), server.URL+"/doc.json", maxDocumentBytes, nil)
	if err != nil {
		t.Fatalf("get with a nil report panicked or failed: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("get returned no body")
	}
}

// capturedLog records what a client logs, standing in for --verbose's real
// *output.Logger so a test can prove the raw transport detail still exists
// somewhere after it was removed from the user-facing message.
type capturedLog struct {
	attributes []any
}

func (l *capturedLog) Debug(message string, attributes ...any) {
	l.attributes = append(l.attributes, attributes...)
}

// unreachableTestOrigin reports an origin nothing is listening on: the port
// was bound and released, so a request to it is a refused connection rather
// than a name that might resolve to somebody else's server.
func unreachableTestOrigin(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a port returned %v", err)
	}
	origin := "http://" + listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("releasing the port returned %v", err)
	}
	return origin
}

// TestUnreachableOriginMessageCarriesNoTransportInternals is fix round 2's
// F3(c): the refusal a user reads must say the origin could not be read in a
// short cause of its own, not by embedding the raw Go error chain — no
// `Get "..."`, no `dial tcp`. The raw error is not lost: it goes to the
// client's diagnostic log, which --verbose turns on.
func TestUnreachableOriginMessageCarriesNoTransportInternals(t *testing.T) {
	log := &capturedLog{}
	client := Client{Origin: unreachableTestOrigin(t), Log: log}

	_, err := client.Index(context.Background())
	var typed problem.Problem
	if !errors.As(err, &typed) || typed.Code != "catalog.origin_unreachable" {
		t.Fatalf("err = %v, want a catalog.origin_unreachable problem", err)
	}
	for _, internals := range []string{`Get "`, "dial tcp"} {
		if strings.Contains(typed.Message, internals) {
			t.Errorf("the message leaks transport internals (%s):\n%s", internals, typed.Message)
		}
	}
	if !strings.Contains(typed.Message, "the connection was refused") {
		t.Errorf("the message does not carry the short cause:\n%s", typed.Message)
	}
	var raw string
	for _, attribute := range log.attributes {
		if s, ok := attribute.(string); ok && strings.Contains(s, "connection refused") {
			raw = s
		}
	}
	if raw == "" {
		t.Errorf("the raw transport error was not logged for --verbose: %v", log.attributes)
	}
}

// TestUnreachableOriginRecoveryFollowsWhereTheOriginCameFrom is fix round 2's
// F3(b): a failure to reach an origin the "catalog-origin" preference chose is
// a configuration to revisit, and the recovery must name wso2 config rather
// than blame the network; an origin nobody configured keeps the
// network-focused recovery.
func TestUnreachableOriginRecoveryFollowsWhereTheOriginCameFrom(t *testing.T) {
	origin := unreachableTestOrigin(t)

	t.Run("default origin blames the network", func(t *testing.T) {
		client := Client{Origin: origin}
		_, err := client.Index(context.Background())
		var typed problem.Problem
		if !errors.As(err, &typed) {
			t.Fatalf("err = %v, want a problem", err)
		}
		if !strings.Contains(typed.Recovery, "Check network access") {
			t.Errorf("recovery = %q, want the network-focused guidance", typed.Recovery)
		}
		if strings.Contains(typed.Recovery, "wso2 config") {
			t.Errorf("recovery = %q, names wso2 config for an origin no preference chose", typed.Recovery)
		}
	})

	t.Run("configured origin names the preference and the fix", func(t *testing.T) {
		client := Client{Origin: origin, OriginConfigured: true}
		_, err := client.Index(context.Background())
		var typed problem.Problem
		if !errors.As(err, &typed) {
			t.Fatalf("err = %v, want a problem", err)
		}
		for _, want := range []string{`"catalog-origin" preference`,
			"wso2 config unset catalog-origin", "wso2 config set catalog-origin"} {
			if !strings.Contains(typed.Recovery, want) {
				t.Errorf("recovery = %q, want it to contain %q", typed.Recovery, want)
			}
		}
	})
}

// TestShortCauseNamesADNSFailure pins the one cause the DNS branch exists
// for — the F3 report's "no such host" — without a test ever asking a real
// resolver anything.
func TestShortCauseNamesADNSFailure(t *testing.T) {
	notFound := &net.DNSError{Name: "catalog.invalid.example", IsNotFound: true}
	if got, want := shortCause(notFound), `no such host "catalog.invalid.example"`; got != want {
		t.Errorf("shortCause(not found) = %q, want %q", got, want)
	}
	failed := &net.DNSError{Name: "catalog.invalid.example"}
	if got, want := shortCause(failed), `the DNS lookup for "catalog.invalid.example" failed`; got != want {
		t.Errorf("shortCause(lookup failure) = %q, want %q", got, want)
	}
}
