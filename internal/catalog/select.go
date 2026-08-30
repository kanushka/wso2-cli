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
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/semver"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// Policy is what the user asked for: a channel, or an exact version pinned.
type Policy struct {
	// Channel is the release channel to select from. An empty channel is
	// stable.
	Channel string
	// Version pins an exact version. A pin overrides the channel, so a pinned
	// prerelease is installable without putting the module on that channel.
	Version string
}

// Selection is one published version and the artifact for the shell's platform.
type Selection struct {
	Version  Version
	Artifact VersionArtifact
}

// Select chooses the version to install from a published version history.
//
// Among the versions the user's channel and pin permit, it selects the newest
// whose protocol versions intersect what the shell speaks and which publishes
// an artifact for the shell's platform. The numerically newest release is not
// assumed usable.
//
// The gate is the protocol versions intersected with the platform, and nothing
// else. A module's version is never compared against the shell's, in either
// direction: a product module carries its product's version scheme, chosen so
// its users recognise it, and that scheme does not track the shell's, so a
// module numbered far above or far below the shell says nothing about whether
// the two can speak. The same invariant is recorded at negotiateProtocol in
// internal/modules/resolve.go, because a selection gate and a launch gate that
// disagreed would install a module that could not then be launched.
func Select(file NamespaceFile, policy Policy, shell modules.ShellIdentity) (Selection, error) {
	permitted, err := permittedVersions(file, policy)
	if err != nil {
		return Selection{}, err
	}

	speakable := make([]Version, 0, len(permitted))
	for _, version := range permitted {
		if intersects(version.Compatibility.ProtocolVersions, shell.ProtocolVersions) {
			speakable = append(speakable, version)
		}
	}
	if len(speakable) == 0 {
		return Selection{}, incompatibleProtocol(file.Namespace, permitted, shell)
	}

	for _, version := range speakable {
		for _, artifact := range version.Artifacts {
			if artifact.Platform == shell.Platform {
				return Selection{Version: version, Artifact: artifact}, nil
			}
		}
	}
	return Selection{}, problem.New(problem.CategoryModuleTrust, "modules.unsupported_platform",
		fmt.Sprintf("the %q module publishes no artifact for %s", file.Namespace, shell.Platform)).
		WithRecovery("This module publishes nothing for this operating system and architecture. It is not a transient failure, and retrying will not change it.")
}

// permittedVersions reports the versions the pin or channel allows, newest
// first. Ordering here is selection order; the published file's own order is
// presentation and is not relied on.
func permittedVersions(file NamespaceFile, policy Policy) ([]Version, error) {
	ordered, err := orderedVersions(file.Versions)
	if err != nil {
		return nil, err
	}

	if policy.Version != "" {
		pinned, err := semver.Parse(policy.Version)
		if err != nil {
			return nil, problem.New(problem.CategoryUsage, "catalog.malformed_version",
				fmt.Sprintf("%q is not a semantic version", policy.Version)).
				WithRecovery("Give a version such as 1.2.3.")
		}
		for _, version := range ordered {
			if version.Version == pinned.String() {
				return []Version{version}, nil
			}
		}
		return nil, problem.New(problem.CategoryUsage, "catalog.version_not_published",
			fmt.Sprintf("the %q module publishes no version %s", file.Namespace, pinned)).
			WithRecovery("Choose a published version.")
	}

	channel := policy.Channel
	if channel == "" {
		channel = ChannelStable
	}
	permitted := make([]Version, 0, len(ordered))
	for _, version := range ordered {
		if version.Channel == channel {
			permitted = append(permitted, version)
		}
	}
	if len(permitted) == 0 {
		published := publishedChannels(ordered)
		if len(published) == 0 {
			return nil, problem.New(problem.CategoryUsage, "catalog.empty_channel",
				fmt.Sprintf("the %q module publishes no versions at all", file.Namespace)).
				WithRecovery("The module is in the catalog but has published nothing. " +
					"Report this to the module's maintainers.")
		}
		on := strings.Join(published, " and ")
		// A name that is not a channel at all is a typo, and no module will ever
		// publish on it. Saying so separates it from a real channel that is
		// merely empty for this module today, where waiting for a release is the
		// right response; the one shared refusal said neither. The split relies
		// on every Version.Channel being one of the two, which Channel in
		// catalog.go establishes by deriving the channel from the version.
		if !slices.Contains(channels, channel) {
			return nil, problem.New(problem.CategoryUsage, "catalog.unknown_channel",
				fmt.Sprintf("there is no release channel named %q", channel)).
				WithRecovery(fmt.Sprintf("The %q module publishes on %s. Choose one with --channel.",
					file.Namespace, on))
		}
		return nil, problem.New(problem.CategoryUsage, "catalog.empty_channel",
			fmt.Sprintf("the %q module publishes no version on the %s channel", file.Namespace, channel)).
			WithRecovery(fmt.Sprintf("It publishes on %s. Choose one with --channel.", on))
	}
	return permitted, nil
}

