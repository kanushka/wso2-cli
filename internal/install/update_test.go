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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
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

	outcome := outcomeFor(status) // see Step 3 on why this seam exists

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

	outcome := outcomeFor(status)

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

// updateFixtureCatalog builds an index and namespace document that publish
// the given versions on the stable channel, all backed by the same real
// archive so each one verifies. latest is what the index names as the
// channel's newest version — the same shape a real catalog publishes, where
// the index carries only the newest and the namespace document carries the
// full history.
func updateFixtureCatalog(archive []byte, latest string, versions ...string) (index, namespace []byte) {
	digest := sha256.Sum256(archive)
	artifact := fmt.Sprintf(`{"os": "linux", "arch": "amd64",
		"url": "https://origin.example/demo.tar.gz",
		"size": %d, "sha256": %q}`, len(archive), hex.EncodeToString(digest[:]))
	index = []byte(fmt.Sprintf(`{
		"schemaVersion": 1,
		"modules": [
			{"namespace": "demo", "path": "demo.json",
			 "channels": [{"channel": "stable", "version": %q}]}
		]
	}`, latest))
	var entries string
	for i, version := range versions {
		if i > 0 {
			entries += ","
		}
		entries += fmt.Sprintf(`{"version": %q, "channel": "stable",
			"compatibility": {"shell": ">=0.0.0", "protocolVersions": [1]},
			"artifacts": [%s]}`, version, artifact)
	}
	namespace = []byte(fmt.Sprintf(`{
		"schemaVersion": 1,
		"namespace": "demo",
		"versions": [%s]
	}`, entries))
	return index, namespace
}

// updateFixtureCatalogBadDigest is like updateFixtureCatalog, but the one
// version it publishes carries a digest that does not match the archive, so
// installing it fails verification.
func updateFixtureCatalogBadDigest(archive []byte, version string) (index, namespace []byte) {
	artifact := fmt.Sprintf(`{"os": "linux", "arch": "amd64",
		"url": "https://origin.example/demo.tar.gz",
		"size": %d, "sha256": "0000000000000000000000000000000000000000000000000000000000000000"}`,
		len(archive))
	index = []byte(fmt.Sprintf(`{
		"schemaVersion": 1,
		"modules": [
			{"namespace": "demo", "path": "demo.json",
			 "channels": [{"channel": "stable", "version": %q}]}
		]
	}`, version))
	namespace = []byte(fmt.Sprintf(`{
		"schemaVersion": 1,
		"namespace": "demo",
		"versions": [{"version": %q, "channel": "stable",
			"compatibility": {"shell": ">=0.0.0", "protocolVersions": [1]},
			"artifacts": [%s]}]
	}`, version, artifact))
	return index, namespace
}

func updateFixtureClient(index, namespace, archive []byte) catalog.Client {
	return catalog.Client{
		Origin: "https://origin.example",
		HTTP: &http.Client{Transport: pinFixtureTransport{
			index: index, namespace: namespace, archive: archive}},
	}
}

// TestCheckWithNothingInstalledReportsNothing pins that Check never contacts
// the catalog when there is nothing installed to report on: the zero Client
// below would panic any request it received.
func TestCheckWithNothingInstalledReportsNothing(t *testing.T) {
	installer := Installer{Store: modules.NewStore(t.TempDir())}
	statuses, err := installer.Check(context.Background())
	if err != nil {
		t.Fatalf("Check returned %v", err)
	}
	if statuses != nil {
		t.Errorf("Check = %v, want nil", statuses)
	}
}

// TestCheckRefusesAnUnknownNamespace pins that Check refuses exactly as
// selectInstalled does, rather than silently reporting an empty result.
func TestCheckRefusesAnUnknownNamespace(t *testing.T) {
	installer := Installer{Store: modules.NewStore(t.TempDir())}
	if _, err := installer.Check(context.Background(), "reference"); err == nil {
		t.Fatal("Check on an uninstalled namespace succeeded, want a refusal")
	}
}

