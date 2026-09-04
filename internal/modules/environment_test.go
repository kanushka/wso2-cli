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

package modules

import (
	"slices"
	"strings"
	"testing"
)

// TestAModuleSeesOnlyTheCertificateFile pins the environment a module process
// is launched with: nothing from the shell's own environment except the one
// variable that names public trust material. A credential a CI runner exports
// must never reach a binary that was downloaded moments ago.
func TestAModuleSeesOnlyTheCertificateFile(t *testing.T) {
	t.Setenv("WSO2_SOME_SECRET", "not-for-modules")
	t.Setenv("WSO2_HOME", "/nowhere")
	t.Setenv(CAFileEnvVar, "")

	for _, entry := range SanitizedEnvironment("reference") {
		if entry == CAFileEnvVar+"=" || slices.Contains([]string{"WSO2_SOME_SECRET=not-for-modules", "WSO2_HOME=/nowhere"}, entry) {
			t.Errorf("a module was handed %q", entry)
		}
	}

	t.Setenv(CAFileEnvVar, "/certs/deployment.pem")
	environment := SanitizedEnvironment("reference")
	if !slices.Contains(environment, CAFileEnvVar+"=/certs/deployment.pem") {
		t.Errorf("the certificate file was withheld from the module: %q", environment)
	}
	for _, entry := range environment {
		if entry == "WSO2_SOME_SECRET=not-for-modules" || entry == "WSO2_HOME=/nowhere" {
			t.Errorf("a module was handed %q", entry)
		}
	}
}

// TestAModuleSeesItsOwnNamespaceVariablesOnly pins the one deliberate leak: a
// secret a user exports under the module's own prefix reaches that module and
// no other, so a bootstrap can be handed an administrator password.
func TestAModuleSeesItsOwnNamespaceVariablesOnly(t *testing.T) {
	t.Setenv("WSO2_IAM_ADMIN_PASSWORD", "secret-for-iam")
	t.Setenv("WSO2_APIM_ADMIN_PASSWORD", "secret-for-apim")
	t.Setenv("WSO2_IAM_EMPTY", "")
	t.Setenv("WSO2_HOME", "/nowhere")

	environment := SanitizedEnvironment("iam")
	if !slices.Contains(environment, "WSO2_IAM_ADMIN_PASSWORD=secret-for-iam") {
		t.Errorf("the module's own variable was withheld: %q", environment)
	}
	for _, entry := range environment {
		if strings.HasPrefix(entry, "WSO2_APIM_") || strings.HasPrefix(entry, "WSO2_HOME") ||
			entry == "WSO2_IAM_EMPTY=" {
			t.Errorf("a module was handed %q", entry)
		}
	}
}
