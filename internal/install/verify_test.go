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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// matchingSelection builds a Selection whose Size and SHA256 describe archive
// exactly, so a test can start from a known-good fixture and corrupt one field
// at a time.
func matchingSelection(archive []byte) catalog.Selection {
	digest := sha256.Sum256(archive)
	return catalog.Selection{
		Version: catalog.Version{Version: "1.0.0"},
		Artifact: catalog.VersionArtifact{
			Size:   int64(len(archive)),
			SHA256: hex.EncodeToString(digest[:]),
		},
	}
}

// TestVerifyAcceptsAMatchingArchive is the positive control the two refusal
// tests below lean on: without it, a bug that made verify refuse everything
// would still leave both refusal tests green.
func TestVerifyAcceptsAMatchingArchive(t *testing.T) {
	archive := []byte("a small but entirely genuine archive")
	if err := verify("demo", matchingSelection(archive), archive); err != nil {
		t.Fatalf("verify rejected a matching archive: %v", err)
	}
}

// TestVerifyRejectsAnArchiveOfTheWrongSize pins the size half of verify,
// which the brief asked for and which the repository had left untested: only
// the digest half was covered, by test/acceptance's
// TestADigestMismatchAbortsLeavingNoExecutableAndNoReceipt. The archive here
// has the RIGHT digest for its own (wrong) length — proving the size check
// fires on its own, independent of the digest check that follows it.
func TestVerifyRejectsAnArchiveOfTheWrongSize(t *testing.T) {
	archive := []byte("a small but entirely genuine archive")
	selection := matchingSelection(archive)
	selection.Artifact.Size++ // the catalog now claims one byte more than this archive has

	err := verify("demo", selection, archive)
	var typed problem.Problem
	if !errors.As(err, &typed) {
		t.Fatalf("verify returned %v, want a typed problem", err)
	}
	if typed.Code != "modules.artifact_size_mismatch" {
		t.Errorf("code = %q, want modules.artifact_size_mismatch", typed.Code)
	}
}

// TestVerifyRejectsAnArchiveWithTheWrongDigest is the digest half, unit-tested
// alongside the size half now that both are exercised at this layer (the
// digest half also has acceptance coverage; the size half, before this
// change, had none anywhere).
func TestVerifyRejectsAnArchiveWithTheWrongDigest(t *testing.T) {
	archive := []byte("a small but entirely genuine archive")
	selection := matchingSelection(archive)
	selection.Artifact.SHA256 = "0000000000000000000000000000000000000000000000000000000000000000"

	err := verify("demo", selection, archive)
	var typed problem.Problem
	if !errors.As(err, &typed) {
		t.Fatalf("verify returned %v, want a typed problem", err)
	}
	if typed.Code != "modules.artifact_digest_mismatch" {
		t.Errorf("code = %q, want modules.artifact_digest_mismatch", typed.Code)
	}
}
