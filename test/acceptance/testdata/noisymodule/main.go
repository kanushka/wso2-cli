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

// Command noisymodule is a conforming module that misbehaves on purpose, used
// by the shell's acceptance tests.
//
// The reference module has nothing to warn about and never fails, so it cannot
// demonstrate that structured output survives a talkative module or that the
// shell reports a module which answers and then exits uncleanly. This one
// writes to standard error before and after returning its result, and claims
// the same namespace and version, so the shell resolves and launches it exactly
// as it would the reference module.
//
// It is steered by files placed beside its own executable rather than by
// arguments or environment variables, because the shell launches a module with
// neither: it passes no arguments and sanitizes the environment to nothing.
// Writing a control file also leaves the executable unchanged, so the receipt
// digest still matches and the module is still launched.
//
// The recognized files are:
//
//	exit-uncleanly      exit with a non-zero status after answering
//	report-environment  write this process's whole environment to standard
//	                    error, so a test can prove what the shell passed on
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/result"
)

// UncleanExitMarker names the control file that makes this module exit with a
// non-zero status once it has answered.
const UncleanExitMarker = "exit-uncleanly"

// uncleanExitCode is the status reported when that file is present.
const uncleanExitCode = 3

// EnvironmentMarker names the control file that makes this module report the
// environment it was launched with.
const EnvironmentMarker = "report-environment"

// EnvironmentPrefix opens each reported environment line.
const EnvironmentPrefix = "module-environment: "

// moduleVersion is injected by the acceptance test so this module matches the
// receipt it is installed under.
var moduleVersion = "0.0.0-dev"

func main() {
	err := module.Serve(context.Background(),
		module.Options{Namespace: "reference", Version: moduleVersion},
		module.Command{Path: []string{"status"}, Run: status})
	if err != nil {
		fmt.Fprintf(os.Stderr, "noisymodule: %v\n", err)
		os.Exit(1)
	}
	// A conforming module exits successfully once it has answered. Exiting
	// otherwise is what the shell has to notice and report.
	if steered(UncleanExitMarker) {
		os.Exit(uncleanExitCode)
	}
}

// steered reports whether the named control file sits beside this executable.
func steered(name string) bool {
	executable, err := os.Executable()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(filepath.Dir(executable), name))
	return err == nil
}

func status(_ context.Context, _ module.Request) (result.Result, error) {
	// The environment is reported as diagnostics, which is the only channel a
	// module has. A test reads it to prove what the shell did not pass on.
	if steered(EnvironmentMarker) {
		fmt.Fprintf(os.Stderr, "%scount=%d\n", EnvironmentPrefix, len(os.Environ()))
		for _, entry := range os.Environ() {
			fmt.Fprintln(os.Stderr, EnvironmentPrefix+entry)
		}
	}
	fmt.Fprintln(os.Stderr, "a diagnostic from the module")
	produced := result.New("reference.status/v1").
		With("organization", "Organization", "reference-org").
		With("service", "Service", "reference").
		With("status", "Status", "operational").
		With("checkedAt", "Checked at", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintln(os.Stderr, "a second diagnostic from the module")
	return produced, nil
}
