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

// Package release is the release job's half of publishing a product module:
// the gate that decides whether a module may be published at all, the archive
// layout its artifacts are published under, and the assembly of the input the
// catalog generator reads.
//
// The gate here is not the pull-request gate. The pull-request gate in
// scripts/previous-protocol.sh asks whether a change to the shell or the SDK
// broke the older half of the protocol window, and it runs on every pull
// request. This one asks whether a module being released can run on any shell
// that exists at all, and it runs on a tag push. They catch different failures
// and neither stands in for the other.
package release

import (
	"fmt"
	"sort"
	"strings"

	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/semver"
	"github.com/wso2/wso2-cli/sdk/protocol"
)

// ShellWindow reports the protocol versions the released shell supports.
//
// It is read from sdk/protocol, which is the same single declaration the shell
// itself reads when it announces what it speaks, so the gate and the shell
// cannot come to disagree about what is supported. The release workflow holds
// the other end of that equality: it asks the published binary what it speaks
// and fails the release when the answer is not what the source declares, so
// what is read here describes the shell a user can actually have.
func ShellWindow() []int {
	return protocol.Window()
}

// Gate decides whether a module version may be published.
//
// The decision is a pure function of the module's declared compatibility and
// the released shell's supported set, which is what makes it provable by test
// rather than only by a real tag push. A module is admitted when at least one
// protocol version it declares is one the released shell speaks. Everything
// else is refused, because publishing it would put a module on the catalog
// that no shell in existence can launch.
//
// The refusal names both sides. A product team reading it has to choose
// between waiting for a shell release and changing the module, and neither
// half of the comparison tells them which.
//
// What the module declares is taken on trust here and proved elsewhere: the
// conformance job builds the module against the published SDK for the previous
// protocol and launches it under the current shell, which is what makes a
// declaration of the older half of the window mean something.
func Gate(namespace, version string, module modules.Compatibility, shellWindow []int) error {
	if len(module.ProtocolVersions) == 0 {
		return fmt.Errorf("release refused: the %s module at %s declares no module-contract protocol version, "+
			"so nothing states which shells can launch it", namespace, version)
	}
	if len(shellWindow) == 0 {
		// Reading an empty window as "no constraint" would turn a broken
		// declaration into an open gate, so it fails closed.
		return fmt.Errorf("release refused: the released shell declares no supported module-contract protocol, "+
			"so whether the %s module at %s can run cannot be decided", namespace, version)
	}
	if module.Shell != "" {
		if _, err := semver.ParseRange(module.Shell); err != nil {
			return fmt.Errorf("release refused: the %s module at %s declares an unreadable shell range %q: %w",
				namespace, version, module.Shell, err)
		}
	} else {
		return fmt.Errorf("release refused: the %s module at %s declares no shell range", namespace, version)
	}

	supported := map[int]bool{}
	for _, version := range shellWindow {
		supported[version] = true
	}
	for _, declared := range module.ProtocolVersions {
		if supported[declared] {
			return nil
		}
	}

	required := FormatProtocols(module.ProtocolVersions)
	speaks := FormatProtocols(shellWindow)
	refusal := fmt.Sprintf("release refused: the %s module at %s requires module-contract protocol %s, "+
		"and the released shell speaks %s", namespace, version, required, speaks)
	if lowest(module.ProtocolVersions) > highest(shellWindow) {
		return fmt.Errorf("%s. The shell ships first: publishing this would put a module on the catalog "+
			"that no installed shell can launch. Either wait for a shell release that speaks %s, "+
			"or build this module against the SDK for %s", refusal, required, speaks)
	}
	return fmt.Errorf("%s. No shell that exists can launch it: %s is outside the window the released shell "+
		"supports, so build this module against the SDK for %s", refusal, required, speaks)
}

// FormatProtocols renders a protocol version set the way the shell's own
// refusals render one, newest first, so the two read alike.
func FormatProtocols(versions []int) string {
	ordered := append([]int(nil), versions...)
	sort.Sort(sort.Reverse(sort.IntSlice(ordered)))
	rendered := make([]string, 0, len(ordered))
	for _, version := range ordered {
		rendered = append(rendered, fmt.Sprintf("v%d", version))
	}
	if len(rendered) == 0 {
		return "no version"
	}
	return strings.Join(rendered, ", ")
}

func lowest(versions []int) int {
	found := versions[0]
	for _, version := range versions {
		if version < found {
			found = version
		}
	}
	return found
}

func highest(versions []int) int {
	found := versions[0]
	for _, version := range versions {
		if version > found {
			found = version
		}
	}
	return found
}
