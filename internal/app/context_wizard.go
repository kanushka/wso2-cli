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
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/internal/wizard"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// This file is the context setup wizard (#189): the questions wso2 context
// create, wso2 context product add and a creating wso2 login ask when their
// flags leave the setup open and a prompt is allowed.
//
// The wizard only gathers answers. Every answer is put into the flags the
// command already parses and goes through the same checks and the same
// descriptor resolution, so a context set up by answering questions is
// exactly the context the equivalent command line writes, frozen defaults
// included (ADR 0016). Nothing is written until the last question.

// suggestedLoginProduct is the product a Thunder context logs in through when
// no installed product names Thunder, installed first.
const suggestedLoginProduct = "iam"

// The sign-in modes the wizard offers, in the order it lists them.
const (
	signInBrowser = iota
	signInDevice
	signInMachine
)

// contextAnswers is everything the create wizard gathered.
type contextAnswers struct {
	name     string
	flags    contextCreateFlags
	products []productAnswer
	// login is whether to log in to the context once it is written.
	login bool
}

// productAnswer is one product the wizard was told the context reaches.
type productAnswer struct {
	namespace, url, gateway, clientID string
}

// wizardProducts is every installed product that declares a descriptor,
// by namespace, with the lookup that reads them. It is best effort, like the
// lookup: a store that cannot be read offers nothing.
func (s Shell) wizardProducts() ([]string, descriptorLookup) {
	lookup := s.installedDescriptors()
	store, err := s.store()
	if err != nil {
		return nil, lookup
	}
	namespaces, err := store.Namespaces()
	if err != nil {
		return nil, lookup
	}
	var described []string
	for _, namespace := range namespaces {
		if lookup(namespace) != nil {
			described = append(described, namespace)
		}
	}
	slices.Sort(described)
	return described, lookup
}

// askContextCreate asks for everything wso2 context create needs that its
// flags did not give. name is the name argument, empty when none was given.
// offerMore is false when the command's result is JSON: a JSON caller gets
// exactly one result, so the wizard offers no products and no login after.
// offerLogin is false when the caller logs in anyway, as wso2 login does.
func (s Shell) askContextCreate(document contexts.Document, name string, flags contextCreateFlags,
	offerMore, offerLogin bool) (contextAnswers, error) {
	answers := contextAnswers{name: name, flags: flags}
	installed, lookup := s.wizardProducts()
	descriptor, err := s.askLogin(&answers.flags, installed, lookup)
	if err != nil {
		return answers, err
	}
	if err := s.askSignIn(&answers.flags, descriptor); err != nil {
		return answers, err
	}
	if offerMore {
		// The login product may have been installed just now.
		installed, lookup = s.wizardProducts()
		answers.products, err = s.askProducts(installed, lookup, answers.flags.loginProduct)
		if err != nil {
			return answers, err
		}
	}
	if answers.name == "" {
		answers.name, _, err = s.askContextName(document, false)
		if err != nil {
			return answers, err
		}
	}
	if err := s.summarize(answers); err != nil {
		return answers, err
	}
	create, err := s.prompter().Confirm(fmt.Sprintf("Create the %q context?", answers.name), true)
	if err := cancelled(err); err != nil {
		return answers, err
	}
	if !create {
		return answers, notCreated()
	}
	if !answers.flags.use {
		answers.flags.use, err = s.prompter().Confirm(
			fmt.Sprintf("Select %q as the current context?", answers.name), true)
		if err := cancelled(err); err != nil {
			return answers, err
		}
	}
	if offerMore && offerLogin && answers.flags.clientSecretVariable == "" {
		answers.login, err = s.prompter().Confirm("Log in now?", true)
		if err := cancelled(err); err != nil {
			return answers, err
		}
	}
	return answers, nil
}

