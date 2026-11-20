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
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/contexts"
)

func withProductSessions(kind, value string) string {
	return strings.Replace(cloudDocument(kind, nil), `"clientId": "wso2-cli",`,
		`"clientId": "wso2-cli", "productSessions": "`+value+`",`, 1)
}

func TestProductSessionsDefaultToTheExchange(t *testing.T) {
	document, err := contexts.Decode([]byte(cloudDocument(contexts.KindOAuthBrowser, nil)))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := document.Contexts[0].Account().Auth.ProductSessionMethod(); got != contexts.ProductSessionsExchange {
		t.Fatalf("a login stating no product sessions reaches products by %q, want %q",
			got, contexts.ProductSessionsExchange)
	}
}

func TestProductSessionsAreReadOnEveryInteractiveKind(t *testing.T) {
	for _, kind := range []string{contexts.KindOAuthBrowser, contexts.KindOAuthDevice} {
		for _, value := range []string{contexts.ProductSessionsExchange, contexts.ProductSessionsBrowser} {
			t.Run(kind+"/"+value, func(t *testing.T) {
				document, err := contexts.Decode([]byte(withProductSessions(kind, value)))
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				context := document.Contexts[0]
				if context.Login.ProductSessions != value {
					t.Fatalf("login decoded as %+v", context.Login)
				}
				if got := context.Account().Auth.ProductSessionMethod(); got != value {
					t.Fatalf("product sessions by %q, want %q", got, value)
				}
				if round := contexts.FromAccount(context.Name, context.Account()); round.Login.ProductSessions != value {
					t.Fatalf("the setting did not survive the account view: %+v", round.Login)
				}
			})
		}
	}
}

func TestAnUnknownProductSessionMethodIsRefused(t *testing.T) {
	for _, value := range []string{"auto", "device-code", "Exchange"} {
		t.Run(value, func(t *testing.T) {
			_, err := contexts.Decode([]byte(withProductSessions(contexts.KindOAuthBrowser, value)))
			assertProblemCode(t, err, "contexts.document_malformed")
		})
	}
}

func TestProductSessionsAreRefusedOnALoginThatHoldsNoSession(t *testing.T) {
	document := strings.Replace(cloudDocument(contexts.KindClientCredentials, nil), `"clientId": "wso2-cli",`,
		`"clientId": "wso2-cli", "productSessions": "browser",`, 1)
	_, err := contexts.Decode([]byte(document))
	assertProblemCode(t, err, "contexts.document_malformed")
}
