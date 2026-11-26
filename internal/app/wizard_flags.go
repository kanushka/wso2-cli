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
	"fmt"
	"strings"

	"github.com/wso2/wso2-cli/internal/output"
)

// printFlagForm writes, after a wizard has done its work, the command lines
// that do the same with no question asked, so a person can script the setup
// they just walked through and an agent never needs the wizard.
func (s Shell) printFlagForm(lines ...string) error {
	name := output.NameOf(s.Streams.Err)
	var text strings.Builder
	text.WriteString("\nThe same without questions:\n")
	for _, line := range lines {
		fmt.Fprintf(&text, "  %s\n", output.Rename(line, name))
	}
	_, err := fmt.Fprint(s.Streams.Err, text.String())
	return err
}

// contextCreateLine is the wso2 context create command line that writes what
// the create wizard gathered, with every flag the command was given.
func contextCreateLine(name string, flags contextCreateFlags) string {
	words := []string{"wso2", "context", "create", shellWord(name),
		"--login-product", shellWord(flags.loginProduct), "--url", shellWord(flags.url)}
	words = appendFlag(words, "--provider", flags.provider)
	words = appendFlag(words, "--client-id", flags.clientID)
	if flags.issuerIDSet {
		words = append(words, "--issuer-id", shellWord(flags.issuerID))
	}
	words = appendFlag(words, "--audience", flags.audience)
	if flags.scopesSet {
		words = append(words, "--scopes", shellWord(strings.Join(flags.scopes, ",")))
	}
	words = appendFlag(words, "--client-secret-variable", flags.clientSecretVariable)
	words = appendFlag(words, "--organization", flags.organization)
	words = appendFlag(words, "--project", flags.project)
	words = appendSwitch(words, "--device", flags.device)
	words = appendSwitch(words, "--no-install", flags.noInstall)
	words = appendSwitch(words, "--use", flags.use)
	return strings.Join(words, " ")
}

// productAddLine is the wso2 context product add command line that records
// one product the way a wizard's answers and flags did, in context.
func productAddLine(namespace, context string, flags contextProductFlags) string {
	words := []string{"wso2", "context", "product", "add", shellWord(namespace), "--url", shellWord(flags.url)}
	words = appendFlag(words, "--gateway", flags.gateway)
	words = appendFlag(words, "--audience", flags.audience)
	if flags.scopesSet {
		words = append(words, "--scopes", shellWord(strings.Join(flags.scopes, ",")))
	}
	words = appendFlag(words, "--gateway-audience", flags.gatewayAudience)
	if flags.gatewayScopesSet {
		words = append(words, "--gateway-scopes", shellWord(strings.Join(flags.gatewayScopes, ",")))
	}
	words = appendFlag(words, "--client-id", flags.clientID)
	words = appendFlag(words, "--client-id-variable", flags.clientIDVariable)
	words = appendFlag(words, "--client-secret-variable", flags.clientSecretVariable)
	words = appendSwitch(words, "--replace", flags.replace)
	words = appendSwitch(words, "--no-install", flags.noInstall)
	words = appendFlag(words, "--context", context)
	return strings.Join(words, " ")
}

func appendFlag(words []string, flag, value string) []string {
	if value == "" {
		return words
	}
	return append(words, flag, shellWord(value))
}

func appendSwitch(words []string, flag string, on bool) []string {
	if !on {
		return words
	}
	return append(words, flag)
}
