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

package module

import (
	"reflect"
	"testing"
)

func TestDescribeReportsSDKOwnedProtocolAndSDKVersions(t *testing.T) {
	descriptor := Describe(Options{
		Namespace:     "reference",
		Version:       "0.1.0",
		AuthAudiences: []string{"reference-status"},
		AuthScopes:    []string{"reference:status:read"},
	})

	if descriptor.Namespace != "reference" || descriptor.Version != "0.1.0" {
		t.Fatalf("descriptor identity = %+v, want namespace reference version 0.1.0", descriptor)
	}
	if descriptor.SDKVersion != SDKVersion {
		t.Fatalf("descriptor SDK version = %q, want %q", descriptor.SDKVersion, SDKVersion)
	}
	if !reflect.DeepEqual(descriptor.ProtocolVersions, []int{1}) {
		t.Fatalf("descriptor protocol versions = %v, want [1]", descriptor.ProtocolVersions)
	}
}

func TestDescribeCopiesDeclaredAccessSlices(t *testing.T) {
	audiences := []string{"reference-status"}
	descriptor := Describe(Options{Namespace: "reference", Version: "0.1.0", AuthAudiences: audiences})

	audiences[0] = "mutated"

	if descriptor.AuthAudiences[0] != "reference-status" {
		t.Fatal("Describe aliased the caller's audience slice; declared access must not change after description")
	}
}
