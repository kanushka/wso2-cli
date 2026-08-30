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
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
)

// identityOnlyDocument is the state a machine is in after wso2 login has
// created an identity and before any context names it. It is the starting point
// for most of these tests because it is the state wso2 context create exists to
// move a user out of.
func identityOnlyDocument() contexts.Document {
	return contexts.Document{
		SchemaVersion: contexts.SchemaVersion,
		Identities: []contexts.Identity{{
			Name: "acme-cloud",
			Type: "cloud",
			Auth: contexts.IdentityAuth{
				Kind:          contexts.KindOAuthBrowser,
				Issuer:        "https://idp.example",
				ClientID:      "wso2-cli",
				CredentialRef: "acme-cloud",
			},
		}},
	}
}

// loadDocument reads what a command actually wrote, through the shell's own
// reader rather than through a second parser that could disagree with it.
func loadDocument(t *testing.T, shell app.Shell) contexts.Document {
	t.Helper()
	document, err := contexts.Load(shell.StateRoot)
	if err != nil {
		t.Fatalf("contexts.Load: %v", err)
	}
	return document
}

// contextNamed reports the named context, or fails the test.
func contextNamed(t *testing.T, document contexts.Document, name string) contexts.Context {
	t.Helper()
	for _, candidate := range document.Contexts {
		if candidate.Name == name {
			return candidate
		}
	}
	t.Fatalf("the document declares no context named %q: %+v", name, document.Contexts)
	return contexts.Context{}
}

func TestContextCreateWritesASchemaVersionTwoContext(t *testing.T) {
	shell, out, errOut := newShell(t)
	installLogin(t, shell, identityOnlyDocument())

	code := shell.Run([]string{"context", "create", "acme",
		"--identity", "acme-cloud", "--organization", "acme", "--project", "retail"})
	if code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stdout: %s stderr: %s", code, exit.OK, out, errOut)
	}
	document := loadDocument(t, shell)
	if document.SchemaVersion != contexts.SchemaVersion {
		t.Errorf("schemaVersion = %d, want %d", document.SchemaVersion, contexts.SchemaVersion)
	}
	created := contextNamed(t, document, "acme")
	if created.Identity != "acme-cloud" || created.Organization != "acme" || created.Project != "retail" {
		t.Errorf("created context = %+v, want the identity, organization and project that were named", created)
	}
}

func TestContextCreateIsRefusedWhenTheNameIsTaken(t *testing.T) {
	shell, out, errOut := newShell(t)
	seeded := identityOnlyDocument()
	seeded.DefaultContext = "acme"
	seeded.Contexts = []contexts.Context{{Name: "acme", Identity: "acme-cloud", Organization: "first"}}
	installLogin(t, shell, seeded)

	code := shell.Run([]string{"context", "create", "acme", "--identity", "acme-cloud",
		"--organization", "second"})
	if code != exit.Usage {
		t.Fatalf("exit code = %d, want the usage class %d; stdout: %s stderr: %s",
			code, exit.Usage, out, errOut)
	}
	if !strings.Contains(errOut.String(), "contexts.context_exists") {
		t.Errorf("stderr does not carry contexts.context_exists:\n%s", errOut)
	}
	if organization := contextNamed(t, loadDocument(t, shell), "acme").Organization; organization != "first" {
		t.Errorf("the existing context was replaced: organization = %q, want %q", organization, "first")
	}
}

func TestContextCreateIsRefusedWhenTheIdentityDoesNotExist(t *testing.T) {
	shell, out, errOut := newShell(t)
	installLogin(t, shell, identityOnlyDocument())

	code := shell.Run([]string{"context", "create", "acme", "--identity", "nosuch"})
	if code != exit.Usage {
		t.Fatalf("exit code = %d, want the usage class %d; stdout: %s stderr: %s",
			code, exit.Usage, out, errOut)
	}
	if !strings.Contains(errOut.String(), "contexts.unknown_identity") {
		t.Errorf("stderr does not carry contexts.unknown_identity:\n%s", errOut)
	}
	// D3: login is the only thing that creates an identity, so it is the only
	// answer a recovery can honestly give.
	if !strings.Contains(errOut.String(), "wso2 login") {
		t.Errorf("the recovery does not name wso2 login, which is what creates an identity:\n%s", errOut)
	}
	if len(loadDocument(t, shell).Contexts) != 0 {
		t.Error("a refused create wrote a context")
	}
}

