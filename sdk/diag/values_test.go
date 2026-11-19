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

package diag

import (
	"bytes"
	"encoding/base64"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// fixtureJWT is shaped like an ID token: a JSON header, a claims segment that
// carries an email, and a signature.
func fixtureJWT() string {
	segment := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	return segment(`{"alg":"RS256","typ":"JWT"}`) + "." +
		segment(`{"sub":"u-1","email":"person@example.test","name":"A Person"}`) + "." +
		segment("signature-bytes")
}

func logged(t *testing.T, attributes ...any) string {
	t.Helper()
	var captured bytes.Buffer
	New(&captured, true).Debug("a diagnostic", attributes...)
	return captured.String()
}

func TestSecretQueryValuesInAURLAreMaskedWhateverTheKey(t *testing.T) {
	for _, parameter := range []string{
		"id_token_hint", "code", "code_verifier", "access_token", "refresh_token", "id_token",
		"token", "client_secret", "device_code", "assertion", "subject_token",
	} {
		t.Run(parameter, func(t *testing.T) {
			const secret = "s3cret-must-never-appear"
			target := "https://issuer.example.test/oidc/logout?client_id=cli&" + parameter + "=" + secret + "&state=keep"
			out := logged(t, "url", target)
			if strings.Contains(out, secret) {
				t.Fatalf("%s survived in the query:\n%s", parameter, out)
			}
			for _, kept := range []string{"https://issuer.example.test/oidc/logout", "client_id=cli", "state=keep", parameter + "="} {
				if !strings.Contains(out, kept) {
					t.Errorf("the record lost %q:\n%s", kept, out)
				}
			}
		})
	}
}

func TestSecretFragmentValuesAreMasked(t *testing.T) {
	const secret = "s3cret-must-never-appear"
	out := logged(t, "redirect", "http://127.0.0.1:8080/callback#access_token="+secret+"&token_type=Bearer")
	if strings.Contains(out, secret) {
		t.Fatalf("the fragment token survived:\n%s", out)
	}
	if !strings.Contains(out, "token_type=Bearer") {
		t.Fatalf("an ordinary fragment parameter was lost:\n%s", out)
	}
}

func TestAURLInsideLongerTextIsMaskedToo(t *testing.T) {
	const secret = "s3cret-must-never-appear"
	out := logged(t, "error", errors.New(`Get "https://api.example.test/apis?apiKey=`+secret+`": dial tcp: refused`))
	if strings.Contains(out, secret) {
		t.Fatalf("a secret inside an error's URL survived:\n%s", out)
	}
	if !strings.Contains(out, "dial tcp: refused") {
		t.Fatalf("the error text was lost:\n%s", out)
	}
}

func TestAJWTIsMaskedUnderAnyKey(t *testing.T) {
	token := fixtureJWT()
	for name, attribute := range map[string]any{
		"a plain value":       slog.String("hint", token),
		"inside a group":      slog.Group("session", slog.String("value", token)),
		"inside longer text":  slog.String("note", "the provider returned "+token+" unexpectedly"),
		"as a query value":    slog.String("url", "https://issuer.example.test/logout?hint="+token),
		"inside an error":     slog.Any("error", errors.New("bad token "+token)),
		"under an innocent k": slog.String("issuer_detail", token),
	} {
		t.Run(name, func(t *testing.T) {
			out := logged(t, attribute)
			for _, part := range strings.Split(token, ".") {
				if strings.Contains(out, part) {
					t.Fatalf("a JWT segment survived:\n%s", out)
				}
			}
			if !strings.Contains(out, Redacted) {
				t.Fatalf("the JWT was dropped rather than masked:\n%s", out)
			}
		})
	}
}

func TestOrdinaryValuesThatLookDottedAreLeftAlone(t *testing.T) {
	for _, value := range []string{
		"1.2.3", "v0.5.0", "api.example.test", "a.b.c",
		"https://issuer.example.test/oauth2/token?client_id=cli&scope=openid",
	} {
		if out := logged(t, "value", value); !strings.Contains(out, value) {
			t.Errorf("%q was altered:\n%s", value, out)
		}
	}
}
