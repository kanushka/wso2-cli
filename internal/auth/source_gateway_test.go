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

package auth_test

import (
	"errors"
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/modules"
)

const (
	// gatewayNamespace is the product whose gateway record is asked for. It
	// sorts after "reference" for the reason siblingNamespace does.
	gatewayNamespace = "zapim"
	// gatewayAudienceName is the logical audience the module declares for
	// its gateway; gatewayResource is what this deployment binds it to.
	gatewayAudienceName = "zapim-gateway"
	gatewayResource     = "http://localhost:18090/hello"
	gatewayScope        = "hello:read"
)

// gatewayDescriptor is the receipt's descriptor for a product with a
// gateway shape a machine client may reach inline.
func gatewayDescriptor() *modules.ProductDescriptor {
	return &modules.ProductDescriptor{
		IssuerPath: "/oauth2/token", Audience: modules.AudienceClient, Scopes: []string{"zapim:api_view"},
		Grant: contexts.GrantFederated, Machine: []string{modules.MachineCredential},
		Gateway: &modules.GatewayDescriptor{Audience: modules.AudienceResource, Machine: []string{modules.MachineInline}},
	}
}

// gatewayProduct is the product record with both records on it.
func gatewayProduct(productIssuer string) contexts.Product {
	return contexts.Product{
		Endpoint: productIssuer, Audience: "zapim-cli", Scopes: []string{"zapim:api_view"},
		Grant:   &contexts.Grant{Kind: contexts.GrantFederated, Issuer: productIssuer, ClientID: "zapim-cli"},
		Gateway: &contexts.Gateway{Endpoint: "https://gw.example", Audience: gatewayResource, Scopes: []string{gatewayScope}},
	}
}

// gatewayBroker is the broker the zapim module builds on a resource-bound
// deployment whose identity records the product with a gateway.
func gatewayBroker(t *testing.T, deployment browserDeployment) *auth.Broker {
	t.Helper()
	broker := deployment.broker(t)
	broker.Selection.Identity.Auth.Provider = contexts.ProviderThunder
	broker.Selection.Identity.Products[gatewayNamespace] = gatewayProduct(deployment.issuer.URL)
	broker.Namespace = gatewayNamespace
	broker.Capabilities.AuthAudiences = []string{"zapim-publisher", gatewayAudienceName}
	broker.Capabilities.AuthScopes = []string{"zapim:api_view"}
	broker.Capabilities.Product = gatewayDescriptor()
	return broker
}

func gatewayRequest() auth.Request {
	return auth.Request{Audience: gatewayAudienceName, Record: contexts.GatewayRecord}
}

func seedGatewaySession(t *testing.T, deployment browserDeployment) {
	t.Helper()
	seeded := deployment.issuer.SeedSessionFor([]string{gatewayScope}, gatewayResource)
	store := session.Store{StateRoot: deployment.stateRoot}
	if err := store.Save(contexts.ProductSessionRef(sessionRef, contexts.GatewayKey(gatewayNamespace)),
		session.Session{Issuer: deployment.issuer.URL, RefreshToken: seeded}); err != nil {
		t.Fatal(err)
	}
}

func TestAGatewayRecordIsAnsweredFromItsOwnSessionWithTheRecordsScopes(t *testing.T) {
	deployment := seedBrowserSession(t, fakeissuer.Options{RequireResource: true})
	seedGatewaySession(t, deployment)
	grant, err := gatewayBroker(t, deployment).Acquire(gatewayRequest())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	active, scopes, audiences := deployment.issuer.Introspect(t, grant.Token)
	if !active || len(scopes) != 1 || scopes[0] != gatewayScope || audiences[0] != gatewayResource {
		t.Fatalf("token scopes %v audiences %v", scopes, audiences)
	}
	if deployment.storedSession(t).RefreshToken != deployment.seeded {
		t.Fatal("the gateway derivation rotated the login session")
	}
}

func TestAMissingGatewaySessionIsEstablishedOnFirstUseUnderItsKey(t *testing.T) {
	deployment := seedBrowserSession(t, fakeissuer.Options{RequireResource: true})
	broker := gatewayBroker(t, deployment)
	var asked contexts.ProductAccess
	broker.EstablishSession = func(access contexts.ProductAccess) error {
		asked = access
		seeded := deployment.issuer.SeedSessionFor(access.Scopes, access.Resource)
		return session.Store{StateRoot: deployment.stateRoot}.Save(access.SessionRef,
			session.Session{Issuer: access.Issuer, RefreshToken: seeded, Strategy: access.Strategy})
	}
	if _, err := broker.Acquire(gatewayRequest()); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if asked.Namespace != contexts.GatewayKey(gatewayNamespace) || asked.Strategy != contexts.StrategySibling ||
		asked.Resource != gatewayResource || asked.Issuer != deployment.issuer.URL ||
		asked.SessionRef != contexts.ProductSessionRef(sessionRef, contexts.GatewayKey(gatewayNamespace)) {
		t.Fatalf("the hook was asked for %+v", asked)
	}
}

