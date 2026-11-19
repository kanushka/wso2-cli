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
	"path/filepath"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
)

// writeCloudEnvironment also points the catalog at a server that has
// nothing, since a first Cloud login installs the Cloud products it can.
func writeCloudEnvironment(t *testing.T, root, env, body string) {
	t.Helper()
	catalogServing(t, emptyCatalog)
	dir := filepath.Join(root, "cloud")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, env+".yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func cloudEnvironmentBody(issuer string) string {
	return "issuer: " + issuer + "\nbaseDomain: dev.cloud.example\nenvironment: development\n"
}

func TestCloudEnvironmentLoginWritesSelectsAndSignsIn(t *testing.T) {
	shell, out, errOut, issuer := newCreatingLogin(t)
	t.Setenv(app.CloudEnvVar, "dev")
	writeCloudEnvironment(t, shell.StateRoot, "dev", cloudEnvironmentBody(issuer.URL))
	shell.Reader = failIfReadReader{t}

	if code := shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	document := loadDocument(t, shell)
	if len(document.Contexts) != 1 || document.DefaultContext != "cloud-dev" {
		t.Fatalf("document = %+v, want cloud-dev alone and selected", document)
	}
	written := document.Contexts[0]
	if written.Name != "cloud-dev" || written.Login.Issuer != issuer.URL || written.Login.ClientID != "WSO2_CLI" ||
		written.Login.Provider != contexts.ProviderThunder || written.Type != contexts.TypeCloud {
		t.Errorf("context = %+v, want a WSO2 Cloud context at the environment's issuer", written)
	}
	if strings.Contains(errOut.String(), "Sign in with:") {
		t.Errorf("a Cloud login asked for a provider:\n%s", errOut)
	}
	if !strings.Contains(out.String(), "cloud-dev") {
		t.Errorf("stdout does not report the login:\n%s", out)
	}

	if err := os.RemoveAll(filepath.Join(shell.StateRoot, "cloud")); err != nil {
		t.Fatal(err)
	}
	if code := shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("a second login needed the environment file again: exit %d, stderr %s", code, errOut)
	}
}

func TestCloudEnvironmentLoginSelectsItsContextBesideOthers(t *testing.T) {
	shell, _, errOut, issuer := newCreatingLogin(t)
	if code := shell.Run([]string{"login", "--url", issuer.URL, "--client-id", "wso2-cli", "--context", "local"}); code != exit.OK {
		t.Fatalf("first login failed: exit %d, stderr %s", code, errOut)
	}
	t.Setenv(app.CloudEnvVar, "stage")
	writeCloudEnvironment(t, shell.StateRoot, "stage", cloudEnvironmentBody(issuer.URL)+"clientId: STAGE_CLI\n")
	shell.Reader = failIfReadReader{t}

	if code := shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("cloud login failed: exit %d, stderr %s", code, errOut)
	}
	document := loadDocument(t, shell)
	stage, found := document.Find("cloud-stage")
	if !found || document.DefaultContext != "cloud-stage" || len(document.Contexts) != 2 {
		t.Fatalf("document = %+v, want cloud-stage added and selected beside local", document)
	}
	if stage.Login.ClientID != "STAGE_CLI" {
		t.Errorf("client id = %q, want the environment file's", stage.Login.ClientID)
	}
}

func TestCloudEnvironmentNamesItsContext(t *testing.T) {
	for env, want := range map[string]string{
		"dev": "cloud-dev", "stage": "cloud-stage", "prod": "cloud-prod-us",
		"prod-us": "cloud-prod-us", "prod-eu": "cloud-prod-eu",
	} {
		t.Run(env, func(t *testing.T) {
			shell, _, errOut, issuer := newCreatingLogin(t)
			t.Setenv(app.CloudEnvVar, env)
			writeCloudEnvironment(t, shell.StateRoot, strings.TrimPrefix(want, "cloud-"), cloudEnvironmentBody(issuer.URL))
			shell.Reader = failIfReadReader{t}
			if code := shell.Run([]string{"login"}); code != exit.OK {
				t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
			}
			if got := loadDocument(t, shell).DefaultContext; got != want {
				t.Errorf("selected context = %q, want %q", got, want)
			}
		})
	}
}

func TestCloudEnvironmentLoginRefusals(t *testing.T) {
	for _, testCase := range []struct {
		name, env, file, code string
		status                exit.Code
	}{
		{"no file", "stage", "", "shell.cloud_environment_unknown", exit.NotFound},
		{"bad name", "Dev Env", "", "shell.invalid_argument", exit.Usage},
		{"no issuer", "dev", "baseDomain: dev.cloud.example\nenvironment: development\n", "shell.input_malformed", exit.Usage},
		{"no base domain", "dev", "issuer: https://idp.example\nenvironment: development\n", "shell.input_malformed", exit.Usage},
		{"no environment", "dev", "issuer: https://idp.example\nbaseDomain: dev.cloud.example\n", "shell.input_malformed", exit.Usage},
		{"unknown member", "dev", "issuer: https://idp.example\nbaseDomain: d.example\nenvironment: development\nsecret: x\n",
			"shell.input_malformed", exit.Usage},
		{"a context file", "dev", "contexts:\n  - name: a\n    login: {issuer: https://a.example, clientId: x}\n",
			"shell.input_malformed", exit.Usage},
		{"base domain with a path", "dev", "issuer: https://idp.example\nbaseDomain: d.example/x\nenvironment: development\n",
			"shell.input_malformed", exit.Usage},
		{"environment with a dot", "dev", "issuer: https://idp.example\nbaseDomain: d.example\nenvironment: a.b\n",
			"shell.input_malformed", exit.Usage},
		{"issuer not a URL", "dev", "issuer: idp.example\nbaseDomain: d.example\nenvironment: development\n",
			"shell.invalid_argument", exit.Usage},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			shell, _, errOut, _ := newCreatingLogin(t)
			t.Setenv(app.CloudEnvVar, testCase.env)
			if testCase.file != "" {
				writeCloudEnvironment(t, shell.StateRoot, testCase.env, testCase.file)
			}
			shell.Reader = failIfReadReader{t}
			if code := shell.Run([]string{"login"}); code != testCase.status {
				t.Fatalf("exit %d, want %d; stderr %s", code, testCase.status, errOut)
			}
			requireRefusal(t, errOut.String(), testCase.code)
			if got := len(loadDocument(t, shell).Contexts); got != 0 {
				t.Errorf("a refused Cloud login wrote %d contexts", got)
			}
		})
	}
}

func TestCloudEnvironmentYieldsToAnExplicitTarget(t *testing.T) {
	shell, _, errOut, issuer := newCreatingLogin(t)
	t.Setenv(app.CloudEnvVar, "dev")
	shell.Reader = failIfReadReader{t}

	if code := shell.Run([]string{"login", "--url", issuer.URL, "--client-id", "wso2-cli", "--context", "local"}); code != exit.OK {
		t.Fatalf("login --url failed under %s: exit %d, stderr %s", app.CloudEnvVar, code, errOut)
	}
	if code := shell.Run([]string{"login", "--context", "local"}); code != exit.OK {
		t.Fatalf("login --context failed under %s: exit %d, stderr %s", app.CloudEnvVar, code, errOut)
	}
	if got := loadDocument(t, shell).DefaultContext; got != "local" {
		t.Errorf("selected context = %q, want local", got)
	}
}
