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

package main

import (
	"net/http"
	"net/url"
	"testing"
)

// The access token follows a redirect only where the shell would send it in
// the first place: HTTPS, or plain HTTP on loopback.
func TestTheBearerClientFollowsRedirectsOnlyToSecureURLs(t *testing.T) {
	for target, allowed := range map[string]bool{
		"https://product.example/next": true,
		"http://127.0.0.1:9443/next":   true,
		"http://localhost/next":        true,
		"http://[::1]/next":            true,
		"http://product.example/next":  false,
		"ftp://product.example/next":   false,
	} {
		parsed, err := url.Parse(target)
		if err != nil {
			t.Fatal(err)
		}
		err = bearerClient.CheckRedirect(&http.Request{URL: parsed}, nil)
		if (err == nil) != allowed {
			t.Errorf("%s: err = %v, want allowed = %v", target, err, allowed)
		}
	}
}
