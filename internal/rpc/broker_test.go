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

package rpc

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/protocol/contractv1"
)

var grantedUntil = time.Date(2026, time.July, 27, 10, 2, 0, 0, time.UTC)

// recordingBroker answers with a fixed outcome and records what it was asked.
type recordingBroker struct {
	grant     auth.Grant
	denial    error
	requested []auth.Request
}

func (b *recordingBroker) Acquire(request auth.Request) (auth.Grant, error) {
	b.requested = append(b.requested, request)
	if b.denial != nil {
		return auth.Grant{}, b.denial
	}
	return b.grant, nil
}

// accessRequest is the message a module sends to ask for access.
func accessRequest() *contractv1.Envelope {
	return &contractv1.Envelope{
		InvocationId:  testInvocationID,
		CorrelationId: "acquire-1",
		Message: &contractv1.Envelope_AcquireAccess{AcquireAccess: &contractv1.AcquireAccess{
			Audience: "reference-status",
			Scopes:   []string{"reference:status:read"},
		}},
	}
}

// runBrokered drives a session with a broker against a fixed module stream.
func runBrokered(t *testing.T, broker Broker, fromModule *bytes.Buffer) (Outcome, []*contractv1.Envelope, error) {
	t.Helper()
	var toModule bytes.Buffer
	session := testSession()
	session.Broker = broker
	outcome, err := session.Run(&toModule, fromModule, statusInvocation())
	return outcome, shellMessages(t, &toModule), err
}

func TestAnAccessRequestIsAnsweredWithTheBrokersGrant(t *testing.T) {
	broker := &recordingBroker{grant: auth.Grant{Token: "fixture-token", ExpiresAt: grantedUntil}}

	outcome, written, err := runBrokered(t, broker, moduleStream(t, conformingHello(), accessRequest(), statusResult()))

	if err != nil {
		t.Fatalf("Run returned %v", err)
	}
	if outcome.Result.Schema != "reference.status/v1" {
		t.Errorf("the module's result did not survive the broker exchange: %+v", outcome.Result)
	}
	want := auth.Request{Audience: "reference-status", Scopes: []string{"reference:status:read"}}
	if !reflect.DeepEqual(broker.requested, []auth.Request{want}) {
		t.Fatalf("the broker was asked for %+v, want %+v", broker.requested, []auth.Request{want})
	}

	answer := lastAccessAnswer(t, written)
	granted := answer.GetAccessGranted()
	if granted == nil {
		t.Fatalf("the shell answered with %v, want a grant", answer.GetMessage())
	}
	if granted.GetToken() != "fixture-token" {
		t.Errorf("the shell granted token %q, want the broker's token", granted.GetToken())
	}
	if granted.GetExpiresAtUnix() != grantedUntil.Unix() {
		t.Errorf("the grant expires at %d, want %d", granted.GetExpiresAtUnix(), grantedUntil.Unix())
	}
	if answer.GetInvocationId() != testInvocationID {
		t.Errorf("the answer carries invocation %q, want %q", answer.GetInvocationId(), testInvocationID)
	}
	if answer.GetCorrelationId() != "acquire-1" {
		t.Errorf("the answer carries correlation %q, want the request's", answer.GetCorrelationId())
	}
}

func TestADeniedRequestIsAnsweredWithTheShellsProblem(t *testing.T) {
	denial := problem.New(problem.CategoryAuthPolicy, "auth.audience_not_declared",
		"the module asked for access its installation does not declare").
		WithRecovery("Reinstall the module.")
	broker := &recordingBroker{denial: denial}

	_, written, err := runBrokered(t, broker, moduleStream(t, conformingHello(), accessRequest(), statusResult()))

	if err != nil {
		t.Fatalf("Run returned %v", err)
	}
	answer := lastAccessAnswer(t, written)
	refused := answer.GetAccessDenied()
	if refused == nil {
		t.Fatalf("the shell answered with %v, want a denial", answer.GetMessage())
	}
	if got := refused.GetProblem(); got.GetCode() != denial.Code || got.GetCategory() != string(denial.Category) {
		t.Errorf("the denial arrived as %s/%s, want %s/%s",
			got.GetCategory(), got.GetCode(), denial.Category, denial.Code)
	}
	if refused.GetProblem().GetRecovery() != denial.Recovery {
		t.Errorf("the denial carries recovery %q, want %q",
			refused.GetProblem().GetRecovery(), denial.Recovery)
	}
	if answer.GetCorrelationId() != "acquire-1" {
		t.Errorf("the answer carries correlation %q, want the request's", answer.GetCorrelationId())
	}
}

func TestAnUntypedBrokerFailureStillDeniesAccess(t *testing.T) {
	// Whatever went wrong inside the shell, the module gets one typed answer:
	// an invocation that cannot be authorized is refused, never granted.
	broker := &recordingBroker{denial: errUnexpected{}}

	_, written, err := runBrokered(t, broker, moduleStream(t, conformingHello(), accessRequest(), statusResult()))

	if err != nil {
		t.Fatalf("Run returned %v", err)
	}
	refused := lastAccessAnswer(t, written).GetAccessDenied()
	if refused == nil {
		t.Fatal("an unexpected broker failure did not deny access")
	}
	if refused.GetProblem().GetCategory() != string(problem.CategoryAuthPolicy) {
		t.Errorf("the denial is in the %q class, want %q",
			refused.GetProblem().GetCategory(), problem.CategoryAuthPolicy)
	}
}

func TestAShellWithNoBrokerDeniesAccessRatherThanIgnoringIt(t *testing.T) {
	_, written, err := runBrokered(t, nil, moduleStream(t, conformingHello(), accessRequest(), statusResult()))

	if err != nil {
		t.Fatalf("Run returned %v", err)
	}
	if lastAccessAnswer(t, written).GetAccessDenied() == nil {
		t.Fatal("a shell that brokered no access left the request unanswered or granted it")
	}
}

func TestAnAccessRequestBoundToAnotherInvocationIsRefused(t *testing.T) {
	// Every post-handshake message is bound to one invocation, and an access
	// request is no exception.
	foreign := accessRequest()
	foreign.InvocationId = "another-invocation"
	broker := &recordingBroker{grant: auth.Grant{Token: "fixture-token", ExpiresAt: grantedUntil}}

	_, _, err := runBrokered(t, broker, moduleStream(t, conformingHello(), foreign, statusResult()))

	if err == nil {
		t.Fatal("the shell answered an access request for another invocation")
	}
	if len(broker.requested) != 0 {
		t.Errorf("the broker was consulted %d times for a foreign invocation", len(broker.requested))
	}
}

// lastAccessAnswer returns the shell's answer to the module's access request.
func lastAccessAnswer(t *testing.T, written []*contractv1.Envelope) *contractv1.Envelope {
	t.Helper()
	for _, envelope := range written {
		if envelope.GetAccessGranted() != nil || envelope.GetAccessDenied() != nil {
			return envelope
		}
	}
	t.Fatal("the shell never answered the module's access request")
	return nil
}

// errUnexpected is a failure that is not a typed problem.
type errUnexpected struct{}

func (errUnexpected) Error() string { return "the broker failed unexpectedly" }
