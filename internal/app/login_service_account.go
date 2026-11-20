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

// The variables a WSO2 Cloud service account is read from unless
// --client-id-variable and --client-secret-variable name others. The context
// records the secret's variable, never its value. The organization variable,
// when set, names the organization to sign in to instead of the one the
// service account is registered in.
const (
	ServiceAccountIDVariable           = "WSO2_CLIENT_ID"
	ServiceAccountSecretVariable       = "WSO2_CLIENT_SECRET"
	ServiceAccountOrganizationVariable = "WSO2_ORGANIZATION"
)

// serviceAccountCICommand is the login CI runs, naming the variables this
// one read when they are not the defaults, so it can be pasted as it is.
func serviceAccountCICommand(flags loginFlags) string {
	command := "wso2 login --service-account --no-input"
	if flags.clientIDVariable != "" {
		command += " --client-id-variable " + flags.clientIDVariable
	}
	if flags.clientSecretVariable != "" {
		command += " --client-secret-variable " + flags.clientSecretVariable
	}
	return command
}

// serviceAccountContextName is the context a service-account login writes
// when --context names none, beside the person's own cloud-<env>.
func serviceAccountContextName(env string) string { return cloudContextName(env) + "-ci" }

// serviceAccountReport is what wso2 login --service-account established.
// Context is empty when nothing was written.
type serviceAccountReport struct {
	Context      string                  `json:"context,omitempty"`
	Environment  string                  `json:"environment"`
	Organization string                  `json:"organization"`
	Products     []serviceAccountProduct `json:"products"`
	next         string
	// named is whether ServiceAccountOrganizationVariable picked Organization.
	named bool
}

// serviceAccountProduct is what one product's grant carried. A product the
// service account holds no permission for has empty Scopes.
type serviceAccountProduct struct {
	Namespace string   `json:"namespace"`
	Scopes    []string `json:"scopes"`
	Withheld  []string `json:"withheld,omitempty"`
}

// promptGate is whether the shell may ask, and why not when it may not.
type promptGate struct {
	may    bool
	reason string
}

// serviceAccountValue is one value a service-account login reads, from its
// variable when that is set, else asked when the gate allows.
type serviceAccountValue struct {
	value string
	asked bool
}

