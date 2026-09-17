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
	"errors"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/sdk/problem"
)

func TestEmptyChannelRefusalNamesThePublishedChannels(t *testing.T) {
	file := NamespaceFile{
		Namespace: "reference",
		Versions: []Version{
			{Version: "0.1.0-rc.3", Channel: ChannelPrerelease},
			{Version: "0.2.0-rc.1", Channel: ChannelPrerelease},
		},
	}
	_, err := permittedVersions(file, Policy{Channel: ChannelStable})
	var typed problem.Problem
	if !errors.As(err, &typed) {
		t.Fatalf("want a typed problem, got %v", err)
	}
	if typed.Code != "catalog.empty_channel" {
		t.Errorf("code = %q, want catalog.empty_channel", typed.Code)
	}
	// The channels the module does publish on, which no command lists: wso2
	// product list names each product on one channel only.
	if !strings.Contains(typed.Recovery, ChannelPrerelease) {
		t.Errorf("recovery does not name the published channel: %q", typed.Recovery)
	}
	// The flag that chooses one.
	if !strings.Contains(typed.Recovery, "--channel") {
		t.Errorf("recovery does not name --channel: %q", typed.Recovery)
	}
}

func TestAChannelTheModulePublishesNothingOnIsDistinguishable(t *testing.T) {
	// A channel name the catalog has never published under is a typo. The
	// stable channel on a module that only ships prereleases is a real channel
	// that happens to be empty today, and waiting for a stable release is the
	// right response to it. One shared code cannot say which happened.
	file := NamespaceFile{
		Namespace: "reference",
		Versions:  []Version{{Version: "0.1.0-rc.3", Channel: ChannelPrerelease}},
	}
	_, unknownErr := permittedVersions(file, Policy{Channel: "nosuch"})
	_, emptyErr := permittedVersions(file, Policy{Channel: ChannelStable})
	var unknown, empty problem.Problem
	if !errors.As(unknownErr, &unknown) {
		t.Fatalf("want a typed problem for an unknown channel, got %v", unknownErr)
	}
	if !errors.As(emptyErr, &empty) {
		t.Fatalf("want a typed problem for an empty channel, got %v", emptyErr)
	}
	if unknown.Code == empty.Code {
		t.Errorf("a typo and a real-but-empty channel share the code %q", unknown.Code)
	}
	if unknown.Code != "catalog.unknown_channel" {
		t.Errorf("code = %q, want catalog.unknown_channel", unknown.Code)
	}
	if empty.Code != "catalog.empty_channel" {
		t.Errorf("code = %q, want catalog.empty_channel", empty.Code)
	}
	if unknown.Message == empty.Message {
		t.Errorf("a typo and a real-but-empty channel are indistinguishable: %q", unknown.Message)
	}
	if !strings.Contains(unknown.Recovery, ChannelPrerelease) || !strings.Contains(unknown.Recovery, "--channel") {
		t.Errorf("recovery does not name the published channel and --channel: %q", unknown.Recovery)
	}
}

func TestAModuleThatHasPublishedNothingIsNotToldToChooseAChannel(t *testing.T) {
	// A module in the catalog with an empty version history is a publishing
	// failure, not a user's mistake, and there is no channel for the user to
	// choose. Naming --channel here would send them round a loop no flag can
	// break, so the recovery points at the maintainers instead.
	_, err := permittedVersions(NamespaceFile{Namespace: "reference"}, Policy{Channel: ChannelStable})
	var typed problem.Problem
	if !errors.As(err, &typed) {
		t.Fatalf("want a typed problem, got %v", err)
	}
	if typed.Code != "catalog.empty_channel" {
		t.Errorf("code = %q, want catalog.empty_channel", typed.Code)
	}
	if !strings.Contains(typed.Message, "reference") {
		t.Errorf("message does not name the module: %q", typed.Message)
	}
	if strings.Contains(typed.Recovery, "--channel") {
		t.Errorf("recovery offers --channel when there is no channel to choose: %q", typed.Recovery)
	}
	if !strings.Contains(typed.Recovery, "maintainers") {
		t.Errorf("recovery does not point at the module's maintainers: %q", typed.Recovery)
	}
}

// TestSelectPicksTheNewestSpeakableVersionForThePlatform exercises Select
// end to end: among what the channel and pin permit, it selects the newest
// version whose protocol versions intersect the shell's and which publishes
// an artifact for the shell's own platform, skipping a newer version this
// shell could not launch.
func TestSelectPicksTheNewestSpeakableVersionForThePlatform(t *testing.T) {
	platform := modules.Platform{OS: "linux", Arch: "amd64"}
	file := NamespaceFile{
		Namespace: "reference",
		Versions: []Version{
			{
				Version:       "2.0.0",
				Channel:       ChannelStable,
				Compatibility: modules.Compatibility{ProtocolVersions: []int{99}}, // this shell cannot speak it
				Artifacts:     []VersionArtifact{{Platform: platform, URL: "https://example/2.0.0", Size: 1}},
			},
			{
				Version:       "1.0.0",
				Channel:       ChannelStable,
				Compatibility: modules.Compatibility{ProtocolVersions: []int{1}},
				Artifacts:     []VersionArtifact{{Platform: platform, URL: "https://example/1.0.0", Size: 1}},
			},
		},
	}
	shell := modules.ShellIdentity{ProtocolVersions: []int{1}, Platform: platform}

	selection, err := Select(file, Policy{}, shell)
	if err != nil {
		t.Fatalf("Select returned %v", err)
	}
	if selection.Version.Version != "1.0.0" {
		t.Errorf("Select chose %q, want 1.0.0: the newer version speaks a protocol this shell does not",
			selection.Version.Version)
	}
}

