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

package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBootstrapRegistersAJWTClientAndPrintsTheIdentityLine(t *testing.T) {
	fake := newFakeAPIM(t)
	t.Setenv("WSO2_APIM_ADMIN_PASSWORD", "admin")

	outcome := fake.run(t, []string{"bootstrap"}, "--url", fake.server.URL+"/")

	if outcome.Problem != nil {
		t.Fatalf("%+v", outcome.Problem)
	}
	if len(outcome.AccessRequests) != 0 {
		t.Error("bootstrap asked the broker for access; it must use the administrator password only")
	}
	if len(fake.requests) != 1 {
		t.Errorf("without --login-provider bootstrap must register the confidential client and nothing else: %v", fake.requests)
	}
	registrations := fake.requestsTo("POST /client-registration/v0.17/register")
	if len(registrations) != 1 || !strings.Contains(registrations[0], `"tokenType":"JWT"`) ||
		!strings.Contains(registrations[0], `"clientName":"wso2-cli"`) ||
		!strings.Contains(registrations[0], `"callbackUrl":"http://127.0.0.1:10425/callback"`) {
		t.Errorf("registration = %v", registrations)
	}
	fields := fieldsOf(outcome)
	if fields["clientId"] != "client-1" || fields["clientSecret"] != "secret-1" ||
		fields["issuer"] != fake.server.URL+"/oauth2/token" ||
		!strings.Contains(fields["next"], "export WSO2_APIM_CLIENT_SECRET=<the client secret above") ||
		!strings.Contains(fields["next"], "wso2 apim connect "+fake.server.URL+
			" --client-id client-1 --client-secret-variable WSO2_APIM_CLIENT_SECRET") {
		t.Errorf("fields = %+v", fields)
	}
	assertEndsWithNext(t, outcome)
}

func TestBootstrapRefusesWithoutThePasswordAndOnAWrongOne(t *testing.T) {
	fake := newFakeAPIM(t)
	t.Setenv("WSO2_APIM_ADMIN_PASSWORD", "")
	outcome := fake.run(t, []string{"bootstrap"}, "--url", fake.server.URL)
	if outcome.Problem == nil || outcome.Problem.Code != "apim.missing_secret" || len(fake.requests) != 0 {
		t.Errorf("unset: %+v requests %v", outcome.Problem, fake.requests)
	}
	t.Setenv("WSO2_APIM_ADMIN_PASSWORD", "wrong")
	outcome = fake.run(t, []string{"bootstrap"}, "--url", fake.server.URL)
	if outcome.Problem == nil || outcome.Problem.Code != "apim.refused" || !strings.Contains(outcome.Problem.Message, "401") {
		t.Errorf("wrong: %+v", outcome.Problem)
	}
	outcome = fake.run(t, []string{"bootstrap"})
	if outcome.Problem == nil || outcome.Problem.Code != "apim.missing_url" {
		t.Errorf("no url: %+v", outcome.Problem)
	}
}

func TestBootstrapKeepsTheSecretOutOfEveryFieldButItsOwn(t *testing.T) {
	// The registration mints the secret and no later command can show it
	// again, so it surfaces once, in the field labelled for it. The next line
	// is the one an operator copies into a shell, so it names the variable
	// that carries the secret rather than the secret.
	fake := newFakeAPIM(t)
	t.Setenv("WSO2_APIM_ADMIN_PASSWORD", "admin")

	outcome := fake.run(t, []string{"bootstrap"}, "--url", fake.server.URL)

	if outcome.Problem != nil {
		t.Fatalf("%+v", outcome.Problem)
	}
	for name, value := range fieldsOf(outcome) {
		if name != "clientSecret" && strings.Contains(value, "secret-1") {
			t.Errorf("the field %q carries the client secret: %q", name, value)
		}
	}
}

const (
	federationSecretVariable = "WSO2_APIM_FEDERATION_CLIENT_SECRET"
	loginProvider            = "http://localhost:8492"
	internalLoginProvider    = "http://host.docker.internal:8492"
)

