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

// Command noisymodule is a conforming module that writes diagnostics while it
// answers, used by the shell's acceptance tests.
//
// The reference module has nothing to warn about, so it cannot demonstrate that
// structured output survives a talkative module. This one writes to standard
// error before and after returning its result, and claims the same namespace
// and version, so the shell resolves and launches it exactly as it would the
// reference module.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/result"
)

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
}

func status(_ context.Context, _ module.Request) (result.Result, error) {
	fmt.Fprintln(os.Stderr, "a diagnostic from the module")
	produced := result.New("reference.status/v1").
		With("organization", "Organization", "reference-org").
		With("service", "Service", "reference").
		With("status", "Status", "operational").
		With("checkedAt", "Checked at", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintln(os.Stderr, "a second diagnostic from the module")
	return produced, nil
}
