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

package smoke_test

import (
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/test/smoke"
)

// The messages below are copied from the shell's own refusal sites in
// internal/auth/narrowing.go and internal/auth/source_browser.go. They are the
// whole input to the classification, so a message the shell reworded and this
// file did not is a verdict silently recorded wrong — which is the failure this
// test exists to catch. Keep them byte-identical to the format strings there.
const (
	messageOpaqueToken = `the deployment issued access for the "reference" module in a form ` +
		`the shell cannot check against what the module asked for`
	messageNoScopeStated = `the deployment did not state which permissions it issued for the ` +
		`"reference" module, so the shell cannot prove they are the ones it asked for`
	messageAudienceUnbound = `the deployment issued access that is not bound to the ` +
		`"reference-status" audience the "reference" module needs`
	messageRefusedToNarrow = `the deployment refused to narrow this session to the permissions ` +
		`the "reference" module asked for`
)

// scopeMismatch renders the refusal narrowing.go raises when the issued
// permissions are not the ones requested, in that site's exact wording.
func scopeMismatch(requested, issued string) string {
	return `the "reference" module asked for the permissions ` + requested +
		` and the deployment issued ` + issued
}

func TestNarrowingVerdictReadsEachRefusal(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		code      string
		message   string
		requested []string
		want      string
	}{
		{
			name: "granted",
			code: "", message: "",
			requested: []string{"reference:status:read"},
			want:      smoke.VerdictHonored,
		},
		{
			name: "issuer answered invalid_scope",
			code: "auth.narrowing_unavailable", message: messageRefusedToNarrow,
			requested: []string{"reference:status:read"},
			want:      smoke.VerdictRejected,
		},
		{
			name: "deployment issued a wider product scope set",
			code: "auth.narrowing_unavailable",
			message: scopeMismatch("reference:status:read",
				"reference:status:read, reference:status:write"),
			requested: []string{"reference:status:read"},
			want:      smoke.VerdictIgnored,
		},
		{
			name: "deployment issued an unrelated scope set",
			code: "auth.narrowing_unavailable",
			message: scopeMismatch("reference:status:read",
				"reference:status:write"),
			requested: []string{"reference:status:read"},
			want:      smoke.VerdictIgnored,
		},
		{
			name: "opaque access token",
			code: "auth.narrowing_unavailable", message: messageOpaqueToken,
			requested: []string{"reference:status:read"},
			want:      smoke.VerdictInconclusiveOpaque,
		},
		{
			name: "deployment stated no scope",
			code: "auth.narrowing_unavailable", message: messageNoScopeStated,
			requested: []string{"reference:status:read"},
			want:      smoke.VerdictInconclusiveUnstated,
		},
		{
			name: "token not bound to the audience",
			code: "auth.narrowing_unavailable", message: messageAudienceUnbound,
			requested: []string{"reference:status:read"},
			want:      smoke.VerdictInconclusiveAudience,
		},
		{
			name: "some other refusal entirely",
			code: "auth.login_required", message: "the stored session was not accepted",
			requested: []string{"reference:status:read"},
			want:      "inconclusive (auth.login_required)",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := smoke.NarrowingVerdict(testCase.code, testCase.message, testCase.requested)
			if got != testCase.want {
				t.Errorf("NarrowingVerdict = %q, want %q", got, testCase.want)
			}
		})
	}
}

// The experiment's own login asks for openid and offline_access alongside the
// product permissions, because oauthflow always does. A deployment that narrows
// the product permissions correctly but keeps those two in its answer has
// honored the request — and the shell still refuses, because it cannot hand a
// module a token carrying permissions it did not ask for.
//
// Reporting that as "ignored" would put a false finding about the deployment
// into a research document, which is the one outcome this whole experiment must
// not produce.
func TestNarrowingVerdictSeparatesProtocolScopesFromAWiderGrant(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		issued string
	}{
		{name: "openid retained", issued: "openid, reference:status:read"},
		{name: "offline_access retained", issued: "offline_access, reference:status:read"},
		{name: "both retained", issued: "offline_access, openid, reference:status:read"},
		{name: "profile and email retained", issued: "email, openid, profile, reference:status:read"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := smoke.NarrowingVerdict("auth.narrowing_unavailable",
				scopeMismatch("reference:status:read", testCase.issued),
				[]string{"reference:status:read"})
			if got != smoke.VerdictHonoredWithProtocolScopes {
				t.Errorf("NarrowingVerdict = %q, want %q", got, smoke.VerdictHonoredWithProtocolScopes)
			}
		})
	}
}

// A protocol scope alongside a product scope that was NOT asked for is still a
// wider grant, and must not be excused by the protocol-scope rule.
func TestNarrowingVerdictDoesNotExcuseAWiderGrantCarryingProtocolScopes(t *testing.T) {
	got := smoke.NarrowingVerdict("auth.narrowing_unavailable",
		scopeMismatch("reference:status:read",
			"openid, reference:status:read, reference:status:write"),
		[]string{"reference:status:read"})
	if got != smoke.VerdictIgnored {
		t.Errorf("NarrowingVerdict = %q, want %q", got, smoke.VerdictIgnored)
	}
}

// A deployment that narrowed away a permission that WAS asked for has not
// honored the request either.
func TestNarrowingVerdictDoesNotCallAShortGrantHonored(t *testing.T) {
	got := smoke.NarrowingVerdict("auth.narrowing_unavailable",
		scopeMismatch("reference:status:read, reference:status:write",
			"openid, reference:status:read"),
		[]string{"reference:status:read", "reference:status:write"})
	if got != smoke.VerdictIgnored {
		t.Errorf("NarrowingVerdict = %q, want %q", got, smoke.VerdictIgnored)
	}
}

// If the shell reworded the mismatch refusal, the issued permissions can no
// longer be read out of it. Guessing at that point would be worse than saying
// so, because the guess would be recorded as a finding.
func TestNarrowingVerdictRefusesToGuessFromAnUnreadableMismatch(t *testing.T) {
	got := smoke.NarrowingVerdict("auth.narrowing_unavailable",
		"the module asked for some permissions and got some others", nil)
	if !strings.HasPrefix(got, "inconclusive") {
		t.Errorf("NarrowingVerdict = %q, want an inconclusive verdict", got)
	}
}