func federated(t *testing.T, fake *fakeAPIM, arguments ...string) (string, []string) {
	t.Helper()
	t.Setenv("WSO2_APIM_ADMIN_PASSWORD", "admin")
	t.Setenv(federationSecretVariable, "fed-secret-1")
	return fake.server.URL, append([]string{"--url", fake.server.URL, "--login-provider", loginProvider,
		"--federation-client-id", "apim-federation"}, arguments...)
}

func TestBootstrapWithALoginProviderRegistersTheIdentityProviderAndThePublicClient(t *testing.T) {
	fake := newFakeAPIM(t)
	base, arguments := federated(t, fake, "--login-provider-internal-url", internalLoginProvider)

	outcome := fake.run(t, []string{"bootstrap"}, arguments...)

	if outcome.Problem != nil {
		t.Fatalf("%+v", outcome.Problem)
	}
	fields := fieldsOf(outcome)
	if fields["identityProvider"] != "wso2-cli-localhost-8492 (created)" || fields["publicClient"] != "sso-1 (created)" ||
		fields["clientId"] != "client-1" || fields["clientSecret"] != "secret-1" {
		t.Errorf("fields = %+v", fields)
	}
	if !strings.HasPrefix(fields["next"], "Run wso2 apim connect "+base+" --client-id sso-1. ") ||
		!strings.Contains(fields["next"], "For a pipeline: export WSO2_APIM_CLIENT_SECRET=<the client secret above, shown once>; "+
			"then wso2 apim connect "+base+" --client-id client-1 --client-secret-variable WSO2_APIM_CLIENT_SECRET") ||
		strings.Contains(fields["next"], "by hand") {
		t.Errorf("next = %q", fields["next"])
	}
	for name, value := range fields {
		if strings.Contains(value, "fed-secret-1") || (name != "clientSecret" && strings.Contains(value, "secret-1")) {
			t.Errorf("the field %q carries a secret: %q", name, value)
		}
	}
	assertEndsWithNext(t, outcome)

	// The identity provider: the OpenID Connect authenticator with the
	// browser-facing authorization endpoint and the internal token and
	// userinfo endpoints, the resource parameter, the mappings and silent
	// just-in-time provisioning, all in one addIdP.
	added := fake.requestsTo("POST /services/IdentityProviderMgtService addIdP")
	if len(added) != 1 {
		t.Fatalf("addIdP = %v", fake.requests)
	}
	for _, want := range []string{
		"<m:identityProviderName>wso2-cli-localhost-8492</m:identityProviderName>",
		"<m:name>OpenIDConnectAuthenticator</m:name>",
		"<m:name>ClientId</m:name><m:value>apim-federation</m:value>",
		"<m:name>ClientSecret</m:name><m:value>fed-secret-1</m:value>",
		"<m:name>OAuth2AuthzEPUrl</m:name><m:value>" + loginProvider + "/oauth2/authorize</m:value>",
		"<m:name>OAuth2TokenEPUrl</m:name><m:value>" + internalLoginProvider + "/oauth2/token</m:value>",
		"<m:name>OIDCUserInfoEPUrl</m:name><m:value>" + internalLoginProvider + "/oauth2/userinfo</m:value>",
		"<m:name>callbackUrl</m:name><m:value>" + base + "/commonauth</m:value>",
		"<m:name>commonAuthQueryParams</m:name><m:value>scope=openid email groups&amp;resource=" + base + "/oauth2/token</m:value>",
		"<m:localClaim><m:claimUri>http://wso2.org/claims/role</m:claimUri></m:localClaim><m:remoteClaim><m:claimUri>groups</m:claimUri></m:remoteClaim>",
		"<m:localClaim><m:claimUri>http://wso2.org/claims/emailaddress</m:claimUri></m:localClaim><m:remoteClaim><m:claimUri>email</m:claimUri></m:remoteClaim>",
		"<m:userClaimURI>sub</m:userClaimURI>",
		"<m:localRole><m:localRoleName>admin</m:localRoleName></m:localRole><m:remoteRole>Administrators</m:remoteRole>",
		"<m:provisioningEnabled>true</m:provisioningEnabled><m:provisioningUserStore>PRIMARY</m:provisioningUserStore>",
		"<m:promptConsent>false</m:promptConsent>",
		"<m:defaultAuthenticatorConfig><m:displayName>openidconnect</m:displayName><m:enabled>true</m:enabled><m:name>OpenIDConnectAuthenticator</m:name></m:defaultAuthenticatorConfig>",
	} {
		if !strings.Contains(added[0], want) {
			t.Errorf("addIdP lacks %s:\n%s", want, added[0])
		}
	}
	// The public client: registered with the four loopback callbacks, then
	// made public, PKCE mandatory and JWT, then federated through the
	// identity provider with consent skipped.
	registered := fake.requestsTo("POST /api/identity/oauth2/dcr/v1.1/register")
	if len(registered) != 1 || !strings.Contains(registered[0], `"client_name":"wso2-cli-sso"`) ||
		!strings.Contains(registered[0], `"redirect_uris":["http://127.0.0.1:10425/callback","http://127.0.0.1:10426/callback",`+
			`"http://127.0.0.1:10427/callback","http://127.0.0.1:10428/callback"]`) ||
		!strings.Contains(registered[0], `"grant_types":["authorization_code","refresh_token"]`) {
		t.Errorf("registration = %v", registered)
	}
	updated := fake.requestsTo("POST /services/OAuthAdminService updateConsumerApplication")
	if len(updated) != 1 || !strings.Contains(updated[0], "<dto:oauthConsumerKey>sso-1</dto:oauthConsumerKey>") ||
		!strings.Contains(updated[0], "<dto:bypassClientCredentials>true</dto:bypassClientCredentials>") ||
		!strings.Contains(updated[0], "<dto:pkceMandatory>true</dto:pkceMandatory>") ||
		!strings.Contains(updated[0], "<dto:pkceSupportPlain>false</dto:pkceSupportPlain>") ||
		!strings.Contains(updated[0], "<dto:tokenType>JWT</dto:tokenType>") ||
		!strings.Contains(updated[0], "<dto:callbackUrl>regexp=(http://127.0.0.1:10425/callback|") {
		t.Errorf("updateConsumerApplication = %v", updated)
	}
	federated := fake.requestsTo("POST /services/IdentityApplicationManagementService updateApplication")
	if len(federated) != 1 || !strings.Contains(federated[0], "<m:applicationID>97</m:applicationID>") ||
		!strings.Contains(federated[0], "<m:inboundAuthKey>sso-1</m:inboundAuthKey><m:inboundAuthType>oauth2</m:inboundAuthType>") ||
		!strings.Contains(federated[0], "<m:identityProviderName>wso2-cli-localhost-8492</m:identityProviderName></m:federatedIdentityProviders><m:stepOrder>1</m:stepOrder>") ||
		!strings.Contains(federated[0], "<m:authenticationType>federated</m:authenticationType>") ||
		!strings.Contains(federated[0], "<m:skipConsent>true</m:skipConsent><m:skipLogoutConsent>true</m:skipLogoutConsent>") ||
		!strings.Contains(federated[0], "<m:owner><m:tenantDomain>carbon.super</m:tenantDomain><m:userName>admin</m:userName><m:userStoreDomain>PRIMARY</m:userStoreDomain></m:owner>") {
		t.Errorf("updateApplication = %v", federated)
	}

	// A rerun reads everything, finds it as written, and writes nothing.
	before := len(fake.requests)
	again := fake.run(t, []string{"bootstrap"}, arguments...)
	if again.Problem != nil {
		t.Fatalf("rerun: %+v", again.Problem)
	}
	if fields := fieldsOf(again); fields["identityProvider"] != "wso2-cli-localhost-8492 (present)" ||
		fields["publicClient"] != "sso-1 (present)" || !strings.HasPrefix(fields["next"], "Run wso2 apim connect "+base+" --client-id sso-1.") {
		t.Errorf("rerun fields = %+v", fields)
	}
	for _, request := range fake.requests[before:] {
		if !strings.HasPrefix(request, "POST /client-registration/") && !strings.HasPrefix(request, "GET ") &&
			!strings.Contains(request, " getOAuthApplicationData ") && !strings.Contains(request, " getIdPByName ") &&
			!strings.Contains(request, " getApplication ") {
			t.Errorf("the rerun wrote: %s", request)
		}
	}
}

