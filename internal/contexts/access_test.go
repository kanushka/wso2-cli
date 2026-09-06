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

func thunderIdentity() contexts.Identity {
	return contexts.Identity{
		Name: "thunder", Type: "onprem",
		Auth: contexts.IdentityAuth{
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
	identity := contexts.Identity{Name: "is", Type: "onprem",
		Auth: contexts.IdentityAuth{Kind: contexts.KindOAuthBrowser, Issuer: "https://is.example",
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
	document := contexts.Document{SchemaVersion: contexts.SchemaVersion, Identities: []contexts.Identity{identity},
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
	document.Identities = []contexts.Identity{identity}
	if _, err := document.Encode(); err == nil {
		t.Fatal("a product credential on a browser identity was accepted")
	}
}