// loginServiceAccount signs in to the WSO2 Cloud environment WSO2_CLOUD_ENV
// names as the service account the client id and secret variables hold: it
// gets one client-credentials token per WSO2 Cloud product, the way every
// later command will, and writes a client-credentials context recording what
// they carried. Run on a terminal it asks for what is not set, checks it, and
// prints what CI needs.
func (s Shell) loginServiceAccount(flags loginFlags) error {
	if flags.issuer != "" || flags.clientID != "" || flags.only != "" || flags.noProducts {
		return problem.New(problem.CategoryUsage, "shell.conflicting_arguments",
			"--service-account signs in to the WSO2 Cloud environment the variables name, and takes no "+
				"--url, --client-id, --only or --no-products").
			WithRecovery("Run wso2 login --service-account, with " + CloudEnvVar + ", " +
				ServiceAccountIDVariable + " and " + ServiceAccountSecretVariable + " set.")
	}
	idVariable := orDefault(flags.clientIDVariable, ServiceAccountIDVariable)
	secretVariable := orDefault(flags.clientSecretVariable, ServiceAccountSecretVariable)
	may, reason := s.mayPrompt(flags.noInput)
	gate := promptGate{may: may, reason: reason}

	env, err := s.serviceAccountInput(CloudEnvVar, gate, func() (string, error) {
		return s.ask("WSO2 Cloud environment", "", func(answer string) error {
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
	organization, err := namedOrganization()
	if err != nil {
		return err
	}

	if gate.may && (strings.TrimSpace(os.Getenv(idVariable)) == "" || strings.TrimSpace(os.Getenv(secretVariable)) == "") {
		if _, err := fmt.Fprintf(s.Streams.Err, "Create a service account in the WSO2 Cloud console, give it "+
			"roles for the products it should reach (%s), and enter its client id and secret.\n",
			strings.Join(slices.Sorted(maps.Keys(cloudProducts)), ", ")); err != nil {
			return err
		}
	}
	clientID, err := s.serviceAccountInput(idVariable, gate, func() (string, error) {
		return s.ask("Service account client id", "", nil)
	})
	if err != nil {
		return err
	}
	secret, err := s.serviceAccountInput(secretVariable, gate, func() (string, error) {
		answer, err := s.prompter().Secret("Service account client secret")
		return answer, cancelled(err)
	})
	if err != nil {
		return err
	}
	clientID.value = strings.TrimSpace(clientID.value)
	typed := clientID.asked || secret.asked
	if gate.may && !typed {
		if _, err := fmt.Fprintf(s.Streams.Err, "Using the service account in %s and %s.\n",
			idVariable, secretVariable); err != nil {
			return err
		}
	}

	// Installed only once every question is answered, so one cancelled
	// leaves this machine as it was.
	if err := s.installCloudProducts(); err != nil {
		return err
	}
	lookup := s.installedDescriptors()
	installed := unrecordedCloudProducts(contexts.Context{}, lookup)
	name := orDefault(flags.contextName, serviceAccountContextName(envName))
	target, err := s.serviceAccountContext(environment, name, clientID.value, secretVariable, installed, lookup)
	if err != nil {
		return err
	}
	target.CloudEnvironment = envName
	// Recorded before the check, so its grants go to the organization's own
	// endpoint as every later command's will.
	target.Organization = organization
	target.Login.Tenant = organization
	report, err := s.checkServiceAccount(target, envName, secret.value)
	if err != nil {
		return credentialRefusal(err, typed, idVariable, secretVariable, organization)
	}
	if typed {
		if _, err := fmt.Fprintln(s.Streams.Err, "No context was written: the client id and secret were typed, "+
			"not set in the environment, so no command here could use them."); err != nil {
			return err
		}
	} else {
		if err := s.writeServiceAccountContext(root, recordGranted(target, report)); err != nil {
			return err
		}
		report.Context = name
		for _, product := range report.Products {
			if len(product.Scopes) > 0 {
				report.next = fmt.Sprintf("Run wso2 %s --help to see what the service account can do there.",
					product.Namespace)
				break
			}
		}
	}
	if gate.may {
		report.next = "Run " + serviceAccountCICommand(flags) + " in CI."
	}
	return s.renderServiceAccountReport(flags, report, gate.may)
}

// namedOrganization is the organization ServiceAccountOrganizationVariable
// names, empty when it is not set.
func namedOrganization() (string, error) {
	organization := strings.TrimSpace(os.Getenv(ServiceAccountOrganizationVariable))
	if organization != "" && !organizationUnitID.MatchString(organization) {
		return "", problem.New(problem.CategoryUsage, "shell.invalid_argument",
			fmt.Sprintf("%s is %q, which is not an organization id", ServiceAccountOrganizationVariable,
				organization)).
			WithRecovery(fmt.Sprintf("Set %s to the id of an organization the service account belongs to, "+
				"or unset it to sign in to the one it is registered in.", ServiceAccountOrganizationVariable))
	}
	return organization, nil
}

// serviceAccountInput reads variable, or asks when it is not set and the
// gate allows.
func (s Shell) serviceAccountInput(variable string, gate promptGate,
	ask func() (string, error)) (serviceAccountValue, error) {
	// Passed on as set: a command sends the secret exactly as its variable
	// holds it, and a login that trimmed it would pass where they fail.
	if value := os.Getenv(variable); strings.TrimSpace(value) != "" {
		return serviceAccountValue{value: value}, nil
	}
	if !gate.may {
		return serviceAccountValue{}, problem.New(problem.CategoryUsage, "shell.non_interactive",
			fmt.Sprintf("wso2 login --service-account needs %s, which is not set, and %s", variable, gate.reason)).
			WithRecovery(fmt.Sprintf("Set %s, then run the command again.", variable))
	}
	answer, err := ask()
	if errors.Is(err, wizard.ErrNoAnswer) {
		return serviceAccountValue{}, problem.New(problem.CategoryUsage, "shell.missing_argument",
			fmt.Sprintf("no value was given for %s", variable)).
			WithRecovery(fmt.Sprintf("Run the command again and answer, or set %s.", variable))
	}
	if err != nil {
		return serviceAccountValue{}, err
	}
	return serviceAccountValue{value: answer, asked: true}, nil
}

// serviceAccountContext is the client-credentials context a service account
// signs in through, recording each installed WSO2 Cloud product whose module
// admits one. One that does not is left out with a line saying so, and the
// login goes on with the rest.
func (s Shell) serviceAccountContext(e cloudEnvironment, name, clientID, secretVariable string,
	installed []string, lookup descriptorLookup) (contexts.Context, error) {
	input := inputContext{
		Name: name,
		Type: contexts.TypeCloud,
		Login: contexts.Login{
			Kind: contexts.KindClientCredentials, Issuer: e.Issuer, ClientID: clientID,
			ClientSecretVariable: secretVariable, Provider: contexts.ProviderThunder,
		},
	}
	if err := input.validate(); err != nil {
		return contexts.Context{}, err
	}
	target, err := input.resolve(func(string) *modules.ProductDescriptor { return nil })
	if err != nil {
		return contexts.Context{}, err
	}
	for _, namespace := range installed {
		recorded, err := withCloudProducts(target, e, []string{namespace}, lookup)
		var refused problem.Problem
		if errors.As(err, &refused) && refused.Code == "auth.product_not_configured" {
			s.log.Debug("a product was left out of the service-account context", "namespace", namespace,
				"error", err.Error())
			if _, err := fmt.Fprintf(s.Streams.Err, "Left out %s: its module does not accept a service "+
				"account.\n", namespace); err != nil {
				return contexts.Context{}, err
			}
			continue
		}
		if err != nil {
			return contexts.Context{}, err
		}
		target = recorded
	}
	if len(target.Products) == 0 {
		return contexts.Context{}, problem.New(problem.CategoryNotFound, "shell.product_not_installed",
			"no installed WSO2 Cloud product accepts a service account").
			WithRecovery("Install a WSO2 Cloud product with wso2 module install <name>, then run the command again.")
	}
	return target, nil
}

// checkServiceAccount gets one token per recorded product, as a command
// would, and reports what each carried. A product the service account holds
// no permission for, or that the identity provider will not issue for, is
// named on standard error and reported with nothing granted; the login is
// refused only when that is every product. A target that already records an
// organization was issued its tokens at that organization's endpoint, so a
// token that names none was issued there too.
func (s Shell) checkServiceAccount(target contexts.Context, env, secret string) (serviceAccountReport, error) {
	selection := contexts.Selection{Context: target, Identity: target.Account()}
	named := target.Organization
	report := serviceAccountReport{Environment: env, Organization: named, named: named != ""}
	ctx, cancel := context.WithTimeout(context.Background(), loginDeadline)
	defer cancel()
	granted := 0
	for _, namespace := range slices.Sorted(maps.Keys(target.Products)) {
		check, err := auth.CheckClientGrant(ctx, nil, selection, namespace, secret)
		var refused problem.Problem
		if errors.As(err, &refused) && refused.Code == "auth.narrowing_unavailable" {
			s.log.Debug("a product refused the service account", "namespace", namespace, "error", err.Error())
			check = auth.ClientGrantCheck{Namespace: namespace, Withheld: target.Products[namespace].Scopes}
		} else if err != nil {
			return report, err
		}
		switch {
		case len(check.Granted) == 0:
			if _, err := fmt.Fprintf(s.Streams.Err, "The service account holds no permission for %s, so its "+
				"commands will be refused.\n", namespace); err != nil {
				return report, err
			}
		case len(check.Withheld) > 0:
			s.log.Debug("permissions withheld from the service account", "namespace", namespace,
				"withheld", strings.Join(check.Withheld, " "))
			if _, err := fmt.Fprintf(s.Streams.Err, "The service account lacks some permissions for %s; "+
				"commands that need them will be refused.\n", namespace); err != nil {
				return report, err
			}
		}
		if len(check.Granted) > 0 {
			granted++
			switch {
			case check.Organization == "" && named == "":
				return report, noOrganization(namespace)
			case named != "" && check.Organization != "" && check.Organization != named:
				return report, problem.New(problem.CategoryAuthPolicy, "auth.organization_mismatch",
					fmt.Sprintf("the identity provider issued the service account's access for %s in the %q "+
						"organization, not in %q, which %s names", namespace, check.Organization, named,
						ServiceAccountOrganizationVariable)).
					WithRecovery(fmt.Sprintf("Set %s to an organization the service account belongs to, or "+
						"unset it, then run the command again.", ServiceAccountOrganizationVariable))
			case report.Organization != "" && check.Organization != "" && check.Organization != report.Organization:
				return report, problem.New(problem.CategoryAuthPolicy, "auth.organization_not_selected",
					"the service account's access names more than one organization").
					WithRecovery("Check the service account's organization in the WSO2 Cloud console, then " +
						"run the command again.")
			}
			report.Organization = orDefault(check.Organization, named)
		}
		report.Products = append(report.Products, serviceAccountProduct{
			Namespace: namespace, Scopes: orEmpty(check.Granted), Withheld: check.Withheld,
		})
	}
	if granted == 0 {
		return report, problem.New(problem.CategoryAuthPolicy, "auth.narrowing_unavailable",
			"the service account holds no permission for any installed WSO2 Cloud product").
			WithRecovery("Give the service account a role for the products it should reach in the WSO2 " +
				"Cloud console, then run the command again.")
	}
	return report, nil
}

// noOrganization refuses a service account whose access for namespace names
// no organization, which every WSO2 Cloud product command needs.
func noOrganization(namespace string) problem.Problem {
	return problem.New(problem.CategoryAuthPolicy, "auth.organization_not_selected",
		fmt.Sprintf("the service account's access for %s names no organization", namespace)).
		WithRecovery("Configure the service account to name its organization in the tokens it is " +
			"issued, then run the command again.")
}

// credentialRefusal rewords a refused credential for a login that has
// written no context yet: the broker's refusal names the context it would
// have come from. A typed credential names no variable either, since none
// holds it. In an organization ServiceAccountOrganizationVariable names, the
// identity provider answers a client registered elsewhere the same way as a
// wrong secret, so the refusal names both.
func credentialRefusal(err error, typed bool, idVariable, secretVariable, organization string) error {
	var refused problem.Problem
	if !errors.As(err, &refused) || refused.Code != "auth.credential_unavailable" {
		return err
	}
	if organization != "" {
		return problem.New(problem.CategoryAuthPolicy, "auth.credential_unavailable",
			fmt.Sprintf("the identity provider did not accept the service account in the %q organization %s "+
				"names", organization, ServiceAccountOrganizationVariable)).
			WithRecovery(fmt.Sprintf("Set %s to an organization the service account belongs to, and check its "+
				"client id and secret, then run the command again.", ServiceAccountOrganizationVariable))
	}
	if typed {
		return problem.New(problem.CategoryAuthPolicy, "auth.credential_unavailable",
			"the identity provider did not accept the client id and secret entered").
			WithRecovery("Check the service account's client id and secret in the WSO2 Cloud console, then " +
				"run the command again.")
	}
	return problem.New(problem.CategoryAuthPolicy, "auth.credential_unavailable",
		fmt.Sprintf("the identity provider did not accept the service account in %s and %s",
			idVariable, secretVariable)).
		WithRecovery(fmt.Sprintf("Set %s and %s to a service account's current client id and secret, then "+
			"run the command again.", idVariable, secretVariable))
}

// recordGranted is target recording what the check found: each product's
// granted scopes, which are what a command may ask for, and the organization
// the tokens name. A product granted nothing keeps its full record, so a
// command there is refused naming what is missing.
func recordGranted(target contexts.Context, report serviceAccountReport) contexts.Context {
	for _, product := range report.Products {
		if len(product.Scopes) == 0 {
			continue
		}
		recorded := target.Products[product.Namespace]
		recorded.Scopes = product.Scopes
		target.Products[product.Namespace] = recorded
	}
	// The service account is registered in that organization and every token
	// it is issued is issued there, so it is the login's home tenant too.
	target.Organization = report.Organization
	target.Login.Tenant = report.Organization
	return target
}

// writeServiceAccountContext writes the context and selects it. It replaces
// only a WSO2 Cloud client-credentials context, one an earlier service-account
// login wrote, since every value in it is derived again on each login.
func (s Shell) writeServiceAccountContext(root string, target contexts.Context) error {
	if err := contexts.Writable(root); err != nil {
		return s.explainWriteRefusal(root, err)
	}
	return contexts.Update(root, func(document contexts.Document) (contexts.Document, error) {
		if existing, found := document.Find(target.Name); found &&
			(existing.Login.Kind != contexts.KindClientCredentials || !existing.WSO2Cloud()) {
			return document, contextExists(target.Name)
		}
		document = document.Put(target)
		document.DefaultContext = target.Name
		return document, nil
	})
}

// renderServiceAccountReport writes the report, and on a terminal what CI
// needs to run the same login.
func (s Shell) renderServiceAccountReport(flags loginFlags, report serviceAccountReport, may bool) error {
	mode, err := s.shellOutputMode(flags.command)
	if err != nil {
		return err
	}
	if mode == output.ModeJSON {
		return encodeJSONWithNext(s.Streams.Out, report, report.next)
	}
	namespaces := make([]string, 0, len(report.Products))
	for _, product := range report.Products {
		if len(product.Scopes) > 0 {
			namespaces = append(namespaces, product.Namespace)
		}
	}
	if _, err := fmt.Fprintln(s.Streams.Out); err != nil {
		return err
	}
	if err := output.Fields(s.Streams.Out, [][2]string{
		{"Context", orDefault(report.Context, "-")},
		{"Environment", report.Environment},
		{"Organization", report.Organization},
		{"Products", strings.Join(namespaces, ", ")},
	}); err != nil {
		return err
	}
	if !may {
		return output.NextStep(s.Streams.Out, report.next)
	}
	idVariable := orDefault(flags.clientIDVariable, ServiceAccountIDVariable)
	secretVariable := orDefault(flags.clientSecretVariable, ServiceAccountSecretVariable)
	lines := []string{
		"",
		"Set these in your CI, the secret as a masked secret:",
		"  " + CloudEnvVar + "=" + report.Environment,
	}
	if report.named {
		lines = append(lines, "  "+ServiceAccountOrganizationVariable+"="+report.Organization)
	}
	_, err = fmt.Fprintln(s.Streams.Out, strings.Join(append(lines,
		"  "+idVariable+"=<the client id>",
		"  "+secretVariable,
		"Then run:",
		"  "+output.Rename(serviceAccountCICommand(flags), output.NameOf(s.Streams.Out)),
		fmt.Sprintf("CI also needs the %s environment file, at %s under the shell's state directory (%s).",
			report.Environment, cloudEnvironmentPath("", report.Environment), state.RootEnvVar),
	), "\n"))
	return err
}

// ignoredServiceAccountVariables says, once, that a plain login to WSO2
// Cloud does not sign in as the service account its environment holds,
// unless the context it logs in to is a service account's own.
func (s Shell) ignoredServiceAccountVariables(selected contexts.Selection) error {
	if !selected.Context.WSO2Cloud() || selected.Context.Login.Kind == contexts.KindClientCredentials {
		return nil
	}
	var set []string
	for _, variable := range []string{ServiceAccountIDVariable, ServiceAccountSecretVariable} {
		if os.Getenv(variable) != "" {
			set = append(set, variable)
		}
	}
	if len(set) == 0 {
		return nil
	}
	s.log.Debug("service-account variables ignored by a plain login", "variables", strings.Join(set, " "))
	verb, pronoun := "is", "it"
	if len(set) > 1 {
		verb, pronoun = "are", "them"
	}
	_, err := fmt.Fprintf(s.Streams.Err, "%s %s set and ignored; %s uses %s.\n", strings.Join(set, " and "),
		verb, output.Rename("wso2 login --service-account", output.NameOf(s.Streams.Err)), pronoun)
	return err
}

// orEmpty is scopes, or an empty list where it is nil, so a product granted
// nothing reports "scopes": [] rather than null.
func orEmpty(scopes []string) []string {
	if scopes == nil {
		return []string{}
	}
	return scopes
}

// orDefault is value, or fallback when value is empty.
func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
