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
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
)

// cloudIssuerID is the identifier a provider names in place of the
// URL it is served from.
const cloudIssuerID = "example-idp"

// issuerIDDoc is browserDoc with the login declaring issuerID.
func issuerIDDoc(issuerURL, issuerID string) contexts.Document {
	document := browserDoc(issuerURL)
	document.Contexts[0].Login.IssuerID = issuerID
	return document
}

// completeInBackground is a browser opener that follows the authorization
// URL, as a person who is already signed in would.
func completeInBackground(authURL string) error {
	go func() {
		response, err := http.Get(authURL)
		if err == nil {
			_ = response.Body.Close()
		}
	}()
	return nil
}

func TestLoginWithAnIssuerIDStoresASessionBoundToTheIssuerURL(t *testing.T) {
	keyring.MockInit()
	issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "reference-status", IssuerIdentifier: cloudIssuerID})
	shell, out, errOut := newLoginShell(t)
	installLogin(t, shell, issuerIDDoc(issuer.URL, cloudIssuerID))
	shell.OpenBrowser = completeInBackground

	if code := shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	stored, err := session.Store{StateRoot: shell.StateRoot}.Load(credentialRef)
	if err != nil {
		t.Fatalf("session not stored: %v", err)
	}
	// The session stays bound to login.issuer, so declaring issuerId never
	// moves or invalidates a session already stored.
	if stored.Issuer != issuer.URL {
		t.Fatalf("stored session names the issuer %q, want %q", stored.Issuer, issuer.URL)
	}
	if !hasField(out.String(), "Email", "dev@example.test") {
		t.Fatalf("the login report does not name the signed-in person:\n%s", out)
	}
}

func TestDeviceLoginWithAnIssuerIDStoresASession(t *testing.T) {
	keyring.MockInit()
	issuer := fakeissuer.New(t, fakeissuer.Options{
		Audience: "reference-status", IssuerIdentifier: cloudIssuerID, DeviceInterval: 1,
	})
	shell, _, errOut := newLoginShell(t)
	document := issuerIDDoc(issuer.URL, cloudIssuerID)
	document.Contexts[0].Login.Kind = contexts.KindOAuthDevice
	installLogin(t, shell, document)

	if code := shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("device login failed: exit %d, stderr %s", code, errOut)
	}
	if _, err := (session.Store{StateRoot: shell.StateRoot}).Load(credentialRef); err != nil {
		t.Fatalf("session not stored: %v", err)
	}
}

func TestLogoutWithAnIssuerIDRevokesAndEndsTheSignOn(t *testing.T) {
	keyring.MockInit()
	issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "reference-status", IssuerIdentifier: cloudIssuerID})
	shell, out, errOut := newLoginShell(t)
	installLogin(t, shell, issuerIDDoc(issuer.URL, cloudIssuerID))
	shell.OpenBrowser = completeInBackground
	refreshToken := issuer.SeedSession([]string{"openid", "reference:status:read"})
	if err := (session.Store{StateRoot: shell.StateRoot}).Save(credentialRef, session.Session{
		Issuer: issuer.URL, RefreshToken: refreshToken, IDToken: "idt-login",
	}); err != nil {
		t.Fatal(err)
	}

	if code := shell.Run([]string{"logout"}); code != exit.OK {
		t.Fatalf("logout failed: exit %d, stderr %s", code, errOut)
	}
	if issuer.RefreshTokenLive(refreshToken) {
		t.Fatalf("the provider still honours the refresh token after logout:\n%s", out)
	}
	waitForLogouts(t, issuer, 1)
}

func TestDoctorOnlineAcceptsAProviderNamingTheDeclaredIssuerID(t *testing.T) {
	keyring.MockInit()
	reachableCatalog(t)
	issuer := fakeissuer.New(t, fakeissuer.Options{IssuerIdentifier: cloudIssuerID})
	shell, out, _ := newLoginShell(t)
	installLogin(t, shell, issuerIDDoc(issuer.URL, cloudIssuerID))
	if err := (session.Store{StateRoot: shell.StateRoot}).Save(credentialRef,
		session.Session{Issuer: issuer.URL, RefreshToken: "rt-1"}); err != nil {
		t.Fatal(err)
	}

	shell.Run([]string{"doctor", "--online", "--output", "json"})
	finding := decodeDoctorReport(t, out.Bytes()).findingFor(t, "issuer")
	if finding.Status != "pass" {
		t.Fatalf("issuer finding = %+v, want pass", finding)
	}
}

