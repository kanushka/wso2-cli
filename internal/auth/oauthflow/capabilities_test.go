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

package oauthflow_test

import (
	"context"
	"testing"

	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/oauthflow"
)

func TestOffersDeviceSecretReadsTheAdvertisedScopes(t *testing.T) {
	for name, testCase := range map[string]struct {
		options fakeissuer.Options
		want    bool
	}{
		"listed":     {fakeissuer.Options{DeviceSSO: true}, true},
		"not listed": {fakeissuer.Options{}, false},
	} {
		t.Run(name, func(t *testing.T) {
			issuer := fakeissuer.New(t, testCase.options)
			got, err := oauthflow.OffersDeviceSecret(context.Background(), issuer.URL, "")
			if err != nil || got != testCase.want {
				t.Errorf("OffersDeviceSecret = %v, %v; want %v", got, err, testCase.want)
			}
		})
	}
}

func TestOffersDeviceSecretRefusesScopesThatAreNotAList(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{ScopesSupportedNotAList: true})
	if got, err := oauthflow.OffersDeviceSecret(context.Background(), issuer.URL, ""); err == nil {
		t.Errorf("OffersDeviceSecret = %v with no error, for a scopes_supported that is not a list", got)
	}
}
