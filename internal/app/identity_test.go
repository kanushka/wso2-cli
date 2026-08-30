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

package app_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
)

// selfHostedDocument is the state wso2 login leaves a self-hosted first run in:
// one identity, one context, no products. It is the state wso2 identity
// add-product exists to move a user out of, and it is B.1 of
// docs/examples/login-walkthroughs.md.
func selfHostedDocument() contexts.Document {
	return contexts.Document{
		SchemaVersion:  contexts.SchemaVersion,
		DefaultContext: "idp-customer-example",
		Identities: []contexts.Identity{{
			Name: "idp-customer-example",
			Type: "onprem",
			Auth: contexts.IdentityAuth{
				Kind:          contexts.KindOAuthBrowser,
				Issuer:        "https://idp.customer.example",
				ClientID:      "wso2-cli",
				CredentialRef: "idp-customer-example",
			},
		}},
		Contexts: []contexts.Context{{
			Name: "idp-customer-example", Identity: "idp-customer-example",
		}},
	}
}

// identityNamed reports the named identity, or fails the test.
func identityNamed(t *testing.T, document contexts.Document, name string) contexts.Identity {
	t.Helper()
	for _, candidate := range document.Identities {
		if candidate.Name == name {
			return candidate
		}
	}
	t.Fatalf("the document declares no identity named %q: %+v", name, document.Identities)
	return contexts.Identity{}
}

func TestAddProductRecordsAnEndpointAudienceAndScopes(t *testing.T) {
	shell, out, errOut := newShell(t)
	installLogin(t, shell, selfHostedDocument())

	code := shell.Run([]string{"identity", "add-product", "idp-customer-example", "api",
		"--endpoint", "https://api.customer.example",
		"--audience", "https://api.customer.example",
		"--scopes", "api:read,api:write"})
	if code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stdout: %s stderr: %s", code, exit.OK, out, errOut)
	}
	identity := identityNamed(t, loadDocument(t, shell), "idp-customer-example")
	product, recorded := identity.Products["api"]
	if !recorded {
		t.Fatalf("the identity records no %q product: %+v", "api", identity.Products)
	}
	if product.Endpoint != "https://api.customer.example" {
		t.Errorf("endpoint = %q, want the one that was named", product.Endpoint)
	}
	if product.Audience != "https://api.customer.example" {
		t.Errorf("audience = %q, want the one that was named", product.Audience)
	}
	// The comma-separated list B.2 shows has to arrive as two scopes, not as
	// one scope with a comma in it.
	if want := []string{"api:read", "api:write"}; !slices.Equal(product.Scopes, want) {
		t.Errorf("scopes = %v, want %v", product.Scopes, want)
	}
}

func TestAddProductCreatesNoIdentityAndNoContext(t *testing.T) {
	shell, out, errOut := newShell(t)
	installLogin(t, shell, selfHostedDocument())

	code := shell.Run([]string{"identity", "add-product", "idp-customer-example", "api",
		"--endpoint", "https://api.customer.example"})
	if code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stdout: %s stderr: %s", code, exit.OK, out, errOut)
	}
	document := loadDocument(t, shell)
	if len(document.Identities) != 1 {
		t.Errorf("the document holds %d identities, want the one login wrote", len(document.Identities))
	}
	if len(document.Contexts) != 1 {
		t.Errorf("the document holds %d contexts, want the one login wrote", len(document.Contexts))
	}
	if document.DefaultContext != "idp-customer-example" {
		t.Errorf("defaultContext = %q, want it unchanged", document.DefaultContext)
	}
}

func TestAddProductIsRefusedForAnUnknownIdentity(t *testing.T) {
	shell, out, errOut := newShell(t)
	installLogin(t, shell, selfHostedDocument())

	code := shell.Run([]string{"identity", "add-product", "nosuch", "api",
		"--endpoint", "https://api.customer.example"})
	if code != exit.Usage {
		t.Fatalf("exit code = %d, want the usage class %d; stdout: %s stderr: %s",
			code, exit.Usage, out, errOut)
	}
	if !strings.Contains(errOut.String(), "contexts.unknown_identity") {
		t.Errorf("stderr does not carry contexts.unknown_identity:\n%s", errOut)
	}
	if products := identityNamed(t, loadDocument(t, shell), "idp-customer-example").Products; len(products) != 0 {
		t.Errorf("a refused command wrote a product: %+v", products)
	}
}

