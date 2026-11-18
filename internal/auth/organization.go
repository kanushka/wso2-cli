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
	"strings"
)

// Organization is the organization a token was issued in, as ThunderID names
// it: the organization unit's id, display name and handle.
type Organization struct {
	ID     string `json:"id,omitempty"`
	Name   string `json:"name,omitempty"`
	Handle string `json:"handle,omitempty"`
}

// IsZero reports whether no organization is named.
func (o Organization) IsZero() bool { return o == Organization{} }

// Label names the organization for a person: its name and handle, whichever
// it has, else its id.
func (o Organization) Label() string {
	switch {
	case o.Name != "" && o.Handle != "":
		return o.Name + " (" + o.Handle + ")"
	case o.Name != "":
		return o.Name
	case o.Handle != "":
		return o.Handle
	}
	return o.ID
}

// TokenOrganization reads the organization from the first of tokens whose
// claims name one. The payload is read without verifying the signature: the
// tokens came from the issuer in answer to this shell, and what is read here
// only describes them to the person who signed in.
func TokenOrganization(tokens ...string) Organization {
	for _, token := range tokens {
		parts := strings.Split(token, ".")
		if len(parts) != 3 {
			continue
		}
		payload, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			continue
		}
		var claims struct {
			ID     string `json:"ouId"`
			Name   string `json:"ouName"`
			Handle string `json:"ouHandle"`
		}
		if json.Unmarshal(payload, &claims) != nil {
			continue
		}
		if organization := (Organization{ID: claims.ID, Name: claims.Name, Handle: claims.Handle}); !organization.IsZero() {
			return organization
		}
	}
	return Organization{}
}
