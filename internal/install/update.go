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
	"context"
	"fmt"

	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/semver"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// Status is what one installed module's own policy and the published index say
// about it: the version installed, the channel it follows and the channel its
// policy actually records, whether it is held at an exact version, and the
// newest version published on the followed channel.
type Status struct {
	Namespace string
	Installed string
	Channel   string
	// PolicyChannel is the channel the policy actually records, empty when none
	// was chosen. Channel resolves that to the stable one so there is something
	// to ask the catalog for; this is the unresolved fact, and it is what a
	// report must print. A pinned module with no recorded channel follows no
	// channel at all, and naming one — stable, necessarily — names the single
	// channel a pinned prerelease provably is not on. #128.
	PolicyChannel string
	Pinned        bool
	// PinnedVersion is the version the policy holds the module at, which is
	// what a report shows. It is empty when nothing is pinned.
	PinnedVersion string
	// Available is the newest version the index publishes on the followed
	// channel. It is empty when the catalog publishes no such version, which is
	// what a module the catalog has never heard of looks like.
	Available string
	// Update reports that Available is newer than Installed and the module is
	// free to move to it.
	Update bool
}

// Check reports what an update run would do, in one catalog request.
//
// The request is the index, whose size is bounded by namespaces times channels
// rather than by release history, so extending a module's history does not make
// this cost more. A version history is deliberately not fetched here: selecting
// a specific version is what pays for one, and a check selects nothing.
//
// namespaces narrows the report to those modules, refusing exactly as Update
// would if one of them is not installed (selectInstalled is shared with it).
// Called with none, it reports every installed module — what wso2 module list
// wants, and also what a --dry-run wso2 module update --all wants, since an
// empty namespace list means "every module" for both.
func (i Installer) Check(ctx context.Context, namespaces ...string) ([]Status, error) {
	installed, _, err := i.Store.Inventory()
	if err != nil {
		return nil, err
	}
	if len(namespaces) > 0 {
		installed, err = selectInstalled(installed, namespaces)
		if err != nil {
			return nil, err
		}
	}
	if len(installed) == 0 {
		return nil, nil
	}
	index, err := i.Client.Index(ctx)
	if err != nil {
		return nil, err
	}
	return i.statuses(index, installed)
}

// CheckLocal reports the installed modules from local state alone, with no
// catalog request: what is installed, the channel each module follows, and
// whether it is pinned. Available is empty and Update false for every module,
// because nothing was asked — which is not the same fact as "the catalog
// publishes nothing", and a caller rendering these must say "unknown" rather
// than reuse the unpublished wording.
//
// It exists so wso2 module list can still answer its local half — what is
// installed — when the catalog origin cannot be reached (fix round 2, F4):
// the same question wso2 version already answers offline.
func (i Installer) CheckLocal() ([]Status, error) {
	installed, _, err := i.Store.Inventory()
	if err != nil {
		return nil, err
	}
	if len(installed) == 0 {
		return nil, nil
	}
	// An empty index answers "nothing published on any channel" for every
	// namespace, which is exactly the zero Available/Update shape documented
	// above, computed by the same join Check uses so the two cannot drift.
	return i.statuses(catalog.Index{}, installed)
}

// NothingWouldMove reports, from local state alone, whether an update run
// over namespaces could not possibly move anything: nothing named is
// installed, or every module that is installed is pinned.
//
// This is a narrower answer than "would this update change anything" — an
// unpinned module that happens to already be at the newest published version
// still counts as "might move" here, since knowing otherwise costs the same
// index request Update itself pays. It exists so a caller deciding whether to
// ask permission first can skip that question when the answer to "would
// anything happen" is knowable without a network call: asking permission to
// do nothing trains a person to answer without reading.
func (i Installer) NothingWouldMove(namespaces []string) (bool, error) {
	installed, _, err := i.Store.Inventory()
	if err != nil {
		return false, err
	}
	selected, err := selectInstalled(installed, namespaces)
	if err != nil {
		return false, err
	}
	if len(selected) == 0 {
		return true, nil
	}
	for _, entry := range selected {
		policy, err := i.Store.ReadPolicy(entry.Namespace)
		if err != nil {
			return false, err
		}
		if !policy.Pinned() {
			return false, nil
		}
	}
	return true, nil
}

