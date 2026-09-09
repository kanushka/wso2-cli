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
	"strings"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/result"
)

// AppsSchema identifies the semantic shape of an application listing.
const AppsSchema = "identity.apps/v1"

type application struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ClientID string `json:"clientId"`
	Type     string `json:"type"`
}

type applicationListing struct {
	TotalResults int           `json:"totalResults"`
	Applications []application `json:"applications"`
}

// appsList answers "wso2 identity apps list".
func appsList(ctx context.Context, request module.Request) (result.Result, error) {
	client, err := clientFor(ctx, request)
	if err != nil {
		return result.Result{}, err
	}
	var listing applicationListing
	if err := client.Get(ctx, "/applications", &listing); err != nil {
		return result.Result{}, callFailed(err, "read the applications", request.Context.Endpoint)
	}
	report := result.New(AppsSchema).
		With("count", "Applications", strconv.Itoa(listing.TotalResults))
	for _, app := range listing.Applications {
		// The client identifier leads: it is what an operator records as the
		// account's client, and what a deployment names in a refusal.
		parts := []string{app.ClientID}
		if app.Type != "" {
			parts = append(parts, app.Type)
		}
		report = report.With("application."+app.ID, app.Name, strings.Join(parts, "  "))
	}
	return report.With(NextField, "Next",
		"The shell logs in as one of these; wso2 account list shows which."), nil
}
