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
	"errors"
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
	// An identity exists here, so the likeliest fault is a mistyped name, and
	// the recovery names the command that shows what login recorded.
	if !strings.Contains(errOut.String(), "wso2 identity list") {
		t.Errorf("the recovery does not name wso2 identity list:\n%s", errOut)
	}
	if len(loadDocument(t, shell).Contexts) != 0 {
		t.Error("a refused create wrote a context")
	}
}

func TestContextCreateWithNoIdentitiesAtAllPointsAtLoginAlone(t *testing.T) {
	// A machine nobody has logged in on holds no identities, so there is
	// nothing for wso2 identity list to show: offering it, or offering wso2
	// context create again, would walk a first-run user in a circle. Login is
	// the one honest way forward.
	shell, out, errOut := newShell(t)

	code := shell.Run([]string{"context", "create", "acme", "--identity", "nosuch"})
	if code != exit.Usage {
		t.Fatalf("exit code = %d, want the usage class %d; stdout: %s stderr: %s",
			code, exit.Usage, out, errOut)
	}
	if !strings.Contains(errOut.String(), "contexts.unknown_identity") {
		t.Errorf("stderr does not carry contexts.unknown_identity:\n%s", errOut)
	}
	if !strings.Contains(errOut.String(), "no identities exist") {
		t.Errorf("the refusal does not say no identities exist:\n%s", errOut)
	}
	if !strings.Contains(errOut.String(), "wso2 login") {
		t.Errorf("the recovery does not name wso2 login:\n%s", errOut)
	}
	if strings.Contains(errOut.String(), "wso2 identity list") {
		t.Errorf("the recovery offers wso2 identity list with nothing to list:\n%s", errOut)
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
			// The shell's own renderer publishes no discriminator, so neither
			// does this family: one command inventing a second convention is
			// worse than the convention being absent.
			if _, published := decoded["schema"]; published {
				t.Errorf("the result publishes a schema key the rest of the shell suppresses:\n%s", out)
			}
		})
	}
}

// failingTransport fails the test if anything it is installed on dials.
//
// family names the command family under test, because more than one installs
// this guard and a failure that named the wrong one would send a reader to the
// wrong package for the most load-bearing assertion either family makes.
type failingTransport struct {
	t      *testing.T
	family string
}

func (f failingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	f.t.Errorf("a wso2 %s subcommand made a request to %s", f.family, request.URL.Redacted())
	return nil, errNoNetwork
}

// errNoNetwork is what the guard answers a caller with. It is a plain error
// rather than a sentinel borrowed from net/http, whose sentinels all mean
// something specific to a client that a transport must not claim.
var errNoNetwork = errors.New("this test permits no network call")

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
	http.DefaultTransport = failingTransport{t: t, family: "context"}

	// The refusal paths are covered alongside the successful ones. A refusal is
	// where a well-meaning "let me check the issuer before I complain" would be
	// added, and it is the path a first-run user reaches first.
	invocations := map[string][]string{
		"create":                 {"context", "create", "gamma", "--identity", "acme-cloud", "--organization", "acme"},
		"use":                    {"context", "use", "beta"},
		"list":                   {"context", "list"},
		"current":                {"context", "current"},
		"create, taken name":     {"context", "create", "acme", "--identity", "acme-cloud"},
		"create, no identity":    {"context", "create", "delta", "--identity", "nosuch"},
		"create, illegal name":   {"context", "create", "Delta", "--identity", "acme-cloud"},
		"create, no name":        {"context", "create"},
		"use, unknown name":      {"context", "use", "nosuch"},
		"list, stray argument":   {"context", "list", "extra"},
		"unsupported shell flag": {"--context", "acme", "context", "list"},
	}
	for name, args := range invocations {
		t.Run(name, func(t *testing.T) {
			shell, _, _ := newShell(t)
			seeded := identityOnlyDocument()
			seeded.DefaultContext = "acme"
			seeded.Contexts = []contexts.Context{
				{Name: "acme", Identity: "acme-cloud"},
				{Name: "beta", Identity: "acme-cloud"},
			}
			installLogin(t, shell, seeded)
			// The exit code is not asserted: what is asserted is that whatever
			// the command decided, it decided it without dialling anything.
			shell.Run(args)
		})
	}
}

