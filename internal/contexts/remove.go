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

import "maps"

// WithoutRecord returns the account with one record removed, and whether the
// account held it. The key is either a product namespace, which takes the
// product's gateway record with it, or a gateway key (GatewayKey), which
// removes the gateway record alone and leaves the product's own in place.
//
// The receiver is not modified. The products map is cloned before anything is
// deleted from it, because an Account passed by value still shares its map
// with the caller's copy, and editing that in place would change a document
// the caller has not yet decided to write.
//
// Removing the pinned login product moves the pin to the product the
// namespace order now chooses, rather than clearing it. A pin naming a
// product that is gone is a document this package refuses to write, and no
// pin at all would let a product recorded later with an earlier-sorting
// namespace take the login over, which is the move the pin exists to prevent.
func (i Account) WithoutRecord(key string) (Account, bool) {
	products := maps.Clone(i.Products)
	if namespace, gateway := SplitGatewayKey(key); gateway {
		product, recorded := products[namespace]
		if !recorded || product.Gateway == nil {
			return i, false
		}
		product.Gateway = nil
		products[namespace] = product
		i.Products = products
		return i, true
	}
	if _, recorded := products[key]; !recorded {
		return i, false
	}
	delete(products, key)
	if len(products) == 0 {
		products = nil
	}
	i.Products = products
	if i.LoginProduct == key {
		// LoginAccess ignores a pin naming a product that is not recorded,
		// so with the product gone it answers with the namespace order.
		i.LoginProduct = i.LoginAccess().Namespace
	}
	return i, true
}

// SessionsUnreachedBy is every session this document reaches that next does
// not, once each: the secure-store entries a change from this document to next
// would leave behind with nothing left to name them.
//
// It is what makes removing a record safe. wso2 logout decides what to end by
// walking an account's records, and the secure store offers no way to list
// what it holds, so an entry whose record is gone can never be found, ended or
// revoked again. Every access returned here has to be ended before next is
// written, and nothing else may be: an entry next still reaches is a session
// something still logs in through.
//
// Reach is judged across every account in next, not only the one that
// changed. Two accounts may name the same credential reference, and a session
// the other one still reaches under it is still in use.
//
// The login session of an account that is still declared is never among the
// results, because LoginAccess names it whatever the account records. A direct
// product therefore never yields a session to end, and neither does an
// exchanged or an inline one, which name no session at all.
func (d Document) SessionsUnreachedBy(next Document) []ProductAccess {
	reached := map[string]bool{}
	for _, account := range next.Accounts {
		for _, access := range account.Accesses() {
			if access.SessionRef != "" {
				reached[access.SessionRef] = true
			}
		}
	}
	var unreached []ProductAccess
	for _, account := range d.Accounts {
		for _, access := range account.Accesses() {
			if access.SessionRef == "" || reached[access.SessionRef] {
				continue
			}
			// Marked as it is taken, so a reference two accounts share is
			// ended once rather than once per account naming it.
			reached[access.SessionRef] = true
			unreached = append(unreached, access)
		}
	}
	return unreached
}
