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
	"errors"
	"slices"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/modules"
)

const (
	firstAudience  = "https://first.example/api"
	secondAudience = "https://second.example/api"
)

// twoAudienceDoc is a ThunderID context whose two direct products are bound to
// different resource servers, as WSO2 Cloud's products are.
func twoAudienceDoc(issuerURL string) contexts.Document {
	document := browserDoc(issuerURL)
	document.Contexts[0].Login.Provider = contexts.ProviderThunder
	document.Contexts[0].Products = map[string]contexts.Product{
		"first":  {Endpoint: "https://first.example", Audience: firstAudience, Scopes: []string{"first:read"}},
		"second": {Endpoint: "https://second.example", Audience: secondAudience, Scopes: []string{"second:read", "second:write"}},
	}
	return document
}

// productBroker is the broker a module of the named product would get in a
// command run after the login.
func productBroker(t *testing.T, shell app.Shell, issuer *fakeissuer.Issuer, namespace string) *auth.Broker {
	t.Helper()
	document, err := contexts.Load(shell.StateRoot)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := document.Select("")
	if err != nil {
		t.Fatal(err)
	}
	product := selected.Identity.Products[namespace]
	return &auth.Broker{
		Namespace:    namespace,
		Capabilities: modules.Capabilities{AuthAudiences: []string{product.Audience}, AuthScopes: product.Scopes},
		Selection:    selected,
		InvocationID: "invocation-" + namespace,
		StateRoot:    shell.StateRoot,
		HTTPClient:   issuer.HTTPClient(),
	}
}

// One sign-in on a ThunderID context reaches two products bound to different
// resource servers: the login runs one authorization per product, and a
// module of each receives a token bound to its own product's audience with
// exactly its own product's scopes.
func TestOneLoginReachesTwoProductsWithDifferentAudiences(t *testing.T) {
	keyring.MockInit()
	issuer := fakeissuer.New(t, fakeissuer.Options{RequireResource: true})
	shell, _, errOut := newLoginShell(t)
	installLogin(t, shell, twoAudienceDoc(issuer.URL))
	followBrowser(&shell)

	if code := shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	store := session.Store{StateRoot: shell.StateRoot}
	first, err := store.Load(credentialRef)
	if err != nil || first.Strategy != contexts.StrategyDirect || first.Resource != firstAudience {
		t.Fatalf("login session %+v, %v; want direct, bound to %s", first, err, firstAudience)
	}
	second, err := store.Load(contexts.ProductSessionRef(credentialRef, "second"))
	if err != nil || second.Strategy != contexts.StrategySibling || second.Resource != secondAudience {
		t.Fatalf("second session %+v, %v; want sibling, bound to %s", second, err, secondAudience)
	}

	for namespace, want := range map[string]struct {
		audience string
		scopes   []string
	}{
		"first":  {firstAudience, []string{"first:read"}},
		"second": {secondAudience, []string{"second:read", "second:write"}},
	} {
		grant, err := productBroker(t, shell, issuer, namespace).Acquire(auth.Request{Audience: want.audience})
		if err != nil {
			t.Fatalf("%s: Acquire: %v", namespace, err)
		}
		active, scopes, audiences := issuer.Introspect(t, grant.Token)
		if !active || !slices.Equal(audiences, []string{want.audience}) {
			t.Errorf("%s: token active=%v aud=%v, want %s", namespace, active, audiences, want.audience)
		}
		var permissions []string
		for _, scope := range scopes {
			if !slices.Contains([]string{"openid", "offline_access", "profile", "email"}, scope) {
				permissions = append(permissions, scope)
			}
		}
		slices.Sort(permissions)
		if !slices.Equal(permissions, want.scopes) {
			t.Errorf("%s: token scopes %v, want %v", namespace, scopes, want.scopes)
		}
	}
}

// ThunderID answers an authorization asking for a permission the user holds
// no role for with a token that leaves it out. The login does not notice: it
// stores the session as asked for. The module's command is where it surfaces:
// the shell authorizes the product once more, and refuses when the second
// authorization is answered the same way.
func TestAWithheldScopeSurfacesAtTheCommandNotTheLogin(t *testing.T) {
	keyring.MockInit()
	issuer := fakeissuer.New(t, fakeissuer.Options{
		RequireResource: true, WithheldScopes: []string{"second:write"},
	})
	shell, _, errOut := newLoginShell(t)
	installLogin(t, shell, twoAudienceDoc(issuer.URL))
	followBrowser(&shell)

	if code := shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	stored, err := session.Store{StateRoot: shell.StateRoot}.Load(contexts.ProductSessionRef(credentialRef, "second"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(stored.Scopes, "second:write") {
		t.Fatalf("stored scopes %v; the login was expected to record what it asked for", stored.Scopes)
	}

	broker := productBroker(t, shell, issuer, "second")
	reauthorized := 0
	broker.EstablishSession = func(contexts.ProductAccess) error { reauthorized++; return nil }
	_, err = broker.Acquire(auth.Request{Audience: secondAudience})
	var refused auth.Denial
	if !errors.As(err, &refused) || refused.Problem.Code != "auth.narrowing_unavailable" {
		t.Fatalf("Acquire = %v, want auth.narrowing_unavailable", err)
	}
	if reauthorized != 1 {
		t.Errorf("the product was authorized again %d times, want once", reauthorized)
	}
}
