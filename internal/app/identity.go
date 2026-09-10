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
	identityAddProductUsage = "Run wso2 account add-product <account> <namespace> " +
		"--endpoint <url> [--audience <resource-id>] [--scopes <list>] [--replace]."
	identityListUsage = "Run wso2 account list [--output table|json]."
)

// identityRecovery is what every refusal from the identity command itself,
// rather than one of its subcommands, points a user at.
const identityRecovery = "Run wso2 account list to see what login recorded, or " +
	"wso2 account add-product to record what a self-hosted deployment reaches. " +
	"Logging in is what creates an account."

// accountCommand builds the wso2 account tree.
//
// There is no create subcommand. Logging in is the only thing that creates an
// identity (#112 D3), and this family only modifies and reads what login
// already wrote, which is why adding it does not reopen that decision.
//
// remove-product is the one member that reaches the network, and only to end
// the sessions a removal would otherwise strand in the secure store; see
// identityRemoveProduct.
func (s Shell) accountCommand() *cobra.Command {
	command := &cobra.Command{
		Use:                   "account <subcommand>",
		Short:                 "Record and inspect what an account reaches.",
		Long:                  identityRecovery,
		DisableFlagsInUseLine: true,
		// A RunE is declared because Cobra validates a non-leaf command's
		// arguments only when it is Runnable: leave it nil and wso2 identity
		// bogus prints help and exits 0, reporting a typo as success to
		// whatever ran it. Never cobra.NoArgs or cobra.ExactArgs for this —
		// both bypass the flag-error hook and exit 70 instead of 64.
		//
		// A bare wso2 account is the other arm, and is deliberately not a
		// refusal. See helpForBareFamily.
		RunE: func(command *cobra.Command, args []string) error {
			if len(args) == 0 {
				return helpForBareFamily(command)
			}
			return problem.New(problem.CategoryUsage, "shell.unknown_command",
				fmt.Sprintf("%q is not a wso2 account subcommand", args[0])).
				WithRecovery(identityRecovery)
		},
	}
	// The family renders a machine-readable result, and takes no --context: an
	// identity is named by this family's own arguments, and a selection flag
	// alongside "wso2 account list" would be a second answer to a question
	// nothing asked.
	declareOutputFlag(command.PersistentFlags())
	command.AddCommand(s.identityCreateCommand(), s.identityAddProductCommand(),
		s.identityRemoveProductCommand(), s.identityListCommand())
	return command
}

