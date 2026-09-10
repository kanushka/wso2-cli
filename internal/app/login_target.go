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
	"os"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// The deployments a new login can be set up against, in the order the
// deployment question lists them.
const (
	deploymentCloud = iota
	deploymentLocal
)

// cloudComingSoon is what picking WSO2 Cloud says until the shell ships the
// WSO2-published public client that lets a cloud login ask nothing (#186).
const cloudComingSoon = "WSO2 Cloud login is coming soon. Choose Local for now."

// resolveLoginTarget decides which context a login without --url is about,
// asking when the flags leave it open and something may ask (#186).
//
// It returns the flags the rest of login acts on: a --context naming an
// existing context, or a --url that sends login down the creating path. Only
// what the user answered is filled in, so every check the creating path makes
// on a flag it makes on an answer too.
//
// When nothing may prompt it asks nothing and changes nothing, except that a
// --context naming no context is refused here with the flags that would create
// it — a login is the command that creates one, so pointing at wso2 context
// list would be advice about the wrong problem. A login with neither --context
// nor a prompt keeps logging in to the selected context, so scripts that
// relied on that keep working.
func (s Shell) resolveLoginTarget(flags loginFlags) (loginFlags, error) {
	root, err := s.stateRoot()
	if err != nil {
		return flags, err
	}
	document, err := contexts.Load(root)
	if err != nil {
		return flags, err
	}
	may, _ := s.mayPrompt(flags.noInput)

	if flags.contextName != "" {
		if declaresContext(document, flags.contextName) {
			return flags, nil
		}
		if err := checkLoginContextName(flags); err != nil {
			return flags, err
		}
		if !may {
			return flags, missingLoginContext(flags.contextName)
		}
		return s.askNewLoginTarget(flags)
	}
	// WSO2_CONTEXT is a selection the user made, just not on this command
	// line, so it is honoured the way the selection that follows honours it.
	if !may || os.Getenv("WSO2_CONTEXT") != "" {
		return flags, nil
	}
	if len(document.Contexts) == 0 {
		if _, err := fmt.Fprintln(s.Streams.Err, "No contexts yet. Let's create one."); err != nil {
			return flags, err
		}
		return s.askNewLoginTarget(flags)
	}

	choice, err := s.choose("Log in to:", []promptOption{
		{label: "An existing context"},
		{label: "A new context"},
	}, 0)
	if err != nil {
		return flags, err
	}
	if choice == 1 {
		return s.askNewLoginTarget(flags)
	}

	options := make([]promptOption, len(document.Contexts))
	fallback := 0
	for index, candidate := range document.Contexts {
		options[index] = promptOption{label: candidate.Name}
		if candidate.Name == document.DefaultContext {
			options[index].label += " (selected)"
			fallback = index
		}
	}
	choice, err = s.choose("Context:", options, fallback)
	if err != nil {
		return flags, err
	}
	flags.contextName = document.Contexts[choice].Name
	return flags, nil
}

// askNewLoginTarget asks where a new context authenticates: the deployment,
// then, for a local one, its issuer URL. The client ID and the name are asked
// afterwards by the creating path, which asks them of a --url login already.
func (s Shell) askNewLoginTarget(flags loginFlags) (loginFlags, error) {
	_, err := s.choose("Deployment:", []promptOption{
		deploymentCloud: {label: "WSO2 Cloud (coming soon)", unavailable: cloudComingSoon},
		deploymentLocal: {label: "Local (Identity Server / Thunder)"},
	}, deploymentLocal)
	if err != nil {
		return flags, err
	}
	issuer, err := s.askIssuerURL()
	if err != nil {
		return flags, err
	}
	flags.issuer = issuer
	return flags, nil
}

// askIssuerURL reads an issuer URL, asking again when the answer is not one.
// The person is still at the terminal, so a typo costs them a line rather
// than the whole command. Like refuseNonIssuerURL, it never echoes the value.
func (s Shell) askIssuerURL() (string, error) {
	for {
		if _, err := fmt.Fprint(s.Streams.Err, "Issuer URL: "); err != nil {
			return "", err
		}
		answer, ok, err := s.readLine()
		if err != nil {
			return "", err
		}
		if !ok {
			return "", problem.New(problem.CategoryUsage, "shell.missing_required_flag",
				"no issuer URL was entered at the prompt").
				WithRecovery(loginUsageRecovery)
		}
		if answer != "" && refuseNonIssuerURL(answer) == nil {
			return answer, nil
		}
		if _, err := fmt.Fprintln(s.Streams.Err,
			"Enter the issuer as an absolute URL with no user name or password, "+
				"as in https://localhost:9443/oauth2/token."); err != nil {
			return "", err
		}
	}
}

// missingLoginContext refuses a login, with nothing to ask, that names a
// context no document declares and gives nothing to create it from.
func missingLoginContext(name string) problem.Problem {
	return problem.New(problem.CategoryUsage, "shell.missing_required_flag",
		fmt.Sprintf("no context named %q exists, and creating it takes --url and --client-id", name)).
		WithRecovery(fmt.Sprintf("Run wso2 login --context %s --url <issuer> --client-id <id> "+
			"to create it, or name an existing context (wso2 context list).", name))
}
