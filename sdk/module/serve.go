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

package module

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/protocol"
	"github.com/wso2/wso2-cli/sdk/protocol/contractv1"
	"github.com/wso2/wso2-cli/sdk/result"
)

// OutputMode is the rendering the shell will apply to a result.
//
// It reaches a handler as advice, never as permission: a module may use it to
// skip work whose only purpose is display, but it must not format or write user
// output itself. It is the contract's own type, so a handler and the shell
// cannot disagree about what a mode means.
type OutputMode = protocol.OutputMode

const (
	// OutputModeUnspecified means the shell did not state a mode.
	OutputModeUnspecified = protocol.OutputModeUnspecified
	// OutputModeTable means the shell will render a human-readable table.
	OutputModeTable = protocol.OutputModeTable
	// OutputModeJSON means the shell will render deterministic JSON.
	OutputModeJSON = protocol.OutputModeJSON
)

// Context is the non-secret invocation context the command runs against.
//
// It never carries credential material. A module obtains access only through
// the shell's authentication broker.
type Context struct {
	// Name is the selected context's name.
	Name string
	// OrganizationID is the organization the command targets. It is empty
	// when no context is selected.
	OrganizationID string
	// Endpoint is the product service the context targets. It says where to
	// call, never that the module may: access still comes from the broker.
	Endpoint string
}

// Request is one command invocation as the shell described it.
type Request struct {
	// InvocationID identifies this shell invocation. A module may include it
	// in its own diagnostics.
	InvocationID string
	// Command is the command path within the namespace. "wso2 reference
	// status" arrives as []string{"status"}.
	Command []string
	// Arguments are the remaining user arguments. The shell does not parse
	// them; the module owns its own flags.
	Arguments []string
	// OutputMode is the shell's advisory rendering mode.
	OutputMode OutputMode
	// Context is the non-secret invocation context.
	Context Context
	// Access is the shell's authentication broker for this invocation. A
	// handler that needs to call a protected service asks it for access; a
	// handler that does not, never touches it.
	Access Broker
}

// Handler runs one product command.
//
// It returns a result or an error. Returning a problem.Problem gives the shell
// a typed failure with a stable category and code; any other error is reported
// as a module process failure.
type Handler func(ctx context.Context, request Request) (result.Result, error)

// Command binds a command path to its handler.
type Command struct {
	// Path is the command path within the namespace, such as
	// []string{"status"}. An empty path is the namespace's own default
	// command.
	Path []string
	// Run executes the command.
	Run Handler
}

// Serve speaks the module contract over the process's standard input and
// standard output until the invocation ends.
//
// Standard output carries protocol frames only. A module writes diagnostics to
// standard error and never writes user-facing output. See
// docs/adr/0002-module-transport.md.
func Serve(ctx context.Context, options Options, commands ...Command) error {
	// Asked to declare itself, a module reports what it accepts and exits
	// without opening the protocol. This is how the shell learns a module's
	// command tree from the executable it installed rather than from the
	// catalog it downloaded, which it cannot authenticate.
	if path := os.Getenv(CommandTreeEnv); path != "" {
		return declare(path, options)
	}
	return ServeStreams(ctx, os.Stdin, os.Stdout, options, commands...)
}

// ServeStreams is Serve over explicit streams. Tests and the SDK test kit use
// it to drive a module without a subprocess.
func ServeStreams(ctx context.Context, in io.Reader, out io.Writer, options Options, commands ...Command) error {
	descriptor := Describe(options)
	reader := protocol.NewReader(in)
	writer := protocol.NewWriter(out)

	if err := sendHello(writer, descriptor); err != nil {
		return err
	}

	invocationID, err := readWelcome(reader, descriptor)
	if err != nil {
		return err
	}

	invoke, err := readInvoke(reader, invocationID, descriptor)
	if err != nil {
		return err
	}

	// The broker shares this invocation's streams, so a handler's access
	// request travels as an ordinary contract message and the shell answers it
	// before the terminal message is written.
	broker := &streamBroker{reader: reader, writer: writer, invocationID: invocationID}

	// From here every outcome is a terminal message, so the shell always has
	// something typed to render rather than a silent exit.
	produced, failure := runCommand(ctx, descriptor, commands, invocationID, invoke, broker)
	if failure != nil {
		return writer.WriteEnvelope(&contractv1.Envelope{
			InvocationId: invocationID,
			Message:      &contractv1.Envelope_Problem{Problem: protocol.EncodeProblem(*failure)},
		})
	}
	return writer.WriteEnvelope(&contractv1.Envelope{
		InvocationId: invocationID,
		Message:      &contractv1.Envelope_Result{Result: protocol.EncodeResult(produced)},
	})
}

// sendHello opens the handshake by stating who is actually running.
func sendHello(writer *protocol.Writer, descriptor Descriptor) error {
	return writer.WriteEnvelope(&contractv1.Envelope{
		Message: &contractv1.Envelope_Hello{Hello: &contractv1.Hello{
			Module: &contractv1.ModuleIdentity{
				Namespace:  descriptor.Namespace,
				Version:    descriptor.Version,
				SdkVersion: descriptor.SDKVersion,
			},
			ProtocolVersions: protocol.EncodeVersions(descriptor.ProtocolVersions),
			// The SDK declares no required capability, because the contract's
			// mandatory behaviour is all a module can depend on today. A
			// module that needed one could not be served by this release.
		}},
	})
}

