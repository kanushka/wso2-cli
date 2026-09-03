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
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/sdk/cobratree"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
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

// ReportSchema identifies the semantic shape of "wso2 reference status": what
// this module is and what the shell brokered for it. It answers from the
// invocation alone and calls nothing, so it is the command that works on a
// developer's machine with nothing deployed (#147).
const ReportSchema = "reference.report/v1"

// StatusSchema identifies the semantic shape of the reference status service's
// answer, returned by "wso2 reference call". The name predates the command
// rename and is left alone: it names the shape of a status service's answer,
// which is what it still is.
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
//
// It returns the tree rather than the commands it serves, so that Serve can
// declare it as well as serve it. A module that hands module.Serve a command
// list alone declares nothing, and the shell then falls back to parsing a
// product line without knowing what the module accepts (#153).
func commandTree() *cobratree.Tree {
	root := &cobra.Command{
		Use:   "reference",
		Short: "Reference product module for the WSO2 CLI.",
	}
	statusCommand := &cobra.Command{
		Use:   "status",
		Short: "Report this module and the access the shell brokered for it.",
	}
	callCommand := &cobra.Command{
		Use:   "call",
		Short: "Read the reference status service with brokered access.",
	}
	// A real flag, declared the way a product module declares one, so the
	// contract is exercised end to end rather than only for command paths: the
	// shell reads this off the declared tree, parses "wso2 reference call
	// --timeout 2s" against it, and forwards it here. Until a module declared
	// its tree, test/acceptance/testdata/declaringmodule was the only thing
	// proving that path.
	timeout := callCommand.Flags().Duration("timeout", defaultStatusTimeout,
		"Give up on the reference status service after this long.")
	whoamiCommand := &cobra.Command{
		Use:   "whoami",
		Short: "Report the access the shell brokered for this invocation.",
	}
	// whoami reads the same service under the same deadline, so it takes the
	// same flag. One of the two carrying it and the other a constant would be a
	// difference with no reason behind it.
	whoamiTimeout := whoamiCommand.Flags().Duration("timeout", defaultStatusTimeout,
		"Give up on the reference status service after this long.")
	root.AddCommand(statusCommand, callCommand, whoamiCommand)

	return cobratree.New(root).
		Handle(statusCommand, status).
		Handle(callCommand, func(ctx context.Context, request module.Request) (result.Result, error) {
			// cobratree parses the command's flags before it calls the handler,
			// so this dereference reads what the invocation asked for rather
			// than the declared default.
			return call(ctx, request, *timeout)
		}).
		Handle(whoamiCommand, func(ctx context.Context, request module.Request) (result.Result, error) {
			return whoami(ctx, request, *whoamiTimeout)
		})
}

// status answers "wso2 reference status".
//
// It reports what this module is and what the shell granted it, and calls
// nothing. That is deliberate: this is a sample module, and the command a
// newcomer types first has to work on their machine with nothing deployed. The
// service-backed proof lives in call below.
//
// An access denial is reported as a field rather than returned as an error, so
// the command answers its own question when the answer is "no". Every other
// command in the shell fails with the auth exit class instead; this one is the
// exception because "did the broker grant anything?" is the whole question, and
// a command that cannot say "no" cannot answer it. call keeps the ordinary
// behaviour, so the exit-class contract is still exercised.
//
// It never sees a credential, and the token is never a field: what is reported
// is which audience and scopes were granted, not the material carrying them.
func status(ctx context.Context, request module.Request) (result.Result, error) {
	// Kept deliberately narrow. The shell renders a module result as a single
	// table row, one column per field, so a report that names everything it
	// could becomes a line no terminal can show. Platform and organization are
	// left out because wso2 version and wso2 whoami already answer for them.
	report := result.New(ReportSchema).
		With("module", "Module", Namespace+" v"+moduleVersion).
		With("context", "Context", request.Context.Name)

	access, err := request.Access.Acquire(ctx, module.AccessRequest{
		Audience: StatusAudience,
		Scopes:   []string{StatusScope},
	})
	if err != nil {
		report = report.With("access", "Access", "refused")
		// The shell's own account of the refusal is reported verbatim. A second
		// wording here would be a second answer to why access was denied.
		var typed problem.Problem
		if errors.As(err, &typed) {
			report = report.With("reason", "Reason", typed.Message)
			if typed.Recovery != "" {
				report = report.With("recovery", "Recovery", typed.Recovery)
			}
			return report, nil
		}
		return report.With("reason", "Reason", err.Error()), nil
	}
	return report.
		With("access", "Access", "granted").
		With("audience", "Audience", StatusAudience).
		With("scopes", "Scopes", StatusScope).
		With("expiresAt", "Expires", access.ExpiresAt.UTC().Format(time.RFC3339)), nil
}

// call answers "wso2 reference call".
//
// It asks the shell for access, reads the status service with what it was
// granted, and returns semantic fields in presentation order. It performs no
// formatting: the shell alone decides whether the user sees a table or JSON,
// and the field order here is the order both renderings follow.
//
// It never sees a credential. It asks for an audience and scope, receives a
// short-lived token, and has no way to obtain another.
//
// This is the command that proves a brokered token is accepted by a service at
// the declared audience, so it needs a reference status service to call and
// cannot succeed without one. It carried the name "status" until #147, which
// gave that name to the command a developer can actually run.
func call(ctx context.Context, request module.Request, timeout time.Duration) (result.Result, error) {
	access, err := request.Access.Acquire(ctx, module.AccessRequest{
		Audience: StatusAudience,
		Scopes:   []string{StatusScope},
	})
	if err != nil {
		// A denial is the shell's own typed problem. Returning it unchanged
		// keeps one account of why access was refused.
		return result.Result{}, err
	}

	status, err := readStatus(ctx, request.Context.Endpoint, request.InvocationID, access.Token, timeout)
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
func whoami(ctx context.Context, request module.Request, timeout time.Duration) (result.Result, error) {
	access, err := request.Access.Acquire(ctx, module.AccessRequest{
		Audience: StatusAudience,
		Scopes:   []string{StatusScope},
	})
	if err != nil {
		return result.Result{}, err
	}

	granted, err := readWhoami(ctx, request.Context.Endpoint, request.InvocationID, access.Token, timeout)
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
