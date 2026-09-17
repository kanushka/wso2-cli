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

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
)

func TestApplyRefusesWithNoFileFlag(t *testing.T) {
	shell, _, _ := newContextShell(t)
	code, _, errOut := run(t, shell, "context", "apply")
	if code != exit.Usage {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(errOut, "shell.missing_required_flag") {
		t.Errorf("stderr does not carry shell.missing_required_flag:\n%s", errOut)
	}
}

func TestApplyRefusesAnUnreadableFile(t *testing.T) {
	shell, _, _ := newContextShell(t)
	code, _, errOut := run(t, shell, "context", "apply", "-f", "/no/such/path/team.json")
	if code != exit.Usage {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(errOut, "shell.input_unreadable") {
		t.Errorf("stderr does not carry shell.input_unreadable:\n%s", errOut)
	}
}

// TestApplyReadsTheFileFromStandardInput proves -f - reads through the
// shell's own reader seam, so a test can drive it without a real pipe.
func TestApplyReadsTheFileFromStandardInput(t *testing.T) {
	shell, _, _ := newContextShell(t)
	shell.Reader = strings.NewReader(teamFile)
	mustRun(t, shell, "context", "apply", "-f", "-")
	if _, found := loadDocument(t, shell).Find("local"); !found {
		t.Error("apply -f - did not write the context read from standard input")
	}
}

func TestApplyRefusesAnUnknownUseTarget(t *testing.T) {
	shell, _, _ := newContextShell(t)
	code, _, errOut := run(t, shell, "context", "apply", "-f", writeFile(t, teamFile), "--use", "nosuch")
	if code != exit.Usage {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(errOut, "shell.invalid_argument") || !strings.Contains(errOut, "--use nosuch") {
		t.Errorf("stderr does not name the unknown --use target:\n%s", errOut)
	}
	if document := loadDocument(t, shell); len(document.Contexts) != 0 {
		t.Error("a refused --use still wrote contexts")
	}
}

// TestApplyRefusesMalformedInputShapes covers the decode-time refusals
// beyond the ones context_test.go already tables: invalid JSON, a second
// document appended after the first, an unsupported schema version, a file
// declaring no contexts at all, and an invalid context name.
func TestApplyRefusesMalformedInputShapes(t *testing.T) {
	cases := map[string]string{
		"invalid JSON":       `{not json`,
		"two JSON documents": `{"contexts": [{"name": "a", "login": {"issuer": "https://i.example", "clientId": "c"}}]} {}`,
		"an unsupported schema version": `{"schemaVersion": 99, "contexts": [{"name": "a",
		  "login": {"issuer": "https://i.example", "clientId": "c"}}]}`,
		"no contexts at all": `{"contexts": []}`,
		"an invalid context name": `{"contexts": [{"name": "Not Valid",
		  "login": {"issuer": "https://i.example", "clientId": "c"}}]}`,
		"an invalid product namespace": `{"contexts": [{"name": "a",
		  "login": {"issuer": "https://i.example", "clientId": "c"},
		  "products": {"Not Valid": {"url": "https://p.example"}}}]}`,
		"a product with no url": `{"contexts": [{"name": "a",
		  "login": {"issuer": "https://i.example", "clientId": "c"},
		  "products": {"p": {"url": ""}}}]}`,
		"a product with an invalid url": `{"contexts": [{"name": "a",
		  "login": {"issuer": "https://i.example", "clientId": "c"},
		  "products": {"p": {"url": "not-a-url"}}}]}`,
		"a gateway with an invalid url": `{"contexts": [{"name": "a",
		  "login": {"issuer": "https://i.example", "clientId": "c"},
		  "products": {"p": {"url": "https://p.example", "gateway": {"url": "not-a-url"}}}}]}`,
		"a login issuer carrying a credential": `{"contexts": [{"name": "a",
		  "login": {"issuer": "https://u:s3cr3t@i.example", "clientId": "c"}}]}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			shell, _, _ := newContextShell(t)
			code, _, errOut := run(t, shell, "context", "apply", "-f", writeFile(t, content))
			if code != exit.Usage {
				t.Fatalf("exit %d: %s", code, errOut)
			}
			if _, err := contexts.Load(shell.StateRoot); err != nil {
				t.Fatalf("Load: %v", err)
			}
			if document := loadDocument(t, shell); len(document.Contexts) != 0 {
				t.Errorf("a refused apply wrote contexts: %+v", document.Contexts)
			}
		})
	}
}

// TestApplyDryRunWithAMissingProductReportsWithoutInstalling proves the
// dry-run path that cannot resolve a product's defaults because it is not
// installed yet: it names the product to install and marks every context it
// touches "resolved after install", without installing or writing anything.
func TestApplyDryRunWithAMissingProductReportsWithoutInstalling(t *testing.T) {
	shell, _, _ := newContextShell(t)
	const file = `{"contexts": [{"name": "corp", "login": {"issuer": "https://idp.corp.example", "clientId": "cli"},
	  "products": {"orders": {"url": "https://orders.example"}}}]}`
	out := mustRun(t, shell, "context", "apply", "-f", writeFile(t, file), "--dry-run")
	if !strings.Contains(out, "Would install: orders") {
		t.Errorf("the plan does not name the product to install:\n%s", out)
	}
	if !strings.Contains(out, "resolved after install") {
		t.Errorf("the plan does not mark the context pending resolution:\n%s", out)
	}
	if _, err := contexts.Load(shell.StateRoot); err != nil {
		t.Fatal(err)
	}
	if document := loadDocument(t, shell); len(document.Contexts) != 0 {
		t.Error("a dry run installed or wrote something")
	}
}

// TestApplyReportsAVersionMismatchAndHowUpdateProductsChangesTheNote proves
// both of reportApply's mismatch-note branches: left alone by default, and
// "updated to the pinned version" when --update-products is also passed.
// Both runs stay --dry-run so neither ever reaches the network to install
// anything.
func TestApplyReportsAVersionMismatchAndHowUpdateProductsChangesTheNote(t *testing.T) {
	shell, _, _ := newContextShell(t)
	const file = `{"contexts": [{"name": "corp", "login": {"issuer": "https://idp.corp.example", "clientId": "cli"},
	  "products": {"reference": {"url": "https://ref.example", "version": "9.9.9"}}}]}`

	left := mustRun(t, shell, "context", "apply", "-f", writeFile(t, file), "--dry-run")
	if !strings.Contains(left, "reference (installed v0.1.0, file pins v9.9.9)") ||
		!strings.Contains(left, "pass --update-products to install the pinned version") {
		t.Errorf("the default mismatch note is wrong:\n%s", left)
	}

	updated := mustRun(t, shell, "context", "apply", "-f", writeFile(t, file), "--dry-run", "--update-products")
	if !strings.Contains(updated, "updated to the pinned version") {
		t.Errorf("--update-products does not change the note:\n%s", updated)
	}
}

// TestApplyReportsAddedAndRemovedMembersNotJustChangedOnes exercises
// flatten's other two branches through contextChanges: a member that was
// unset and is now set ("set to"), and one that was set and is now removed
// ("removed"), alongside the "->" branch context_test.go already covers.
func TestApplyReportsAddedAndRemovedMembersNotJustChangedOnes(t *testing.T) {
	shell, _, _ := newContextShell(t)
	const withoutAPI = `{"contexts": [{"name": "local", "login": {"product": "iam"},
	  "products": {"iam": {"url": "` + thunderURL + `"}}}]}`
	mustRun(t, shell, "context", "apply", "-f", writeFile(t, withoutAPI))

	const withAPI = `{"contexts": [{"name": "local", "login": {"product": "iam"},
	  "products": {"iam": {"url": "` + thunderURL + `"}, "api": {"url": "` + apiURL + `", "gateway": {"url": "` + apiGatewayURL + `"}}}}]}`
	added := mustRun(t, shell, "context", "apply", "-f", writeFile(t, withAPI), "--dry-run")
	if !strings.Contains(added, "set to") {
		t.Errorf("adding a product did not report a \"set to\" change:\n%s", added)
	}
	mustRun(t, shell, "context", "apply", "-f", writeFile(t, withAPI))

	removed := mustRun(t, shell, "context", "apply", "-f", writeFile(t, withoutAPI), "--dry-run")
	if !strings.Contains(removed, "removed (was") {
		t.Errorf("dropping a product did not report a \"removed\" change:\n%s", removed)
	}
}

func TestContextExportOfAKnownNameFiltersToIt(t *testing.T) {
	shell, _, _ := newContextShell(t)
	installLogin(t, shell, twoContextDocument())
	out := mustRun(t, shell, "context", "export", "beta")
	if !strings.Contains(out, `"name": "beta"`) || strings.Contains(out, `"name": "acme"`) {
		t.Errorf("export <name> did not filter to that context alone:\n%s", out)
	}
}

func TestContextExportOfAnUnknownNameIsRefused(t *testing.T) {
	shell, _, _ := newContextShell(t)
	installLogin(t, shell, oneContextDocument())
	code, _, errOut := run(t, shell, "context", "export", "nosuch")
	if code != exit.Usage {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(errOut, "contexts.unknown_context") {
		t.Errorf("stderr does not carry contexts.unknown_context:\n%s", errOut)
	}
}

// TestContextShowFlagsAGrantDrift is productDrift's other branch (the scope
// drift is covered by TestContextShowReportsAnUpgradeAndDrift in
// context_test.go): a direct product whose recorded grant no longer matches
// the installed descriptor's.
func TestContextShowFlagsAGrantDrift(t *testing.T) {
	shell, _, _ := newContextShell(t)
	mustRun(t, shell, "context", "create", "local", "--issuer", "https://idp.corp.example", "--client-id", "cli", "--use")
	mustRun(t, shell, "context", "product", "add", "apim", "--url", apimURL, "--client-id", "apim-cli")
	document := loadDocument(t, shell)
	local := contextNamed(t, document, "local")
	apim := local.Products["apim"]
	if apim.Grant == nil {
		t.Fatalf("apim = %+v, want a grant recorded", apim)
	}
	apim.Grant = &contexts.Grant{Kind: contexts.GrantJWTBearer, Issuer: apim.Grant.Issuer, ClientID: apim.Grant.ClientID}
	local.Products["apim"] = apim
	if err := contexts.Save(shell.StateRoot, document.Put(local)); err != nil {
		t.Fatal(err)
	}
	out := mustRun(t, shell, "context", "show")
	if !strings.Contains(out, `records the grant "jwt-bearer"`) {
		t.Errorf("wso2 context show does not flag the grant drift:\n%s", out)
	}
}
