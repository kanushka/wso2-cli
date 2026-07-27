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

// Command wso2-module-reference is the WSO2 CLI reference module.
//
// The reference module is not a product module. It exists only to prove and
// test the shell, the public SDK, and the module contract, and it owns the
// reserved non-product "reference" namespace.
//
// It is built against the public SDK alone. It imports no shell package, so it
// can move to another repository without changing its imports.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/wso2/wso2-cli/sdk/module"
)

// Namespace is the reserved non-product namespace this module owns.
const Namespace = "reference"

// Declared access. The shell intersects a runtime request with the module
// receipt, so these values also appear in the receipt written at installation.
const (
	StatusAudience = "reference-status"
	StatusScope    = "reference:status:read"
)

// moduleVersion is this module's own release version. A build injects it with:
//
//	go build -ldflags "-X main.moduleVersion=0.1.0"
//
// It moves independently of the shell, protocol, and SDK versions.
var moduleVersion = "0.0.0-dev"

func main() {
	flags := flag.NewFlagSet("wso2-module-reference", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	describe := flags.Bool("module-info", false,
		"Report this module's runtime identity as JSON on standard error and exit. Used by tests, not by the shell.")
	if err := flags.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	descriptor := module.Describe(module.Options{
		Namespace:     Namespace,
		Version:       moduleVersion,
		AuthAudiences: []string{StatusAudience},
		AuthScopes:    []string{StatusScope},
	})

	if *describe {
		// Standard output carries protocol frames only, so even this
		// test-only report goes to standard error. See
		// docs/adr/0002-module-transport.md.
		encoder := json.NewEncoder(os.Stderr)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(descriptor); err != nil {
			fmt.Fprintf(os.Stderr, "wso2-module-reference: cannot report module identity: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Nothing is written to standard output until the module contract is
	// implemented. Diagnostics belong on standard error, where the shell
	// captures them as bounded diagnostics.
	fmt.Fprintln(os.Stderr,
		"wso2-module-reference: the module contract is not implemented yet; this executable cannot be invoked by the shell.")
	os.Exit(1)
}
