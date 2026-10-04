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
	"time"

	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// publishedLookupTimeout bounds the one index request a word no installed
// product owns can cost under an origin other than the built-in one. It is
// far shorter than catalogTimeout: the request only decides how a refusal is
// worded, and a typo must not hang on a slow origin.
const publishedLookupTimeout = 10 * time.Second

// publishedProduct reports whether namespace is a product the catalog
// publishes on the stable channel, the one wso2 product install selects, and
// its title when it is.
//
// Against the built-in origin it reads the index this release was built with,
// the same copy the help page lists products from, so a typo costs no request.
// Any other origin, from WSO2_CLI_CATALOG_ORIGIN or the "catalog-origin"
// preference, is asked, because the release's copy says nothing about what it
// publishes. A failure to ask reads as not published: the caller then refuses
// the word as unknown, exactly as it did before installs were offered.
func (s Shell) publishedProduct(namespace string) (string, bool) {
	if !modules.ValidNamespace(namespace) {
		return "", false
	}
	root, err := s.stateRoot()
	if err != nil {
		return "", false
	}
	var index catalog.Index
	if origin := catalog.Origin(root); origin == catalog.DefaultOrigin {
		index = catalog.ReleasedIndex()
		if s.ReleasedIndex != nil {
			index = *s.ReleasedIndex
		}
	} else {
		installer, err := s.installer()
		if err != nil {
			return "", false
		}
		ctx, cancel := context.WithTimeout(context.Background(), publishedLookupTimeout)
		defer cancel()
		index, err = installer.Client.Index(ctx)
		if err != nil {
			s.log.Debug("the catalog could not say whether a product is published",
				"namespace", namespace, "catalog_origin", origin, "error", err.Error())
			return "", false
		}
	}
	entry, err := index.Module(namespace)
	if err != nil || !entry.Publishes(catalog.ChannelStable) {
		return "", false
	}
	return catalog.SanitizedTitle(entry.Title), true
}

// offerInstall answers a command for a product the catalog publishes but this
// machine does not have. When a question can be put, it asks whether to
// install the product, defaulting to yes, and installs it; the command then
// runs as typed. When nothing can be asked, or the answer is no, it refuses
// with the install command, and nothing is written.
func (s Shell) offerInstall(namespace, title string, noInput bool) error {
	allowed, reason := s.mayPrompt(noInput)
	if !allowed {
		return notInstalled(namespace, "nothing can ask to install it: "+reason)
	}
	named := fmt.Sprintf("The %s product", namespace)
	if title != "" {
		named = fmt.Sprintf("%s (%s)", title, namespace)
	}
	yes, err := s.confirmDefaultYes(named + " is not installed. Install it now?")
	if err != nil {
		return err
	}
	if !yes {
		return notInstalled(namespace, "the install was declined")
	}
	return s.install([]installNeed{{Namespace: namespace}})
}

// confirmDefaultYes asks a yes/no question whose default is yes. It is for a
// question whose yes the person has already all but given, such as installing
// the product they just typed the name of; anything that removes or
// overwrites asks through confirm, whose default is no.
func (s Shell) confirmDefaultYes(title string) (bool, error) {
	yes, err := s.prompter().Confirm(title, true)
	return yes, cancelled(err)
}

// notInstalled is the refusal for a product the catalog publishes and this
// machine does not have, naming why it was not installed and the command
// that installs it.
func notInstalled(namespace, why string) problem.Problem {
	return problem.New(problem.CategoryUsage, "shell.product_not_installed",
		fmt.Sprintf("the %s product is not installed, and %s", namespace, why)).
		WithRecovery(fmt.Sprintf("Run wso2 product install %s, then run the command again.", namespace))
}
