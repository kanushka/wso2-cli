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
	"encoding/binary"
	"errors"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/protocol"
	contractv1 "github.com/wso2/wso2-cli/sdk/protocol/contractv1"
)

const (
	testInvocationID = "inv-test"
	testNamespace    = "reference"
	testVersion      = "0.1.0"
)

// testSession is a session whose receipt describes a reference module at
// version 0.1.0 speaking protocol 1.
func testSession() Session {
	return Session{
		Resolved: modules.Resolved{
			Receipt: modules.Receipt{
				SchemaVersion: modules.ReceiptSchemaVersion,
				Namespace:     testNamespace,
				ModuleVersion: testVersion,
			},
			ExecutablePath:  "/managed/store/wso2-module-reference",
			ProtocolVersion: 1,
		},
		Shell:        ShellIdentity{Version: "0.0.0-dev", Platform: "test/arch"},
		InvocationID: testInvocationID,
	}
}

// conformingHello is the handshake a module matching testSession would send.
func conformingHello() *contractv1.Envelope {
	return &contractv1.Envelope{Message: &contractv1.Envelope_Hello{Hello: &contractv1.Hello{
		Module: &contractv1.ModuleIdentity{
			Namespace: testNamespace, Version: testVersion, SdkVersion: "0.1.0",
		},
		ProtocolVersions: []uint32{1},
	}}}
}

func statusResult() *contractv1.Envelope {
	return &contractv1.Envelope{
		InvocationId: testInvocationID,
		Message: &contractv1.Envelope_Result{Result: &contractv1.Result{
			Schema: "reference.status/v1",
			Fields: []*contractv1.ResultField{
				{Name: "organization", Label: "Organization", Value: "acme"},
				{Name: "status", Label: "Status", Value: "operational"},
			},
		}},
	}
}

// moduleStream encodes the frames a fake module would write, in order.
func moduleStream(t *testing.T, envelopes ...*contractv1.Envelope) *bytes.Buffer {
	t.Helper()
	var wire bytes.Buffer
	writer := protocol.NewWriter(&wire)
	for _, envelope := range envelopes {
		if err := writer.WriteEnvelope(envelope); err != nil {
			t.Fatalf("encoding the fake module's output: %v", err)
		}
	}
	return &wire
}

// statusInvocation is the invocation under test.
func statusInvocation() Invocation {
	return Invocation{
		Namespace:  testNamespace,
		Command:    []string{"status"},
		OutputMode: OutputJSON,
		Context:    InvocationContext{Name: "default"},
	}
}

// run drives a session against a fixed module stream and returns what the shell
// wrote alongside the outcome.
func run(t *testing.T, fromModule *bytes.Buffer) (Outcome, *bytes.Buffer, error) {
	t.Helper()
	var toModule bytes.Buffer
	outcome, err := testSession().Run(&toModule, fromModule, statusInvocation())
	return outcome, &toModule, err
}

// shellMessages decodes everything the shell wrote to the module.
func shellMessages(t *testing.T, wire *bytes.Buffer) []*contractv1.Envelope {
	t.Helper()
	reader := protocol.NewReader(wire)
	var envelopes []*contractv1.Envelope
	for {
		envelope, err := reader.ReadEnvelope()
		if err != nil {
			return envelopes
		}
		envelopes = append(envelopes, envelope)
	}
}

// problemCode reports the code of a typed failure, failing the test when the
// error is not one.
func problemCode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("the session reported no failure")
	}
	var typed problem.Problem
	if !errors.As(err, &typed) {
		t.Fatalf("the session failed with an untyped error: %v", err)
	}
	return typed.Code
}

// problemCategory reports the category of a typed failure.
func problemCategory(t *testing.T, err error) problem.Category {
	t.Helper()
	var typed problem.Problem
	if !errors.As(err, &typed) {
		t.Fatalf("the session failed with an untyped error: %v", err)
	}
	return typed.Category
}

func TestASuccessfulExchangeReturnsTheModulesResult(t *testing.T) {
	outcome, _, err := run(t, moduleStream(t, conformingHello(), statusResult()))
	if err != nil {
		t.Fatalf("a conforming exchange failed: %v", err)
	}
	if outcome.Problem != nil {
		t.Fatalf("a successful exchange carried a problem: %v", *outcome.Problem)
	}
	if outcome.Result.Schema != "reference.status/v1" {
		t.Errorf("result schema is %q, want %q", outcome.Result.Schema, "reference.status/v1")
	}
	if got, want := len(outcome.Result.Fields), 2; got != want {
		t.Fatalf("result carries %d fields, want %d", got, want)
	}
	if outcome.Result.Fields[0].Name != "organization" {
		t.Errorf("the first field is %q, want the module's declared order", outcome.Result.Fields[0].Name)
	}
}

