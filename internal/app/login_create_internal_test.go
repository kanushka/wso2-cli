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

package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/output"
)

// TestPlanLoginDerivesTheIdentityTypeFromTheIssuer pins the identity a login
// writes to the deployment it authenticated against: an Asgardeo issuer
// records cloud, and any other issuer records onprem. It calls planLogin
// directly because the package-level login tests run against a localhost fake
// issuer, which can only ever exercise the onprem half.
func TestPlanLoginDerivesTheIdentityTypeFromTheIssuer(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		issuer string
		want   string
	}{
		{"an Asgardeo issuer records cloud",
			"https://api.asgardeo.io/t/acme/oauth2/token", contexts.TypeCloud},
		{"a self-hosted issuer records onprem",
			"https://idp.customer.example", contexts.TypeOnprem},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			selected, err := planLogin(contexts.Document{}, "customer", testCase.issuer, "wso2-cli")
			if err != nil {
				t.Fatalf("planLogin: %v", err)
			}
			if selected.Identity.Type != testCase.want {
				t.Errorf("planned identity type = %q, want %q",
					selected.Identity.Type, testCase.want)
			}
		})
	}
}

// TestTheProductlessLoginReportMatchesTheDeploymentKind pins the post-login
// message to the identity it describes: only a self-hosted identity is told
// its deployment is not discoverable, because only of one is that true, and
// both kinds are pointed at the command that records a product. It drives
// reportLoginWrite directly for the reason the planLogin test states — the
// fake issuer lives on localhost, so a cloud login cannot be run end to end.
func TestTheProductlessLoginReportMatchesTheDeploymentKind(t *testing.T) {
	for _, testCase := range []struct {
		name         string
		identityType string
		wants        []string
		refuses      []string
	}{
		{
			name:         "a cloud identity is not called self-hosted",
			identityType: contexts.TypeCloud,
			wants: []string{
				"No products are configured for this identity yet.",
				"discovered automatically",
				"wso2 identity add-product customer",
			},
			refuses: []string{"self-hosted"},
		},
		{
			name:         "a self-hosted identity keeps the discoverability explanation",
			identityType: contexts.TypeOnprem,
			wants: []string{
				"No products are configured for this identity.",
				"A self-hosted deployment is not\ndiscoverable",
				"wso2 identity add-product customer",
			},
			refuses: []string{"discovered automatically"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			out := &bytes.Buffer{}
			shell := Shell{Streams: output.Streams{Out: out, Err: &bytes.Buffer{}}}
			identity := contexts.Identity{Name: "customer", Type: testCase.identityType}

			err := shell.reportLoginWrite(loginWrite{
				Identity: "customer", Context: "customer",
				CreatedIdentity: true, CreatedContext: true,
			}, identity)
			if err != nil {
				t.Fatalf("reportLoginWrite: %v", err)
			}
			for _, want := range testCase.wants {
				if !strings.Contains(out.String(), want) {
					t.Errorf("the report is missing %q in:\n%s", want, out)
				}
			}
			for _, refused := range testCase.refuses {
				if strings.Contains(out.String(), refused) {
					t.Errorf("the report wrongly says %q in:\n%s", refused, out)
				}
			}
		})
	}
}
