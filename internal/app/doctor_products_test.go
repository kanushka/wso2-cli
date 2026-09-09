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

package app_test

import (
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
)

func TestDoctorReportsNoneNamingTheProductsWithoutASession(t *testing.T) {
	keyring.MockInit()
	shell, out, _ := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	installLogin(t, shell, thunderDoc("http://login.example", "http://apim.example"))
	store := session.Store{StateRoot: shell.StateRoot}
	if err := store.Save(credentialRef, session.Session{Issuer: "http://login.example", RefreshToken: "rt"}); err != nil {
		t.Fatal(err)
	}
	// Being logged out of a product is the state wso2 logout leaves behind,
	// so the check names the products without a session and still exits 0.
	code := shell.Run([]string{"doctor", "--output", "json"})
	if code != exit.OK {
		t.Fatalf("exit %d, want %d for products that are merely logged out", code, exit.OK)
	}
	finding := decodeDoctorReport(t, out.Bytes()).findingFor(t, "session")
	if finding.Status != "none" || !strings.Contains(finding.Detail, "apim") || !strings.Contains(finding.Detail, "iam") ||
		!strings.Contains(finding.Recovery, "wso2 login --only") {
		t.Fatalf("finding %+v", finding)
	}
}

func TestDoctorTreatsAClientCredentialsIdentityAsHealthyWithoutASession(t *testing.T) {
	keyring.MockInit()
	shell, out, errOut := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	installLogin(t, shell, identityDoc(contexts.KindClientCredentials)("http://login.example"))
	if code := shell.Run([]string{"doctor", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if finding := decodeDoctorReport(t, out.Bytes()).findingFor(t, "session"); finding.Status != "not-applicable" {
		t.Fatalf("finding %+v", finding)
	}
}
