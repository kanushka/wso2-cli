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

package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/install"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// catalogTimeout bounds one install. It covers the whole operation rather than
// one request, so a stalled origin cannot hold a command open indefinitely.
const catalogTimeout = 10 * time.Minute

// module runs the module lifecycle commands.
func (s Shell) module(args []string) error {
	if len(args) == 0 {
		return problem.New(problem.CategoryUsage, "shell.missing_argument",
			"wso2 module needs a subcommand").
			WithRecovery("Run wso2 module install <module> to install a product module.")
	}
	switch args[0] {
	case "install":
		return s.moduleInstall(args[1:])
	default:
		return problem.New(problem.CategoryUsage, "shell.unknown_command",
			fmt.Sprintf("%q is not a wso2 module subcommand", args[0])).
			WithRecovery("Run wso2 module install <module> to install a product module.")
	}
}

// moduleInstall installs one product module from the catalog.
//
// The module may be named as "<module>@<version>" to install an exact version,
// which is what a pipeline pins so its behaviour does not depend on what is
// newest that day. Without a pin, the newest version on the chosen channel that
// this shell can launch on this platform is installed.
func (s Shell) moduleInstall(args []string) error {
	namespace, policy, err := parseInstallArguments(args)
	if err != nil {
		return err
	}

	store, err := s.store()
	if err != nil {
		return err
	}
	identity, err := s.identity()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), catalogTimeout)
	defer cancel()
	installer := install.Installer{
		Store:  store,
		Client: catalog.Client{Origin: catalog.Origin(), HTTP: &http.Client{}},
		Shell:  identity,
	}
	installed, err := installer.Run(ctx, install.Request{Namespace: namespace, Policy: policy})
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(s.Streams.Out,
		"Installed %s v%s for %s.\nThe artifact was checked against the digest the catalog publishes. "+
			"Artifacts are integrity-checked, not signed.\n",
		installed.Namespace, installed.Version, installed.Platform)
	return err
}

// parseInstallArguments reads the module to install and the policy to select
// its version under.
func parseInstallArguments(args []string) (string, catalog.Policy, error) {
	var namespace string
	var policy catalog.Policy
	for index := 0; index < len(args); index++ {
		argument := args[index]
		switch {
		case argument == "--channel":
			if index+1 >= len(args) {
				return "", catalog.Policy{}, problem.New(problem.CategoryUsage, "shell.missing_argument",
					"--channel needs a channel name").
					WithRecovery("Run wso2 module install <module> --channel stable.")
			}
			index++
			policy.Channel = args[index]
		case strings.HasPrefix(argument, "-"):
			return "", catalog.Policy{}, problem.New(problem.CategoryUsage, "shell.unknown_flag",
				fmt.Sprintf("%q is not a wso2 module install flag", argument)).
				WithRecovery("Run wso2 module install <module> [--channel <channel>].")
		case namespace != "":
			return "", catalog.Policy{}, problem.New(problem.CategoryUsage, "shell.unexpected_argument",
				fmt.Sprintf("wso2 module install takes one module, got %q as well", argument)).
				WithRecovery("Run wso2 module install <module>.")
		default:
			namespace, policy.Version, _ = strings.Cut(argument, "@")
		}
	}
	if namespace == "" {
		return "", catalog.Policy{}, problem.New(problem.CategoryUsage, "shell.missing_argument",
			"wso2 module install needs a module to install").
			WithRecovery("Run wso2 module install <module>.")
	}
	if policy.Channel != "" && policy.Version != "" {
		return "", catalog.Policy{}, problem.New(problem.CategoryUsage, "shell.conflicting_arguments",
			"a pinned version and a channel cannot both be given").
			WithRecovery("Pin a version, or choose a channel, but not both.")
	}
	return namespace, policy, nil
}
