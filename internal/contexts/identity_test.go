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

package contexts_test

import (
	"testing"

	"github.com/wso2/wso2-cli/internal/contexts"
)

// TestIdentityTypeForIssuer pins the deployment-kind derivation: an issuer on
// a WSO2-operated cloud host is cloud, and every other issuer is onprem. The
// look-alike cases matter most — a host that merely contains "asgardeo.io"
// somewhere is not WSO2's cloud and must not be recorded as it.
func TestIdentityTypeForIssuer(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		issuer string
		want   string
	}{
		{"an Asgardeo tenant issuer",
			"https://api.asgardeo.io/t/acme/oauth2/token", contexts.TypeCloud},
		{"the asgardeo.io apex itself",
			"https://asgardeo.io", contexts.TypeCloud},
		{"a deeper asgardeo.io host",
			"https://api.eu.asgardeo.io/t/acme/oauth2/token", contexts.TypeCloud},
		{"upper-case letters in the host",
			"https://API.ASGARDEO.IO/t/acme/oauth2/token", contexts.TypeCloud},
		{"a self-hosted issuer",
			"https://idp.customer.example", contexts.TypeOnprem},
		{"a host that ends in asgardeo.io without the label boundary",
			"https://notasgardeo.io", contexts.TypeOnprem},
		{"a host that carries asgardeo.io as a prefix",
			"https://api.asgardeo.io.evil.example", contexts.TypeOnprem},
		{"an http issuer off the cloud",
			"http://localhost:9443", contexts.TypeOnprem},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := contexts.IdentityTypeForIssuer(testCase.issuer); got != testCase.want {
				t.Errorf("IdentityTypeForIssuer(%q) = %q, want %q",
					testCase.issuer, got, testCase.want)
			}
		})
	}
}
