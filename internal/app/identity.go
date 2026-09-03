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
	"maps"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// The way back from each subcommand's usage refusals.
const (
	identityAddProductUsage = "Run wso2 identity add-product <identity> <namespace> " +
		"--endpoint <url> [--audience <resource-id>] [--scopes <list>] [--replace]."
	identityListUsage = "Run wso2 identity list [--output table|json]."
)

// identityRecovery is what every refusal from the identity command itself,
// rather than one of its subcommands, points a user at.
const identityRecovery = "Run wso2 identity list to see what login recorded, or " +
	"wso2 identity add-product to record what a self-hosted deployment reaches. " +
	"Logging in is what creates an identity."

// identityCommand builds the wso2 identity tree.
//
// There is no create subcommand. Logging in is the only thing that creates an
// identity (#112 D3), and this family only modifies and reads what login
// already wrote, which is why adding it does not reopen that decision.
func (s Shell) identityCommand() *cobra.Command {
	command := &cobra.Command{
		Use:                   "identity <subcommand>",
		Short:                 "Record and inspect what an identity reaches.",
		Long:                  identityRecovery,
		DisableFlagsInUseLine: true,
		// A RunE is declared because Cobra validates a non-leaf command's
		// arguments only when it is Runnable: leave it nil and wso2 identity
		// bogus prints help and exits 0, reporting a typo as success to
		// whatever ran it. Never cobra.NoArgs or cobra.ExactArgs for this —
		// both bypass the flag-error hook and exit 70 instead of 64.
		//
		// A bare wso2 identity is the other arm, and is deliberately not a
		// refusal. See helpForBareFamily.
		RunE: func(command *cobra.Command, args []string) error {
			if len(args) == 0 {
				return helpForBareFamily(command)
			}
			return problem.New(problem.CategoryUsage, "shell.unknown_command",
				fmt.Sprintf("%q is not a wso2 identity subcommand", args[0])).
				WithRecovery(identityRecovery)
		},
	}
	// The family renders a machine-readable result, and takes no --context: an
	// identity is named by this family's own arguments, and a selection flag
	// alongside "wso2 identity list" would be a second answer to a question
	// nothing asked.
	declareOutputFlag(command.PersistentFlags())
	command.AddCommand(s.identityAddProductCommand(), s.identityListCommand())
	return command
}

func (s Shell) identityAddProductCommand() *cobra.Command {
	var endpoint, audience string
	var scopes []string
	var replace bool
	command := &cobra.Command{
		Use:   "add-product <identity> <namespace>",
		Short: "Record a product endpoint a self-hosted deployment cannot advertise.",
		Args: exactlyTwoArguments("an identity and a product namespace",
			identityAddProductUsage),
		RunE: func(command *cobra.Command, args []string) error {
			return s.identityAddProduct(command, args[0], args[1],
				contexts.Product{Endpoint: endpoint, Audience: audience, Scopes: scopes},
				replace)
		},
	}
	command.Flags().StringVar(&endpoint, "endpoint", "",
		"The product service's base URL.")
	command.Flags().StringVar(&audience, "audience", "",
		"The token audience the product's services accept.")
	// StringSliceVar splits on commas, which is the shape the walkthrough shows:
	// --scopes api:read,api:write.
	command.Flags().StringSliceVar(&scopes, "scopes", nil,
		"The permissions the shell may request for this product, comma-separated.")
	command.Flags().BoolVar(&replace, "replace", false,
		"Replace the namespace's existing record instead of refusing.")
	return command
}

func (s Shell) identityListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the identities and the products each one reaches.",
		Args:  noArguments(identityListUsage),
		RunE: func(command *cobra.Command, args []string) error {
			return s.identityList(command)
		},
	}
}

