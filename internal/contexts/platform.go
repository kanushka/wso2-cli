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

package contexts

import "maps"

// PlatformKey is the key a WSO2 Cloud context's platform access is held
// under, as a product would be. No module may take it as its namespace, so
// the session it names can never be a product's.
const PlatformKey = "cloud"

// PlatformAudience is the resource server WSO2 Cloud's platform service is
// authorized under.
const PlatformAudience = "urn:wso2:cloud"

// PlatformScopes are the permissions the platform access asks for. A role
// that lacks one is not refused at sign-in: ThunderID leaves it out of the
// token, and the platform service refuses the call.
var PlatformScopes = []string{"cloud:project:create", "cloud:project:delete", "cloud:project:read"}

// HasPlatform reports whether the account reaches WSO2 Cloud's platform
// service: a WSO2 Cloud context that signs in interactively.
func (i Account) HasPlatform() bool {
	return i.Type == TypeCloud && i.Auth.Provider == ProviderThunder &&
		(i.Auth.Kind == KindOAuthBrowser || i.Auth.Kind == KindOAuthDevice)
}

// WithPlatform is the account with its platform access recorded at endpoint
// beside its products, for a broker to serve. The context is not changed.
func (i Account) WithPlatform(endpoint string) Account {
	products := maps.Clone(i.Products)
	if products == nil {
		products = map[string]Product{}
	}
	products[PlatformKey] = Product{Endpoint: endpoint, Audience: PlatformAudience, Scopes: PlatformScopes}
	i.Products = products
	return i
}

// PlatformAccess is how the platform access is reached, and whether the
// account has one.
func (i Account) PlatformAccess() (ProductAccess, bool) {
	if !i.HasPlatform() {
		return ProductAccess{}, false
	}
	return i.WithPlatform("").Access(PlatformKey)
}
