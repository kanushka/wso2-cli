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

package auth_test

import (
	"testing"

	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
)

// A session records what it was authorized for, and is presented only for a
// record that still asks for exactly that (ADR 0016). These pin the resource
// half of the binding; the issuer, client and scope halves predate it.

func TestASessionBoundToAnotherResourceIsNotPresented(t *testing.T) {
	deployment, broker := thunderBroker(t, fakeissuer.Options{}, []string{readScope})
	store := session.Store{StateRoot: deployment.stateRoot}
	stored := deployment.storedSession(t)
	stored.Resource = "https://another.example/resource"
	if err := store.Save(sessionRef, stored); err != nil {
		t.Fatal(err)
	}
	refusal := denied(t, broker, declaredRequest())
	if refusal.Problem.Code != "auth.login_required" {
		t.Fatalf("code = %q, want auth.login_required", refusal.Problem.Code)
	}
}

func TestASessionStoredBeforeBindingsWereRecordedIsNotPresentedForAResource(t *testing.T) {
	deployment, broker := thunderBroker(t, fakeissuer.Options{}, []string{readScope})
	store := session.Store{StateRoot: deployment.stateRoot}
	stored := deployment.storedSession(t)
	stored.Bound, stored.Resource = false, ""
	if err := store.Save(sessionRef, stored); err != nil {
		t.Fatal(err)
	}
	refusal := denied(t, broker, declaredRequest())
	if refusal.Problem.Code != "auth.login_required" || !containsText(refusal.Problem.Message, "earlier WSO2 CLI") {
		t.Fatalf("refusal = %+v, want auth.login_required naming the earlier CLI", refusal.Problem)
	}
}

func TestASessionStoredBeforeBindingsWereRecordedStillServesARecordAskingNoResource(t *testing.T) {
	// A scope-bound deployment asks for no resource, so an entry written
	// before the binding existed says nothing that could contradict it.
	deployment := seedBrowserSession(t, fakeissuer.Options{})
	if deployment.storedSession(t).Bound {
		t.Fatal("the fixture is meant to model an entry written before bindings were recorded")
	}
	if _, err := deployment.broker(t).Acquire(declaredRequest()); err != nil {
		t.Fatalf("acquire: %v", err)
	}
}
