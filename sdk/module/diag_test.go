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

package module_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/sdk/diag"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/result"
	"github.com/wso2/wso2-cli/sdk/testkit"
)

func loggingCommand() module.Command {
	return module.Command{
		Path: []string{"status"},
		Run: func(ctx context.Context, request module.Request) (result.Result, error) {
			request.Log.Debug("from the request", "token", "t-must-never-appear")
			diag.FromContext(ctx).Debug("from the context")
			return result.New("probe.status/v1").With("status", "Status", "operational"), nil
		},
	}
}

func runLogging(t *testing.T, mode module.OutputMode) string {
	t.Helper()
	var captured bytes.Buffer
	module.CaptureDiagnostics(t, &captured)
	outcome := testkit.Run(t.Context(), probeOptions(), []module.Command{loggingCommand()},
		testkit.Invocation{Command: []string{"status"}, OutputMode: mode})
	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("the invocation failed: %v %v", outcome.Err, outcome.Problem)
	}
	return captured.String()
}

func TestAHandlerLogsNothingUnlessTheShellAskedForDiagnostics(t *testing.T) {
	t.Setenv(diag.EnvVar, "")
	if logged := runLogging(t, module.OutputModeTable); logged != "" {
		t.Fatalf("a handler logged without WSO2_DEBUG:\n%s", logged)
	}
}

func TestAHandlerLogsThroughTheRequestAndItsContextUnderWSO2Debug(t *testing.T) {
	t.Setenv(diag.EnvVar, "1")
	logged := runLogging(t, module.OutputModeTable)
	for _, expected := range []string{"from the request", "from the context", diag.Redacted} {
		if !strings.Contains(logged, expected) {
			t.Errorf("the module log is missing %q:\n%s", expected, logged)
		}
	}
	if strings.Contains(logged, "t-must-never-appear") {
		t.Fatalf("the module log carries a token:\n%s", logged)
	}
}

func TestAHandlerLogsJSONWhenTheInvocationRendersJSON(t *testing.T) {
	t.Setenv(diag.EnvVar, "1")
	logged := runLogging(t, module.OutputModeJSON)
	lines := strings.Split(strings.TrimSpace(logged), "\n")
	if len(lines) != 2 {
		t.Fatalf("want two records, got:\n%s", logged)
	}
	for _, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("the record %q is not JSON: %v", line, err)
		}
	}
}
