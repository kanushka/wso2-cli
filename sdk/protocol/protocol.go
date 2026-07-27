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

// Version is the protocol version this SDK release implements.
//
// The value is a build-time variable so tests can compose a module that
// advertises a protocol version other than the shell's, without editing
// source. Override it with:
//
//	-ldflags "-X github.com/wso2/wso2-cli/sdk/protocol.Version=2"
var Version = "1"

// Supported reports the protocol versions this SDK can speak, newest first.
//
// The architecture proof implements exactly one version. The plural shape
// exists because the handshake negotiates over a set.
func Supported() []int {
	versions := ParseVersions(Version)
	if len(versions) == 0 {
		return nil
	}
	return versions
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
