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

package diag

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

// Key-based redaction cannot see a secret carried inside a value: an
// end-session URL logged as "url" holds the ID token in id_token_hint, and an
// error's text can quote a request URL whole. These are the two shapes a
// secret takes inside a string, masked whatever the attribute is called.

// sensitiveParameters are the query and fragment parameter names masked in a
// URL, beside every name sensitiveKeys masks as an attribute.
var sensitiveParameters = map[string]bool{
	"idtokenhint":     true,
	"assertion":       true,
	"clientassertion": true,
	"subjecttoken":    true,
	"actortoken":      true,
}

var (
	urlPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.\-]*://[^\s"'<>]+`)
	jwtPattern = regexp.MustCompile(`[A-Za-z0-9_-]{2,}\.[A-Za-z0-9_-]{2,}\.[A-Za-z0-9_-]*`)
)

// maskText masks secret parameters of every URL in text, then every JWT.
func maskText(text string) string {
	if strings.Contains(text, "://") {
		text = urlPattern.ReplaceAllStringFunc(text, maskURL)
	}
	if strings.Count(text, ".") >= 2 {
		text = jwtPattern.ReplaceAllStringFunc(text, func(candidate string) string {
			if isJWT(candidate) {
				return Redacted
			}
			return candidate
		})
	}
	return text
}

// maskURL rewrites only the values of secret parameters, leaving the rest of
// the URL byte for byte as it was written.
func maskURL(target string) string {
	rest, fragment, hasFragment := strings.Cut(target, "#")
	base, query, hasQuery := strings.Cut(rest, "?")
	if hasQuery {
		base += "?" + maskParameters(query)
	}
	if hasFragment {
		base += "#" + maskParameters(fragment)
	}
	return base
}

func maskParameters(encoded string) string {
	pairs := strings.Split(encoded, "&")
	for index, pair := range pairs {
		name, value, hasValue := strings.Cut(pair, "=")
		if !hasValue {
			continue
		}
		decodedName, err := url.QueryUnescape(name)
		if err != nil {
			decodedName = name
		}
		decodedValue, err := url.QueryUnescape(value)
		if err != nil {
			decodedValue = value
		}
		normalized := normalizeKey(decodedName)
		if sensitiveKeys[normalized] || sensitiveParameters[normalized] || isJWT(decodedValue) {
			pairs[index] = name + "=" + Redacted
		}
	}
	return strings.Join(pairs, "&")
}

// isJWT reports whether value is a compact JWS or JWT: three base64url
// segments whose first decodes to a JSON header naming an algorithm. The
// header check is what tells a token from a version or a host name.
func isJWT(value string) bool {
	segments := strings.Split(value, ".")
	if len(segments) != 3 || segments[0] == "" || segments[1] == "" {
		return false
	}
	header, err := base64.RawURLEncoding.DecodeString(segments[0])
	if err != nil {
		return false
	}
	var fields map[string]any
	if json.Unmarshal(header, &fields) != nil {
		return false
	}
	_, named := fields["alg"]
	return named
}
