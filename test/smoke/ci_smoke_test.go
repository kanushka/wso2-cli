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

package smoke_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/test/smoke"
)

// TestCISmoke brokers one acquisition the way a CI job does: inline, from a
// client secret already on the machine, with no login and no browser.
//
// It is the first live coverage of that path against any deployment. Until now
// the non-interactive source was proven only against the in-process fake
// issuer, on every product — so the guarantee it makes to a module rested on a
// fixture rather than on a deployment. The guarantee is the same one the
// browser path makes, and the reason it is worth proving live is that a module
// cannot tell which kind of context invoked it and must not need to.
//
// It needs no browser and no human, so unlike TestLoginSmoke it can run
// unattended. What it does need is a confidential client and its secret, and
// the secret comes from the environment rather than from any file — see
// RUNNING.md.
func TestCISmoke(t *testing.T) {
	config := requireDeployment(t)
	if config.CIClientID == "" {
		t.Skipf("no confidential client is configured: set %s (see test/smoke/RUNNING.md)",
			smoke.CIClientIDVar)
	}
	if strings.TrimSpace(os.Getenv(smoke.SecretVariable)) == "" {
		t.Skipf("no client secret is exported: set %s in this shell "+
			"(see test/smoke/RUNNING.md; it deliberately belongs in no file)",
			smoke.SecretVariable)
	}

	selection, err := config.CIDocument().Select("")
	if err != nil {
		t.Fatalf("the non-interactive document selects no context: %v", err)
	}
	broker := &auth.Broker{
		Namespace:    smoke.Namespace,
		Capabilities: config.Capabilities(),
		Selection:    selection,
		InvocationID: "smoke-ci",
		StateRoot:    t.TempDir(),
	}

	target, err := config.NarrowTarget()
	if err != nil {
		t.Skipf("%v", err)
	}
	// One permission out of the configured set, so the run proves narrowing
	// rather than proving that asking for everything returns everything. #35
	// fixed exactly that defect in the browser run; this one is written not to
	// have it.
	request := auth.Request{Audience: config.Audience, Scopes: []string{target}}

	grant, err := broker.Acquire(request)
	if err != nil {
		t.Fatalf("the broker refused a CI acquisition: %v", err)
	}
	if grant.Token == "" {
		t.Fatal("the broker granted an empty token")
	}
	if !grant.ExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("the grant expires at %s, which is not in the future", grant.ExpiresAt)
	}

	// The broker proved the narrowing before returning: it verifies that the
	// issued token carries exactly the permissions asked for and is bound to
	// the audience asked for, and refuses when it cannot. Reaching here is the
	// assertion, and decoding the token again would only restate it somewhere
	// that could drift from the check that matters.
	t.Logf("CI acquisition granted for %q against %q, expiring %s",
		target, config.Audience, grant.ExpiresAt.Format(time.RFC3339))
}
