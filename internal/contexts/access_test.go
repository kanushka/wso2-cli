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

package contexts_test

import (
	"slices"
	"testing"

	"github.com/wso2/wso2-cli/internal/contexts"
)

func thunderIdentity() contexts.Account {
	return contexts.Account{
		Name: "thunder", Type: "onprem",
		Auth: contexts.AccountAuth{
			Kind: contexts.KindOAuthBrowser, Issuer: "http://localhost:8492",
			ClientID: "wso2-cli", CredentialRef: "thunder", Provider: contexts.ProviderThunder,
		},
		Products: map[string]contexts.Product{
			"iam": {Endpoint: "http://localhost:8492", Audience: "https://localhost:8090/mcp",
				Scopes: []string{"system"}},
			"gateway": {Endpoint: "https://localhost:8243", Audience: "http://localhost:18080/mockapi",
				Scopes: []string{"orders:read"}},
			"apim": {Endpoint: "https://localhost:9443", Audience: "DgP2V4Arw9KYeo2ltIm4r8r19vca",
				Scopes: []string{"apim:api_view"},
				Grant: &contexts.Grant{Kind: contexts.GrantFederated,
					Issuer: "https://localhost:9443/oauth2/token", ClientID: "DgP2V4Arw9KYeo2ltIm4r8r19vca"}},
		},
	}
}

func TestTheLoginProductIsTheFirstDirectProductByNamespace(t *testing.T) {
	access := thunderIdentity().LoginAccess()
	if access.Namespace != "gateway" || access.Strategy != contexts.StrategyDirect {
		t.Fatalf("login access = %+v, want the gateway product, direct", access)
	}
	if access.SessionRef != "thunder" || access.Resource != "http://localhost:18080/mockapi" ||
		access.Issuer != "http://localhost:8492" || access.ClientID != "wso2-cli" {
		t.Fatalf("login access = %+v", access)
	}
}

func TestASecondResourceIsASiblingWithItsOwnSession(t *testing.T) {
	access, ok := thunderIdentity().Access("iam")
	if !ok || access.Strategy != contexts.StrategySibling {
		t.Fatalf("iam access = %+v, %v", access, ok)
	}
	if access.SessionRef != "thunder.iam" || access.Resource != "https://localhost:8090/mcp" ||
		!slices.Equal(access.Scopes, []string{"system"}) {
		t.Fatalf("iam access = %+v", access)
	}
}

func TestAFederatedGrantIsReachedAtItsOwnIssuerAsItsOwnClient(t *testing.T) {
	access, _ := thunderIdentity().Access("apim")
	if access.Strategy != contexts.StrategyFederated || access.Issuer != "https://localhost:9443/oauth2/token" ||
		access.ClientID != "DgP2V4Arw9KYeo2ltIm4r8r19vca" || access.SessionRef != "thunder.apim" ||
		access.Resource != "" || !slices.Equal(access.Scopes, []string{"apim:api_view"}) ||
		access.Audience != "DgP2V4Arw9KYeo2ltIm4r8r19vca" {
		t.Fatalf("apim access = %+v", access)
	}
}

func TestAJWTBearerGrantIsDerivedFromItsOwnAssertionSession(t *testing.T) {
	identity := thunderIdentity()
	identity.Products["apim"] = contexts.Product{Endpoint: "https://localhost:9443", Audience: "cid",
		Scopes: []string{"apim:api_view"},
		Grant: &contexts.Grant{Kind: contexts.GrantJWTBearer, Issuer: "https://localhost:9443/oauth2/token",
			ClientID: "cid", Scopes: []string{"email", "groups"}, Resource: "https://localhost:9443/oauth2/token"}}
	access, _ := identity.Access("apim")
	if access.Strategy != contexts.StrategyDerived || access.Issuer != "http://localhost:8492" ||
		access.ClientID != "wso2-cli" || access.SessionRef != "thunder.apim" ||
		access.Resource != "https://localhost:9443/oauth2/token" ||
		!slices.Equal(access.Scopes, []string{"email", "groups", "openid"}) {
		t.Fatalf("derived access = %+v", access)
	}
}

func TestAProductSharingTheLoginScopeSetIsDirect(t *testing.T) {
	identity := contexts.Account{Name: "is", Type: "onprem",
		Auth: contexts.AccountAuth{Kind: contexts.KindOAuthBrowser, Issuer: "https://is.example",
			ClientID: "wso2-cli", CredentialRef: "is"},
		Products: map[string]contexts.Product{
			"a": {Endpoint: "https://a.example", Audience: "a", Scopes: []string{"x", "y"}},
			"b": {Endpoint: "https://b.example", Audience: "b", Scopes: []string{"y", "x"}},
			"c": {Endpoint: "https://c.example", Audience: "c", Scopes: []string{"x"}},
		}}
	b, _ := identity.Access("b")
	c, _ := identity.Access("c")
	if b.Strategy != contexts.StrategyDirect || b.SessionRef != "is" {
		t.Fatalf("b = %+v, want direct on the login session", b)
	}
	if c.Strategy != contexts.StrategySibling || c.SessionRef != "is.c" {
		t.Fatalf("c = %+v, want a sibling", c)
	}
}

