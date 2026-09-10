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
	"maps"
	"slices"
	"strings"

	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/result"
)

// The two records a product may hold on an identity, as the connect report
// names them: the management record connect writes from the URL alone, and
// the gateway record --gateway writes beside it.
const (
	recordManagement = "management"
	recordGateway    = contexts.GatewayRecord
)

// gatewayUsage is the way back from connect --gateway's usage refusals.
func gatewayUsage(namespace string) string {
	return fmt.Sprintf("Run wso2 %s connect <gateway-url> --gateway [--audience <value>] [--scopes <list>] "+
		"[--replace] [--account <name>] [--login-provider <issuer-url>].", namespace)
}

// checkGatewayFlags refuses a --gateway line the shell could not write from,
// before the document is opened.
//
// A gateway record is reached at the account's login provider as the
// identity's own client, so the flags that name another client — the one
// the management record presents at the product's own issuer — describe
// nothing a gateway record can hold.
func checkGatewayFlags(namespace string, descriptor modules.ProductDescriptor, flags connectFlags) error {
	usage := gatewayUsage(namespace)
	if descriptor.Gateway == nil {
		return problem.New(problem.CategoryUsage, "shell.connect_unsupported",
			fmt.Sprintf("the %s module declares no gateway shape, so the shell cannot record a gateway "+
				"for the %s product", namespace, namespace)).
			WithRecovery(fmt.Sprintf("Record the product itself with wso2 %s connect <url>, or install a "+
				"version of the module whose descriptor declares a gateway.", namespace))
	}
	if flags.clientIDSet || flags.clientSecretVariable != "" || flags.clientIDVariable != "" {
		return problem.New(problem.CategoryUsage, "shell.conflicting_arguments",
			"--gateway records a gateway reached at the account's login provider as the account's own "+
				"client, so --client-id, --client-id-variable and --client-secret-variable name a client "+
				"it never presents").
			WithRecovery("Omit them. " + usage)
	}
	if err := checkIdentityFlags(flags, usage); err != nil {
		return err
	}
	// An exchanged product's gateway defaults to its own URL, as the product
	// does, so only the plan can tell whether an audience is still missing.
	if descriptor.Gateway.Audience == modules.AudienceResource && flags.audience == "" && !descriptor.Exchanged() {
		return audienceRequired(namespace, usage)
	}
	return nil
}

// audienceRequired refuses a gateway whose audience nothing supplied: the
// descriptor binds it to a resource server, or the identity's deployment
// binds access by resource, and either way only the API's own identifier
// will do.
func audienceRequired(namespace, usage string) problem.Problem {
	return problem.New(problem.CategoryUsage, "shell.missing_required_flag",
		fmt.Sprintf("the %s module's gateway is bound to a resource server, so connect --gateway "+
			"needs --audience with the API's own resource identifier", namespace)).
		WithRecovery(usage)
}

// gatewayPlan is what one connect --gateway will write: the identity it acts
// on and the gateway record it adds to that identity's product.
type gatewayPlan struct {
	identity  contexts.Account
	namespace string
	gateway   contexts.Gateway
	// replaced reports that the product recorded a gateway before.
	replaced bool
	// sole reports that the identity's context is the document's only one
	// and is selected, so the login needs no --context.
	sole bool
}

// planGateway decides what connect --gateway writes, from the document as it
// is. The identity is found the way a non-provider product's connect finds
// it, and it must already record the product: the gateway is a second record
// of that product, not a product of its own, and the login product pin is
// what a product recorded first decides.
func planGateway(document contexts.Document, namespace string, descriptor modules.ProductDescriptor,
	gatewayURL string, flags connectFlags, contextName string) (gatewayPlan, error) {
	target, found, err := connectTarget(document, flags, contextName)
	if err != nil {
		return gatewayPlan{}, err
	}
	product, recorded := target.Products[namespace]
	if !found || !recorded {
		return gatewayPlan{}, productRequired(namespace)
	}
	if target.Auth.Kind == contexts.KindClientCredentials && !descriptor.Gateway.AllowsMachine(modules.MachineInline) {
		return gatewayPlan{}, problem.New(problem.CategoryAuthPolicy, "auth.product_not_configured",
			fmt.Sprintf("the %s product's gateway does not accept the machine client the %q account "+
				"holds", namespace, target.Name)).
			WithRecovery("Record the gateway on an account that signs in through the browser, or install " +
				"a version of the module whose descriptor says how a machine client reaches its gateway.")
	}
	plan := gatewayPlan{identity: target, namespace: namespace}
	if product.Gateway != nil {
		if !flags.replace {
			return gatewayPlan{}, gatewayExists(target.Name, namespace)
		}
		plan.replaced = true
	}
	plan.gateway = contexts.Gateway{Endpoint: gatewayURL, Audience: flags.audience, Scopes: descriptor.Gateway.Scopes}
	// A deployment that binds access by resource needs the API's own
	// identifier whatever kind the descriptor names; only a scope-bound one
	// can fall back to the identity's client.
	resourceBound := target.Auth.Derivation() == contexts.DerivationTokenResource
	if plan.gateway.Audience == "" && descriptor.Gateway.Audience == modules.AudienceClient && !resourceBound {
		plan.gateway.Audience = target.Auth.ClientID
	}
	// A gateway of a product reached by exchange is reached by exchange too
	// (contexts.Account.Access), so it takes the same default the product
	// does: the URL it answers at. The recorded grant decides rather than the
	// descriptor, because a product recorded by hand may be reached otherwise.
	if plan.gateway.Audience == "" && product.Grant != nil && product.Grant.Kind == contexts.GrantExchange {
		plan.gateway.Audience = gatewayURL
	}
	if plan.gateway.Audience == "" {
		return gatewayPlan{}, audienceRequired(namespace, gatewayUsage(namespace))
	}
	if len(flags.scopes) > 0 {
		plan.gateway.Scopes = flags.scopes
	}
	plan.sole = soleIdentityContext(document, target.Name)
	return plan, nil
}

