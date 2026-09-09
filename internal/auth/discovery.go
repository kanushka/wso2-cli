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

	"github.com/wso2/wso2-cli/internal/auth/issuertrust"
)

// tokenEndpoint resolves an issuer's token endpoint through OpenID discovery.
//
// The endpoint is discovered rather than derived, because the shell's whole
// claim to work against both Asgardeo and a self-hosted Identity Server is that
// it reads the deployment's own configuration instead of assuming a URL shape.
// Discovery also validates that the document belongs to the issuer it was
// fetched from, so a redirected host cannot substitute its own token endpoint.
func tokenEndpoint(ctx context.Context, client *http.Client, issuer string) (string, error) {
	provider, err := discover(ctx, client, issuer)
	if err != nil {
		return "", err
	}
	endpoint := provider.Endpoint().TokenURL
	if endpoint == "" {
		return "", discoveryUnreachable()
	}
	return endpoint, nil
}

// ProbeIssuer reads an issuer's OpenID configuration and reports nothing
// else about it. It is the check wso2 doctor --online runs, and it refuses
// exactly the way a product command's own discovery would, so the two never
// disagree about a deployment.
func ProbeIssuer(ctx context.Context, client *http.Client, issuer string) error {
	_, err := discover(ctx, client, issuer)
	return err
}

// discover fetches the issuer's configuration, telling a certificate this
// machine does not trust apart from every other failure. The provider's own
// error may quote the request that produced it, and the shell renders
// problems verbatim, so it is classified and not carried through.
func discover(ctx context.Context, client *http.Client, issuer string) (*oidc.Provider, error) {
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, client), issuer)
	if err != nil {
		return nil, issuerUnreadable(err, issuer)
	}
	return provider, nil
}

// issuerUnreadable is the denial for a discovery fetch that failed with err.
//
// An untrusted certificate is the one cause worth naming: it is what every
// fresh self-hosted deployment fails with, and the generic recovery sends the
// user to check an issuer that is right and a network that works. Everything
// else stays the closed discovery failure, because guessing among the rest
// would put the shell in the position of explaining a deployment it cannot
// see.
func issuerUnreadable(err error, issuer string) error {
	if issuertrust.Untrusted(err) {
		return Denial{Problem: issuertrust.Problem(issuer)}
	}
	return discoveryUnreachable()
}

// discoveryUnreachable reports an issuer the shell could not read a usable
// configuration from, whether it answered badly or not at all. The recovery is
// the same in either case.
func discoveryUnreachable() error {
	return denial("auth.discovery_failed",
		"the shell could not read the identity provider's OpenID configuration",
		"Check the issuer of the selected context and that this machine can reach it, then retry.")
}
