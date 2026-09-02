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
	"github.com/wso2/wso2-cli/internal/parsetree"
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
	// The tree comes from the receipt the resolver already verified, and from
	// nowhere else. See internal/parsetree.
	command, arguments, mode, contextName, err := parseProductArgs(
		namespace, parsetree.FromReceipt(resolved.Receipt), args)
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

	// The broker below performs the whole of this invocation's authentication —
	// it narrows the session to an audience and exchanges it — and refuses in
	// terms a module is allowed to hear, which is deliberately less than a
	// maintainer needs. This is where that missing half is written: the
	// invocation the access will be bound to, the ceiling the receipt declares,
	// and the derivation the selected identity will be narrowed by. Everything
	// here is public — a namespace, an audience list, a scheme name — and the
	// credential the broker reads is named nowhere, in keeping with a denial
	// never naming it either.
	s.log.Debug("brokering module access",
		"namespace", namespace,
		"invocation_id", invocationID,
		"context", selection.Context.Name,
		"organization", selection.Context.Organization,
		"grant_kind", selection.Identity.Auth.Kind,
		"declared_audiences", strings.Join(resolved.Receipt.Capabilities.AuthAudiences, " "),
		"declared_scopes", strings.Join(resolved.Receipt.Capabilities.AuthScopes, " "),
		"narrowing", selection.Identity.Auth.Derivation())

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
	selected, _, err := s.selectionAndDocument(flagName)
	return selected, err
}

// selectionAndDocument resolves the selection and hands back the document it
// was resolved from.
//
// A command that has to reason about contexts other than the selected one — such
// as wso2 logout, which names every context sharing the ended session — reads
// them from this document rather than loading it a second time. Two reads of a
// file a user can edit can disagree, and a command that reported one answer
// about the selection and another about its neighbours would be reporting a
// document that never existed.
func (s Shell) selectionAndDocument(flagName string) (contexts.Selection, contexts.Document, error) {
	name := flagName
	if name == "" {
		name = os.Getenv("WSO2_CONTEXT")
	}
	root, err := s.stateRoot()
	if err != nil {
		return contexts.Selection{}, contexts.Document{}, err
	}
	document, err := contexts.Load(root)
	if err != nil {
		return contexts.Selection{}, contexts.Document{}, err
	}
	selected, err := document.Select(name)
	return selected, document, err
}

// outputFlagValue reports the value carried by a single argument that spells the
// output flag with its value attached, and the empty string when the argument is
// not that. The separated spellings are handled by the caller, which can see the
// argument that follows.
// The bool reports whether the argument is the shell's attached output flag at
// all, which is separate from whether it carries a usable value. `--output=`
// claims the flag and supplies nothing; reading that as "not the shell's flag"
// would hand the shell's own malformed flag to the module instead of failing
// as the usage error it is.
func outputFlagValue(argument string) (string, bool) {
	for _, prefix := range []string{"--output=", "-o="} {
		if value, found := strings.CutPrefix(argument, prefix); found {
			return value, true
		}
	}
	// A shorthand may carry its value attached with no separator: -ojson. Only
	// an actual mode is claimed that way, so a module flag that merely starts
	// with -o stays the module's to interpret.
	if value, found := strings.CutPrefix(argument, "-o"); found && value != "" && !strings.HasPrefix(value, "=") {
		if _, ok := output.ParseMode(value); ok {
			return value, true
		}
	}
	return "", false
}

// attachedOutput reports whether the argument is the shell's output flag with
// its value attached, however it spells it.
func attachedOutput(argument string) bool {
	_, matched := outputFlagValue(argument)
	return matched
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
	// A following option is a missing value, not a context named "--output".
	// Taking it would refuse as an unknown context and send the reader looking
	// for a context they never asked for, instead of at the flag they left
	// empty. No context name may begin with "-", so this cannot swallow one.
	if strings.HasPrefix(args[1], "-") {
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
