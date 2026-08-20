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

package acceptance_test

import (
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/statusservice"
)

// whoamiFields are the semantic fields the reference whoami result carries, in
// the order both renderings must follow.
var whoamiFields = []string{"organization", "audiences", "scopes", "invocation", "boundTo", "expiresAt"}

// The whoami command is the reference module's second command. Where status
// proves that brokered access works, whoami proves what that access is: these
// runs read the claims back from the audience that verified them, which is the
// only account of the hand-off neither the shell nor the module can fake.

func TestReferenceWhoamiReportsTheBrokeredAccess(t *testing.T) {
	shell := buildShell(t)
	stateRoot := deploy(t, statusservice.Options{}).stateRoot

	stdout, stderr := runShell(t, shell, stateRoot, "reference", "whoami")

	// The audience and scope are the ones the module declared and the shell
	// granted. Reading them back from the service proves the token the module
	// presented actually carried them.
	for _, want := range []string{"AUDIENCES", "SCOPES", "reference-status", "reference:status:read"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the table does not contain %q:\n%s", want, stdout)
		}
	}
	if stderr != "" {
		t.Errorf("a successful command wrote diagnostics:\n%s", stderr)
	}
}

func TestReferenceWhoamiRendersDeterministicJSON(t *testing.T) {
	shell := buildShell(t)
	stateRoot := deploy(t, statusservice.Options{}).stateRoot

	stdout, stderr := runShell(t, shell, stateRoot, "reference", "whoami", "--output", "json")

	decoded := decodeStatusJSON(t, stdout)
	for _, field := range whoamiFields {
		if decoded[field] == "" {
			t.Errorf("the JSON result has no %q field:\n%s", field, stdout)
		}
	}
	if got := keyOrder(t, stdout); !equalStrings(got, whoamiFields) {
		t.Errorf("JSON keys are in order %v, want the module's declared order %v", got, whoamiFields)
	}
	if stderr != "" {
		t.Errorf("a successful command wrote diagnostics:\n%s", stderr)
	}
}

func TestWhoamiReportsAccessBoundToThisInvocation(t *testing.T) {
	// The token carries the invocation it was minted for, and the service
	// refuses one presented under a different invocation. Reporting both the
	// invocation the shell ran and the one the token names shows that binding
	// holding rather than asserting it.
	shell := buildShell(t)
	stateRoot := deploy(t, statusservice.Options{}).stateRoot

	first := decodeStatusJSON(t, mustJSON(t, shell, stateRoot))
	if first["invocation"] != first["boundTo"] {
		t.Errorf("access was bound to invocation %q while the command ran as %q",
			first["boundTo"], first["invocation"])
	}

	// A second run is a second invocation, so it must be granted different
	// access. Access that survived between invocations would be access the
	// binding does not actually constrain.
	second := decodeStatusJSON(t, mustJSON(t, shell, stateRoot))
	if second["invocation"] == first["invocation"] {
		t.Errorf("two invocations reported the same invocation ID %q", first["invocation"])
	}
	if second["boundTo"] == first["boundTo"] {
		t.Errorf("two invocations were granted access bound to the same invocation %q", first["boundTo"])
	}

	// The organization and the granted permissions are policy, not per-run
	// state, so they must not move between invocations.
	for _, field := range []string{"organization", "audiences", "scopes"} {
		if first[field] != second[field] {
			t.Errorf("%s changed between invocations: %q then %q", field, first[field], second[field])
		}
	}
}

func TestWhoamiNeverReportsTheAccessMaterialItself(t *testing.T) {
	// whoami exists to describe a token, which is exactly the command most
	// likely to leak one. A claim is not secret; the material carrying it is.
	shell := buildShell(t)
	stateRoot := deploy(t, statusservice.Options{}).stateRoot

	stdout, stderr := runShell(t, shell, stateRoot, "reference", "whoami")

	for _, forbidden := range []string{"Bearer", "Authorization", "eyJ"} {
		if strings.Contains(stdout, forbidden) || strings.Contains(stderr, forbidden) {
			t.Errorf("output contains %q, which suggests access material reached the user:\n%s\n%s",
				forbidden, stdout, stderr)
		}
	}
}

// mustJSON runs "wso2 reference whoami --output json" and fails on diagnostics.
func mustJSON(t *testing.T, shell, stateRoot string) string {
	t.Helper()
	stdout, stderr := runShell(t, shell, stateRoot, "reference", "whoami", "--output", "json")
	if stderr != "" {
		t.Fatalf("a successful command wrote diagnostics:\n%s", stderr)
	}
	return stdout
}
