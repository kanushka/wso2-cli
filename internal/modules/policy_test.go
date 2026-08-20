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

package modules_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/modules"
)

// A namespace with no recorded policy follows the stable channel and is not
// pinned, which is what a module installed before any policy was written
// follows too.
func TestAnUnrecordedPolicyFollowsTheStableChannel(t *testing.T) {
	store := modules.NewStore(filepath.Join(t.TempDir(), "modules"))

	policy, err := store.ReadPolicy("reference")
	if err != nil {
		t.Fatalf("ReadPolicy returned %v", err)
	}
	if policy.FollowedChannel() != modules.ChannelStable {
		t.Errorf("channel = %q, want %q", policy.FollowedChannel(), modules.ChannelStable)
	}
	if policy.Pinned() {
		t.Error("a module with no recorded policy reports as pinned")
	}
}

// A policy document round-trips, so what an install records is what an update
// run reads back.
func TestARecordedPolicyRoundTrips(t *testing.T) {
	store := writePolicy(t, modules.Policy{
		SchemaVersion: modules.PolicySchemaVersion,
		Namespace:     "reference",
		Channel:       "prerelease",
		PinnedVersion: "4.4.0",
	})

	policy, err := store.ReadPolicy("reference")
	if err != nil {
		t.Fatalf("ReadPolicy returned %v", err)
	}
	if policy.FollowedChannel() != "prerelease" || policy.PinnedVersion != "4.4.0" {
		t.Errorf("policy = %+v, want the prerelease channel pinned to 4.4.0", policy)
	}
}

// A policy this shell does not own fails closed rather than being partially
// interpreted, exactly as a receipt and an active-version pointer do. Silently
// treating one as the default would move a module the user pinned.
func TestAPolicyThisShellDoesNotOwnFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy modules.Policy
		code   string
	}{
		{
			name:   "unknown schema version",
			policy: modules.Policy{SchemaVersion: modules.PolicySchemaVersion + 1, Namespace: "reference"},
			code:   "modules.policy_schema_unsupported",
		},
		{
			name: "another namespace",
			policy: modules.Policy{
				SchemaVersion: modules.PolicySchemaVersion,
				Namespace:     "elsewhere",
			},
			code: "modules.policy_malformed",
		},
		{
			name: "an unusable pinned version",
			policy: modules.Policy{
				SchemaVersion: modules.PolicySchemaVersion,
				Namespace:     "reference",
				PinnedVersion: "../elsewhere",
			},
			code: "modules.policy_malformed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := writePolicy(t, tc.policy)

			_, err := store.ReadPolicy("reference")
			if err == nil {
				t.Fatalf("ReadPolicy accepted %+v", tc.policy)
			}
			if !strings.Contains(err.Error(), tc.code) {
				t.Errorf("ReadPolicy returned %v, want %s", err, tc.code)
			}
		})
	}
}

// writePolicy stores one policy document under the reference namespace and
// reports the store holding it.
func writePolicy(t *testing.T, policy modules.Policy) modules.Store {
	t.Helper()
	store := modules.NewStore(filepath.Join(t.TempDir(), "modules"))
	if err := os.MkdirAll(store.NamespaceDir("reference"), 0o755); err != nil {
		t.Fatalf("creating the namespace directory returned %v", err)
	}
	document, err := policy.Encode()
	if err != nil {
		t.Fatalf("Encode returned %v", err)
	}
	if err := os.WriteFile(store.PolicyPath("reference"), document, 0o644); err != nil {
		t.Fatalf("writing the policy returned %v", err)
	}
	return store
}
