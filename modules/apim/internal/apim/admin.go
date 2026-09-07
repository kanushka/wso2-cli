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

package apim

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Admin speaks to API Manager's identity administration with the
// administrator's basic credentials: dynamic client registration and the
// three SOAP services that hold what the REST APIs do not expose, which is
// a service provider's authentication steps and an identity provider's
// federated authenticator. Every write has a read beside it, so a handler
// writes only when the read differs. The request and response shapes here
// are the ones API Manager 4.7.0 answered on 2026-09-06.
type Admin struct {
	client         *Client
	user, password string
}

// Admin returns the administration client on this deployment.
func (c *Client) Admin(user, password string) *Admin {
	return &Admin{client: c, user: user, password: password}
}

const (
	dcrPath                 = "/api/identity/oauth2/dcr/v1.1/register"
	oauthAdminService       = "/services/OAuthAdminService"
	identityProviderService = "/services/IdentityProviderMgtService"
	applicationService      = "/services/IdentityApplicationManagementService"

	// OpenIDConnectAuthenticator is API Manager's name for the federated
	// authenticator that signs a user in at an OpenID Connect provider.
	OpenIDConnectAuthenticator = "OpenIDConnectAuthenticator"
	openIDConnectDisplayName   = "openidconnect"
	// PrimaryUserStore is where just-in-time provisioning writes.
	PrimaryUserStore = "PRIMARY"
	// RoleClaim and EmailClaim are the local claims a federated group and
	// email land on.
	RoleClaim  = "http://wso2.org/claims/role"
	EmailClaim = "http://wso2.org/claims/emailaddress"
)

// RegisteredClient is what dynamic client registration knows of a client.
type RegisteredClient struct {
	ID     string `json:"client_id"`
	Secret string `json:"client_secret"`
	Name   string `json:"client_name"`
}

// FindClient looks a registered client up by name; found is false when the
// registration has none. API Manager 4.7.0 answers that lookup with 401
// rather than 404 (measured 2026-09-07), the same status a wrong password
// gets, so both are read as absent here and a bad password is reported by
// the registration that follows.
func (a *Admin) FindClient(ctx context.Context, name string) (RegisteredClient, bool, error) {
	var client RegisteredClient
	err := a.getBasic(ctx, dcrPath+"?client_name="+url.QueryEscape(name), &client)
	var refusal *Refusal
	if errors.As(err, &refusal) && (refusal.Status == http.StatusNotFound || refusal.Status == http.StatusUnauthorized) {
		return client, false, nil
	}
	return client, err == nil, err
}

// RegisterClient registers a client under name with the redirect URIs and
// grant types given.
func (a *Admin) RegisterClient(ctx context.Context, name string, redirects, grants []string) (RegisteredClient, error) {
	var client RegisteredClient
	err := a.client.PostBasic(ctx, dcrPath, a.user, a.password, map[string]any{
		"client_name": name, "redirect_uris": redirects, "grant_types": grants}, &client)
	return client, err
}

// OAuthApplication is the part of an OAuth consumer application that
// bootstrap reads and sets: whether it is public, whether PKCE is required
// and which token type it issues. The rest is carried through an update
// unchanged.
type OAuthApplication struct {
	Name, ConsumerKey, ConsumerSecret, Callback, GrantTypes, Username string
	// Public is bypassClientCredentials: the client presents no secret.
	Public        bool
	PKCEMandatory bool
	TokenType     string
	expiries      map[string]string
}

// oauthExpiries are the lifetimes an update must carry back, or the
// service resets them.
var oauthExpiries = []string{"applicationAccessTokenExpiryTime", "idTokenExpiryTime",
	"refreshTokenExpiryTime", "userAccessTokenExpiryTime"}

// OAuthApplication reads the application behind a consumer key.
func (a *Admin) OAuthApplication(ctx context.Context, consumerKey string) (*OAuthApplication, error) {
	body := soapElement("xsd:getOAuthApplicationData", soapElement("xsd:consumerKey", text(consumerKey)))
	answer, err := a.soap(ctx, oauthAdminService, "getOAuthApplicationData",
		`xmlns:xsd="http://org.apache.axis2/xsd"`, body)
	if err != nil {
		return nil, err
	}
	app := &OAuthApplication{
		Name: answer.text("applicationName"), ConsumerKey: answer.text("oauthConsumerKey"),
		ConsumerSecret: answer.text("oauthConsumerSecret"), Callback: answer.text("callbackUrl"),
		GrantTypes: answer.text("grantTypes"), Username: answer.text("username"),
		Public: answer.text("bypassClientCredentials") == "true", PKCEMandatory: answer.text("pkceMandatory") == "true",
		TokenType: answer.text("tokenType"), expiries: map[string]string{},
	}
	for _, name := range oauthExpiries {
		app.expiries[name] = answer.text(name)
	}
	return app, nil
}

