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
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/result"
)

// Namespace is the reserved non-product namespace this module owns.
const Namespace = "reference"

// Declared access. The shell intersects a runtime request with the module
// receipt, so these values also appear in the receipt written at installation.
const (
	StatusAudience = "reference-status"
	StatusScope    = "reference:status:read"
)

// StatusSchema identifies the semantic shape of the reference status result.
// The shell renders it without interpreting it.
const StatusSchema = "reference.status/v1"

// staticOrganization is reported when the shell supplies no organization.
//
// The shell does not own a context store yet, so this slice increment answers
// with static semantic data. The authentication broker increment replaces it
// with the selected context's organization and a real status lookup.
const staticOrganization = "reference-org"

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

	options := module.Options{
		Namespace:     Namespace,
		Version:       moduleVersion,
		AuthAudiences: []string{StatusAudience},
		AuthScopes:    []string{StatusScope},
	}

	if *describe {
		reportIdentity(module.Describe(options))
		return
	}

	// Standard output now carries protocol frames only. Anything this process
	// wants to say goes to standard error, where the shell captures it as
	// bounded diagnostics.
	err := module.Serve(context.Background(), options,
		module.Command{Path: []string{"status"}, Run: status})
	if err != nil {
		fmt.Fprintf(os.Stderr, "wso2-module-reference: %v\n", err)
		os.Exit(1)
	}
}

// status answers "wso2 reference status".
//
// It returns semantic fields in presentation order and no formatting: the shell
// alone decides whether the user sees a table or JSON. The field order here is
// the order both renderings follow.
func status(_ context.Context, request module.Request) (result.Result, error) {
	organization := request.Context.OrganizationID
	if organization == "" {
		organization = staticOrganization
	}
	return result.New(StatusSchema).
		With("organization", "Organization", organization).
		With("service", "Service", "reference").
		With("status", "Status", "operational").
		With("checkedAt", "Checked at", time.Now().UTC().Format(time.RFC3339)), nil
}

// reportIdentity writes the module's runtime identity for tests.
func reportIdentity(descriptor module.Descriptor) {
	// Even this test-only report goes to standard error, because standard
	// output is reserved for protocol frames. See
	// docs/adr/0002-module-transport.md.
	encoder := json.NewEncoder(os.Stderr)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(descriptor); err != nil {
		fmt.Fprintf(os.Stderr, "wso2-module-reference: cannot report module identity: %v\n", err)
		os.Exit(1)
	}
}
