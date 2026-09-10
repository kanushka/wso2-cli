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

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
)

func TestWhoamiReportsEveryProductSession(t *testing.T) {
	keyring.MockInit()
	shell, out, errOut := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	installLogin(t, shell, thunderDoc("http://login.example", "http://apim.example"))
	store := session.Store{StateRoot: shell.StateRoot}
	if err := store.Save(credentialRef, session.Session{Issuer: "http://login.example", RefreshToken: "rt", Subject: "user-1"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(contexts.ProductSessionRef(credentialRef, "iam"),
		session.Session{Issuer: "http://login.example", RefreshToken: "rt2", Strategy: contexts.StrategySibling}); err != nil {
		t.Fatal(err)
	}
	if code := shell.Run([]string{"whoami"}); code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, want := range []string{"gateway: direct, present", "iam: sibling, present", "apim: federated, none"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	out.Reset()
	if code := shell.Run([]string{"whoami", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	report := decodeWhoamiReport(t, out.Bytes())
	if len(report.Products) != 3 || report.Products[0].Namespace != "apim" || report.Products[0].Session != "none" {
		t.Fatalf("products %+v", report.Products)
	}
}

func TestWhoamiReportsAClientCredentialsIdentityAsInline(t *testing.T) {
	keyring.MockInit()
	shell, out, errOut := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	installLogin(t, shell, identityDoc(contexts.KindClientCredentials)("http://login.example"))
	if code := shell.Run([]string{"whoami"}); code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out.String(), "inline") || strings.Contains(out.String(), "wso2 login") {
		t.Fatalf("a client-credentials account was told to log in:\n%s", out)
	}
}

func TestWhoamiReportsAnExchangedProductAsServedByTheLoginSession(t *testing.T) {
	// An exchanged product holds no session of its own, so the honest report
	// is not "none" — which reads as "log in" and would send the user to run
	// a login that establishes nothing. The login session is what serves it.
	keyring.MockInit()
	shell, out, errOut := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	document := thunderDoc("http://login.example", "http://apim.example")
	document.Accounts[0].Products["apip"] = contexts.Product{
		Endpoint: "http://apip.example", Audience: "http://apip.example",
		Grant: &contexts.Grant{Kind: contexts.GrantExchange}}
	installLogin(t, shell, document)
	store := session.Store{StateRoot: shell.StateRoot}
	if err := store.Save(credentialRef, session.Session{
		Issuer: "http://login.example", RefreshToken: "rt", Subject: "user-1"}); err != nil {
		t.Fatal(err)
	}
	if code := shell.Run([]string{"whoami"}); code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out.String(), "apip: exchanged, exchanged") {
		t.Fatalf("an exchanged product was not reported as served by the login session:\n%s", out)
	}
}
