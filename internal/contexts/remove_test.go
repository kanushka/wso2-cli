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

// removalIdentity is thunderIdentity with one product of every kind a removal
// has to tell apart: the pinned login product, a direct product sharing its
// session, a sibling, a federated product carrying a gateway record, a derived
// product, and an exchanged one.
func removalIdentity() contexts.Account {
	identity := thunderIdentity()
	identity.LoginProduct = "iam"
	delete(identity.Products, "gateway")
	identity.Products["console"] = contexts.Product{Endpoint: "http://localhost:8492",
		Audience: "https://localhost:8090/mcp", Scopes: []string{"system"}}
	identity.Products["api"] = contexts.Product{Endpoint: "https://api.example",
		Audience: "https://api.example", Scopes: []string{"api:read"}}
	apim := identity.Products["apim"]
	apim.Gateway = &contexts.Gateway{Endpoint: "https://localhost:8243",
		Audience: "http://localhost:18090/hello", Scopes: []string{"hello:read"}}
	identity.Products["apim"] = apim
	identity.Products["agent"] = contexts.Product{Endpoint: "https://agent.example", Audience: "agent-cli",
		Grant: &contexts.Grant{Kind: contexts.GrantJWTBearer, Issuer: "https://agent.example/oauth2/token",
			ClientID: "agent-cli", Resource: "https://agent.example"}}
	identity.Products["ai"] = contexts.Product{Endpoint: "https://ai.example", Audience: "https://ai.example",
		Grant: &contexts.Grant{Kind: contexts.GrantExchange}}
	return identity
}

// oneAccount wraps an account in the document a removal is computed over.
func oneAccount(identity contexts.Account) contexts.Document {
	return contexts.Document{Accounts: []contexts.Account{identity}}
}

// unreachedRefs names the sessions a removal of key leaves nothing to reach.
func unreachedRefs(t *testing.T, identity contexts.Account, key string) []string {
	t.Helper()
	next, removed := identity.WithoutRecord(key)
	if !removed {
		t.Fatalf("%q was not removed", key)
	}
	var refs []string
	for _, access := range oneAccount(identity).SessionsUnreachedBy(oneAccount(next)) {
		refs = append(refs, access.SessionRef)
	}
	slices.Sort(refs)
	return refs
}

func TestRemovingAProductTakesItsGatewayRecordWithIt(t *testing.T) {
	next, removed := removalIdentity().WithoutRecord("apim")
	if !removed {
		t.Fatal("apim was not removed")
	}
	if _, recorded := next.Products["apim"]; recorded {
		t.Fatalf("apim survived: %+v", next.Products)
	}
	if slices.Contains(next.RecordKeys(), contexts.GatewayKey("apim")) {
		t.Fatalf("the gateway record survived its product: %v", next.RecordKeys())
	}
}

func TestRemovingAGatewayKeyKeepsTheProductItBelongsTo(t *testing.T) {
	next, removed := removalIdentity().WithoutRecord(contexts.GatewayKey("apim"))
	if !removed {
		t.Fatal("the gateway record was not removed")
	}
	apim, recorded := next.Products["apim"]
	if !recorded || apim.Gateway != nil || apim.Grant == nil || apim.Endpoint != "https://localhost:9443" {
		t.Fatalf("apim is %+v, %v, want its management record without the gateway", apim, recorded)
	}
}

func TestRemovingARecordTheAccountDoesNotHoldRemovesNothing(t *testing.T) {
	for _, key := range []string{"nosuch", contexts.GatewayKey("iam"), contexts.GatewayKey("nosuch")} {
		identity := removalIdentity()
		if _, removed := identity.WithoutRecord(key); removed {
			t.Errorf("%q was reported removed", key)
		}
		if len(identity.Products) != len(removalIdentity().Products) {
			t.Errorf("removing %q changed the account it was asked of", key)
		}
	}
}

func TestRemovingThePinnedLoginProductPinsTheOneTheNamespaceOrderNowChooses(t *testing.T) {
	next, _ := removalIdentity().WithoutRecord("iam")
	if next.LoginProduct != "api" || next.LoginAccess().Namespace != "api" {
		t.Fatalf("pin = %q, login = %q, want api for both", next.LoginProduct, next.LoginAccess().Namespace)
	}
	// With no direct product left there is nothing to pin, and a pin naming
	// one would be a document the shell refuses to write.
	lone := thunderIdentity()
	lone.LoginProduct = "iam"
	delete(lone.Products, "gateway")
	next, _ = lone.WithoutRecord("iam")
	if next.LoginProduct != "" {
		t.Fatalf("pin = %q, want none", next.LoginProduct)
	}
}

func TestRemovingAProductLeavesUnreachedOnlyTheSessionsItHeldItself(t *testing.T) {
	cases := []struct {
		name string
		key  string
		want []string
	}{
		{"sibling", "api", []string{"thunder.api"}},
		{"federated, with its gateway", "apim", []string{"thunder.apim", "thunder.apim/gateway"}},
		{"gateway alone", contexts.GatewayKey("apim"), []string{"thunder.apim/gateway"}},
		{"derived", "agent", []string{"thunder.agent"}},
		// The login session is never among them: the account still logs in
		// through it, whatever product is removed.
		{"direct, sharing the login session", "console", nil},
		{"exchanged, holding no session", "ai", nil},
		// The login product's own session stays; what goes is the sibling
		// that the login now covers, because nothing names its entry any more.
		{"the login product itself", "iam", []string{"thunder.api"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := unreachedRefs(t, removalIdentity(), tc.key); !slices.Equal(got, tc.want) {
				t.Fatalf("unreached = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestASessionAnotherAccountStillReachesIsNotUnreached(t *testing.T) {
	identity := removalIdentity()
	twin := removalIdentity()
	twin.Name = "twin"
	before := contexts.Document{Accounts: []contexts.Account{identity, twin}}
	removed, _ := identity.WithoutRecord("api")
	after := contexts.Document{Accounts: []contexts.Account{removed, twin}}
	if got := before.SessionsUnreachedBy(after); len(got) != 0 {
		t.Fatalf("unreached = %+v, want none: the twin account still reaches the same entry", got)
	}
}

func TestAClientCredentialsAccountLeavesNoSessionUnreached(t *testing.T) {
	identity := contexts.Account{Name: "ci", Type: "cloud",
		Auth: contexts.AccountAuth{Kind: contexts.KindClientCredentials, Issuer: "https://is.example",
			ClientID: "ci", ClientSecretVariable: "WSO2_CI_SECRET"},
		Products: map[string]contexts.Product{
			"api": {Endpoint: "https://api.example", Scopes: []string{"api:read"}},
		}}
	if got := unreachedRefs(t, identity, "api"); len(got) != 0 {
		t.Fatalf("unreached = %v, want none", got)
	}
}
