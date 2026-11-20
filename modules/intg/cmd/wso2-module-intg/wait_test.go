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
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/testkit"
)

// fastPolling makes every wait in the test poll at once.
func fastPolling(t *testing.T) {
	t.Helper()
	previous := pollInterval
	pollInterval = time.Millisecond
	t.Cleanup(func() { pollInterval = previous })
}

// The commands that wait declare a deadline the shell honours; the rest keep
// its default.
func TestTheWaitingCommandsDeclareALongerDeadline(t *testing.T) {
	declared := commands().Declare()
	for _, command := range declared.Commands {
		path := strings.Join(command.Path, " ")
		want := 0
		switch path {
		case "integration build run", "integration deploy", "integration promote", "integration redeploy":
			want = int(waitDeadline / time.Second)
		}
		if command.TimeoutSeconds != want {
			t.Errorf("%q declares %d seconds, want %d", path, command.TimeoutSeconds, want)
		}
	}
}

// --- build run --wait -----------------------------------------------------

// buildWaitRoutes is order-api before and after a build is started: the
// listing gains b3, which runs and then ends as final.
func buildWaitRoutes(final string) map[string]stubRoute {
	return map[string]stubRoute{
		"GET /components/order-api/builds": {status: http.StatusOK, body: `{"items":[{"name":"b2","status":"Succeeded"}]}`,
			then: []stubRoute{{status: http.StatusOK,
				body: `{"items":[{"name":"b3","status":"Running","trigger":"manual"},{"name":"b2","status":"Succeeded"}]}`}}},
		"POST /components/order-api/builds": {status: http.StatusCreated, body: `{"message":"build triggered","success":true}`},
		"/components/order-api/builds/b3": {status: http.StatusOK, body: `{"name":"b3","status":"Running"}`,
			then: []stubRoute{{status: http.StatusOK, body: `{"name":"b3","status":"` + final +
				`","image":"registry.example/order-api:b3","commit":"` + fullCommit + `"}`}}},
	}
}

func TestBuildRunWaitFollowsTheNewBuildUntilItSucceeds(t *testing.T) {
	fastPolling(t)
	stub := newPlatformStub(t, buildWaitRoutes("Succeeded"))

	outcome := run([]string{"integration", "build", "run"}, []string{"order-api", "--wait"}, stub.endpoint(), &testkit.Access{Token: "t"})

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("build run --wait failed: %v %v", outcome.Err, outcome.Problem)
	}
	if outcome.Result.Schema != BuildSchema || field(outcome, "name") != "b3" || field(outcome, "status") != "Succeeded" {
		t.Errorf("result = %+v, want the new build b3, succeeded", outcome.Result.Fields)
	}
	if next := field(outcome, NextField); !strings.Contains(next, "wso2 intg integration deploy order-api --env <environment> --build b3") {
		t.Errorf("next = %q, want it to name deploying b3", next)
	}
	if stub.paths[0] != "GET "+basePath+"/components/order-api/builds" || stub.paths[1] != "POST "+basePath+"/components/order-api/builds" {
		t.Errorf("calls = %v, want the listing read before the build is started", stub.paths)
	}
}

func TestBuildRunWaitReportsAFailedBuildAsAProblem(t *testing.T) {
	fastPolling(t)
	stub := newPlatformStub(t, buildWaitRoutes("Failed"))

	outcome := run([]string{"integration", "build", "run"}, []string{"order-api", "--wait"}, stub.endpoint(), &testkit.Access{Token: "t"})

	assertProblem(t, outcome, "intg.build_failed", problem.CategoryProductService)
	if !strings.Contains(outcome.Problem.Recovery, "wso2 intg integration build logs order-api b3") {
		t.Errorf("recovery = %q, want it to name the build's logs", outcome.Problem.Recovery)
	}
}

func TestBuildRunWaitStopsAtItsTimeout(t *testing.T) {
	fastPolling(t)
	routes := buildWaitRoutes("Succeeded")
	routes["/components/order-api/builds/b3"] = stubRoute{status: http.StatusOK, body: `{"name":"b3","status":"Running"}`}
	stub := newPlatformStub(t, routes)

	outcome := run([]string{"integration", "build", "run"}, []string{"order-api", "--wait", "--wait-timeout", "50ms"}, stub.endpoint(),
		&testkit.Access{Token: "t"})

	assertProblem(t, outcome, "intg.wait_timeout", problem.CategoryProductService)
	if !strings.Contains(outcome.Problem.Recovery, "wso2 intg integration build show order-api b3") {
		t.Errorf("recovery = %q, want it to name the build to follow", outcome.Problem.Recovery)
	}
}

