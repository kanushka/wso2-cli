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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/yamldoc"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// CloudEnvVar names the WSO2 Cloud environment a login without a target signs
// in to, such as dev, stage, prod-us or prod-eu. An environment's values are
// read from a file under the state root, so no hostname lives in this source.
const CloudEnvVar = "WSO2_CLOUD_ENV"

// cloudClientID is the public client WSO2 Cloud registers for this shell.
const cloudClientID = "WSO2_CLI"

var cloudEnvName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)

var (
	hostLabel  = `[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?`
	hostLabels = regexp.MustCompile(`^` + hostLabel + `$`)
	hostDomain = regexp.MustCompile(`^` + hostLabel + `(\.` + hostLabel + `)+$`)
)

// cloudEnvironment is what one environment file states.
type cloudEnvironment struct {
	Issuer string `json:"issuer"`
	// BaseDomain and Environment locate the environment's product APIs.
	BaseDomain  string `json:"baseDomain"`
	Environment string `json:"environment"`
	ClientID    string `json:"clientId,omitempty"`
}

// cloudEnvironmentName is the environment CloudEnvVar selects.
func cloudEnvironmentName() (string, error) {
	env := os.Getenv(CloudEnvVar)
	if !cloudEnvName.MatchString(env) {
		return "", problem.New(problem.CategoryUsage, "shell.invalid_argument",
			fmt.Sprintf("%s=%q is not an environment name", CloudEnvVar, env)).
			WithRecovery(fmt.Sprintf("Set %s to a lowercase name such as dev, stage, prod-us or prod-eu, "+
				"or unset it.", CloudEnvVar))
	}
	if env == "prod" {
		return "prod-us", nil
	}
	return env, nil
}

func cloudContextName(env string) string { return "cloud-" + env }

func cloudEnvironmentPath(root, env string) string {
	return filepath.Join(root, "cloud", env+".yaml")
}

// loginCloud returns the flags that log in to the selected environment's
// context, selecting it, and writing it first when this machine has none.
func (s Shell) loginCloud(flags loginFlags, root string, document contexts.Document) (loginFlags, error) {
	env, err := cloudEnvironmentName()
	if err != nil {
		return flags, err
	}
	name := cloudContextName(env)
	flags.contextName = name
	var written *contexts.Context
	if !declaresContext(document, name) {
		environment, err := readCloudEnvironment(root, env)
		if err != nil {
			return flags, err
		}
		if err := s.installCloudProducts(); err != nil {
			return flags, err
		}
		lookup := s.installedDescriptors()
		context, err := environment.context(name)
		if err != nil {
			return flags, err
		}
		context, err = withCloudProducts(context, environment, unrecordedCloudProducts(context, lookup), lookup)
		if err != nil {
			return flags, err
		}
		written = &context
	} else {
		if err := s.installCloudProducts(); err != nil {
			return flags, err
		}
		existing, _ := document.Find(name)
		if err := s.recordCloudProducts(root, env, existing, s.installedDescriptors()); err != nil {
			return flags, err
		}
		if document.DefaultContext == name {
			return flags, nil
		}
	}
	if err := contexts.Writable(root); err != nil {
		return flags, s.explainWriteRefusal(root, err)
	}
	return flags, contexts.Update(root, func(document contexts.Document) (contexts.Document, error) {
		if written != nil {
			written.CredentialRef = document.NewCredentialRef(name)
			document = document.Put(*written)
		}
		document.DefaultContext = name
		return document, nil
	})
}

func readCloudEnvironment(root, env string) (cloudEnvironment, error) {
	path := cloudEnvironmentPath(root, env)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cloudEnvironment{}, problem.New(problem.CategoryUsage, "shell.cloud_environment_unknown",
			fmt.Sprintf("no WSO2 Cloud %q environment is configured on this machine", env)).
			WithRecovery(fmt.Sprintf("Place the %s environment file at %s, or unset %s.", env, path, CloudEnvVar))
	}
	if err != nil {
		return cloudEnvironment{}, problem.New(problem.CategoryUsage, "shell.input_unreadable",
			fmt.Sprintf("the WSO2 Cloud %q environment file %s cannot be read", env, path)).
			WithRecovery("Check that the file is readable.")
	}
	malformed := func(detail string) error {
		return problem.New(problem.CategoryUsage, "shell.input_malformed",
			fmt.Sprintf("the WSO2 Cloud environment file %s %s", path, detail)).
			WithRecovery("State issuer, baseDomain and environment, and optionally clientId, then run the command again.")
	}
	data, err = yamldoc.ToJSON(data)
	if err != nil {
		return cloudEnvironment{}, malformed("is not YAML: " + err.Error())
	}
	var environment cloudEnvironment
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&environment); err != nil {
		return cloudEnvironment{}, malformed("does not have the shape of an environment file: " + err.Error())
	}
	for member, value := range map[string]string{
		"issuer": environment.Issuer, "baseDomain": environment.BaseDomain, "environment": environment.Environment,
	} {
		if value == "" {
			return cloudEnvironment{}, malformed("states no " + member)
		}
	}
	// Both become part of every product's host name.
	if !hostDomain.MatchString(environment.BaseDomain) {
		return cloudEnvironment{}, malformed("states a baseDomain that is not a lowercase domain name")
	}
	if !hostLabels.MatchString(environment.Environment) {
		return cloudEnvironment{}, malformed("states an environment that is not a lowercase host name label")
	}
	return environment, nil
}

// context is the context a login to the environment writes: its login alone.
// Its products are recorded beside it, for the modules installed.
func (e cloudEnvironment) context(name string) (contexts.Context, error) {
	clientID := e.ClientID
	if clientID == "" {
		clientID = cloudClientID
	}
	input := inputContext{
		Name: name,
		Type: contexts.TypeCloud,
		Login: contexts.Login{
			Kind: contexts.KindOAuthBrowser, Issuer: e.Issuer, ClientID: clientID, Provider: contexts.ProviderThunder,
		},
	}
	if err := input.validate(); err != nil {
		return contexts.Context{}, err
	}
	return input.resolve(func(string) *modules.ProductDescriptor { return nil })
}
