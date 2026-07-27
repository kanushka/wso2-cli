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

// Package version reports the shell's own identity: its release version, the
// module-contract protocol version it speaks, and the platform it runs on.
//
// The shell, protocol, SDK, and each module version move independently. Only
// the first two belong to this package.
package version

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"github.com/wso2/wso2-cli/internal/semver"
	"github.com/wso2/wso2-cli/sdk/protocol"
)

// Build-time variables. A release build injects them with:
//
//	go build -ldflags "-X github.com/wso2/wso2-cli/internal/version.shellVersion=0.1.0"
//
// Tests inject them to prove the shell, protocol, SDK, and module versions can
// vary independently.
var (
	shellVersion    = "0.0.0-dev"
	protocolVersion = "1"
)

// Info is the shell-owned version inventory, excluding installed modules. Its
// fields are display strings; ProtocolVersions reports the comparable protocol
// values.
type Info struct {
	Shell    string
	Protocol string
	Platform string
}

// Current reports this shell's version inventory.
func Current() Info {
	return Info{Shell: "v" + Shell(), Protocol: ProtocolDisplay(), Platform: Platform()}
}

// Shell reports the shell release version.
func Shell() string {
	return shellVersion
}

// ShellSemver reports the shell release version as a comparable semantic
// version. A malformed injected version is an error rather than a silent
// compatibility decision.
func ShellSemver() (semver.Version, error) {
	version, err := semver.Parse(shellVersion)
	if err != nil {
		return semver.Version{}, fmt.Errorf("shell version %q is not a semantic version: %w", shellVersion, err)
	}
	return version, nil
}

// ProtocolDisplay renders the protocol versions this shell speaks, newest
// first, for user output. A shell that speaks more than one version says so
// rather than printing its raw configuration.
func ProtocolDisplay() string {
	versions := ProtocolVersions()
	if len(versions) == 0 {
		return "unavailable"
	}
	rendered := make([]string, 0, len(versions))
	for _, version := range versions {
		rendered = append(rendered, "v"+strconv.Itoa(version))
	}
	return strings.Join(rendered, ", ")
}

// ProtocolVersions reports the protocol versions this shell can speak, newest
// first. The shell owns its own configured list rather than reading the SDK's,
// so a module built against a different SDK release cannot widen shell support;
// only the parsing of that list is shared with the SDK.
func ProtocolVersions() []int {
	return protocol.ParseVersions(protocolVersion)
}

// Platform reports the operating system and architecture pair used for module
// compatibility decisions.
func Platform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}
