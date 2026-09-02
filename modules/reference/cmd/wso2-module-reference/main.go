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

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/sdk/cobratree"
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

// WhoamiSchema identifies the semantic shape of the reference whoami result.
const WhoamiSchema = "reference.whoami/v1"

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

	options := moduleOptions()

	if *describe {
		reportIdentity(module.Describe(options))
		return
	}

	// Standard output now carries protocol frames only. Anything this process
	// wants to say goes to standard error, where the shell captures it as
	// bounded diagnostics.
	err := commandTree().Serve(context.Background(), options)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wso2-module-reference: %v\n", err)
		os.Exit(1)
	}
}

// moduleOptions describe this module to the SDK.
func moduleOptions() module.Options {
	return module.Options{
		Namespace:     Namespace,
		Version:       moduleVersion,
		AuthAudiences: []string{StatusAudience},
		AuthScopes:    []string{StatusScope},
	}
}

// commandTree builds this module's command tree and binds each command to its
// handler.
//
// The tree is a Cobra tree because a product CLI being migrated already has one:
// its commands, its flags, and its help are declared here exactly as they would
// be in a standalone CLI. What changes is the ending — a handler returns typed
// fields instead of printing, because the shell owns rendering.
func commandTree() *cobratree.Tree {
	root := &cobra.Command{
		Use:   "reference",
		Short: "Reference product module for the WSO2 CLI.",
	}
	statusCommand := &cobra.Command{
		Use:   "status",
		Short: "Report the reference status service's own answer.",
	}
	whoamiCommand := &cobra.Command{
		Use:   "whoami",
		Short: "Report the access the shell brokered for this invocation.",
	}
	root.AddCommand(statusCommand, whoamiCommand)

	return cobratree.New(root).
		Handle(statusCommand, status).
		Handle(whoamiCommand, whoami)
}

// status answers "wso2 reference status".
//
// It asks the shell for access, reads the status service with what it was
// granted, and returns semantic fields in presentation order. It performs no
// formatting: the shell alone decides whether the user sees a table or JSON,
// and the field order here is the order both renderings follow.
//
// It never sees a credential. It asks for an audience and scope, receives a
// short-lived token, and has no way to obtain another.
func status(ctx context.Context, request module.Request) (result.Result, error) {
	access, err := request.Access.Acquire(ctx, module.AccessRequest{
		Audience: StatusAudience,
		Scopes:   []string{StatusScope},
	})
	if err != nil {
		// A denial is the shell's own typed problem. Returning it unchanged
		// keeps one account of why access was refused.
		return result.Result{}, err
	}

	status, err := readStatus(ctx, request.Context.Endpoint, request.InvocationID, access.Token)
	if err != nil {
		return result.Result{}, err
	}
	return result.New(StatusSchema).
		With("organization", "Organization", status.Organization).
		With("service", "Service", status.Service).
		With("status", "Status", status.Status).
		With("checkedAt", "Checked at", status.CheckedAt), nil
}

// whoami answers "wso2 reference whoami".
//
// It reports what the access this invocation was granted actually conveys. The
// second command exists to make the brokered hand-off visible: status proves
// the access works, and whoami proves what it is — which audience it names,
// which scopes it carries, and which invocation it is bound to.
//
// The claims are read back from the audience, never from the token. This
// handler holds the same opaque string status does and has no more ability to
// read it. The token itself is never part of the result: a claim is not secret,
// the material that carries it is.
func whoami(ctx context.Context, request module.Request) (result.Result, error) {
	access, err := request.Access.Acquire(ctx, module.AccessRequest{
		Audience: StatusAudience,
		Scopes:   []string{StatusScope},
	})
	if err != nil {
		return result.Result{}, err
	}

	granted, err := readWhoami(ctx, request.Context.Endpoint, request.InvocationID, access.Token)
	if err != nil {
		return result.Result{}, err
	}
	return result.New(WhoamiSchema).
		With("organization", "Organization", granted.Organization).
		With("audiences", "Audiences", granted.Audiences).
		With("scopes", "Scopes", granted.Scopes).
		With("invocation", "Invocation", granted.Invocation).
		With("boundTo", "Bound to", granted.BoundTo).
		With("expiresAt", "Expires at", access.ExpiresAt.Format(time.RFC3339)), nil
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
