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
	// The channels the module does publish on, so the user does not have to run
	// wso2 module available to find out.
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
