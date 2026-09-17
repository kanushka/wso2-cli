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

package testkit_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/result"
	"github.com/wso2/wso2-cli/sdk/testkit"
)

// probeOptions describe a module used only to exercise the test kit.
func probeOptions() module.Options {
	return module.Options{Namespace: "probe", Version: "1.2.3"}
}

// statusCommand answers with a fixed result and does not touch the broker.
func statusCommand() module.Command {
	return module.Command{
		Path: []string{"status"},
		Run: func(_ context.Context, _ module.Request) (result.Result, error) {
			return result.New("probe.status/v1").With("status", "Status", "operational"), nil
		},
	}
}

// acquireCommand asks the broker for access and reports what it got back, so
// a test can prove the test kit's scripted answer reached the handler.
func acquireCommand() module.Command {
	return module.Command{
		Path: []string{"acquire"},
		Run: func(ctx context.Context, request module.Request) (result.Result, error) {
			access, err := request.Access.Acquire(ctx, module.AccessRequest{
				Audience: "reference-api",
				Scopes:   []string{"read"},
			})
			if err != nil {
				return result.Result{}, err
			}
			return result.New("probe.acquire/v1").With("token", "Token", access.Token), nil
		},
	}
}

// twiceCommand asks the broker for access twice, so a test can prove
// AccessRequests preserves order across more than one exchange.
func twiceCommand() module.Command {
	return module.Command{
		Path: []string{"twice"},
		Run: func(ctx context.Context, request module.Request) (result.Result, error) {
			if _, err := request.Access.Acquire(ctx, module.AccessRequest{Audience: "first"}); err != nil {
				return result.Result{}, err
			}
			if _, err := request.Access.Acquire(ctx, module.AccessRequest{Audience: "second"}); err != nil {
				return result.Result{}, err
			}
			return result.New("probe.twice/v1").With("status", "Status", "operational"), nil
		},
	}
}

// failingCommand returns a plain error, never a typed problem.
func failingCommand() module.Command {
	return module.Command{
		Path: []string{"fail"},
		Run: func(_ context.Context, _ module.Request) (result.Result, error) {
			return result.Result{}, errors.New("boom")
		},
	}
}

func TestRunReturnsTheResultAHandlerProduces(t *testing.T) {
	outcome := testkit.Run(t.Context(), probeOptions(), []module.Command{statusCommand()},
		testkit.Invocation{Command: []string{"status"}})

	if outcome.Err != nil {
		t.Fatalf("Run failed: %v", outcome.Err)
	}
	if outcome.Result == nil {
		t.Fatal("Run reported no result")
	}
	if outcome.Result.Schema != "probe.status/v1" {
		t.Errorf("result schema is %q, want %q", outcome.Result.Schema, "probe.status/v1")
	}
	if outcome.Problem != nil {
		t.Errorf("Run also reported a problem: %v", outcome.Problem)
	}
}

func TestRunReportsAnUnknownCommandAsAProblem(t *testing.T) {
	outcome := testkit.Run(t.Context(), probeOptions(), []module.Command{statusCommand()},
		testkit.Invocation{Command: []string{"missing"}})

	if outcome.Err != nil {
		t.Fatalf("Run failed rather than returning the module's problem: %v", outcome.Err)
	}
	if outcome.Problem == nil {
		t.Fatal("Run reported no problem for an unimplemented command")
	}
	if outcome.Problem.Category != problem.CategoryUsage {
		t.Errorf("problem category is %q, want %q", outcome.Problem.Category, problem.CategoryUsage)
	}
}

func TestRunReportsAPlainHandlerErrorAsAModuleProcessProblem(t *testing.T) {
	outcome := testkit.Run(t.Context(), probeOptions(), []module.Command{failingCommand()},
		testkit.Invocation{Command: []string{"fail"}})

	if outcome.Err != nil {
		t.Fatalf("Run failed: %v", outcome.Err)
	}
	if outcome.Problem == nil {
		t.Fatal("Run reported no problem for a failing handler")
	}
	if outcome.Problem.Category != problem.CategoryModuleProcess {
		t.Errorf("problem category is %q, want %q", outcome.Problem.Category, problem.CategoryModuleProcess)
	}
}

func TestRunDeniesAccessTheInvocationDidNotScript(t *testing.T) {
	// A handler that expects a grant it never arranged must fail, or a test
	// could pass because the harness was more generous than a shell would be.
	outcome := testkit.Run(t.Context(), probeOptions(), []module.Command{acquireCommand()},
		testkit.Invocation{Command: []string{"acquire"}})

	if outcome.Err != nil {
		t.Fatalf("Run failed: %v", outcome.Err)
	}
	if outcome.Problem == nil {
		t.Fatal("an unscripted access request was not denied")
	}
	if outcome.Problem.Code != "testkit.access_not_scripted" {
		t.Errorf("denial code is %q, want %q", outcome.Problem.Code, "testkit.access_not_scripted")
	}
	if len(outcome.AccessRequests) != 1 {
		t.Fatalf("recorded %d access requests, want 1", len(outcome.AccessRequests))
	}
	if outcome.AccessRequests[0].Audience != "reference-api" {
		t.Errorf("recorded audience %q, want %q", outcome.AccessRequests[0].Audience, "reference-api")
	}
}

