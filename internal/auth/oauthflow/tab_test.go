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
	"context"
	"errors"
	"io"
	"net/http"
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
	for _, member := range []string{"state", "code_challenge", "nonce"} {
		if authorizations[0].Get(member) == authorizations[1].Get(member) {
			t.Errorf("both authorizations carried the same %s", member)
		}
	}
	if !strings.Contains(page, "<strong>apim</strong> and <strong>iam</strong>") {
		t.Errorf("the final page does not name both products:\n%s", page)
	}
	if !strings.Contains(printed, `Authorizing the "iam" product in the same browser tab.`) {
		t.Errorf("the second authorization was not announced:\n%s", printed)
	}
}

func TestARefusedSilentAuthorizationIsAskedAgainWithAPromptInTheSameTab(t *testing.T) {
	for _, refusal := range []string{"login_required", "access_denied", "invalid_request"} {
		t.Run(refusal, func(t *testing.T) {
			issuer := fakeissuer.New(t, fakeissuer.Options{AllowAnyLoopbackPort: true, SilentRefusal: refusal})

			opened, page, printed := tabRun(t, issuer)

			if !strings.Contains(printed, `The browser needs you to sign in for "iam"; continuing in the same tab.`) ||
				strings.Contains(printed, refusal) || strings.Contains(printed, "prompt") {
				t.Errorf("the retry was not said plainly:\n%s", printed)
			}
			if opened != 1 {
				t.Errorf("the browser was opened %d times, want 1", opened)
			}
			authorizations := issuer.Authorizations()
			if len(authorizations) != 3 || authorizations[2].Get("prompt") != "" ||
				authorizations[2].Get("resource") != "https://iam.example/v1" {
				t.Errorf("authorizations = %v, want a silent one then an interactive one for iam", authorizations)
			}
			if !strings.Contains(page, "<strong>apim</strong> and <strong>iam</strong>") {
				t.Errorf("the tab did not end signed in to both:\n%s", page)
			}
		})
	}
}

// browser plays a person's browser for one tab: it follows authorizeURL to
// the end of the chain and delivers the page it ends on.
type browser struct {
	issuer *fakeissuer.Issuer
	opens  atomic.Int32
	pages  chan string
	ctx    context.Context
}

func newBrowser(issuer *fakeissuer.Issuer) *browser {
	return &browser{issuer: issuer, pages: make(chan string, 4), ctx: context.Background()}
}

func (b *browser) open(authURL string) error {
	b.opens.Add(1)
	go func() {
		request, _ := http.NewRequestWithContext(b.ctx, http.MethodGet, authURL, nil)
		response, err := b.issuer.HTTPClient().Do(request)
		if err != nil {
			b.pages <- ""
			return
		}
		defer func() { _ = response.Body.Close() }()
		body, _ := io.ReadAll(response.Body)
		b.pages <- string(body)
	}()
	return nil
}

func (b *browser) page(t *testing.T) string {
	t.Helper()
	select {
	case page := <-b.pages:
		return page
	case <-time.After(5 * time.Second):
		t.Fatal("the tab was never answered")
		return ""
	}
}

func tabLogin(issuer *fakeissuer.Issuer, b *browser, tab *oauthflow.Tab, label string) oauthflow.Login {
	login := browserLogin(issuer, &recorder{}, b.open)
	login.Label, login.Resource, login.Tab = label, "urn:example:"+label, tab
	return login
}

