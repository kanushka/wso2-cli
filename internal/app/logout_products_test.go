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

	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
)

func TestLogoutEndsEveryProductSession(t *testing.T) {
	keyring.MockInit()
	login := fakeissuer.New(t, fakeissuer.Options{RequireResource: true})
	product := fakeissuer.New(t, fakeissuer.Options{Audience: "apim-cli"})
	shell, out, errOut := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	installLogin(t, shell, thunderDoc(login.URL, product.URL))
	store := session.Store{StateRoot: shell.StateRoot}
	for ref, issuer := range map[string]string{
		credentialRef: login.URL,
		contexts.ProductSessionRef(credentialRef, "iam"):  login.URL,
		contexts.ProductSessionRef(credentialRef, "apim"): product.URL,
	} {
		if err := store.Save(ref, session.Session{Issuer: issuer, RefreshToken: "rt-" + ref}); err != nil {
			t.Fatal(err)
		}
	}
	if code := shell.Run([]string{"logout"}); code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, ref := range []string{credentialRef, contexts.ProductSessionRef(credentialRef, "iam"),
		contexts.ProductSessionRef(credentialRef, "apim")} {
		if _, err := store.Load(ref); err == nil {
			t.Fatalf("%s survived logout", ref)
		}
	}
	if !strings.Contains(out.String(), "apim ended") || !strings.Contains(out.String(), "iam ended") {
		t.Fatalf("report:\n%s", out)
	}
}

func TestLogoutOnAClientCredentialsIdentityExitsCleanly(t *testing.T) {
	keyring.MockInit()
	shell, out, errOut := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	installLogin(t, shell, identityDoc(contexts.KindClientCredentials)("http://login.example"))
	if code := shell.Run([]string{"logout"}); code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out.String(), "none") {
		t.Fatalf("report:\n%s", out)
	}
}
