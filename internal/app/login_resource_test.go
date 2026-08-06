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
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/zalando/go-keyring"
)

// thunderDoc is browserDoc against a deployment that decides the audience at
// authorization time.
func thunderDoc(issuerURL string) contexts.Document {
	document := browserDoc(issuerURL)
	document.Identities[0].Auth.Provider = contexts.ProviderThunder
	return document
}

// A deployment that requires a resource indicator refuses a login that carries
// none, so the login has to take the one its product names. Without this the
// shell cannot log in against such a deployment at all.
func TestLoginBindsTheSessionToTheResourceTheProductNames(t *testing.T) {
	keyring.MockInit()
	issuer := fakeissuer.New(t, fakeissuer.Options{RequireResource: true})
	shell, _, errOut := newLoginShell(t)
	installLogin(t, shell, thunderDoc(issuer.URL))
	shell.OpenBrowser = func(authURL string) error {
		go func() {
			response, err := http.Get(authURL)
			if err == nil {
				_ = response.Body.Close()
			}
		}()
		return nil
	}

	if code := shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}

	stored, err := session.Store{StateRoot: shell.StateRoot}.Load(credentialRef)
	if err != nil {
		t.Fatalf("session not stored: %v", err)
	}
	if stored.RefreshToken == "" {
		t.Fatal("the stored session holds no refresh token")
	}
	if !strings.Contains(errOut.String(), "resource="+url.QueryEscape("reference-status")) {
		t.Fatalf("the authorization URL carried no resource indicator:\n%s", errOut)
	}
}

// An identity that names no such deployment must keep asking exactly as it did
// before, or every deployment already working would start receiving an
// indicator it never agreed to interpret.
func TestLoginSendsNoResourceIndicatorForAnOrdinaryDeployment(t *testing.T) {
	keyring.MockInit()
	issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "reference-status"})
	shell, _, errOut := newLoginShell(t)
	installLogin(t, shell, browserDoc(issuer.URL))
	shell.OpenBrowser = func(authURL string) error {
		go func() {
			response, err := http.Get(authURL)
			if err == nil {
				_ = response.Body.Close()
			}
		}()
		return nil
	}

	if code := shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	if strings.Contains(errOut.String(), "resource=") {
		t.Fatalf("an ordinary login sent a resource indicator:\n%s", errOut)
	}
}