func TestAWaitTimeoutBeyondTheDeclaredDeadlineIsRefused(t *testing.T) {
	stub := newPlatformStub(t, nil)

	outcome := run([]string{"integration", "build", "run"}, []string{"order-api", "--wait", "--wait-timeout", "1h"}, stub.endpoint(),
		&testkit.Access{Token: "t"})

	assertProblem(t, outcome, "intg.invalid_argument", problem.CategoryUsage)
	if stub.calls != 0 {
		t.Errorf("a refused command line reached the API (%v)", stub.paths)
	}
}

// --- integration deploy and promote --wait ----------------------------------

// rollout is development answering the old release, then the new one in
// progress, then the new one ending as final.
func rollout(environment, release, final string) map[string]stubRoute {
	return map[string]stubRoute{
		"GET /components/order-api/deployments?environmentId=" + environment: {status: http.StatusOK, body: activeDeployment,
			then: []stubRoute{
				{status: http.StatusOK, body: `{"releaseId":"` + release + `","deploymentStatusV2":"IN_PROGRESS"}`},
				{status: http.StatusOK, body: `{"releaseId":"` + release + `","deploymentStatusV2":"` + final + `"}`},
			}},
	}
}

func TestIntegrationDeployWaitFollowsTheNewReleaseUntilItIsActive(t *testing.T) {
	fastPolling(t)
	routes := rollout("development", "order-api-r4", "ACTIVE")
	routes["POST /components/order-api/deployments"] = stubRoute{status: http.StatusOK, body: `{"message":"order-api-r4"}`}
	stub := newPlatformStub(t, deploymentRoutes(routes))

	outcome := run([]string{"integration", "deploy"}, []string{"order-api", "--env", "development", "--project", "orders",
		"--wait"}, stub.endpoint(), &testkit.Access{Token: "t"})

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("integration deploy --wait failed: %v %v", outcome.Err, outcome.Problem)
	}
	if field(outcome, "release") != "order-api-r4" || field(outcome, "status") != "ACTIVE" {
		t.Errorf("result = %+v, want order-api-r4 active", outcome.Result.Fields)
	}
}

func TestIntegrationDeployWithoutWaitReportsTheRequest(t *testing.T) {
	stub := newPlatformStub(t, deploymentRoutes(map[string]stubRoute{
		"POST /components/order-api/deployments": {status: http.StatusOK, body: `{"message":"order-api-r4"}`},
	}))

	outcome := run([]string{"integration", "deploy"}, []string{"order-api", "--env", "development", "--project", "orders"},
		stub.endpoint(), &testkit.Access{Token: "t"})

	if field(outcome, "status") != "requested" {
		t.Errorf("status = %q, want requested", field(outcome, "status"))
	}
	assertNoReadAfterWrite(t, stub)
}

func TestIntegrationDeployWaitReportsAFailedRollout(t *testing.T) {
	fastPolling(t)
	routes := rollout("development", "order-api-r4", "ERROR")
	routes["POST /components/order-api/deployments"] = stubRoute{status: http.StatusOK, body: `{"message":"order-api-r4"}`}
	stub := newPlatformStub(t, deploymentRoutes(routes))

	outcome := run([]string{"integration", "deploy"}, []string{"order-api", "--env", "development", "--project", "orders",
		"--wait"}, stub.endpoint(), &testkit.Access{Token: "t"})

	assertProblem(t, outcome, "intg.deployment_failed", problem.CategoryProductService)
	if !strings.Contains(outcome.Problem.Recovery, "wso2 intg integration logs order-api --env development") {
		t.Errorf("recovery = %q, want it to name the integration's logs", outcome.Problem.Recovery)
	}
}

