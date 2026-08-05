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

package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/internal/rpc"
	"github.com/wso2/wso2-cli/internal/version"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/protocol"
)

// invokeModule runs one product command in the resolved module and renders its
// outcome.
//
// The shell owns everything a user sees, and everything the module is trusted
// with: it resolves the module, selects the context, mints the invocation the
// broker binds access to, launches the module, renders the result, attributes
// the module's diagnostics, and returns a typed problem for the exit class. The
// module contributes semantics only.
func (s Shell) invokeModule(namespace string, resolved modules.Resolved, args []string) error {
	command, arguments, mode, contextName, err := parseProductArgs(namespace, args)
	if err != nil {
		return err
	}
	selection, err := s.selection(contextName)
	if err != nil {
		return err
	}
	// The state root reaches the broker for the session rotation lock alone.
	// No session material is written under it; the OS secure store holds all
	// of that.
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	// The invocation is minted here, before anything is launched, so the
	// broker and the module session bind access to the same command.
	invocationID, err := rpc.NewInvocationID()
	if err != nil {
		return problem.New(problem.CategoryModuleProcess, "shell.invocation_id_unavailable",
			"the shell cannot generate an invocation identifier").
			WithRecovery("Retry the command. Report the failure if it persists.")
	}

	launcher := rpc.Launcher{
		Resolved: resolved,
		Shell: rpc.ShellIdentity{
			Version:  version.Shell(),
			Platform: version.Platform(),
		},
		InvocationID: invocationID,
		// The broker is created per invocation and never leaves the shell.
		// The module receipt is its ceiling and the context is its source of
		// organization and credential; the module influences neither.
		Broker: &auth.Broker{
			Namespace:    namespace,
			Capabilities: resolved.Receipt.Capabilities,
			Selection:    selection,
			InvocationID: invocationID,
			StateRoot:    root,
		},
	}
	outcome, invokeErr := launcher.Invoke(context.Background(), rpc.Invocation{
		Namespace:  namespace,
		Command:    command,
		Arguments:  arguments,
		OutputMode: contractOutputMode(mode),
		Context: rpc.InvocationContext{
			Name:           selection.Context.Name,
			OrganizationID: selection.Context.Organization,
			Endpoint:       selection.Identity.Products[namespace].Endpoint,
		},
		Interactive: false,
	})

	// Diagnostics are rendered whatever the outcome, because what a module
	// wrote before failing is usually the only explanation of why.
	output.ModuleDiagnostics(s.Streams.Err, namespace, outcome.Diagnostics.Lines(), outcome.Diagnostics.Truncated)

	if invokeErr != nil {
		return invokeErr
	}
	if outcome.Problem != nil {
		return *outcome.Problem
	}
	return output.Result(s.Streams.Out, mode, outcome.Result)
}

// selection resolves the context this invocation runs against: the --context
// flag wins over the WSO2_CONTEXT environment variable, which wins over the
// document's default context.
//
// A shell with no context document still runs commands: the selection is then
// an empty one, and a module that needs access is refused by the broker with
// guidance rather than run against a guessed target.
func (s Shell) selection(flagName string) (contexts.Selection, error) {
	name := flagName
	if name == "" {
		name = os.Getenv("WSO2_CONTEXT")
	}
	root, err := s.stateRoot()
	if err != nil {
		return contexts.Selection{}, err
	}
	document, err := contexts.Load(root)
	if err != nil {
		return contexts.Selection{}, err
	}
	return document.Select(name)
}

// parseProductArgs separates the shell's own flags from the module's arguments.
//
// The shell parses only what it owns. Everything after the first argument it
// does not recognize belongs to the module, so a module can add flags without
// the shell being released.
func parseProductArgs(namespace string, args []string) (command, arguments []string, mode output.Mode, contextName string, err error) {
	mode = output.ModeTable
	remaining := args

	for len(remaining) > 0 {
		argument := remaining[0]
		switch {
		case argument == "--output" || argument == "-o":
			if len(remaining) < 2 {
				return nil, nil, "", "", missingOutputValue(namespace, argument)
			}
			parsed, ok := output.ParseMode(remaining[1])
			if !ok {
				return nil, nil, "", "", unknownOutputMode(namespace, remaining[1])
			}
			mode = parsed
			remaining = remaining[2:]
		case strings.HasPrefix(argument, "--output="):
			value := strings.TrimPrefix(argument, "--output=")
			parsed, ok := output.ParseMode(value)
			if !ok {
				return nil, nil, "", "", unknownOutputMode(namespace, value)
			}
			mode = parsed
			remaining = remaining[1:]
		case argument == "--context" || strings.HasPrefix(argument, "--context="):
			name, consumed := contextFlagValue(remaining)
			if name == "" {
				return nil, nil, "", "", missingContextValue(
					fmt.Sprintf("Run wso2 %s --context <name>.", namespace))
			}
			contextName = name
			remaining = remaining[consumed:]
		case strings.HasPrefix(argument, "-"):
			// An unrecognized flag is the module's to interpret or reject.
			return command, remaining, mode, contextName, nil
		default:
			// The command path is the leading run of plain words; everything
			// from the first flag onward is the module's.
			command = append(command, argument)
			remaining = remaining[1:]
		}
	}
	return command, remaining, mode, contextName, nil
}

// contractOutputMode maps the shell's rendering choice onto the contract value
// the module is told about.
//
// The two are separate types on purpose: output.Mode is what a user asked the
// shell to print, and the contract value is advice sent to a module. They
// happen to coincide today, and nothing should assume they always will.
func contractOutputMode(mode output.Mode) protocol.OutputMode {
	if mode == output.ModeJSON {
		return protocol.OutputModeJSON
	}
	return protocol.OutputModeTable
}

// contextFlagValue reads the value of a --context argument at the front of
// args, in either the "--context <name>" or "--context=<name>" spelling, and
// reports how many arguments it consumed. The caller has already established
// that args starts with one of those two spellings.
//
// An empty name means the flag carried no value. Every command refuses that
// rather than treating the flag as absent: a user who named a context
// explicitly must not silently get another one.
func contextFlagValue(args []string) (name string, consumed int) {
	if value, found := strings.CutPrefix(args[0], "--context="); found {
		return value, 1
	}
	if len(args) < 2 {
		return "", 1
	}
	return args[1], 2
}

// missingContextValue refuses a --context flag that carried no value. Every
// command states the refusal identically and differs only in the way back.
func missingContextValue(recovery string) problem.Problem {
	return problem.New(problem.CategoryUsage, "shell.missing_flag_value",
		"--context needs a value").
		WithRecovery(recovery)
}

func missingOutputValue(namespace, flag string) problem.Problem {
	return problem.New(problem.CategoryUsage, "shell.missing_flag_value",
		fmt.Sprintf("%s needs a value", flag)).
		WithRecovery(fmt.Sprintf("Run wso2 %s --output %s.", namespace, output.ModeTable))
}

func unknownOutputMode(namespace, value string) problem.Problem {
	supported := make([]string, 0, len(output.Modes()))
	for _, mode := range output.Modes() {
		supported = append(supported, string(mode))
	}
	return problem.New(problem.CategoryUsage, "shell.unknown_output_mode",
		fmt.Sprintf("%q is not an output mode this shell renders", value)).
		WithRecovery(fmt.Sprintf("Run wso2 %s --output %s.", namespace, strings.Join(supported, " or --output ")))
}