func TestBootstrapUpdatesAnExistingIdentityProviderWithItsDefaultAuthenticatorEnabled(t *testing.T) {
	// An identity provider registered by hand, or by an earlier bootstrap
	// that trusted ThunderID's JWTs only, is brought to the federated
	// shape in place; the default authenticator entry carries enabled=true,
	// or a deployment whose service provider already references it refuses
	// with "Error in disabling default federated authenticator".
	fake := newFakeAPIM(t)
	fake.idps["Thunder3"] = strings.ReplaceAll(recordedIdentityProvider, "%[1]s", "Thunder3")
	_, arguments := federated(t, fake, "--identity-provider", "Thunder3")

	outcome := fake.run(t, []string{"bootstrap"}, arguments...)

	if outcome.Problem != nil {
		t.Fatalf("%+v", outcome.Problem)
	}
	if fields := fieldsOf(outcome); fields["identityProvider"] != "Thunder3 (present)" || fields["publicClient"] != "sso-1 (created)" {
		t.Errorf("fields = %+v", fields)
	}
	if len(fake.requestsTo("POST /services/IdentityProviderMgtService addIdP")) != 0 {
		t.Error("addIdP was called for an identity provider that exists")
	}
	updated := fake.requestsTo("POST /services/IdentityProviderMgtService updateIdP")
	if len(updated) != 1 {
		t.Fatalf("updateIdP = %v", fake.requests)
	}
	for _, want := range []string{
		"<mgt:oldIdPName>Thunder3</mgt:oldIdPName>",
		"<m:defaultAuthenticatorConfig><m:displayName>openidconnect</m:displayName><m:enabled>true</m:enabled><m:name>OpenIDConnectAuthenticator</m:name></m:defaultAuthenticatorConfig>",
		"<m:federatedAuthenticatorConfigs><m:displayName>openidconnect</m:displayName><m:enabled>true</m:enabled><m:name>OpenIDConnectAuthenticator</m:name>",
		"<m:name>OAuth2TokenEPUrl</m:name><m:value>" + loginProvider + "/oauth2/token</m:value>",
		"<m:provisioningEnabled>true</m:provisioningEnabled>",
		// What the identity provider carried and bootstrap does not set stays.
		"<m:alias>wso2-cli</m:alias>",
		"<m:idpProperties><m:name>jwksUri</m:name><m:value>http://host.docker.internal:8492/oauth2/jwks</m:value></m:idpProperties>",
	} {
		if !strings.Contains(updated[0], want) {
			t.Errorf("updateIdP lacks %s:\n%s", want, updated[0])
		}
	}
	if !strings.Contains(fake.requestsTo("POST /services/IdentityApplicationManagementService updateApplication")[0],
		"<m:identityProviderName>Thunder3</m:identityProviderName>") {
		t.Error("the public client does not federate through the named identity provider")
	}

	before := len(fake.requests)
	fake.run(t, []string{"bootstrap"}, arguments...)
	if writes := fake.requestsTo("POST /services/IdentityProviderMgtService updateIdP"); len(writes) != 1 {
		t.Errorf("the rerun updated the identity provider again: %v", fake.requests[before:])
	}
}