func TestIntegrationPromoteWaitFollowsTheTargetEnvironment(t *testing.T) {
	fastPolling(t)
	routes := map[string]stubRoute{
		"GET /components/order-api/deployments?environmentId=production": {status: http.StatusOK, body: ``,
			then: []stubRoute{{status: http.StatusOK, body: `{"releaseId":"order-api-r3","deploymentStatusV2":"ACTIVE"}`}}},
		"POST /components/order-api/deployments/promote": {status: http.StatusOK, body: `{"message":"order-api-r3"}`},
	}
	stub := newPlatformStub(t, deploymentRoutes(routes))

	outcome := run([]string{"integration", "promote"}, []string{"order-api", "--from", "development", "--to", "production",
		"--project", "orders", "--wait"}, stub.endpoint(), &testkit.Access{Token: "t"})

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("integration promote --wait failed: %v %v", outcome.Err, outcome.Problem)
	}
	if field(outcome, "status") != "ACTIVE" {
		t.Errorf("status = %q, want ACTIVE", field(outcome, "status"))
	}
}

// assertNoReadAfterWrite checks a command without --wait made no call after
// its write: it reports the request and leaves.
func assertNoReadAfterWrite(t *testing.T, stub *platformStub) {
	t.Helper()
	last := stub.paths[len(stub.paths)-1]
	if strings.HasPrefix(last, "GET ") {
		t.Errorf("the command read %s after its write, without --wait", last)
	}
}

// --- integration redeploy --wait ------------------------------------------

// restart is development answering a stopped release, then the same release
// starting, then ending as final.
func restart(final string) map[string]stubRoute {
	return map[string]stubRoute{
		"POST /components/order-api/deployments/redeploy": {status: http.StatusOK, body: `{"message":"redeploy requested"}`},
		"GET /components/order-api/deployments?environmentId=development": {status: http.StatusOK,
			body: `{"releaseId":"order-api-r3","deploymentStatusV2":"SUSPENDED"}`,
			then: []stubRoute{
				{status: http.StatusOK, body: `{"releaseId":"order-api-r3","deploymentStatusV2":"IN_PROGRESS"}`},
				{status: http.StatusOK, body: `{"releaseId":"order-api-r3","deploymentStatusV2":"` + final + `"}`},
			}},
	}
}

// A stopped release reads SUSPENDED until it starts again, so a wait after a
// stop goes on through it rather than reporting a failure.
func TestIntegrationRedeployWaitFollowsAStoppedReleaseUntilItIsActive(t *testing.T) {
	fastPolling(t)
	stub := newPlatformStub(t, deploymentRoutes(restart("ACTIVE")))

	outcome := run([]string{"integration", "redeploy"}, []string{"order-api", "--env", "development", "--wait"},
		stub.endpoint(), &testkit.Access{Token: "t"})

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("integration redeploy --wait failed: %v %v", outcome.Err, outcome.Problem)
	}
	if field(outcome, "release") != "order-api-r3" || field(outcome, "status") != "ACTIVE" {
		t.Errorf("result = %+v, want order-api-r3 active", outcome.Result.Fields)
	}
	if next := field(outcome, NextField); !strings.Contains(next, "wso2 intg integration logs order-api --env development") {
		t.Errorf("next = %q, want it to name the integration's logs", next)
	}
}

func TestIntegrationRedeployWaitReportsAFailedRestart(t *testing.T) {
	fastPolling(t)
	stub := newPlatformStub(t, deploymentRoutes(restart("ERROR")))

	outcome := run([]string{"integration", "redeploy"}, []string{"order-api", "--env", "development", "--wait"},
		stub.endpoint(), &testkit.Access{Token: "t"})

	assertProblem(t, outcome, "intg.deployment_failed", problem.CategoryProductService)
}

func TestIntegrationRedeployWithoutWaitReportsTheRequest(t *testing.T) {
	stub := newPlatformStub(t, deploymentRoutes(restart("ACTIVE")))

	outcome := run([]string{"integration", "redeploy"}, []string{"order-api", "--env", "development"},
		stub.endpoint(), &testkit.Access{Token: "t"})

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("integration redeploy failed: %v %v", outcome.Err, outcome.Problem)
	}
	if field(outcome, "status") != "requested" || field(outcome, "message") != "redeploy requested" {
		t.Errorf("result = %+v, want the request reported", outcome.Result.Fields)
	}
	assertNoReadAfterWrite(t, stub)
}