func TestContextCreateNamesTheFlagWhenNoIdentityIsGiven(t *testing.T) {
	shell, _, errOut := newShell(t)
	installLogin(t, shell, identityOnlyDocument())

	if code := shell.Run([]string{"context", "create", "acme"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want the usage class %d; stderr: %s", code, exit.Usage, errOut)
	}
	if !strings.Contains(errOut.String(), "--identity") {
		t.Errorf("the refusal does not name --identity:\n%s", errOut)
	}
}

func TestTheFirstContextCreatedBecomesTheDefault(t *testing.T) {
	shell, out, errOut := newShell(t)
	installLogin(t, shell, identityOnlyDocument())

	if code := shell.Run([]string{"context", "create", "acme", "--identity", "acme-cloud"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if selected := loadDocument(t, shell).DefaultContext; selected != "acme" {
		t.Errorf("defaultContext = %q, want %q", selected, "acme")
	}
	// A user who is not told that the first create also selected the context
	// has to run wso2 context current to find out.
	if !strings.Contains(out.String(), "selected") {
		t.Errorf("the output does not say the new context was selected:\n%s", out)
	}
}

func TestASecondContextCreatedDoesNotStealTheDefault(t *testing.T) {
	shell, _, errOut := newShell(t)
	installLogin(t, shell, identityOnlyDocument())

	if code := shell.Run([]string{"context", "create", "acme", "--identity", "acme-cloud"}); code != exit.OK {
		t.Fatalf("first create: exit code = %d; stderr: %s", code, errOut)
	}
	if code := shell.Run([]string{"context", "create", "beta", "--identity", "acme-cloud"}); code != exit.OK {
		t.Fatalf("second create: exit code = %d; stderr: %s", code, errOut)
	}
	if selected := loadDocument(t, shell).DefaultContext; selected != "acme" {
		t.Errorf("defaultContext = %q, want the first context %q", selected, "acme")
	}
}

func TestContextUseSelectsAndWritesNothingElse(t *testing.T) {
	shell, _, errOut := newShell(t)
	seeded := identityOnlyDocument()
	seeded.DefaultContext = "acme"
	seeded.Contexts = []contexts.Context{
		{Name: "acme", Identity: "acme-cloud", Organization: "acme"},
		{Name: "beta", Identity: "acme-cloud", Organization: "beta"},
	}
	installLogin(t, shell, seeded)
	before := loadDocument(t, shell)

	if code := shell.Run([]string{"context", "use", "beta"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	after := loadDocument(t, shell)
	if after.DefaultContext != "beta" {
		t.Errorf("defaultContext = %q, want %q", after.DefaultContext, "beta")
	}
	before.DefaultContext = after.DefaultContext
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	if string(beforeJSON) != string(afterJSON) {
		t.Errorf("wso2 context use changed more than the selection:\nbefore %s\nafter  %s", beforeJSON, afterJSON)
	}
}

func TestContextUseIsRefusedForAnUnknownName(t *testing.T) {
	shell, _, errOut := newShell(t)
	seeded := identityOnlyDocument()
	seeded.DefaultContext = "acme"
	seeded.Contexts = []contexts.Context{{Name: "acme", Identity: "acme-cloud"}}
	installLogin(t, shell, seeded)

	if code := shell.Run([]string{"context", "use", "nosuch"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want the usage class %d; stderr: %s", code, exit.Usage, errOut)
	}
	if !strings.Contains(errOut.String(), "contexts.unknown_context") {
		t.Errorf("stderr does not carry contexts.unknown_context:\n%s", errOut)
	}
	if selected := loadDocument(t, shell).DefaultContext; selected != "acme" {
		t.Errorf("a refused use changed the selection to %q", selected)
	}
}

func TestContextListRendersEveryContextAndMarksTheDefault(t *testing.T) {
	shell, out, errOut := newShell(t)
	seeded := identityOnlyDocument()
	seeded.DefaultContext = "beta"
	seeded.Contexts = []contexts.Context{
		{Name: "acme", Identity: "acme-cloud"},
		{Name: "beta", Identity: "acme-cloud"},
	}
	installLogin(t, shell, seeded)

	if code := shell.Run([]string{"context", "list"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	for _, name := range []string{"acme", "beta"} {
		if !strings.Contains(out.String(), name) {
			t.Errorf("the listing omits the %q context:\n%s", name, out)
		}
	}
	// Which one commands run against is the fact the listing exists to answer.
	selected := ""
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.Contains(line, "*") {
			selected = line
		}
	}
	if !strings.Contains(selected, "beta") {
		t.Errorf("the listing does not mark beta as the selected context:\n%s", out)
	}
}

func TestContextListOnAMachineWithNoDocumentSaysSoPlainly(t *testing.T) {
	shell, out, errOut := newShell(t)

	if code := shell.Run([]string{"context", "list"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.Contains(out.String(), "context create") {
		t.Errorf("an empty listing does not name the command that fills it:\n%s", out)
	}
}

func TestContextCurrentReportsTheSelectedContext(t *testing.T) {
	shell, out, errOut := newShell(t)
	seeded := identityOnlyDocument()
	seeded.DefaultContext = "acme"
	seeded.Contexts = []contexts.Context{{Name: "acme", Identity: "acme-cloud", Organization: "acme"}}
	installLogin(t, shell, seeded)

	if code := shell.Run([]string{"context", "current"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.Contains(out.String(), "acme-cloud") {
		t.Errorf("the report does not name the identity the context authenticates as:\n%s", out)
	}
}

// TestContextCurrentOnAMachineWithNoDocumentSaysSoPlainly proves an
// unconfigured machine is reported as a state rather than as a breakage: a
// first-run user meets this before they have done anything wrong.
func TestContextCurrentOnAMachineWithNoDocumentSaysSoPlainly(t *testing.T) {
	shell, out, errOut := newShell(t)

	if code := shell.Run([]string{"context", "current"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stdout: %s stderr: %s", code, exit.OK, out, errOut)
	}
	if errOut.Len() != 0 {
		t.Errorf("an unconfigured machine wrote to stderr:\n%s", errOut)
	}
	if !strings.Contains(out.String(), "wso2 login") {
		t.Errorf("the report does not name what to run next:\n%s", out)
	}
}

func TestEveryContextSubcommandRendersJSON(t *testing.T) {
	for name, args := range map[string][]string{
		"create":  {"context", "create", "gamma", "--identity", "acme-cloud", "--output", "json"},
		"use":     {"context", "use", "beta", "--output", "json"},
		"list":    {"context", "list", "--output", "json"},
		"current": {"context", "current", "--output", "json"},
	} {
		t.Run(name, func(t *testing.T) {
			shell, out, errOut := newShell(t)
			seeded := identityOnlyDocument()
			seeded.DefaultContext = "acme"
			seeded.Contexts = []contexts.Context{
				{Name: "acme", Identity: "acme-cloud"},
				{Name: "beta", Identity: "acme-cloud"},
			}
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
		})
	}
}

// failingTransport fails the test if anything it is installed on dials.
type failingTransport struct{ t *testing.T }

func (f failingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	f.t.Errorf("a wso2 context subcommand made a request to %s", request.URL.Redacted())
	return nil, http.ErrUseLastResponse
}

// TestNoContextSubcommandOpensANetworkConnection is the D8 guard: an issuer
// typo has to surface at wso2 login, never at wso2 context create, which is
// what makes ADR 0011's claim checkable.
//
// It is asserted at runtime rather than by reading the source. Every HTTP
// client the shell builds leaves its Transport nil or names
// http.DefaultTransport explicitly, so replacing that one value intercepts
// every request this binary can make today. A client that carried its own
// transport would evade this, which is why the assertion is here rather than
// only in a source-reading boundary test: a new client added to a context
// command body would have to be written deliberately to escape it.
func TestNoContextSubcommandOpensANetworkConnection(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = failingTransport{t: t}

	for _, args := range [][]string{
		{"context", "create", "gamma", "--identity", "acme-cloud", "--organization", "acme"},
		{"context", "use", "beta"},
		{"context", "list"},
		{"context", "current"},
	} {
		shell, _, errOut := newShell(t)
		seeded := identityOnlyDocument()
		seeded.DefaultContext = "acme"
		seeded.Contexts = []contexts.Context{
			{Name: "acme", Identity: "acme-cloud"},
			{Name: "beta", Identity: "acme-cloud"},
		}
		installLogin(t, shell, seeded)
		if code := shell.Run(args); code != exit.OK {
			t.Fatalf("%v: exit code = %d; stderr: %s", args, code, errOut)
		}
	}
}

// legacyDocumentJSON is a schema version 1 document, of the shape the
// architecture proof published and a user could still have on disk.
const legacyDocumentJSON = `{
  "schemaVersion": 1,
  "defaultContext": "legacy",
  "contexts": [
    {
      "name": "legacy",
      "organizationId": "acme",
      "endpoint": "https://api.example",
      "auth": {"method": "development-credential", "credentialVariable": "WSO2_DEV_CREDENTIAL"}
    }
  ]
}
`

// installLegacy writes a version 1 document into the shell's isolated state,
// without going through a writer, because no writer in the repository produces
// one at this path.
func installLegacy(t *testing.T, shell app.Shell) {
	t.Helper()
	path := contexts.Path(shell.StateRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(legacyDocumentJSON), 0o600); err != nil {
		t.Fatalf("seed a version 1 document: %v", err)
	}
}

// TestContextCreateOnAVersionOneDocumentExplainsWhatToDo covers the user this
// command exists for: someone who hand-wrote a context file before the shell
// could write one. The writer refuses to overwrite their document, and the bare
// refusal would meet them with a failure and no route forward.
func TestContextCreateOnAVersionOneDocumentExplainsWhatToDo(t *testing.T) {
	shell, _, errOut := newShell(t)
	installLegacy(t, shell)

	if code := shell.Run([]string{"context", "create", "acme", "--identity", "legacy"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want the usage class %d; stderr: %s", code, exit.Usage, errOut)
	}
	reported := errOut.String()
	for _, wanted := range []string{
		contexts.Path(shell.StateRoot), // which file
		"version 1",                    // what is wrong with it
		"wso2 context list",            // that it still works for reading
	} {
		if !strings.Contains(reported, wanted) {
			t.Errorf("the refusal does not mention %q:\n%s", wanted, reported)
		}
	}
	// Nothing invents a migration, and nothing touches the file.
	if strings.Contains(reported, "migrate") {
		t.Errorf("the refusal offers a migration that does not exist:\n%s", reported)
	}
	data, err := os.ReadFile(contexts.Path(shell.StateRoot))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != legacyDocumentJSON {
		t.Errorf("the refused create modified the user's document:\n%s", data)
	}
}

// TestTheContextFamilyRefusesTheContextFlag proves the family is registered in
// shellFlagsFor rather than silently ignoring a shell flag it cannot act on:
// naming a context is what its own arguments do.
func TestTheContextFamilyRefusesTheContextFlag(t *testing.T) {
	shell, _, errOut := newShell(t)
	installLogin(t, shell, identityOnlyDocument())

	if code := shell.Run([]string{"--context", "acme", "context", "list"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want the usage class %d; stderr: %s", code, exit.Usage, errOut)
	}
	if !strings.Contains(errOut.String(), "shell.unsupported_flag") {
		t.Errorf("stderr does not carry shell.unsupported_flag:\n%s", errOut)
	}
}
