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
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/sdk/result"
)

func reportFixture() result.Result {
	return result.New("shell.logout/v1").
		With("context", "Context", "acme-cloud").
		With("revocation", "Revocation", "confirmed")
}

func TestReportTableReadsDownTheLeft(t *testing.T) {
	var rendered bytes.Buffer
	if err := output.Report(&rendered, output.ModeTable, reportFixture()); err != nil {
		t.Fatalf("report: %v", err)
	}
	lines := strings.Split(strings.TrimRight(rendered.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want one line per field, got %d: %q", len(lines), rendered.String())
	}
	if !strings.HasPrefix(lines[0], "Context") || !strings.HasSuffix(lines[0], "acme-cloud") {
		t.Fatalf("first line = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "Revocation") {
		t.Fatalf("second line = %q", lines[1])
	}
}

// The JSON keys follow the field order the command chose, and are the machine
// names rather than the labels.
func TestReportJSONKeepsFieldOrder(t *testing.T) {
	var rendered bytes.Buffer
	if err := output.Report(&rendered, output.ModeJSON, reportFixture()); err != nil {
		t.Fatalf("report: %v", err)
	}
	want := "{\n  \"context\": \"acme-cloud\",\n  \"revocation\": \"confirmed\"\n}\n"
	if rendered.String() != want {
		t.Fatalf("json = %q, want %q", rendered.String(), want)
	}
}