func TestBootstrapMapsGroupsAndNamesWhatItRegisters(t *testing.T) {
	fake := newFakeAPIM(t)
	_, arguments := federated(t, fake, "--map-group", "Developers=Internal/publisher", "--map-group", "Administrators=admin",
		"--identity-provider", "thunder", "--public-client-name", "cli-sso")

	outcome := fake.run(t, []string{"bootstrap"}, arguments...)

	if outcome.Problem != nil {
		t.Fatalf("%+v", outcome.Problem)
	}
	if fields := fieldsOf(outcome); fields["identityProvider"] != "thunder (created)" {
		t.Errorf("fields = %+v", fields)
	}
	added := fake.requestsTo("POST /services/IdentityProviderMgtService addIdP")
	if len(added) != 1 ||
		!strings.Contains(added[0], "<m:roleMappings><m:localRole><m:localRoleName>Internal/publisher</m:localRoleName></m:localRole><m:remoteRole>Developers</m:remoteRole></m:roleMappings>"+
			"<m:roleMappings><m:localRole><m:localRoleName>admin</m:localRoleName></m:localRole><m:remoteRole>Administrators</m:remoteRole></m:roleMappings>") ||
		!strings.Contains(added[0], "<m:idpRoles>Developers</m:idpRoles><m:idpRoles>Administrators</m:idpRoles>") ||
		// The internal URL defaults to the login provider.
		!strings.Contains(added[0], "<m:name>OAuth2TokenEPUrl</m:name><m:value>"+loginProvider+"/oauth2/token</m:value>") {
		t.Errorf("addIdP = %v", added)
	}
	if registered := fake.requestsTo("POST /api/identity/oauth2/dcr/v1.1/register"); len(registered) != 1 ||
		!strings.Contains(registered[0], `"client_name":"cli-sso"`) {
		t.Errorf("registration = %v", registered)
	}

	bad := fake.run(t, []string{"bootstrap"}, append(arguments, "--map-group", "Administrators")...)
	if bad.Problem == nil || bad.Problem.Code != "apim.invalid_flag" || !strings.Contains(bad.Problem.Message, "--map-group") {
		t.Errorf("malformed --map-group: %+v", bad.Problem)
	}
}