// TestTheNetworkGuardWouldNoticeARequest proves the guard is not vacuous: the
// same transport, reached by the same in-process route, fails a test.
func TestTheNetworkGuardWouldNoticeARequest(t *testing.T) {
	watched := &testing.T{}
	transport := failingTransport{t: watched, family: "context"}
	request, err := http.NewRequest(http.MethodGet, "https://idp.example/.well-known/openid-configuration", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if _, err := transport.RoundTrip(request); !errors.Is(err, errNoNetwork) {
		t.Errorf("RoundTrip returned %v, want the guard's own error", err)
	}
	if !watched.Failed() {
		t.Error("the guard did not fail the test it was watching, so it would miss a real request")
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
// its own declaration rather than silently ignoring a flag it cannot act on:
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

// TestSelectedIsABooleanWhereverItAppears proves one field name does not carry
// two types inside one command family: a caller that reads create's "selected"
// and list's must not have to know which is a string.
func TestSelectedIsABooleanWhereverItAppears(t *testing.T) {
	shell, out, errOut := newShell(t)
	installLogin(t, shell, identityOnlyDocument())

	if code := shell.Run([]string{"context", "create", "acme",
		"--identity", "acme-cloud", "--output", "json"}); code != exit.OK {
		t.Fatalf("create: exit code = %d; stderr: %s", code, errOut)
	}
	var created map[string]any
	if err := json.Unmarshal(out.Bytes(), &created); err != nil {
		t.Fatalf("create output is not JSON: %v\n%s", err, out)
	}
	if _, ok := created["selected"].(bool); !ok {
		t.Errorf("create reports selected as %T, want a boolean: %v", created["selected"], created["selected"])
	}

	out.Reset()
	if code := shell.Run([]string{"context", "list", "--output", "json"}); code != exit.OK {
		t.Fatalf("list: exit code = %d; stderr: %s", code, errOut)
	}
	var listed struct {
		Contexts []map[string]any `json:"contexts"`
	}
	if err := json.Unmarshal(out.Bytes(), &listed); err != nil {
		t.Fatalf("list output is not JSON: %v\n%s", err, out)
	}
	if len(listed.Contexts) != 1 {
		t.Fatalf("the listing carries %d contexts, want 1:\n%s", len(listed.Contexts), out)
	}
	if _, ok := listed.Contexts[0]["selected"].(bool); !ok {
		t.Errorf("list reports selected as %T, want a boolean", listed.Contexts[0]["selected"])
	}
}

// TestContextCurrentReportsAnUnconfiguredMachineInBothRenderings proves the
// table and the JSON cannot disagree about what the command found: the prose
// says no context is configured, and the JSON has to say the same thing rather
// than leaving a caller to infer it from four empty strings. See ADR 0003.
func TestContextCurrentReportsAnUnconfiguredMachineInBothRenderings(t *testing.T) {
	shell, out, errOut := newShell(t)

	if code := shell.Run([]string{"context", "current", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit code = %d; stderr: %s", code, errOut)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("the output is not JSON: %v\n%s", err, out)
	}
	configured, ok := decoded["configured"].(bool)
	if !ok {
		t.Fatalf("the report does not say whether a context is configured: %v", decoded)
	}
	if configured {
		t.Error("an unconfigured machine reported itself as configured")
	}
}

// TestAWrongArgumentCountIsAUsageRefusal proves a miscounted argument is
// reported in the usage class with a way back, rather than in the class the
// command reference reserves for a module process that crashed.
//
// wso2 context create with no argument is the likeliest first-run typo in the
// family, which is why the class it exits in matters.
func TestAWrongArgumentCountIsAUsageRefusal(t *testing.T) {
	for name, args := range map[string][]string{
		"create with no name":      {"context", "create"},
		"create with two names":    {"context", "create", "one", "two"},
		"use with no name":         {"context", "use"},
		"use with two names":       {"context", "use", "one", "two"},
		"list with an argument":    {"context", "list", "extra"},
		"current with an argument": {"context", "current", "extra"},
	} {
		t.Run(name, func(t *testing.T) {
			shell, _, errOut := newShell(t)

			if code := shell.Run(args); code != exit.Usage {
				t.Fatalf("exit code = %d, want the usage class %d; stderr: %s",
					code, exit.Usage, errOut)
			}
			if strings.Contains(errOut.String(), "shell.unexpected_failure") {
				t.Errorf("a miscounted argument is reported as an unexpected failure:\n%s", errOut)
			}
			// A refusal with no way back leaves the user to guess the shape.
			if !strings.Contains(errOut.String(), "Run wso2 context") {
				t.Errorf("the refusal names no way back:\n%s", errOut)
			}
		})
	}
}

// TestContextCreateRefusesANameTheDocumentCannotHold proves a name the user
// typed is refused as the argument it is. Telling them their document is
// malformed and offering to remove it would destroy contexts they already have,
// over a name that never reached the file.
func TestContextCreateRefusesANameTheDocumentCannotHold(t *testing.T) {
	for _, name := range []string{"Acme", "a b", "a/b", "1acme", ""} {
		t.Run(name, func(t *testing.T) {
			shell, _, errOut := newShell(t)
			installLogin(t, shell, identityOnlyDocument())

			code := shell.Run([]string{"context", "create", name, "--identity", "acme-cloud"})
			if code != exit.Usage {
				t.Fatalf("exit code = %d, want the usage class %d; stderr: %s",
					code, exit.Usage, errOut)
			}
			reported := errOut.String()
			if strings.Contains(reported, "contexts.document_malformed") {
				t.Errorf("a mistyped argument is reported as a malformed document:\n%s", reported)
			}
			if strings.Contains(reported, "remove it") {
				t.Errorf("the refusal offers to remove the user's document:\n%s", reported)
			}
			// The rule, so the next attempt is informed rather than another guess.
			if !strings.Contains(reported, contexts.NameRule) {
				t.Errorf("the refusal does not state the naming rule:\n%s", reported)
			}
		})
	}
}

// TestTheFrozenDocumentRecoveryRoutesThroughLogin proves the way out actually
// works. Moving a version 1 document aside takes its identities with it: they
// exist only as the compatibility read's synthetic ones, so a user who moves
// the file and re-runs the create meets a second refusal with their old file
// already renamed. Login is what creates an identity (#112 D3), so it has to
// come first.
func TestTheFrozenDocumentRecoveryRoutesThroughLogin(t *testing.T) {
	shell, _, errOut := newShell(t)
	installLegacy(t, shell)

	if code := shell.Run([]string{"context", "create", "acme", "--identity", "legacy"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want the usage class %d; stderr: %s", code, exit.Usage, errOut)
	}
	reported := errOut.String()
	if !strings.Contains(reported, "wso2 login") {
		t.Errorf("the recovery does not route through wso2 login, which is what creates an identity:\n%s",
			reported)
	}
	// The instruction that does not work: moving the file aside and re-running
	// this command refuses again, because the identity went with the file.
	if strings.Contains(reported, "run the command again") {
		t.Errorf("the recovery still tells the user to re-run a command that would refuse:\n%s", reported)
	}
}
