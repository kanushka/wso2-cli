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
	"net/url"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/oauthflow"
)

func TestEndSessionURLNamesTheClientAndTheTokenHint(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	target, err := oauthflow.EndSession{Issuer: issuer.URL, ClientID: "wso2-cli", IDToken: "id.tok.en",
		HTTPClient: issuer.HTTPClient()}.URL(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(target)
	if err != nil || !strings.HasPrefix(target, issuer.URL+"/logout?") {
		t.Fatalf("url %q, %v", target, err)
	}
	if parsed.Query().Get("client_id") != "wso2-cli" || parsed.Query().Get("id_token_hint") != "id.tok.en" {
		t.Fatalf("query %v", parsed.Query())
	}
	if parsed.Query().Has("post_logout_redirect_uri") {
		t.Fatal("a post-logout redirect was asked for; providers refuse an unregistered one")
	}
}

func TestEndSessionURLOmitsAnAbsentTokenHint(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{})
	target, err := oauthflow.EndSession{Issuer: issuer.URL, ClientID: "wso2-cli",
		HTTPClient: issuer.HTTPClient()}.URL(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(target, "id_token_hint") {
		t.Fatalf("url %q carries an empty hint", target)
	}
}
