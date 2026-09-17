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

package main

import (
	"context"
	"strings"

	"github.com/wso2/wso2-cli/modules/identity/internal/thunder"
)

// organizationUnit is one entry of a ThunderID organization unit listing.
type organizationUnit struct {
	ID     string `json:"id"`
	Handle string `json:"handle"`
	Name   string `json:"name"`
}

type organizationUnitListing struct {
	TotalResults      int                `json:"totalResults"`
	OrganizationUnits []organizationUnit `json:"organizationUnits"`
}

// resolveOrganizationUnitID answers ThunderID's ouId for every create command
// this module has: ThunderID requires every resource it organizes to name the
// organization unit that owns it, but a caller creating one thing rarely wants
// to look up an id first.
//
// ou may be empty, an id, or a handle. Given, it is checked against the
// deployment's own listing and refused if it names neither. Empty, the
// deployment's only organization unit is used as the default; a deployment
// that records more than one is refused rather than guessed at, naming every
// unit so the caller can choose with --ou.
func resolveOrganizationUnitID(
	ctx context.Context, client thunder.Client, ou, endpoint string,
) (string, error) {
	var listing organizationUnitListing
	if err := client.Get(ctx, "/organization-units", &listing); err != nil {
		return "", callFailed(err, "read the organization units", endpoint)
	}
	if ou != "" {
		for _, unit := range listing.OrganizationUnits {
			if unit.ID == ou || unit.Handle == ou {
				return unit.ID, nil
			}
		}
		return "", usageProblem("--ou " + ou + " does not name an organization unit this deployment " +
			"records; it records " + organizationUnitList(listing) + ".")
	}
	switch len(listing.OrganizationUnits) {
	case 0:
		return "", usageProblem(
			"this deployment records no organization units, so there is no default for --ou")
	case 1:
		return listing.OrganizationUnits[0].ID, nil
	default:
		return "", usageProblem(
			"this deployment records more than one organization unit, so --ou must name one: " +
				organizationUnitList(listing))
	}
}

// organizationUnitList states every organization unit a listing carries, by
// handle and id, so a refusal that names one names what the caller can pass.
func organizationUnitList(listing organizationUnitListing) string {
	entries := make([]string, len(listing.OrganizationUnits))
	for i, unit := range listing.OrganizationUnits {
		entries[i] = unit.Handle + " (" + unit.ID + ")"
	}
	return strings.Join(entries, ", ")
}
