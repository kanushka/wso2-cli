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
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/internal/state"
	"github.com/wso2/wso2-cli/internal/wizard"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// The variables a WSO2 Cloud service account is read from. The context
// records the secret's variable, never its value.
const (
	ServiceAccountIDVariable     = "WSO2_CLIENT_ID"
	ServiceAccountSecretVariable = "WSO2_CLIENT_SECRET"
)

const serviceAccountCICommand = "wso2 login --service-account --no-input"

func serviceAccountContextName(env string) string { return cloudContextName(env) + "-ci" }

// serviceAccountReport is what wso2 login --service-account established.
// Context is empty when nothing was written: the values were typed rather
// than set in the environment, so a context naming their variables would
// fail every command on this machine.
type serviceAccountReport struct {
	Context      string                  `json:"context,omitempty"`
	Environment  string                  `json:"environment"`
	Organization string                  `json:"organization"`
	Products     []serviceAccountProduct `json:"products"`
	next         string
}

type serviceAccountProduct struct {
	Namespace string   `json:"namespace"`
	Scopes    []string `json:"scopes"`
	Withheld  []string `json:"withheld,omitempty"`
}

// serviceAccountValue is one value a service-account login reads, from its
// variable when that is set, else asked when the shell may ask.
type serviceAccountValue struct {
	value string
	asked bool
}

// loginServiceAccount signs in to the WSO2 Cloud environment WSO2_CLOUD_ENV
// names as the service account WSO2_CLIENT_ID and WSO2_CLIENT_SECRET hold:
// it gets one client-credentials token per Cloud product, the way every later
// command will, and writes a client-credentials context recording what they
// carried. Run on a terminal it asks for what is not set, checks it, and
// prints what CI needs.
func (s Shell) loginServiceAccount(flags loginFlags) error {
	if flags.issuer != "" || flags.clientID != "" || flags.only != "" || flags.noProducts {
		return problem.New(problem.CategoryUsage, "shell.conflicting_arguments",
			"--service-account signs in to the WSO2 Cloud environment the variables name, and takes no "+
				"--url, --client-id, --only or --no-products").
			WithRecovery("Run wso2 login --service-account, with " + CloudEnvVar + ", " +
				ServiceAccountIDVariable + " and " + ServiceAccountSecretVariable + " set.")
	}
	may, reason := s.mayPrompt(flags.noInput)
	env, err := s.serviceAccountInput(CloudEnvVar, "WSO2 Cloud environment", may, reason,
		func(title, fallback string, _ func(string) error) (string, error) {
			return s.ask(title, fallback, func(answer string) error {
				if _, err := checkedCloudEnvironment(answer); err != nil {
					return wizard.Hint("Name an environment such as dev, stage, prod-us or prod-eu.")
				}
				return nil
			})
		})
	if err != nil {
		return err
	}
	envName, err := checkedCloudEnvironment(env.value)
	if err != nil {
		return err
	}
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	environment, err := readCloudEnvironment(root, envName)
	if err != nil {
		return err
	}
	clientID, err := s.serviceAccountInput(ServiceAccountIDVariable, "Service account client id", may, reason, s.ask)
	if err != nil {
		return err
	}
	secret, err := s.serviceAccountInput(ServiceAccountSecretVariable, "Service account client secret", may, reason,
		func(title, _ string, _ func(string) error) (string, error) {
			answer, err := s.prompter().Secret(title)
			return answer, cancelled(err)
		})
	if err != nil {
		return err
	}
	if may && !clientID.asked && !secret.asked {
		if _, err := fmt.Fprintf(s.Streams.Err, "Using the service account in %s and %s.\n",
			ServiceAccountIDVariable, ServiceAccountSecretVariable); err != nil {
			return err
		}
	}

	if err := s.installCloudProducts(); err != nil {
		return err
	}
	name := flags.contextName
	if name == "" {
		name = serviceAccountContextName(envName)
	}
	target, err := environment.serviceAccountContext(name, clientID.value, s.installedDescriptors())
	if err != nil {
		return err
	}
	target.CloudEnvironment = envName
	if len(target.Products) == 0 {
		return problem.New(problem.CategoryNotFound, "shell.product_not_installed",
			"no WSO2 Cloud product is installed for the service account to sign in to").
			WithRecovery("Install a WSO2 Cloud product with wso2 module install <name>, then run the command again.")
	}

	report, err := s.checkServiceAccount(target, envName, secret.value)
	if err != nil {
		return err
	}
	written := !env.asked && !clientID.asked && !secret.asked
	if written {
		// What the account was granted is what a command on this context may
		// ask for; a product it holds no role for keeps its full record, so a
		// command there is refused naming what is missing.
		for _, product := range report.Products {
			recorded := target.Products[product.Namespace]
			recorded.Scopes = product.Scopes
			target.Products[product.Namespace] = recorded
		}
		// The account is registered in that organization and every token it
		// is issued is issued there, so it is the login's home tenant too.
		target.Organization = report.Organization
		target.Login.Tenant = report.Organization
		if err := s.writeServiceAccountContext(root, target); err != nil {
			return err
		}
		report.Context = name
		report.next = "Run wso2 whoami to see the context commands now run against."
	}
	if may {
		report.next = "Run " + serviceAccountCICommand + " in CI."
	}
	return s.renderServiceAccountReport(flags, report, envName, may, written)
}

