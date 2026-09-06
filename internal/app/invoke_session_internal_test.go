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

package app

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/contexts/fixture"
	"github.com/wso2/wso2-cli/internal/output"
)

// sessionEstablisherCredentialRef is this file's own login identity name. It
// cannot reach into login_test.go's package app_test credentialRef, so it
// keeps a small copy of its own rather than reaching across packages.
const sessionEstablisherCredentialRef = "sessionestablisher-login"

// sessionEstablisherDoc is a minimal copy of login_products_test.go's
// thunderDoc, carrying just the two products (iam being the login product
// itself would defeat the point) TestTheSessionEstablisherAnnouncesThenAuthorizesTheProduct
// needs: a login product ("gateway") and a sibling product ("iam") whose
// session the establisher is asked to acquire. That helper lives in package
// app_test and is unreachable from here, which is the whole reason for this
// smaller copy.
func sessionEstablisherDoc(loginIssuer string) contexts.Document {
	return contexts.Document{
		SchemaVersion:  contexts.SchemaVersion,
		DefaultContext: "acme-dev",
		Identities: []contexts.Identity{{
			Name: "acme-cloud",
			Type: "cloud",
			Auth: contexts.IdentityAuth{
				Kind:          contexts.KindOAuthBrowser,
				Provider:      contexts.ProviderThunder,
				Issuer:        loginIssuer,
				ClientID:      "client-123",
				Tenant:        "acme",
				CredentialRef: sessionEstablisherCredentialRef,
			},
			Products: map[string]contexts.Product{
				"gateway": {
					Endpoint: "https://gw.example",
					Audience: "http://localhost:18080/mockapi",
					Scopes:   []string{"orders:read"},
				},
				"iam": {
					Endpoint: loginIssuer,
					Audience: "https://localhost:8090/mcp",
					Scopes:   []string{"system"},
				},
			},
		}},
		Contexts: []contexts.Context{{Name: "acme-dev", Identity: "acme-cloud", Organization: "acme"}},
	}
}

func TestTheSessionEstablisherRefusesUnderNoInput(t *testing.T) {
	t.Setenv("WSO2_NO_INPUT", "1")
	errOut := &bytes.Buffer{}
	shell := Shell{
		StateRoot: t.TempDir(),
		Streams:   output.Streams{Out: &bytes.Buffer{}, Err: errOut},
		OpenBrowser: func(string) error {
			t.Error("the session establisher opened a browser under no-input")
			return nil
		},
	}

	establish := shell.sessionEstablisher(contexts.Selection{}, "iam", false)
	err := establish(contexts.ProductAccess{Namespace: "iam"})

	var refusal auth.Denial
	if !errors.As(err, &refusal) {
		t.Fatalf("error is not an auth.Denial: %v", err)
	}
	if refusal.Problem.Code != "auth.session_required" {
		t.Fatalf("problem code = %q, want auth.session_required", refusal.Problem.Code)
	}
	if !strings.Contains(refusal.Guidance, "wso2 login --only iam") {
		t.Fatalf("guidance does not name the retry command:\n%s", refusal.Guidance)
	}
	if !strings.Contains(refusal.Guidance, "WSO2_NO_INPUT") {
		t.Fatalf("guidance does not name the control that refused:\n%s", refusal.Guidance)
	}
	// The flag, written on the product line, is named as the control instead.
	t.Setenv("WSO2_NO_INPUT", "")
	err = shell.sessionEstablisher(contexts.Selection{}, "iam", true)(contexts.ProductAccess{Namespace: "iam"})
	if !errors.As(err, &refusal) || !strings.Contains(refusal.Guidance, "--no-input") {
		t.Fatalf("under the flag: %v", err)
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr = %q, want nothing written before the refusal", errOut.String())
	}
}

func TestTheSessionEstablisherAnnouncesThenAuthorizesTheProduct(t *testing.T) {
	t.Setenv("WSO2_NO_INPUT", "")
	keyring.MockInit()
	login := fakeissuer.New(t, fakeissuer.Options{RequireResource: true})

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	root := t.TempDir()
	shell := Shell{
		StateRoot: root,
		Streams:   output.Streams{Out: out, Err: errOut},
	}
	shell.OpenBrowser = func(authURL string) error {
		go func() {
			if response, err := http.Get(authURL); err == nil {
				_ = response.Body.Close()
			}
		}()
		return nil
	}

	document := sessionEstablisherDoc(login.URL)
	if err := fixture.WriteV2(root, document); err != nil {
		t.Fatalf("fixture.WriteV2 returned %v", err)
	}
	loaded, err := contexts.Load(root)
	if err != nil {
		t.Fatalf("contexts.Load returned %v", err)
	}
	selection, err := loaded.Select("")
	if err != nil {
		t.Fatalf("Select returned %v", err)
	}
	access, recorded := selection.Identity.Access("iam")
	if !recorded {
		t.Fatal("iam is not a recorded product")
	}

	establish := shell.sessionEstablisher(selection, "iam", false)
	if err := establish(access); err != nil {
		t.Fatalf("establish returned %v", err)
	}

	if !strings.Contains(errOut.String(), `The "iam" product needs to be authorized`) {
		t.Fatalf("stderr does not carry the notice:\n%s", errOut.String())
	}
	if !strings.Contains(errOut.String(), login.URL) {
		t.Fatalf("stderr does not name the issuer:\n%s", errOut.String())
	}

	stored, err := (session.Store{StateRoot: root}).Load(access.SessionRef)
	if err != nil {
		t.Fatalf("iam session not stored: %v", err)
	}
	if stored.Strategy != contexts.StrategySibling {
		t.Fatalf("stored strategy = %q, want %q", stored.Strategy, contexts.StrategySibling)
	}
}
