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

package oauthflow_test

import (
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/oauthflow"
)

// tabRun runs two logins for two products in one tab, the browser following
// every redirect from the first authorization URL, and reports how many times
// the browser was opened and the page the tab ended on.
func tabRun(t *testing.T, issuer *fakeissuer.Issuer) (opened int32, page string, printed string) {
	t.Helper()
	tab := oauthflow.NewTab()
	out := &recorder{}
	final := make(chan string, 1)
	var opens atomic.Int32
	open := func(authURL string) error {
		opens.Add(1)
		go func() {
			response, err := issuer.HTTPClient().Get(authURL)
			if err != nil {
				final <- ""
				return
			}
			defer func() { _ = response.Body.Close() }()
			body, _ := io.ReadAll(response.Body)
			final <- string(body)
		}()
		return nil
	}
	for _, product := range []struct{ label, resource string }{
		{"apim", "urn:example:shared"}, {"iam", "https://iam.example/v1"},
	} {
		login := browserLogin(issuer, out, open)
		login.Label, login.Resource, login.Tab = product.label, product.resource, tab
		result, err := login.Run(testContext(t, 30*time.Second))
		if err != nil {
			t.Fatalf("%s: %v", product.label, err)
		}
		if result.Token.RefreshToken == "" {
			t.Fatalf("%s: no refresh token", product.label)
		}
	}
	tab.Close()
	select {
	case page = <-final:
	case <-time.After(5 * time.Second):
		t.Fatal("the tab was never answered")
	}
	return opens.Load(), page, out.String()
}

func TestLoginsSharingATabOpenTheBrowserOnce(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{AllowAnyLoopbackPort: true})

	opened, page, printed := tabRun(t, issuer)

	if opened != 1 {
		t.Errorf("the browser was opened %d times, want 1", opened)
	}
	authorizations := issuer.Authorizations()
	if len(authorizations) != 2 {
		t.Fatalf("%d authorizations, want 2", len(authorizations))
	}
	if authorizations[0].Get("prompt") != "" || authorizations[1].Get("prompt") != "none" {
		t.Errorf("prompts = %q, %q, want the second alone silent",
			authorizations[0].Get("prompt"), authorizations[1].Get("prompt"))
	}
	if !strings.Contains(page, "<strong>apim</strong> and <strong>iam</strong>") {
		t.Errorf("the final page does not name both products:\n%s", page)
	}
	if !strings.Contains(printed, `Authorizing the "iam" product in the same browser tab.`) {
		t.Errorf("the second authorization was not announced:\n%s", printed)
	}
}

func TestASilentAuthorizationThatNeedsThePersonContinuesInTheSameTab(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{AllowAnyLoopbackPort: true, SilentRefusal: "login_required"})

	opened, page, _ := tabRun(t, issuer)

	if opened != 1 {
		t.Errorf("the browser was opened %d times, want 1", opened)
	}
	authorizations := issuer.Authorizations()
	if len(authorizations) != 3 || authorizations[2].Get("prompt") != "" ||
		authorizations[2].Get("resource") != "https://iam.example/v1" {
		t.Errorf("authorizations = %v, want a silent one then an interactive one for iam", authorizations)
	}
	if !strings.Contains(page, "You are signed in") {
		t.Errorf("the tab did not end signed in:\n%s", page)
	}
}