type askFunc func(title, fallback string, validate func(string) error) (string, error)

func (s Shell) serviceAccountInput(variable, title string, may bool, reason string, ask askFunc) (serviceAccountValue, error) {
	if value := strings.TrimSpace(os.Getenv(variable)); value != "" {
		return serviceAccountValue{value: value}, nil
	}
	if !may {
		return serviceAccountValue{}, problem.New(problem.CategoryUsage, "shell.non_interactive",
			fmt.Sprintf("wso2 login --service-account needs %s, which is not set, and %s", variable, reason)).
			WithRecovery(fmt.Sprintf("Set %s, %s and %s, then run the command again.",
				CloudEnvVar, ServiceAccountIDVariable, ServiceAccountSecretVariable))
	}
	answer, err := ask(title, "", nil)
	if errors.Is(err, wizard.ErrNoAnswer) {
		return serviceAccountValue{}, problem.New(problem.CategoryUsage, "shell.missing_argument",
			fmt.Sprintf("no %s was given", strings.ToLower(title))).
			WithRecovery(fmt.Sprintf("Run the command again and answer, or set %s.", variable))
	}
	if err != nil {
		return serviceAccountValue{}, err
	}
	return serviceAccountValue{value: answer, asked: true}, nil
}

// serviceAccountContext is the client-credentials context a service account
// signs in through, recording every installed WSO2 Cloud product.
func (e cloudEnvironment) serviceAccountContext(name, clientID string, lookup descriptorLookup) (contexts.Context, error) {
	input := inputContext{
		Name: name,
		Type: contexts.TypeCloud,
		Login: contexts.Login{
			Kind: contexts.KindClientCredentials, Issuer: e.Issuer, ClientID: clientID,
			ClientSecretVariable: ServiceAccountSecretVariable, Provider: contexts.ProviderThunder,
		},
	}
	if err := input.validate(); err != nil {
		return contexts.Context{}, err
	}
	target, err := input.resolve(func(string) *modules.ProductDescriptor { return nil })
	if err != nil {
		return contexts.Context{}, err
	}
	var installed []string
	for _, namespace := range slices.Sorted(maps.Keys(cloudProducts)) {
		if lookup(namespace) != nil {
			installed = append(installed, namespace)
		}
	}
	return withCloudProducts(target, e, installed, lookup)
}

