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

package protocol

import (
	"reflect"
	"testing"
)

func TestParseVersionsSortsNewestFirstAndDropsInvalidEntries(t *testing.T) {
	tests := []struct {
		name string
		list string
		want []int
	}{
		{name: "single", list: "1", want: []int{1}},
		{name: "sorted newest first", list: "1,3,2", want: []int{3, 2, 1}},
		{name: "duplicates removed", list: "2,2,1", want: []int{2, 1}},
		{name: "whitespace tolerated", list: " 2 , 1 ", want: []int{2, 1}},
		{name: "non numeric dropped", list: "1,v2,x", want: []int{1}},
		{name: "non positive dropped", list: "0,-1,1", want: []int{1}},
		{name: "empty", list: "", want: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ParseVersions(test.list); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("ParseVersions(%q) = %v, want %v", test.list, got, test.want)
			}
		})
	}
}

func TestSupportedReflectsBuildTimeVersion(t *testing.T) {
	if got := Supported(); !reflect.DeepEqual(got, ParseVersions(Version)) {
		t.Fatalf("Supported() = %v, want the versions declared by Version %q", got, Version)
	}
}

func TestWindowIsTheCurrentVersionAndItsPredecessor(t *testing.T) {
	original := Version
	t.Cleanup(func() { Version = original })

	tests := []struct {
		name    string
		version string
		want    []int
	}{
		{name: "declared", version: original, want: []int{2, 1}},
		{name: "first generation has no predecessor", version: "1", want: []int{1}},
		{name: "later generation", version: "7", want: []int{7, 6}},
		{name: "unreadable", version: "x", want: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			Version = test.version
			if got := Window(); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Window() with Version %q = %v, want %v", test.version, got, test.want)
			}
		})
	}
}
