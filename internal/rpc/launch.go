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
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/wso2/wso2-cli/internal/modules"
)

// Launcher runs one resolved module as a short-lived child process.
//
// It launches only the executable the active module receipt selected, whose
// digest was recomputed during resolution. PATH and the working directory are
// never consulted, so a same-named executable elsewhere cannot be reached.
type Launcher struct {
	// Resolved is the integrity-checked installation to launch.
	Resolved modules.Resolved
	// Shell is the identity reported to the module.
	Shell ShellIdentity
	// InvocationID binds this invocation's messages together. It is
	// generated when empty.
	InvocationID string
	// DiagnosticLimit bounds captured standard error. Zero means
	// DefaultDiagnosticLimit.
	DiagnosticLimit int
}

// Invoke launches the module, runs one command, and returns its terminal
// outcome with the module's bounded diagnostics.
//
// Whatever happens, the child process is finished with by the time Invoke
// returns: a module that never answers is terminated rather than left to block
// the shell.
func (l Launcher) Invoke(ctx context.Context, invocation Invocation) (Outcome, error) {
	invocationID := l.InvocationID
	if invocationID == "" {
		generated, err := newInvocationID()
		if err != nil {
			return Outcome{}, processProblem("rpc.invocation_id_unavailable",
				"the shell cannot generate an invocation identifier", retryRecovery)
		}
		invocationID = generated
	}

	command := exec.Command(l.Resolved.ExecutablePath)
	// The module inherits nothing from the shell's environment. A later slice
	// increment adds the authentication broker; until then a module has no
	// path by which an environment secret could reach it.
	command.Env = sanitizedEnvironment()

	toModule, err := command.StdinPipe()
	if err != nil {
		return Outcome{}, l.spawnProblem(err)
	}
	fromModule, err := command.StdoutPipe()
	if err != nil {
		return Outcome{}, l.spawnProblem(err)
	}
	diagnostics := newBoundedWriter(l.DiagnosticLimit)
	command.Stderr = diagnostics

	if err := command.Start(); err != nil {
		return Outcome{}, l.spawnProblem(err)
	}

	session := Session{Resolved: l.Resolved, Shell: l.Shell, InvocationID: invocationID}
	outcome, sessionErr := l.runSession(ctx, command, toModule, fromModule, session, invocation)

	captured := diagnostics.Diagnostics()
	outcome.Diagnostics = captured
	outcome.InvocationID = invocationID
	if sessionErr != nil {
		return Outcome{Diagnostics: captured, InvocationID: invocationID}, sessionErr
	}
	return outcome, nil
}

// completion is one finished session: its outcome, or the typed problem that
// ended it.
type completion struct {
	outcome Outcome
	err     error
}

// runSession runs the protocol under the invocation deadline and reaps the
// child, so exactly one problem describes what went wrong.
func (l Launcher) runSession(
	ctx context.Context,
	command *exec.Cmd,
	toModule io.WriteCloser,
	fromModule io.ReadCloser,
	session Session,
	invocation Invocation,
) (Outcome, error) {
	// The session runs on its own goroutine so the deadline can act while it
	// is blocked reading. Nothing calls Wait until the session has stopped
	// reading, because Wait closes the pipe it reads from.
	finished := make(chan completion, 1)
	go func() {
		produced, err := session.Run(toModule, fromModule, invocation)
		finished <- completion{outcome: produced, err: err}
	}()

	deadline := time.NewTimer(invocation.timeout())
	defer deadline.Stop()

	var done completion
	select {
	case done = <-finished:
	case <-deadline.C:
		l.terminate(command, toModule, finished)
		return Outcome{}, processProblem("rpc.timed_out",
			fmt.Sprintf("the %q module did not answer within %s", l.namespace(), invocation.timeout()),
			"Retry the command. Report the failure if the module keeps not answering.")
	case <-ctx.Done():
		l.terminate(command, toModule, finished)
		return Outcome{}, processProblem("rpc.cancelled",
			fmt.Sprintf("the %q module was stopped before it answered", l.namespace()), retryRecovery)
	}

	// The protocol is over, one way or another. The module is expected to
	// exit on its own now that its input is closed, but it may instead be
	// blocked writing to a standard output nobody is reading any more, so
	// reaping it is bounded rather than open-ended.
	waitErr := l.reap(command, toModule)

	if done.err != nil {
		return Outcome{}, done.err
	}
	if err := l.checkExit(waitErr); err != nil {
		return Outcome{}, err
	}
	return done.outcome, nil
}