func TestAddingANamespaceTheIdentityAlreadyCarriesIsRefused(t *testing.T) {
	shell, out, errOut := newShell(t)
	seeded := selfHostedDocument()
	seeded.Identities[0].Products = map[string]contexts.Product{
		"api": {Endpoint: "https://api.customer.example", Scopes: []string{"api:read"}},
	}
	installLogin(t, shell, seeded)

	code := shell.Run([]string{"identity", "add-product", "idp-customer-example", "api",
		"--endpoint", "https://elsewhere.customer.example"})
	if code != exit.Usage {
		t.Fatalf("exit code = %d, want the usage class %d; stdout: %s stderr: %s",
			code, exit.Usage, out, errOut)
	}
	if !strings.Contains(errOut.String(), "contexts.product_exists") {
		t.Errorf("stderr does not carry contexts.product_exists:\n%s", errOut)
	}
	// The recovery has to name the way through, or the refusal is a dead end.
	if !strings.Contains(errOut.String(), "--replace") {
		t.Errorf("stderr does not name --replace:\n%s", errOut)
	}
	product := identityNamed(t, loadDocument(t, shell), "idp-customer-example").Products["api"]
	if product.Endpoint != "https://api.customer.example" ||
		!slices.Equal(product.Scopes, []string{"api:read"}) {
		t.Errorf("the recorded product was changed by a refused command: %+v", product)
	}
}

func TestReplacingAnExistingNamespaceRequiresTheReplaceFlag(t *testing.T) {
	shell, out, errOut := newShell(t)
	seeded := selfHostedDocument()
	seeded.Identities[0].Products = map[string]contexts.Product{
		"api": {Endpoint: "https://api.customer.example", Scopes: []string{"api:read"}},
	}
	installLogin(t, shell, seeded)

	code := shell.Run([]string{"identity", "add-product", "idp-customer-example", "api",
		"--endpoint", "https://elsewhere.customer.example", "--replace"})
	if code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stdout: %s stderr: %s", code, exit.OK, out, errOut)
	}
	product := identityNamed(t, loadDocument(t, shell), "idp-customer-example").Products["api"]
	if product.Endpoint != "https://elsewhere.customer.example" {
		t.Errorf("endpoint = %q, want the replacement", product.Endpoint)
	}
	// A replacement replaces the whole record. A scope left over from the
	// record that was replaced would be a permission nobody asked for.
	if len(product.Scopes) != 0 {
		t.Errorf("scopes = %v, want none: the replaced record's scopes survived", product.Scopes)
	}
}

func TestAnEndpointEmbeddingUserInformationIsRefused(t *testing.T) {
	shell, out, errOut := newShell(t)
	installLogin(t, shell, selfHostedDocument())

	const endpoint = "https://ops:hunter2@api.customer.example"
	code := shell.Run([]string{"identity", "add-product", "idp-customer-example", "api",
		"--endpoint", endpoint})
	if code != exit.Usage {
		t.Fatalf("exit code = %d, want the usage class %d; stdout: %s stderr: %s",
			code, exit.Usage, out, errOut)
	}
	// The rejected endpoint is the likeliest place for a credential to have
	// been typed by mistake, so neither stream repeats it. internal/contexts
	// deliberately does not echo it, and this command must not undo that by
	// echoing it itself.
	for name, stream := range map[string]string{"stdout": out.String(), "stderr": errOut.String()} {
		if strings.Contains(stream, "hunter2") || strings.Contains(stream, endpoint) {
			t.Errorf("%s echoes the rejected endpoint:\n%s", name, stream)
		}
	}
	if products := identityNamed(t, loadDocument(t, shell), "idp-customer-example").Products; len(products) != 0 {
		t.Errorf("a refused endpoint was written: %+v", products)
	}
	// The document's own refusal already carries the best recovery in this
	// family: it names the thing to take out of the endpoint, and it says why.
	// Rewording a refusal at this call site must not overwrite a recovery that
	// was better than what replaces it.
	if !strings.Contains(errOut.String(), "Remove the user information from the endpoint") {
		t.Errorf("the refusal lost its own recovery:\n%s", errOut)
	}
	if strings.Contains(errOut.String(), "correct the command and run it again") {
		t.Errorf("the refusal was given the generic recovery in place of its own:\n%s", errOut)
	}
}