// askLogin fills in the login a new context signs in with. The wizard sets up
// a self-hosted Thunder context, the one provider beside WSO2 Cloud, whose
// login needs no questions; Asgardeo and Identity Server contexts are created
// with their flags. It returns the login product's descriptor.
func (s Shell) askLogin(flags *contextCreateFlags, installed []string,
	lookup descriptorLookup) (*modules.ProductDescriptor, error) {
	namespace := suggestedLoginProduct
	for _, candidate := range installed {
		if lookup(candidate).Provider == contexts.ProviderThunder {
			namespace = candidate
			break
		}
	}
	flags.loginProduct = namespace
	installedVersion, err := s.ensureInstalled(namespace, flags.noInstall)
	if err != nil {
		return nil, err
	}
	if installedVersion != "" {
		if _, err := fmt.Fprintf(s.Streams.Err, "Installed %s %s.\n", namespace, installedVersion); err != nil {
			return nil, err
		}
	}
	descriptor := s.installedDescriptors()(namespace)
	if descriptor == nil || !descriptor.LoginProvider() {
		_, _, err := resolveLoginProduct(namespace, descriptor, productSpec{}, contexts.Login{})
		return nil, err
	}
	flags.url, err = s.askURL("Thunder URL", "")
	if err != nil {
		return nil, err
	}
	// Every Thunder deployment registers its own application, so the
	// client ID is always asked, offering the one the descriptor names.
	if flags.clientID == "" {
		flags.clientID, err = s.ask("Client ID of the registered OAuth application", descriptor.ClientID, nil)
		if errors.Is(err, wizard.ErrNoAnswer) {
			return nil, noAnswer("the client ID")
		}
		if err != nil {
			return nil, err
		}
	}
	if flags.audience == "" && descriptor.AudienceFor(flags.clientID) == "" {
		flags.audience, err = s.askRequired("Audience (the resource server access is bound to)",
			"the audience")
		if err != nil {
			return nil, err
		}
	}
	return descriptor, nil
}

// askSignIn asks how a person or a job signs in to the context, unless the
// flags already said.
func (s Shell) askSignIn(flags *contextCreateFlags, descriptor *modules.ProductDescriptor) error {
	if flags.device || flags.clientSecretVariable != "" {
		return nil
	}
	machine := wizard.Option{Label: "Client credentials (CI)"}
	if !descriptor.AllowsMachine(modules.MachineInline) {
		machine.Unavailable = fmt.Sprintf("The %s product does not accept a machine client at its own "+
			"issuer. Choose a browser or device sign-in.", flags.loginProduct)
	}
	device := wizard.Option{Label: "Device code"}
	picked, err := s.choose("Sign in using:", []wizard.Option{
		signInBrowser: {Label: "Browser"},
		signInDevice:  device,
		signInMachine: machine,
	}, signInBrowser)
	if err != nil {
		return err
	}
	switch picked {
	case signInDevice:
		flags.device = true
	case signInMachine:
		flags.clientSecretVariable, err = s.ask("Environment variable holding the client secret", "",
			func(answer string) error {
				if !contexts.ValidVariable(answer) {
					return wizard.Hint("Enter the variable's name, such as WSO2_CLIENT_SECRET, not the secret.")
				}
				return nil
			})
		if errors.Is(err, wizard.ErrNoAnswer) {
			return noAnswer("the environment variable")
		}
		return err
	}
	return nil
}

// productSummaries is each product's one-line summary as the root help page
// names it, by namespace.
func (s Shell) productSummaries() map[string]string {
	products, _ := s.helpProducts()
	summaries := make(map[string]string, len(products))
	for _, product := range products {
		summaries[product.namespace] = product.summary
	}
	return summaries
}

// productLabel names a product in a picker: its namespace, then its summary
// when it has one.
func productLabel(namespace, summary string) string {
	if summary == "" {
		return namespace
	}
	return namespace + " — " + summary
}

