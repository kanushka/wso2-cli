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

package output_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/sdk/result"
)

// statusResult is the reference status shape both renderers must agree on.
func statusResult() result.Result {
	return result.New("reference.status/v1").
		With("organization", "Organization", "acme").
		With("service", "Service", "reference").
		With("status", "Status", "operational").
		With("checkedAt", "Checked at", "2026-07-27T09:30:00Z")
}

func render(t *testing.T, mode output.Mode, produced result.Result) string {
	t.Helper()
	var out bytes.Buffer
	if err := output.Result(&out, mode, produced); err != nil {
		t.Fatalf("rendering %s output: %v", mode, err)
	}
	return out.String()
}

func TestTableOutputLabelsEveryFieldAndReportsItsValue(t *testing.T) {
	rendered := render(t, output.ModeTable, statusResult())

	lines := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("the table has %d lines, want a header and one row:\n%s", len(lines), rendered)
	}
	for _, want := range []string{"ORGANIZATION", "SERVICE", "STATUS", "CHECKED AT"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("the header does not contain %q:\n%s", want, rendered)
		}
	}
	for _, want := range []string{"acme", "reference", "operational", "2026-07-27T09:30:00Z"} {
		if !strings.Contains(lines[1], want) {
			t.Errorf("the row does not contain %q:\n%s", want, rendered)
		}
	}
}

func TestJSONOutputKeepsTheModulesFieldOrder(t *testing.T) {
	// A Go map would sort the keys and lose the order the module chose, so
	// the document is assembled field by field.
	rendered := render(t, output.ModeJSON, statusResult())

	decoder := json.NewDecoder(strings.NewReader(rendered))
	if _, err := decoder.Token(); err != nil {
		t.Fatalf("the document does not open an object: %v\n%s", err, rendered)
	}
	var keys []string
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			t.Fatalf("reading a key: %v", err)
		}
		keys = append(keys, key.(string))
		if _, err := decoder.Token(); err != nil {
			t.Fatalf("reading a value: %v", err)
		}
	}

	want := []string{"organization", "service", "status", "checkedAt"}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Errorf("JSON keys are %v, want %v", keys, want)
	}
}

func TestJSONOutputIsAValidDocumentEndingInOneNewline(t *testing.T) {
	rendered := render(t, output.ModeJSON, statusResult())

	var decoded map[string]string
	if err := json.Unmarshal([]byte(rendered), &decoded); err != nil {
		t.Fatalf("the document is not valid JSON: %v\n%s", err, rendered)
	}
	if decoded["status"] != "operational" {
		t.Errorf("status is %q, want %q", decoded["status"], "operational")
	}
	if !strings.HasSuffix(rendered, "}\n") || strings.HasSuffix(rendered, "}\n\n") {
		t.Errorf("the document does not end in exactly one newline:\n%q", rendered)
	}
}

func TestJSONOutputEscapesValuesRatherThanBreakingTheDocument(t *testing.T) {
	// Field names and values come from a module, so a value containing a quote
	// or a newline must not be able to produce an unparseable document.
	hostile := result.New("reference.status/v1").
		With("status", "Status", "\"operational\"\nand more").
		With("note\"key", "Note", "tab\there")

	rendered := render(t, output.ModeJSON, hostile)

	var decoded map[string]string
	if err := json.Unmarshal([]byte(rendered), &decoded); err != nil {
		t.Fatalf("a module value broke the document: %v\n%s", err, rendered)
	}
	if decoded["status"] != "\"operational\"\nand more" {
		t.Errorf("status round-tripped as %q, want the module's value", decoded["status"])
	}
	if decoded["note\"key"] != "tab\there" {
		t.Errorf("the escaped key round-tripped as %q", decoded["note\"key"])
	}
}

func TestBothRenderingsReportTheSameValues(t *testing.T) {
	produced := statusResult()
	table := render(t, output.ModeTable, produced)

	var decoded map[string]string
	if err := json.Unmarshal([]byte(render(t, output.ModeJSON, produced)), &decoded); err != nil {
		t.Fatalf("the JSON document is invalid: %v", err)
	}
	for _, field := range produced.Fields {
		if decoded[field.Name] != field.Value {
			t.Errorf("JSON reports %s as %q, want %q", field.Name, decoded[field.Name], field.Value)
		}
		if !strings.Contains(table, field.Value) {
			t.Errorf("the table does not report the %s value %q:\n%s", field.Name, field.Value, table)
		}
	}
}

func TestAFieldWithoutALabelIsStillNamedInTheTable(t *testing.T) {
	rendered := render(t, output.ModeTable, result.New("probe/v1").With("checkedAt", "", "now"))

	if !strings.Contains(rendered, "CHECKEDAT") {
		t.Errorf("an unlabelled field is not named in the table:\n%s", rendered)
	}
}

func TestParseModeAcceptsOnlyTheRenderingsTheShellSupports(t *testing.T) {
	for _, mode := range output.Modes() {
		if parsed, ok := output.ParseMode(string(mode)); !ok || parsed != mode {
			t.Errorf("ParseMode(%q) = %q, %v; want the mode itself", mode, parsed, ok)
		}
	}
	for _, unsupported := range []string{"yaml", "", "TABLE", "json ", "text"} {
		if _, ok := output.ParseMode(unsupported); ok {
			t.Errorf("ParseMode accepted the unsupported mode %q", unsupported)
		}
	}
}
