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
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/contexts"
)

func cloudAccount(products map[string]contexts.Product) contexts.Account {
	return contexts.Account{
		Name: "cloud-dev", Type: contexts.TypeCloud,
		Auth: contexts.AccountAuth{
			Kind: contexts.KindOAuthBrowser, Issuer: "https://idp.example", ClientID: "WSO2_CLI",
			CredentialRef: "cloud-dev", Provider: contexts.ProviderThunder,
		},
		Products: products,
	}
}

func TestAWSO2CloudContextEndsItsPlatformSessionWithTheRest(t *testing.T) {
	account := cloudAccount(map[string]contexts.Product{
		"apim": {Endpoint: "https://apim.example", Audience: "urn:wso2:cloud", Scopes: []string{"ap:api:read"}},
	})

	accesses := account.Accesses()
	last := accesses[len(accesses)-1]
	if last.Namespace != contexts.PlatformKey || last.SessionRef != "cloud-dev.cloud" ||
		last.Strategy != contexts.StrategySibling || !slices.Equal(last.Scopes, contexts.PlatformScopes) {
		t.Errorf("last access = %+v, want the platform's sibling session", last)
	}
	if login := account.LoginAccess(); login.Namespace != "apim" {
		t.Errorf("login access = %+v, want apim's", login)
	}
}

func TestThePlatformIsNeverALoginAccess(t *testing.T) {
	account := cloudAccount(nil).WithPlatform("https://platform.example")

	if login := account.LoginAccess(); login.Namespace == contexts.PlatformKey {
		t.Errorf("login access = %+v; a context with no product signs in to no product", login)
	}
	if access, _ := account.Access(contexts.PlatformKey); access.SessionRef == "cloud-dev" {
		t.Errorf("platform access = %+v; it must not share the login session", access)
	}
}

func TestOnlyAPersonsWSO2CloudSignInHasAPlatform(t *testing.T) {
	onprem := cloudAccount(nil)
	onprem.Type = contexts.TypeOnprem
	machine := cloudAccount(nil)
	machine.Auth.Kind = contexts.KindClientCredentials
	for _, account := range []contexts.Account{onprem, machine} {
		if _, found := account.PlatformAccess(); found {
			t.Errorf("%+v has a platform access", account)
		}
	}
}

func TestAWSO2CloudContextCannotRecordAProductUnderThePlatformsKey(t *testing.T) {
	document := contexts.Document{
		SchemaVersion: contexts.SchemaVersion, DefaultContext: "cloud-dev",
		Contexts: []contexts.Context{{Name: "cloud-dev", Type: contexts.TypeCloud, CredentialRef: "cloud-dev",
			Login: contexts.Login{Kind: contexts.KindOAuthBrowser, Issuer: "https://idp.example",
				ClientID: "WSO2_CLI", Provider: contexts.ProviderThunder},
			Products: map[string]contexts.Product{
				contexts.PlatformKey: {Endpoint: "https://x.example", Audience: "urn:wso2:cloud"},
			}}},
	}
	root := t.TempDir()
	err := contexts.Save(root, document)
	if err == nil || !strings.Contains(err.Error(), "platform") {
		t.Errorf("saving a cloud product on a WSO2 Cloud context: %v, want a refusal naming the platform", err)
	}
}
