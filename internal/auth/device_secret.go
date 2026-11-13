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
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/wso2/wso2-cli/internal/auth/trustedhttp"
	"github.com/wso2/wso2-cli/internal/contexts"
)

const jwtTokenType = "urn:ietf:params:oauth:token-type:jwt"

// ErrDeviceSecretSpent is a device secret the provider no longer answers: the
// browser session it describes has ended, it belongs to another user, or the
// provider cannot read it. The secret is discarded and the product is
// authorized through the browser instead.
var ErrDeviceSecretSpent = errors.New("auth: the identity provider no longer accepts the device secret")

// DeviceSecretAuthorization is what exchanging a device secret for one
// product yields: an authorization of the product's own, with a refresh token
// that renews it like one a browser authorization returned.
type DeviceSecretAuthorization struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	// Granted is the scopes the provider issued, which may be fewer than were
	// asked for: ThunderID leaves out those the user's roles do not carry.
	Granted []string
	// RefreshTokenExpiresIn is the refresh token's own lifetime in seconds,
	// zero when the provider stated none.
	RefreshTokenExpiresIn int64
}

// DeviceSecretExpiry is when a device secret stops working, read from its
// exp claim, or the zero time when it states none.
func DeviceSecretExpiry(secret string) time.Time {
	facts, err := bearerClaims(secret)
	if err != nil {
		return time.Time{}
	}
	return facts.ExpiresAt
}

// ExchangeDeviceSecret obtains access's product session from a device secret
// rather than a browser authorization: a token exchange at the login issuer
// presenting the secret, naming the product's resource and exactly its
// scopes. The token is proved bound to the product's audience before it is
// returned, so a session is never stored for something it was not issued
// for.
func ExchangeDeviceSecret(ctx context.Context, client *http.Client, access contexts.ProductAccess,
	secret string, now time.Time) (DeviceSecretAuthorization, error) {
	client = trustedhttp.Client(client)
	endpoint, err := tokenEndpoint(ctx, client, access.Issuer, access.IssuerID)
	if err != nil {
		return DeviceSecretAuthorization{}, err
	}
	form := url.Values{
		"grant_type":         {exchangeGrantType},
		"subject_token":      {secret},
		"subject_token_type": {jwtTokenType},
		"resource":           {access.Resource},
		"scope":              {strings.Join(access.Scopes, " ")},
	}
	issued, err := requestToken(ctx, client, endpoint, form, clientAuth{id: access.ClientID})
	if err != nil {
		return DeviceSecretAuthorization{}, refusedDeviceSecret(err, access)
	}
	facts, err := bearerClaims(issued.AccessToken)
	if err != nil || !slices.Contains(facts.Audiences, access.Audience) {
		return DeviceSecretAuthorization{}, denial("auth.exchange_unusable",
			fmt.Sprintf("the identity provider answered the device secret with access for the %q product "+
				"that is not bound to the %q audience this context records for it", access.Namespace,
				access.Audience),
			fmt.Sprintf("Register %q at %s as the resource server for the %q product, then run wso2 "+
				"login --only %s.", access.Audience, access.Issuer, access.Namespace, access.Namespace))
	}
	if issued.RefreshToken == "" {
		return DeviceSecretAuthorization{}, denial("auth.exchange_unusable",
			fmt.Sprintf("the identity provider answered the device secret for the %q product with no "+
				"refresh token, so no session can be stored", access.Namespace),
			fmt.Sprintf("Run wso2 login --only %s.", access.Namespace))
	}
	return DeviceSecretAuthorization{
		AccessToken:           issued.AccessToken,
		RefreshToken:          issued.RefreshToken,
		ExpiresAt:             issued.expiry(facts, now),
		Granted:               GrantedScopes(issued.Scope, issued.AccessToken),
		RefreshTokenExpiresIn: int64(issued.RefreshTokenExpiresIn),
	}, nil
}

// refusedDeviceSecret sorts a refused exchange: the refusals that say the
// secret is no longer good send the caller to the browser, and every other
// one is reported.
func refusedDeviceSecret(err error, access contexts.ProductAccess) error {
	var refusal issuerRefusal
	if !errors.As(err, &refusal) {
		if errors.Is(err, errNoAccessToken) {
			return denial("auth.exchange_unusable",
				fmt.Sprintf("the identity provider accepted the device secret for the %q product and "+
					"returned no access token", access.Namespace),
				fmt.Sprintf("Run wso2 login --only %s.", access.Namespace))
		}
		return issuerUnreachable(err, access.Issuer)
	}
	switch refusal.code {
	case "interaction_required", "invalid_grant", "invalid_request":
		return ErrDeviceSecretSpent
	}
	return denial("auth.exchange_unavailable",
		fmt.Sprintf("the identity provider refused to exchange the device secret for access to the %q "+
			"product (%s)", access.Namespace, refusalCode(refusal)),
		fmt.Sprintf("Run wso2 login --only %s to authorize it through the browser. If the refusal "+
			"persists, the %q application at %s is not registered for the token exchange grant.",
			access.Namespace, access.ClientID, access.Issuer))
}

func refusalCode(refusal issuerRefusal) string {
	if refusal.code == "" {
		return fmt.Sprintf("HTTP %d", refusal.status)
	}
	return refusal.code
}