func TestAMissingGatewaySessionThatCannotBeEstablishedNamesTheProductsLogin(t *testing.T) {
	deployment := seedBrowserSession(t, fakeissuer.Options{RequireResource: true})
	for name, hook := range map[string]func(contexts.ProductAccess) error{
		"no hook":  nil,
		"no input": func(contexts.ProductAccess) error { return auth.BrowserUnavailable{Control: "--no-input"} },
	} {
		t.Run(name, func(t *testing.T) {
			broker := gatewayBroker(t, deployment)
			broker.EstablishSession = hook
			_, err := broker.Acquire(gatewayRequest())
			var denial auth.Denial
			if !errors.As(err, &denial) || denial.Problem.Code != "auth.session_required" {
				t.Fatalf("got %v, want auth.session_required", err)
			}
			if !strings.Contains(denial.Problem.Message, contexts.GatewayKey(gatewayNamespace)) {
				t.Errorf("the refusal %q does not name the gateway record", denial.Problem.Message)
			}
			if !strings.Contains(denial.Reported().Recovery, "wso2 login --only "+gatewayNamespace+" ") &&
				!strings.Contains(denial.Reported().Recovery, "wso2 login --only "+gatewayNamespace+".") {
				t.Errorf("the recovery %q does not name the product's login", denial.Reported().Recovery)
			}
			if hook != nil && !strings.Contains(denial.Reported().Recovery, "--no-input") {
				t.Errorf("the recovery %q does not name the control", denial.Reported().Recovery)
			}
		})
	}
}

func TestAGatewayRecordTheDescriptorDoesNotDeclareIsRefused(t *testing.T) {
	deployment := seedBrowserSession(t, fakeissuer.Options{RequireResource: true})
	seedGatewaySession(t, deployment)
	broker := gatewayBroker(t, deployment)
	broker.Capabilities.Product.Gateway = nil
	_, err := broker.Acquire(gatewayRequest())
	var denial auth.Denial
	if !errors.As(err, &denial) || denial.Problem.Code != "auth.product_not_configured" ||
		!strings.Contains(denial.Problem.Message, "gateway") {
		t.Fatalf("got %v, want auth.product_not_configured naming the gateway", err)
	}
	// An unknown record is refused the same way.
	broker = gatewayBroker(t, deployment)
	_, err = broker.Acquire(auth.Request{Audience: gatewayAudienceName, Record: "sandbox"})
	if !errors.As(err, &denial) || denial.Problem.Code != "auth.product_not_configured" {
		t.Fatalf("an unknown record: got %v, want auth.product_not_configured", err)
	}
}

func TestAGatewayTheIdentityDoesNotRecordNamesTheConnectToRun(t *testing.T) {
	deployment := seedBrowserSession(t, fakeissuer.Options{RequireResource: true})
	broker := gatewayBroker(t, deployment)
	product := broker.Selection.Identity.Products[gatewayNamespace]
	product.Gateway = nil
	broker.Selection.Identity.Products[gatewayNamespace] = product
	_, err := broker.Acquire(gatewayRequest())
	var denial auth.Denial
	if !errors.As(err, &denial) || denial.Problem.Code != "auth.product_not_configured" ||
		!strings.Contains(denial.Problem.Recovery, "wso2 "+gatewayNamespace+" connect <gateway-url> --gateway") {
		t.Fatalf("got %v, want auth.product_not_configured naming the gateway connect", err)
	}
}

// machineGatewayBroker is a client-credentials broker on ThunderID whose
// product records a gateway the descriptor declares.
func machineGatewayBroker(t *testing.T, issuer *fakeissuer.Issuer) *auth.Broker {
	t.Helper()
	broker := clientCredentialsBroker(t, issuer)
	broker.Selection.Identity.Auth.Provider = contexts.ProviderThunder
	broker.Selection.Identity.Products[gatewayNamespace] = gatewayProduct("https://product.example")
	broker.Namespace = gatewayNamespace
	broker.Capabilities.AuthAudiences = []string{gatewayAudienceName}
	broker.Capabilities.Product = gatewayDescriptor()
	return broker
}

func TestAMachineIdentityMintsTheGatewayInlineFromItsOwnClient(t *testing.T) {
	keyring.MockInit()
	issuer := fakeissuer.New(t, fakeissuer.Options{ClientSecret: "machine-secret", RequireResource: true})
	broker := machineGatewayBroker(t, issuer)
	// The management record carries the product's own credential; the
	// gateway is minted from the identity's machine client regardless.
	product := broker.Selection.Identity.Products[gatewayNamespace]
	product.ClientSecretVariable = "WSO2_ZAPIM_CLIENT_SECRET"
	broker.Selection.Identity.Products[gatewayNamespace] = product
	broker.StateRoot = t.TempDir()
	broker.Now = nil
	grant, err := broker.Acquire(gatewayRequest())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	active, scopes, audiences := issuer.Introspect(t, grant.Token)
	if !active || len(scopes) != 1 || scopes[0] != gatewayScope || audiences[0] != gatewayResource {
		t.Fatalf("token scopes %v audiences %v", scopes, audiences)
	}
	// A gateway shape that admits no inline machine client refuses.
	broker = machineGatewayBroker(t, issuer)
	broker.Capabilities.Product.Gateway.Machine = []string{modules.MachineCredential}
	_, err = broker.Acquire(gatewayRequest())
	var denial auth.Denial
	if !errors.As(err, &denial) || denial.Problem.Code != "auth.product_not_configured" {
		t.Fatalf("got %v, want auth.product_not_configured", err)
	}
}