func TestTheShellAnswersTheHandshakeWithTheNegotiatedProtocolAndItsIdentity(t *testing.T) {
	_, toModule, err := run(t, moduleStream(t, conformingHello(), statusResult()))
	if err != nil {
		t.Fatalf("a conforming exchange failed: %v", err)
	}

	sent := shellMessages(t, toModule)
	if len(sent) != 2 {
		t.Fatalf("the shell sent %d messages, want a welcome and an invocation", len(sent))
	}
	welcome := sent[0].GetWelcome()
	if welcome == nil {
		t.Fatal("the shell's first message was not a welcome")
	}
	if welcome.GetProtocolVersion() != 1 {
		t.Errorf("the welcome selected protocol v%d, want v1", welcome.GetProtocolVersion())
	}
	if welcome.GetShell().GetVersion() != "0.0.0-dev" || welcome.GetShell().GetPlatform() != "test/arch" {
		t.Errorf("the welcome reported shell identity %v, want the session's", welcome.GetShell())
	}
}

func TestEveryPostHandshakeMessageCarriesTheInvocationIdentifier(t *testing.T) {
	_, toModule, err := run(t, moduleStream(t, conformingHello(), statusResult()))
	if err != nil {
		t.Fatalf("a conforming exchange failed: %v", err)
	}

	for index, envelope := range shellMessages(t, toModule) {
		if envelope.GetInvocationId() != testInvocationID {
			t.Errorf("message %d carries invocation %q, want %q",
				index, envelope.GetInvocationId(), testInvocationID)
		}
	}
}

func TestTheInvocationCarriesTheCommandOutputModePolicyAndContext(t *testing.T) {
	_, toModule, err := run(t, moduleStream(t, conformingHello(), statusResult()))
	if err != nil {
		t.Fatalf("a conforming exchange failed: %v", err)
	}

	sent := shellMessages(t, toModule)
	invoke := sent[1].GetInvoke()
	if invoke == nil {
		t.Fatal("the shell's second message was not an invocation")
	}
	if invoke.GetNamespace() != testNamespace {
		t.Errorf("the invocation names namespace %q, want %q", invoke.GetNamespace(), testNamespace)
	}
	if got, want := strings.Join(invoke.GetCommandPath(), " "), "status"; got != want {
		t.Errorf("the invocation carries command %q, want %q", got, want)
	}
	if invoke.GetOutputMode() != contractv1.OutputMode_OUTPUT_MODE_JSON {
		t.Errorf("the invocation carries output mode %v, want JSON", invoke.GetOutputMode())
	}
	if invoke.GetPolicy().GetDeadlineMillis() != uint32(DefaultTimeout.Milliseconds()) {
		t.Errorf("the invocation carries deadline %dms, want the shell default %dms",
			invoke.GetPolicy().GetDeadlineMillis(), DefaultTimeout.Milliseconds())
	}
	if invoke.GetContext().GetName() != "default" {
		t.Errorf("the invocation carries context %q, want %q", invoke.GetContext().GetName(), "default")
	}
}

func TestTheModuleIsToldTheDeadlineItWillBeHeldTo(t *testing.T) {
	// A module cannot budget its own work against a deadline it was never
	// told, so the policy must carry the deadline actually in force rather
	// than the shell default.
	var toModule bytes.Buffer
	invocation := statusInvocation()
	invocation.Timeout = 4 * time.Second

	if _, err := testSession().Run(&toModule, moduleStream(t, conformingHello(), statusResult()), invocation); err != nil {
		t.Fatalf("a conforming exchange failed: %v", err)
	}

	sent := shellMessages(t, &toModule)
	if got := sent[1].GetInvoke().GetPolicy().GetDeadlineMillis(); got != 4000 {
		t.Errorf("the invocation carries deadline %dms, want 4000ms", got)
	}
}

func TestAModuleThatWillNotAcceptTheHandshakeIsAProcessProblem(t *testing.T) {
	// A module that closed its protocol input makes the shell's write fail.
	// That is a module process failure, not an internal shell error.
	_, err := testSession().Run(refusingWriter{}, moduleStream(t, conformingHello(), statusResult()),
		statusInvocation())

	if code := problemCode(t, err); code != "rpc.stream_failed" {
		t.Errorf("problem code is %q, want %q", code, "rpc.stream_failed")
	}
	if problemCategory(t, err) != problem.CategoryModuleProcess {
		t.Errorf("problem category is %q, want %q", problemCategory(t, err), problem.CategoryModuleProcess)
	}
}

// refusingWriter stands in for a module that stopped reading its input.
type refusingWriter struct{}

func (refusingWriter) Write([]byte) (int, error) { return 0, syscall.EPIPE }

