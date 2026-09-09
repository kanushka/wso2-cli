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

package contexts

import (
	"fmt"
	"net/url"
	"strings"
)

// nameLimit is how long a derived name may be. It is namePattern's own bound,
// written here because a host longer than this has to be cut before it is
// checked rather than refused for a length nobody chose.
const nameLimit = 64

// IdentityNameForIssuer derives an identity name from an issuer URL.
//
// The rule is deliberately mechanical — take the host, drop the port, lower-case
// it, and replace each label separator with a hyphen — because the name is
// written into a document the user later reads and types, and a rule they
// cannot predict is worse than a name they would not have chosen. #112 D6
// requires only that the name be derived from the issuer host and that login
// report the name it assigned.
//
// An Asgardeo issuer is the exception the host rule cannot serve: every tenant
// shares the api.asgardeo.io host, so a host-derived name identifies the
// vendor, and a second organization's login would collide with the first's
// (the identity_exists refusal is right to fire there; what was wrong was two
// tenants deriving one name). On that host the tenant in the issuer's
// /t/<tenant>/ path is what distinguishes the login, so the derived name is
// <tenant>-asgardeo — the tenant leads, the way the documented examples name
// their contexts (docs/examples/login-walkthroughs.md's acme-dev, acme-prod),
// and the vendor trails saying where it lives. Every other issuer keeps the
// host rule exactly.
//
// Not every host, and not every tenant, makes a legal name. The document
// requires a name starting with a lower-case letter, so an issuer at a bare IP
// address, at a host whose first label starts with a digit, or in a tenant
// whose name starts with one yields a typed refusal rather than a mangled
// name; --context is how a user supplies one directly. The check is ValidName,
// the same function the document validates a hand-written name with, so the
// command and the document cannot disagree about what is legal.
func IdentityNameForIssuer(issuer string) (string, error) {
	parsed, err := url.Parse(issuer)
	if err != nil || parsed.Hostname() == "" {
		// The issuer rather than the host, because there is no host to name:
		// what the user typed is the only thing they can go and correct.
		return "", underivableIdentityName(issuer)
	}
	derivedFrom := parsed.Hostname()
	name := strings.ToLower(strings.ReplaceAll(derivedFrom, ".", "-"))
	if tenant := TenantForIssuer(issuer); tenant != "" {
		derivedFrom = tenant
		name = sanitizedNamePart(tenant) + "-asgardeo"
	}
	if len(name) > nameLimit {
		name = name[:nameLimit]
	}
	if !ValidName(name) {
		return "", underivableIdentityName(derivedFrom)
	}
	return name, nil
}

// sanitizedNamePart lower-cases one derived word and replaces each character a
// name may not carry with a hyphen — the same mechanical spirit as the host
// rule's dot replacement, applied to a value the user did not spell as a host.
// What it cannot repair, a leading character no name may start with, is left
// for ValidName to refuse.
func sanitizedNamePart(part string) string {
	var sanitized strings.Builder
	for _, r := range strings.ToLower(part) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			sanitized.WriteRune(r)
		} else {
			sanitized.WriteRune('-')
		}
	}
	return sanitized.String()
}

// underivableIdentityName refuses an issuer whose host cannot become a name.
//
// It is a usage problem because the way out is a flag the user types, not a
// state they have to repair: --context names the identity and the context
// directly, which is the same flag that names them when the derivation would
// have succeeded.
func underivableIdentityName(from string) error {
	return contextProblem("contexts.identity_name_underivable",
		fmt.Sprintf("no identity name can be derived from %q", from),
		fmt.Sprintf("Name the identity yourself with --context <name>. A name is %s.", NameRule))
}
