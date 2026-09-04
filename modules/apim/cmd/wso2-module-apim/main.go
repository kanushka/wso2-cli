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

// Command wso2-module-apim is the API Manager product module for the WSO2 CLI.
//
// It registers this CLI on API Manager's resident key manager once
// (bootstrap, with an administrator password the shell hands it under the
// WSO2_APIM_ prefix), then publishes APIs, subscribes applications, wires
// an external key manager in, and calls an API through the gateway, each
// with exactly the scopes the shell brokers for it.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/sdk/cobratree"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/result"
)

// Namespace is the top-level command this module owns.
const Namespace = "apim"

// PublisherAudience is the logical audience this module asks the shell for.
// On API Manager the concrete audience is the registered client's id, which
// bootstrap prints into the identity create line.
const PublisherAudience = "apim-publisher"

// The scopes API Manager's REST APIs require, asked for per command.
const (
	ScopeAPIView    = "apim:api_view"
	ScopeAPICreate  = "apim:api_create"
	ScopeAPIPublish = "apim:api_publish"
	ScopeSubscribe  = "apim:subscribe"
	ScopeAppManage  = "apim:app_manage"
	ScopeAdmin      = "apim:admin"
)

// StatusSchema identifies the shape of the status result.
const StatusSchema = "apim.status/v1"

// NextField is the field name the shell renders as a trailing next-step line.
const NextField = "next"

var moduleVersion = "0.0.0-dev"

func main() {
	if err := commands().Serve(context.Background(), moduleOptions()); err != nil {
		fmt.Fprintf(os.Stderr, "wso2-module-apim: %v\n", err)
		os.Exit(1)
	}
}

func moduleOptions() module.Options {
	return module.Options{
		Namespace:     Namespace,
		Version:       moduleVersion,
		AuthAudiences: []string{PublisherAudience},
		AuthScopes: []string{ScopeAPIView, ScopeAPICreate, ScopeAPIPublish,
			ScopeSubscribe, ScopeAppManage, ScopeAdmin},
	}
}

func commands() *cobratree.Tree {
	root := &cobra.Command{
		Use:   Namespace,
		Short: "API Manager commands for the WSO2 CLI.",
	}
	statusCommand := &cobra.Command{
		Use:   "status",
		Short: "Report this module's own status and what to run first.",
	}
	bootstrapCommand, bootstrapFlags := bootstrapCommand()
	root.AddCommand(statusCommand, bootstrapCommand)
	return cobratree.New(root).
		Handle(statusCommand, status).
		Handle(bootstrapCommand, bootstrap(bootstrapFlags))
}

func status(ctx context.Context, request module.Request) (result.Result, error) {
	next := "Run wso2 apim bootstrap --url <base> once, then the wso2 identity create line it prints."
	if request.Context.Endpoint != "" {
		next = "Run wso2 apim apis list to see what this deployment publishes."
	}
	return result.New(StatusSchema).
		With("namespace", "Namespace", Namespace).
		With("version", "Version", moduleVersion).
		With("endpoint", "Endpoint", request.Context.Endpoint).
		With(NextField, "Next", next), nil
}