// UpdateOAuthApplication writes the application back with its flags as set.
func (a *Admin) UpdateOAuthApplication(ctx context.Context, app *OAuthApplication) error {
	expiry := func(name, fallback string) string {
		if value := app.expiries[name]; value != "" {
			return text(value)
		}
		return fallback
	}
	dto := soapElement("dto:OAuthVersion", "OAuth-2.0") +
		soapElement("dto:applicationAccessTokenExpiryTime", expiry("applicationAccessTokenExpiryTime", "3600")) +
		soapElement("dto:applicationName", text(app.Name)) +
		soapElement("dto:bypassClientCredentials", flag(app.Public)) +
		soapElement("dto:callbackUrl", text(app.Callback)) +
		soapElement("dto:grantTypes", text(app.GrantTypes)) +
		soapElement("dto:idTokenExpiryTime", expiry("idTokenExpiryTime", "3600")) +
		soapElement("dto:oauthConsumerKey", text(app.ConsumerKey)) +
		soapElement("dto:oauthConsumerSecret", text(app.ConsumerSecret)) +
		soapElement("dto:pkceMandatory", flag(app.PKCEMandatory)) +
		soapElement("dto:pkceSupportPlain", "false") +
		soapElement("dto:refreshTokenExpiryTime", expiry("refreshTokenExpiryTime", "86400")) +
		soapElement("dto:tokenType", text(app.TokenType)) +
		soapElement("dto:userAccessTokenExpiryTime", expiry("userAccessTokenExpiryTime", "3600")) +
		soapElement("dto:username", text(app.Username))
	body := soapElement("xsd:updateConsumerApplication", soapElement("xsd:consumerAppDTO", dto))
	_, err := a.soap(ctx, oauthAdminService, "updateConsumerApplication",
		`xmlns:xsd="http://org.apache.axis2/xsd" xmlns:dto="http://dto.oauth.identity.carbon.wso2.org/xsd"`, body)
	return err
}

// OpenIDConnect is the federated authenticator on an identity provider:
// the client it signs in through and where.
type OpenIDConnect struct {
	ClientID, ClientSecret                                           string
	AuthorizationEndpoint, TokenEndpoint, UserInfoEndpoint, Callback string
	// QueryParameters is commonAuthQueryParams, appended to the
	// authorization request.
	QueryParameters string
	Scopes          string
}

// Mapping is one remote-to-local pair: a claim or a role.
type Mapping struct{ Remote, Local string }

// NameValue is one identity provider property.
type NameValue struct{ Name, Value string }

// IdentityProvider is an identity provider as bootstrap shapes it: the
// OpenID Connect authenticator, the claim and role mappings, and
// just-in-time provisioning. Alias and Properties are read and carried
// through an update so that what bootstrap does not set is kept.
type IdentityProvider struct {
	Name, Description string
	Authenticator     OpenIDConnect
	ClaimMappings     []Mapping
	UserClaim         string
	RoleMappings      []Mapping
	JITProvisioning   bool
	Alias             string
	Properties        []NameValue
}

