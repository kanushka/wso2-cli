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

package auth

import (
	"encoding/json"
	"strconv"
	"testing"
)

// TestTokenResponseUnmarshalNeverFailsOnRefreshTokenExpiresIn is the
// blocker-fix proof: before optionalSeconds existed, a string-shaped
// refresh_token_expires_in failed json.Unmarshal for the WHOLE tokenResponse,
// which requestToken (tokenrequest.go) reads as errNoAccessToken — a total
// refresh failure against an issuer whose only sin was spelling a
// non-standard, optional member as text. Every case here must decode without
// error; only the resulting seconds value distinguishes a stated lifetime
// from an unstated one.
func TestTokenResponseUnmarshalNeverFailsOnRefreshTokenExpiresIn(t *testing.T) {
	for name, testCase := range map[string]struct {
		body string
		want int64
	}{
		"a JSON number, the standard shape": {
			body: `{"access_token":"at","refresh_token_expires_in":86400}`,
			want: 86400,
		},
		"a JSON string, the non-standard shape that used to break the exchange": {
			body: `{"access_token":"at","refresh_token_expires_in":"86400"}`,
			want: 86400,
		},
		"absent entirely, the common case per R7 (#112)": {
			body: `{"access_token":"at"}`,
			want: 0,
		},
		"a JSON bool": {
			body: `{"access_token":"at","refresh_token_expires_in":true}`,
			want: 0,
		},
		"a JSON object": {
			body: `{"access_token":"at","refresh_token_expires_in":{"seconds":86400}}`,
			want: 0,
		},
		"a JSON array": {
			body: `{"access_token":"at","refresh_token_expires_in":[86400]}`,
			want: 0,
		},
		"a negative number, not a positive lifetime": {
			body: `{"access_token":"at","refresh_token_expires_in":-1}`,
			want: 0,
		},
		"zero, not a positive lifetime": {
			body: `{"access_token":"at","refresh_token_expires_in":0}`,
			want: 0,
		},
		"a non-numeric string": {
			body: `{"access_token":"at","refresh_token_expires_in":"soon"}`,
			want: 0,
		},
		"a JSON null": {
			body: `{"access_token":"at","refresh_token_expires_in":null}`,
			want: 0,
		},
	} {
		t.Run(name, func(t *testing.T) {
			var decoded tokenResponse
			if err := json.Unmarshal([]byte(testCase.body), &decoded); err != nil {
				t.Fatalf("json.Unmarshal returned %v, want no error: a field that exists only so "+
					"whoami can print something must never be able to fail a token exchange", err)
			}
			if decoded.AccessToken != "at" {
				t.Errorf("AccessToken = %q, want %q: the rest of the response must decode normally too",
					decoded.AccessToken, "at")
			}
			if int64(decoded.RefreshTokenExpiresIn) != testCase.want {
				t.Errorf("RefreshTokenExpiresIn = %d, want %d",
					int64(decoded.RefreshTokenExpiresIn), testCase.want)
			}
		})
	}
}

// TestTokenResponseUnmarshalRejectsMalformedJSONAsBeforeAtTheDocumentLevel
// proves the leniency added for refresh_token_expires_in is scoped to that
// member: a document that is not valid JSON at all still fails to unmarshal,
// exactly as it did before optionalSeconds existed. Leniency at one field must
// not quietly swallow a broken response.
func TestTokenResponseUnmarshalRejectsMalformedJSONAsBeforeAtTheDocumentLevel(t *testing.T) {
	var decoded tokenResponse
	if err := json.Unmarshal([]byte(`{not valid json`), &decoded); err == nil {
		t.Fatal("json.Unmarshal accepted malformed JSON")
	}
}