func TestIntegrationRedeployRefusesAWaitItCannotHonour(t *testing.T) {
	stub := newPlatformStub(t, deploymentRoutes(nil))

	outcome := run([]string{"integration", "redeploy"}, []string{"order-api", "--env", "development", "--wait",
		"--wait-timeout", "2h"}, stub.endpoint(), &testkit.Access{Token: "t"})

	assertProblem(t, outcome, "intg.invalid_argument", problem.CategoryUsage)
	if stub.calls != 0 {
		t.Errorf("a refused command line reached the API (%v)", stub.paths)
	}
}

// --- review follow-ups: what a wait counts as done --------------------------

// running is development answering the release order-api-r3 in each status in
// turn, the last repeated.
func running(statuses ...string) stubRoute {
	answers := make([]stubRoute, len(statuses))
	for i, status := range statuses {
		answers[i] = stubRoute{status: http.StatusOK,
			body: `{"releaseId":"order-api-r3","deploymentStatusV2":"` + status + `"}`}
	}
	return stubRoute{status: answers[0].status, body: answers[0].body, then: answers[1:]}
}

func redeployRoutes(deployment stubRoute) map[string]stubRoute {
	return deploymentRoutes(map[string]stubRoute{
		"POST /components/order-api/deployments/redeploy":                 {status: http.StatusOK, body: `{"message":"redeployed"}`},
		"GET /components/order-api/deployments?environmentId=development": deployment,
	})
}

// A running release reads ACTIVE before its restart too, so the wait holds out
// for the rollout before it counts ACTIVE as the restart done.
func TestIntegrationRedeployWaitOfARunningReleaseWaitsForItsRollout(t *testing.T) {
	fastPolling(t)
	stub := newPlatformStub(t, redeployRoutes(running("ACTIVE", "ACTIVE", "IN_PROGRESS", "ACTIVE")))

	outcome := run([]string{"integration", "redeploy"}, []string{"order-api", "--env", "development", "--wait"},
		stub.endpoint(), &testkit.Access{Token: "t"})

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("integration redeploy --wait failed: %v %v", outcome.Err, outcome.Problem)
	}
	reads := 0
	for _, path := range stub.paths {
		if strings.HasPrefix(path, "GET "+basePath+"/components/order-api/deployments") {
			reads++
		}
	}
	if field(outcome, "status") != "ACTIVE" || reads != 4 {
		t.Errorf("status %q after %d reads, want ACTIVE only after the rollout was seen (4 reads)",
			field(outcome, "status"), reads)
	}
	if next := field(outcome, NextField); !strings.Contains(next, "wso2 intg integration logs order-api") {
		t.Errorf("next = %q, want it to name the logs", next)
	}
}

func TestIntegrationRedeployWaitSaysSoWhenNoRolloutWasSeen(t *testing.T) {
	fastPolling(t)
	previous := restartGrace
	restartGrace = 20 * time.Millisecond
	t.Cleanup(func() { restartGrace = previous })
	stub := newPlatformStub(t, redeployRoutes(running("ACTIVE")))

	outcome := run([]string{"integration", "redeploy"}, []string{"order-api", "--env", "development", "--wait"},
		stub.endpoint(), &testkit.Access{Token: "t"})

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("integration redeploy --wait failed: %v %v", outcome.Err, outcome.Problem)
	}
	if next := field(outcome, NextField); !strings.Contains(next, "no rollout was seen") {
		t.Errorf("next = %q, want it to say no rollout was seen", next)
	}
}

func TestIntegrationRedeployWaitOfARunningReleaseReportsAFailedRollout(t *testing.T) {
	fastPolling(t)
	stub := newPlatformStub(t, redeployRoutes(running("ACTIVE", "IN_PROGRESS", "ERROR")))

	outcome := run([]string{"integration", "redeploy"}, []string{"order-api", "--env", "development", "--wait"},
		stub.endpoint(), &testkit.Access{Token: "t"})

	assertProblem(t, outcome, "intg.deployment_failed", problem.CategoryProductService)
}

