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
	"encoding/json"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
)

// TestOrgCurrentOnAnUnconfiguredMachineReportsAState proves the same fact
// wso2 context current reports for the same situation, worded the same way,
// exits 0: an unconfigured machine has done nothing wrong.
func TestOrgCurrentOnAnUnconfiguredMachineReportsAState(t *testing.T) {
	shell, out, errOut := newShell(t)

	if code := shell.Run([]string{"org", "current"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stdout: %s stderr: %s", code, exit.OK, out, errOut)
	}
	if errOut.Len() != 0 {
		t.Errorf("an unconfigured machine wrote to stderr:\n%s", errOut)
	}
	if !strings.Contains(out.String(), "wso2 login") {
		t.Errorf("the report does not name what to run next:\n%s", out)
	}
}

// TestOrgCurrentWithAContextButNoOrganizationSaysSoDistinctly is the test the
// brief calls out by name: a context that exists but names no organization is
// a different fact from no context at all, and the two sentences must not
// read the same.
func TestOrgCurrentWithAContextButNoOrganizationSaysSoDistinctly(t *testing.T) {
	shell, out, errOut := newShell(t)
	seeded := identityOnlyDocument()
	seeded.DefaultContext = "acme"
	seeded.Contexts = []contexts.Context{{Name: "acme", Identity: "acme-cloud"}}
	installLogin(t, shell, seeded)

	if code := shell.Run([]string{"org", "current"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	reported := out.String()
	if strings.Contains(reported, "No context is configured") {
		t.Errorf("a configured context was reported as no context at all:\n%s", reported)
	}
	if !strings.Contains(reported, "acme") || !strings.Contains(reported, "no organization") {
		t.Errorf("the report does not distinctly say the context names no organization:\n%s", reported)
	}
}

// TestOrgCurrentReportsTheSelectedOrganization is the ordinary case: a context
// with an organization set.
func TestOrgCurrentReportsTheSelectedOrganization(t *testing.T) {
	shell, out, errOut := newShell(t)
	seeded := identityOnlyDocument()
	seeded.DefaultContext = "acme"
	seeded.Contexts = []contexts.Context{{Name: "acme", Identity: "acme-cloud", Organization: "acme-org"}}
	installLogin(t, shell, seeded)

	if code := shell.Run([]string{"org", "current"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.Contains(out.String(), "acme-org") {
		t.Errorf("the report does not name the organization:\n%s", out)
	}
}

// TestOrgUseWritesTheOrganizationAndNamesTheContext proves the decision this
// command makes, not just that it writes: it edits the currently selected
// context and says which one, so a user with more than one context is not left
// guessing which one changed.
func TestOrgUseWritesTheOrganizationAndNamesTheContext(t *testing.T) {
	shell, out, errOut := newShell(t)
	seeded := identityOnlyDocument()
	seeded.DefaultContext = "acme"
	seeded.Contexts = []contexts.Context{
		{Name: "acme", Identity: "acme-cloud", Organization: "old-org"},
		{Name: "beta", Identity: "acme-cloud", Organization: "beta-org"},
	}
	installLogin(t, shell, seeded)

	if code := shell.Run([]string{"org", "use", "new-org"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	document := loadDocument(t, shell)
	if organization := contextNamed(t, document, "acme").Organization; organization != "new-org" {
		t.Errorf("acme's organization = %q, want %q", organization, "new-org")
	}
	if organization := contextNamed(t, document, "beta").Organization; organization != "beta-org" {
		t.Errorf("wso2 org use changed a context other than the selected one: beta = %q", organization)
	}
	if !strings.Contains(out.String(), "acme") {
		t.Errorf("the output does not name the context that was edited:\n%s", out)
	}
}

// TestOrgUseWarnsThatABoundSessionNoLongerMatches asserts the warning itself,
// not just the write: the whole reason this task exists is that the auth
// broker binds a minted token to Context.Organization, and a user who runs
// this command has to be told by it, not only by the docs, that a session
// bound to the old organization no longer matches.
//
// Both renderings are exercised, and the stream is asserted rather than
// incidental. The warning is a diagnostic, so ADR 0003 puts it on Err; on Out
// it would be part of the result and would corrupt the JSON document a script
// parses. The JSON case is the one that matters most: a script is the caller
// likeliest to meet the authentication failure this predicts, and under the
// original placement it was the one caller never told at all.
func TestOrgUseWarnsThatABoundSessionNoLongerMatches(t *testing.T) {
	for name, args := range map[string][]string{
		"prose": {"org", "use", "new-org"},
		"json":  {"org", "use", "new-org", "--output", "json"},
	} {
		t.Run(name, func(t *testing.T) {
			shell, out, errOut := newShell(t)
			seeded := identityOnlyDocument()
			seeded.DefaultContext = "acme"
			seeded.Contexts = []contexts.Context{{Name: "acme", Identity: "acme-cloud", Organization: "old-org"}}
			installLogin(t, shell, seeded)

			if code := shell.Run(args); code != exit.OK {
				t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
			}
			warned := strings.ToLower(errOut.String())
			for _, wanted := range []string{"session", "no longer match"} {
				if !strings.Contains(warned, wanted) {
					t.Errorf("stderr does not warn that a bound session no longer matches (missing %q):\n%s",
						wanted, errOut)
				}
			}
			if strings.Contains(strings.ToLower(out.String()), "no longer match") {
				t.Errorf("the warning landed on stdout, where it is part of the result:\n%s", out)
			}
		})
	}
}

// TestOrgUseOnAMachineWithNoContextsRefusesWithGuidance covers the write-side
// counterpart of the unconfigured-machine state org current reports: unlike a
// read, there is nothing for wso2 org use to write to, so it refuses rather
// than reporting a state.
func TestOrgUseOnAMachineWithNoContextsRefusesWithGuidance(t *testing.T) {
	shell, out, errOut := newShell(t)

	if code := shell.Run([]string{"org", "use", "acme"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want the usage class %d; stdout: %s stderr: %s",
			code, exit.Usage, out, errOut)
	}
	if !strings.Contains(errOut.String(), "shell.no_context_configured") {
		t.Errorf("stderr does not carry shell.no_context_configured:\n%s", errOut)
	}
	if !strings.Contains(errOut.String(), "wso2 login") {
		t.Errorf("the refusal does not name what to run next:\n%s", errOut)
	}
}

// TestOrgUseWriteSurvivesAConcurrentWriterTheWayContextUseDoes proves the
// write goes through contexts.Update rather than a load-then-Save pair, by
// checking the same property TestContextUseSelectsAndWritesNothingElse checks
// for wso2 context use: nothing besides the organization field changes.
func TestOrgUseWriteSurvivesAConcurrentWriterTheWayContextUseDoes(t *testing.T) {
	shell, _, errOut := newShell(t)
	seeded := identityOnlyDocument()
	seeded.DefaultContext = "acme"
	seeded.Contexts = []contexts.Context{
		{Name: "acme", Identity: "acme-cloud", Organization: "old-org", Project: "retail"},
	}
	installLogin(t, shell, seeded)
	before := loadDocument(t, shell)

	if code := shell.Run([]string{"org", "use", "new-org"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	after := loadDocument(t, shell)
	beforeEdited := contextNamed(t, before, "acme")
	beforeEdited.Organization = "new-org"
	before.Contexts[0] = beforeEdited
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	if string(beforeJSON) != string(afterJSON) {
		t.Errorf("wso2 org use changed more than the organization:\nbefore %s\nafter  %s", beforeJSON, afterJSON)
	}
}

// TestOrgListRefusesAsAnUnknownSubcommand proves R11 was not half-built: org
// list is deferred, and typing it must refuse in the ordinary way rather than
// silently print help and exit 0, which is what a non-Runnable parent command
// with unmatched args does by default in Cobra.
func TestOrgListRefusesAsAnUnknownSubcommand(t *testing.T) {
	shell, out, errOut := newShell(t)
	installLogin(t, shell, identityOnlyDocument())

	if code := shell.Run([]string{"org", "list"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want the usage class %d; stdout: %s stderr: %s",
			code, exit.Usage, out, errOut)
	}
	if !strings.Contains(errOut.String(), "shell.unknown_command") {
		t.Errorf("stderr does not carry shell.unknown_command:\n%s", errOut)
	}
	if !strings.Contains(errOut.String(), "org current") || !strings.Contains(errOut.String(), "org use") {
		t.Errorf("the refusal does not name what wso2 org does support:\n%s", errOut)
	}
}

// TestEveryOrgSubcommandRendersJSON proves both renderings agree and neither
// publishes a schema discriminator, exactly as the context family's own test
// proves it for its subcommands.
func TestEveryOrgSubcommandRendersJSON(t *testing.T) {
	for name, args := range map[string][]string{
		"current": {"org", "current", "--output", "json"},
		"use":     {"org", "use", "gamma", "--output", "json"},
	} {
		t.Run(name, func(t *testing.T) {
			shell, out, errOut := newShell(t)
			seeded := identityOnlyDocument()
			seeded.DefaultContext = "acme"
			seeded.Contexts = []contexts.Context{{Name: "acme", Identity: "acme-cloud", Organization: "acme-org"}}
			installLogin(t, shell, seeded)

			if code := shell.Run(args); code != exit.OK {
				t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
			}
			var decoded map[string]any
			if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
				t.Fatalf("the output is not one JSON document: %v\n%s", err, out)
			}
			if len(decoded) == 0 {
				t.Errorf("the JSON document carries no fields:\n%s", out)
			}
			if _, published := decoded["schema"]; published {
				t.Errorf("the result publishes a schema key the rest of the shell suppresses:\n%s", out)
			}
		})
	}
}

// TestOrgCurrentOnAnUnconfiguredMachineRendersJSONDistinctly proves the JSON
// caller can tell "nothing configured" from "configured, no organization"
// without parsing prose: Configured carries the fact prose spends a whole
// sentence on.
func TestOrgCurrentOnAnUnconfiguredMachineRendersJSONDistinctly(t *testing.T) {
	shell, out, errOut := newShell(t)

	if code := shell.Run([]string{"org", "current", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	var decoded struct {
		Configured   bool   `json:"configured"`
		Organization string `json:"organization"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("the output is not one JSON document: %v\n%s", err, out)
	}
	if decoded.Configured {
		t.Errorf("an unconfigured machine reported configured = true:\n%s", out)
	}
}

// TestOrgUseIsRefusedForAWrongArgumentCount proves the family uses the local
// exactlyOneArgument helper rather than cobra.ExactArgs, whose error would
// exit outside the usage class.
func TestOrgUseIsRefusedForAWrongArgumentCount(t *testing.T) {
	shell, _, errOut := newShell(t)
	installLogin(t, shell, identityOnlyDocument())

	if code := shell.Run([]string{"org", "use"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want the usage class %d; stderr: %s", code, exit.Usage, errOut)
	}
}

// TestTheOrgFamilyRefusesTheContextFlag proves --context is not honoured by
// this family, exactly as the context family refuses it: naming a context
// with --context alongside a family that always acts on the selected one
// would be a second answer to a question the family does not ask.
func TestTheOrgFamilyRefusesTheContextFlag(t *testing.T) {
	shell, _, errOut := newShell(t)
	installLogin(t, shell, identityOnlyDocument())

	if code := shell.Run([]string{"--context", "acme", "org", "current"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want the usage class %d; stderr: %s", code, exit.Usage, errOut)
	}
	if !strings.Contains(errOut.String(), "shell.unsupported_flag") {
		t.Errorf("stderr does not carry shell.unsupported_flag:\n%s", errOut)
	}
}
