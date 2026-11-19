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
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestRequestedReadsTheVariableAsASwitch(t *testing.T) {
	for value, want := range map[string]bool{
		"": false, "0": false, "false": false, "FALSE": false, " false ": false,
		"1": true, "true": true, "yes": true, "anything": true,
	} {
		if got := Requested(value); got != want {
			t.Errorf("Requested(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestDiscardWritesNothingAndAcceptsEveryCall(t *testing.T) {
	log := Discard()
	log.Debug("a diagnostic", "token", "t")
	if log.Enabled(t.Context(), slog.LevelDebug) {
		t.Fatal("a discarding logger reports itself enabled")
	}
}

func TestNewFollowsTheRequestedRendering(t *testing.T) {
	var text, structured bytes.Buffer
	New(&text, false).Debug("a diagnostic", "namespace", "reference")
	if !strings.Contains(text.String(), "namespace=reference") {
		t.Fatalf("the text record is missing its attribute: %s", &text)
	}
	New(&structured, true).Debug("a diagnostic", "namespace", "reference")
	var record map[string]any
	if err := json.Unmarshal(structured.Bytes(), &record); err != nil {
		t.Fatalf("the JSON record is unparseable %q: %v", &structured, err)
	}
	if record["namespace"] != "reference" {
		t.Fatalf("the JSON record is missing its attribute: %v", record)
	}
}

// TestSensitiveAttributesAreMasked walks the spellings a key can arrive in.
// The point of normalizing is that accessToken and access_token are one entry
// in the denylist rather than two chances to have missed one.
func TestSensitiveAttributesAreMasked(t *testing.T) {
	const secret = "rt-must-never-appear"
	for _, key := range []string{
		"access_token", "accessToken", "Access-Token", "refresh_token",
		"id_token", "token", "client_secret", "code", "device_code",
		"user_code", "code_verifier", "authorization", "credential",
		"source_credential", "password", "secret", "api_key",
	} {
		var captured bytes.Buffer
		New(&captured, true).Debug("a diagnostic", key, secret)
		if strings.Contains(captured.String(), secret) {
			t.Errorf("the value logged under %q survived redaction: %s", key, &captured)
		}
		if !strings.Contains(captured.String(), Redacted) {
			t.Errorf("the value logged under %q was dropped rather than masked: %s", key, &captured)
		}
	}
}

// TestRedactionReachesEveryWayAnAttributeCanArrive is why redaction wraps the
// handler instead of riding on slog.HandlerOptions.ReplaceAttr.
func TestRedactionReachesEveryWayAnAttributeCanArrive(t *testing.T) {
	const secret = "rt-must-never-appear"
	cases := map[string]func(*slog.Logger){
		"a plain attribute": func(log *slog.Logger) {
			log.Debug("a diagnostic", "refresh_token", secret)
		},
		"an attribute inside an innocent group": func(log *slog.Logger) {
			log.Debug("a diagnostic", slog.Group("session", "refresh_token", secret))
		},
		"a group named for the secret itself": func(log *slog.Logger) {
			log.Debug("a diagnostic", slog.Group("token", "value", secret))
		},
		"a group nested two deep": func(log *slog.Logger) {
			log.Debug("a diagnostic", slog.Group("outer", slog.Group("inner", "access_token", secret)))
		},
		"an attribute preformatted through WithAttrs": func(log *slog.Logger) {
			log.With("refresh_token", secret).Debug("a diagnostic")
		},
		"an attribute under a group opened by WithGroup": func(log *slog.Logger) {
			log.WithGroup("session").With("refresh_token", secret).Debug("a diagnostic")
		},
		"a leaf under a group named for the secret": func(log *slog.Logger) {
			log.WithGroup("client_secret").Debug("a diagnostic", "value", secret)
		},
	}
	for name, write := range cases {
		t.Run(name, func(t *testing.T) {
			var captured bytes.Buffer
			write(New(&captured, true))
			if strings.Contains(captured.String(), secret) {
				t.Fatalf("the secret escaped redaction: %s", &captured)
			}
			if !strings.Contains(captured.String(), Redacted) {
				t.Fatalf("the secret was dropped rather than masked: %s", &captured)
			}
		})
	}
}

// TestAnUnlistedKeyIsLeftAlone states the limit of key-based redaction out
// loud: it is only as complete as the denylist.
func TestAnUnlistedKeyIsLeftAlone(t *testing.T) {
	var captured bytes.Buffer
	New(&captured, false).Debug("a diagnostic", "issuer", "https://issuer.example.test")
	if !strings.Contains(captured.String(), "https://issuer.example.test") {
		t.Fatalf("an ordinary attribute was redacted: %s", &captured)
	}
}
