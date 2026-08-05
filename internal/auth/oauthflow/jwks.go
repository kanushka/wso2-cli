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

package oauthflow

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
)

// certificateFields are the members of a JSON Web Key that describe an X.509
// certificate rather than the key itself. x5t and x5t#S256 are thumbprints of
// the chain in x5c, and go-jose checks them against it, so they leave together
// or not at all.
var certificateFields = []string{"x5c", "x5t", "x5t#S256"}

// keyParameters are the members that fully define a public key, by key type.
// A key carrying its own parameters needs nothing from a certificate.
var keyParameters = map[string][]string{
	"RSA": {"n", "e"},
	"EC":  {"crv", "x", "y"},
	"OKP": {"crv", "x"},
}

// withoutCertificates drops the certificate members from every self-describing
// key in a JSON Web Key Set, and reports whether it changed anything.
//
// This exists because go-jose parses x5c eagerly while unmarshalling a key set
// and fails the whole document when any certificate in it does not parse. Go
// has rejected certificates with negative serial numbers since 1.23, and WSO2
// deployments have published them for years — so an issuer whose keys can
// verify a token perfectly well becomes an issuer with no readable keys at
// all, over a field the verification never needed.
//
// Only keys that already carry their own parameters are stripped. A key that
// somehow depended on its certificate keeps it and fails loudly, which is the
// right outcome: this removes a spurious failure, it does not paper over a key
// the shell genuinely cannot read.
func withoutCertificates(body []byte) ([]byte, bool) {
	var document map[string]json.RawMessage
	if err := json.Unmarshal(body, &document); err != nil {
		return nil, false
	}
	rawKeys, present := document["keys"]
	if !present {
		return nil, false
	}
	var keys []map[string]json.RawMessage
	if err := json.Unmarshal(rawKeys, &keys); err != nil {
		return nil, false
	}
	stripped := false
	for _, key := range keys {
		if !selfDescribing(key) {
			continue
		}
		for _, field := range certificateFields {
			if _, carried := key[field]; carried {
				delete(key, field)
				stripped = true
			}
		}
	}
	if !stripped {
		return nil, false
	}
	rewrittenKeys, err := json.Marshal(keys)
	if err != nil {
		return nil, false
	}
	document["keys"] = rewrittenKeys
	rewritten, err := json.Marshal(document)
	if err != nil {
		return nil, false
	}
	return rewritten, true
}

// selfDescribing reports whether a key carries every parameter its type needs,
// so that discarding its certificate loses nothing.
func selfDescribing(key map[string]json.RawMessage) bool {
	var keyType string
	if err := json.Unmarshal(key["kty"], &keyType); err != nil {
		return false
	}
	required, known := keyParameters[keyType]
	if !known {
		return false
	}
	for _, parameter := range required {
		if _, carried := key[parameter]; !carried {
			return false
		}
	}
	return true
}

// certificateStripper removes certificate members from key sets on their way
// back from the issuer, before any library parses them.
//
// It sits at the transport because that is the one place every fetch the OIDC
// library makes passes through, including the key set it fetches lazily on
// first verification. Responses that are not key sets are returned exactly as
// they arrived, byte for byte.
type certificateStripper struct{ base http.RoundTripper }

func (s certificateStripper) RoundTrip(request *http.Request) (*http.Response, error) {
	base := s.base
	if base == nil {
		base = http.DefaultTransport
	}
	response, err := base.RoundTrip(request)
	if err != nil || response.Body == nil || response.StatusCode != http.StatusOK {
		return response, err
	}
	body, err := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	stripped, changed := withoutCertificates(body)
	if !changed {
		stripped = body
	}
	response.Body = io.NopCloser(bytes.NewReader(stripped))
	response.ContentLength = int64(len(stripped))
	response.Header.Set("Content-Length", strconv.Itoa(len(stripped)))
	return response, nil
}
