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
	"errors"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/sdk/problem"
)

func TestIdentityNameForIssuer(t *testing.T) {
	for _, testCase := range []struct {
		issuer string
		want   string
	}{
		{"https://idp.customer.example", "idp-customer-example"},
		{"https://idp.customer.example/oauth2/token", "idp-customer-example"},
		{"https://IDP.Customer.Example", "idp-customer-example"},
		{"https://idp.customer.example:8443", "idp-customer-example"},
		{"https://api.asgardeo.io/t/acme/oauth2/token", "api-asgardeo-io"},
	} {
		got, err := contexts.IdentityNameForIssuer(testCase.issuer)
		if err != nil {
			t.Errorf("IdentityNameForIssuer(%q): %v", testCase.issuer, err)
			continue
		}
		if got != testCase.want {
			t.Errorf("IdentityNameForIssuer(%q) = %q, want %q", testCase.issuer, got, testCase.want)
		}
	}
}

// TestIdentityNameForIssuerProducesANameTheDocumentAccepts is the property the
// derivation exists to hold: whatever it returns, the document validates it the
// same way it validates a hand-written name.
func TestIdentityNameForIssuerProducesANameTheDocumentAccepts(t *testing.T) {
	for _, issuer := range []string{
		"https://idp.customer.example",
		"https://api.asgardeo.io/t/acme/oauth2/token",
		"https://" + strings.Repeat("a", 40) + "." + strings.Repeat("b", 40) + ".example",
	} {
		name, err := contexts.IdentityNameForIssuer(issuer)
		if err != nil {
			t.Errorf("IdentityNameForIssuer(%q): %v", issuer, err)
			continue
		}
		if !contexts.ValidName(name) {
			t.Errorf("IdentityNameForIssuer(%q) = %q, which the document would refuse", issuer, name)
		}
	}
}

func TestIdentityNameForIssuerRefusesAHostThatCannotBecomeAName(t *testing.T) {
	// The derived name has to satisfy the same pattern a hand-written one does,
	// because the document validates it either way. A host that cannot produce
	// one is refused here, with --context named as the way to supply a name
	// directly, rather than producing a document the shell then refuses to read.
	for _, issuer := range []string{
		"https://127.0.0.1:9443",
		"https://[::1]:9443",
		"https://9front.example",
		"https://",
		"not a url at all",
	} {
		_, err := contexts.IdentityNameForIssuer(issuer)
		if err == nil {
			t.Errorf("IdentityNameForIssuer(%q) produced a name from a host that cannot make one", issuer)
			continue
		}
		var typed problem.Problem
		if !errors.As(err, &typed) {
			t.Errorf("IdentityNameForIssuer(%q) failed untyped: %v", issuer, err)
			continue
		}
		if typed.Code != "contexts.identity_name_underivable" {
			t.Errorf("code = %q, want contexts.identity_name_underivable", typed.Code)
		}
		if !strings.Contains(typed.Recovery, "--context") {
			t.Errorf("the recovery does not name --context: %q", typed.Recovery)
		}
	}
}
