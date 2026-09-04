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
	"strings"
	"testing"
)

func TestBootstrapRegistersAJWTClientAndPrintsTheIdentityLine(t *testing.T) {
	fake := newFakeAPIM(t)
	t.Setenv("WSO2_APIM_ADMIN_PASSWORD", "admin")

	outcome := fake.run(t, []string{"bootstrap"}, "--url", fake.server.URL+"/")

	if outcome.Problem != nil {
		t.Fatalf("%+v", outcome.Problem)
	}
	if len(outcome.AccessRequests) != 0 {
		t.Error("bootstrap asked the broker for access; it must use the administrator password only")
	}
	registrations := fake.requestsTo("POST /client-registration/v0.17/register")
	if len(registrations) != 1 || !strings.Contains(registrations[0], `"tokenType":"JWT"`) ||
		!strings.Contains(registrations[0], `"clientName":"wso2-cli"`) ||
		!strings.Contains(registrations[0], `"callbackUrl":"http://127.0.0.1:10425/callback"`) {
		t.Errorf("registration = %v", registrations)
	}
	fields := fieldsOf(outcome)
	if fields["clientId"] != "client-1" || fields["clientSecret"] != "secret-1" ||
		fields["issuer"] != fake.server.URL+"/oauth2/token" ||
		!strings.Contains(fields["next"], "export WSO2_APIM_CLIENT_SECRET=secret-1") ||
		!strings.Contains(fields["next"], "--client-id client-1 --client-secret-variable WSO2_APIM_CLIENT_SECRET") ||
		!strings.Contains(fields["next"], "--audience client-1") || !strings.Contains(fields["next"], "--scope apim:admin") {
		t.Errorf("fields = %+v", fields)
	}
	assertEndsWithNext(t, outcome)
}

func TestBootstrapRefusesWithoutThePasswordAndOnAWrongOne(t *testing.T) {
	fake := newFakeAPIM(t)
	t.Setenv("WSO2_APIM_ADMIN_PASSWORD", "")
	outcome := fake.run(t, []string{"bootstrap"}, "--url", fake.server.URL)
	if outcome.Problem == nil || outcome.Problem.Code != "apim.missing_secret" || len(fake.requests) != 0 {
		t.Errorf("unset: %+v requests %v", outcome.Problem, fake.requests)
	}
	t.Setenv("WSO2_APIM_ADMIN_PASSWORD", "wrong")
	outcome = fake.run(t, []string{"bootstrap"}, "--url", fake.server.URL)
	if outcome.Problem == nil || outcome.Problem.Code != "apim.refused" || !strings.Contains(outcome.Problem.Message, "401") {
		t.Errorf("wrong: %+v", outcome.Problem)
	}
	outcome = fake.run(t, []string{"bootstrap"})
	if outcome.Problem == nil || outcome.Problem.Code != "apim.missing_url" {
		t.Errorf("no url: %+v", outcome.Problem)
	}
}
