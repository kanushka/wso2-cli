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

// Command wso2-module-amp is the Agent Manager product module for the WSO2 CLI.
//
// It is the worked example the module-author guide follows: a small,
// read-only module that lists the projects an organization holds and the
// agents a project deploys, through access the shell brokers for it. It is
// built against the public SDK alone and imports no shell package, which is
// what lets it be released, installed, and updated on its own schedule.
//
// The shell owns rendering. A handler returns semantic fields in presentation
// order and never prints: the same handler answers a table run and a JSON run,
// and the field order here is the order both follow. See
// docs/adr/0003-shell-owned-output.md.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/sdk/cobratree"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/result"
)

// Namespace is the product namespace this module owns. It is the first word of
// every command the module answers.
const Namespace = "amp"

// APIAudience is the logical audience this module asks the shell for: the
// stable name Agent Manager's API is known by, the same against every
// deployment. The concrete value a deployment stamps into a token, on Agent
// Manager the resource URL its bundled ThunderID binds access to, is the
// operator's to record on the identity's amp product entry, never this
// module's to compile in.
const APIAudience = "amp-api"

// The scopes Agent Manager's API requires for what this module does. They are
// declared once, as the ceiling the receipt records; commands ask for none,
// and the shell answers with the scopes the identity's amp product records.
const (
	ScopeProjectRead = "amp:project:read"
	ScopeAgentRead   = "amp:agent:read"
)

// StatusSchema identifies the semantic shape of this module's status result.
// The shell renders it without interpreting it, so a consumer of JSON output
// can rely on the name to know what the fields mean.
const StatusSchema = "amp.status/v1"

// NextField is the field name the shell renders as a trailing next-step line.
// Every result this module returns ends with it, so a user is never left
// wondering what to run.
const NextField = "next"

// moduleVersion is this module's own release version. A release injects it:
//
//	go build -ldflags "-X main.moduleVersion=0.1.0"
//
// It moves independently of the shell, protocol, and SDK versions.
var moduleVersion = "0.0.0-dev"

func main() {
	// Standard output carries protocol frames only. Anything this process wants
	// to say goes to standard error, where the shell captures it as bounded
	// diagnostics. See docs/adr/0002-module-transport.md.
	//
	// The tree is served, not its commands: Serve declares the tree to the
	// shell as well, which is what lets the shell answer --help, name a
	// mistyped command, and parse this module's flags before it is launched.
	if err := commands().Serve(context.Background(), moduleOptions()); err != nil {
		fmt.Fprintf(os.Stderr, "wso2-module-amp: %v\n", err)
		os.Exit(1)
	}
}

// moduleOptions describe this module to the SDK.
//
// AuthAudiences and AuthScopes are the same values module.json declares. They
// are two declarations of one fact: installation copies the manifest's into
// the receipt, the broker refuses a request the receipt did not authorize, and
// the repository's boundary test fails when the two disagree.
func moduleOptions() module.Options {
	return module.Options{
		Namespace:     Namespace,
		Version:       moduleVersion,
		AuthAudiences: []string{APIAudience},
		AuthScopes:    []string{ScopeProjectRead, ScopeAgentRead},
	}
}

// commands builds this module's command tree and binds each command to its
// handler.
//
// The tree is an ordinary Cobra tree: commands, flags, and help are declared
// here exactly as they would be in a standalone CLI. What differs is the
// ending, because a handler returns fields instead of printing them.
func commands() *cobratree.Tree {
	root := &cobra.Command{
		Use:   Namespace,
		Short: "Amp commands for the WSO2 CLI.",
	}
	statusCommand := &cobra.Command{
		Use:   "status",
		Short: "Report this module's own status and what to run first.",
	}
	projects, projectsListCommand := projectCommands()
	agents, agentsListCommand := agentCommands()
	root.AddCommand(statusCommand, projects, agents)

	return cobratree.New(root).
		Handle(statusCommand, status).
		Handle(projectsListCommand, projectsList(projectsListCommand)).
		Handle(agentsListCommand, agentsList(agentsListCommand))
}

// status answers "wso2 amp status".
//
// It reports what it can know without asking anything of the shell, so it
// answers before the module has been given an identity to act as, and its
// last field says what to run next.
func status(ctx context.Context, request module.Request) (result.Result, error) {
	next := "Record where Agent Manager runs on the identity you log in with: " +
		"wso2 identity add-product <identity> amp --endpoint <url> --audience <resource> --scopes " +
		strings.Join(recordedScopes(), ",") + ", then wso2 login."
	if request.Context.Endpoint != "" {
		next = "Run wso2 amp projects list to see what this deployment holds."
	}
	return result.New(StatusSchema).
		With("namespace", "Namespace", Namespace).
		With("version", "Version", moduleVersion).
		With("endpoint", "Endpoint", request.Context.Endpoint).
		With(NextField, "Next", next), nil
}
