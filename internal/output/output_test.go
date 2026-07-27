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
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/sdk/problem"
)

func TestTableRendersUppercaseHeadersAlignedWithoutTrailingSpaces(t *testing.T) {
	table := NewTable("name", "version")
	table.Append("reference", "v0.1.0")
	table.Append("longer-namespace", "v1.2.3")

	var buffer bytes.Buffer
	if err := table.Render(&buffer); err != nil {
		t.Fatalf("Render returned %v", err)
	}

	want := strings.Join([]string{
		"NAME               VERSION",
		"reference          v0.1.0",
		"longer-namespace   v1.2.3",
		"",
	}, "\n")
	if got := buffer.String(); got != want {
		t.Fatalf("Render produced:\n%q\nwant:\n%q", got, want)
	}
}

func TestFieldsAlignsLabels(t *testing.T) {
	var buffer bytes.Buffer
	if err := Fields(&buffer, [][2]string{{"WSO2 CLI", "v0.1.0"}, {"Protocol", "v1"}}); err != nil {
		t.Fatalf("Fields returned %v", err)
	}

	want := "WSO2 CLI   v0.1.0\nProtocol   v1\n"
	if got := buffer.String(); got != want {
		t.Fatalf("Fields produced %q, want %q", got, want)
	}
}

func TestDiagnosticAndProblemIncludeCodeAndRecovery(t *testing.T) {
	failure := problem.New(problem.CategoryModuleTrust, "modules.receipt_missing", "the module has no receipt").
		WithRecovery("Reinstall the module.")

	var diagnostic, terminal bytes.Buffer
	Diagnostic(&diagnostic, failure)
	Problem(&terminal, failure)

	wantDiagnostic := "warning: the module has no receipt (modules.receipt_missing)\n  Reinstall the module.\n"
	if got := diagnostic.String(); got != wantDiagnostic {
		t.Fatalf("Diagnostic produced %q, want %q", got, wantDiagnostic)
	}
	wantProblem := "error: the module has no receipt (modules.receipt_missing)\n  Reinstall the module.\n"
	if got := terminal.String(); got != wantProblem {
		t.Fatalf("Problem produced %q, want %q", got, wantProblem)
	}
}

func TestProblemOmitsAnAbsentRecoveryLine(t *testing.T) {
	var buffer bytes.Buffer
	Problem(&buffer, problem.New(problem.CategoryUsage, "shell.unknown_command", "unknown command"))

	if got := buffer.String(); got != "error: unknown command (shell.unknown_command)\n" {
		t.Fatalf("Problem produced %q", got)
	}
}