// A provider that stops naming a bare identifier and names its own URL leaves
// a declared issuerId stale. Setting issuerId to that URL is refused by the
// document, so the remedy is to remove it.
func TestLoginTellsAStaleIssuerIDToBeRemoved(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	shell, _, errOut := newLoginShell(t)
	installLogin(t, shell, issuerIDDoc(issuer.URL, cloudIssuerID))

	if code := shell.Run([]string{"login"}); code != exit.AuthPolicy {
		t.Fatalf("exit code = %d, want %d (auth policy); stderr: %s", code, exit.AuthPolicy, errOut)
	}
	rendered := errOut.String()
	requireRefusal(t, rendered, "auth.discovery_failed")
	if !strings.Contains(rendered, "Remove login.issuerId") || strings.Contains(rendered, "set login.issuerId") {
		t.Fatalf("the refusal does not say to remove login.issuerId:\n%s", rendered)
	}
	if strings.Contains(rendered, issuer.URL) || strings.Contains(rendered, cloudIssuerID) {
		t.Fatalf("the refusal echoes the context's issuer or issuer identifier:\n%s", rendered)
	}
}

// requireIssuerMismatch asserts the refusal for a provider naming another
// issuer: the discovery code, the identifier the provider names, the remedy,
// and never the context's own issuer URL.
func requireIssuerMismatch(t *testing.T, rendered, issuerURL string) {
	t.Helper()
	requireRefusal(t, rendered, "auth.discovery_failed")
	for _, want := range []string{`names a different issuer, "example-idp"`, "set login.issuerId"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("the refusal does not say %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, issuerURL) {
		t.Fatalf("the refusal echoes the context's issuer:\n%s", rendered)
	}
}

func TestLoginRefusesAProviderNamingAnUndeclaredIssuerID(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{IssuerIdentifier: cloudIssuerID})
	shell, _, errOut := newLoginShell(t)
	installLogin(t, shell, browserDoc(issuer.URL))

	if code := shell.Run([]string{"login"}); code != exit.AuthPolicy {
		t.Fatalf("exit code = %d, want %d (auth policy); stderr: %s", code, exit.AuthPolicy, errOut)
	}
	requireIssuerMismatch(t, errOut.String(), issuer.URL)
}

func TestLoginRefusesAProviderNamingAnotherIssuerThanTheDeclaredOne(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{IssuerIdentifier: cloudIssuerID})
	shell, _, errOut := newLoginShell(t)
	installLogin(t, shell, issuerIDDoc(issuer.URL, "other-idp"))

	if code := shell.Run([]string{"login"}); code != exit.AuthPolicy {
		t.Fatalf("exit code = %d, want %d (auth policy); stderr: %s", code, exit.AuthPolicy, errOut)
	}
	requireIssuerMismatch(t, errOut.String(), issuer.URL)
	if strings.Contains(errOut.String(), "other-idp") {
		t.Fatalf("the refusal echoes the context's declared issuer:\n%s", errOut)
	}
}

func TestLoginWithAnIssuerIDStillRefusesAPlaintextEndpoint(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{IssuerIdentifier: cloudIssuerID, PlaintextEndpoint: true})
	shell, _, errOut := newLoginShell(t)
	installLogin(t, shell, issuerIDDoc(issuer.URL, cloudIssuerID))

	if code := shell.Run([]string{"login"}); code != exit.AuthPolicy {
		t.Fatalf("exit code = %d, want %d (auth policy); stderr: %s", code, exit.AuthPolicy, errOut)
	}
	requireRefusal(t, errOut.String(), "auth.discovery_failed")
	if !strings.Contains(errOut.String(), "not served over HTTPS") {
		t.Fatalf("the refusal does not name the plaintext endpoint:\n%s", errOut)
	}
}

func TestLoginRefusesAnIdentityTokenFromAnotherIssuer(t *testing.T) {
	keyring.MockInit()
	issuer := fakeissuer.New(t, fakeissuer.Options{
		Audience: "reference-status", IssuerIdentifier: cloudIssuerID, IDTokenIssuer: "other-idp",
	})
	shell, _, errOut := newLoginShell(t)
	installLogin(t, shell, issuerIDDoc(issuer.URL, cloudIssuerID))
	shell.OpenBrowser = completeInBackground

	if code := shell.Run([]string{"login"}); code != exit.AuthPolicy {
		t.Fatalf("exit code = %d, want %d (auth policy); stderr: %s", code, exit.AuthPolicy, errOut)
	}
	requireRefusal(t, errOut.String(), "auth.credential_unavailable")
	if !strings.Contains(errOut.String(), "issued by a different issuer") {
		t.Fatalf("the refusal does not say the identity token's issuer did not match:\n%s", errOut)
	}
	if _, err := (session.Store{StateRoot: shell.StateRoot}).Load(credentialRef); err == nil {
		t.Fatal("a login whose identity token came from another issuer was stored")
	}
}
