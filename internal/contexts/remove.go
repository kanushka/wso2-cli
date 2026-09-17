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
	"maps"
	"slices"
	"strings"
)

// WithoutRecord returns the context with one record removed, and whether the
// context held it. The key is either a product namespace, which takes the
// product's gateway record with it, or a gateway key (GatewayKey), which
// removes the gateway record alone and leaves the product's own in place.
//
// The receiver is not modified. The products map is cloned before anything is
// deleted from it, because a Context passed by value still shares its map
// with the caller's copy, and editing that in place would change a document
// the caller has not yet decided to write.
//
// It does not decide which product the context logs in through, and leaves
// the login product exactly as it was. A caller that removes the login product
// is left with a login naming a product that is gone, which is a document this
// package refuses to write: that invariant, not this function, is what keeps
// the login from moving silently. wso2 context product remove refuses the
// login product before calling this.
func (c Context) WithoutRecord(key string) (Context, bool) {
	products := maps.Clone(c.Products)
	if namespace, gateway := SplitGatewayKey(key); gateway {
		product, recorded := products[namespace]
		if !recorded || product.Gateway == nil {
			return c, false
		}
		product.Gateway = nil
		products[namespace] = product
		c.Products = products
		return c, true
	}
	if _, recorded := products[key]; !recorded {
		return c, false
	}
	delete(products, key)
	if len(products) == 0 {
		products = nil
	}
	c.Products = products
	return c, true
}

// Sessions is every session the context holds a reference for, once each: the
// login session first, then each product and gateway session of its own. An
// exchanged or inline record holds none and is not listed.
func (c Context) Sessions() []ProductAccess {
	if c.Login.Kind == KindClientCredentials || c.synthetic {
		return nil
	}
	var sessions []ProductAccess
	seen := map[string]bool{}
	for _, access := range c.Account().Accesses() {
		if access.SessionRef == "" || seen[access.SessionRef] {
			continue
		}
		seen[access.SessionRef] = true
		sessions = append(sessions, access)
	}
	return sessions
}

// SessionsUnreachedBy is every session this document reaches that next does
// not reach the same way, once each: the secure-store entries a change from
// this document to next would leave behind, either with nothing left to name
// them or named by a record that now asks for something else.
//
// It is what makes changing a record safe. wso2 logout decides what to end by
// walking a context's records, and the secure store offers no way to list
// what it holds, so an entry whose record is gone can never be found, ended or
// revoked again. An entry whose record changed is worse: it is still found,
// and presented for a URL, audience or grant it was never authorized for.
// Every access returned here has to be ended before next is written, and
// nothing else may be: an entry next still reaches the same way is a session
// something still logs in through.
//
// "The same way" is the session's binding: the issuer and client it was
// obtained at and as, the scope set and resource it was authorized for, and
// the strategy that obtained it. The login session of a context whose login
// did not change is never among the results, because LoginAccess names it
// whatever the context records.
func (d Document) SessionsUnreachedBy(next Document) []ProductAccess {
	reached := map[string]string{}
	for _, context := range next.Contexts {
		for _, access := range context.Sessions() {
			reached[access.SessionRef] = binding(access)
		}
	}
	var unreached []ProductAccess
	ended := map[string]bool{}
	for _, context := range d.Contexts {
		for _, access := range context.Sessions() {
			if ended[access.SessionRef] {
				continue
			}
			if still, found := reached[access.SessionRef]; found && still == binding(access) {
				continue
			}
			ended[access.SessionRef] = true
			unreached = append(unreached, access)
		}
	}
	return unreached
}

// binding is what a stored session was authorized for, as one comparable
// value. The audience is left out: it is proved on each token rather than
// asked for, except as the resource, which is in.
func binding(access ProductAccess) string {
	return strings.Join([]string{
		access.Issuer, access.ClientID, access.Strategy, access.Resource,
		strings.Join(slices.Sorted(slices.Values(access.Scopes)), " "),
	}, "\x00")
}