func TestAModuleProblemIsCarriedRatherThanReplaced(t *testing.T) {
	// A product failure is a successful exchange. The module's category, code,
	// message, and recovery must reach the user unchanged.
	failure := &contractv1.Envelope{
		InvocationId: testInvocationID,
		Message: &contractv1.Envelope_Problem{Problem: &contractv1.Problem{
			Category: string(problem.CategoryProductService),
			Code:     "reference.status_unavailable",
			Message:  "the status service did not answer",
			Recovery: "Try again shortly.",
		}},
	}

	outcome, _, err := run(t, moduleStream(t, conformingHello(), failure))
	if err != nil {
		t.Fatalf("an exchange that returned a problem failed: %v", err)
	}
	if outcome.Problem == nil {
		t.Fatal("the module's problem did not reach the caller")
	}
	want := problem.New(problem.CategoryProductService, "reference.status_unavailable",
		"the status service did not answer").WithRecovery("Try again shortly.")
	if *outcome.Problem != want {
		t.Errorf("the problem arrived as %+v, want %+v", *outcome.Problem, want)
	}
}

func TestAModuleWhoseRuntimeIdentityContradictsItsReceiptIsRefused(t *testing.T) {
	tests := map[string]*contractv1.Hello{
		"another namespace": {
			Module:           &contractv1.ModuleIdentity{Namespace: "billing", Version: testVersion},
			ProtocolVersions: []uint32{1},
		},
		"another version": {
			Module:           &contractv1.ModuleIdentity{Namespace: testNamespace, Version: "9.9.9"},
			ProtocolVersions: []uint32{1},
		},
	}

	for name, hello := range tests {
		t.Run(name, func(t *testing.T) {
			stream := moduleStream(t,
				&contractv1.Envelope{Message: &contractv1.Envelope_Hello{Hello: hello}}, statusResult())

			outcome, toModule, err := run(t, stream)
			if code := problemCode(t, err); code != "rpc.identity_mismatch" {
				t.Errorf("problem code is %q, want %q", code, "rpc.identity_mismatch")
			}
			if category := problemCategory(t, err); category != problem.CategoryModuleTrust {
				t.Errorf("problem category is %q, want %q", category, problem.CategoryModuleTrust)
			}
			if outcome.Result.Schema != "" {
				t.Error("a refused module still produced a result")
			}
			// The command must never be sent to a module the shell does not
			// recognize, so the refusal has to precede the invocation.
			for _, envelope := range shellMessages(t, toModule) {
				if envelope.GetInvoke() != nil {
					t.Error("the shell invoked a module whose identity contradicted its receipt")
				}
			}
		})
	}
}

func TestAModuleThatDoesNotSpeakTheNegotiatedProtocolIsRefused(t *testing.T) {
	hello := &contractv1.Envelope{Message: &contractv1.Envelope_Hello{Hello: &contractv1.Hello{
		Module:           &contractv1.ModuleIdentity{Namespace: testNamespace, Version: testVersion},
		ProtocolVersions: []uint32{2, 3},
	}}}

	outcome, toModule, err := run(t, moduleStream(t, hello, statusResult()))
	if code := problemCode(t, err); code != "rpc.protocol_mismatch" {
		t.Errorf("problem code is %q, want %q", code, "rpc.protocol_mismatch")
	}
	if problemCategory(t, err) != problem.CategoryModuleTrust {
		t.Errorf("problem category is %q, want %q", problemCategory(t, err), problem.CategoryModuleTrust)
	}
	if outcome.Result.Schema != "" {
		t.Error("a refused module still produced a result")
	}
	for _, envelope := range shellMessages(t, toModule) {
		if envelope.GetInvoke() != nil {
			t.Error("the shell invoked a module before negotiation succeeded")
		}
	}
}

func TestARequiredCapabilityTheShellDoesNotProvideFailsClosed(t *testing.T) {
	// Ignoring the requirement would run the module with a behaviour it said
	// it cannot work without.
	hello := &contractv1.Envelope{Message: &contractv1.Envelope_Hello{Hello: &contractv1.Hello{
		Module:               &contractv1.ModuleIdentity{Namespace: testNamespace, Version: testVersion},
		ProtocolVersions:     []uint32{1},
		RequiredCapabilities: []string{"streaming"},
	}}}

	_, _, err := run(t, moduleStream(t, hello, statusResult()))
	if code := problemCode(t, err); code != "rpc.unsupported_capability" {
		t.Errorf("problem code is %q, want %q", code, "rpc.unsupported_capability")
	}
}

func TestAModuleThatOpensWithSomethingOtherThanAHandshakeIsRefused(t *testing.T) {
	_, _, err := run(t, moduleStream(t, statusResult()))
	if code := problemCode(t, err); code != "rpc.unexpected_message" {
		t.Errorf("problem code is %q, want %q", code, "rpc.unexpected_message")
	}
}