func TestBootstrapRefusesFederationWithoutTheClientIdOrItsSecret(t *testing.T) {
	// Both refusals name the iam command that makes the federation client,
	// so the order of the two commands is on screen.
	fake := newFakeAPIM(t)
	t.Setenv("WSO2_APIM_ADMIN_PASSWORD", "admin")
	t.Setenv(federationSecretVariable, "fed-secret-1")
	base := fake.server.URL

	outcome := fake.run(t, []string{"bootstrap"}, "--url", base, "--login-provider", loginProvider)
	if outcome.Problem == nil || outcome.Problem.Code != "apim.missing_flag" ||
		!strings.Contains(outcome.Problem.Message, "--federation-client-id") ||
		!strings.Contains(outcome.Problem.Recovery, "wso2 iam apps create <id> --type federation --for "+base) {
		t.Errorf("no client id: %+v", outcome.Problem)
	}

	t.Setenv(federationSecretVariable, "")
	outcome = fake.run(t, []string{"bootstrap"}, "--url", base, "--login-provider", loginProvider,
		"--federation-client-id", "apim-federation")
	if outcome.Problem == nil || outcome.Problem.Code != "apim.missing_flag" ||
		!strings.Contains(outcome.Problem.Message, federationSecretVariable) ||
		!strings.Contains(outcome.Problem.Recovery, "wso2 iam apps create apim-federation --type federation --for "+base) ||
		!strings.Contains(outcome.Problem.Recovery, "export "+federationSecretVariable+"=") {
		t.Errorf("no secret: %+v", outcome.Problem)
	}
	if len(fake.requests) != 0 {
		t.Errorf("a refused bootstrap wrote: %v", fake.requests)
	}
}

func TestBootstrapWithALoginProviderSurfacesRefusalsAndUntrustedCertificates(t *testing.T) {
	fake := newFakeAPIM(t)
	_, arguments := federated(t, fake)
	t.Setenv("WSO2_APIM_ADMIN_PASSWORD", "wrong")
	outcome := fake.run(t, []string{"bootstrap"}, arguments...)
	if outcome.Problem == nil || outcome.Problem.Code != "apim.refused" || !strings.Contains(outcome.Problem.Message, "401") {
		t.Errorf("wrong password: %+v", outcome.Problem)
	}

	selfSigned := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unreachable", http.StatusInternalServerError)
	}))
	t.Cleanup(selfSigned.Close)
	t.Setenv("WSO2_APIM_ADMIN_PASSWORD", "admin")
	t.Setenv("WSO2_CA_FILE", "")
	outcome = fake.run(t, []string{"bootstrap"}, "--url", selfSigned.URL, "--login-provider", loginProvider,
		"--federation-client-id", "apim-federation")
	if outcome.Problem == nil || outcome.Problem.Code != "apim.certificate_untrusted" ||
		!strings.Contains(outcome.Problem.Recovery, "WSO2_CA_FILE") {
		t.Errorf("untrusted certificate: %+v", outcome.Problem)
	}
}