// askProducts offers the installed products the context does not log in
// through, one at a time, until the person is done.
func (s Shell) askProducts(installed []string, lookup descriptorLookup, loginProduct string) ([]productAnswer, error) {
	remaining := reachable(installed, lookup, nil)
	summaries := s.productSummaries()
	var answers []productAnswer
	for len(remaining) > 0 {
		// The installed products first, the first as the default, and a way
		// to stop last.
		options := make([]wizard.Option, 0, len(remaining)+1)
		for _, namespace := range remaining {
			options = append(options, wizard.Option{Label: productLabel(namespace, summaries[namespace])})
		}
		options = append(options, wizard.Option{Label: "Skip"})
		title := "Add a product this context reaches:"
		if len(answers) > 0 {
			title = "Add another product:"
		}
		picked, err := s.choose(title, options, 0)
		if err != nil {
			return nil, err
		}
		if picked == len(remaining) {
			break
		}
		namespace := remaining[picked]
		answer, err := s.askProduct(namespace, lookup(namespace))
		if err != nil {
			return nil, err
		}
		answers = append(answers, answer)
		remaining = slices.Delete(remaining, picked, picked+1)
	}
	return answers, nil
}

// reachable is the installed products a context can be told it reaches: not
// one it records already, and not a login provider, which serves only the
// context that logs in through it and is recorded when that context is
// created.
func reachable(installed []string, lookup descriptorLookup, recorded map[string]contexts.Product) []string {
	return slices.DeleteFunc(slices.Clone(installed), func(namespace string) bool {
		_, found := recorded[namespace]
		return found || lookup(namespace).LoginProvider()
	})
}

// askProduct asks where one product runs, and where its gateway runs when it
// has one.
func (s Shell) askProduct(namespace string, descriptor *modules.ProductDescriptor) (productAnswer, error) {
	answer := productAnswer{namespace: namespace}
	var err error
	if answer.url, err = s.askURL(fmt.Sprintf("%s URL", namespace), ""); err != nil {
		return answer, err
	}
	if descriptor != nil && descriptor.ClientID == "" && descriptor.Grant != "" &&
		descriptor.Grant != contexts.GrantExchange {
		// Reached through its own issuer, with a client only the deployment
		// knows.
		answer.clientID, err = s.askRequired(
			fmt.Sprintf("Client ID %s's deployment registered for this CLI", namespace), "the client ID")
		if err != nil {
			return answer, err
		}
	}
	if descriptor == nil || descriptor.Gateway == nil {
		return answer, nil
	}
	answer.gateway, err = s.ask(fmt.Sprintf("%s gateway URL (empty for none)", namespace), "",
		func(typed string) error {
			if typed == "" {
				return nil
			}
			return urlRefusal(typed)
		})
	if errors.Is(err, wizard.ErrNoAnswer) {
		return answer, nil
	}
	return answer, err
}

// askURL reads a product URL, asking again until it is one.
func (s Shell) askURL(title, fallback string) (string, error) {
	answer, err := s.ask(title, fallback, urlRefusal)
	if errors.Is(err, wizard.ErrNoAnswer) {
		return "", noAnswer("the URL")
	}
	if err != nil {
		return "", err
	}
	return strings.TrimRight(answer, "/"), nil
}

// urlRefusal is why an answer is not a product URL, in words for the
// prompt, or nil. Like productURL, it never repeats the value.
func urlRefusal(answer string) error {
	if _, err := productURL("URL", answer); err != nil {
		return wizard.Hint("Enter an absolute https URL with no user name or password, as in " +
			"https://localhost:9443. Plain http is accepted only on localhost, 127.0.0.1 or ::1.")
	}
	return nil
}

// askRequired reads a value that has no default.
func (s Shell) askRequired(title, what string) (string, error) {
	answer, err := s.ask(title, "", nil)
	if errors.Is(err, wizard.ErrNoAnswer) {
		return "", noAnswer(what)
	}
	return answer, err
}

