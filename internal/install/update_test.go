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

import "testing"

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
