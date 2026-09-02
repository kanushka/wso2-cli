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

// Command declaringmodule is a conforming module that declares its command tree,
// used to prove the shell parses a product command line against what the module
// says it accepts.
//
// The reference module cannot play this part. It is built outside this
// workspace by the relocation test, against the published SDK, which is how the
// architecture proves a product module can live in another repository — so it
// cannot use an SDK API until that API is released. This module is built from
// the workspace and is free to.
//
// Its commands do nothing but report what reached them. What is under test is
// the line as the shell split it: which words became the command, which
// arguments were forwarded, and which flags the shell kept for itself.
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

// moduleVersion is injected by the build, as it is for every module.
var moduleVersion = "0.0.0-dev"

// ResultSchema identifies the shape this module reports.
const ResultSchema = "reference.declared/v1"

func main() {
	root := &cobra.Command{Use: "reference", Short: "A module that declares its commands."}
	status := &cobra.Command{Use: "status", Short: "Report what the shell forwarded."}
	status.Flags().String("since", "", "How far back to look.")
	status.Flags().BoolP("all", "a", false, "Include everything.")
	status.Flags().StringP("region", "r", "", "The region to look in.")
	apps := &cobra.Command{Use: "apps", Short: "Work with applications."}
	list := &cobra.Command{Use: "list", Short: "List applications."}
	root.AddCommand(status, apps)
	apps.AddCommand(list)

	tree := cobratree.New(root).
		Handle(status, report("status")).
		Handle(list, report("apps list"))

	options := module.Options{
		Namespace:     "reference",
		Version:       moduleVersion,
		AuthAudiences: []string{"reference-status"},
		AuthScopes:    []string{"reference:status:read"},
	}
	if err := tree.Serve(context.Background(), options); err != nil {
		fmt.Fprintf(os.Stderr, "declaringmodule: %v\n", err)
		os.Exit(1)
	}
}

// report answers a command by naming itself and echoing the arguments the shell
// forwarded, so a test can see exactly where the shell drew the line.
func report(command string) module.Handler {
	return func(_ context.Context, request module.Request) (result.Result, error) {
		return result.New(ResultSchema).
			With("command", "Command", command).
			With("arguments", "Arguments", fmt.Sprint(request.Arguments)), nil
	}
}