// TestStringShapedExpiresInDoesNotFailTheExchange pins that a decorative
// lifetime field cannot discard a credential that arrived intact. expires_in
// is a number by RFC 6749 §5.1, but stating it as a string is a widespread
// interoperability wart, and against such an issuer this shell could not
// authenticate at all: the whole Unmarshal failed and the caller saw
// errNoAccessToken, which names nothing about the real cause. #131.
func TestStringShapedExpiresInDoesNotFailTheExchange(t *testing.T) {
	for _, shape := range []string{`"3600"`, `3600`} {
		body := []byte(`{"access_token":"a","refresh_token":"r","expires_in":` + shape + `}`)

		var issued tokenResponse
		if err := json.Unmarshal(body, &issued); err != nil {
			t.Fatalf("expires_in %s failed the exchange: %v", shape, err)
		}
		if issued.AccessToken != "a" {
			t.Errorf("expires_in %s: access_token = %q, want the token that arrived in the same response",
				shape, issued.AccessToken)
		}
		if issued.ExpiresIn != 3600 {
			t.Errorf("expires_in %s decoded to %d, want 3600", shape, issued.ExpiresIn)
		}
	}
}

// TestUnreadableExpiresInLeavesTheExpiryUnstated pins the other half of the
// rule: a shape that cannot be read as a positive number of seconds must
// neither fail the exchange nor invent a lifetime. expiry() then falls back to
// the JWT exp it already reads. #131.
func TestUnreadableExpiresInLeavesTheExpiryUnstated(t *testing.T) {
	for _, shape := range []string{`{"a":1}`, `true`, `"not a number"`, `-1`, `0`} {
		body := []byte(`{"access_token":"a","expires_in":` + shape + `}`)

		var issued tokenResponse
		if err := json.Unmarshal(body, &issued); err != nil {
			t.Fatalf("expires_in %s failed the exchange: %v", shape, err)
		}
		if issued.ExpiresIn != 0 {
			t.Errorf("expires_in %s decoded to %d, want 0 for not stated", shape, issued.ExpiresIn)
		}
		if issued.AccessToken != "a" {
			t.Errorf("expires_in %s discarded the access token", shape)
		}
	}
}

// TestLifetimeSecondsAcceptsBothWireShapes pins the shared helper login.go
// and narrowing.go both call, directly, against the same already-decoded
// values *oauth2.Token.Extra and encoding/json's any-decoding both produce:
// float64 for a JSON number, string for a JSON string.
func TestLifetimeSecondsAcceptsBothWireShapes(t *testing.T) {
	for name, testCase := range map[string]struct {
		value     any
		wantOK    bool
		wantValue int64
	}{
		"a float64, what a JSON number decodes to via encoding/json": {
			value: float64(3600), wantOK: true, wantValue: 3600,
		},
		"a numeric string": {
			value: "3600", wantOK: true, wantValue: 3600,
		},
		"a numeric string with surrounding whitespace": {
			value: "  3600 ", wantOK: true, wantValue: 3600,
		},
		"nil, what Extra returns for an absent key": {
			value: nil, wantOK: false,
		},
		"a bool": {
			value: true, wantOK: false,
		},
		"a non-numeric string": {
			value: "soon", wantOK: false,
		},
		"a negative float64": {
			value: float64(-1), wantOK: false,
		},
		"a zero float64": {
			value: float64(0), wantOK: false,
		},
		"an int, a shape neither call site actually produces but must not panic on": {
			value: int(3600), wantOK: false,
		},
		"the largest lifetime a time.Duration can carry": {
			value: float64(maxLifetimeSeconds), wantOK: true, wantValue: maxLifetimeSeconds,
		},
		"the largest lifetime, stated as a string": {
			value: strconv.FormatInt(maxLifetimeSeconds, 10), wantOK: true, wantValue: maxLifetimeSeconds,
		},
		"one second past what a time.Duration can carry": {
			// Converted, this wraps to a negative duration and dates the
			// session in the past, so it is refused rather than reported.
			value: float64(maxLifetimeSeconds + 1), wantOK: false,
		},
		"one second past the limit, stated as a string": {
			value: strconv.FormatInt(maxLifetimeSeconds+1, 10), wantOK: false,
		},
		"a float64 far larger than an int64 can hold": {
			// The bound is checked before the conversion, because converting
			// this at all is implementation-defined in Go.
			value: float64(1e30), wantOK: false,
		},
		"a string of digits too long for an int64": {
			value: "99999999999999999999", wantOK: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			seconds, ok := LifetimeSeconds(testCase.value)
			if ok != testCase.wantOK {
				t.Fatalf("ok = %v, want %v", ok, testCase.wantOK)
			}
			if ok && seconds != testCase.wantValue {
				t.Errorf("seconds = %d, want %d", seconds, testCase.wantValue)
			}
		})
	}
}
