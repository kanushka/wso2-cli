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
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/protocol"
	"github.com/wso2/wso2-cli/sdk/protocol/contractv1"
	"github.com/wso2/wso2-cli/sdk/result"
	"github.com/wso2/wso2-cli/sdk/testkit"
)

// probeOptions describe a module used only to exercise the contract.
func probeOptions() module.Options {
	return module.Options{Namespace: "probe", Version: "1.2.3"}
}

// statusCommand answers with a fixed result and records what it was asked.
func statusCommand(seen *module.Request) module.Command {
	return module.Command{
		Path: []string{"status"},
		Run: func(_ context.Context, request module.Request) (result.Result, error) {
			if seen != nil {
				*seen = request
			}
			return result.New("probe.status/v1").
				With("organization", "Organization", "acme").
				With("status", "Status", "operational"), nil
		},
	}
}

func TestAModuleAnnouncesItsRuntimeIdentityBeforeAnythingElse(t *testing.T) {
	// The shell compares this identity with the module receipt, so a module
	// must state it before any command could run.
	outcome := testkit.Run(t.Context(), probeOptions(), []module.Command{statusCommand(nil)},
		testkit.Invocation{Command: []string{"status"}})

	if outcome.Err != nil {
		t.Fatalf("the invocation failed: %v", outcome.Err)
	}
	identity := outcome.Hello.GetModule()
	if identity.GetNamespace() != "probe" {
		t.Errorf("hello declares namespace %q, want %q", identity.GetNamespace(), "probe")
	}
	if identity.GetVersion() != "1.2.3" {
		t.Errorf("hello declares module version %q, want %q", identity.GetVersion(), "1.2.3")
	}
	if identity.GetSdkVersion() == "" {
		t.Error("hello declares no SDK version")
	}
	if len(outcome.Hello.GetProtocolVersions()) == 0 {
		t.Error("hello offers no protocol version")
	}
}

func TestASuccessfulInvocationReturnsOneTypedResult(t *testing.T) {
	outcome := testkit.Run(t.Context(), probeOptions(), []module.Command{statusCommand(nil)},
		testkit.Invocation{Command: []string{"status"}})

	if outcome.Err != nil {
		t.Fatalf("the invocation failed: %v", outcome.Err)
	}
	if outcome.Problem != nil {
		t.Fatalf("the module returned a problem: %v", *outcome.Problem)
	}
	if outcome.Result.Schema != "probe.status/v1" {
		t.Errorf("result schema is %q, want %q", outcome.Result.Schema, "probe.status/v1")
	}
	if got, want := len(outcome.Result.Fields), 2; got != want {
		t.Fatalf("result carries %d fields, want %d", got, want)
	}
	if outcome.Result.Fields[0].Name != "organization" || outcome.Result.Fields[1].Name != "status" {
		t.Errorf("result fields arrived as %v, want them in declared order", outcome.Result.Fields)
	}
}

func TestTheInvocationReachesTheHandlerIntact(t *testing.T) {
	var seen module.Request
	outcome := testkit.Run(t.Context(), probeOptions(), []module.Command{statusCommand(&seen)},
		testkit.Invocation{
			Command:      []string{"status"},
			Arguments:    []string{"--since", "1h"},
			OutputMode:   module.OutputModeJSON,
			Context:      module.Context{Name: "default", OrganizationID: "acme"},
			InvocationID: "inv-42",
		})

	if outcome.Err != nil {
		t.Fatalf("the invocation failed: %v", outcome.Err)
	}
	if got, want := strings.Join(seen.Arguments, " "), "--since 1h"; got != want {
		t.Errorf("the handler saw arguments %q, want %q", got, want)
	}
	if seen.OutputMode != module.OutputModeJSON {
		t.Errorf("the handler saw output mode %q, want %q", seen.OutputMode, module.OutputModeJSON)
	}
	if seen.Context.Name != "default" || seen.Context.OrganizationID != "acme" {
		t.Errorf("the handler saw context %+v, want the invocation context", seen.Context)
	}
	if seen.InvocationID != "inv-42" {
		t.Errorf("the handler saw invocation %q, want %q", seen.InvocationID, "inv-42")
	}
}

