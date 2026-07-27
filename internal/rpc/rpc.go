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

// Package rpc runs one product command across the shell-to-module boundary.
//
// It is split so the protocol can be tested without a process and the process
// can be tested without a real module:
//
//   - Session speaks the module contract over a pair of streams. It negotiates
//     the handshake, sends one invocation, and returns one terminal outcome.
//   - Launcher owns the child process: a sanitized environment, the standard
//     stream wiring, bounded diagnostics, the invocation deadline, and
//     termination.
//
// Nothing here renders user output. Every failure leaves this package as a
// typed problem, and internal/output decides how it is shown. See
// docs/adr/0002-module-transport.md and docs/adr/0003-shell-owned-output.md.
package rpc

import (
	"time"

	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/protocol"
	"github.com/wso2/wso2-cli/sdk/result"
)

// DefaultTimeout is how long the shell waits for a terminal message before it
// closes the protocol stream and terminates the module.
const DefaultTimeout = 30 * time.Second

// TerminationGrace is how long a module has to exit after its protocol input
// is closed, before the shell kills it.
const TerminationGrace = 2 * time.Second

// DefaultDiagnosticLimit is the number of module standard-error bytes the shell
// keeps. Diagnostics are bounded so a chatty or looping module cannot exhaust
// shell memory.
const DefaultDiagnosticLimit = 8 << 10

// ShellIdentity is the shell-side identity reported to a module.
type ShellIdentity struct {
	// Version is the shell's release version.
	Version string
	// Platform is the "os/arch" pair the shell runs on.
	Platform string
}

// InvocationContext is the non-secret context a command runs against. It never
// carries credential material.
type InvocationContext struct {
	// Name is the selected context's name.
	Name string
	// OrganizationID is the organization the command targets. It is empty
	// until the shell owns a context store.
	OrganizationID string
}

// Invocation is one product command as the shell resolved it.
type Invocation struct {
	// Namespace is the resolved product namespace.
	Namespace string
	// Command is the command path within the namespace.
	Command []string
	// Arguments are the remaining user arguments, unparsed.
	Arguments []string
	// OutputMode is the rendering the shell will perform. It reaches the
	// module as advice only.
	OutputMode protocol.OutputMode
	// Context is the non-secret invocation context.
	Context InvocationContext
	// Interactive reports whether a terminal is attached.
	Interactive bool
	// Timeout bounds the whole exchange. Zero means DefaultTimeout.
	Timeout time.Duration
}

// timeout reports the deadline in force, applying the shell default. The module
// is told the same value it will actually be held to.
func (i Invocation) timeout() time.Duration {
	if i.Timeout <= 0 {
		return DefaultTimeout
	}
	return i.Timeout
}

// Outcome is the single terminal message a module returned.
//
// Exactly one of Result and Problem is set. A module problem is carried rather
// than returned as an error, because it is a successful exchange that reports a
// product failure; the caller renders it and maps it to an exit class.
type Outcome struct {
	// Result is the module's terminal success.
	Result result.Result
	// Problem is the module's terminal failure.
	Problem *problem.Problem
	// InvocationID identifies the exchange, for diagnostics.
	InvocationID string
	// Diagnostics is the module's bounded standard-error output.
	Diagnostics Diagnostics
}

// supportedCapabilities are the optional protocol behaviours this shell
// provides. A module may require only these.
//
// The architecture proof defines none: the contract's mandatory behaviour is
// all a module can depend on. A module that requires anything else fails
// closed, so it can never silently lose a behaviour it needs.
var supportedCapabilities = map[string]struct{}{}

// processProblem reports a protocol or module process failure.
func processProblem(code, message, recovery string) problem.Problem {
	return problem.New(problem.CategoryModuleProcess, code, message).WithRecovery(recovery)
}

// trustProblem reports a module integrity or compatibility failure.
func trustProblem(code, message, recovery string) problem.Problem {
	return problem.New(problem.CategoryModuleTrust, code, message).WithRecovery(recovery)
}

// reinstallRecovery is the guidance for a module whose installation no longer
// describes what actually runs.
const reinstallRecovery = "Reinstall the module so the shell can resolve an installation that matches the executable."

// retryRecovery is the guidance for a module that failed while running.
const retryRecovery = "Retry the command. Report the failure if it persists."