func TestAccessesListsTheLoginSessionFirstThenEachOtherSession(t *testing.T) {
	var names []string
	for _, access := range thunderIdentity().Accesses() {
		names = append(names, access.Namespace+":"+access.Strategy)
	}
	want := []string{"gateway:direct", "apim:federated", "iam:sibling"}
	if !slices.Equal(names, want) {
		t.Fatalf("accesses = %v, want %v", names, want)
	}
}

func TestAnIdentityWithoutProductsStillHasALoginAccess(t *testing.T) {
	identity := thunderIdentity()
	identity.Auth.Provider = ""
	identity.Products = nil
	access := identity.LoginAccess()
	if access.Namespace != "" || access.Strategy != contexts.StrategyDirect ||
		access.SessionRef != "thunder" || len(access.Scopes) != 0 || access.Resource != "" {
		t.Fatalf("login access = %+v", access)
	}
	if got := identity.Accesses(); len(got) != 1 {
		t.Fatalf("accesses = %+v, want the login access alone", got)
	}
}

func TestAClientCredentialsIdentityMintsEveryProductInline(t *testing.T) {
	identity := thunderIdentity()
	identity.Auth.Kind = contexts.KindClientCredentials
	identity.Auth.CredentialRef = ""
	identity.Auth.ClientSecretVariable = "WSO2_CI_SECRET"
	for _, access := range identity.Accesses() {
		if access.Strategy != contexts.StrategyInline || access.SessionRef != "" {
			t.Fatalf("%s = %+v, want inline with no session", access.Namespace, access)
		}
	}
	apim, _ := identity.Access("apim")
	if apim.Issuer != "https://localhost:9443/oauth2/token" {
		t.Fatalf("a federated product is minted at its own issuer, got %+v", apim)
	}
}

func TestAProductCredentialIsBothVariablesOrNeither(t *testing.T) {
	identity := thunderIdentity()
	identity.Auth.Kind = contexts.KindClientCredentials
	identity.Auth.CredentialRef = ""
	identity.Auth.ClientSecretVariable = "WSO2_CI_SECRET"
	identity.Products["apim"] = contexts.Product{Endpoint: "https://localhost:9443", Audience: "https://localhost:9443/apim",
		ClientIDVariable: "WSO2_APIM_CLIENT_ID"}
	document := contexts.Document{SchemaVersion: contexts.SchemaVersion, Accounts: []contexts.Account{identity},
		Contexts: []contexts.Context{{Name: "ci", Identity: "thunder"}}, DefaultContext: "ci"}
	if _, err := document.Encode(); err == nil {
		t.Fatal("a product naming only a client id variable was accepted")
	}
	identity.Products["apim"] = contexts.Product{Endpoint: "https://localhost:9443", Audience: "https://localhost:9443/apim",
		ClientIDVariable: "WSO2_APIM_CLIENT_ID", ClientSecretVariable: "WSO2_APIM_CLIENT_SECRET"}
	if _, err := document.Encode(); err != nil {
		t.Fatalf("a product naming both variables was refused: %v", err)
	}
	identity.Auth.Kind = contexts.KindOAuthBrowser
	identity.Auth.CredentialRef = "thunder"
	identity.Auth.ClientSecretVariable = ""
	// Auth is a plain struct, not a map: the document copied it by value when
	// built above, so the mutation above has to be re-applied to the document
	// itself, not just to the local identity variable, to be seen.
	document.Accounts = []contexts.Account{identity}
	if _, err := document.Encode(); err == nil {
		t.Fatal("a product credential on a browser identity was accepted")
	}
}

