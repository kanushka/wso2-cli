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

package issuertrust

import (
	"net"
	"net/url"
	"strings"

	"github.com/wso2/wso2-cli/sdk/problem"
)

// Secure reports whether the shell may speak to target at all: over HTTPS,
// or over plain HTTP only when the host is this machine's loopback interface.
//
// It is the one rule for every URL the shell sends a credential to or takes a
// token or key from — the issuer a context names and every endpoint that
// issuer's configuration advertises. A client secret, a refresh token, or a
// key set carried in plaintext across a network is readable and replaceable by
// anyone on the path; on loopback there is no path, which is why a local
// deployment may run without a certificate.
func Secure(target *url.URL) bool {
	if target == nil {
		return false
	}
	switch strings.ToLower(target.Scheme) {
	case "https":
		return true
	case "http":
		return loopback(target.Hostname())
	}
	return false
}

// loopback reports whether host names this machine's loopback interface:
// localhost, 127.0.0.0/8, or ::1. A name is not resolved, so a host that
// merely resolves to loopback is not trusted as one.
func loopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// discoveredEndpoints are the members of an OpenID configuration the shell
// may follow. The issuer member is among them: go-oidc proves it equals the
// URL the document was fetched from, and checking it here keeps an issuer that
// reached discovery without passing through context validation to the rule.
var discoveredEndpoints = []string{
	"issuer",
	"authorization_endpoint",
	"token_endpoint",
	"device_authorization_endpoint",
	"jwks_uri",
	"userinfo_endpoint",
	"revocation_endpoint",
	"introspection_endpoint",
	"end_session_endpoint",
}

// Plaintext reports whether a discovered OpenID configuration names an
// endpoint this shell may not speak to under Secure. claims decodes the
// configuration (an oidc.Provider's Claims method); it is taken as a function
// so this package, which context validation also imports, carries no HTTP
// client. An endpoint that is absent is not a refusal; one that is present and
// cannot be read is.
func Plaintext(claims func(any) error) bool {
	var advertised map[string]any
	if claims(&advertised) != nil {
		return true
	}
	for _, member := range discoveredEndpoints {
		value, present := advertised[member]
		if !present {
			continue
		}
		raw, isString := value.(string)
		if !isString {
			return true
		}
		if raw == "" {
			continue
		}
		parsed, err := url.Parse(raw)
		if err != nil || !Secure(parsed) {
			return true
		}
	}
	return false
}

// PlaintextProblem is the refusal for a configuration Plaintext rejects. It
// shares the discovery failure's code, because that list is closed and the
// deployment's configuration is what has to change, but it says why: the
// generic recovery would send the user to check an issuer that answered.
// No endpoint is echoed, because the shell renders problems verbatim and an
// endpoint is not the user's to have typed.
func PlaintextProblem() problem.Problem {
	return problem.New(problem.CategoryAuthPolicy, "auth.discovery_failed",
		"the identity provider's OpenID configuration names an endpoint that is not served over HTTPS").
		WithRecovery("Configure the deployment to advertise https:// endpoints. Plain http is " +
			"accepted only on a loopback host (localhost, 127.0.0.1, ::1), because the shell sends " +
			"credentials to these endpoints.")
}