func TestAnUnknownCommandBecomesAUsageProblem(t *testing.T) {
	outcome := testkit.Run(t.Context(), probeOptions(), []module.Command{statusCommand(nil)},
		testkit.Invocation{Command: []string{"teleport"}})

	if outcome.Err != nil {
		t.Fatalf("the invocation failed: %v", outcome.Err)
	}
	if outcome.Problem == nil {
		t.Fatal("an unknown command produced no problem")
	}
	if outcome.Problem.Category != problem.CategoryUsage {
		t.Errorf("problem category is %q, want %q", outcome.Problem.Category, problem.CategoryUsage)
	}
	if outcome.Problem.Code != "probe.unknown_command" {
		t.Errorf("problem code is %q, want %q", outcome.Problem.Code, "probe.unknown_command")
	}
}

func TestAHandlerProblemTravelsWithItsCategoryAndRecovery(t *testing.T) {
	failing := module.Command{
		Path: []string{"status"},
		Run: func(context.Context, module.Request) (result.Result, error) {
			return result.Result{}, problem.New(problem.CategoryProductService, "probe.status_unavailable",
				"the status service did not answer").WithRecovery("Try again shortly.")
		},
	}

	outcome := testkit.Run(t.Context(), probeOptions(), []module.Command{failing},
		testkit.Invocation{Command: []string{"status"}})

	if outcome.Err != nil {
		t.Fatalf("the invocation failed: %v", outcome.Err)
	}
	if outcome.Problem == nil {
		t.Fatal("a handler problem produced no problem")
	}
	want := problem.New(problem.CategoryProductService, "probe.status_unavailable",
		"the status service did not answer").WithRecovery("Try again shortly.")
	if *outcome.Problem != want {
		t.Errorf("problem arrived as %+v, want %+v", *outcome.Problem, want)
	}
}

func TestAnUntypedHandlerErrorStillReturnsATerminalProblem(t *testing.T) {
	failing := module.Command{
		Path: []string{"status"},
		Run: func(context.Context, module.Request) (result.Result, error) {
			return result.Result{}, errors.New("the local socket is closed")
		},
	}

	outcome := testkit.Run(t.Context(), probeOptions(), []module.Command{failing},
		testkit.Invocation{Command: []string{"status"}})

	if outcome.Err != nil {
		t.Fatalf("the invocation failed: %v", outcome.Err)
	}
	if outcome.Problem == nil {
		t.Fatal("an untyped handler error produced no problem")
	}
	if outcome.Problem.Category != problem.CategoryModuleProcess {
		t.Errorf("problem category is %q, want %q", outcome.Problem.Category, problem.CategoryModuleProcess)
	}
	if outcome.Problem.Code != "probe.handler_failed" {
		t.Errorf("problem code is %q, want %q", outcome.Problem.Code, "probe.handler_failed")
	}
}

func TestAPanickingHandlerReturnsATerminalProblemWithoutRuntimeText(t *testing.T) {
	// A panic is a module fault, not a message the user should read. It must
	// still produce one terminal problem so the shell never reports a silent
	// exit.
	panicking := module.Command{
		Path: []string{"status"},
		Run: func(context.Context, module.Request) (result.Result, error) {
			panic("secret-looking panic detail")
		},
	}

	outcome := testkit.Run(t.Context(), probeOptions(), []module.Command{panicking},
		testkit.Invocation{Command: []string{"status"}})

	if outcome.Err != nil {
		t.Fatalf("the invocation failed: %v", outcome.Err)
	}
	if outcome.Problem == nil {
		t.Fatal("a panicking handler produced no problem")
	}
	if outcome.Problem.Code != "probe.handler_panicked" {
		t.Errorf("problem code is %q, want %q", outcome.Problem.Code, "probe.handler_panicked")
	}
	if strings.Contains(outcome.Problem.Message, "secret-looking panic detail") {
		t.Error("the problem repeats the panic value; runtime text must stay in diagnostics")
	}
}

