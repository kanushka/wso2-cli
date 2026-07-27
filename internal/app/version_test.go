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
	"bytes"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/modules/fixture"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/internal/state"
	"github.com/wso2/wso2-cli/internal/version"
)

func TestVersionReportsShellProtocolAndPlatformWithoutAnyInstalledModule(t *testing.T) {
	shell, out, err := newShell(t)

	if code := shell.Run([]string{"version"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, err)
	}

	stdout := out.String()
	for _, want := range []string{
		"WSO2 CLI",
		"v" + version.Shell(),
		"Protocol",
		version.ProtocolDisplay(),
		version.Platform(),
		"Installed modules",
		"No modules are installed.",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout does not contain %q:\n%s", want, stdout)
		}
	}
	if err.Len() != 0 {
		t.Errorf("stderr = %q, want no diagnostics", err.String())
	}
}

func TestVersionReportsTheInstalledReferenceModuleFromItsReceipt(t *testing.T) {
	shell, out, errOut := newShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})

	if code := shell.Run([]string{"version"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}

	stdout := out.String()
	if !strings.Contains(stdout, "reference") || !strings.Contains(stdout, "v0.1.0") {
		t.Fatalf("stdout does not report the installed reference module:\n%s", stdout)
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr = %q, want no diagnostics", errOut.String())
	}
}

func TestVersionDoesNotLaunchTheModule(t *testing.T) {
	shell, out, errOut := newShell(t)
	// The installed "executable" would fail loudly if the shell ever ran it:
	// it is not a valid program on any supported platform.
	installFixture(t, shell, fixture.Module{
		Namespace: "reference",
		Version:   "0.1.0",
		Contents:  []byte("this file is not an executable program\n"),
	})

	if code := shell.Run([]string{"version"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	if !strings.Contains(out.String(), "v0.1.0") {
		t.Fatalf("stdout does not report the module version from its receipt:\n%s", out)
	}
}

func TestVersionStillReportsAfterABrokenInstallationAndWarnsOnStandardError(t *testing.T) {
	shell, out, errOut := newShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})
	installFixture(t, shell, fixture.Module{Namespace: "broken", Version: "0.1.0", Inactive: true})

	if code := shell.Run([]string{"version"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}

	if !strings.Contains(out.String(), "reference") {
		t.Fatalf("stdout lost the healthy module:\n%s", out)
	}
	if strings.Contains(out.String(), "broken") {
		t.Fatalf("stdout reported an unusable module as installed:\n%s", out)
	}
	if !strings.Contains(errOut.String(), "modules.no_active_version") {
		t.Fatalf("stderr does not diagnose the broken installation:\n%s", errOut)
	}
}

func TestVersionRejectsUnexpectedArguments(t *testing.T) {
	shell, _, errOut := newShell(t)

	if code := shell.Run([]string{"version", "extra"}); code != exit.Usage {
		t.Fatalf("exit code = %d, want the usage class %d", code, exit.Usage)
	}
	if !strings.Contains(errOut.String(), "shell.unexpected_argument") {
		t.Fatalf("stderr does not name the usage problem:\n%s", errOut)
	}
}

func newShell(t *testing.T) (app.Shell, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	return app.Shell{
		StateRoot: t.TempDir(),
		Streams:   output.Streams{Out: out, Err: errOut},
	}, out, errOut
}

func installFixture(t *testing.T, shell app.Shell, module fixture.Module) {
	t.Helper()
	if _, err := fixture.Install(storeRoot(shell), module); err != nil {
		t.Fatalf("fixture.Install returned %v", err)
	}
}

// storeRoot reports the managed module store inside a test shell's isolated
// state root.
func storeRoot(shell app.Shell) string {
	return state.ModuleStore(shell.StateRoot)
}