func TestAnInvalidNamespaceIsRefusedAsTheArgumentItIs(t *testing.T) {
	shell, out, errOut := newShell(t)
	installLogin(t, shell, selfHostedDocument())

	code := shell.Run([]string{"identity", "add-product", "idp-customer-example", "API",
		"--endpoint", "https://api.customer.example"})
	if code != exit.Usage {
		t.Fatalf("exit code = %d, want the usage class %d; stdout: %s stderr: %s",
			code, exit.Usage, out, errOut)
	}
	if !strings.Contains(errOut.String(), "shell.invalid_argument") {
		t.Errorf("stderr does not carry shell.invalid_argument:\n%s", errOut)
	}
	// The user's file is not the thing that is wrong, and advice to remove it
	// would destroy the identity they just logged in as.
	if strings.Contains(errOut.String(), "contexts.document_malformed") {
		t.Errorf("a mistyped argument was reported as a malformed document:\n%s", errOut)
	}
}

func TestAddProductWithoutAnEndpointIsRefusedAsAMissingFlag(t *testing.T) {
	shell, out, errOut := newShell(t)
	installLogin(t, shell, selfHostedDocument())

	code := shell.Run([]string{"identity", "add-product", "idp-customer-example", "api"})
	if code != exit.Usage {
		t.Fatalf("exit code = %d, want the usage class %d; stdout: %s stderr: %s",
			code, exit.Usage, out, errOut)
	}
	if !strings.Contains(errOut.String(), "shell.missing_required_flag") {
		t.Errorf("stderr does not carry shell.missing_required_flag:\n%s", errOut)
	}
}

func TestAddProductRefusesAWrongArgumentCountInTheUsageClass(t *testing.T) {
	for name, args := range map[string][]string{
		"no arguments":  {"identity", "add-product"},
		"one argument":  {"identity", "add-product", "idp-customer-example"},
		"three":         {"identity", "add-product", "idp-customer-example", "api", "extra"},
		"list, a stray": {"identity", "list", "extra"},
	} {
		t.Run(name, func(t *testing.T) {
			shell, out, errOut := newShell(t)
			installLogin(t, shell, selfHostedDocument())
			// The class is the assertion. cobra.ExactArgs refuses the same
			// counts, but its error never reaches the flag-error hook and
			// arrives at the classifier untyped, exiting in the module-process
			// class the command reference documents as a module that crashed.
			if code := shell.Run(args); code != exit.Usage {
				t.Fatalf("exit code = %d, want the usage class %d; stdout: %s stderr: %s",
					code, exit.Usage, out, errOut)
			}
		})
	}
}