// statuses joins local inventory and policy against the published index.
func (i Installer) statuses(index catalog.Index, installed []modules.Installed) ([]Status, error) {
	statuses := make([]Status, 0, len(installed))
	for _, entry := range installed {
		policy, err := i.Store.ReadPolicy(entry.Namespace)
		if err != nil {
			return nil, err
		}
		status := Status{
			Namespace:     entry.Namespace,
			Installed:     entry.Version,
			Channel:       policy.FollowedChannel(),
			PolicyChannel: policy.Channel,
			Pinned:        policy.Pinned(),
			PinnedVersion: policy.PinnedVersion,
		}
		status.Available = latestOnChannel(index, entry.Namespace, status.Channel)
		newer, err := isNewer(status.Available, status.Installed)
		if err != nil {
			return nil, err
		}
		// A pinned module is never reported as having an update to take: it is
		// held where the user put it, and reporting it as movable would invite
		// an update run to move it.
		status.Update = newer && !status.Pinned
		statuses = append(statuses, status)
	}
	return statuses, nil
}

// latestOnChannel reports the newest version the index publishes for one
// namespace on one channel, or the empty string when it publishes none.
func latestOnChannel(index catalog.Index, namespace, channel string) string {
	for _, module := range index.Modules {
		if module.Namespace != namespace {
			continue
		}
		for _, published := range module.Channels {
			if published.Channel == channel {
				return published.Version
			}
		}
	}
	return ""
}

// isNewer compares two versions of one module. It never compares a module's
// version against the shell's: what is being asked is whether this module has
// moved on, which is a question about one product's own version scheme.
func isNewer(candidate, installed string) (bool, error) {
	if candidate == "" {
		return false, nil
	}
	published, err := semver.Parse(candidate)
	if err != nil {
		return false, problem.New(problem.CategoryModuleTrust, "catalog.malformed_version",
			fmt.Sprintf("the module catalog publishes an unreadable version %q", candidate)).
			WithRecovery("Report this to the module catalog's maintainers.")
	}
	local, err := semver.Parse(installed)
	if err != nil {
		// Unreachable: a receipt the shell would resolve records a semantic
		// version, and inventory is what produced this one. Reported rather
		// than ignored so a future change cannot make it silent.
		return false, problem.New(problem.CategoryModuleTrust, "modules.receipt_malformed",
			fmt.Sprintf("the installed version %q is not a readable version", installed)).
			WithRecovery("Reinstall the module so the shell can record a readable version.")
	}
	return semver.Compare(published, local) > 0, nil
}

// Action is what an update run did to one module.
type Action string

const (
	// ActionUpdated means a newer version was installed and activated.
	ActionUpdated Action = "updated"
	// ActionCurrent means the module already had the newest version its
	// channel publishes.
	ActionCurrent Action = "current"
	// ActionPinned means the module is held at an exact version and was passed
	// over.
	ActionPinned Action = "pinned"
	// ActionFailed means the update was attempted and refused. The version that
	// was active before it is still active.
	ActionFailed Action = "failed"
	// ActionNotPublished means the catalog publishes no version of the module
	// on the channel it follows, so no statement about whether the installed
	// version is current is available to make. A module that was withdrawn,
	// renamed, or published only on a channel the install no longer follows
	// looks exactly like this, and used to be reported as already current.
	ActionNotPublished Action = "not_published"
)

// Outcome is what happened to one module in an update run.
type Outcome struct {
	Namespace string
	Action    Action
	// From is the version that was active before the run, and To the version
	// active after it. They are equal for every action but ActionUpdated.
	From string
	To   string
	// Err is why an attempted update was refused.
	Err error
	// Channel is the channel the decision was made against. It is carried here
	// rather than re-derived from policy at render time, because updateLine
	// must name the channel that publishes nothing and a second derivation is
	// a second chance to name a different one.
	Channel string
}

