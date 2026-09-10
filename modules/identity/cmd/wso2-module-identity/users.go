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
	"strconv"

	"github.com/wso2/wso2-cli/modules/identity/internal/thunder"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/result"
)

// UsersSchema identifies the semantic shape of a user listing.
const UsersSchema = "identity.users/v1"

// user is one entry of a ThunderID user listing. Attributes are schema-driven
// on the deployment, so they are read as a map rather than as named members: a
// deployment that adds one must not make this module stop reading the rest.
type user struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	Attributes map[string]string `json:"attributes"`
}

type userListing struct {
	TotalResults int    `json:"totalResults"`
	Users        []user `json:"users"`
}

// usersList answers "wso2 identity users list".
func usersList(ctx context.Context, request module.Request) (result.Result, error) {
	client, err := clientFor(ctx, request)
	if err != nil {
		return result.Result{}, err
	}
	var listing userListing
	if err := client.Get(ctx, "/users", &listing); err != nil {
		return result.Result{}, callFailed(err, "read the users", request.Context.Endpoint)
	}
	report := result.New(UsersSchema).
		With("count", "Users", strconv.Itoa(listing.TotalResults)).
		WithColumn("username", "Username").
		WithColumn("email", "Email").
		WithColumn("id", "ID")
	for _, u := range listing.Users {
		// The username is the display attribute ThunderID's Person type
		// declares, and the one an administrator recognizes; the id is what
		// every other command takes, so both are reported.
		report = report.WithRow(attributeOr(u, "username", u.ID), u.Attributes["email"], u.ID)
	}
	return report.With(NextField, "Next", usersNext(listing)), nil
}

func attributeOr(u user, name, fallback string) string {
	if value := u.Attributes[name]; value != "" {
		return value
	}
	return fallback
}

func usersNext(listing userListing) string {
	if listing.TotalResults == 0 {
		return "This deployment records no users yet."
	}
	return "Run wso2 identity resource-servers list to see what those users can be granted."
}

// callFailed states a refused management call in terms an administrator can
// act on, keeping the deployment's own words rather than inventing a second
// account of them.
func callFailed(err error, attempted, endpoint string) error {
	var refusal thunder.Failure
	if !asFailure(err, &refusal) {
		return moduleProblem("identity.deployment_unreachable",
			"the shell could not reach the account deployment at "+endpoint+" to "+attempted,
			"Check that this machine can reach that URL, then retry.")
	}
	if refusal.Status == 401 || refusal.Status == 403 {
		return moduleProblem("identity.not_authorized",
			"the deployment refused this account's access when asked to "+attempted,
			"Ask an administrator to grant this user a role carrying the system permission on "+
				"the deployment's own resource server, then run wso2 login again.")
	}
	message := refusal.Message
	if message == "" {
		message = refusal.Error()
	}
	return moduleProblem("identity.call_failed",
		"the deployment would not "+attempted+": "+message,
		"Check the deployment's own logs for the refusal, then retry.")
}
