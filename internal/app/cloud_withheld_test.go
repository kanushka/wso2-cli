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
	"slices"
	"testing"

	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
)

// A user whose role lacks one of a product's recorded scopes still runs the
// product's commands, one after another: a module asking for the record's
// whole set is given what the sign-in was granted, and each refresh asks for
// and is proved against that set.
func TestACloudProductServesCommandsInARowWhenTheSignInWithheldARecordedScope(t *testing.T) {
	login := newCloudLoginWith(t, fakeissuer.Options{
		WithheldScopes: []string{"ap:api:manage"}, RotateRefreshTokens: true, RefreshScopeMode: "honor",
	})
	installCloudAPIM(t, login.shell)
	installCloudIAM(t, login.shell)
	login.run(t, "login")

	for command := 1; command <= 2; command++ {
		broker := productBroker(t, login.shell, login.issuer, "apim")
		grant, err := broker.Acquire(auth.Request{Audience: cloudShared})
		if err != nil {
			t.Fatalf("command %d: %v", command, err)
		}
		_, scopes, _ := login.issuer.Introspect(t, grant.Token)
		var product []string
		for _, scope := range scopes {
			if slices.Contains(cloudAPIMScopes, scope) {
				product = append(product, scope)
			}
		}
		if !slices.Equal(product, []string{"ap:api:view"}) {
			t.Errorf("command %d carried %v, want the granted [ap:api:view]", command, scopes)
		}
	}
}
