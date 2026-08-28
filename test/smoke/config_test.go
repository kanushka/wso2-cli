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

package smoke_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/test/smoke"
)

// environment turns a map into the lookup Load reads its configuration through.
// An entry that is present but empty is reported as present, exactly as the
// process environment reports an exported empty variable.
func environment(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, present := values[name]
		return value, present
	}
}

// configured is the smallest environment that describes a live deployment.
func configured() map[string]string {
	return map[string]string{
		smoke.IssuerVar:   "https://api.asgardeo.io/t/acme/oauth2/token",
		smoke.ClientIDVar: "abc123",
		smoke.AudienceVar: "reference-status",
		smoke.ScopeVar:    "reference:status:read reference:status:write",
	}
}

func TestLoadReadsALiveDeployment(t *testing.T) {
	config, err := smoke.Load(environment(configured()))
	if err != nil {
		t.Fatalf("Load refused a complete environment: %v", err)
	}
	if config.Issuer != "https://api.asgardeo.io/t/acme/oauth2/token" {
		t.Errorf("issuer = %q", config.Issuer)
	}
	if config.ClientID != "abc123" {
		t.Errorf("client id = %q", config.ClientID)
	}
	if config.Audience != "reference-status" {
		t.Errorf("audience = %q", config.Audience)
	}
	want := []string{"reference:status:read", "reference:status:write"}
	if !slices.Equal(config.Scopes, want) {
		t.Errorf("scopes = %v, want %v", config.Scopes, want)
	}
}

func TestLoadSkipsWhenNothingIsConfigured(t *testing.T) {
	_, err := smoke.Load(environment(nil))
	if !errors.Is(err, smoke.ErrNotConfigured) {
		t.Fatalf("Load reported %v, want ErrNotConfigured", err)
	}
	for _, name := range []string{
		smoke.IssuerVar, smoke.ClientIDVar, smoke.AudienceVar, smoke.ScopeVar,
	} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("the skip reason does not name %s: %v", name, err)
		}
	}
}

func TestLoadNamesOnlyTheMissingVariables(t *testing.T) {
	values := configured()
	delete(values, smoke.AudienceVar)

	_, err := smoke.Load(environment(values))
	if !errors.Is(err, smoke.ErrNotConfigured) {
		t.Fatalf("Load reported %v, want ErrNotConfigured", err)
	}
	if !strings.Contains(err.Error(), smoke.AudienceVar) {
		t.Errorf("the skip reason does not name the missing %s: %v", smoke.AudienceVar, err)
	}
	if strings.Contains(err.Error(), smoke.IssuerVar) {
		t.Errorf("the skip reason names %s, which was set: %v", smoke.IssuerVar, err)
	}
}

func TestLoadTreatsAnEmptyVariableAsUnset(t *testing.T) {
	values := configured()
	values[smoke.ClientIDVar] = "   "

	_, err := smoke.Load(environment(values))
	if !errors.Is(err, smoke.ErrNotConfigured) {
		t.Fatalf("Load reported %v, want ErrNotConfigured", err)
	}
	if !strings.Contains(err.Error(), smoke.ClientIDVar) {
		t.Errorf("the skip reason does not name the blank %s: %v", smoke.ClientIDVar, err)
	}
}

func TestLoadSplitsScopesOnWhitespaceAndCommas(t *testing.T) {
	values := configured()
	values[smoke.ScopeVar] = "  read , ,\twrite\n"

	config, err := smoke.Load(environment(values))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := []string{"read", "write"}; !slices.Equal(config.Scopes, want) {
		t.Errorf("scopes = %v, want %v", config.Scopes, want)
	}
}

func TestLoadRefusesAScopeListThatNamesNoPermission(t *testing.T) {
	// Separators alone are not an empty string, so a check against the raw
	// variable passes and a run reaches a live browser to ask for nothing.
	for _, value := range []string{",", " , ", "\t\n"} {
		values := configured()
		values[smoke.ScopeVar] = value

		_, err := smoke.Load(environment(values))
		if !errors.Is(err, smoke.ErrNotConfigured) {
			t.Errorf("a scope list of %q loaded as %v, want it reported unconfigured", value, err)
			continue
		}
		if !strings.Contains(err.Error(), smoke.ScopeVar) {
			t.Errorf("the refusal for %q does not name %s: %v", value, smoke.ScopeVar, err)
		}
	}
}

func TestLoadDerivesTheProductEndpointFromTheIssuer(t *testing.T) {
	config, err := smoke.Load(environment(configured()))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if config.Endpoint != "https://api.asgardeo.io" {
		t.Errorf("endpoint = %q, want the issuer's origin", config.Endpoint)
	}
}

