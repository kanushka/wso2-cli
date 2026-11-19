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

package oauthflow

import (
	"context"
	"crypto/subtle"
	"net"
	"net/http"
	"sync"
	"time"
)

// Tab is the browser tab, and the loopback listener behind it, that a run of
// browser logins shares. Each accepted callback is held open rather than
// answered, and the next login sends the tab on to its own authorization from
// there, asking for it without a prompt, so one wso2 login authorizes several
// products in one tab. The tab ends on the signed-in page when the run closes
// it, or when no login has followed for holdLimit.
type Tab struct {
	mutex    sync.Mutex
	listener net.Listener
	server   *http.Server
	logins   []*callback
	held     *heldPage
	products []string
}

// heldPage is an accepted callback whose response waits for the tab's next
// authorization URL, or for "" to end on the signed-in page. Whoever takes it
// off the tab is its one sender.
type heldPage struct {
	next chan string
	done chan struct{}
}

// holdLimit bounds how long a held callback waits for the next login, so a
// run that stops between logins still ends the tab on a page.
const holdLimit = time.Minute

// NewTab starts a tab no login has used yet.
func NewTab() *Tab { return &Tab{} }

// bind takes a loopback port for the tab, the first time a login uses it.
func (t *Tab) bind(ports []int) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if t.listener != nil {
		return nil
	}
	listener, err := bindLoopback(ports)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, t.handle)
	t.listener = listener
	t.server = &http.Server{Handler: mux, ReadHeaderTimeout: callbackReadHeaderTimeout}
	go func() { _ = t.server.Serve(listener) }()
	return nil
}

func (t *Tab) redirectURL() string { return redirectURL(t.listener) }

// expect registers one authorization the tab may be called back for.
func (t *Tab) expect(state, label, resource string, silent bool) *callback {
	waiting := &callback{state: state, label: label, resource: resource, silent: silent,
		results: make(chan callbackResult, 1)}
	t.mutex.Lock()
	t.logins = append(t.logins, waiting)
	t.mutex.Unlock()
	return waiting
}

// forget drops an authorization that has its answer, so a replay of its
// callback is a stray tab.
func (t *Tab) forget(done *callback) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	for index, waiting := range t.logins {
		if waiting == done {
			t.logins = append(t.logins[:index], t.logins[index+1:]...)
			return
		}
	}
}

// find is the authorization a callback's state belongs to. Every one is
// compared in constant time, so the comparison says nothing about the states.
func (t *Tab) find(state string) *callback {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	var found *callback
	for _, waiting := range t.logins {
		if subtle.ConstantTimeCompare([]byte(state), []byte(waiting.state)) == 1 {
			found = waiting
		}
	}
	return found
}

// holding reports whether an accepted callback waits for the next login.
func (t *Tab) holding() bool {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return t.held != nil
}

// handOff sends the held tab on to authURL, reporting false when no tab is
// held, in which case the caller opens one.
func (t *Tab) handOff(authURL string) bool {
	t.mutex.Lock()
	page := t.held
	t.held = nil
	t.mutex.Unlock()
	if page == nil {
		return false
	}
	page.next <- authURL
	return true
}

// Close ends the held tab on the signed-in page and stops the listener.
func (t *Tab) Close() {
	t.mutex.Lock()
	page := t.held
	t.held = nil
	server := t.server
	t.mutex.Unlock()
	if page != nil {
		page.next <- ""
		select {
		case <-page.done:
		case <-time.After(5 * time.Second):
		}
	}
	if server != nil {
		_ = server.Close()
	}
}

// handle answers one callback. A callback carrying no registered state is not
// this run's: it is answered and discarded while the tab keeps waiting, which
// is what makes a stray or forged browser tab pointed at this port harmless.
// The page it is answered with names no product: a tab this run did not start
// is told only that it belongs to nothing here.
func (t *Tab) handle(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	waiting := t.find(query.Get("state"))
	if waiting == nil {
		respond(w, pageStrayTab)
		return
	}
	switch refusal := query.Get("error"); {
	case refusal == "invalid_target":
		respond(w, pageRefused)
		waiting.finish(callbackResult{err: targetRejected(waiting.resource)})
	case waiting.silent && needsThePerson(refusal):
		// The tab is kept, and the login asks again in it with a prompt.
		t.hold(w, r, func() { waiting.finish(callbackResult{interactive: true}) })
	case refusal != "":
		// The provider's error code is not echoed to the terminal; what the
		// user must do is the same whichever of these refusals it was.
		respond(w, pageRefused)
		waiting.finish(callbackResult{err: notCompleted(
			"the identity provider refused this login",
			"Retry wso2 login and complete the sign-in the browser asks for.")})
	case query.Get("code") == "":
		respond(w, pageNoCode)
	default:
		code := query.Get("code")
		t.hold(w, r, func() {
			if waiting.label != "" {
				t.mutex.Lock()
				t.products = append(t.products, waiting.label)
				t.mutex.Unlock()
			}
			waiting.finish(callbackResult{code: code})
		})
	}
}

// needsThePerson reports whether a refusal of a silent authorization means
// only that the provider wants the person, which an authorization with a
// prompt gives it.
func needsThePerson(refusal string) bool {
	switch refusal {
	case "login_required", "interaction_required", "consent_required", "account_selection_required":
		return true
	}
	return false
}

// hold makes this response the tab's held page, delivers the callback's
// outcome, and answers with whatever comes next: the next authorization URL,
// or the signed-in page.
func (t *Tab) hold(w http.ResponseWriter, r *http.Request, deliver func()) {
	page := &heldPage{next: make(chan string, 1), done: make(chan struct{})}
	defer close(page.done)
	t.mutex.Lock()
	previous := t.held
	t.held = page
	t.mutex.Unlock()
	if previous != nil {
		previous.next <- ""
	}
	deliver()

	timer := time.NewTimer(holdLimit)
	defer timer.Stop()
	var next string
	select {
	case next = <-page.next:
	case <-timer.C:
		next = t.release(page)
	case <-r.Context().Done():
		next = t.release(page)
	}
	if next != "" {
		http.Redirect(w, r, next, http.StatusFound)
		return
	}
	t.mutex.Lock()
	products := append([]string(nil), t.products...)
	t.mutex.Unlock()
	respond(w, pageSignedIn, products...)
}

// release takes a held page back from the tab when it stops waiting, unless
// a login already took it, in which case that login's URL is on its way.
func (t *Tab) release(page *heldPage) string {
	t.mutex.Lock()
	if t.held == page {
		t.held = nil
		t.mutex.Unlock()
		return ""
	}
	t.mutex.Unlock()
	return <-page.next
}

// wait blocks until the browser comes back for this authorization or the
// caller gives up.
func (c *callback) wait(ctx context.Context) (callbackResult, error) {
	select {
	case result := <-c.results:
		return result, result.err
	case <-ctx.Done():
		return callbackResult{}, notCompleted("the browser login did not complete",
			"Run wso2 login again and finish the sign-in in the browser, or open the printed URL yourself.")
	}
}