func TestIdentityListShowsWhatEachIdentityReaches(t *testing.T) {
	shell, out, errOut := newShell(t)
	seeded := selfHostedDocument()
	seeded.Identities[0].Products = map[string]contexts.Product{
		"api": {
			Endpoint: "https://api.customer.example",
			Audience: "https://api.customer.example",
			Scopes:   []string{"api:read", "api:write"},
		},
		"integration": {Endpoint: "https://esb.customer.example"},
	}
	installLogin(t, shell, seeded)

	if code := shell.Run([]string{"identity", "list"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	rendered := out.String()
	for _, want := range []string{
		"idp-customer-example", "https://idp.customer.example",
		"api", "https://api.customer.example",
		"integration", "https://esb.customer.example",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the listing does not show %q:\n%s", want, rendered)
		}
	}
	// A listing is read over a shoulder and pasted into tickets. The
	// secure-store reference is not a credential, but it is the name of where
	// one lives and nothing in this listing needs it.
	if strings.Contains(rendered, "credentialRef") {
		t.Errorf("the listing names the credential reference:\n%s", rendered)
	}
}

func TestIdentityListOnAMachineWithNoDocumentSaysSoPlainly(t *testing.T) {
	shell, out, errOut := newShell(t)

	if code := shell.Run([]string{"identity", "list"}); code != exit.OK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, exit.OK, errOut)
	}
	// An unconfigured machine is a state, not a breakage, and login is the only
	// thing that creates an identity (#112 D3).
	if !strings.Contains(out.String(), "wso2 login") {
		t.Errorf("the empty listing does not name wso2 login:\n%s", out)
	}
}

func TestIdentitySubcommandsRenderJSON(t *testing.T) {
	t.Run("add-product", func(t *testing.T) {
		shell, out, errOut := newShell(t)
		installLogin(t, shell, selfHostedDocument())
		code := shell.Run([]string{"--output", "json", "identity", "add-product",
			"idp-customer-example", "api", "--endpoint", "https://api.customer.example",
			"--scopes", "api:read,api:write"})
		if code != exit.OK {
			t.Fatalf("exit code = %d, want %d; stdout: %s stderr: %s", code, exit.OK, out, errOut)
		}
		var added struct {
			Identity  string   `json:"identity"`
			Namespace string   `json:"namespace"`
			Endpoint  string   `json:"endpoint"`
			Scopes    []string `json:"scopes"`
			Replaced  bool     `json:"replaced"`
		}
		if err := json.Unmarshal(out.Bytes(), &added); err != nil {
			t.Fatalf("the output is not JSON: %v\n%s", err, out)
		}
		if added.Identity != "idp-customer-example" || added.Namespace != "api" ||
			added.Endpoint != "https://api.customer.example" ||
			!slices.Equal(added.Scopes, []string{"api:read", "api:write"}) || added.Replaced {
			t.Errorf("the result does not carry what was recorded: %+v", added)
		}
	})

	t.Run("list", func(t *testing.T) {
		shell, out, errOut := newShell(t)
		seeded := selfHostedDocument()
		seeded.Identities[0].Products = map[string]contexts.Product{
			"api": {Endpoint: "https://api.customer.example", Scopes: []string{"api:read"}},
		}
		installLogin(t, shell, seeded)
		if code := shell.Run([]string{"--output", "json", "identity", "list"}); code != exit.OK {
			t.Fatalf("exit code = %d, want %d; stdout: %s stderr: %s", code, exit.OK, out, errOut)
		}
		var listing struct {
			Identities []struct {
				Name     string `json:"name"`
				Type     string `json:"type"`
				Kind     string `json:"kind"`
				Issuer   string `json:"issuer"`
				Products []struct {
					Namespace string   `json:"namespace"`
					Endpoint  string   `json:"endpoint"`
					Scopes    []string `json:"scopes"`
				} `json:"products"`
			} `json:"identities"`
		}
		if err := json.Unmarshal(out.Bytes(), &listing); err != nil {
			t.Fatalf("the output is not JSON: %v\n%s", err, out)
		}
		if len(listing.Identities) != 1 || len(listing.Identities[0].Products) != 1 {
			t.Fatalf("the listing does not carry the identity and its product: %+v", listing)
		}
		if listing.Identities[0].Products[0].Namespace != "api" {
			t.Errorf("the product namespace is not carried: %+v", listing.Identities[0].Products[0])
		}
		if strings.Contains(out.String(), "credentialRef") {
			t.Errorf("the JSON listing names the credential reference:\n%s", out)
		}
	})
}

