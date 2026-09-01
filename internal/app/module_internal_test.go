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

package app

import (
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/install"
)

// TestTheThreeUpdateRenderingsAgreeOnAnUnpublishedModule pins that a dry run
// predicts what the real run reports, and that the table's update column
// agrees with both. #134 added dryRunUpdateLine mirroring updateOne's branches
// deliberately, which is why #135 could not be fixed in only one of them:
// correcting the dry run alone would make --dry-run contradict the command it
// predicts. #135.
func TestTheThreeUpdateRenderingsAgreeOnAnUnpublishedModule(t *testing.T) {
	status := install.Status{Namespace: "reference", Installed: "1.2.3", Channel: "stable"}
	outcome := install.Outcome{
		Namespace: "reference",
		Action:    install.ActionNotPublished,
		From:      "1.2.3",
		To:        "1.2.3",
		Channel:   "stable",
	}

	dryRun := dryRunUpdateLine(status)
	real, err := updateLine(outcome)
	if err != nil {
		t.Fatalf("updateLine returned %v, want an unpublished module not to be a refusal", err)
	}

	for name, line := range map[string]string{"dry run": dryRun, "real run": real} {
		if strings.Contains(line, "current") {
			t.Errorf("the %s calls an unpublished module current: %q", name, line)
		}
		if !strings.Contains(line, "stable") {
			t.Errorf("the %s does not name the channel that publishes nothing: %q", name, line)
		}
		if !strings.Contains(line, "wso2 module available") {
			t.Errorf("the %s does not name a way to find out what is published: %q", name, line)
		}
	}

	if column := updateColumn(status); column != "not published" {
		t.Errorf("the update column reports %q for an unpublished module, want %q", column, "not published")
	}
}

// TestChannelColumnSaysNothingAboutAPinnedModulesChannel pins that the table
// does not name a channel for a module held at an exact version with no
// channel recorded. Policy.FollowedChannel resolves an unrecorded channel to
// stable so the catalog query has something to ask for; that resolution is not
// a fact about the installed version, and printing it named the one channel a
// pinned prerelease provably is not on. #128.
func TestChannelColumnSaysNothingAboutAPinnedModulesChannel(t *testing.T) {
	pinned := install.Status{
		Namespace:     "reference",
		Installed:     "0.1.0-rc.2",
		Channel:       "stable", // what FollowedChannel resolved for the query
		PolicyChannel: "",       // what the policy actually records
		Pinned:        true,
		PinnedVersion: "0.1.0-rc.2",
	}

	if column := channelColumn(pinned); column != "—" {
		t.Errorf("channelColumn = %q, want an em dash: a pinned module with no "+
			"recorded channel follows no channel at all", column)
	}
}

// TestChannelColumnNamesARecordedChannel pins the two cases that must keep
// printing a channel: an unpinned module following one, and a module pinned
// after a channel was explicitly chosen. #128.
func TestChannelColumnNamesARecordedChannel(t *testing.T) {
	following := install.Status{Channel: "prerelease", PolicyChannel: "prerelease"}
	if column := channelColumn(following); column != "prerelease" {
		t.Errorf("channelColumn = %q, want prerelease for a module following one", column)
	}

	pinnedOnAChannel := install.Status{
		Channel: "prerelease", PolicyChannel: "prerelease",
		Pinned: true, PinnedVersion: "0.1.0-rc.2",
	}
	if column := channelColumn(pinnedOnAChannel); column != "prerelease" {
		t.Errorf("channelColumn = %q, want the channel the user chose", column)
	}

	unpinnedDefault := install.Status{Channel: "stable", PolicyChannel: ""}
	if column := channelColumn(unpinnedDefault); column != "stable" {
		t.Errorf("channelColumn = %q, want stable: no channel chosen means stable "+
			"for a module that is free to move", column)
	}
}