// checkServiceAccount gets one token per recorded product, as a command
// would, and reports what each carried. A product granted nothing is named
// on standard error; a login no product granted anything is refused.
func (s Shell) checkServiceAccount(target contexts.Context, env, secret string) (serviceAccountReport, error) {
	selection := contexts.Selection{Context: target, Identity: target.Account()}
	report := serviceAccountReport{Environment: env}
	ctx, cancel := context.WithTimeout(context.Background(), loginDeadline)
	defer cancel()
	for _, namespace := range slices.Sorted(maps.Keys(target.Products)) {
		check, err := auth.CheckClientGrant(ctx, nil, selection, namespace, secret)
		if err != nil {
			return report, err
		}
		if len(check.Granted) == 0 {
			if _, err := fmt.Fprintf(s.Streams.Err, "The service account holds no role for %s, so its commands "+
				"will be refused.\n", namespace); err != nil {
				return report, err
			}
			continue
		}
		if len(check.Withheld) > 0 {
			if _, err := fmt.Fprintf(s.Streams.Err, "The service account was not granted %s for %s; commands "+
				"that need it will be refused.\n", strings.Join(check.Withheld, ", "), namespace); err != nil {
				return report, err
			}
		}
		if report.Organization == "" {
			report.Organization = check.Organization
		}
		report.Products = append(report.Products, serviceAccountProduct{
			Namespace: namespace, Scopes: check.Granted, Withheld: check.Withheld,
		})
	}
	if len(report.Products) == 0 {
		return report, problem.New(problem.CategoryAuthPolicy, "auth.narrowing_unavailable",
			"the service account holds no role for any installed WSO2 Cloud product").
			WithRecovery("Give the service account a role for the products it should reach in the WSO2 " +
				"Cloud console, then run the command again.")
	}
	if report.Organization == "" {
		return report, problem.New(problem.CategoryAuthPolicy, "auth.organization_not_selected",
			"the service account's access names no organization").
			WithRecovery("Configure the service account to name its organization in the tokens it is " +
				"issued, then run the command again.")
	}
	return report, nil
}

// writeServiceAccountContext writes the context and selects it, replacing
// one a previous service-account login wrote: every value in it is derived
// again on each login.
func (s Shell) writeServiceAccountContext(root string, target contexts.Context) error {
	if err := contexts.Writable(root); err != nil {
		return s.explainWriteRefusal(root, err)
	}
	return contexts.Update(root, func(document contexts.Document) (contexts.Document, error) {
		if existing, found := document.Find(target.Name); found && existing.Login.Kind != contexts.KindClientCredentials {
			return document, contextExists(target.Name)
		}
		document = document.Put(target)
		document.DefaultContext = target.Name
		return document, nil
	})
}

func (s Shell) renderServiceAccountReport(flags loginFlags, report serviceAccountReport, env string,
	may, written bool) error {
	mode, err := s.shellOutputMode(flags.command)
	if err != nil {
		return err
	}
	if mode == output.ModeJSON {
		return encodeJSONWithNext(s.Streams.Out, report, report.next)
	}
	namespaces := make([]string, len(report.Products))
	for index, product := range report.Products {
		namespaces[index] = product.Namespace
	}
	context := report.Context
	if !written {
		context = "- (nothing written: the values were typed, not set in the environment)"
	}
	if _, err := fmt.Fprintln(s.Streams.Out); err != nil {
		return err
	}
	if err := output.Fields(s.Streams.Out, [][2]string{
		{"Context", context},
		{"Environment", env},
		{"Organization", report.Organization},
		{"Products", strings.Join(namespaces, ", ")},
	}); err != nil {
		return err
	}
	if !may {
		return output.NextStep(s.Streams.Out, report.next)
	}
	lines := []string{
		"",
		"Set these in your CI, the secret as a masked secret:",
		"  " + CloudEnvVar + "=" + env,
		"  " + ServiceAccountIDVariable + "=<the client id>",
		"  " + ServiceAccountSecretVariable,
		"Then run:",
		"  " + output.Rename(serviceAccountCICommand, output.NameOf(s.Streams.Out)),
	}
	if env != "prod-us" && env != "prod-eu" {
		lines = append(lines, fmt.Sprintf("CI also needs the %s environment file, at %s under the "+
			"shell's state directory (%s).", env, cloudEnvironmentPath("", env), state.RootEnvVar))
	}
	_, err = fmt.Fprintln(s.Streams.Out, strings.Join(lines, "\n"))
	return err
}

// ignoredServiceAccountVariables says, once, that a plain login does not
// sign in as the service account its environment holds.
func (s Shell) ignoredServiceAccountVariables(flags loginFlags) error {
	if s.nonInteractiveControl(flags.noInput) != "" {
		return nil
	}
	if os.Getenv(ServiceAccountIDVariable) == "" && os.Getenv(ServiceAccountSecretVariable) == "" {
		return nil
	}
	_, err := fmt.Fprintf(s.Streams.Err, "%s and %s are set and were ignored; %s uses them.\n",
		ServiceAccountIDVariable, ServiceAccountSecretVariable,
		output.Rename("wso2 login --service-account", output.NameOf(s.Streams.Err)))
	return err
}
