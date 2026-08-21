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

// Package protocol declares the module-contract protocol versions this SDK
// implements. The shell selects one mutually supported version during the
// handshake.
package protocol

import (
	"sort"
	"strconv"
	"strings"
)

// Version is the protocol version this SDK release implements. It is the one
// declaration of the current protocol generation, so everything that has to
// agree about what is supported — the shell's window today, and the release
// gate that will refuse a module the released shell cannot launch — derives
// from it rather than restating it.
//
// The value is a build-time variable so tests can compose a module that
// advertises a protocol version other than the shell's, without editing
// source. Override it with:
//
//	-ldflags "-X github.com/wso2/wso2-cli/sdk/protocol.Version=1"
var Version = "2"

// Supported reports the protocol versions a module built against this SDK
// release speaks, newest first.
//
// A module speaks the version of the SDK it was built against. Widening is the
// shell's job, not a module's: see Window.
func Supported() []int {
	versions := ParseVersions(Version)
	if len(versions) == 0 {
		return nil
	}
	return versions
}

// Window reports the protocol versions a shell of this release supports,
// newest first: the current version and its predecessor.
//
// Version declares one generation, so the window is derived from the newest
// version it names. The predecessor is what gives a user a full protocol
// generation in which to update the shell before a module release can outrun
// it: a shell speaking only the current version would cut off every user one
// generation behind on the day the generation changed. The first generation
// has no predecessor, so the window is one version wide until there is
// something to be behind.
func Window() []int {
	current := Supported()
	if len(current) == 0 {
		return nil
	}
	newest := current[0]
	if newest <= 1 {
		return []int{newest}
	}
	return []int{newest, newest - 1}
}

// ParseVersions reads a comma-separated protocol version list such as "1,2".
// Entries that are not positive integers are ignored, and the result is sorted
// newest first with duplicates removed.
func ParseVersions(list string) []int {
	seen := make(map[int]struct{})
	var versions []int
	for _, field := range strings.Split(list, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		value, err := strconv.Atoi(field)
		if err != nil || value <= 0 {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		versions = append(versions, value)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(versions)))
	return versions
}