// IdentityProvider reads the identity provider of that name; found is false
// when there is none.
func (a *Admin) IdentityProvider(ctx context.Context, name string) (*IdentityProvider, bool, error) {
	body := soapElement("mgt:getIdPByName", soapElement("mgt:idPName", text(name)))
	answer, err := a.soap(ctx, identityProviderService, "getIdPByName",
		`xmlns:mgt="http://mgt.idp.carbon.wso2.org"`, body)
	if err != nil {
		return nil, false, err
	}
	if answer.text("identityProviderName") != name {
		return nil, false, nil
	}
	idp := &IdentityProvider{Name: name, Description: answer.text("identityProviderDescription"),
		Alias: answer.text("alias"), UserClaim: answer.text("userClaimURI")}
	for _, authenticator := range answer.all("federatedAuthenticatorConfigs") {
		if authenticator.text("name") != OpenIDConnectAuthenticator {
			continue
		}
		properties := map[string]string{}
		for _, property := range authenticator.all("properties") {
			properties[property.text("name")] = property.text("value")
		}
		idp.Authenticator = OpenIDConnect{
			ClientID: properties["ClientId"], ClientSecret: properties["ClientSecret"],
			AuthorizationEndpoint: properties["OAuth2AuthzEPUrl"], TokenEndpoint: properties["OAuth2TokenEPUrl"],
			UserInfoEndpoint: properties["OIDCUserInfoEPUrl"], Callback: properties["callbackUrl"],
			QueryParameters: properties["commonAuthQueryParams"], Scopes: properties["Scopes"],
		}
	}
	if claims := answer.first("claimConfig"); claims != nil {
		for _, mapping := range claims.all("claimMappings") {
			idp.ClaimMappings = append(idp.ClaimMappings, Mapping{
				Remote: mapping.first("remoteClaim").text("claimUri"), Local: mapping.first("localClaim").text("claimUri")})
		}
	}
	if roles := answer.first("permissionAndRoleConfig"); roles != nil {
		for _, mapping := range roles.all("roleMappings") {
			idp.RoleMappings = append(idp.RoleMappings, Mapping{
				Remote: mapping.text("remoteRole"), Local: mapping.first("localRole").text("localRoleName")})
		}
	}
	if jit := answer.first("justInTimeProvisioningConfig"); jit != nil {
		idp.JITProvisioning = jit.text("provisioningEnabled") == "true"
	}
	for _, property := range answer.all("idpProperties") {
		idp.Properties = append(idp.Properties, NameValue{Name: property.text("name"), Value: property.text("value")})
	}
	return idp, true, nil
}

// Federates reports whether this identity provider already carries what
// want sets: the authenticator, the mappings and provisioning. What
// bootstrap carries through unchanged is not compared.
// The comparison includes the authenticator's client secret, which API
// Manager 4.7.0 returns in clear from getIdPByName (measured 2026-09-07: a
// rerun wrote nothing). A deployment that masked it would update on every
// run while still reporting the provider present.
func (i *IdentityProvider) Federates(want *IdentityProvider) bool {
	return i.Authenticator == want.Authenticator && i.UserClaim == want.UserClaim &&
		i.JITProvisioning == want.JITProvisioning &&
		sameMappings(i.ClaimMappings, want.ClaimMappings) && sameMappings(i.RoleMappings, want.RoleMappings)
}

func sameMappings(have, want []Mapping) bool {
	if len(have) != len(want) {
		return false
	}
	for index := range have {
		if have[index] != want[index] {
			return false
		}
	}
	return true
}

// AddIdentityProvider registers the identity provider.
func (a *Admin) AddIdentityProvider(ctx context.Context, idp *IdentityProvider) error {
	body := soapElement("mgt:addIdP", soapElement("mgt:identityProvider", identityProviderXML(idp)))
	_, err := a.soap(ctx, identityProviderService, "addIdP", identityProviderNamespaces, body)
	return err
}

// UpdateIdentityProvider rewrites the identity provider of that name. The
// default authenticator entry says enabled, because the service refuses
// to disable it while a service provider references the provider.
func (a *Admin) UpdateIdentityProvider(ctx context.Context, idp *IdentityProvider) error {
	body := soapElement("mgt:updateIdP", soapElement("mgt:oldIdPName", text(idp.Name))+
		soapElement("mgt:identityProvider", identityProviderXML(idp)))
	_, err := a.soap(ctx, identityProviderService, "updateIdP", identityProviderNamespaces, body)
	return err
}

const identityProviderNamespaces = `xmlns:mgt="http://mgt.idp.carbon.wso2.org" ` +
	`xmlns:m="http://model.common.application.identity.carbon.wso2.org/xsd"`

