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
// Not every host makes a legal name. The document requires a name starting with
// a lower-case letter, so an issuer at a bare IP address or at a host whose
// first label starts with a digit yields a typed refusal rather than a mangled
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
	host := parsed.Hostname()
	name := strings.ToLower(strings.ReplaceAll(host, ".", "-"))
	if len(name) > nameLimit {
		name = name[:nameLimit]
	}
	if !ValidName(name) {
		return "", underivableIdentityName(host)
	}
	return name, nil
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
