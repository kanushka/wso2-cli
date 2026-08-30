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

package output

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// TestALoggerThatWasNeverEnabledWritesNothing pins what the absence of
// --verbose means. A call site logs unconditionally, so the silence has to come
// from the logger and not from every caller remembering to ask.
func TestALoggerThatWasNeverEnabledWritesNothing(t *testing.T) {
	logger := NewLogger()
	// It holds no stream at all until Enable gives it one, so this call has
	// nowhere to write even if it wanted to. What is asserted is that it
	// survives being called anyway, because every call site calls it anyway.
	logger.Debug("something happened", "issuer", "https://issuer.example.test")
	if logger.Enabled() {
		t.Fatal("a logger that was never enabled reports itself enabled")
	}
	// A nil logger is what a Shell built directly in a test holds, and it must
	// not panic.
	var absent *Logger
	absent.Debug("something happened")
	if absent.Enabled() {
		t.Fatal("a nil logger reports itself enabled")
	}
}

func TestAnEnabledLoggerWritesToTheGivenStream(t *testing.T) {
	var captured bytes.Buffer
	logger := NewLogger()
	logger.Enable(&captured, ModeTable)
	logger.Debug("resolved a product namespace", "namespace", "reference")

	if !strings.Contains(captured.String(), "resolved a product namespace") {
		t.Fatalf("the message is missing from:\n%s", &captured)
	}
	if !strings.Contains(captured.String(), "namespace=reference") {
		t.Fatalf("the attribute is missing from:\n%s", &captured)
	}
}

// TestJSONOutputModeLogsJSON pins the pairing: a caller reading results with a
// program gets diagnostics it can read the same way.
func TestJSONOutputModeLogsJSON(t *testing.T) {
	var captured bytes.Buffer
	logger := NewLogger()
	logger.Enable(&captured, ModeJSON)
	logger.Debug("starting a login", "issuer", "https://issuer.example.test")

	var record map[string]any
	if err := json.Unmarshal(captured.Bytes(), &record); err != nil {
		t.Fatalf("the JSON handler wrote unparseable output %q: %v", &captured, err)
	}
	if record["msg"] != "starting a login" {
		t.Fatalf("the record does not carry the message: %v", record)
	}
	if record["issuer"] != "https://issuer.example.test" {
		t.Fatalf("the record does not carry the attribute: %v", record)
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
		logger := NewLogger()
		logger.Enable(&captured, ModeJSON)
		logger.Debug("a diagnostic", key, secret)
		if strings.Contains(captured.String(), secret) {
			t.Errorf("the value logged under %q survived redaction: %s", key, &captured)
		}
		if !strings.Contains(captured.String(), redactedValue) {
			t.Errorf("the value logged under %q was dropped rather than masked: %s", key, &captured)
		}
	}
}

// TestRedactionReachesEveryWayAnAttributeCanArrive is why redaction wraps the
// handler instead of riding on slog.HandlerOptions.ReplaceAttr. slog never
// calls ReplaceAttr for a group attribute itself, only for the leaves inside
// it, so a secret carried as slog.Group("token", ...) or nested under an
// innocent group name would have reached the stream verbatim.
func TestRedactionReachesEveryWayAnAttributeCanArrive(t *testing.T) {
	const secret = "rt-must-never-appear"
	cases := map[string]func(*Logger){
		"a plain attribute": func(logger *Logger) {
			logger.Debug("a diagnostic", "refresh_token", secret)
		},
		"an attribute inside an innocent group": func(logger *Logger) {
			logger.Debug("a diagnostic", slog.Group("session", "refresh_token", secret))
		},
		"a group named for the secret itself": func(logger *Logger) {
			logger.Debug("a diagnostic", slog.Group("token", "value", secret))
		},
		"a group nested two deep": func(logger *Logger) {
			logger.Debug("a diagnostic",
				slog.Group("outer", slog.Group("inner", "access_token", secret)))
		},
		"an attribute preformatted through WithAttrs": func(logger *Logger) {
			logger.delegate = logger.delegate.With("refresh_token", secret)
			logger.Debug("a diagnostic")
		},
		"an attribute under a group opened by WithGroup": func(logger *Logger) {
			logger.delegate = logger.delegate.WithGroup("session").With("refresh_token", secret)
			logger.Debug("a diagnostic")
		},
		// The group name is the only thing naming a secret here, so nothing
		// key-based below it would catch the leaf on its own.
		"a leaf under a group named for the secret": func(logger *Logger) {
			logger.delegate = logger.delegate.WithGroup("client_secret")
			logger.Debug("a diagnostic", "value", secret)
		},
	}
	for name, log := range cases {
		t.Run(name, func(t *testing.T) {
			var captured bytes.Buffer
			logger := NewLogger()
			logger.Enable(&captured, ModeJSON)
			log(logger)
			if strings.Contains(captured.String(), secret) {
				t.Fatalf("the secret escaped redaction: %s", &captured)
			}
			if !strings.Contains(captured.String(), redactedValue) {
				t.Fatalf("the secret was dropped rather than masked: %s", &captured)
			}
		})
	}
}

// TestAnUnlistedKeyIsLeftAlone states the limit of key-based redaction out
// loud. Redaction here is only as complete as the denylist, which is why the
// shell also greps real captured output for real fixture token values; see
// TestVerboseLoggingNeverLeaksTokenMaterial in internal/app.
func TestAnUnlistedKeyIsLeftAlone(t *testing.T) {
	var captured bytes.Buffer
	logger := NewLogger()
	logger.Enable(&captured, ModeTable)
	logger.Debug("a diagnostic", "issuer", "https://issuer.example.test")
	if !strings.Contains(captured.String(), "https://issuer.example.test") {
		t.Fatalf("an ordinary attribute was redacted: %s", &captured)
	}
}
