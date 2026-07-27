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
	"strings"

	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/internal/rpc"
	"github.com/wso2/wso2-cli/internal/version"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// DefaultContextName is the context a command runs against until the shell owns
// a context store. The isolated reference context arrives with the
// authentication broker in the next slice increment.
const DefaultContextName = "default"

// invokeModule runs one product command in the resolved module and renders its
// outcome.
//
// The shell owns everything a user sees: it resolves the module, launches it,
// renders the result, attributes the module's diagnostics, and returns a typed
// problem for the exit class. The module contributes semantics only.
func (s Shell) invokeModule(namespace string, resolved modules.Resolved, args []string) error {
	command, arguments, mode, err := parseProductArgs(namespace, args)
	if err != nil {
		return err
	}

	launcher := rpc.Launcher{
		Resolved: resolved,
		Shell: rpc.ShellIdentity{
			Version:  version.Shell(),
			Platform: version.Platform(),
		},
	}
	outcome, invokeErr := launcher.Invoke(s.context(), rpc.Invocation{
		Namespace:   namespace,
		Command:     command,
		Arguments:   arguments,
		OutputMode:  outputModeFor(mode),
		Context:     rpc.InvocationContext{Name: DefaultContextName},
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

// context reports the cancellation context for one invocation.
func (s Shell) context() context.Context {
	if s.Context != nil {
		return s.Context
	}
	return context.Background()
}

// parseProductArgs separates the shell's own flags from the module's arguments.
//
// The shell parses only what it owns. Everything after the first argument it
// does not recognize belongs to the module, so a module can add flags without
// the shell being released.
func parseProductArgs(namespace string, args []string) (command, arguments []string, mode output.Mode, err error) {
	mode = output.ModeTable
	remaining := args

	for len(remaining) > 0 {
		argument := remaining[0]
		switch {
		case argument == "--output" || argument == "-o":
			if len(remaining) < 2 {
				return nil, nil, "", missingOutputValue(namespace, argument)
			}
			parsed, ok := output.ParseMode(remaining[1])
			if !ok {
				return nil, nil, "", unknownOutputMode(namespace, remaining[1])
			}
			mode = parsed
			remaining = remaining[2:]
		case strings.HasPrefix(argument, "--output="):
			value := strings.TrimPrefix(argument, "--output=")
			parsed, ok := output.ParseMode(value)
			if !ok {
				return nil, nil, "", unknownOutputMode(namespace, value)
			}
			mode = parsed
			remaining = remaining[1:]
		case strings.HasPrefix(argument, "-"):
			// An unrecognized flag is the module's to interpret or reject.
			return command, remaining, mode, nil
		default:
			// The command path is the leading run of plain words; everything
			// from the first flag onward is the module's.
			command = append(command, argument)
			remaining = remaining[1:]
		}
	}
	return command, remaining, mode, nil
}

func outputModeFor(mode output.Mode) rpc.OutputMode {
	if mode == output.ModeJSON {
		return rpc.OutputJSON
	}
	return rpc.OutputTable
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