// TestNoIdentitySubcommandOpensANetworkConnection is the D8 guard for this
// family: recording an endpoint asserts what a deployment serves and never
// checks it, so a wrong assertion surfaces at first use (B.3) rather than
// making the write depend on the deployment being up.
//
// It uses the same seam as the context family's guard, for the same reason:
// every HTTP client the shell builds leaves its Transport nil or names
// http.DefaultTransport, so replacing that one value intercepts every request
// this binary can make today. TestTheNetworkGuardWouldNoticeARequest, beside
// the context family's guard, is what proves the seam is not vacuous.
func TestNoIdentitySubcommandOpensANetworkConnection(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = failingTransport{t: t}

	invocations := map[string][]string{
		"add-product": {"identity", "add-product", "idp-customer-example", "integration",
			"--endpoint", "https://esb.customer.example"},
		"add-product, replacing": {"identity", "add-product", "idp-customer-example", "api",
			"--endpoint", "https://elsewhere.customer.example", "--replace"},
		"list": {"identity", "list"},
		// The refusals matter more than the successes: a refusal is where a
		// well-meaning "let me check the endpoint before I complain" would be
		// added, and it is the path a first-run user reaches first.
		"add-product, unknown identity": {"identity", "add-product", "nosuch", "api",
			"--endpoint", "https://api.customer.example"},
		"add-product, taken namespace": {"identity", "add-product", "idp-customer-example", "api",
			"--endpoint", "https://elsewhere.customer.example"},
		"add-product, user information": {"identity", "add-product", "idp-customer-example",
			"integration", "--endpoint", "https://ops:hunter2@esb.customer.example"},
		"add-product, illegal namespace": {"identity", "add-product", "idp-customer-example",
			"API", "--endpoint", "https://api.customer.example"},
		"add-product, no endpoint":  {"identity", "add-product", "idp-customer-example", "integration"},
		"add-product, no arguments": {"identity", "add-product"},
		"unsupported shell flag":    {"--context", "idp-customer-example", "identity", "list"},
	}
	for name, args := range invocations {
		t.Run(name, func(t *testing.T) {
			shell, _, _ := newShell(t)
			seeded := selfHostedDocument()
			seeded.Identities[0].Products = map[string]contexts.Product{
				"api": {Endpoint: "https://api.customer.example"},
			}
			installLogin(t, shell, seeded)
			// The exit code is not asserted: what is asserted is that whatever
			// the command decided, it decided it without dialling anything.
			shell.Run(args)
		})
	}
}

// TestASecondProductOnAResourceBoundIdentityIsRefusedIntelligibly pins what a
// user is told when the document's own validation refuses the write.
//
// A token-resource identity binds one login to one product, so a second product
// cannot be added to it at all. The refusal is the document's and is correct;
// what this pins is that it arrives as a usage problem about the command that
// was run, and not as advice to remove a document nothing wrong ever reached.
func TestASecondProductOnAResourceBoundIdentityIsRefusedIntelligibly(t *testing.T) {
	shell, out, errOut := newShell(t)
	seeded := selfHostedDocument()
	seeded.Identities[0].Auth.Narrowing = contexts.DerivationTokenResource
	seeded.Identities[0].Products = map[string]contexts.Product{
		"api": {
			Endpoint: "https://api.customer.example",
			Audience: "https://api.customer.example",
		},
	}
	installLogin(t, shell, seeded)

	code := shell.Run([]string{"identity", "add-product", "idp-customer-example", "integration",
		"--endpoint", "https://esb.customer.example",
		"--audience", "https://esb.customer.example"})
	if code != exit.Usage {
		t.Fatalf("exit code = %d, want the usage class %d; stdout: %s stderr: %s",
			code, exit.Usage, out, errOut)
	}
	if strings.Contains(errOut.String(), "remove it to run without a context") {
		t.Errorf("the refusal offers to remove a document nothing wrong reached:\n%s", errOut)
	}
	if !strings.Contains(errOut.String(), "was not changed") {
		t.Errorf("the refusal does not say the document is unchanged:\n%s", errOut)
	}
	// No correction of this command succeeds: the constraint is on the
	// identity rather than on any flag, so advice to correct the command and
	// re-run it would be false. What does work has to be named instead.
	if strings.Contains(errOut.String(), "correct the command and run it again") {
		t.Errorf("the refusal offers a correction of a command no correction fixes:\n%s", errOut)
	}
	for _, want := range []string{"--replace", "wso2 login"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("the refusal does not name %s, which does work:\n%s", want, errOut)
		}
	}
	if products := identityNamed(t, loadDocument(t, shell), "idp-customer-example").Products; len(products) != 1 {
		t.Errorf("the refused write reached the document: %+v", products)
	}
}

