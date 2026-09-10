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
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
)

// servedPage is what a browser is handed by the shell's own listener.
type servedPage struct {
	status int
	body   string
}

// fetchCallback runs one browser login as far as its callback, answers that
// callback with the query build returns, and reports the page the browser gets.
//
// The login itself is expected to fail on every path but the accepted one, so
// its result is discarded: what is under test is the document, not the flow.
func fetchCallback(t *testing.T, label string, deadline time.Duration,
	build func(code, state string) url.Values) servedPage {
	t.Helper()
	issuer := fakeissuer.New(t, fakeissuer.Options{
		Audience: "reference-status", AllowAnyLoopbackPort: true,
	})
	printed := &recorder{}
	served := make(chan servedPage, 1)
	login := browserLogin(issuer, printed, func(authURL string) error {
		go func() {
			code, callbackURL := interceptCode(t, issuer, authURL)
			authorization, err := url.Parse(authURL)
			if err != nil {
				t.Errorf("parse the authorization URL: %v", err)
				return
			}
			query := build(code, authorization.Query().Get("state"))
			response, err := issuer.HTTPClient().Get(callbackURL + "?" + query.Encode())
			if err != nil {
				t.Errorf("fetch the callback: %v", err)
				return
			}
			defer func() { _ = response.Body.Close() }()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Errorf("read the callback page: %v", err)
				return
			}
			served <- servedPage{response.StatusCode, string(body)}
		}()
		return nil
	})
	login.Label = label

	_, _ = login.Run(testContext(t, deadline))
	select {
	case page := <-served:
		return page
	case <-time.After(5 * time.Second):
		t.Fatal("the callback was never answered")
		return servedPage{}
	}
}

// TestAcceptedCallbackNamesTheProduct covers the page a person actually reaches
// after signing in. One wso2 login opens this page once per product session, so
// naming the product is what tells the second tab apart from the first.
func TestAcceptedCallbackNamesTheProduct(t *testing.T) {
	var authorizationCode string
	page := fetchCallback(t, "apim", 30*time.Second, func(code, state string) url.Values {
		authorizationCode = code
		return url.Values{"code": {code}, "state": {state}}
	})

	if page.status != 200 {
		t.Fatalf("the accepted callback was served %d, want 200", page.status)
	}
	if !strings.Contains(page.body, "You are signed in") {
		t.Fatalf("the accepted page does not say the login worked:\n%s", page.body)
	}
	if !strings.Contains(page.body, "run <strong>apim</strong> product commands") {
		t.Fatalf("the accepted page does not name the product:\n%s", page.body)
	}
	// The page never repeats what the browser carried to it. The code is the
	// one value on that query string a page could leak into a screenshot, a
	// bookmark, or a colleague's shoulder.
	if authorizationCode == "" {
		t.Fatal("the test drove the callback without an authorization code")
	}
	if strings.Contains(page.body, authorizationCode) {
		t.Fatalf("the authorization code reached the page:\n%s", page.body)
	}
}

// TestAcceptedCallbackWithoutProduct covers a login that names no product: the
// sentence drops the product rather than leaving a gap where it would go.
func TestAcceptedCallbackWithoutProduct(t *testing.T) {
	page := fetchCallback(t, "", 30*time.Second, func(code, state string) url.Values {
		return url.Values{"code": {code}, "state": {state}}
	})

	if !strings.Contains(page.body, "The WSO2 CLI can now run commands for you.") {
		t.Fatalf("the accepted page without a product lost its sentence:\n%s", page.body)
	}
	if strings.Contains(page.body, "<strong>") {
		t.Fatalf("the accepted page without a product still sets one in bold:\n%s", page.body)
	}
}

// TestRejectedCallbacksDoNotReadAsSuccess covers the three pages nobody should
// mistake for a completed login: a stray or forged tab, a callback with no
// code, and a provider that refused. Each keeps its status, and none of them
// claims a session was established or names a product, because none was.
func TestRejectedCallbacksDoNotReadAsSuccess(t *testing.T) {
	cases := []struct {
		name   string
		status int
		says   string
		build  func(code, state string) url.Values
	}{
		{
			name:   "a tab this login did not start",
			status: 400,
			says:   "This tab is not part of a login",
			build: func(code, _ string) url.Values {
				return url.Values{"code": {code}, "state": {"not-the-state-this-login-sent"}}
			},
		},
		{
			name:   "a callback carrying no code",
			status: 400,
			says:   "Nothing to finish here",
			build: func(_, state string) url.Values {
				return url.Values{"state": {state}}
			},
		},
		{
			name:   "a provider that refused",
			status: 200,
			says:   "Login failed",
			build: func(_, state string) url.Values {
				return url.Values{"state": {state}, "error": {"access_denied"}}
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			page := fetchCallback(t, "apim", 2*time.Second, testCase.build)

			if page.status != testCase.status {
				t.Fatalf("served %d, want %d:\n%s", page.status, testCase.status, page.body)
			}
			if !strings.Contains(page.body, testCase.says) {
				t.Fatalf("the page does not say %q:\n%s", testCase.says, page.body)
			}
			for _, forbidden := range []string{"signed in", "complete", "apim", "access_denied"} {
				if strings.Contains(page.body, forbidden) {
					t.Fatalf("a rejected callback's page contains %q:\n%s", forbidden, page.body)
				}
			}
		})
	}
}
