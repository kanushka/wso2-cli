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

package version

import (
	"reflect"
	"runtime"
	"testing"
)

func TestCurrentReportsShellProtocolAndPlatform(t *testing.T) {
	info := Current()

	if info.Shell != "v"+shellVersion {
		t.Errorf("shell version = %q, want %q", info.Shell, "v"+shellVersion)
	}
	if info.Protocol != "v"+protocolVersion {
		t.Errorf("protocol version = %q, want %q", info.Protocol, "v"+protocolVersion)
	}
	if want := runtime.GOOS + "/" + runtime.GOARCH; info.Platform != want {
		t.Errorf("platform = %q, want %q", info.Platform, want)
	}
}

func TestShellSemverParsesTheInjectedVersion(t *testing.T) {
	original := shellVersion
	t.Cleanup(func() { shellVersion = original })

	shellVersion = "1.4.2"
	version, err := ShellSemver()
	if err != nil {
		t.Fatalf("ShellSemver returned %v", err)
	}
	if version.Major != 1 || version.Minor != 4 || version.Patch != 2 {
		t.Fatalf("ShellSemver = %+v, want 1.4.2", version)
	}
}

func TestShellSemverFailsClosedOnAMalformedInjectedVersion(t *testing.T) {
	original := shellVersion
	t.Cleanup(func() { shellVersion = original })

	shellVersion = "not-a-version"
	if _, err := ShellSemver(); err == nil {
		t.Fatal("ShellSemver accepted a malformed shell version; a compatibility decision must not be guessed")
	}
}

func TestTheDefaultShellVersionIsAValidSemanticVersion(t *testing.T) {
	if _, err := ShellSemver(); err != nil {
		t.Fatalf("the default shell version is unusable: %v", err)
	}
}

func TestProtocolDisplayRendersEverySupportedVersionNewestFirst(t *testing.T) {
	original := protocolVersion
	t.Cleanup(func() { protocolVersion = original })

	protocolVersion = "1,2"
	if got := ProtocolDisplay(); got != "v2, v1" {
		t.Fatalf("ProtocolDisplay() = %q, want %q; a multi-version shell must not print its raw configuration", got, "v2, v1")
	}
}

func TestProtocolDisplayReportsAnUnusableConfigurationInstead(t *testing.T) {
	original := protocolVersion
	t.Cleanup(func() { protocolVersion = original })

	protocolVersion = "not-a-version"
	if got := ProtocolDisplay(); got != "unavailable" {
		t.Fatalf("ProtocolDisplay() = %q, want %q", got, "unavailable")
	}
}

func TestProtocolVersionsReadsTheInjectedList(t *testing.T) {
	original := protocolVersion
	t.Cleanup(func() { protocolVersion = original })

	protocolVersion = "1,3,2,3,x,0"
	if got := ProtocolVersions(); !reflect.DeepEqual(got, []int{3, 2, 1}) {
		t.Fatalf("ProtocolVersions() = %v, want [3 2 1] with invalid entries dropped", got)
	}
}

func TestTheDefaultProtocolVersionIsSupported(t *testing.T) {
	if got := ProtocolVersions(); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("ProtocolVersions() = %v, want [1]", got)
	}
}
