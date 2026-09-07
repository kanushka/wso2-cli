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
// connect records from its --client-id.
const PublisherAudience = "apim-publisher"

// GatewayAudience is the logical audience gateway invoke asks the shell
// for, with the gateway record. The concrete audience is the API's own
// resource identifier, which connect --gateway records from its --audience.
const GatewayAudience = "apim-gateway"

// The scopes API Manager's REST APIs require. The module declares them
// once, for the product record to hold; commands ask for none.
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
		AuthAudiences: []string{PublisherAudience, GatewayAudience},
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
	apis, apisListCommand, apisImportCommand, apisDeployCommand, apisPublishCommand, importFlags, deployFlags := apiCommands()
	apps, appsListCommand, appsCreateCommand, appsSubscribeCommand, appsKeysCommand, appsMapKeysCommand, appFlags := appCommands()
	keyManagers, keyManagersListCommand, keyManagersAddCommand, keyManagerFlags := keyManagerCommands()
	gateway, gatewayInvokeCommand, gatewayFlags := gatewayCommands()
	root.AddCommand(statusCommand, bootstrapCommand, apis, apps, keyManagers, gateway)
	return cobratree.New(root).
		Handle(keyManagersListCommand, keyManagersList).
		Handle(keyManagersAddCommand, keyManagersAdd(keyManagersAddCommand, keyManagerFlags)).
		Handle(gatewayInvokeCommand, gatewayInvoke(gatewayInvokeCommand, gatewayFlags)).
		Handle(statusCommand, status).
		Handle(bootstrapCommand, bootstrap(bootstrapFlags)).
		Handle(apisListCommand, apisList).
		Handle(apisImportCommand, apisImport(importFlags)).
		Handle(apisDeployCommand, apisDeploy(apisDeployCommand, deployFlags)).
		Handle(apisPublishCommand, apisPublish(apisPublishCommand)).
		Handle(appsListCommand, appsList).
		Handle(appsCreateCommand, appsCreate(appsCreateCommand, appFlags)).
		Handle(appsSubscribeCommand, appsSubscribe(appsSubscribeCommand, appFlags)).
		Handle(appsKeysCommand, appsKeys(appsKeysCommand, appFlags)).
		Handle(appsMapKeysCommand, appsMapKeys(appsMapKeysCommand, appFlags))
}

func status(ctx context.Context, request module.Request) (result.Result, error) {
	next := "Run wso2 apim connect <base> --client-id <id> on the logged-in identity, with the public " +
		"client API Manager holds for this CLI, federated to the login provider; a pipeline runs " +
		"wso2 apim bootstrap --url <base> once and the wso2 apim connect line it prints instead."
	if request.Context.Endpoint != "" {
		next = "Run wso2 apim apis list to see what this deployment publishes."
	}
	return result.New(StatusSchema).
		With("namespace", "Namespace", Namespace).
		With("version", "Version", moduleVersion).
		With("endpoint", "Endpoint", request.Context.Endpoint).
		With(NextField, "Next", next), nil
}