// productRequired refuses a gateway for a product the identity does not
// record yet.
func productRequired(namespace string) problem.Problem {
	return problem.New(problem.CategoryUsage, "shell.product_required",
		fmt.Sprintf("the %s product is not recorded on the account, and a gateway is a second record "+
			"of a product, not a product of its own", namespace)).
		WithRecovery(fmt.Sprintf("Run wso2 %s connect <management-url> first, then this command; or pass "+
			"--account <name> or --login-provider <issuer-url> naming an account that records the "+
			"product. wso2 account list shows them.", namespace))
}

// gatewayExists refuses a second gateway on a product without --replace.
func gatewayExists(identity, namespace string) problem.Problem {
	return problem.New(problem.CategoryUsage, "contexts.product_exists",
		fmt.Sprintf("the account %q already records a gateway for the %q product", identity, namespace)).
		WithRecovery("Run wso2 account list to see what it records. " +
			"Pass --replace to overwrite the gateway record, which replaces the whole of it.")
}

// identityWithGateway is the identity as the plan leaves it: the gateway
// record set on the product, every other record in place. It copies rather
// than mutates, so the plan can be reported and applied from the same value.
func (p gatewayPlan) identityWithGateway() contexts.Account {
	identity := p.identity
	products := maps.Clone(identity.Products)
	product := products[p.namespace]
	gateway := p.gateway
	product.Gateway = &gateway
	products[p.namespace] = product
	identity.Products = products
	return identity
}

// apply writes the plan into the document.
func (p gatewayPlan) apply(document contexts.Document) contexts.Document {
	identity := p.identityWithGateway()
	position := slices.IndexFunc(document.Accounts, func(candidate contexts.Account) bool {
		return candidate.Name == identity.Name
	})
	document.Accounts[position] = identity
	return document
}

// reportGateway states what was recorded and what to run next.
//
// The next line narrows the login to this product when the identity already
// holds its login session: the other sessions exist, and one wso2 login
// --only <namespace> establishes the gateway's beside them. Whether it does
// is read from the secure store, never written; a store that cannot be read
// is reported as holding nothing, which names the wider command.
func (s Shell) reportGateway(mode output.Mode, root string, plan gatewayPlan) error {
	// The strategy is derived on the identity as written, every product in
	// place: direct or sibling is a comparison with the login session, which
	// another product's record decides.
	access, _ := plan.identityWithGateway().Access(contexts.GatewayKey(plan.namespace))
	verb := "Recorded"
	if plan.replaced {
		verb = "Replaced"
	}
	next := s.gatewayNext(root, plan)
	reported := result.New(connectSchema).
		With("product", "Product", plan.namespace).
		With("record", "Record", recordGateway).
		With("account", "Account", plan.identity.Name).
		With("created", "Account created", "false").
		With("endpoint", "Endpoint", plan.gateway.Endpoint).
		With("issuer", "Issuer", access.Issuer).
		With("clientId", "Client ID", access.ClientID).
		With("audience", "Audience", plan.gateway.Audience).
		With("scopes", "Scopes", strings.Join(plan.gateway.Scopes, ",")).
		With("strategy", "Strategy", access.Strategy).
		With("replaced", "Replaced", fmt.Sprintf("%t", plan.replaced)).
		With(output.NextField, "Next", next)
	if mode == output.ModeJSON {
		return output.Report(s.Streams.Out, mode, reported)
	}
	if _, err := fmt.Fprintf(s.Streams.Out, "\n%s the %q gateway on the %q account.\n\n",
		verb, plan.namespace, plan.identity.Name); err != nil {
		return err
	}
	return output.Report(s.Streams.Out, mode, reported)
}

// gatewayNext is the command that authorizes the gateway record just written.
func (s Shell) gatewayNext(root string, plan gatewayPlan) string {
	identity := plan.identity
	if identity.Auth.Kind == contexts.KindClientCredentials {
		return fmt.Sprintf("Run wso2 %s status --context %s.", plan.namespace, identity.Name)
	}
	access, _ := plan.identityWithGateway().Access(contexts.GatewayKey(plan.namespace))
	if next, ok := exchangedNext(root, plan.namespace, identity, access); ok {
		return next
	}
	context := " --context " + identity.Name
	if plan.sole {
		context = ""
	}
	store := session.Store{StateRoot: root}
	held := func(access contexts.ProductAccess) bool {
		_, err := store.Load(access.SessionRef)
		return err == nil
	}
	own, _ := identity.Access(plan.namespace)
	if held(identity.LoginAccess()) && held(own) {
		return fmt.Sprintf("Run wso2 login --only %s%s.", plan.namespace, context)
	}
	return fmt.Sprintf("Run wso2 login%s.", context)
}
