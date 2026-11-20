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

package main

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/modules/intg/internal/platform"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// waitDeadline is what the commands that can wait declare to the shell, in
// place of its 30-second default. It is the shell's ceiling.
const waitDeadline = 30 * time.Minute

// defaultWaitTimeout and maxWaitTimeout bound --wait-timeout. The module stops
// waiting on its own, comfortably inside waitDeadline, so a wait that runs out
// ends with a problem saying where things stand rather than with the shell
// stopping the module mid-wait.
const (
	defaultWaitTimeout = 15 * time.Minute
	maxWaitTimeout     = 25 * time.Minute
)

// pollInterval is how often a wait asks the platform again. Tests shorten it.
var pollInterval = 5 * time.Second

// errWaitTimeout is a wait that ran out before what it waited for happened.
var errWaitTimeout = errors.New("the wait ran out")

// waitFlags are the --wait and --wait-timeout of a command that can wait.
type waitFlags struct {
	wait    bool
	timeout time.Duration
}

func (f *waitFlags) bind(command *cobra.Command, what string) {
	command.Flags().BoolVar(&f.wait, "wait", false, "Wait until "+what+", and report how it ended.")
	command.Flags().DurationVar(&f.timeout, "wait-timeout", defaultWaitTimeout,
		"How long --wait waits, at most 25m. Defaults to 15m.")
}

// validate refuses a --wait-timeout the module could not honour inside the
// deadline it declares.
func (f *waitFlags) validate() error {
	if !f.wait {
		return nil
	}
	if f.timeout <= 0 || f.timeout > maxWaitTimeout {
		return usageProblem("--wait-timeout "+f.timeout.String()+" is outside what --wait can honour",
			"Give --wait-timeout a duration up to 25m, or leave it out for 15m.")
	}
	return nil
}

// poll asks check every pollInterval until it reports done or an error, or
// until limit has passed, which is errWaitTimeout.
func poll(ctx context.Context, limit time.Duration, check func() (bool, error)) error {
	deadline := time.Now().Add(limit)
	for {
		done, err := check()
		if err != nil || done {
			return err
		}
		if time.Now().Add(pollInterval).After(deadline) {
			return errWaitTimeout
		}
		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// buildInProgress reports whether a build status is one it can still leave.
// Every other status, including the completion reasons the API passes
// through, is final.
func buildInProgress(status string) bool {
	return status == "" || status == "Pending" || status == "Running"
}

// restartGrace is how long a redeploy of a running release waits to see its
// rollout begin. Tests shorten it.
var restartGrace = time.Minute

// awaited is the release a wait is for, and the statuses that end it early.
type awaited struct {
	// release is the one release to wait for; empty accepts any.
	release string
	// notRelease is a release that does not count, the one an environment ran
	// before the command; empty excludes none.
	notRelease string
	// failed are the statuses that end the wait as intg.deployment_failed.
	failed []string
}

func (w awaited) matches(found deployment) bool {
	return found.deployed() &&
		(w.release == "" || found.Release == w.release) &&
		(w.notRelease == "" || found.Release != w.notRelease)
}

// waitForRollout polls what integrationID runs in environmentID until the
// release a deploy or promotion bound is active, and returns it. A rollout
// that ends in ERROR or SUSPENDED is intg.deployment_failed.
//
// The API answers with the bound release's name, which every release of an
// integration starts with "<integration>-". An answer that is not one (the
// API's form for an agent) is not waited for by name: any release other than
// previous, the one the environment ran before, is.
func waitForRollout(ctx context.Context, client platform.Client, integrationID, environmentID, release,
	previous string, limit time.Duration, endpoint string) (deployment, error) {
	want := awaited{release: release, failed: []string{"ERROR", "SUSPENDED"}}
	if !strings.HasPrefix(release, integrationID+"-") {
		want = awaited{notRelease: previous, failed: want.failed}
	}
	return waitUntilActive(ctx, client, integrationID, environmentID, want, limit, endpoint)
}

// waitForRestart polls what integrationID runs in environmentID until it is
// active after a redeploy, and reports whether the restart was seen.
//
// A stopped release reads SUSPENDED until it starts again, so only ERROR ends
// the wait early. A running one reads ACTIVE before the restart as well as
// after it, so ACTIVE counts only once the rollout has been seen to begin; one
// not seen within restartGrace is reported as unseen rather than waited out.
func waitForRestart(ctx context.Context, client platform.Client, integrationID, environmentID string,
	wasActive bool, limit time.Duration, endpoint string) (deployment, bool, error) {
	want := awaited{failed: []string{"ERROR"}}
	if !wasActive {
		running, err := waitUntilActive(ctx, client, integrationID, environmentID, want, limit, endpoint)
		return running, true, err
	}
	started := time.Now()
	var current deployment
	err := poll(ctx, min(restartGrace, limit), func() (bool, error) {
		found, err := currentDeployment(ctx, client, integrationID, environmentID, endpoint)
		if err != nil {
			return false, err
		}
		current = found
		if found.Status == "ERROR" {
			return false, rolloutFailed(integrationID, environmentID, found)
		}
		return found.Status != "ACTIVE", nil
	})
	if errors.Is(err, errWaitTimeout) {
		return current, false, nil
	}
	if err != nil {
		return current, false, err
	}
	running, err := waitUntilActive(ctx, client, integrationID, environmentID, want, limit-time.Since(started),
		endpoint)
	return running, true, err
}

// waitUntilActive polls until a release want matches is ACTIVE, failing as
// intg.deployment_failed on any of want's failed statuses.
func waitUntilActive(ctx context.Context, client platform.Client, integrationID, environmentID string,
	want awaited, limit time.Duration, endpoint string) (deployment, error) {
	var current deployment
	err := poll(ctx, limit, func() (bool, error) {
		found, err := currentDeployment(ctx, client, integrationID, environmentID, endpoint)
		if err != nil {
			return false, err
		}
		current = found
		if !want.matches(found) {
			return false, nil
		}
		switch {
		case found.Status == "ACTIVE":
			return true, nil
		case slices.Contains(want.failed, found.Status):
			return false, rolloutFailed(integrationID, environmentID, found)
		default:
			return false, nil
		}
	})
	if errors.Is(err, errWaitTimeout) {
		return current, problem.New(problem.CategoryProductService, "intg.wait_timeout",
			integrationID+" was not running in "+environmentID+" within "+limit.String()).
			WithRecovery("Run wso2 intg integration deployment show " + integrationID + " --env " + environmentID +
				" to see where it stands.")
	}
	return current, err
}

// rolloutFailed is a release that ended in a status it cannot leave by itself.
func rolloutFailed(integrationID, environmentID string, found deployment) error {
	return problem.New(problem.CategoryProductService, "intg.deployment_failed",
		"the release "+found.Release+" of "+integrationID+" is "+found.Status+" in "+environmentID).
		WithRecovery("Run wso2 intg integration logs " + integrationID + " --env " + environmentID + " to see why.")
}