func TestAMissingTerminalMessageIsAProcessProblem(t *testing.T) {
	_, _, err := run(t, moduleStream(t, conformingHello()))
	if code := problemCode(t, err); code != "rpc.no_terminal_message" {
		t.Errorf("problem code is %q, want %q", code, "rpc.no_terminal_message")
	}
	if problemCategory(t, err) != problem.CategoryModuleProcess {
		t.Errorf("problem category is %q, want %q", problemCategory(t, err), problem.CategoryModuleProcess)
	}
}

func TestATerminalMessageForAnotherInvocationIsRefused(t *testing.T) {
	stray := statusResult()
	stray.InvocationId = "inv-somebody-else"

	_, _, err := run(t, moduleStream(t, conformingHello(), stray))
	if code := problemCode(t, err); code != "rpc.invocation_mismatch" {
		t.Errorf("problem code is %q, want %q", code, "rpc.invocation_mismatch")
	}
}

func TestProtocolFramesAfterTheTerminalMessageAreRefused(t *testing.T) {
	// Extra output means the module and the shell disagree about where the
	// invocation ended, so no part of it is believed.
	_, _, err := run(t, moduleStream(t, conformingHello(), statusResult(), statusResult()))
	if code := problemCode(t, err); code != "rpc.extra_message" {
		t.Errorf("problem code is %q, want %q", code, "rpc.extra_message")
	}
}

func TestATruncatedMessageIsRefused(t *testing.T) {
	complete := moduleStream(t, conformingHello(), statusResult())
	truncated := bytes.NewBuffer(complete.Bytes()[:complete.Len()-3])

	_, _, err := run(t, truncated)
	if code := problemCode(t, err); code != "rpc.truncated_message" {
		t.Errorf("problem code is %q, want %q", code, "rpc.truncated_message")
	}
}

func TestAMalformedMessageIsRefused(t *testing.T) {
	wire := moduleStream(t, conformingHello())
	payload := []byte{0xff, 0xff, 0xff, 0xff}
	var length [binary.MaxVarintLen64]byte
	wire.Write(length[:binary.PutUvarint(length[:], uint64(len(payload)))])
	wire.Write(payload)

	_, _, err := run(t, wire)
	if code := problemCode(t, err); code != "rpc.malformed_message" {
		t.Errorf("problem code is %q, want %q", code, "rpc.malformed_message")
	}
}

func TestAnOversizedMessageIsRefused(t *testing.T) {
	wire := moduleStream(t, conformingHello())
	var length [binary.MaxVarintLen64]byte
	wire.Write(length[:binary.PutUvarint(length[:], uint64(protocol.MaxFrameBytes)+1)])

	_, _, err := run(t, wire)
	if code := problemCode(t, err); code != "rpc.oversized_message" {
		t.Errorf("problem code is %q, want %q", code, "rpc.oversized_message")
	}
}

func TestAResultTheShellCannotRenderIsRefused(t *testing.T) {
	ambiguous := &contractv1.Envelope{
		InvocationId: testInvocationID,
		Message: &contractv1.Envelope_Result{Result: &contractv1.Result{
			Schema: "reference.status/v1",
			Fields: []*contractv1.ResultField{
				{Name: "status", Value: "operational"},
				{Name: "status", Value: "degraded"},
			},
		}},
	}

	_, _, err := run(t, moduleStream(t, conformingHello(), ambiguous))
	if code := problemCode(t, err); code != "rpc.invalid_result" {
		t.Errorf("problem code is %q, want %q", code, "rpc.invalid_result")
	}
}

func TestAProblemWithoutACodeIsRefused(t *testing.T) {
	// An unnamed failure gives a user nothing to act on or search for, so it
	// is reported as a module fault rather than passed through.
	nameless := &contractv1.Envelope{
		InvocationId: testInvocationID,
		Message: &contractv1.Envelope_Problem{Problem: &contractv1.Problem{
			Category: string(problem.CategoryProductService),
			Message:  "something went wrong",
		}},
	}

	_, _, err := run(t, moduleStream(t, conformingHello(), nameless))
	if code := problemCode(t, err); code != "rpc.invalid_problem" {
		t.Errorf("problem code is %q, want %q", code, "rpc.invalid_problem")
	}
}

func TestAnEnvelopeWithNoRecognizedMessageFailsClosed(t *testing.T) {
	// Skipping an unknown message kind risks skipping the one that mattered.
	empty := &contractv1.Envelope{InvocationId: testInvocationID}

	_, _, err := run(t, moduleStream(t, conformingHello(), empty))
	if code := problemCode(t, err); code != "rpc.unexpected_message" {
		t.Errorf("problem code is %q, want %q", code, "rpc.unexpected_message")
	}
}