// noAnswer refuses a wizard whose input ended before a required answer.
func noAnswer(what string) problem.Problem {
	return problem.New(problem.CategoryUsage, "shell.missing_required_flag",
		fmt.Sprintf("input ended before %s was entered", what)).
		WithRecovery("Nothing was written. " + contextCreateUsage)
}

// notCreated is a wizard the person declined at its summary.
func notCreated() problem.Problem {
	return problem.New(problem.CategoryUsage, "shell.cancelled", "the context was not created").
		WithRecovery("Nothing was written. Run wso2 context create again to start over.")
}

// summarize prints what the wizard is about to create, on standard error
// with the questions.
func (s Shell) summarize(answers contextAnswers) error {
	flags := answers.flags
	pairs := [][2]string{{"Context", answers.name}, {"Login provider", "Thunder"},
		{"Login product", flags.loginProduct}, {"URL", flags.url}}
	if flags.clientID != "" {
		pairs = append(pairs, [2]string{"Client ID", flags.clientID})
	}
	if flags.audience != "" {
		pairs = append(pairs, [2]string{"Audience", flags.audience})
	}
	signIn := "browser"
	switch {
	case flags.device:
		signIn = "device code"
	case flags.clientSecretVariable != "":
		signIn = "client credentials from $" + flags.clientSecretVariable
	}
	pairs = append(pairs, [2]string{"Sign in", signIn})
	for _, product := range answers.products {
		value := product.url
		if product.gateway != "" {
			value += " (gateway " + product.gateway + ")"
		}
		pairs = append(pairs, [2]string{"Product " + product.namespace, value})
	}
	if _, err := fmt.Fprintln(s.Streams.Err); err != nil {
		return err
	}
	if err := output.Fields(s.Streams.Err, pairs); err != nil {
		return err
	}
	_, err := fmt.Fprintln(s.Streams.Err)
	return err
}

// contextCreateWizard runs the create wizard for wso2 context create, then
// logs in when the person asked to.
func (s Shell) contextCreateWizard(command *cobra.Command, name string, flags contextCreateFlags) error {
	mode, err := s.shellOutputMode(command)
	if err != nil {
		return err
	}
	answers, err := s.setUpContext(command, name, flags, mode != output.ModeJSON, true)
	if err != nil || !answers.login {
		return err
	}
	if _, err := fmt.Fprintln(s.Streams.Out); err != nil {
		return err
	}
	return s.login(loginFlags{command: command, contextName: answers.name})
}

// setUpContext asks the create wizard's questions and does what they
// gathered: writes the context and records its products. It returns the
// answers, so the caller can log in to the context.
func (s Shell) setUpContext(command *cobra.Command, name string, flags contextCreateFlags,
	offerMore, offerLogin bool) (contextAnswers, error) {
	root, err := s.stateRoot()
	if err != nil {
		return contextAnswers{}, err
	}
	if err := contexts.Writable(root); err != nil {
		return contextAnswers{}, s.explainWriteRefusal(root, err)
	}
	document, err := contexts.Load(root)
	if err != nil {
		return contextAnswers{}, err
	}
	if name != "" {
		// Refused before any question, as the flag form refuses them.
		if err := refuseContextName(name); err != nil {
			return contextAnswers{}, err
		}
		if declaresContext(document, name) {
			return contextAnswers{}, contextExists(name)
		}
	}
	answers, err := s.askContextCreate(document, name, flags, offerMore, offerLogin)
	if err != nil {
		return answers, err
	}
	if err := s.contextCreate(command, answers.name, answers.flags); err != nil {
		return answers, err
	}
	lines := []string{contextCreateLine(answers.name, answers.flags)}
	for _, product := range answers.products {
		productFlags := contextProductFlags{url: product.url, gateway: product.gateway, clientID: product.clientID,
			context: answers.name, noInstall: flags.noInstall}
		if err := s.contextProductAdd(command, product.namespace, productFlags); err != nil {
			return answers, err
		}
		lines = append(lines, productAddLine(product.namespace, answers.name, productFlags))
	}
	return answers, s.printFlagForm(lines...)
}

