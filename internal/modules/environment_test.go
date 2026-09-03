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

	for _, entry := range SanitizedEnvironment() {
		if entry == CAFileEnvVar+"=" || slices.Contains([]string{"WSO2_SOME_SECRET=not-for-modules", "WSO2_HOME=/nowhere"}, entry) {
			t.Errorf("a module was handed %q", entry)
		}
	}

	t.Setenv(CAFileEnvVar, "/certs/deployment.pem")
	environment := SanitizedEnvironment()
	if !slices.Contains(environment, CAFileEnvVar+"=/certs/deployment.pem") {
		t.Errorf("the certificate file was withheld from the module: %q", environment)
	}
	for _, entry := range environment {
		if entry == "WSO2_SOME_SECRET=not-for-modules" || entry == "WSO2_HOME=/nowhere" {
			t.Errorf("a module was handed %q", entry)
		}
	}
}