func TestAResultTheShellCouldNotRenderBecomesAProblem(t *testing.T) {
	// A module that emits duplicate field names would produce ambiguous JSON,
	// so the fault is caught while a typed problem is still possible.
	ambiguous := module.Command{
		Path: []string{"status"},
		Run: func(context.Context, module.Request) (result.Result, error) {
			return result.New("probe.status/v1").
				With("status", "Status", "operational").
				With("status", "Status", "degraded"), nil
		},
	}

	outcome := testkit.Run(t.Context(), probeOptions(), []module.Command{ambiguous},
		testkit.Invocation{Command: []string{"status"}})

	if outcome.Err != nil {
		t.Fatalf("the invocation failed: %v", outcome.Err)
	}
	if outcome.Problem == nil {
		t.Fatal("an unrenderable result produced no problem")
	}
	if outcome.Problem.Code != "probe.invalid_result" {
		t.Errorf("problem code is %q, want %q", outcome.Problem.Code, "probe.invalid_result")
	}
}

func TestACommandRoutedToAnotherNamespaceIsRefused(t *testing.T) {
	// A module owns exactly one namespace. Acting on another namespace's
	// command would let a routing fault run the wrong product code.
	outcome := testkit.Run(t.Context(), probeOptions(), []module.Command{statusCommand(nil)},
		testkit.Invocation{Command: []string{"status"}, Namespace: "other"})

	if outcome.Err == nil {
		t.Fatal("the module accepted a command routed to another namespace")
	}
	if outcome.Result != nil {
		t.Error("the module produced a result for another namespace's command")
	}
}

func TestAShellThatSelectsAnUnsupportedProtocolIsRefused(t *testing.T) {
	// Negotiation is not advice: a peer must select from what the module
	// offered, so an unsupported selection fails closed before any command
	// runs.
	outcome := testkit.Run(t.Context(), probeOptions(), []module.Command{statusCommand(nil)},
		testkit.Invocation{Command: []string{"status"}, ProtocolVersion: 99})

	if outcome.Err == nil {
		t.Fatal("the module accepted a protocol version it does not speak")
	}
	if outcome.Result != nil || outcome.Problem != nil {
		t.Error("the module ran a command after a failed negotiation")
	}
}

func TestAWelcomeWithoutAnInvocationIdentifierIsRefused(t *testing.T) {
	// Every post-handshake message is bound to one invocation, so a handshake
	// that establishes none cannot proceed.
	var toModule, fromModule bytes.Buffer
	if err := protocol.NewWriter(&toModule).WriteEnvelope(&contractv1.Envelope{
		Message: &contractv1.Envelope_Welcome{Welcome: &contractv1.Welcome{ProtocolVersion: 1}},
	}); err != nil {
		t.Fatalf("writing the welcome: %v", err)
	}

	err := module.ServeStreams(t.Context(), &toModule, &fromModule, probeOptions(), statusCommand(nil))
	if err == nil {
		t.Fatal("the module accepted a welcome with no invocation identifier")
	}
}

func TestAnInvocationIdentifierThatChangesAfterTheHandshakeIsRefused(t *testing.T) {
	var toModule, fromModule bytes.Buffer
	writer := protocol.NewWriter(&toModule)
	if err := writer.WriteEnvelope(&contractv1.Envelope{
		InvocationId: "inv-1",
		Message:      &contractv1.Envelope_Welcome{Welcome: &contractv1.Welcome{ProtocolVersion: 1}},
	}); err != nil {
		t.Fatalf("writing the welcome: %v", err)
	}
	if err := writer.WriteEnvelope(&contractv1.Envelope{
		InvocationId: "inv-2",
		Message: &contractv1.Envelope_Invoke{Invoke: &contractv1.Invoke{
			Namespace: "probe", CommandPath: []string{"status"},
		}},
	}); err != nil {
		t.Fatalf("writing the invocation: %v", err)
	}

	err := module.ServeStreams(t.Context(), &toModule, &fromModule, probeOptions(), statusCommand(nil))
	if err == nil {
		t.Fatal("the module accepted an invocation bound to another invocation identifier")
	}
}