func TestLoadPrefersAnExplicitProductEndpoint(t *testing.T) {
	values := configured()
	values[smoke.EndpointVar] = "https://reference.example.test"

	config, err := smoke.Load(environment(values))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if config.Endpoint != "https://reference.example.test" {
		t.Errorf("endpoint = %q", config.Endpoint)
	}
}

// A typo in a configured variable must fail the run rather than skip it.
// Skipping would report a deployment as untested when it was in fact misnamed.
func TestLoadRefusesAMalformedIssuer(t *testing.T) {
	values := configured()
	values[smoke.IssuerVar] = "not-a-url"

	_, err := smoke.Load(environment(values))
	if err == nil {
		t.Fatal("Load accepted a malformed issuer")
	}
	if errors.Is(err, smoke.ErrNotConfigured) {
		t.Errorf("a malformed issuer was reported as an absent configuration: %v", err)
	}
}

func TestLoadRefusesAMalformedUnregisteredPort(t *testing.T) {
	values := configured()
	values[smoke.UnregisteredPortVar] = "sixteen-thousand"

	_, err := smoke.Load(environment(values))
	if err == nil {
		t.Fatal("Load accepted a malformed port")
	}
	if errors.Is(err, smoke.ErrNotConfigured) {
		t.Errorf("a malformed port was reported as an absent configuration: %v", err)
	}
}

func TestLoadDefaultsTheExperimentKnobs(t *testing.T) {
	config, err := smoke.Load(environment(configured()))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if config.UnregisteredPort != 16000 {
		t.Errorf("unregistered port = %d, want 16000", config.UnregisteredPort)
	}
	if config.Deadline != 3*time.Minute {
		t.Errorf("deadline = %s, want 3m", config.Deadline)
	}
	if config.IdentityType != "cloud" {
		t.Errorf("identity type = %q, want cloud", config.IdentityType)
	}
}

func TestLoadAcceptsAnOnPremisesIdentityType(t *testing.T) {
	values := configured()
	values[smoke.IdentityTypeVar] = "onprem"

	config, err := smoke.Load(environment(values))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if config.IdentityType != "onprem" {
		t.Errorf("identity type = %q", config.IdentityType)
	}
}

func TestLoadRefusesAnUnknownIdentityType(t *testing.T) {
	values := configured()
	values[smoke.IdentityTypeVar] = "saas"

	if _, err := smoke.Load(environment(values)); err == nil {
		t.Fatal("Load accepted an identity type the context schema rejects")
	}
}

// The document a live run writes must be one the shell agrees to read. Proving
// that here rather than in front of a human with a browser open is the whole
// reason this seam is a package of its own.
func TestDocumentIsReadableByTheShell(t *testing.T) {
	config, err := smoke.Load(environment(configured()))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	encoded, err := config.Document().Encode()
	if err != nil {
		t.Fatalf("the smoke document does not encode: %v", err)
	}
	decoded, err := contexts.Decode(encoded)
	if err != nil {
		t.Fatalf("the shell refuses to read the smoke document: %v", err)
	}
	selection, err := decoded.Select("")
	if err != nil {
		t.Fatalf("the smoke document selects no context: %v", err)
	}
	if selection.Identity.Auth.Kind != contexts.KindOAuthBrowser {
		t.Errorf("auth kind = %q, want %q", selection.Identity.Auth.Kind, contexts.KindOAuthBrowser)
	}
	if selection.Identity.Auth.Issuer != config.Issuer {
		t.Errorf("issuer = %q, want %q", selection.Identity.Auth.Issuer, config.Issuer)
	}
	if selection.Identity.Auth.ClientID != config.ClientID {
		t.Errorf("client id = %q", selection.Identity.Auth.ClientID)
	}
	if selection.Identity.Auth.CredentialRef != smoke.CredentialRef {
		t.Errorf("credential ref = %q", selection.Identity.Auth.CredentialRef)
	}
	product, declared := selection.Identity.Products[smoke.Namespace]
	if !declared {
		t.Fatalf("the smoke identity does not configure the %q product", smoke.Namespace)
	}
	if product.Audience != config.Audience {
		t.Errorf("audience = %q", product.Audience)
	}
	if !slices.Equal(product.Scopes, config.Scopes) {
		t.Errorf("scopes = %v, want %v", product.Scopes, config.Scopes)
	}
}

// A context naming an organization the identity does not call home is refused
// by the broker with auth.organization_switch_unsupported. The smoke document
// must never provoke that refusal by accident.
func TestDocumentKeepsTheContextInTheIdentityHomeTenant(t *testing.T) {
	values := configured()
	values[smoke.TenantVar] = "acme"

	config, err := smoke.Load(environment(values))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	selection, err := config.Document().Select("")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if selection.Context.Organization != "acme" {
		t.Errorf("organization = %q", selection.Context.Organization)
	}
	if selection.Identity.Auth.Tenant != selection.Context.Organization {
		t.Errorf("tenant %q and organization %q differ",
			selection.Identity.Auth.Tenant, selection.Context.Organization)
	}
}