// TestSelectRefusesWhenNoSpeakableVersionPublishesThisPlatform pins the
// unsupported-platform refusal: every speakable version exists, but none
// publishes an artifact for the shell's platform.
func TestSelectRefusesWhenNoSpeakableVersionPublishesThisPlatform(t *testing.T) {
	file := NamespaceFile{
		Namespace: "reference",
		Versions: []Version{{
			Version:       "1.0.0",
			Channel:       ChannelStable,
			Compatibility: modules.Compatibility{ProtocolVersions: []int{1}},
			Artifacts: []VersionArtifact{{
				Platform: modules.Platform{OS: "windows", Arch: "amd64"},
				URL:      "https://example/1.0.0", Size: 1,
			}},
		}},
	}
	shell := modules.ShellIdentity{
		ProtocolVersions: []int{1},
		Platform:         modules.Platform{OS: "linux", Arch: "amd64"},
	}

	_, err := Select(file, Policy{}, shell)
	var typed problem.Problem
	if !errors.As(err, &typed) {
		t.Fatalf("want a typed problem, got %v", err)
	}
	if typed.Code != "modules.unsupported_platform" {
		t.Errorf("code = %q, want modules.unsupported_platform", typed.Code)
	}
}

// TestSelectRefusesAnIncompatibleProtocol pins the other refusal Select
// itself decides between: every permitted version exists but none speaks a
// protocol this shell speaks.
func TestSelectRefusesAnIncompatibleProtocol(t *testing.T) {
	file := NamespaceFile{
		Namespace: "reference",
		Versions: []Version{{
			Version:       "1.0.0",
			Channel:       ChannelStable,
			Compatibility: modules.Compatibility{ProtocolVersions: []int{99}},
		}},
	}
	shell := modules.ShellIdentity{ProtocolVersions: []int{1}}

	_, err := Select(file, Policy{}, shell)
	var typed problem.Problem
	if !errors.As(err, &typed) {
		t.Fatalf("want a typed problem, got %v", err)
	}
	if typed.Code != "modules.incompatible_protocol" {
		t.Errorf("code = %q, want modules.incompatible_protocol", typed.Code)
	}
	if !strings.Contains(typed.Message, "v99") || !strings.Contains(typed.Message, "v1") {
		t.Errorf("message does not name both protocol sets: %q", typed.Message)
	}
}

// TestSelectRefusesAnUnpublishedPin pins that pinning a version the module
// does not publish is refused rather than falling through to channel
// selection.
func TestSelectRefusesAnUnpublishedPin(t *testing.T) {
	file := NamespaceFile{Namespace: "reference", Versions: []Version{
		{Version: "1.0.0", Channel: ChannelStable},
	}}
	_, err := Select(file, Policy{Version: "9.9.9"}, modules.ShellIdentity{})
	var typed problem.Problem
	if !errors.As(err, &typed) || typed.Code != "catalog.version_not_published" {
		t.Fatalf("err = %v, want a catalog.version_not_published problem", err)
	}
}

// TestSelectRefusesAMalformedPin pins the version-parse refusal on Policy.Version.
func TestSelectRefusesAMalformedPin(t *testing.T) {
	file := NamespaceFile{Namespace: "reference"}
	_, err := Select(file, Policy{Version: "not-a-version"}, modules.ShellIdentity{})
	var typed problem.Problem
	if !errors.As(err, &typed) || typed.Code != "catalog.malformed_version" {
		t.Fatalf("err = %v, want a catalog.malformed_version problem", err)
	}
}

// TestIntersectsReportsAnySharedProtocolVersion pins the two-sided membership
// test that decides what "speakable" means for Select.
func TestIntersectsReportsAnySharedProtocolVersion(t *testing.T) {
	if !intersects([]int{1, 2}, []int{2, 3}) {
		t.Error("intersects([1,2],[2,3]) = false, want true: they share 2")
	}
	if intersects([]int{1, 2}, []int{3, 4}) {
		t.Error("intersects([1,2],[3,4]) = true, want false: they share nothing")
	}
	if intersects(nil, []int{1}) || intersects([]int{1}, nil) {
		t.Error("intersects with an empty side reported true, want false")
	}
}

// TestFormatVersionsNamesEachAsAVLine and
// TestFormatVersionsWithNoneNamesNoVersion pin the two shapes
// incompatibleProtocol's message is built from.
func TestFormatVersionsNamesEachAsAVLine(t *testing.T) {
	if got, want := formatVersions([]int{2, 1}), "v2, v1"; got != want {
		t.Errorf("formatVersions([2,1]) = %q, want %q", got, want)
	}
}

func TestFormatVersionsWithNoneNamesNoVersion(t *testing.T) {
	if got, want := formatVersions(nil), "no version"; got != want {
		t.Errorf("formatVersions(nil) = %q, want %q", got, want)
	}
}

// TestFormatProtocolsOrdersNewestFirst pins that the published side of the
// refusal is sorted descending, unlike formatVersions' input order, which is
// the shell's own declared order and is left alone.
func TestFormatProtocolsOrdersNewestFirst(t *testing.T) {
	got := formatProtocols(map[int]bool{1: true, 3: true, 2: true})
	if got != "v3, v2, v1" {
		t.Errorf("formatProtocols(...) = %q, want v3, v2, v1", got)
	}
}