func (s Shell) identityAddProductCommand() *cobra.Command {
	var endpoint, audience string
	var scopes []string
	var replace bool
	var grant grantFlags
	command := &cobra.Command{
		Use:   "add-product <account> <namespace>",
		Short: "Record a product endpoint a self-hosted deployment cannot advertise.",
		Args: exactlyTwoArguments("an account and a product namespace",
			identityAddProductUsage),
		RunE: func(command *cobra.Command, args []string) error {
			product := contexts.Product{Endpoint: endpoint, Audience: audience, Scopes: scopes}
			derived, err := grant.product()
			if err != nil {
				return err
			}
			product.Grant = derived
			return s.identityAddProduct(command, args[0], args[1], product, replace)
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
	command.Flags().StringVar(&grant.kind, "grant", "",
		"How access for this product is derived when its issuer is not the account's: "+
			contexts.GrantJWTBearer+" presents an identity token from the login session; "+
			contexts.GrantFederated+" signs in at the product's own issuer as the named client; "+
			contexts.GrantExchange+" exchanges the login session for the product's audience, with "+
			"no authorization of its own.")
	command.Flags().StringVar(&grant.issuer, "grant-issuer", "",
		"The product's own OpenID issuer, whose token endpoint takes the assertion.")
	command.Flags().StringVar(&grant.clientID, "grant-client-id", "",
		"The public client the shell presents at the grant issuer.")
	command.Flags().StringSliceVar(&grant.scopes, "grant-scopes", nil,
		"The scopes the login session is refreshed with for the assertion, comma-separated; "+
			"openid is always among them.")
	command.Flags().StringVar(&grant.resource, "grant-resource", "",
		"The resource indicator the grant's session is authorized under, when its issuer requires one.")
	return command
}

// grantFlags are the flags that describe a derived product. They are
// legal only together: a grant is one arrangement, and half of one names
// nothing the broker could carry out.
type grantFlags struct {
	kind, issuer, clientID, resource string
	scopes                           []string
}

// product turns the flags into the grant a product records, or nil when none
// was given.
func (g grantFlags) product() (*contexts.Grant, error) {
	if g.kind == "" && g.issuer == "" && g.clientID == "" && len(g.scopes) == 0 && g.resource == "" {
		return nil, nil
	}
	if g.kind == "" {
		return nil, problem.New(problem.CategoryUsage, "shell.missing_required_flag",
			"wso2 account add-product needs --grant with --grant-issuer and --grant-client-id").
			WithRecovery("Pass --grant " + contexts.GrantJWTBearer + " or --grant " + contexts.GrantFederated +
				" to derive this product's access. " + identityAddProductUsage)
	}
	if g.kind != contexts.GrantJWTBearer && g.kind != contexts.GrantFederated &&
		g.kind != contexts.GrantExchange {
		return nil, problem.New(problem.CategoryUsage, "shell.invalid_argument",
			fmt.Sprintf("%q is not a grant this shell implements", g.kind)).
			WithRecovery("Pass --grant " + contexts.GrantJWTBearer + ", --grant " + contexts.GrantFederated +
				" or --grant " + contexts.GrantExchange + ". " + identityAddProductUsage)
	}
	if g.kind == contexts.GrantExchange {
		// An exchange runs at the account's own issuer as its own client, and
		// asks for the product's registered audience. There is nothing left
		// for these flags to name, so naming one is a mistake worth catching
		// here rather than a value to accept and ignore.
		if g.issuer != "" || g.clientID != "" {
			return nil, problem.New(problem.CategoryUsage, "shell.invalid_argument",
				"--grant-issuer and --grant-client-id do not belong to an exchange grant, which runs "+
					"at the account's own issuer as its own client").
				WithRecovery("Omit both flags. " + identityAddProductUsage)
		}
		if len(g.scopes) > 0 || g.resource != "" {
			return nil, problem.New(problem.CategoryUsage, "shell.invalid_argument",
				"--grant-scopes and --grant-resource do not belong to an exchange grant, which asks "+
					"for the product's own audience and carries the login session's scopes").
				WithRecovery("Omit both flags; --audience is what an exchange asks for. " +
					identityAddProductUsage)
		}
		return &contexts.Grant{Kind: g.kind}, nil
	}
	if g.issuer == "" || g.clientID == "" {
		return nil, problem.New(problem.CategoryUsage, "shell.missing_required_flag",
			"wso2 account add-product needs --grant-issuer and --grant-client-id with --grant").
			WithRecovery("Name the product's own issuer and the public client the shell presents " +
				"there. " + identityAddProductUsage)
	}
	if g.kind == contexts.GrantFederated && len(g.scopes) > 0 {
		return nil, problem.New(problem.CategoryUsage, "shell.invalid_argument",
			"--grant-scopes belongs to a jwt-bearer grant, not to federated").
			WithRecovery("Omit --grant-scopes or pass --grant " + contexts.GrantJWTBearer + ". " +
				identityAddProductUsage)
	}
	return &contexts.Grant{Kind: g.kind, Issuer: g.issuer, ClientID: g.clientID, Scopes: g.scopes, Resource: g.resource}, nil
}

// grantSummary states where a recorded grant runs and as whom.
//
// An exchange names neither an issuer nor a client, because it uses the
// identity's own, so the general form would render "exchange at  as " — two
// blanks that read as values the shell failed to load rather than as values
// that were never there to load.
func grantSummary(grant contexts.Grant) string {
	if grant.Kind == contexts.GrantExchange {
		return grant.Kind + " at the account's own issuer, as its own client"
	}
	return grant.Kind + " at " + grant.Issuer + " as " + grant.ClientID
}

func (s Shell) identityListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the accounts and the products each one reaches.",
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
			"wso2 account add-product needs the endpoint the product is served at").
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
	s.log.Debug("recording a product under an account",
		"identity", identity, "namespace", namespace,
		"scopes", strings.Join(product.Scopes, ","), "replace", replace,
		"document", contexts.Path(root))

	added := productAdded{
		Account:   identity,
		Namespace: namespace,
		Endpoint:  product.Endpoint,
		Audience:  product.Audience,
		Scopes:    product.Scopes,
		Grant:     product.Grant,
	}
	// changed records that the update reached the point of returning a modified
	// document. Everything Update refuses after that is a refusal of what this
	// command just built, and nothing before it is; that is what lets the
	// refusal be reworded honestly. See explainProductRefusal.
	changed := false
	err = contexts.Update(root, func(document contexts.Document) (contexts.Document, error) {
		position := slices.IndexFunc(document.Accounts, func(candidate contexts.Account) bool {
			return candidate.Name == identity
		})
		if position < 0 {
			return document, unknownIdentity(identity, len(document.Accounts) > 0)
		}
		declared := document.Accounts[position]
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
		document.Accounts[position] = declared
		changed = true
		return document, nil
	})
	if err != nil {
		return s.explainProductRefusal(root, changed, err)
	}

	if mode == output.ModeJSON {
		return renderContext(s.Streams.Out, mode, added)
	}
	// "to" for the ordinary case and "on" for the replacement, because the two
	// are worth telling apart at a glance: one added something that was not
	// there and the other overwrote something that was.
	line := fmt.Sprintf("Added product %q to account %q.", namespace, identity)
	if added.Replaced {
		line = fmt.Sprintf("Replaced product %q on account %q.", namespace, identity)
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

	listing := accountListing{Accounts: make([]accountEntry, 0, len(document.Accounts))}
	withoutProducts := 0
	for _, declared := range document.Accounts {
		entry := accountEntry{
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
				Grant:     product.Grant,
				Gateway:   product.Gateway,
			})
		}
		if len(entry.Products) == 0 {
			withoutProducts++
		}
		listing.Accounts = append(listing.Accounts, entry)
	}

	if mode == output.ModeJSON {
		return encodeContextJSON(s.Streams.Out, listing)
	}
	// An unconfigured machine is a state, not a breakage, so it reports what to
	// run rather than that nothing is there. Logging in is the only thing that
	// creates an identity (#112 D3), so nothing else could be named here.
	if len(listing.Accounts) == 0 {
		_, err := fmt.Fprintln(s.Streams.Out, "No accounts are configured.\n\n"+
			"Run wso2 login --url <issuer> --client-id <id> to create one.")
		return err
	}
	table := output.NewTable("account", "type", "issuer", "product", "endpoint", "scopes")
	for _, entry := range listing.Accounts {
		if len(entry.Products) == 0 {
			table.Append(entry.Name, entry.Type, entry.Issuer, "", "", "")
			continue
		}
		// One row per product, and the account's own columns repeated on each:
		// what a reader of this table wants is the pair, and a blank identity
		// column on the second row would leave them counting upward to find it.
		for _, product := range entry.Products {
			table.Append(entry.Name, entry.Type, entry.Issuer,
				product.Namespace, product.Endpoint, strings.Join(product.Scopes, ","))
			// The gateway record is a row of its own, under the product's
			// gateway key, so a reader sees both of what connect wrote.
			if product.Gateway != nil {
				table.Append(entry.Name, entry.Type, entry.Issuer, contexts.GatewayKey(product.Namespace),
					product.Gateway.Endpoint, strings.Join(product.Gateway.Scopes, ","))
			}
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
	// productAdded is what wso2 account add-product reports.
	productAdded struct {
		Account   string   `json:"account"`
		Namespace string   `json:"namespace"`
		Endpoint  string   `json:"endpoint"`
		Audience  string   `json:"audience"`
		Scopes    []string `json:"scopes"`
		// Grant is how the product is derived, absent when the session itself
		// answers for it.
		Grant *contexts.Grant `json:"grant,omitempty"`
		// Replaced reports that a record for this namespace was overwritten,
		// which happens only under --replace.
		Replaced bool `json:"replaced"`
	}

	// productEntry is one product an identity reaches, with its gateway
	// record when it holds one.
	productEntry struct {
		Namespace string            `json:"namespace"`
		Endpoint  string            `json:"endpoint"`
		Audience  string            `json:"audience"`
		Scopes    []string          `json:"scopes"`
		Grant     *contexts.Grant   `json:"grant,omitempty"`
		Gateway   *contexts.Gateway `json:"gateway,omitempty"`
	}

	// accountEntry is one row group of the listing.
	accountEntry struct {
		Name     string         `json:"name"`
		Type     string         `json:"type"`
		Kind     string         `json:"kind"`
		Issuer   string         `json:"issuer"`
		Products []productEntry `json:"products"`
	}

	// accountListing is what wso2 account list reports.
	accountListing struct {
		Accounts []accountEntry `json:"accounts"`
	}
)

func (p productAdded) fields() [][2]string {
	fields := [][2]string{
		{"Account", p.Account},
		{"Product", p.Namespace},
		{"Endpoint", p.Endpoint},
		{"Audience", p.Audience},
		{"Scopes", strings.Join(p.Scopes, ",")},
	}
	if p.Grant != nil {
		fields = append(fields,
			[2]string{"Grant", grantSummary(*p.Grant)})
	}
	return append(fields, [2]string{"Replaced", yesNo(p.Replaced)})
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
		fmt.Sprintf("the account %q already records a product in the %q namespace",
			identity, namespace)).
		WithRecovery("Run wso2 account list to see what it records. " +
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
func (s Shell) explainProductRefusal(stateRoot string, changed bool, err error) error {
	err = s.explainWriteRefusal(stateRoot, err)
	var typed problem.Problem
	if !changed || !errors.As(err, &typed) || !contexts.CarriesDefaultDocumentRecovery(err) {
		return err
	}
	return problem.New(problem.CategoryUsage, "shell.invalid_argument", typed.Message).
		WithRecovery(productRefusalRecovery())
}

// productRefusalRecovery is the way out of a refused product record.
//
// This lives here rather than beside the check in internal/contexts because it
// is advice about commands.
func productRefusalRecovery() string {
	return "The context document was not changed. Run wso2 account list to see what the " +
		"account records, then correct the command and run it again. " +
		identityAddProductUsage
}
