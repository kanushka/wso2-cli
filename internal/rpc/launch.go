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
	// Broker answers the module's access requests. A launcher without one
	// denies access rather than granting it.
	Broker Broker
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
		generated, err := NewInvocationID()
		if err != nil {
			return Outcome{}, processProblem("rpc.invocation_id_unavailable",
				"the shell cannot generate an invocation identifier", retryRecovery)
		}
		invocationID = generated
	}

	command := exec.Command(l.Resolved.ExecutablePath)
	// The module inherits nothing from the shell's environment, including the
	// credential source the broker reads. Access reaches a module only as a
	// short-lived token inside the protocol, so there is no ambient value for
	// it to find, log, or pass on.
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

	session := Session{
		Resolved:     l.Resolved,
		Shell:        l.Shell,
		InvocationID: invocationID,
		Broker:       l.Broker,
	}
	outcome, sessionErr := l.runSession(ctx, command, toModule, fromModule, session, invocation)

	captured := diagnostics.Diagnostics()
	outcome.Diagnostics = captured
	outcome.InvocationID = invocationID
	if sessionErr != nil {
		return Outcome{Diagnostics: captured, InvocationID: invocationID}, sessionErr
	}
	return outcome, nil
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
	// is blocked reading. Its result is published by closing ended, which every
	// path below can wait on more than once.
	var produced Outcome
	var sessionErr error
	ended := make(chan struct{})
	go func() {
		produced, sessionErr = session.Run(toModule, fromModule, invocation)
		close(ended)
	}()

	deadline := time.NewTimer(invocation.timeout())
	defer deadline.Stop()

	var ending error
	select {
	case <-ended:
	case <-deadline.C:
		ending = processProblem("rpc.timed_out",
			fmt.Sprintf("the %q module did not answer within %s", l.namespace(), invocation.timeout()),
			"Retry the command. Report the failure if the module keeps not answering.")
	case <-ctx.Done():
		ending = processProblem("rpc.cancelled",
			fmt.Sprintf("the %q module was stopped before it answered", l.namespace()), retryRecovery)
	}

	// The module is finished with either way, so it is stopped before this
	// returns and its exit status is available to classify.
	waitErr := l.stop(command, toModule, ended)

	switch {
	case ending != nil:
		return Outcome{}, ending
	case sessionErr != nil:
		return Outcome{}, sessionErr
	}
	if err := l.checkExit(waitErr); err != nil {
		return Outcome{}, err
	}
	return produced, nil
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

// stop ends the exchange and the module process, and reports how it exited.
//
// It closes the module's protocol input, which tells a module that is merely
// slow that the exchange is over and lets it exit cleanly. Every wait after
// that is bounded by TerminationGrace and backed by a kill, because a module
// can outlive the protocol in two ways: it may never exit, and it may block
// forever writing to a standard output nobody reads any more. Either way the
// child is gone before Invoke returns.
//
// The wait on ended comes first because Wait closes the pipe the session reads
// from, so nothing may still be reading it.
func (l Launcher) stop(command *exec.Cmd, toModule io.WriteCloser, ended <-chan struct{}) error {
	_ = toModule.Close()

	if !waitOrKill(command, ended) {
		// Signalling the whole process group is out of scope for the proof,
		// so a module that spawned children of its own may leave them behind.
		<-ended
	}

	exited := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = command.Wait()
		close(exited)
	}()
	waitOrKill(command, exited)
	<-exited
	return waitErr
}

// waitOrKill waits for a signal until the termination grace expires, killing
// the module if it has not arrived. It reports whether the signal arrived
// before the kill.
func waitOrKill(command *exec.Cmd, signal <-chan struct{}) bool {
	grace := time.NewTimer(TerminationGrace)
	defer grace.Stop()
	select {
	case <-signal:
		return true
	case <-grace.C:
		_ = command.Process.Kill()
		return false
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
