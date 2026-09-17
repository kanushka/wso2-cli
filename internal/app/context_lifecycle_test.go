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

package app_test

import (
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/exit"
)

// TestContextDeleteOfAnUnknownNameIsRefused proves contextNotFound fires for
// delete, the same refusal wso2 context use gives for a name the document
// does not declare.
func TestContextDeleteOfAnUnknownNameIsRefused(t *testing.T) {
	shell, _, _ := newContextShell(t)
	localSetup(t, shell)
	code, _, errOut := run(t, shell, "context", "delete", "nosuch")
	if code != exit.Usage {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(errOut, "contexts.unknown_context") || !strings.Contains(errOut, "wso2 context list") {
		t.Errorf("stderr does not carry contextNotFound's refusal:\n%s", errOut)
	}
	if len(loadDocument(t, shell).Contexts) != 1 {
		t.Error("a refused delete changed the document")
	}
}

// TestContextRenameOfAnUnknownNameIsRefused is rename's half of the same
// refusal.
func TestContextRenameOfAnUnknownNameIsRefused(t *testing.T) {
	shell, _, _ := newContextShell(t)
	localSetup(t, shell)
	code, _, errOut := run(t, shell, "context", "rename", "nosuch", "renamed")
	if code != exit.Usage {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(errOut, "contexts.unknown_context") {
		t.Errorf("stderr does not carry contexts.unknown_context:\n%s", errOut)
	}
}

// TestContextRenameRefusesATakenName proves rename will not shadow an
// existing context.
func TestContextRenameRefusesATakenName(t *testing.T) {
	shell, _, _ := newContextShell(t)
	installLogin(t, shell, twoContextDocument())
	code, _, errOut := run(t, shell, "context", "rename", "acme", "beta")
	if code != exit.Usage {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(errOut, "contexts.name_exists") && !strings.Contains(errOut, "exists") {
		t.Errorf("stderr does not name the collision:\n%s", errOut)
	}
	document := loadDocument(t, shell)
	if _, found := document.Find("acme"); !found {
		t.Error("a refused rename lost the original context")
	}
}

// TestContextDeleteWithoutSelectionDoesNotClaimOne proves contextDeleted's
// WasSelected field is false for a context that was not selected, so the
// "no context is selected" next step is not printed for it.
func TestContextDeleteWithoutSelectionDoesNotClaimOne(t *testing.T) {
	shell, _, _ := newContextShell(t)
	seeded := twoContextDocument()
	seeded.DefaultContext = "acme"
	installLogin(t, shell, seeded)
	out := mustRun(t, shell, "context", "delete", "beta")
	if strings.Contains(out, "No context is selected now.") {
		t.Errorf("deleting an unselected context claimed the selection was cleared:\n%s", out)
	}
	if selected := loadDocument(t, shell).DefaultContext; selected != "acme" {
		t.Errorf("the selection changed to %q", selected)
	}
}
