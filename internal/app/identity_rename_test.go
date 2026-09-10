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

// wso2 account rename (#176).
package app_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
)

// newRenameShell holds two accounts: account-1, with its same-named context
// and a second context beside it, and other.
func newRenameShell(t *testing.T) (app.Shell, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	keyring.MockInit()
	t.Setenv("WSO2_CONTEXT", "")
	shell, out, errOut := newShell(t)
	for _, args := range [][]string{
		{"account", "create", "account-1", "--issuer", "http://localhost:8490", "--client-id", "wso2-cli"},
		{"context", "create", "staging", "--account", "account-1"},
		{"account", "create", "other", "--issuer", "http://localhost:8491", "--client-id", "wso2-cli"},
	} {
		if code := shell.Run(args); code != exit.OK {
			t.Fatalf("%v: exit %d: %s", args, code, errOut)
		}
	}
	out.Reset()
	errOut.Reset()
	return shell, out, errOut
}

func TestAccountRenameRenamesTheAccountAndEveryContextUsingIt(t *testing.T) {
	shell, out, errOut := newRenameShell(t)
	if err := (session.Store{StateRoot: shell.StateRoot}).Save("account-1",
		session.Session{Issuer: "http://localhost:8490", RefreshToken: "rt"}); err != nil {
		t.Fatal(err)
	}

	if code := shell.Run([]string{"account", "rename", "account-1", "local-idp"}); code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	document := loadDocument(t, shell)
	renamed := document.Accounts[0]
	if renamed.Name != "local-idp" || renamed.Auth.Issuer != "http://localhost:8490" {
		t.Fatalf("account = %+v, want local-idp on the first issuer", renamed)
	}
	// The secure-store entry is named by the reference, which does not move:
	// the store cannot list its entries, so one moved and lost could never be
	// revoked.
	if renamed.Auth.CredentialRef != "account-1" {
		t.Errorf("credentialRef = %q, want it left at account-1", renamed.Auth.CredentialRef)
	}
	if document.Accounts[1].Name != "other" {
		t.Errorf("the other account changed: %+v", document.Accounts[1])
	}
	want := map[string]string{"account-1": "local-idp", "staging": "local-idp", "other": "other"}
	for _, context := range document.Contexts {
		if want[context.Name] != context.Account {
			t.Errorf("context %q uses %q, want %q", context.Name, context.Account, want[context.Name])
		}
	}
	if document.DefaultContext != "account-1" {
		t.Errorf("the selected context moved to %q", document.DefaultContext)
	}
	// The session still loads for the renamed account's context.
	selected, err := document.Select("account-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (session.Store{StateRoot: shell.StateRoot}).Load(selected.Identity.Auth.CredentialRef); err != nil {
		t.Errorf("the renamed account no longer reaches its session: %v", err)
	}
	for _, expected := range []string{`Renamed account "account-1" to "local-idp".`,
		`The context "account-1" keeps its name`} {
		if !strings.Contains(out.String(), expected) {
			t.Errorf("the report is missing %q in:\n%s", expected, out)
		}
	}
}

func TestAccountRenameRendersJSON(t *testing.T) {
	shell, out, errOut := newRenameShell(t)

	code := shell.Run([]string{"account", "rename", "account-1", "local-idp", "--output", "json"})
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var rendered struct {
		From     string   `json:"from"`
		To       string   `json:"to"`
		Contexts []string `json:"contexts"`
	}
	if err := json.Unmarshal(out.Bytes(), &rendered); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if rendered.From != "account-1" || rendered.To != "local-idp" ||
		strings.Join(rendered.Contexts, ",") != "account-1,staging" {
		t.Errorf("rendered = %+v", rendered)
	}
}

func TestAccountRenameRefusals(t *testing.T) {
	for _, testCase := range []struct {
		name, code string
		args       []string
	}{
		{"an unknown account", "contexts.unknown_identity", []string{"missing", "local-idp"}},
		{"a taken name", "contexts.identity_exists", []string{"account-1", "other"}},
		{"the same name", "contexts.identity_exists", []string{"account-1", "account-1"}},
		{"an invalid name", "shell.invalid_argument", []string{"account-1", "Local IdP"}},
		{"one argument", "shell.missing_argument", []string{"account-1"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			shell, out, errOut := newRenameShell(t)
			before, err := os.ReadFile(contexts.Path(shell.StateRoot))
			if err != nil {
				t.Fatal(err)
			}
			code := shell.Run(append([]string{"account", "rename"}, testCase.args...))
			if code != exit.Usage || !strings.Contains(errOut.String(), testCase.code) {
				t.Fatalf("exit %d, want %d with %s; stderr:\n%s", code, exit.Usage, testCase.code, errOut)
			}
			after, err := os.ReadFile(contexts.Path(shell.StateRoot))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Error("a refused rename rewrote the document")
			}
			if out.String() != "" {
				t.Errorf("a refused rename wrote to standard output:\n%s", out)
			}
		})
	}
}