// askProductAdd asks which product to add, and where it runs, for a
// wso2 context product add that named neither.
func (s Shell) askProductAdd(namespace string, recorded map[string]contexts.Product) (productAnswer, error) {
	installed, lookup := s.wizardProducts()
	if namespace == "" {
		installed = reachable(installed, lookup, recorded)
		if len(installed) == 0 {
			return productAnswer{}, problem.New(problem.CategoryUsage, "shell.missing_argument",
				"no installed product is left to add, so wso2 context product add needs one named").
				WithRecovery(contextProductAddUsage)
		}
		options := make([]wizard.Option, len(installed))
		for index, candidate := range installed {
			options[index] = wizard.Option{Label: candidate}
		}
		picked, err := s.choose("Product:", options, 0)
		if err != nil {
			return productAnswer{}, err
		}
		namespace = installed[picked]
	}
	return s.askProduct(namespace, lookup(namespace))
}

// contextProductAddWizard asks for the product and where it runs, shows what
// adding it would change, and adds it once the person agrees.
func (s Shell) contextProductAddWizard(command *cobra.Command, namespace string, flags contextProductFlags) error {
	mode, err := s.shellOutputMode(command)
	if err != nil {
		return err
	}
	var recorded map[string]contexts.Product
	if !flags.replace {
		recorded, err = s.recordedProducts(command, flags)
		if err != nil {
			return err
		}
	}
	answer, err := s.askProductAdd(namespace, recorded)
	if err != nil {
		return err
	}
	flags.url = answer.url
	if flags.gateway == "" {
		flags.gateway = answer.gateway
	}
	if flags.clientID == "" {
		flags.clientID = answer.clientID
	}
	if mode == output.ModeJSON || flags.dryRun {
		// One result for a JSON caller, and a dry run is its own preview.
		return s.contextProductAdd(command, answer.namespace, flags)
	}
	preview := flags
	preview.dryRun = true
	if err := s.contextProductAdd(command, answer.namespace, preview); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(s.Streams.Err); err != nil {
		return err
	}
	write, err := s.prompter().Confirm(fmt.Sprintf("Add the %s product?", answer.namespace), true)
	if err := cancelled(err); err != nil {
		return err
	}
	if !write {
		return problem.New(problem.CategoryUsage, "shell.cancelled", "the product was not added").
			WithRecovery("Nothing was written. Run wso2 context product add again to start over.")
	}
	target, err := s.productTarget(command, flags)
	if err != nil {
		return err
	}
	if err := s.contextProductAdd(command, answer.namespace, flags); err != nil {
		return err
	}
	return s.printFlagForm(productAddLine(answer.namespace, target.Name, flags))
}

// recordedProducts is what the context product add targets records already.
func (s Shell) recordedProducts(command *cobra.Command, flags contextProductFlags) (map[string]contexts.Product, error) {
	target, err := s.productTarget(command, flags)
	return target.Products, err
}

// targetsWSO2Cloud reports whether a product subcommand acts on a WSO2 Cloud
// context. A context it cannot find is not one; the command refuses it later.
func (s Shell) targetsWSO2Cloud(command *cobra.Command, flags contextProductFlags) bool {
	target, err := s.productTarget(command, flags)
	return err == nil && target.WSO2Cloud()
}

// productTarget is the context a product subcommand acts on, as written.
func (s Shell) productTarget(command *cobra.Command, flags contextProductFlags) (contexts.Context, error) {
	root, err := s.stateRoot()
	if err != nil {
		return contexts.Context{}, err
	}
	document, err := contexts.Load(root)
	if err != nil {
		return contexts.Context{}, err
	}
	name := flags.context
	if name == "" {
		if name, err = targetContextName(command, document); err != nil {
			return contexts.Context{}, err
		}
	}
	target, _ := document.Find(name)
	return target, nil
}