// checkExit treats any unclean exit as a module process failure.
//
// A conforming module exits successfully once it has written its terminal
// message, so a non-zero status means the module and the shell disagree about
// whether the command finished. The proof refuses that rather than reporting a
// result the module may not stand behind.
func (l Launcher) checkExit(waitErr error) error {
	if waitErr == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		return processProblem("rpc.module_exited",
			fmt.Sprintf("the %q module exited with status %d after answering", l.namespace(), exitErr.ExitCode()),
			retryRecovery)
	}
	return processProblem("rpc.module_process_failed",
		fmt.Sprintf("the %q module process could not be completed", l.namespace()), retryRecovery)
}

// terminate ends a module that has stopped answering and reaps it.
//
// Closing the protocol input first gives a module that is merely slow the
// chance to notice and exit cleanly; the kill is the fallback for one that will
// not. Either way the child is gone before Invoke returns, so a hanging module
// cannot outlive the shell invocation that started it.
//
// A module's exit closes its standard output, which ends the session's pending
// read. Waiting on the session is therefore how termination observes the exit,
// and it also guarantees nothing is still reading the pipe that Wait closes.
func (l Launcher) terminate(command *exec.Cmd, toModule io.WriteCloser, finished <-chan completion) {
	_ = toModule.Close()

	grace := time.NewTimer(TerminationGrace)
	defer grace.Stop()
	select {
	case <-finished:
	case <-grace.C:
		// Signalling the whole process group is out of scope for the proof,
		// so a module that spawned children of its own may leave them behind.
		_ = command.Process.Kill()
		<-finished
	}
	_ = l.reap(command, toModule)
}

// reap closes the module's input and waits for it to exit, killing it if it
// does not.
//
// The wait is bounded because a module can outlive the protocol: one that keeps
// writing to a standard output nobody reads any more blocks in the kernel
// forever, and an unbounded Wait would hand the shell's lifetime to it.
//
// It must be called only once the session has stopped reading, because Wait
// closes the pipe the session reads from.
func (l Launcher) reap(command *exec.Cmd, toModule io.WriteCloser) error {
	_ = toModule.Close()

	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()

	grace := time.NewTimer(TerminationGrace)
	defer grace.Stop()
	select {
	case err := <-waited:
		return err
	case <-grace.C:
		_ = command.Process.Kill()
		return <-waited
	}
}

func (l Launcher) spawnProblem(err error) error {
	return processProblem("rpc.module_not_launched",
		fmt.Sprintf("the %q module could not be started: %s", l.namespace(), err),
		reinstallRecovery)
}

func (l Launcher) namespace() string {
	return l.Resolved.Receipt.Namespace
}

// sanitizedEnvironment builds the child environment from nothing.
//
// The shell decides what a module may see rather than filtering what it must
// not: a deny list would leak every variable nobody thought of. Only the
// entries an operating system needs to start a process at all are added back.
func sanitizedEnvironment() []string {
	if runtime.GOOS != "windows" {
		return []string{}
	}
	// Windows cannot reliably start a process without these, and neither
	// carries user or credential data.
	var environment []string
	for _, name := range []string{"SYSTEMROOT", "SYSTEMDRIVE", "WINDIR"} {
		if value, present := os.LookupEnv(name); present {
			environment = append(environment, name+"="+value)
		}
	}
	return environment
}
