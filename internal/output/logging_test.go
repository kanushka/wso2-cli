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

func TestAnAttachedFileGetsRecordsButNoDiagnosticsUntilEnabled(t *testing.T) {
	var file, stream bytes.Buffer
	logger := NewLogger()
	logger.AttachFile(&file, "invocation", "inv-1")
	logger.Record("a run started", "shell_version", "1.0.0")
	logger.Debug("a diagnostic")
	logger.FileDebug("a module line")
	if logger.Enabled() {
		t.Fatal("attaching a file turned diagnostics on")
	}
	if strings.Contains(file.String(), "a diagnostic") || strings.Contains(file.String(), "a module line") {
		t.Fatalf("a diagnostic reached the file without being asked for:\n%s", &file)
	}
	var record map[string]any
	if err := json.Unmarshal(file.Bytes(), &record); err != nil {
		t.Fatalf("the file record is not one JSON line %q: %v", &file, err)
	}
	if record["msg"] != "a run started" || record["invocation"] != "inv-1" || record["shell_version"] != "1.0.0" {
		t.Fatalf("the file record is %v", record)
	}

	logger.Enable(&stream, ModeTable)
	logger.Debug("a diagnostic", "token", "t-must-never-appear")
	logger.FileDebug("a module line")
	logger.Record("a run ended")
	for _, expected := range []string{"a diagnostic", "a module line", "a run ended", "[redacted]"} {
		if !strings.Contains(file.String(), expected) {
			t.Errorf("the file is missing %q:\n%s", expected, &file)
		}
	}
	if strings.Contains(file.String(), "t-must-never-appear") {
		t.Fatalf("the file carries a token:\n%s", &file)
	}
	if !strings.Contains(stream.String(), "a diagnostic") || !strings.Contains(stream.String(), "a run ended") {
		t.Fatalf("the stream is missing what was logged:\n%s", &stream)
	}
	if strings.Contains(stream.String(), "a module line") || strings.Contains(stream.String(), "inv-1") {
		t.Fatalf("a file-only record or attribute reached the stream:\n%s", &stream)
	}
}

func TestRecordWithoutAFileOrStreamWritesNothing(t *testing.T) {
	NewLogger().Record("a run started")
	var absent *Logger
	absent.Record("a run started")
	absent.FileDebug("a module line")
	absent.AttachFile(&bytes.Buffer{})
}