// publishedChannels names every channel the history publishes on, sorted, so a
// refusal can tell the user what to choose instead of telling them to choose.
// The published file already carries the answer the user would otherwise go
// looking for with wso2 module available. Which of those names are channels at
// all is the channels list in catalog.go.
func publishedChannels(versions []Version) []string {
	var channels []string
	for _, version := range versions {
		if !slices.Contains(channels, version.Channel) {
			channels = append(channels, version.Channel)
		}
	}
	slices.Sort(channels)
	return channels
}

// orderedVersions copies a history into selection order, newest first.
func orderedVersions(versions []Version) ([]Version, error) {
	ordered := append([]Version(nil), versions...)
	parsed := make(map[string]semver.Version, len(ordered))
	for _, version := range ordered {
		value, err := semver.Parse(version.Version)
		if err != nil {
			return nil, problem.New(problem.CategoryModuleTrust, "catalog.malformed_version",
				fmt.Sprintf("the module catalog publishes an unreadable version %q", version.Version)).
				WithRecovery("Report this to the module catalog's maintainers.")
		}
		parsed[version.Version] = value
	}
	sort.SliceStable(ordered, func(left, right int) bool {
		return semver.Compare(parsed[ordered[left].Version], parsed[ordered[right].Version]) > 0
	})
	return ordered, nil
}

func intersects(module, shell []int) bool {
	for _, candidate := range shell {
		for _, supported := range module {
			if candidate == supported {
				return true
			}
		}
	}
	return false
}

// incompatibleProtocol states the refusal in terms of the protocol versions on
// both sides, so a user reads a compatibility problem rather than something
// that looks like a broken download.
func incompatibleProtocol(namespace string, permitted []Version, shell modules.ShellIdentity) problem.Problem {
	published := map[int]bool{}
	for _, version := range permitted {
		for _, supported := range version.Compatibility.ProtocolVersions {
			published[supported] = true
		}
	}
	return problem.New(problem.CategoryModuleTrust, "modules.incompatible_protocol",
		fmt.Sprintf("no published version of the %q module speaks a module-contract protocol this shell speaks; "+
			"the published versions speak %s, and this shell speaks %s",
			namespace, formatProtocols(published), formatVersions(shell.ProtocolVersions))).
		WithRecovery("Update the WSO2 CLI. This shell is too old for every published version of this module.")
}

func formatProtocols(versions map[int]bool) string {
	ordered := make([]int, 0, len(versions))
	for version := range versions {
		ordered = append(ordered, version)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(ordered)))
	return formatVersions(ordered)
}

func formatVersions(versions []int) string {
	if len(versions) == 0 {
		return "no version"
	}
	rendered := make([]string, 0, len(versions))
	for _, version := range versions {
		rendered = append(rendered, fmt.Sprintf("v%d", version))
	}
	return strings.Join(rendered, ", ")
}
