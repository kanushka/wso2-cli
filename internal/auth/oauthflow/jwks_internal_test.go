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

package oauthflow

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestWithoutCertificatesTouchesOnlyKeySets proves the stripper is inert
// everywhere except the one document it exists for. Every fetch a login makes
// passes through it — discovery, the token exchange, the key set — so a
// rewrite it applied too eagerly would corrupt a response nobody asked it to
// read.
func TestWithoutCertificatesTouchesOnlyKeySets(t *testing.T) {
	for name, body := range map[string]string{
		"a token response":      `{"access_token":"a.b.c","expires_in":3600,"scope":"read"}`,
		"a discovery document":  `{"issuer":"https://example.test","jwks_uri":"https://example.test/jwks"}`,
		"a key set without x5c": `{"keys":[{"kty":"RSA","n":"abc","e":"AQAB"}]}`,
		"not JSON at all":       `<html>gateway timeout</html>`,
		"a JSON array":          `[1,2,3]`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, changed := withoutCertificates([]byte(body)); changed {
				t.Fatalf("the stripper rewrote %s:\n%s", name, body)
			}
		})
	}
}

// TestWithoutCertificatesKeepsWhatTheKeyNeeds proves the stripper only
// discards a certificate when the key beside it is already complete. A key
// that genuinely depended on its certificate must keep it and fail loudly:
// this removes a spurious failure, it does not hide an unreadable key.
func TestWithoutCertificatesKeepsWhatTheKeyNeeds(t *testing.T) {
	for name, body := range map[string]string{
		"an RSA key missing its exponent": `{"keys":[{"kty":"RSA","n":"abc","x5c":["MII"]}]}`,
		"an EC key missing its curve":     `{"keys":[{"kty":"EC","x":"a","y":"b","x5c":["MII"]}]}`,
		"a key type nothing knows":        `{"keys":[{"kty":"XYZ","x5c":["MII"]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, changed := withoutCertificates([]byte(body)); changed {
				t.Fatalf("the stripper discarded a certificate the key may need:\n%s", body)
			}
		})
	}
}

// TestWithoutCertificatesDropsTheChainAndItsThumbprints proves the whole
// certificate group leaves together. go-jose checks x5t and x5t#S256 against
// the chain in x5c, so a thumbprint left behind after the chain has gone is a
// check with nothing to check against.
func TestWithoutCertificatesDropsTheChainAndItsThumbprints(t *testing.T) {
	body := `{"keys":[{"kty":"RSA","n":"abc","e":"AQAB","use":"sig","kid":"k1",` +
		`"x5c":["MII"],"x5t":"t1","x5t#S256":"t256"}],"other":"preserved"}`

	rewritten, changed := withoutCertificates([]byte(body))
	if !changed {
		t.Fatal("the stripper left a certificate on a key that did not need one")
	}
	for _, gone := range []string{"x5c", "x5t", "x5t#S256", "MII"} {
		if strings.Contains(string(rewritten), gone) {
			t.Fatalf("%q survived the strip:\n%s", gone, rewritten)
		}
	}

	var document struct {
		Keys  []map[string]json.RawMessage `json:"keys"`
		Other string                       `json:"other"`
	}
	if err := json.Unmarshal(rewritten, &document); err != nil {
		t.Fatalf("the stripper produced something that is not a key set: %v", err)
	}
	if document.Other != "preserved" {
		t.Fatalf("a member outside keys was lost: %s", rewritten)
	}
	if len(document.Keys) != 1 {
		t.Fatalf("the key set lost its key: %s", rewritten)
	}
	for _, needed := range []string{"kty", "n", "e", "use", "kid"} {
		if _, carried := document.Keys[0][needed]; !carried {
			t.Fatalf("the strip took %q with it: %s", needed, rewritten)
		}
	}
}