// identityAddProduct records one product under an identity login already wrote.
//
// It creates no identity and no context, and it makes no network call: listing
// a product is the operator's assertion that the login's session reaches it,
// and the shell does not verify that assertion here. A wrong one surfaces as a
// typed authentication failure at the first command that needs the product
// (#112 D8, and docs/examples/login-walkthroughs.md B.3), which is where the
// deployment is being talked to anyway. Verifying here would instead make
// recording an endpoint depend on the deployment being up.
func (s Shell) identityAddProduct(
	command *cobra.Command, identity, namespace string, product contexts.Product, replace bool,
) error {
	mode, err := s.shellOutputMode(command)
	if err != nil {
		return err
	}
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	if product.Endpoint == "" {
		// Checked here rather than with Cobra's MarkFlagRequired, whose error
		// never reaches the flag-error hook and would exit outside the
		// documented classes. Left to the document, the same omission arrives
		// as contexts.document_malformed, which tells a user their file is
		// wrong over a flag they simply did not type.
		return problem.New(problem.CategoryUsage, "shell.missing_required_flag",
			"wso2 identity add-product needs the endpoint the product is served at").
			WithRecovery(identityAddProductUsage + " A self-hosted deployment publishes no " +
				"catalogue of what it serves, so the endpoint can only come from you.")
	}
	// Checked before the document is opened, so that a namespace the user
	// mistyped is refused as the argument it is. contexts.ValidName is the same
	// pattern Identity.validate holds a product namespace to, so this cannot
	// disagree with the document about what is legal; what it changes is who
	// the complaint is about. Left to the document, the same mistake arrives as
	// contexts.document_malformed, which offers to remove a file the user did
	// not write and that this command never reached.
	if !contexts.ValidName(namespace) {
		return problem.New(problem.CategoryUsage, "shell.invalid_argument",
			fmt.Sprintf("%q cannot be used as a product namespace", namespace)).
			WithRecovery(fmt.Sprintf("A product namespace is %s. %s",
				contexts.NameRule, identityAddProductUsage))
	}

	// The endpoint and audience are deliberately absent from this record. Both
	// are the flag values most likely to have had a credential typed into them
	// by mistake — that is why internal/contexts refuses a URL embedding user
	// information and never echoes the value it refused — and output redaction
	// is key-based, so it would not catch one here. The identity, namespace and
	// scopes are names.
	s.log.Debug("recording a product under an identity",
		"identity", identity, "namespace", namespace,
		"scopes", strings.Join(product.Scopes, ","), "replace", replace,
		"document", contexts.Path(root))

	added := productAdded{
		Identity:  identity,
		Namespace: namespace,
		Endpoint:  product.Endpoint,
		Audience:  product.Audience,
		Scopes:    product.Scopes,
	}
	// changed records that the update reached the point of returning a modified
	// document. Everything Update refuses after that is a refusal of what this
	// command just built, and nothing before it is; that is what lets the
	// refusal be reworded honestly. See explainProductRefusal.
	changed := false
	// uncorrectable records that the identity is bound to one protected
	// resource and the change would leave it holding more than one product.
	// Such a command cannot be made to succeed by correcting a flag, whichever
	// of the document's checks refuses it first, so the ordinary "correct it
	// and run it again" would be false. It is read from the identity's own
	// Derivation rather than reasoned about here: this decides wording, never
	// whether to refuse, which stays entirely the document's.
	uncorrectable := false
	err = contexts.Update(root, func(document contexts.Document) (contexts.Document, error) {
		position := slices.IndexFunc(document.Identities, func(candidate contexts.Identity) bool {
			return candidate.Name == identity
		})
		if position < 0 {
			return document, unknownIdentity(identity, len(document.Identities) > 0)
		}
		declared := document.Identities[position]
		_, carried := declared.Products[namespace]
		if carried && !replace {
			return document, productExists(identity, namespace)
		}
		added.Replaced = carried
		// The map is copied rather than mutated in place. Load returns the
		// document by value but the map header inside it is shared, so writing
		// through it would edit the caller's document before Update had
		// accepted the result.
		products := maps.Clone(declared.Products)
		if products == nil {
			products = map[string]contexts.Product{}
		}
		// Assigned whole, so a replacement replaces the record rather than
		// merging with it: an audience or a scope left over from the record
		// being replaced would be a permission nobody asked for.
		products[namespace] = product
		declared.Products = products
		document.Identities[position] = declared
		uncorrectable = declared.Auth.Derivation() == contexts.DerivationTokenResource &&
			len(products) > 1
		changed = true
		return document, nil
	})
	if err != nil {
		return s.explainProductRefusal(root, changed, uncorrectable, err)
	}

	if mode == output.ModeJSON {
		return renderContext(s.Streams.Out, mode, added)
	}
	// "to" for the ordinary case and "on" for the replacement, because the two
	// are worth telling apart at a glance: one added something that was not
	// there and the other overwrote something that was.
	line := fmt.Sprintf("Added product %q to identity %q.", namespace, identity)
	if added.Replaced {
		line = fmt.Sprintf("Replaced product %q on identity %q.", namespace, identity)
	}
	if _, err := fmt.Fprintf(s.Streams.Out, "\n%s\n", line); err != nil {
		return err
	}
	return renderContext(s.Streams.Out, mode, added)
}

