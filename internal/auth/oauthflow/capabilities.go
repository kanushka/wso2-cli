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
	"slices"

	oidc "github.com/coreos/go-oidc/v3/oidc"

	"github.com/wso2/wso2-cli/internal/auth/oidcdiscovery"
)

// OffersDeviceSecret reports whether the provider's discovery document lists
// the device_sso scope, which is how ThunderID says it can hand out a device
// secret.
func OffersDeviceSecret(ctx context.Context, issuer, issuerID string) (bool, error) {
	ctx = oidc.ClientContext(ctx, Login{}.httpClient())
	provider, err := oidcdiscovery.Discover(ctx, issuer, issuerID)
	if err != nil {
		return false, err
	}
	var capabilities struct {
		ScopesSupported []string `json:"scopes_supported"`
	}
	_ = provider.Claims(&capabilities)
	return slices.Contains(capabilities.ScopesSupported, scopeDeviceSSO), nil
}
