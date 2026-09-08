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

func TestProjectsListReadsTheOrganizationFromTheContext(t *testing.T) {
	fake := newFakeAgentManager(t)
	fake.seed("acme", "payments", "support")

	outcome := fake.run(t, "acme", []string{"projects", "list"})
	if outcome.Problem != nil {
		t.Fatalf("projects list: %+v", outcome.Problem)
	}
	if got := fake.requestsTo("GET"); len(got) != 1 || got[0] != "GET /api/v1/orgs/acme/projects" {
		t.Errorf("requests = %v", got)
	}
	// No scopes: the identity's product record names them, and is the ceiling.
	if len(outcome.AccessRequests) != 1 || outcome.AccessRequests[0].Audience != APIAudience ||
		len(outcome.AccessRequests[0].Scopes) != 0 {
		t.Errorf("access asked = %+v", outcome.AccessRequests)
	}
	fields := fieldsOf(outcome)
	if outcome.Result.Schema != ProjectsSchema || fields["organization"] != "acme" || fields["count"] != "2" ||
		!strings.Contains(fields["projects"], "payments (Payments, 2026-09-01)") ||
		!strings.Contains(fields["next"], "agents list") {
		t.Errorf("fields = %+v", fields)
	}
	assertEndsWithNext(t, outcome)
}

func TestTheOrgFlagOverridesTheContextAndPaginationReachesTheQuery(t *testing.T) {
	fake := newFakeAgentManager(t)
	fake.seed("other")

	outcome := fake.run(t, "acme", []string{"projects", "list"}, "--org", "other", "--limit", "5", "--offset", "10")
	if outcome.Problem != nil {
		t.Fatalf("projects list: %+v", outcome.Problem)
	}
	if got := fake.requestsTo("GET"); len(got) != 1 || got[0] != "GET /api/v1/orgs/other/projects?limit=5&offset=10" {
		t.Errorf("requests = %v", got)
	}
	if fields := fieldsOf(outcome); fields["organization"] != "other" || fields["count"] != "0" ||
		fields["projects"] != "(none)" || !strings.Contains(fields["next"], "amctl project create") {
		t.Errorf("fields = %+v", fields)
	}
}

func TestNoOrganizationAnywhereIsAUsageProblemBeforeAnyCall(t *testing.T) {
	fake := newFakeAgentManager(t)
	outcome := fake.run(t, "", []string{"projects", "list"})
	if outcome.Problem == nil || outcome.Problem.Code != "amp.org_required" ||
		!strings.Contains(outcome.Problem.Recovery, "wso2 org use") {
		t.Fatalf("problem = %+v", outcome.Problem)
	}
	if len(outcome.AccessRequests) != 0 || len(fake.requestsTo("GET")) != 0 {
		t.Error("a command with no organization asked for access or called the deployment")
	}
}

func TestAgentsListNeedsAProjectAndListsItsAgents(t *testing.T) {
	fake := newFakeAgentManager(t)
	fake.seed("acme", "payments")
	fake.addAgent("acme", "payments", "refund-bot", "Running")
	fake.addAgent("acme", "payments", "triage", "")

	missing := fake.run(t, "acme", []string{"agents", "list"})
	if missing.Problem == nil || missing.Problem.Code != "amp.missing_flag" {
		t.Fatalf("without --project: %+v", missing.Problem)
	}

	outcome := fake.run(t, "acme", []string{"agents", "list"}, "--project", "payments")
	if outcome.Problem != nil {
		t.Fatalf("agents list: %+v", outcome.Problem)
	}
	if got := fake.requestsTo("GET"); len(got) != 1 || got[0] != "GET /api/v1/orgs/acme/projects/payments/agents" {
		t.Errorf("requests = %v", got)
	}
	fields := fieldsOf(outcome)
	if outcome.Result.Schema != AgentsSchema || fields["project"] != "payments" || fields["count"] != "2" ||
		!strings.Contains(fields["agents"], "refund-bot (refund-bot, Running)") ||
		!strings.Contains(fields["agents"], "triage (triage, unknown)") {
		t.Errorf("fields = %+v", fields)
	}
	assertEndsWithNext(t, outcome)
}

func TestADeploymentRefusalIsAProductServiceProblemWithItsWords(t *testing.T) {
	fake := newFakeAgentManager(t)
	outcome := fake.run(t, "nowhere", []string{"projects", "list"})
	if outcome.Problem == nil || outcome.Problem.Code != "amp.refused" ||
		!strings.Contains(outcome.Problem.Message, "404") ||
		!strings.Contains(outcome.Problem.Message, "organization not found") {
		t.Fatalf("problem = %+v", outcome.Problem)
	}
}

func TestEveryDeclaredCommandIsServed(t *testing.T) {
	declared := commands().Declare()
	var paths []string
	for _, command := range declared.Commands {
		paths = append(paths, strings.Join(command.Path, " "))
	}
	for _, want := range []string{"status", "projects list", "agents list"} {
		found := false
		for _, path := range paths {
			if path == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%q is not declared; declared %v", want, paths)
		}
	}
}