// identityProviderXML is the model element in the order the service reads
// it, which is alphabetical; an element out of order is silently dropped.
func identityProviderXML(idp *IdentityProvider) string {
	var claims, idpClaims strings.Builder
	for _, mapping := range idp.ClaimMappings {
		claims.WriteString(soapElement("m:claimMappings",
			soapElement("m:localClaim", soapElement("m:claimUri", text(mapping.Local)))+
				soapElement("m:remoteClaim", soapElement("m:claimUri", text(mapping.Remote)))))
		idpClaims.WriteString(soapElement("m:idpClaims", soapElement("m:claimUri", text(mapping.Remote))))
	}
	if idp.UserClaim != "" {
		idpClaims.WriteString(soapElement("m:idpClaims", soapElement("m:claimUri", text(idp.UserClaim))))
	}
	roleClaim := ""
	for _, mapping := range idp.ClaimMappings {
		if mapping.Local == RoleClaim {
			roleClaim = mapping.Remote
		}
	}
	claimConfig := claims.String() + idpClaims.String() + soapElement("m:localClaimDialect", "false") +
		soapElement("m:roleClaimURI", text(roleClaim)) + soapElement("m:userClaimURI", text(idp.UserClaim))
	authenticatorHead := soapElement("m:displayName", openIDConnectDisplayName) + soapElement("m:enabled", "true") +
		soapElement("m:name", OpenIDConnectAuthenticator)
	var properties strings.Builder
	for _, property := range []NameValue{
		{"ClientId", idp.Authenticator.ClientID}, {"ClientSecret", idp.Authenticator.ClientSecret},
		{"OAuth2AuthzEPUrl", idp.Authenticator.AuthorizationEndpoint}, {"OAuth2TokenEPUrl", idp.Authenticator.TokenEndpoint},
		{"OIDCUserInfoEPUrl", idp.Authenticator.UserInfoEndpoint}, {"callbackUrl", idp.Authenticator.Callback},
		{"commonAuthQueryParams", idp.Authenticator.QueryParameters}, {"Scopes", idp.Authenticator.Scopes},
		{"IsBasicAuthEnabled", "false"}, {"IsUserIdInClaims", "false"},
	} {
		properties.WriteString(soapElement("m:properties", soapElement("m:name", property.Name)+soapElement("m:value", text(property.Value))))
	}
	var idpProperties strings.Builder
	for _, property := range idp.Properties {
		idpProperties.WriteString(soapElement("m:idpProperties",
			soapElement("m:name", text(property.Name))+soapElement("m:value", text(property.Value))))
	}
	// The order the deployment accepted on 2026-09-06.
	jit := soapElement("m:dumbMode", "false") + soapElement("m:provisioningEnabled", flag(idp.JITProvisioning)) +
		soapElement("m:provisioningUserStore", PrimaryUserStore) + soapElement("m:associateLocalUserEnabled", "false") +
		soapElement("m:modifyUserNameAllowed", "false") + soapElement("m:passwordProvisioningEnabled", "false") +
		soapElement("m:promptConsent", "false")
	var idpRoles, roleMappings strings.Builder
	for _, mapping := range idp.RoleMappings {
		idpRoles.WriteString(soapElement("m:idpRoles", text(mapping.Remote)))
		roleMappings.WriteString(soapElement("m:roleMappings",
			soapElement("m:localRole", soapElement("m:localRoleName", text(mapping.Local)))+
				soapElement("m:remoteRole", text(mapping.Remote))))
	}
	alias := ""
	if idp.Alias != "" {
		alias = soapElement("m:alias", text(idp.Alias))
	}
	return alias +
		soapElement("m:claimConfig", claimConfig) +
		soapElement("m:defaultAuthenticatorConfig", authenticatorHead) +
		soapElement("m:displayName", text(idp.Name)) +
		soapElement("m:enable", "true") +
		soapElement("m:federatedAuthenticatorConfigs", authenticatorHead+properties.String()) +
		soapElement("m:identityProviderDescription", text(idp.Description)) +
		soapElement("m:identityProviderName", text(idp.Name)) +
		idpProperties.String() +
		soapElement("m:justInTimeProvisioningConfig", jit) +
		soapElement("m:permissionAndRoleConfig", idpRoles.String()+roleMappings.String())
}

// ServiceProvider is an application as the application management service
// holds it: the OAuth client it fronts and how it authenticates.
type ServiceProvider struct {
	ID, Name, ResourceID, Description, TenantDomain string
	ClientID                                        string
	OwnerTenant, OwnerName, OwnerStore              string
	// AuthenticationType is "federated" once the step below is set.
	AuthenticationType string
	// FederatedThrough is the identity provider of the single step.
	FederatedThrough string
	SkipConsent      bool
}

// Federates reports whether this application already signs in through
// the identity provider, consent skipped.
func (s *ServiceProvider) Federates(identityProvider string) bool {
	return s.AuthenticationType == "federated" && s.FederatedThrough == identityProvider && s.SkipConsent
}

