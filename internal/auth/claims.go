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
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// bearerFacts are the facts the shell reads out of an access token to steer its
// own policy.
type bearerFacts struct {
	// Audiences are the services the token is bound to.
	Audiences []string
	// Scopes are the permissions it carries.
	Scopes []string
	// ExpiresAt is when it stops being accepted.
	ExpiresAt time.Time
}

// errNotABearerToken reports material the shell cannot read facts out of.
var errNotABearerToken = errors.New("auth: the access token is not a readable JWT")

// bearerClaims reads a JWT's payload without verifying its signature.
//
// Not verifying is deliberate and safe here: the token arrived over the
// issuer's own TLS connection in direct answer to this shell's request, so
// there is no third party whose forgery a signature check would catch. What the
// claims decide is shell policy — whether to hand the module this token at all
// — and the service the module presents it to verifies it properly. A shell
// that verified here would be asserting a second time what it already knows,
// while still leaving the only check that matters to the audience.
func bearerClaims(token string) (bearerFacts, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return bearerFacts{}, errNotABearerToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return bearerFacts{}, errNotABearerToken
	}
	var claims struct {
		Audience any    `json:"aud"`
		Scope    string `json:"scope"`
		Expires  int64  `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return bearerFacts{}, errNotABearerToken
	}
	facts := bearerFacts{
		Audiences: audienceList(claims.Audience),
		Scopes:    strings.Fields(claims.Scope),
	}
	if claims.Expires > 0 {
		facts.ExpiresAt = time.Unix(claims.Expires, 0).UTC()
	}
	return facts, nil
}

// audienceList normalizes the aud claim, which RFC 7519 allows to be either one
// string or an array of them.
func audienceList(claim any) []string {
	switch value := claim.(type) {
	case string:
		if value == "" {
			return nil
		}
		return []string{value}
	case []any:
		audiences := make([]string, 0, len(value))
		for _, entry := range value {
			if name, ok := entry.(string); ok && name != "" {
				audiences = append(audiences, name)
			}
		}
		return audiences
	default:
		return nil
	}
}