// identityList reports every identity and what it reaches.
func (s Shell) identityList(command *cobra.Command) error {
	mode, err := s.shellOutputMode(command)
	if err != nil {
		return err
	}
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	document, err := contexts.Load(root)
	if err != nil {
		return err
	}

	listing := identityListing{Identities: make([]identityEntry, 0, len(document.Identities))}
	withoutProducts := 0
	for _, declared := range document.Identities {
		entry := identityEntry{
			Name:   declared.Name,
			Type:   declared.Type,
			Kind:   declared.Auth.Kind,
			Issuer: declared.Auth.Issuer,
			// The credential reference is deliberately absent, here and from
			// the table. It is a name rather than a credential, so publishing
			// it would grant nobody anything, but it is the name of where a
			// credential lives and nothing a reader of this listing does needs
			// it. Nothing from the secure store is read at all.
			Products: make([]productEntry, 0, len(declared.Products)),
		}
		// Sorted, so two runs against one document render the same rows in the
		// same order: the map's iteration order is not one.
		for _, namespace := range slices.Sorted(maps.Keys(declared.Products)) {
			product := declared.Products[namespace]
			entry.Products = append(entry.Products, productEntry{
				Namespace: namespace,
				Endpoint:  product.Endpoint,
				Audience:  product.Audience,
				Scopes:    product.Scopes,
			})
		}
		if len(entry.Products) == 0 {
			withoutProducts++
		}
		listing.Identities = append(listing.Identities, entry)
	}

	if mode == output.ModeJSON {
		return encodeContextJSON(s.Streams.Out, listing)
	}
	// An unconfigured machine is a state, not a breakage, so it reports what to
	// run rather than that nothing is there. Logging in is the only thing that
	// creates an identity (#112 D3), so nothing else could be named here.
	if len(listing.Identities) == 0 {
		_, err := fmt.Fprintln(s.Streams.Out, "No identities are configured.\n\n"+
			"Run wso2 login --url <issuer> --client-id <id> to create one.")
		return err
	}
	table := output.NewTable("identity", "type", "issuer", "product", "endpoint", "scopes")
	for _, entry := range listing.Identities {
		if len(entry.Products) == 0 {
			table.Append(entry.Name, entry.Type, entry.Issuer, "", "", "")
			continue
		}
		// One row per product, and the identity's own columns repeated on each:
		// what a reader of this table wants is the pair, and a blank identity
		// column on the second row would leave them counting upward to find it.
		for _, product := range entry.Products {
			table.Append(entry.Name, entry.Type, entry.Issuer,
				product.Namespace, product.Endpoint, strings.Join(product.Scopes, ","))
		}
	}
	if err := table.Render(s.Streams.Out); err != nil {
		return err
	}
	// Said only when there is something to say. An identity with no products is
	// exactly where a self-hosted first run stops, and nothing else in the
	// output names the command that carries on from there.
	if withoutProducts > 0 {
		_, err = fmt.Fprintf(s.Streams.Out,
			"\n%s\n", identityAddProductUsage)
		return err
	}
	return nil
}

// exactlyTwoArguments refuses a wrong argument count as the usage failure it is,
// for the reason exactlyOneArgument states: cobra.ExactArgs would report it
// outside the shell's exit classes.
func exactlyTwoArguments(what, usage string) cobra.PositionalArgs {
	return func(command *cobra.Command, args []string) error {
		switch {
		case len(args) < 2:
			return problem.New(problem.CategoryUsage, "shell.missing_argument",
				fmt.Sprintf("%s needs %s, got %d", command.CommandPath(), what, len(args))).
				WithRecovery(usage)
		case len(args) > 2:
			return problem.New(problem.CategoryUsage, "shell.unexpected_argument",
				fmt.Sprintf("%s takes two arguments, got %d", command.CommandPath(), len(args))).
				WithRecovery(usage)
		}
		return nil
	}
}

// The results this family reports. They are rendered the way the context family
// renders its own; see the comment on that family's result types.
type (
	// productAdded is what wso2 identity add-product reports.
	productAdded struct {
		Identity  string   `json:"identity"`
		Namespace string   `json:"namespace"`
		Endpoint  string   `json:"endpoint"`
		Audience  string   `json:"audience"`
		Scopes    []string `json:"scopes"`
		// Replaced reports that a record for this namespace was overwritten,
		// which happens only under --replace.
		Replaced bool `json:"replaced"`
	}

	// productEntry is one product an identity reaches.
	productEntry struct {
		Namespace string   `json:"namespace"`
		Endpoint  string   `json:"endpoint"`
		Audience  string   `json:"audience"`
		Scopes    []string `json:"scopes"`
	}

	// identityEntry is one row group of the listing.
	identityEntry struct {
		Name     string         `json:"name"`
		Type     string         `json:"type"`
		Kind     string         `json:"kind"`
		Issuer   string         `json:"issuer"`
		Products []productEntry `json:"products"`
	}

	// identityListing is what wso2 identity list reports.
	identityListing struct {
		Identities []identityEntry `json:"identities"`
	}
)