// TestAPinnedLoginProductKeepsTheLoginSessionWhereItWas is the drift case:
// recording a product that sorts before the login product would otherwise
// move the login session from under every session already stored.
func TestAPinnedLoginProductKeepsTheLoginSessionWhereItWas(t *testing.T) {
	identity := thunderIdentity()
	delete(identity.Products, "gateway")
	if access := identity.LoginAccess(); access.Namespace != "iam" {
		t.Fatalf("login access = %+v, want iam", access)
	}
	identity.Products["gateway"] = contexts.Product{Endpoint: "https://localhost:8243",
		Audience: "http://localhost:18080/mockapi", Scopes: []string{"orders:read"}}
	if access := identity.LoginAccess(); access.Namespace != "gateway" {
		t.Fatalf("without a pin the login product is %q, want the namespace order", access.Namespace)
	}
	identity.LoginProduct = "iam"
	access := identity.LoginAccess()
	if access.Namespace != "iam" || access.Resource != "https://localhost:8090/mcp" ||
		access.SessionRef != "thunder" {
		t.Fatalf("pinned login access = %+v", access)
	}
	gateway, _ := identity.Access("gateway")
	if gateway.Strategy != contexts.StrategySibling || gateway.SessionRef != "thunder.gateway" {
		t.Fatalf("the earlier-sorting product is %+v, want a sibling", gateway)
	}
	iam, _ := identity.Access("iam")
	if iam.Strategy != contexts.StrategyDirect || iam.SessionRef != "thunder" {
		t.Fatalf("the pinned product is %+v, want direct", iam)
	}
}

func TestAPinNamingNoDirectProductFallsBackToTheNamespaceOrder(t *testing.T) {
	identity := thunderIdentity()
	identity.LoginProduct = "apim"
	if access := identity.LoginAccess(); access.Namespace != "gateway" {
		t.Fatalf("login access = %+v, want the namespace order", access)
	}
	identity.LoginProduct = "nosuch"
	if access := identity.LoginAccess(); access.Namespace != "gateway" {
		t.Fatalf("login access = %+v, want the namespace order", access)
	}
}

func TestAPinNamingAnUnreachableProductIsMalformed(t *testing.T) {
	for _, pin := range []string{"apim", "nosuch"} {
		identity := thunderIdentity()
		identity.LoginProduct = pin
		document := contexts.Document{SchemaVersion: contexts.SchemaVersion, DefaultContext: "thunder",
			Accounts: []contexts.Account{identity},
			Contexts: []contexts.Context{{Name: "thunder", Identity: "thunder"}}}
		root := t.TempDir()
		if err := contexts.Save(root, document); err == nil {
			t.Errorf("a pin naming %q was written", pin)
		}
	}
}

func TestAnExchangeGrantIsReachedAtTheLoginIssuerWithTheProductAudienceAsItsResource(t *testing.T) {
	identity := thunderIdentity()
	identity.Products["apip"] = contexts.Product{Endpoint: "http://localhost:9251",
		Audience: "http://localhost:9251", Grant: &contexts.Grant{Kind: contexts.GrantExchange}}
	access, ok := identity.Access("apip")
	if !ok || access.Strategy != contexts.StrategyExchanged {
		t.Fatalf("apip access = %+v, %v, want an exchanged strategy", access, ok)
	}
	// The exchange runs at the identity's own issuer, as the identity's own
	// client: it is the login session that is exchanged, so nothing about
	// the product's own deployment takes part in obtaining the token.
	if access.Issuer != "http://localhost:8492" || access.ClientID != "wso2-cli" {
		t.Fatalf("apip access = %+v, want the login issuer and client", access)
	}
	// The product's audience is what the exchange asks for as its resource,
	// and it is what the returned token must be bound to.
	if access.Resource != "http://localhost:9251" || access.Audience != "http://localhost:9251" {
		t.Fatalf("apip access = %+v, want the product audience as the resource", access)
	}
	// An exchanged product stores no session of its own: its token is minted
	// from the login session on demand and never outlives the command.
	if access.SessionRef != "" {
		t.Fatalf("apip access = %+v, want no session reference of its own", access)
	}
}

func TestALoginRunsNoAuthorizationForAnExchangedProduct(t *testing.T) {
	// An exchanged product is reached by exchanging the login session, so a
	// login that authorized one would be opening a browser for a product that
	// never needed it — which is the whole of what the strategy buys.
	identity := contexts.Account{Name: "thunder", Type: "onprem",
		Auth: contexts.AccountAuth{Kind: contexts.KindOAuthBrowser, Issuer: "http://localhost:8501",
			ClientID: "wso2-cli", CredentialRef: "thunder", Provider: contexts.ProviderThunder},
		Products: map[string]contexts.Product{
			"thunder": {Endpoint: "http://localhost:8501", Audience: "https://localhost:8090/mcp",
				Scopes: []string{"system"}},
			"apip": {Endpoint: "http://localhost:9251", Audience: "http://localhost:9251",
				Grant: &contexts.Grant{Kind: contexts.GrantExchange}},
		}}
	accesses := identity.Accesses()
	if len(accesses) != 1 {
		t.Fatalf("a login would run %d authorizations, want only the login one: %+v", len(accesses), accesses)
	}
	if accesses[0].Namespace != "thunder" || accesses[0].Strategy != contexts.StrategyDirect {
		t.Fatalf("the one authorization is %+v, want the direct login product", accesses[0])
	}
}
