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
	"os"
	"testing"

	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/modules"
)

// TestUpdateReportsAModuleTheCatalogDoesNotPublish pins that an absent module
// is distinguished from a current one. Status.Available is empty both when the
// catalog has never heard of a module and when it publishes nothing on the
// followed channel, so a withdrawn, renamed, or channel-moved module used to
// fall into the current branch and be reported as up to date — which the
// catalog cannot possibly know, because it does not publish it. #135.
func TestUpdateReportsAModuleTheCatalogDoesNotPublish(t *testing.T) {
	status := Status{
		Namespace: "reference",
		Installed: "1.2.3",
		Channel:   "stable",
		Available: "",
		Update:    false,
	}

	outcome := OutcomeFor(status) // see Step 3 on why this seam exists

	if outcome.Action != ActionNotPublished {
		t.Errorf("Action = %q, want %q", outcome.Action, ActionNotPublished)
	}
	if outcome.To != "1.2.3" {
		t.Errorf("To = %q, want the version that is still active", outcome.To)
	}
	if outcome.Channel != "stable" {
		t.Errorf("Channel = %q, want the channel that publishes nothing, "+
			"which is the fact the refusal has to name", outcome.Channel)
	}
}

// TestUpdateStillReportsAGenuinelyCurrentModule pins the other side: a module
// at the newest version its channel publishes is still current, and this fix
// must not turn every up-to-date module into a warning. #135.
func TestUpdateStillReportsAGenuinelyCurrentModule(t *testing.T) {
	status := Status{
		Namespace: "reference",
		Installed: "1.2.3",
		Channel:   "stable",
		Available: "1.2.3",
		Update:    false,
	}

	outcome := OutcomeFor(status)

	if outcome.Action != ActionCurrent {
		t.Errorf("Action = %q, want %q", outcome.Action, ActionCurrent)
	}
}

// TestStatusesCarriesThePolicysUnresolvedChannelForAPin is the direct pin on
// #128's wiring: statuses joins the published index against ReadPolicy, and it
// is the only place that decides what Status.PolicyChannel holds. The
// renderer-level tests in internal/app assert what channelColumn does with a
// hand-built Status; neither of them calls statuses, so a regression here —
// resolving PolicyChannel the same way Channel is resolved, which would make a
// pinned module's report indistinguishable from an unpinned one following
// stable — would revert #128 while every existing test still passed. #128.
func TestStatusesCarriesThePolicysUnresolvedChannelForAPin(t *testing.T) {
	root := t.TempDir()
	store := modules.NewStore(root)
	if err := os.MkdirAll(store.NamespaceDir("reference"), 0o755); err != nil {
		t.Fatalf("creating the namespace directory returned %v", err)
	}
	// A pin with no channel recorded: what an install at an exact version
	// writes, deliberately, because the pin overrides the channel.
	policy := modules.Policy{
		SchemaVersion: modules.PolicySchemaVersion,
		Namespace:     "reference",
		PinnedVersion: "0.1.0-rc.2",
	}
	document, err := policy.Encode()
	if err != nil {
		t.Fatalf("encoding the policy returned %v", err)
	}
	if err := os.WriteFile(store.PolicyPath("reference"), document, 0o644); err != nil {
		t.Fatalf("writing the policy returned %v", err)
	}

	installer := Installer{Store: store}
	installed := []modules.Installed{{Namespace: "reference", Version: "0.1.0-rc.2"}}

	statuses, err := installer.statuses(catalog.Index{}, installed)
	if err != nil {
		t.Fatalf("statuses returned %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("statuses returned %d entries, want 1", len(statuses))
	}
	status := statuses[0]

	if status.Channel != modules.ChannelStable {
		t.Errorf("Channel = %q, want %q: FollowedChannel still resolves an "+
			"unrecorded channel to stable so latestOnChannel has something to "+
			"ask for", status.Channel, modules.ChannelStable)
	}
	if status.PolicyChannel != "" {
		t.Errorf("PolicyChannel = %q, want empty: the policy recorded no "+
			"channel, and a report must be able to tell that apart from "+
			"Channel's resolution", status.PolicyChannel)
	}
}