// ServiceProvider reads the application of that name; found is false when
// there is none.
func (a *Admin) ServiceProvider(ctx context.Context, name string) (*ServiceProvider, bool, error) {
	body := soapElement("xsd:getApplication", soapElement("xsd:applicationName", text(name)))
	answer, err := a.soap(ctx, applicationService, "getApplication", `xmlns:xsd="http://org.apache.axis2/xsd"`, body)
	if err != nil {
		return nil, false, err
	}
	if answer.text("applicationName") != name {
		return nil, false, nil
	}
	sp := &ServiceProvider{ID: answer.text("applicationID"), Name: name, ResourceID: answer.text("applicationResourceId"),
		Description: answer.text("description"), TenantDomain: answer.text("tenantDomain")}
	for _, inbound := range answer.all("inboundAuthenticationRequestConfigs") {
		if inbound.text("inboundAuthType") == "oauth2" {
			sp.ClientID = inbound.text("inboundAuthKey")
		}
	}
	if owner := answer.first("owner"); owner != nil {
		sp.OwnerTenant, sp.OwnerName, sp.OwnerStore = owner.text("tenantDomain"), owner.text("userName"), owner.text("userStoreDomain")
	}
	if auth := answer.first("localAndOutBoundAuthenticationConfig"); auth != nil {
		sp.AuthenticationType = auth.text("authenticationType")
		sp.SkipConsent = auth.text("skipConsent") == "true"
		for _, step := range auth.all("authenticationSteps") {
			if step.text("stepOrder") == "1" {
				sp.FederatedThrough = step.first("federatedIdentityProviders").text("identityProviderName")
			}
		}
	}
	return sp, true, nil
}

// UpdateServiceProvider writes the application back with one federated
// authentication step through sp.FederatedThrough and consent skipped.
func (a *Admin) UpdateServiceProvider(ctx context.Context, sp *ServiceProvider) error {
	authenticator := soapElement("m:displayName", openIDConnectDisplayName) + soapElement("m:enabled", "true") +
		soapElement("m:name", OpenIDConnectAuthenticator)
	step := soapElement("m:attributeStep", "true") +
		soapElement("m:federatedIdentityProviders", soapElement("m:defaultAuthenticatorConfig", authenticator)+
			soapElement("m:federatedAuthenticatorConfigs", authenticator)+
			soapElement("m:identityProviderName", text(sp.FederatedThrough))) +
		soapElement("m:stepOrder", "1") + soapElement("m:subjectStep", "true")
	authentication := soapElement("m:alwaysSendBackAuthenticatedListOfIdPs", "false") +
		soapElement("m:authenticationSteps", step) +
		soapElement("m:authenticationType", "federated") + soapElement("m:enableAuthorization", "false") +
		soapElement("m:skipConsent", "true") + soapElement("m:skipLogoutConsent", "true") +
		soapElement("m:useTenantDomainInLocalSubjectIdentifier", "false") +
		soapElement("m:useUserstoreDomainInLocalSubjectIdentifier", "false") +
		soapElement("m:useUserstoreDomainInRoles", "true")
	resourceID := ""
	if sp.ResourceID != "" {
		resourceID = soapElement("m:applicationResourceId", text(sp.ResourceID))
	}
	provider := soapElement("m:applicationID", text(sp.ID)) + soapElement("m:applicationName", text(sp.Name)) + resourceID +
		soapElement("m:claimConfig", soapElement("m:alwaysSendMappedLocalSubjectId", "false")+
			soapElement("m:localClaimDialect", "true")+soapElement("m:roleClaimURI", RoleClaim)) +
		soapElement("m:description", text(sp.Description)) + soapElement("m:discoverable", "false") +
		soapElement("m:inboundAuthenticationConfig", soapElement("m:inboundAuthenticationRequestConfigs",
			soapElement("m:inboundAuthKey", text(sp.ClientID))+soapElement("m:inboundAuthType", "oauth2")+
				soapElement("m:inboundConfigType", "standardAPP"))) +
		soapElement("m:inboundProvisioningConfig", soapElement("m:dumbMode", "false")+soapElement("m:provisioningEnabled", "false")) +
		soapElement("m:localAndOutBoundAuthenticationConfig", authentication) +
		soapElement("m:managementApp", "false") + soapElement("m:outboundProvisioningConfig", "") +
		soapElement("m:owner", soapElement("m:tenantDomain", text(sp.OwnerTenant))+soapElement("m:userName", text(sp.OwnerName))+
			soapElement("m:userStoreDomain", text(sp.OwnerStore))) +
		soapElement("m:permissionAndRoleConfig", "") + soapElement("m:saasApp", "false") +
		soapElement("m:spProperties", soapElement("m:name", "skipConsent")+soapElement("m:value", "true")) +
		soapElement("m:spProperties", soapElement("m:name", "skipLogoutConsent")+soapElement("m:value", "true")) +
		soapElement("m:tenantDomain", text(sp.TenantDomain))
	body := soapElement("xsd:updateApplication", soapElement("xsd:serviceProvider", provider))
	_, err := a.soap(ctx, applicationService, "updateApplication",
		`xmlns:xsd="http://org.apache.axis2/xsd" xmlns:m="http://model.common.application.identity.carbon.wso2.org/xsd"`, body)
	return err
}