// Update brings installed modules to the newest version their own channel
// publishes, within the policy each module carries.
//
// A pinned module is passed over rather than moved, so updating everything else
// cannot silently take a module off the version it is held at. A module whose
// update is refused keeps the version that was active before the run: nothing
// is deactivated until a replacement has been downloaded, verified, and
// unpacked, so a run that fails partway can only fail to add.
//
// One index request serves the whole run, however many modules it moves. A
// module actually being updated then pays for its own version history and
// archive, because that is what selecting a specific version costs.
func (i Installer) Update(ctx context.Context, namespaces []string) ([]Outcome, error) {
	installed, _, err := i.Store.Inventory()
	if err != nil {
		return nil, err
	}
	selected, err := selectInstalled(installed, namespaces)
	if err != nil {
		return nil, err
	}
	if len(selected) == 0 {
		return nil, nil
	}

	index, err := i.Client.Index(ctx)
	if err != nil {
		return nil, err
	}
	statuses, err := i.statuses(index, selected)
	if err != nil {
		return nil, err
	}

	outcomes := make([]Outcome, 0, len(statuses))
	for _, status := range statuses {
		outcomes = append(outcomes, i.updateOne(ctx, index, status))
	}
	return outcomes, nil
}

// outcomeFor reports what an update run would decide for one status, without
// performing it. updateOne calls it and then acts; a test calls it to pin the
// decision. The branches are the whole of what #135 turned on, and reaching
// them through a live catalog proved nothing that this does not.
func outcomeFor(status Status) Outcome {
	outcome := Outcome{
		Namespace: status.Namespace,
		From:      status.Installed,
		To:        status.Installed,
		Channel:   status.Channel,
	}
	switch {
	case status.Pinned:
		outcome.Action = ActionPinned
	case status.Available == "":
		outcome.Action = ActionNotPublished
	case !status.Update:
		outcome.Action = ActionCurrent
	default:
		outcome.Action = ActionUpdated // provisional; updateOne performs it
	}
	return outcome
}

// updateOne moves one module, or reports why it was not moved.
func (i Installer) updateOne(ctx context.Context, index catalog.Index, status Status) Outcome {
	outcome := outcomeFor(status)
	switch outcome.Action {
	case ActionPinned, ActionNotPublished, ActionCurrent:
		return outcome
	}

	request := Request{
		Namespace: status.Namespace,
		Policy:    catalog.Policy{Channel: status.Channel},
	}
	updated, err := i.runWithIndex(ctx, index, request)
	if err != nil {
		outcome.Action = ActionFailed
		outcome.Err = err
		return outcome
	}
	outcome.Action = ActionUpdated
	outcome.To = updated.Version
	if updated.Version == status.Installed {
		// The channel's newest version was not launchable here, and the newest
		// that was is the one already installed. Nothing moved.
		outcome.Action = ActionCurrent
	}
	return outcome
}

// selectInstalled reports the installed modules an update run covers. Naming a
// module that is not installed is a mistake rather than a silent no-op: an
// update run over nothing looks exactly like one that worked.
func selectInstalled(installed []modules.Installed, namespaces []string) ([]modules.Installed, error) {
	if len(namespaces) == 0 {
		return installed, nil
	}
	selected := make([]modules.Installed, 0, len(namespaces))
	for _, namespace := range namespaces {
		found := false
		for _, entry := range installed {
			if entry.Namespace == namespace {
				selected = append(selected, entry)
				found = true
				break
			}
		}
		if !found {
			return nil, problem.New(problem.CategoryUsage, "modules.not_installed",
				fmt.Sprintf("no version of the %q module is installed", namespace)).
				WithRecovery("Run wso2 module install " + namespace + " to install it, or wso2 version to see what is installed.")
		}
	}
	return selected, nil
}

// Available reports the modules the catalog publishes, in one index request, so
// what can be installed is discoverable without reading documentation.
func (i Installer) Available(ctx context.Context) ([]catalog.IndexModule, error) {
	index, err := i.Client.Index(ctx)
	if err != nil {
		return nil, err
	}
	return index.Modules, nil
}
