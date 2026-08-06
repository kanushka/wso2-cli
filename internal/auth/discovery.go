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
	"context"
	"net/http"

	oidc "github.com/coreos/go-oidc/v3/oidc"
)

// tokenEndpoint resolves an issuer's token endpoint through OpenID discovery.
//
// The endpoint is discovered rather than derived, because the shell's whole
// claim to work against both Asgardeo and a self-hosted Identity Server is that
// it reads the deployment's own configuration instead of assuming a URL shape.
// Discovery also validates that the document belongs to the issuer it was
// fetched from, so a redirected host cannot substitute its own token endpoint.
func tokenEndpoint(ctx context.Context, client *http.Client, issuer string) (string, error) {
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, client), issuer)
	if err != nil {
		// The provider's own error may quote the request that produced it, and
		// the shell renders problems verbatim, so it is not carried through.
		return "", discoveryUnreachable()
	}
	endpoint := provider.Endpoint().TokenURL
	if endpoint == "" {
		return "", discoveryUnreachable()
	}
	return endpoint, nil
}

// discoveryUnreachable reports an issuer the shell could not read a usable
// configuration from, whether it answered badly or not at all. The recovery is
// the same in either case, and guessing which one it was would put the shell in
// the position of explaining a deployment it cannot see.
func discoveryUnreachable() error {
	return denial("auth.discovery_failed",
		"the shell could not read the identity provider's OpenID configuration",
		"Check the issuer of the selected context and that this machine can reach it, then retry.")
}