// soap posts one operation to a service and parses the answer. A SOAP
// fault comes back as a 500 with the fault in the body, so it is a
// Refusal carrying the service's own words, as a REST refusal is.
func (a *Admin) soap(ctx context.Context, service, action, namespaces, body string) (*element, error) {
	envelope := `<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/" ` + namespaces + `>` +
		`<soapenv:Body>` + body + `</soapenv:Body></soapenv:Envelope>`
	call, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(call, http.MethodPost, a.client.Base+service, strings.NewReader(envelope))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "text/xml;charset=UTF-8")
	request.Header.Set("SOAPAction", "urn:"+action)
	request.SetBasicAuth(a.user, a.password)
	response, err := a.client.HTTP.Do(request)
	if err != nil {
		return nil, classifyTransport(a.client.Base, err)
	}
	defer response.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, &Refusal{Status: response.StatusCode, Body: faultText(answer)}
	}
	parsed, err := parseXML(answer)
	if err != nil {
		return nil, &unreadable{err: err}
	}
	return parsed, nil
}

// faultText is the faultstring of a SOAP fault, or the body as it came.
func faultText(answer []byte) string {
	if parsed, err := parseXML(answer); err == nil {
		if fault := parsed.text("faultstring"); fault != "" {
			return fault
		}
	}
	return string(answer)
}

// getBasic reads JSON under HTTP basic credentials.
func (a *Admin) getBasic(ctx context.Context, path string, into any) error {
	call, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(call, http.MethodGet, a.client.Base+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.SetBasicAuth(a.user, a.password)
	return a.client.read(request, into)
}

// element is one parsed XML element, prefixes dropped: the services answer
// with generated prefixes that differ between deployments and versions.
type element struct {
	name     string
	body     string
	children []*element
}

func parseXML(raw []byte) (*element, error) {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	root := &element{}
	stack := []*element{root}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := token.(type) {
		case xml.StartElement:
			child := &element{name: t.Name.Local}
			parent := stack[len(stack)-1]
			parent.children = append(parent.children, child)
			stack = append(stack, child)
		case xml.CharData:
			stack[len(stack)-1].body += string(t)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		}
	}
	return root, nil
}

// first is the first descendant with that local name, depth first, or
// nil.
func (e *element) first(name string) *element {
	if e == nil {
		return nil
	}
	for _, child := range e.children {
		if child.name == name {
			return child
		}
		if found := child.first(name); found != nil {
			return found
		}
	}
	return nil
}

// all is every direct child with that local name, or, when there is
// none, every such element under the first descendant that has one.
func (e *element) all(name string) []*element {
	if e == nil {
		return nil
	}
	var matching []*element
	for _, child := range e.children {
		if child.name == name {
			matching = append(matching, child)
		}
	}
	if len(matching) > 0 {
		return matching
	}
	for _, child := range e.children {
		if found := child.all(name); len(found) > 0 {
			return found
		}
	}
	return nil
}

// text is the trimmed text of the first descendant with that name.
func (e *element) text(name string) string {
	found := e.first(name)
	if found == nil {
		return ""
	}
	return strings.TrimSpace(found.body)
}

// Encoding of what goes out: the value already escaped.
func soapElement(name, content string) string {
	return "<" + name + ">" + content + "</" + name + ">"
}

func text(value string) string {
	var buffer bytes.Buffer
	_ = xml.EscapeText(&buffer, []byte(value))
	return buffer.String()
}

func flag(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