func TestDocumentOmitsTheTenantWhenNoneIsConfigured(t *testing.T) {
	config, err := smoke.Load(environment(configured()))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	selection, err := config.Document().Select("")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if selection.Context.Organization != "" || selection.Identity.Auth.Tenant != "" {
		t.Errorf("organization = %q and tenant = %q, want both empty",
			selection.Context.Organization, selection.Identity.Auth.Tenant)
	}
}

func TestCapabilitiesDeclareExactlyWhatTheRunRequests(t *testing.T) {
	config, err := smoke.Load(environment(configured()))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	capabilities := config.Capabilities()
	// The receipt declares the module's own logical name, not the deployment's
	// audience. Holding the two equal would model a coincidence no real module
	// has, and would leave every live run unable to fail on a broker that
	// compared them.
	if !slices.Contains(capabilities.AuthAudiences, smoke.ModuleAudience) {
		t.Errorf("audiences = %v, want to contain %q", capabilities.AuthAudiences, smoke.ModuleAudience)
	}
	for _, scope := range config.Scopes {
		if !slices.Contains(capabilities.AuthScopes, scope) {
			t.Errorf("scopes = %v, want to contain %q", capabilities.AuthScopes, scope)
		}
	}
}

func TestNarrowTargetPicksOneScopeOutOfMany(t *testing.T) {
	config, err := smoke.Load(environment(configured()))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(config.Scopes) < 2 {
		t.Fatal("the fixture no longer configures two scopes")
	}
	target, err := config.NarrowTarget()
	if err != nil {
		t.Fatalf("NarrowTarget: %v", err)
	}
	if !slices.Contains(config.Scopes, target) {
		t.Errorf("narrow target %q is not one of %v", target, config.Scopes)
	}
}

// Narrowing one scope to one scope proves nothing, so the experiment refuses to
// report a verdict it cannot support.
func TestNarrowTargetRefusesASingleScope(t *testing.T) {
	values := configured()
	values[smoke.ScopeVar] = "reference:status:read"

	config, err := smoke.Load(environment(values))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := config.NarrowTarget(); err == nil {
		t.Fatal("NarrowTarget reported a verdict from a single scope")
	}
}

func TestEmpiricalIsOptedIntoExplicitly(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		values  map[string]string
		wantSet bool
	}{
		{name: "absent", values: nil, wantSet: false},
		{name: "empty", values: map[string]string{smoke.EmpiricalVar: ""}, wantSet: false},
		{name: "blank", values: map[string]string{smoke.EmpiricalVar: "  "}, wantSet: false},
		{name: "one", values: map[string]string{smoke.EmpiricalVar: "1"}, wantSet: true},
		{name: "word", values: map[string]string{smoke.EmpiricalVar: "yes"}, wantSet: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := smoke.Empirical(environment(testCase.values)); got != testCase.wantSet {
				t.Errorf("Empirical = %v, want %v", got, testCase.wantSet)
			}
		})
	}
}

// TestTheRunAsksByALogicalAudienceAndProvesTheDeploymentsOwn is the regression
// guard for the blind spot this suite used to carry.
//
// Every live run once took a single WSO2_SMOKE_AUDIENCE for both the audience
// the module asks by and the one the deployment binds. That models a
// coincidence no real module has: a module's audience is a constant compiled
// into it, identical against every deployment, while a deployment's is its own
// — the client ID on Asgardeo, the resource identifier on Identity Server, a
// URI on Thunder. With the two held equal the gate could not fail on a broker
// that compared them, and one shipped that did.
//
// Keeping them distinct is what makes a live run evidence about the real shape.
func TestTheRunAsksByALogicalAudienceAndProvesTheDeploymentsOwn(t *testing.T) {
	values := configured()
	values[smoke.AudienceVar] = "M0Hkzofj2ZoTEKuEJEPS75EfW8ga" // an Asgardeo client ID
	config, err := smoke.Load(environment(values))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if smoke.ModuleAudience == config.Audience {
		t.Fatalf("the module asks by %q and the deployment binds %q; a live run that holds them "+
			"equal cannot fail on a broker that compares them", smoke.ModuleAudience, config.Audience)
	}
	if got := config.Capabilities().AuthAudiences; !slices.Contains(got, smoke.ModuleAudience) {
		t.Errorf("the receipt declares %v, want the module's own %q", got, smoke.ModuleAudience)
	}
	product := config.Document().Identities[0].Products[smoke.Namespace]
	if product.Audience != config.Audience {
		t.Errorf("the context registers %q, want the deployment's own %q",
			product.Audience, config.Audience)
	}
}
