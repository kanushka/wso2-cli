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

package auth

import (
	"errors"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/sdk/problem"
)

func TestAClientCredentialAnswerWithoutAnAccessTokenIsNotUnreachable(t *testing.T) {
	err := clientCredentialsSource{namespace: "reference"}.refusedGrant(errNoAccessToken, "")
	var refused Denial
	if !errors.As(err, &refused) || refused.Problem.Category != problem.CategoryAuthPolicy {
		t.Errorf("refusal = %v, want an auth_policy denial", err)
	}
}

func TestAClientCredentialAnswerWithoutAnAccessTokenIsNotCalledARefusal(t *testing.T) {
	err := clientCredentialsSource{namespace: "reference"}.refusedGrant(errNoAccessToken, "")
	var refused Denial
	if !errors.As(err, &refused) || refused.Problem.Code != "auth.discovery_failed" {
		t.Fatalf("refusal = %v, want auth.discovery_failed", err)
	}
	for _, said := range []string{refused.Problem.Message, refused.Problem.Recovery} {
		if strings.Contains(said, "refused") || strings.Contains(said, "registered") {
			t.Errorf("an answer with no access token is described as a refusal or a registration fault: %q", said)
		}
	}
}