func TestRunGrantsAccessTheInvocationScripts(t *testing.T) {
	expires := time.Unix(1_800_000_000, 0)
	outcome := testkit.Run(t.Context(), probeOptions(), []module.Command{acquireCommand()},
		testkit.Invocation{
			Command: []string{"acquire"},
			Access:  &testkit.Access{Token: "a-token", ExpiresAt: expires},
		})

	if outcome.Err != nil {
		t.Fatalf("Run failed: %v", outcome.Err)
	}
	if outcome.Problem != nil {
		t.Fatalf("Run reported a problem for a scripted grant: %v", outcome.Problem)
	}
	if outcome.Result == nil {
		t.Fatal("Run reported no result")
	}
	if got := outcome.Result.Fields[0].Value; got != "a-token" {
		t.Errorf("handler observed token %q, want %q", got, "a-token")
	}
}

func TestRunDeniesAccessWithTheScriptedProblem(t *testing.T) {
	denial := problem.New(problem.CategoryAuthPolicy, "probe.no_scope", "the identity lacks the scope").
		WithRecovery("Grant the scope and retry.")
	outcome := testkit.Run(t.Context(), probeOptions(), []module.Command{acquireCommand()},
		testkit.Invocation{
			Command: []string{"acquire"},
			Access:  &testkit.Access{Deny: &denial},
		})

	if outcome.Err != nil {
		t.Fatalf("Run failed: %v", outcome.Err)
	}
	if outcome.Problem == nil {
		t.Fatal("Run reported no problem for a scripted denial")
	}
	if outcome.Problem.Code != "probe.no_scope" {
		t.Errorf("problem code is %q, want %q", outcome.Problem.Code, "probe.no_scope")
	}
}

func TestRunRecordsAccessRequestsInOrder(t *testing.T) {
	outcome := testkit.Run(t.Context(), probeOptions(), []module.Command{twiceCommand()},
		testkit.Invocation{
			Command: []string{"twice"},
			Access:  &testkit.Access{Token: "shared-token", ExpiresAt: time.Now()},
		})

	if outcome.Err != nil {
		t.Fatalf("Run failed: %v", outcome.Err)
	}
	if len(outcome.AccessRequests) != 2 {
		t.Fatalf("recorded %d access requests, want 2", len(outcome.AccessRequests))
	}
	if outcome.AccessRequests[0].Audience != "first" || outcome.AccessRequests[1].Audience != "second" {
		t.Errorf("access requests are %v, want first then second", outcome.AccessRequests)
	}
}

func TestRunDefaultsTheInvocationIdentifier(t *testing.T) {
	var seen module.Request
	command := module.Command{
		Path: []string{"status"},
		Run: func(_ context.Context, request module.Request) (result.Result, error) {
			seen = request
			return result.New("probe.status/v1").With("status", "Status", "operational"), nil
		},
	}

	outcome := testkit.Run(t.Context(), probeOptions(), []module.Command{command},
		testkit.Invocation{Command: []string{"status"}})

	if outcome.Err != nil {
		t.Fatalf("Run failed: %v", outcome.Err)
	}
	if seen.InvocationID != testkit.DefaultInvocationID {
		t.Errorf("invocation id is %q, want the default %q", seen.InvocationID, testkit.DefaultInvocationID)
	}
}

func TestRunFailsWhenTheInvocationTargetsAnotherNamespace(t *testing.T) {
	// The shell never routes a command to a namespace a module does not own;
	// scripting that anyway proves the module rejects it rather than the test
	// kit silently correcting it.
	outcome := testkit.Run(t.Context(), probeOptions(), []module.Command{statusCommand()},
		testkit.Invocation{Command: []string{"status"}, Namespace: "someone-else"})

	if outcome.Err == nil {
		t.Fatal("Run succeeded despite the invocation naming a namespace the module does not own")
	}
}

func TestRunFailsWhenTheInvocationSelectsAnUnofferedProtocolVersion(t *testing.T) {
	outcome := testkit.Run(t.Context(), probeOptions(), []module.Command{statusCommand()},
		testkit.Invocation{Command: []string{"status"}, ProtocolVersion: 999})

	if outcome.Err == nil {
		t.Fatal("Run succeeded despite selecting a protocol version the module never offered")
	}
}

func TestRunCarriesArgumentsAndContextToTheHandler(t *testing.T) {
	var seen module.Request
	command := module.Command{
		Path: []string{"status"},
		Run: func(_ context.Context, request module.Request) (result.Result, error) {
			seen = request
			return result.New("probe.status/v1").With("status", "Status", "operational"), nil
		},
	}

	outcome := testkit.Run(t.Context(), probeOptions(), []module.Command{command}, testkit.Invocation{
		Command:   []string{"status"},
		Arguments: []string{"--verbose"},
		Context:   module.Context{Name: "dev", OrganizationID: "acme"},
	})

	if outcome.Err != nil {
		t.Fatalf("Run failed: %v", outcome.Err)
	}
	if len(seen.Arguments) != 1 || seen.Arguments[0] != "--verbose" {
		t.Errorf("handler saw arguments %v, want [--verbose]", seen.Arguments)
	}
	if seen.Context.Name != "dev" || seen.Context.OrganizationID != "acme" {
		t.Errorf("handler saw context %+v, want name dev and org acme", seen.Context)
	}
}