// TestIdentityIsAReservedNamespace guards the consequence of adding the family:
// a module namespace called "identity" is unreachable now, so the scaffold has
// to refuse it before anyone publishes one.
func TestIdentityIsAReservedNamespace(t *testing.T) {
	if !slices.Contains(app.CommandNames(), "identity") {
		t.Errorf("CommandNames() = %v, which does not reserve identity", app.CommandNames())
	}
}

// TestTheSelfHostedFirstRunPathRunsWithoutAnEditor is what #119's last
// acceptance criterion asks for: B.1 and B.2 of
// docs/examples/login-walkthroughs.md, run against the shell rather than read.
//
// It runs in this process rather than against the built binary because the
// login half needs a browser hook and a mocked secure store, neither of which a
// separately executed binary has. Everything after the login is the same
// command dispatch the binary runs.
func TestTheSelfHostedFirstRunPathRunsWithoutAnEditor(t *testing.T) {
	shell, out, errOut, issuer := newCreatingLogin(t)

	if code := shell.Run([]string{"login", "--url", issuer.URL, "--client-id", "wso2-cli"}); code != exit.OK {
		t.Fatalf("login exited %d; stderr: %s", code, errOut)
	}
	// The name login assigned, read back rather than assumed: B.1's whole
	// point is that login reports it and the next command takes it.
	identity := loadDocument(t, shell).Identities[0].Name
	if !strings.Contains(out.String(), "wso2 identity add-product") {
		t.Errorf("login does not name the command that carries on from here:\n%s", out)
	}
	out.Reset()

	for _, args := range [][]string{
		{"identity", "add-product", identity, "api",
			"--endpoint", "https://api.customer.example",
			"--audience", "https://api.customer.example",
			"--scopes", "api:read,api:write"},
		{"identity", "add-product", identity, "integration",
			"--endpoint", "https://esb.customer.example",
			"--audience", "https://esb.customer.example",
			"--scopes", "integration:read"},
	} {
		if code := shell.Run(args); code != exit.OK {
			t.Fatalf("%v exited %d; stderr: %s", args, code, errOut)
		}
	}

	recorded := identityNamed(t, loadDocument(t, shell), identity).Products
	if len(recorded) != 2 {
		t.Fatalf("the identity records %d products, want the two B.2 adds: %+v", len(recorded), recorded)
	}
	if recorded["integration"].Endpoint != "https://esb.customer.example" ||
		!slices.Equal(recorded["integration"].Scopes, []string{"integration:read"}) {
		t.Errorf("the integration product is not what B.2 records: %+v", recorded["integration"])
	}
	out.Reset()
	if code := shell.Run([]string{"identity", "list"}); code != exit.OK {
		t.Fatalf("identity list exited %d; stderr: %s", code, errOut)
	}
	for _, want := range []string{"api", "integration",
		"https://api.customer.example", "https://esb.customer.example"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the listing does not show %q:\n%s", want, out)
		}
	}
	// The claim the whole wave makes: no step above opened a file in an editor,
	// and the listing is now what a self-hosted deployment reaches.
	if strings.Contains(out.String(), identityAddProductUsageProbe) {
		t.Errorf("the listing still asks for a product to be recorded:\n%s", out)
	}
}

// identityAddProductUsageProbe is the fragment the listing prints only while an
// identity still has no product. Spelled here rather than imported: the command
// constant is unexported and this is an external test package.
const identityAddProductUsageProbe = "Run wso2 identity add-product"