func (p productAdded) fields() [][2]string {
	return [][2]string{
		{"Identity", p.Identity},
		{"Product", p.Namespace},
		{"Endpoint", p.Endpoint},
		{"Audience", p.Audience},
		{"Scopes", strings.Join(p.Scopes, ",")},
		{"Replaced", yesNo(p.Replaced)},
	}
}

// productExists refuses to overwrite a product record without being asked to.
//
// Overwriting silently would be the one thing a user cannot undo: the endpoint,
// audience and scopes the record held are not written down anywhere else, and
// the ordinary way to reach this refusal is a second add-product run from shell
// history with one flag corrected. --replace is how that user says they meant
// it.
func productExists(identity, namespace string) problem.Problem {
	return problem.New(problem.CategoryUsage, "contexts.product_exists",
		fmt.Sprintf("the identity %q already records a product in the %q namespace",
			identity, namespace)).
		WithRecovery("Run wso2 identity list to see what it records. " +
			"Pass --replace to overwrite it, which replaces the whole record.")
}

// explainProductRefusal replaces a document refusal's generic recovery with one
// that fits a write that never happened.
//
// The document's own validation is what refuses an endpoint that embeds user
// information, an endpoint no URL parser reads, and a product a resource-bound
// identity cannot carry. Those checks are not repeated in this package: a
// second copy is how the two come to disagree. Neither is the message touched,
// which is also what keeps the rejected endpoint unechoed — Product.validate
// deliberately never repeats it, and neither does anything here.
//
// Only the recovery is replaced, and only when it is the generic one. Several
// of these refusals carry a sentence naming the exact thing to change; the
// endpoint-embeds-credentials refusal is the one that matters most, and
// overwriting its advice with anything generic would make the refusal worse
// exactly where it counts. The problem code cannot tell the two apart, because
// both are contexts.document_malformed, so the question asked is whether the
// recovery is the default one: contexts.CarriesDefaultDocumentRecovery.
//
// What makes the default one wrong here is that it offers to remove a document
// this command never wrote to. Following it would destroy the identity the user
// just logged in as.
//
// Whether the refusal is about the change is answered by changed, not by
// reading the message. contexts.Update loads the document, calls the change,
// and only then encodes and decodes the result, so a refusal raised before the
// change returned cannot be about the change, and one raised after it can be
// about nothing else.
func (s Shell) explainProductRefusal(stateRoot string, changed, uncorrectable bool, err error) error {
	err = s.explainWriteRefusal(stateRoot, err)
	var typed problem.Problem
	if !changed || !errors.As(err, &typed) || !contexts.CarriesDefaultDocumentRecovery(err) {
		return err
	}
	return problem.New(problem.CategoryUsage, "shell.invalid_argument", typed.Message).
		WithRecovery(productRefusalRecovery(uncorrectable))
}

// productRefusalRecovery is the way out of a refused product record.
//
// The resource-bound case gets its own, because the ordinary advice would be
// false there: the constraint is on the identity rather than on any flag, so no
// correction of the command that was typed succeeds. What does succeed is named
// instead. Both routes have been driven: --replace on the product such an
// identity already holds is accepted, and a second login under another name
// produces a second identity for the other product.
//
// This lives here rather than beside the check in internal/contexts because it
// is advice about commands. The same refusal reaches a plain reader — wso2
// context list, loading a document already holding two products under such an
// identity — and telling that reader to pass --replace to a command they did
// not run would be nonsense.
func productRefusalRecovery(uncorrectable bool) string {
	if uncorrectable {
		return "The context document was not changed, and correcting the flags will not " +
			"help: this identity is bound to one protected resource, so it carries one " +
			"product and no more. Pass --replace to record this product in place of the " +
			"one it holds, or run wso2 login --url <issuer> --context <name> to create a " +
			"second identity for the other product. Run wso2 identity list to see what " +
			"each identity records."
	}
	return "The context document was not changed. Run wso2 identity list to see what the " +
		"identity records, then correct the command and run it again. " +
		identityAddProductUsage
}