func waitUntilReleased(t *testing.T, tab *oauthflow.Tab) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for tab.Holding() {
		if time.Now().After(deadline) {
			t.Fatal("the tab still holds its page")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestAHoldThatRunsOutEndsTheTabAndTheNextLoginOpensAnother(t *testing.T) {
	t.Cleanup(oauthflow.SetHoldLimit(50 * time.Millisecond))
	issuer := fakeissuer.New(t, fakeissuer.Options{AllowAnyLoopbackPort: true})
	b := newBrowser(issuer)
	tab := oauthflow.NewTab()
	defer tab.Close()

	if _, err := tabLogin(issuer, b, tab, "apim").Run(testContext(t, 30*time.Second)); err != nil {
		t.Fatal(err)
	}
	if page := b.page(t); !strings.Contains(page, "<strong>apim</strong>") {
		t.Errorf("the expired hold did not end on the signed-in page:\n%s", page)
	}
	reopened := &recorder{}
	second := tabLogin(issuer, b, tab, "iam")
	second.Out = reopened
	if _, err := second.Run(testContext(t, 30*time.Second)); err != nil {
		t.Fatal(err)
	}

	if got := b.opens.Load(); got != 2 {
		t.Errorf("the browser was opened %d times, want 2", got)
	}
	if !strings.Contains(reopened.String(),
		`The previous browser tab is no longer waiting; opening a new one for "iam".`) {
		t.Errorf("the new tab was not explained:\n%s", reopened)
	}
	if prompt := issuer.Authorizations()[1].Get("prompt"); prompt != "" {
		t.Errorf("a login in a newly opened tab asked with prompt=%q", prompt)
	}
}

func TestATabTheBrowserClosedIsReleased(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{AllowAnyLoopbackPort: true})
	b := newBrowser(issuer)
	ctx, closeTab := context.WithCancel(context.Background())
	b.ctx = ctx
	tab := oauthflow.NewTab()
	defer tab.Close()

	if _, err := tabLogin(issuer, b, tab, "apim").Run(testContext(t, 30*time.Second)); err != nil {
		t.Fatal(err)
	}
	closeTab()
	waitUntilReleased(t, tab)
	b.ctx = context.Background()
	reopened := &recorder{}
	second := tabLogin(issuer, b, tab, "iam")
	second.Out = reopened
	if _, err := second.Run(testContext(t, 30*time.Second)); err != nil {
		t.Fatal(err)
	}

	if got := b.opens.Load(); got != 2 {
		t.Errorf("the browser was opened %d times, want 2", got)
	}
	if !strings.Contains(reopened.String(),
		`The previous browser tab is no longer waiting; opening a new one for "iam".`) {
		t.Errorf("the new tab was not explained:\n%s", reopened)
	}
}

// failingExchange refuses every token request, as a provider that will not
// redeem the code does.
type failingExchange struct{ base http.RoundTripper }

func (f failingExchange) RoundTrip(request *http.Request) (*http.Response, error) {
	if strings.HasSuffix(request.URL.Path, "/token") {
		return nil, errors.New("refused")
	}
	return f.base.RoundTrip(request)
}

func TestTheSignedInPageNamesOnlyTheProductsWhoseLoginCompleted(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{AllowAnyLoopbackPort: true})
	b := newBrowser(issuer)
	tab := oauthflow.NewTab()
	if _, err := tabLogin(issuer, b, tab, "apim").Run(testContext(t, 30*time.Second)); err != nil {
		t.Fatal(err)
	}
	failing := tabLogin(issuer, b, tab, "iam")
	base := issuer.HTTPClient().Transport
	if base == nil {
		base = http.DefaultTransport
	}
	failing.HTTPClient = &http.Client{Transport: failingExchange{base}}

	if _, err := failing.Run(testContext(t, 30*time.Second)); err == nil {
		t.Fatal("the iam login succeeded though its exchange was refused")
	}
	tab.Close()

	page := b.page(t)
	if !strings.Contains(page, "<strong>apim</strong>") || strings.Contains(page, "iam") {
		t.Errorf("the page does not name apim alone:\n%s", page)
	}
}

func TestAReplayOfAnEarlierCallbackIsAStrayTab(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{AllowAnyLoopbackPort: true})
	client := *issuer.HTTPClient()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	step := func(target string) (*http.Response, string) {
		response, err := client.Get(target)
		if err != nil {
			t.Errorf("get: %v", err)
			return nil, ""
		}
		defer func() { _ = response.Body.Close() }()
		body, _ := io.ReadAll(response.Body)
		return response, string(body)
	}
	replay := make(chan *http.Response, 1)
	open := func(authURL string) error {
		go func() {
			first, _ := step(authURL)
			callback := first.Header.Get("Location")
			held, _ := step(callback)
			stray, body := step(callback)
			if !strings.Contains(body, "This tab is not part of a login") {
				t.Errorf("the replay was served:\n%s", body)
			}
			replay <- stray
			second, _ := step(held.Header.Get("Location"))
			step(second.Header.Get("Location"))
		}()
		return nil
	}
	tab := oauthflow.NewTab()
	for _, label := range []string{"apim", "iam"} {
		login := browserLogin(issuer, &recorder{}, open)
		login.Label, login.Resource, login.Tab = label, "urn:example:"+label, tab
		if _, err := login.Run(testContext(t, 30*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	tab.Close()

	if response := <-replay; response.StatusCode != http.StatusBadRequest {
		t.Errorf("the replay was answered %d, want 400", response.StatusCode)
	}
}
