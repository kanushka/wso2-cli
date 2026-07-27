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

// Package testkit drives a module through the module contract without building
// or launching a subprocess.
//
// It exists so a module author can test handlers against the contract's
// observable behaviour — the messages a module actually emits — rather than
// against the SDK's internal calls. It is a conforming peer, not the shell: it
// performs no receipt resolution, no integrity check, and no rendering, and a
// module that only satisfies the test kit is not thereby proven to satisfy the
// shell.
package testkit

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/protocol"
	"github.com/wso2/wso2-cli/sdk/protocol/contractv1"
	"github.com/wso2/wso2-cli/sdk/result"
)

// Invocation is one scripted command run against a module.
type Invocation struct {
	// Command is the command path within the namespace, such as
	// []string{"status"}.
	Command []string
	// Arguments are the user arguments passed through unparsed.
	Arguments []string
	// OutputMode is the rendering the peer claims it will perform.
	OutputMode module.OutputMode
	// Context is the non-secret invocation context.
	Context module.Context
	// InvocationID identifies the invocation. It defaults to
	// DefaultInvocationID so results are reproducible.
	InvocationID string
	// ProtocolVersion is the version the peer selects. It defaults to the
	// newest version the module offered in its hello.
	ProtocolVersion int
	// Namespace overrides the namespace the peer invokes. It defaults to the
	// namespace the module declared, and exists so a test can prove a module
	// rejects a command routed to another namespace.
	Namespace string
}

// DefaultInvocationID is the invocation identifier used when an invocation does
// not set one.
const DefaultInvocationID = "testkit-invocation"

// Outcome is everything the peer observed.
//
// Exactly one of Result and Problem is set when Err is nil: the contract
// requires one terminal message per invocation.
type Outcome struct {
	// Hello is the runtime identity the module announced.
	Hello *contractv1.Hello
	// Result is the terminal success, if the module returned one.
	Result *result.Result
	// Problem is the terminal failure, if the module returned one.
	Problem *problem.Problem
	// Err reports that the exchange itself failed: the module wrote no
	// terminal message, wrote an unexpected one, or its serve loop returned
	// an error.
	Err error
}

// Run performs a complete handshake and invocation against the given commands.
//
// The module is served in this process over an in-memory stream pair, so a test
// needs no build step and no temporary files.
func Run(ctx context.Context, options module.Options, commands []module.Command, invocation Invocation) Outcome {
	toModule, moduleInput := io.Pipe()
	moduleOutput, fromModule := io.Pipe()

	served := make(chan error, 1)
	go func() {
		err := module.ServeStreams(ctx, toModule, fromModule, options, commands...)
		// Closing both ends unblocks the peer whatever the module did, so a
		// module that stops early fails the test rather than hanging it.
		_ = fromModule.Close()
		_ = toModule.Close()
		served <- err
	}()

	outcome := exchange(moduleInput, moduleOutput, options, invocation)
	_ = moduleInput.Close()
	_ = moduleOutput.Close()

	if serveErr := <-served; serveErr != nil && outcome.Err == nil {
		outcome.Err = serveErr
	}
	return outcome
}

// exchange plays the peer half of one invocation.
func exchange(toModule io.WriteCloser, fromModule io.Reader, options module.Options, invocation Invocation) Outcome {
	reader := protocol.NewReader(fromModule)
	writer := protocol.NewWriter(toModule)

	helloEnvelope, err := reader.ReadEnvelope()
	if err != nil {
		return Outcome{Err: fmt.Errorf("testkit: cannot read the module's hello: %w", err)}
	}
	hello := helloEnvelope.GetHello()
	if hello == nil {
		return Outcome{Err: errors.New("testkit: the module's first message was not a hello")}
	}
	outcome := Outcome{Hello: hello}

	invocationID := invocation.InvocationID
	if invocationID == "" {
		invocationID = DefaultInvocationID
	}
	selected := invocation.ProtocolVersion
	if selected == 0 {
		if len(hello.GetProtocolVersions()) == 0 {
			outcome.Err = errors.New("testkit: the module offered no protocol version")
			return outcome
		}
		selected = int(hello.GetProtocolVersions()[0])
	}
	namespace := invocation.Namespace
	if namespace == "" {
		namespace = options.Namespace
	}

	if err := writer.WriteEnvelope(&contractv1.Envelope{
		InvocationId: invocationID,
		Message: &contractv1.Envelope_Welcome{Welcome: &contractv1.Welcome{
			ProtocolVersion: uint32(selected),
			Shell:           &contractv1.ShellIdentity{Version: "0.0.0-testkit", Platform: "testkit"},
		}},
	}); err != nil {
		outcome.Err = fmt.Errorf("testkit: cannot send the welcome: %w", err)
		return outcome
	}

	if err := writer.WriteEnvelope(&contractv1.Envelope{
		InvocationId: invocationID,
		Message: &contractv1.Envelope_Invoke{Invoke: &contractv1.Invoke{
			Namespace:   namespace,
			CommandPath: invocation.Command,
			Arguments:   invocation.Arguments,
			OutputMode:  protocol.EncodeOutputMode(invocation.OutputMode),
			Policy:      &contractv1.InvocationPolicy{},
			Context: &contractv1.InvocationContext{
				Name:           invocation.Context.Name,
				OrganizationId: invocation.Context.OrganizationID,
			},
		}},
	}); err != nil {
		outcome.Err = fmt.Errorf("testkit: cannot send the invocation: %w", err)
		return outcome
	}

	terminal, err := reader.ReadEnvelope()
	if err != nil {
		outcome.Err = fmt.Errorf("testkit: the module returned no terminal message: %w", err)
		return outcome
	}
	if terminal.GetInvocationId() != invocationID {
		outcome.Err = fmt.Errorf("testkit: the terminal message carries invocation %q, want %q",
			terminal.GetInvocationId(), invocationID)
		return outcome
	}

	switch {
	case terminal.GetResult() != nil:
		decoded := protocol.DecodeResult(terminal.GetResult())
		outcome.Result = &decoded
	case terminal.GetProblem() != nil:
		decoded, err := protocol.DecodeProblem(terminal.GetProblem())
		if err != nil {
			outcome.Err = fmt.Errorf("testkit: the module returned an unreportable problem: %w", err)
			return outcome
		}
		outcome.Problem = &decoded
	default:
		outcome.Err = errors.New("testkit: the module's terminal message was neither a result nor a problem")
	}
	return outcome
}