// An answer that is not a release of the integration is not waited for by
// name: any release other than the one running before is.
func TestIntegrationDeployWaitWithoutAReleaseNameWaitsForANewRelease(t *testing.T) {
	fastPolling(t)
	routes := deploymentRoutes(map[string]stubRoute{
		"POST /components/order-api/deployments": {status: http.StatusOK, body: `{"message":"development"}`},
		"GET /components/order-api/deployments?environmentId=development": {status: http.StatusOK,
			body: `{"releaseId":"order-api-r3","deploymentStatusV2":"ACTIVE"}`,
			then: []stubRoute{
				{status: http.StatusOK, body: `{"releaseId":"order-api-r3","deploymentStatusV2":"ACTIVE"}`},
				{status: http.StatusOK, body: `{"releaseId":"order-api-r4","deploymentStatusV2":"ACTIVE"}`},
			}},
	})
	stub := newPlatformStub(t, routes)

	outcome := run([]string{"integration", "deploy"}, []string{"order-api", "--env", "development", "--project", "orders",
		"--wait"}, stub.endpoint(), &testkit.Access{Token: "t"})

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("integration deploy --wait failed: %v %v", outcome.Err, outcome.Problem)
	}
	if field(outcome, "release") != "order-api-r4" {
		t.Errorf("release = %q, want the new order-api-r4, not the r3 that ran before", field(outcome, "release"))
	}
}

// Two builds that start together cannot be told apart, so the wait names them
// rather than following either.
func TestBuildRunWaitRefusesTwoBuildsItCannotTellApart(t *testing.T) {
	fastPolling(t)
	routes := buildWaitRoutes("Succeeded")
	routes["GET /components/order-api/builds"] = stubRoute{status: http.StatusOK,
		body: `{"items":[{"name":"b2","status":"Succeeded"}]}`,
		then: []stubRoute{{status: http.StatusOK, body: `{"items":[{"name":"b4","status":"Running"},` +
			`{"name":"b3","status":"Running"},{"name":"b2","status":"Succeeded"}]}`}}}
	stub := newPlatformStub(t, routes)

	outcome := run([]string{"integration", "build", "run"}, []string{"order-api", "--wait"}, stub.endpoint(),
		&testkit.Access{Token: "t"})

	assertProblem(t, outcome, "intg.build_ambiguous", problem.CategoryProductService)
	if !strings.Contains(outcome.Problem.Message, "b4, b3") || !strings.Contains(outcome.Problem.Recovery, "--commit") {
		t.Errorf("problem = %+v, want both builds named and --commit offered", outcome.Problem)
	}
}

func TestBuildRunWaitWithACommitFollowsTheBuildOfThatCommit(t *testing.T) {
	fastPolling(t)
	routes := buildWaitRoutes("Succeeded")
	routes["GET /components/order-api/builds"] = stubRoute{status: http.StatusOK,
		body: `{"items":[{"name":"b2","status":"Succeeded"}]}`,
		then: []stubRoute{{status: http.StatusOK, body: `{"items":[{"name":"b4","status":"Running","commit":"` +
			strings.Repeat("b", 40) + `"},{"name":"b3","status":"Running","commit":"` + fullCommit + `"},` +
			`{"name":"b2","status":"Succeeded"}]}`}}}
	stub := newPlatformStub(t, routes)

	outcome := run([]string{"integration", "build", "run"}, []string{"order-api", "--commit", fullCommit, "--wait"},
		stub.endpoint(), &testkit.Access{Token: "t"})

	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("build run --commit --wait failed: %v %v", outcome.Err, outcome.Problem)
	}
	if field(outcome, "name") != "b3" {
		t.Errorf("followed %q, want b3, the build of the commit asked for", field(outcome, "name"))
	}
}

// A 400 is the request itself refused, so the recovery points at the command
// line rather than at retrying it.
func TestABadRequestPointsAtTheCommandLine(t *testing.T) {
	stub := newPlatformStub(t, deploymentRoutes(map[string]stubRoute{
		"PUT /components/order-api/deployments/stop": {status: http.StatusBadRequest,
			body: `{"error":"Bad Request","message":"environment is required"}`},
	}))

	outcome := run([]string{"integration", "stop"}, []string{"order-api", "--env", "development"}, stub.endpoint(),
		&testkit.Access{Token: "t"})

	assertProblem(t, outcome, "intg.call_failed", problem.CategoryUsage)
	if strings.Contains(outcome.Problem.Recovery, "Retry") || !strings.Contains(outcome.Problem.Recovery, "command line") {
		t.Errorf("recovery = %q, want it to point at the command line", outcome.Problem.Recovery)
	}
}
