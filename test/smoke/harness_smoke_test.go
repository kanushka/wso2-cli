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

//go:build smoke

// Shared ground for the live runs: reading the configured deployment, keeping
// the machine's secure store clean, and naming the typed refusals a run reads.

package smoke_test

import (
	"errors"
	"os"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/test/smoke"
)

// The refusal codes a live run reads.
//
// The shell states its codes as literals at each refusal site rather than
// exporting them, so a test that wants to name one restates it here. A rename
// the shell made and this file did not follow shows up as a run reporting an
// outcome it did not expect, which is the failure mode worth having.
const (
	codeNarrowingUnavailable = "auth.narrowing_unavailable"
	codeDiscoveryFailed      = "auth.discovery_failed"
)

// requireDeployment reads the configured deployment.
//
// An absent configuration skips: there is nothing to run against, and saying so
// is the honest result. A configuration that cannot describe a deployment
// fails, because someone believed they had set one up and a skip would tell
// them they had.
func requireDeployment(t *testing.T) smoke.Config {
	t.Helper()
	config, err := smoke.Load(os.LookupEnv)
	if errors.Is(err, smoke.ErrNotConfigured) {
		t.Skip(err.Error())
	}
	if err != nil {
		t.Fatalf("the configured deployment cannot be read: %v", err)
	}
	return config
}

// forgetSmokeSession removes the run's secure-store entry before and after the
// run, so no run reads what an earlier one left and none leaves a credential
// behind on the machine.
//
// It only ever names smoke.CredentialRef, which is not a reference a human
// would choose for a real context, so this can never delete a real session.
func forgetSmokeSession(t *testing.T) {
	t.Helper()
	forget := func() {
		err := keyring.Delete(session.Service, smoke.CredentialRef)
		if err != nil && !errors.Is(err, keyring.ErrNotFound) {
			t.Logf("could not remove the smoke session %q from the secure store: %v",
				smoke.CredentialRef, err)
		}
	}
	forget()
	t.Cleanup(forget)
}

// refusalCode names the typed code behind err, whether the broker reported it
// as a denial or a lower layer reported the problem directly.
//
// auth.Denial carries its problem in a field rather than wrapping it, so the
// two cases have to be read separately.
func refusalCode(err error) string {
	var denial auth.Denial
	if errors.As(err, &denial) {
		return denial.Problem.Code
	}
	var reported problem.Problem
	if errors.As(err, &reported) {
		return reported.Code
	}
	return ""
}

// refusalMessage reads the human-readable half of a typed refusal.
func refusalMessage(err error) string {
	var denial auth.Denial
	if errors.As(err, &denial) {
		return denial.Problem.Message
	}
	var reported problem.Problem
	if errors.As(err, &reported) {
		return reported.Message
	}
	if err == nil {
		return ""
	}
	return err.Error()
}
