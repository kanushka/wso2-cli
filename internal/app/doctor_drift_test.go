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
	"os"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/modules/fixture"
)

func TestDoctorDoesNotFlagAScopeListStatedEmptyOnPurpose(t *testing.T) {
	shell, _, _ := newContextShell(t)
	localSetup(t, shell)
	document := loadDocument(t, shell)
	local := contextNamed(t, document, "local")
	iam := local.Products["iam"]
	iam.Scopes = []string{}
	local.Products["iam"] = iam
	if err := contexts.Save(shell.StateRoot, document.Put(local)); err != nil {
		t.Fatal(err)
	}
	if saved := contextNamed(t, loadDocument(t, shell), "local").Products["iam"].Scopes; saved == nil {
		t.Fatal("the document lost the stated empty scope list on the way to disk")
	}

	_, out, _ := run(t, shell, "doctor", "--output", "json")
	if finding := decodeDoctorReport(t, []byte(out)).findingFor(t, "defaults"); finding.Status != "pass" {
		t.Errorf("defaults = %+v, want pass for scopes stated empty on purpose", finding)
	}
}

func TestDoctorDoesNotFlagACloudProductForTheGrantCloudSetsAside(t *testing.T) {
	login := newCloudLogin(t)
	installCloudAPIM(t, login.shell)
	installFixture(t, login.shell, fixture.Module{Namespace: "intg", Version: "0.1.0",
		Product: &modules.ProductDescriptor{
			Audience: modules.AudienceResource, Grant: contexts.GrantExchange, Scopes: []string{"cloud:project:read"},
		}})
	login.run(t, "login")
	if grant := cloudContext(t, login.shell).Products["intg"].Grant; grant != nil {
		t.Fatalf("intg grant = %+v, want none on Cloud", grant)
	}

	login.out.Reset()
	login.shell.Run([]string{"doctor", "--output", "json"})
	if finding := decodeDoctorReport(t, login.out.Bytes()).findingFor(t, "defaults"); finding.Status != "pass" {
		t.Errorf("defaults = %+v, want pass: a Cloud product is never reached by exchange", finding)
	}
}

func TestDoctorPutsTheDriftCommandInTheRecoveryUnderTheInvokedName(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	shell, _, _ := newContextShell(t)
	localSetup(t, shell)
	document := loadDocument(t, shell)
	local := contextNamed(t, document, "local")
	iam := local.Products["iam"]
	iam.Scopes = []string{"legacy"}
	local.Products["iam"] = iam
	if err := contexts.Save(shell.StateRoot, document.Put(local)); err != nil {
		t.Fatal(err)
	}
	shell.Name = "ws"

	_, out, _ := run(t, shell, "doctor")
	if strings.Contains(out, "wso2 ") {
		t.Errorf("doctor still names wso2:\n%s", out)
	}
	if !strings.Contains(out, "ws context product add iam") {
		t.Errorf("doctor does not name the command that adopts the new value:\n%s", out)
	}
	_, out, _ = run(t, shell, "doctor", "--output", "json")
	finding := decodeDoctorReport(t, []byte(out)).findingFor(t, "defaults")
	if finding.Status != "differs" || strings.Contains(finding.Detail, "context product add") ||
		!strings.Contains(finding.Recovery, "ws context product add iam") {
		t.Errorf("defaults = %+v, want the fact in the detail and the command in the recovery", finding)
	}

	data, err := os.ReadFile(contexts.Path(shell.StateRoot))
	if err != nil || strings.Contains(string(data), "scopes: null") {
		t.Errorf("the document writes an absent scope list as null:\n%s", data)
	}
}
