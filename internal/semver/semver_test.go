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

package semver

import "testing"

func TestParseAcceptsOptionalVPrefixAndPrerelease(t *testing.T) {
	version, err := Parse("v1.2.3-rc.1")
	if err != nil {
		t.Fatalf("Parse returned %v", err)
	}
	if version.Major != 1 || version.Minor != 2 || version.Patch != 3 {
		t.Fatalf("Parse produced %+v, want 1.2.3", version)
	}
	if version.Prerelease != "rc.1" {
		t.Fatalf("prerelease = %q, want rc.1", version.Prerelease)
	}
	if got := version.String(); got != "1.2.3-rc.1" {
		t.Fatalf("String() = %q, want 1.2.3-rc.1", got)
	}
}

func TestParseRejectsMalformedVersions(t *testing.T) {
	for _, input := range []string{"", "1", "1.2", "1.2.x", "1.2.3.4", "-1.2.3", "1.2.3-", "latest"} {
		if _, err := Parse(input); err == nil {
			t.Errorf("Parse(%q) succeeded, want an error", input)
		}
	}
}

func TestCompareOrdersVersionsAndRanksPrereleaseBelowRelease(t *testing.T) {
	tests := []struct {
		left  string
		right string
		want  int
	}{
		{left: "1.0.0", right: "1.0.0", want: 0},
		{left: "1.0.1", right: "1.0.0", want: 1},
		{left: "1.1.0", right: "1.0.9", want: 1},
		{left: "2.0.0", right: "1.9.9", want: 1},
		{left: "1.0.0-rc.1", right: "1.0.0", want: -1},
		{left: "1.0.0-rc.1", right: "1.0.0-rc.2", want: -1},
		{left: "1.0.0-rc.2", right: "1.0.0-rc.10", want: -1},
		{left: "1.0.0-alpha", right: "1.0.0-beta", want: -1},
		{left: "1.0.0-rc.1", right: "1.0.0-rc.1", want: 0},
	}
	for _, test := range tests {
		left := mustParse(t, test.left)
		right := mustParse(t, test.right)
		if got := Compare(left, right); got != test.want {
			t.Errorf("Compare(%s, %s) = %d, want %d", test.left, test.right, got, test.want)
		}
		if got := Compare(right, left); got != -test.want {
			t.Errorf("Compare(%s, %s) = %d, want %d", test.right, test.left, got, -test.want)
		}
	}
}

func TestRangeContains(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		version string
		want    bool
	}{
		{name: "inside half open range", spec: ">=0.1.0 <1.0.0", version: "0.4.2", want: true},
		{name: "at inclusive lower bound", spec: ">=0.1.0 <1.0.0", version: "0.1.0", want: true},
		{name: "at exclusive upper bound", spec: ">=0.1.0 <1.0.0", version: "1.0.0", want: false},
		{name: "below lower bound", spec: ">=0.1.0 <1.0.0", version: "0.0.9", want: false},
		{name: "exact match", spec: "=0.1.0", version: "0.1.0", want: true},
		{name: "exact mismatch", spec: "=0.1.0", version: "0.1.1", want: false},
		{name: "bare version means exact", spec: "0.1.0", version: "0.1.0", want: true},
		{name: "greater than excludes equal", spec: ">0.1.0", version: "0.1.0", want: false},
		{name: "less than or equal includes equal", spec: "<=0.1.0", version: "0.1.0", want: true},
		{name: "prerelease shell inside range", spec: ">=0.1.0 <1.0.0", version: "0.2.0-dev", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			constraint, err := ParseRange(test.spec)
			if err != nil {
				t.Fatalf("ParseRange(%q) returned %v", test.spec, err)
			}
			if got := constraint.Contains(mustParse(t, test.version)); got != test.want {
				t.Fatalf("%q contains %q = %v, want %v", test.spec, test.version, got, test.want)
			}
		})
	}
}

func TestParseRangeRejectsMalformedSpecifications(t *testing.T) {
	for _, spec := range []string{"", "   ", ">=", "~>1.0.0", ">=1.0", "^1.0.0", ">=1.0.0 <", "1.0.0 || 2.0.0"} {
		if _, err := ParseRange(spec); err == nil {
			t.Errorf("ParseRange(%q) succeeded, want an error", spec)
		}
	}
}

func TestRangeStringRoundTripsTheOriginalSpecification(t *testing.T) {
	constraint, err := ParseRange(">=0.1.0 <1.0.0")
	if err != nil {
		t.Fatalf("ParseRange returned %v", err)
	}
	if got := constraint.String(); got != ">=0.1.0 <1.0.0" {
		t.Fatalf("String() = %q, want the original specification", got)
	}
}

func mustParse(t *testing.T, input string) Version {
	t.Helper()
	version, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(%q) returned %v", input, err)
	}
	return version
}
