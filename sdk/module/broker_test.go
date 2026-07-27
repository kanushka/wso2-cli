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

package module_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/result"
	"github.com/wso2/wso2-cli/sdk/testkit"
)

var grantedUntil = time.Date(2026, time.July, 27, 10, 2, 0, 0, time.UTC)

// acquiringCommand asks the broker for access and reports what it received.
func acquiringCommand(request module.AccessRequest, seen *module.Access, failure *error) module.Command {
	return module.Command{
		Path: []string{"status"},
		Run: func(ctx context.Context, invocation module.Request) (result.Result, error) {
			access, err := invocation.Access.Acquire(ctx, request)
			if err != nil {
				if failure != nil {
					*failure = err
				}
				return result.Result{}, err
			}
			if seen != nil {
				*seen = access
			}
			return result.New("probe.status/v1").With("status", "Status", "operational"), nil
		},
	}
}

func statusRequest() module.AccessRequest {
	return module.AccessRequest{Audience: "probe-status", Scopes: []string{"probe:status:read"}}
}

func TestAHandlerAsksTheBrokerForTheAccessItNeeds(t *testing.T) {
	var received module.Access

	outcome := testkit.Run(t.Context(), probeOptions(),
		[]module.Command{acquiringCommand(statusRequest(), &received, nil)},
		testkit.Invocation{
			Command: []string{"status"},
			Access:  &testkit.Access{Token: "fixture-token", ExpiresAt: grantedUntil},
		})

	if outcome.Err != nil {
		t.Fatalf("the invocation failed: %v", outcome.Err)
	}
	if len(outcome.AccessRequests) != 1 {
		t.Fatalf("the module sent %d access requests, want 1", len(outcome.AccessRequests))
	}
	if !reflect.DeepEqual(outcome.AccessRequests[0], statusRequest()) {
		t.Errorf("the broker was asked for %+v, want %+v", outcome.AccessRequests[0], statusRequest())
	}
	if received.Token != "fixture-token" {
		t.Errorf("the handler received token %q, want %q", received.Token, "fixture-token")
	}
	if !received.ExpiresAt.Equal(grantedUntil) {
		t.Errorf("the handler received expiry %s, want %s", received.ExpiresAt, grantedUntil)
	}
}

func TestAccessCarriesNothingBesidesTheTokenAndItsExpiry(t *testing.T) {
	// A module must be unable to reach the credential the access was derived
	// from, or to renew the access on its own.
	members := make([]string, 0)
	structure := reflect.TypeOf(module.Access{})
	for index := range structure.NumField() {
		members = append(members, structure.Field(index).Name)
	}

	if want := []string{"Token", "ExpiresAt"}; !reflect.DeepEqual(members, want) {
		t.Fatalf("access carries %v; it may carry only %v", members, want)
	}
}

func TestADeniedRequestReachesTheHandlerAsTheShellsProblem(t *testing.T) {
	// The shell owns authentication policy, so it owns the failure a user
	// reads. A handler propagates it rather than inventing its own.
	denial := problem.New(problem.CategoryAuthPolicy, "auth.audience_not_declared",
		"the module asked for access its installation does not declare").
		WithRecovery("Reinstall the module.")
	var handlerSaw error

	outcome := testkit.Run(t.Context(), probeOptions(),
		[]module.Command{acquiringCommand(statusRequest(), nil, &handlerSaw)},
		testkit.Invocation{
			Command: []string{"status"},
			Access:  &testkit.Access{Deny: &denial},
		})

	if outcome.Err != nil {
		t.Fatalf("the invocation failed: %v", outcome.Err)
	}
	var typed problem.Problem
	if !asProblem(handlerSaw, &typed) {
		t.Fatalf("the handler saw %v, want a typed problem", handlerSaw)
	}
	if typed != denial {
		t.Errorf("the handler saw %+v, want the shell's denial %+v", typed, denial)
	}
	if outcome.Problem == nil {
		t.Fatal("a denied invocation returned no terminal problem")
	}
	if *outcome.Problem != denial {
		t.Errorf("the terminal problem is %+v, want the shell's denial %+v", *outcome.Problem, denial)
	}
}

func TestAHandlerThatNeedsNoAccessAsksForNone(t *testing.T) {
	// Access is requested, never issued by default: a command that reads
	// nothing protected must produce no broker traffic at all.
	outcome := testkit.Run(t.Context(), probeOptions(), []module.Command{statusCommand(nil)},
		testkit.Invocation{Command: []string{"status"}})

	if outcome.Err != nil {
		t.Fatalf("the invocation failed: %v", outcome.Err)
	}
	if len(outcome.AccessRequests) != 0 {
		t.Fatalf("a handler that asked for nothing sent %d access requests", len(outcome.AccessRequests))
	}
}

func TestAnUnscriptedBrokerRefusesAccessRatherThanInventingIt(t *testing.T) {
	var handlerSaw error

	outcome := testkit.Run(t.Context(), probeOptions(),
		[]module.Command{acquiringCommand(statusRequest(), nil, &handlerSaw)},
		testkit.Invocation{Command: []string{"status"}})

	if outcome.Err != nil {
		t.Fatalf("the invocation failed: %v", outcome.Err)
	}
	if handlerSaw == nil {
		t.Fatal("the handler received access from a peer that scripted none")
	}
	if outcome.Problem == nil {
		t.Fatal("the invocation produced no terminal problem")
	}
}

func TestTheContextTellsAHandlerWhichServiceToCall(t *testing.T) {
	var seen module.Request

	outcome := testkit.Run(t.Context(), probeOptions(), []module.Command{statusCommand(&seen)},
		testkit.Invocation{
			Command: []string{"status"},
			Context: module.Context{Name: "probe-local", OrganizationID: "acme",
				Endpoint: "http://127.0.0.1:8080"},
		})

	if outcome.Err != nil {
		t.Fatalf("the invocation failed: %v", outcome.Err)
	}
	if seen.Context.Endpoint != "http://127.0.0.1:8080" {
		t.Errorf("the handler saw endpoint %q, want the context's endpoint", seen.Context.Endpoint)
	}
}

func asProblem(err error, target *problem.Problem) bool {
	typed, ok := err.(problem.Problem)
	if ok {
		*target = typed
	}
	return ok
}
