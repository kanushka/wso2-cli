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

// Package trustedhttp holds the HTTP client policy for traffic to an issuer.
// It is apart from issuertrust so that packages which only validate URLs, such
// as contexts, do not depend on net/http.
package trustedhttp

import (
	"errors"
	"net/http"

	"github.com/wso2/wso2-cli/internal/auth/issuertrust"
)

// Client returns a copy of client that refuses to follow a redirect to a URL
// Secure rejects. A 307 or 308 from an HTTPS endpoint resends the request's
// body, and with it a refresh token, an authorization code, or a client
// secret, so checking the endpoint a request starts at is not enough: the
// place it ends must pass the same rule. A nil client stands for
// http.DefaultClient. The client's own redirect policy, or the standard limit
// of ten when it has none, still applies after this one.
func Client(client *http.Client) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}
	guarded := *client
	next := client.CheckRedirect
	guarded.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if !issuertrust.Secure(request.URL) {
			return errPlaintextRedirect
		}
		if next != nil {
			return next(request, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	return &guarded
}

// errPlaintextRedirect names no URL, for the same reason no refused URL is
// echoed.
var errPlaintextRedirect = errors.New("refused a redirect to a URL not served over HTTPS")