// TestCheckReportsAnAvailableUpdate installs an old version through one
// catalog fixture and then checks it against a second that publishes a newer
// one, proving Check joins local inventory against a live index request.
func TestCheckReportsAnAvailableUpdate(t *testing.T) {
	archive := pinFixtureArchive(t)
	oldIndex, oldNamespace := updateFixtureCatalog(archive, "1.0.0", "1.0.0")
	installer := Installer{
		Store:  modules.NewStore(t.TempDir()),
		Client: updateFixtureClient(oldIndex, oldNamespace, archive),
		Shell:  fixtureShell(),
	}
	if _, err := installer.Run(context.Background(), Request{Namespace: fixtureNamespace}); err != nil {
		t.Fatalf("the initial install returned %v", err)
	}

	newIndex, newNamespace := updateFixtureCatalog(archive, "1.1.0", "1.0.0", "1.1.0")
	installer.Client = updateFixtureClient(newIndex, newNamespace, archive)

	statuses, err := installer.Check(context.Background())
	if err != nil {
		t.Fatalf("Check returned %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("Check returned %d statuses, want 1", len(statuses))
	}
	status := statuses[0]
	if status.Installed != "1.0.0" || status.Available != "1.1.0" || !status.Update {
		t.Errorf("status = %+v, want Installed 1.0.0, Available 1.1.0, Update true", status)
	}
}

// TestCheckLocalReportsNoAvailabilityWithoutAskingTheCatalog pins that
// CheckLocal answers from local state alone: Available and Update are their
// zero values, distinct from "the catalog publishes nothing", and no request
// is made — the client is left at its zero value, which would fail loudly
// if it were ever dialled.
func TestCheckLocalReportsNoAvailabilityWithoutAskingTheCatalog(t *testing.T) {
	archive := pinFixtureArchive(t)
	index, namespace := updateFixtureCatalog(archive, "1.0.0", "1.0.0")
	installer := Installer{
		Store:  modules.NewStore(t.TempDir()),
		Client: updateFixtureClient(index, namespace, archive),
		Shell:  fixtureShell(),
	}
	if _, err := installer.Run(context.Background(), Request{Namespace: fixtureNamespace}); err != nil {
		t.Fatalf("the install returned %v", err)
	}
	installer.Client = catalog.Client{} // must not be dialled

	statuses, err := installer.CheckLocal()
	if err != nil {
		t.Fatalf("CheckLocal returned %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("CheckLocal returned %d statuses, want 1", len(statuses))
	}
	if statuses[0].Available != "" || statuses[0].Update {
		t.Errorf("status = %+v, want empty Available and Update false", statuses[0])
	}
}

// TestCheckLocalWithNothingInstalledReportsNothing pins CheckLocal's other
// early return.
func TestCheckLocalWithNothingInstalledReportsNothing(t *testing.T) {
	installer := Installer{Store: modules.NewStore(t.TempDir())}
	statuses, err := installer.CheckLocal()
	if err != nil {
		t.Fatalf("CheckLocal returned %v", err)
	}
	if statuses != nil {
		t.Errorf("CheckLocal = %v, want nil", statuses)
	}
}

// TestNothingWouldMoveWithNoneSelectedIsTrue pins that asking about zero
// modules (nothing installed, and no names given) reports nothing could move.
func TestNothingWouldMoveWithNoneSelectedIsTrue(t *testing.T) {
	installer := Installer{Store: modules.NewStore(t.TempDir())}
	move, err := installer.NothingWouldMove(nil)
	if err != nil {
		t.Fatalf("NothingWouldMove returned %v", err)
	}
	if !move {
		t.Error("NothingWouldMove = false, want true when nothing is installed")
	}
}

// TestNothingWouldMoveRefusesAnUnknownNamespace pins that this reaches the
// same refusal selectInstalled gives Check and Update, rather than reporting
// a false answer for a name that names nothing.
func TestNothingWouldMoveRefusesAnUnknownNamespace(t *testing.T) {
	installer := Installer{Store: modules.NewStore(t.TempDir())}
	if _, err := installer.NothingWouldMove([]string{"reference"}); err == nil {
		t.Fatal("NothingWouldMove on an uninstalled namespace succeeded, want a refusal")
	}
}

// TestNothingWouldMoveIsFalseForAnUnpinnedModule and
// TestNothingWouldMoveIsTrueForAPinnedModule pin the two answers that matter:
// an unpinned install could always move once a newer version is published,
// and a pinned one never can regardless of what the catalog publishes — so
// NothingWouldMove must answer both without a network request.
func TestNothingWouldMoveIsFalseForAnUnpinnedModule(t *testing.T) {
	archive := pinFixtureArchive(t)
	index, namespace := updateFixtureCatalog(archive, "1.0.0", "1.0.0")
	installer := Installer{
		Store:  modules.NewStore(t.TempDir()),
		Client: updateFixtureClient(index, namespace, archive),
		Shell:  fixtureShell(),
	}
	if _, err := installer.Run(context.Background(), Request{Namespace: fixtureNamespace}); err != nil {
		t.Fatalf("the install returned %v", err)
	}

	move, err := installer.NothingWouldMove([]string{fixtureNamespace})
	if err != nil {
		t.Fatalf("NothingWouldMove returned %v", err)
	}
	if move {
		t.Error("NothingWouldMove = true for an unpinned module, want false")
	}
}

func TestNothingWouldMoveIsTrueForAPinnedModule(t *testing.T) {
	archive := pinFixtureArchive(t)
	index, namespace := updateFixtureCatalog(archive, "1.0.0", "1.0.0")
	installer := Installer{
		Store:  modules.NewStore(t.TempDir()),
		Client: updateFixtureClient(index, namespace, archive),
		Shell:  fixtureShell(),
	}
	if _, err := installer.Run(context.Background(),
		Request{Namespace: fixtureNamespace, Policy: catalog.Policy{Version: "1.0.0"}}); err != nil {
		t.Fatalf("the pinning install returned %v", err)
	}

	move, err := installer.NothingWouldMove([]string{fixtureNamespace})
	if err != nil {
		t.Fatalf("NothingWouldMove returned %v", err)
	}
	if !move {
		t.Error("NothingWouldMove = false for a pinned module, want true")
	}
}

// TestUpdateWithNothingSelectedReturnsNil pins Update's own early return,
// distinct from Check's: it is reached through selectInstalled returning
// nothing, which happens when nothing is installed and no names were given.
func TestUpdateWithNothingSelectedReturnsNil(t *testing.T) {
	installer := Installer{Store: modules.NewStore(t.TempDir())}
	outcomes, err := installer.Update(context.Background(), nil)
	if err != nil {
		t.Fatalf("Update returned %v", err)
	}
	if outcomes != nil {
		t.Errorf("Update = %v, want nil", outcomes)
	}
}

// TestUpdateRefusesAnUnknownNamespace pins that Update refuses naming a
// module that is not installed, rather than reporting an empty run.
func TestUpdateRefusesAnUnknownNamespace(t *testing.T) {
	installer := Installer{Store: modules.NewStore(t.TempDir())}
	if _, err := installer.Update(context.Background(), []string{"reference"}); err == nil {
		t.Fatal("Update on an uninstalled namespace succeeded, want a refusal")
	}
}

// TestUpdateSkipsAPinnedModule exercises the full Update path — not just
// outcomeFor — over a module pinned at install time, and proves the store
// still holds the pinned version afterwards.
func TestUpdateSkipsAPinnedModule(t *testing.T) {
	installer := pinFixtureInstaller(t)
	if _, err := installer.Run(context.Background(),
		Request{Namespace: fixtureNamespace, Policy: catalog.Policy{Version: "1.0.0"}}); err != nil {
		t.Fatalf("the pinning install returned %v", err)
	}

	outcomes, err := installer.Update(context.Background(), []string{fixtureNamespace})
	if err != nil {
		t.Fatalf("Update returned %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("Update returned %d outcomes, want 1", len(outcomes))
	}
	if outcomes[0].Action != ActionPinned || outcomes[0].To != "1.0.0" {
		t.Errorf("outcome = %+v, want ActionPinned holding 1.0.0", outcomes[0])
	}
}

// TestUpdateMovesAnUnpinnedModuleToTheNewestVersion exercises the branch that
// actually performs a move: installed at 1.0.0 through one catalog fixture,
// then updated against a second that publishes a newer 1.1.0.
func TestUpdateMovesAnUnpinnedModuleToTheNewestVersion(t *testing.T) {
	archive := pinFixtureArchive(t)
	oldIndex, oldNamespace := updateFixtureCatalog(archive, "1.0.0", "1.0.0")
	installer := Installer{
		Store:  modules.NewStore(t.TempDir()),
		Client: updateFixtureClient(oldIndex, oldNamespace, archive),
		Shell:  fixtureShell(),
	}
	if _, err := installer.Run(context.Background(), Request{Namespace: fixtureNamespace}); err != nil {
		t.Fatalf("the initial install returned %v", err)
	}

	newIndex, newNamespace := updateFixtureCatalog(archive, "1.1.0", "1.0.0", "1.1.0")
	installer.Client = updateFixtureClient(newIndex, newNamespace, archive)

	outcomes, err := installer.Update(context.Background(), []string{fixtureNamespace})
	if err != nil {
		t.Fatalf("Update returned %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("Update returned %d outcomes, want 1", len(outcomes))
	}
	outcome := outcomes[0]
	if outcome.Action != ActionUpdated || outcome.From != "1.0.0" || outcome.To != "1.1.0" {
		t.Errorf("outcome = %+v, want ActionUpdated from 1.0.0 to 1.1.0", outcome)
	}

	active, err := installer.Store.ReadActive(fixtureNamespace)
	if err != nil {
		t.Fatalf("ReadActive returned %v", err)
	}
	if active.Version != "1.1.0" {
		t.Errorf("the store activated %q, want 1.1.0", active.Version)
	}
}

// TestUpdateReportsAFailedMoveAndLeavesTheOldVersionActive pins updateOne's
// failure branch: the newer version the catalog publishes fails digest
// verification, and the run must report the failure rather than lose the
// version that was working.
func TestUpdateReportsAFailedMoveAndLeavesTheOldVersionActive(t *testing.T) {
	archive := pinFixtureArchive(t)
	oldIndex, oldNamespace := updateFixtureCatalog(archive, "1.0.0", "1.0.0")
	installer := Installer{
		Store:  modules.NewStore(t.TempDir()),
		Client: updateFixtureClient(oldIndex, oldNamespace, archive),
		Shell:  fixtureShell(),
	}
	if _, err := installer.Run(context.Background(), Request{Namespace: fixtureNamespace}); err != nil {
		t.Fatalf("the initial install returned %v", err)
	}

	badIndex, badNamespace := updateFixtureCatalogBadDigest(archive, "1.1.0")
	installer.Client = updateFixtureClient(badIndex, badNamespace, archive)

	outcomes, err := installer.Update(context.Background(), []string{fixtureNamespace})
	if err != nil {
		t.Fatalf("Update returned %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("Update returned %d outcomes, want 1", len(outcomes))
	}
	outcome := outcomes[0]
	if outcome.Action != ActionFailed || outcome.Err == nil {
		t.Errorf("outcome = %+v, want ActionFailed carrying the cause", outcome)
	}
	if outcome.From != "1.0.0" || outcome.To != "1.0.0" {
		t.Errorf("outcome = %+v, want From and To both still 1.0.0: a failed move changes nothing", outcome)
	}

	active, err := installer.Store.ReadActive(fixtureNamespace)
	if err != nil {
		t.Fatalf("ReadActive returned %v", err)
	}
	if active.Version != "1.0.0" {
		t.Errorf("the store activated %q, want the untouched 1.0.0", active.Version)
	}
}

// TestSelectInstalledWithNoNamesReturnsEveryInstalledModule and
// TestSelectInstalledRefusesANameThatIsNotInstalled pin selectInstalled
// directly, since it is the one refusal Check, NothingWouldMove and Update
// all share.
func TestSelectInstalledWithNoNamesReturnsEveryInstalledModule(t *testing.T) {
	installed := []modules.Installed{{Namespace: "reference", Version: "1.0.0"}}
	selected, err := selectInstalled(installed, nil)
	if err != nil {
		t.Fatalf("selectInstalled returned %v", err)
	}
	if len(selected) != 1 || selected[0].Namespace != "reference" {
		t.Errorf("selectInstalled(nil) = %v, want the one installed module", selected)
	}
}

func TestSelectInstalledRefusesANameThatIsNotInstalled(t *testing.T) {
	installed := []modules.Installed{{Namespace: "reference", Version: "1.0.0"}}
	if _, err := selectInstalled(installed, []string{"other"}); err == nil {
		t.Fatal("selectInstalled with an uninstalled name succeeded, want a refusal")
	}
}

// TestListReportsAnInstalledModuleAndAPublishedOne pins List's join: an
// installed module reports through statuses, a published module that is not
// installed reports through installable, and the two are merged and sorted
// by namespace.
func TestListReportsAnInstalledModuleAndAPublishedOne(t *testing.T) {
	archive := pinFixtureArchive(t)
	digest := sha256.Sum256(archive)
	artifact := fmt.Sprintf(`{"os": "linux", "arch": "amd64",
		"url": "https://origin.example/demo.tar.gz",
		"size": %d, "sha256": %q}`, len(archive), hex.EncodeToString(digest[:]))
	index := []byte(fmt.Sprintf(`{
		"schemaVersion": 1,
		"modules": [
			{"namespace": "demo", "path": "demo.json",
			 "channels": [{"channel": "stable", "version": "1.0.0"}]},
			{"namespace": "reference", "path": "reference.json",
			 "channels": [{"channel": "stable", "version": "2.0.0"}]}
		]
	}`))
	namespace := []byte(fmt.Sprintf(`{
		"schemaVersion": 1,
		"namespace": "demo",
		"versions": [{"version": "1.0.0", "channel": "stable",
			"compatibility": {"shell": ">=0.0.0", "protocolVersions": [1]},
			"artifacts": [%s]}]
	}`, artifact))
	installer := Installer{
		Store:  modules.NewStore(t.TempDir()),
		Client: updateFixtureClient(index, namespace, archive),
		Shell:  fixtureShell(),
	}
	if _, err := installer.Run(context.Background(), Request{Namespace: fixtureNamespace}); err != nil {
		t.Fatalf("the install returned %v", err)
	}

	statuses, err := installer.List(context.Background())
	if err != nil {
		t.Fatalf("List returned %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("List returned %d statuses, want 2: %+v", len(statuses), statuses)
	}
	// Sorted by namespace: "demo" before "reference".
	if statuses[0].Namespace != "demo" || statuses[0].Installed != "1.0.0" {
		t.Errorf("statuses[0] = %+v, want the installed demo module", statuses[0])
	}
	if statuses[1].Namespace != "reference" || statuses[1].Installed != "" || statuses[1].Available != "2.0.0" {
		t.Errorf("statuses[1] = %+v, want the published, uninstalled reference module at 2.0.0", statuses[1])
	}
}

// TestInstallableReportsNothingForAModuleWithNoChannels,
// TestInstallablePrefersTheStableChannel and
// TestInstallableFallsBackToTheFirstChannel pin installable directly: what a
// plain install would follow, given what a module publishes.
func TestInstallableReportsNothingForAModuleWithNoChannels(t *testing.T) {
	_, ok := installable(catalog.IndexModule{Namespace: "demo"})
	if ok {
		t.Error("installable on a module with no channels reported one, want false")
	}
}

func TestInstallablePrefersTheStableChannel(t *testing.T) {
	module := catalog.IndexModule{
		Namespace: "demo",
		Channels: []catalog.IndexChannel{
			{Channel: "nightly", Version: "9.9.9"},
			{Channel: "stable", Version: "1.0.0"},
		},
	}
	status, ok := installable(module)
	if !ok {
		t.Fatal("installable reported no status for a published module")
	}
	if status.Channel != "stable" || status.Available != "1.0.0" {
		t.Errorf("status = %+v, want the stable channel at 1.0.0", status)
	}
}

func TestInstallableFallsBackToTheFirstChannel(t *testing.T) {
	module := catalog.IndexModule{
		Namespace: "demo",
		Channels: []catalog.IndexChannel{
			{Channel: "beta", Version: "2.0.0"},
		},
	}
	status, ok := installable(module)
	if !ok {
		t.Fatal("installable reported no status for a published module")
	}
	if status.Channel != "beta" || status.Available != "2.0.0" {
		t.Errorf("status = %+v, want the only published channel beta at 2.0.0", status)
	}
}