// readWelcome closes the handshake, binding this process to one protocol
// version and one invocation identity.
func readWelcome(reader *protocol.Reader, descriptor Descriptor) (string, error) {
	envelope, err := reader.ReadEnvelope()
	if err != nil {
		return "", fmt.Errorf("module: cannot read the shell's welcome: %w", err)
	}
	welcome := envelope.GetWelcome()
	if welcome == nil {
		return "", fmt.Errorf("module: expected a welcome, got %s", protocol.DescribeMessage(envelope))
	}
	invocationID := envelope.GetInvocationId()
	if invocationID == "" {
		return "", errors.New("module: the shell's welcome carries no invocation identifier")
	}
	// The shell must select from what this module offered, so a shell that
	// picks an unsupported version is rejected rather than guessed at.
	selected := int(welcome.GetProtocolVersion())
	if !slices.Contains(descriptor.ProtocolVersions, selected) {
		return "", fmt.Errorf("module: the shell selected protocol v%d, and this module speaks %s",
			selected, protocol.FormatVersions(descriptor.ProtocolVersions))
	}
	return invocationID, nil
}

// readInvoke reads the single command invocation of this session.
func readInvoke(reader *protocol.Reader, invocationID string, descriptor Descriptor) (*contractv1.Invoke, error) {
	envelope, err := reader.ReadEnvelope()
	if err != nil {
		return nil, fmt.Errorf("module: cannot read the invocation: %w", err)
	}
	invoke := envelope.GetInvoke()
	if invoke == nil {
		return nil, fmt.Errorf("module: expected an invocation, got %s", protocol.DescribeMessage(envelope))
	}
	if envelope.GetInvocationId() != invocationID {
		return nil, errors.New("module: the invocation identifier changed after the handshake")
	}
	// A module owns exactly one namespace, so a command routed to another
	// namespace is a shell fault rather than an unknown command.
	if invoke.GetNamespace() != descriptor.Namespace {
		return nil, fmt.Errorf("module: the shell invoked namespace %q on the %q module",
			invoke.GetNamespace(), descriptor.Namespace)
	}
	return invoke, nil
}

// runCommand dispatches one invocation to its handler and returns either a
// validated result or the problem to report.
func runCommand(
	ctx context.Context,
	descriptor Descriptor,
	commands []Command,
	invocationID string,
	invoke *contractv1.Invoke,
	broker Broker,
) (result.Result, *problem.Problem) {
	path := invoke.GetCommandPath()
	handler := findHandler(commands, path)
	if handler == nil {
		failure := problem.New(problem.CategoryUsage, descriptor.Namespace+".unknown_command",
			fmt.Sprintf("the %q module does not implement %q", descriptor.Namespace,
				strings.TrimSpace(descriptor.Namespace+" "+strings.Join(path, " ")))).
			WithRecovery("Run wso2 help to see the available commands.")
		return result.Result{}, &failure
	}

	request := Request{
		InvocationID: invocationID,
		Command:      path,
		Arguments:    invoke.GetArguments(),
		OutputMode:   protocol.DecodeOutputMode(invoke.GetOutputMode()),
		Context: Context{
			Name:           invoke.GetContext().GetName(),
			OrganizationID: invoke.GetContext().GetOrganizationId(),
			Endpoint:       invoke.GetContext().GetEndpoint(),
		},
		Access: broker,
	}

	produced, err := call(ctx, descriptor, handler, request)
	if err != nil {
		var typed problem.Problem
		if errors.As(err, &typed) {
			return result.Result{}, &typed
		}
		failure := problem.New(problem.CategoryModuleProcess, descriptor.Namespace+".handler_failed", err.Error())
		return result.Result{}, &failure
	}
	// A result the shell could not render is caught here, where the module
	// still can report a typed problem instead of an unreadable success.
	if err := produced.Validate(); err != nil {
		failure := problem.New(problem.CategoryModuleProcess, descriptor.Namespace+".invalid_result", err.Error())
		return result.Result{}, &failure
	}
	return produced, nil
}

// call runs a handler, converting a panic into an error so a module fault
// still reaches the shell as one terminal problem.
func call(ctx context.Context, descriptor Descriptor, handler Handler, request Request) (
	produced result.Result, err error,
) {
	defer func() {
		if recovered := recover(); recovered != nil {
			// The panic value and stack go to standard error, where the
			// shell captures them as bounded diagnostics. They are kept out
			// of the problem because they are runtime text no module author
			// curated for a user.
			fmt.Fprintf(os.Stderr, "%s: handler panicked: %v\n%s\n", descriptor.Namespace, recovered, debug.Stack())
			err = problem.New(problem.CategoryModuleProcess, descriptor.Namespace+".handler_panicked",
				fmt.Sprintf("the %q module failed while handling the command", descriptor.Namespace)).
				WithRecovery("Retry the command. Report the failure if it persists.")
		}
	}()
	return handler(ctx, request)
}

// findHandler selects the command whose path matches, treating command paths
// as exact.
func findHandler(commands []Command, path []string) Handler {
	for _, command := range commands {
		if command.Run != nil && slices.Equal(command.Path, path) {
			return command.Run
		}
	}
	return nil
}
