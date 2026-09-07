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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/testkit"
)

const fixtureToken = "wso2-development-token.fixture"

// fakeAPIM is enough of API Manager for this module: dynamic client
// registration, the publisher, devportal and admin planes, and the key
// manager's token endpoint. It records every request so tests can assert
// bodies, and it can be seeded.
type fakeAPIM struct {
	mu     sync.Mutex
	server *httptest.Server
	// gateway is the gateway's own origin, apart from the management planes
	// as the deployment keeps them.
	gateway *httptest.Server
	// endpoint is what the identity's apim product records: the management
	// origin.
	endpoint string
	// gatewayEndpoint is what the identity's gateway record names, when a
	// test records one; empty otherwise, as after wso2 apim connect alone.
	gatewayEndpoint string
	apis            []map[string]any
	deployments     map[string][]map[string]any
	apps            []map[string]any
	subs            []map[string]any
	keys            map[string][]map[string]any
	kms             []map[string]any
	requests        []string // "METHOD path body"
	seq             int
	// mapKeysRefusals makes map-keys answer "Key Manager not Registered" this
	// many times before succeeding, as the deployment does briefly.
	mapKeysRefusals int
	// deployPolls counts GET deployments before reporting success.
	deployPolls int
	// gatewayStatus and gatewayBody, when set, are what the gateway answers
	// every call with, as it does when a subscription check fails.
	gatewayStatus int
	gatewayBody   string
	// listLag hides every API from this many listings, as the search index does
	// for a few seconds after a creation.
	listLag int
	// What bootstrap --login-provider writes through the identity
	// administration services, keyed the way each service looks them up:
	// registered clients by name, OAuth applications by consumer key,
	// identity providers by name (the identityProvider fragment as sent),
	// service providers by application name (the serviceProvider fragment).
	dcr       map[string]map[string]any
	oauthApps map[string]map[string]string
	idps      map[string]string
	sps       map[string]string
}

func newFakeAPIM(t *testing.T) *fakeAPIM {
	t.Helper()
	fake := &fakeAPIM{deployments: map[string][]map[string]any{}, keys: map[string][]map[string]any{},
		dcr: map[string]map[string]any{}, oauthApps: map[string]map[string]string{},
		idps: map[string]string{}, sps: map[string]string{}}
	id := func() string { fake.seq++; return fmt.Sprintf("id-%d", fake.seq) }
	record := func(r *http.Request) map[string]any {
		raw, _ := io.ReadAll(r.Body)
		fake.requests = append(fake.requests, r.Method+" "+r.URL.RequestURI()+" "+string(raw))
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		return body
	}
	list := func(w http.ResponseWriter, items []map[string]any) {
		_ = json.NewEncoder(w).Encode(map[string]any{"count": len(items), "list": items})
	}
	bearer := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+fixtureToken {
				http.Error(w, `{"code":401,"message":"Unauthenticated request"}`, http.StatusUnauthorized)
				return
			}
			fake.mu.Lock()
			defer fake.mu.Unlock()
			next(w, r)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /client-registration/v0.17/register", func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		user, password, _ := r.BasicAuth()
		body := record(r)
		if user != "admin" || password != "admin" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"clientId": "client-1", "clientSecret": "secret-1",
			"clientName": body["clientName"], "tokenType": body["tokenType"]})
	})
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		record(r)
		user, password, _ := r.BasicAuth()
		if user != "ck-1" || password != "cs-1" {
			http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "a.b.c", "expires_in": 3600})
	})
	// publisher
	mux.HandleFunc("GET /api/am/publisher/v4/apis", bearer(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		query := r.URL.Query().Get("query")
		var matching []map[string]any
		if fake.listLag > 0 {
			// The search index has not caught up yet.
			fake.listLag--
			list(w, matching)
			return
		}
		for _, api := range fake.apis {
			if query == "" || query == "name:"+api["name"].(string) {
				matching = append(matching, api)
			}
		}
		list(w, matching)
	}))
	mux.HandleFunc("POST /api/am/publisher/v4/apis/import-openapi", bearer(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "no file", http.StatusBadRequest)
			return
		}
		content, _ := io.ReadAll(file)
		fake.requests = append(fake.requests, "POST /api/am/publisher/v4/apis/import-openapi "+
			header.Filename+" "+string(content)+" "+r.FormValue("additionalProperties"))
		var props map[string]any
		_ = json.Unmarshal([]byte(r.FormValue("additionalProperties")), &props)
		props["id"] = id()
		props["lifeCycleStatus"] = "CREATED"
		fake.apis = append(fake.apis, props)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(props)
	}))
	mux.HandleFunc("POST /api/am/publisher/v4/apis/{id}/revisions", bearer(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "rev-" + r.PathValue("id"), "displayName": "Revision 1"})
	}))
	mux.HandleFunc("POST /api/am/publisher/v4/apis/{id}/deploy-revision", bearer(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		fake.deployments[r.PathValue("id")] = []map[string]any{{"name": "Default", "status": "APPROVED"}}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`[{"name":"Default","status":"APPROVED"}]`))
	}))
	mux.HandleFunc("GET /api/am/publisher/v4/apis/{id}/deployments", bearer(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		deployed := fake.deployments[r.PathValue("id")]
		if fake.deployPolls > 0 {
			fake.deployPolls--
			_, _ = w.Write([]byte(`[{"name":"Default","successDeployedTime":0}]`))
			return
		}
		for _, d := range deployed {
			d["successDeployedTime"] = 1788475966000
		}
		_ = json.NewEncoder(w).Encode(deployed)
	}))
	mux.HandleFunc("POST /api/am/publisher/v4/apis/change-lifecycle", bearer(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		for _, api := range fake.apis {
			if api["id"] == r.URL.Query().Get("apiId") {
				api["lifeCycleStatus"] = "PUBLISHED"
			}
		}
		_, _ = w.Write([]byte(`{"lifecycleState":{"state":"Published"}}`))
	}))
	// devportal
	mux.HandleFunc("GET /api/am/devportal/v3/apis", bearer(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		list(w, fake.apis)
	}))
	mux.HandleFunc("GET /api/am/devportal/v3/applications", bearer(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		list(w, fake.apps)
	}))
	mux.HandleFunc("POST /api/am/devportal/v3/applications", bearer(func(w http.ResponseWriter, r *http.Request) {
		body := record(r)
		body["applicationId"] = id()
		fake.apps = append(fake.apps, body)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(body)
	}))
	mux.HandleFunc("POST /api/am/devportal/v3/subscriptions", bearer(func(w http.ResponseWriter, r *http.Request) {
		body := record(r)
		for _, sub := range fake.subs {
			if sub["applicationId"] == body["applicationId"] && sub["apiId"] == body["apiId"] {
				http.Error(w, `{"code":409,"description":"Specified subscription already exists"}`, http.StatusConflict)
				return
			}
		}
		body["subscriptionId"] = id()
		body["status"] = "UNBLOCKED"
		fake.subs = append(fake.subs, body)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(body)
	}))
	mux.HandleFunc("POST /api/am/devportal/v3/applications/{id}/generate-keys", bearer(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		key := map[string]any{"keyMappingId": id(), "keyType": "PRODUCTION", "keyState": "APPROVED",
			"consumerKey": "ck-1", "consumerSecret": "cs-1", "keyManager": "Resident Key Manager"}
		fake.keys[r.PathValue("id")] = append(fake.keys[r.PathValue("id")], key)
		_ = json.NewEncoder(w).Encode(key)
	}))
	mux.HandleFunc("POST /api/am/devportal/v3/applications/{id}/map-keys", bearer(func(w http.ResponseWriter, r *http.Request) {
		body := record(r)
		if fake.mapKeysRefusals > 0 {
			fake.mapKeysRefusals--
			http.Error(w, `{"code":500,"description":"Key Manager not Registered"}`, http.StatusInternalServerError)
			return
		}
		name := ""
		for _, app := range fake.apps {
			if app["applicationId"] == r.PathValue("id") {
				name = app["name"].(string)
			}
		}
		for _, mapped := range fake.keys {
			for _, key := range mapped {
				if key["keyManager"] == body["keyManager"] && key["consumerKey"] == body["consumerKey"] {
					// What API Manager 4.7.0 says when the client is mapped
					// already, whichever application holds the mapping.
					http.Error(w, `{"code":901409,"message":"Key Mappings already exists","description":"Key Mappings already exists for application `+
						name+` or consumer key `+body["consumerKey"].(string)+`","moreInfo":"","error":[]}`, http.StatusConflict)
					return
				}
			}
		}
		body["mode"] = "MAPPED"
		body["keyMappingId"] = id()
		fake.keys[r.PathValue("id")] = append(fake.keys[r.PathValue("id")], body)
		_ = json.NewEncoder(w).Encode(body)
	}))
	mux.HandleFunc("GET /api/am/devportal/v3/applications/{id}/oauth-keys", bearer(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		list(w, fake.keys[r.PathValue("id")])
	}))
	// admin
	mux.HandleFunc("GET /api/am/admin/v4/key-managers", bearer(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		list(w, fake.kms)
	}))
	mux.HandleFunc("GET /api/am/admin/v4/key-managers/{id}", bearer(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		for _, manager := range fake.kms {
			if manager["id"] == r.PathValue("id") {
				_ = json.NewEncoder(w).Encode(manager)
				return
			}
		}
		// 4.7.0 answers an unknown client name with 401, not 404.
		http.Error(w, `{"code":401}`, http.StatusUnauthorized)
	}))
	mux.HandleFunc("POST /api/am/admin/v4/key-managers", bearer(func(w http.ResponseWriter, r *http.Request) {
		body := record(r)
		if body["tokenEndpoint"] == nil || body["revokeEndpoint"] == nil {
			http.Error(w, `{"code":901401,"message":"Required Key Manager configuration missing"}`, http.StatusBadRequest)
			return
		}
		body["id"] = id()
		fake.kms = append(fake.kms, body)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(body)
	}))
	// The identity administration: dynamic client registration and the three
	// SOAP services, each under the administrator's basic credentials.
	basic := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			fake.mu.Lock()
			defer fake.mu.Unlock()
			user, password, _ := r.BasicAuth()
			if user != "admin" || password != "admin" {
				fake.requests = append(fake.requests, r.Method+" "+r.URL.RequestURI()+" (unauthorized)")
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("GET /api/identity/oauth2/dcr/v1.1/register", basic(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		client, ok := fake.dcr[r.URL.Query().Get("client_name")]
		if !ok {
			http.Error(w, `{"error":"invalid_client_metadata","error_description":"Application not available"}`, http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(client)
	}))
	mux.HandleFunc("POST /api/identity/oauth2/dcr/v1.1/register", basic(func(w http.ResponseWriter, r *http.Request) {
		body := record(r)
		name, _ := body["client_name"].(string)
		if _, exists := fake.dcr[name]; exists {
			http.Error(w, `{"error":"invalid_client_metadata","error_description":"Application with the name `+name+` already exist"}`, http.StatusBadRequest)
			return
		}
		var callbacks []string
		if uris, ok := body["redirect_uris"].([]any); ok {
			for _, uri := range uris {
				callbacks = append(callbacks, uri.(string))
			}
		}
		var grants []string
		if types, ok := body["grant_types"].([]any); ok {
			for _, grant := range types {
				grants = append(grants, grant.(string))
			}
		}
		key := "sso-" + fmt.Sprint(len(fake.dcr)+1)
		client := map[string]any{"client_id": key, "client_secret": key + "-secret", "client_name": name,
			"redirect_uris": []string{"regexp=(" + strings.Join(callbacks, "|") + ")"}, "grant_types": grants}
		fake.dcr[name] = client
		fake.oauthApps[key] = map[string]string{"applicationName": name, "bypassClientCredentials": "false",
			"callbackUrl": "regexp=(" + strings.Join(callbacks, "|") + ")", "grantTypes": strings.Join(grants, " "),
			"oauthConsumerKey": key, "oauthConsumerSecret": key + "-secret", "pkceMandatory": "false",
			"pkceSupportPlain": "false", "tokenType": "Default", "username": "admin@carbon.super"}
		fake.sps[name] = fmt.Sprintf(recordedServiceProvider, name, key)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(client)
	}))
	mux.HandleFunc("POST /services/{service}", basic(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)
		action := strings.Trim(strings.TrimPrefix(r.Header.Get("SOAPAction"), "urn:"), `"`)
		fake.requests = append(fake.requests, "POST "+r.URL.Path+" "+action+" "+body)
		w.Header().Set("Content-Type", "text/xml;charset=UTF-8")
		fault := func(message string) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`<?xml version='1.0' encoding='UTF-8'?><soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"><soapenv:Body><soapenv:Fault><faultcode>soapenv:Server</faultcode><faultstring>` + message + `</faultstring></soapenv:Fault></soapenv:Body></soapenv:Envelope>`))
		}
		nilReturn := func(response, namespace string) {
			_, _ = w.Write([]byte(`<?xml version='1.0' encoding='UTF-8'?><soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"><soapenv:Body><ns:` + response + ` xmlns:ns="` + namespace + `"><ns:return xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:nil="true"/></ns:` + response + `></soapenv:Body></soapenv:Envelope>`))
		}
		switch r.PathValue("service") + " " + action {
		case "OAuthAdminService getOAuthApplicationData":
			app, ok := fake.oauthApps[xmlText(body, "consumerKey")]
			if !ok {
				fault("Error while retrieving the app information")
				return
			}
			_, _ = w.Write([]byte(fmt.Sprintf(recordedOAuthApplication, app["applicationName"], app["bypassClientCredentials"],
				app["callbackUrl"], app["grantTypes"], app["oauthConsumerKey"], app["oauthConsumerSecret"],
				app["pkceMandatory"], app["pkceSupportPlain"], app["tokenType"], app["username"])))
		case "OAuthAdminService updateConsumerApplication":
			app, ok := fake.oauthApps[xmlText(body, "oauthConsumerKey")]
			if !ok {
				fault("Error while updating the app information")
				return
			}
			for field := range app {
				if value := xmlText(body, field); value != "" {
					app[field] = value
				}
			}
			nilReturn("updateConsumerApplicationResponse", "http://org.apache.axis2/xsd")
		case "IdentityProviderMgtService getIdPByName":
			fragment, ok := fake.idps[xmlText(body, "idPName")]
			if !ok {
				nilReturn("getIdPByNameResponse", "http://mgt.idp.carbon.wso2.org")
				return
			}
			_, _ = w.Write([]byte(`<?xml version='1.0' encoding='UTF-8'?><soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"><soapenv:Body><ns:getIdPByNameResponse xmlns:ns="http://mgt.idp.carbon.wso2.org"><ns:return xmlns:m="http://model.common.application.identity.carbon.wso2.org/xsd" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:type="m:IdentityProvider">` +
				fragment + `</ns:return></ns:getIdPByNameResponse></soapenv:Body></soapenv:Envelope>`))
		case "IdentityProviderMgtService addIdP":
			name := xmlText(body, "identityProviderName")
			if _, exists := fake.idps[name]; exists {
				fault("An Identity Provider has already been registered with the name " + name)
				return
			}
			fake.idps[name] = xmlFragment(body, "identityProvider")
			nilReturn("addIdPResponse", "http://mgt.idp.carbon.wso2.org")
		case "IdentityProviderMgtService updateIdP":
			old := xmlText(body, "oldIdPName")
			if _, exists := fake.idps[old]; !exists {
				fault("Identity Provider with name " + old + " does not exist")
				return
			}
			delete(fake.idps, old)
			fake.idps[xmlText(body, "identityProviderName")] = xmlFragment(body, "identityProvider")
			nilReturn("updateIdPResponse", "http://mgt.idp.carbon.wso2.org")
		case "IdentityApplicationManagementService getApplication":
			fragment, ok := fake.sps[xmlText(body, "applicationName")]
			if !ok {
				nilReturn("getApplicationResponse", "http://org.apache.axis2/xsd")
				return
			}
			_, _ = w.Write([]byte(`<?xml version='1.0' encoding='UTF-8'?><soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"><soapenv:Body><ns:getApplicationResponse xmlns:ns="http://org.apache.axis2/xsd"><ns:return xmlns:m="http://model.common.application.identity.carbon.wso2.org/xsd" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:type="m:ServiceProvider">` +
				fragment + `</ns:return></ns:getApplicationResponse></soapenv:Body></soapenv:Envelope>`))
		case "IdentityApplicationManagementService updateApplication":
			name := xmlText(body, "applicationName")
			if _, exists := fake.sps[name]; !exists {
				fault("Application with name " + name + " does not exist")
				return
			}
			fake.sps[name] = xmlFragment(body, "serviceProvider")
			nilReturn("updateApplicationResponse", "http://org.apache.axis2/xsd")
		default:
			fault("unknown operation " + action)
		}
	}))
	// an issuer's discovery document, for key-managers add --well-known
	mux.HandleFunc("GET /issuer/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": fake.server.URL + "/issuer", "jwks_uri": fake.server.URL + "/issuer/oauth2/jwks",
			"token_endpoint":      fake.server.URL + "/issuer/oauth2/token",
			"revocation_endpoint": fake.server.URL + "/issuer/oauth2/revoke",
		})
	})
	fake.server = httptest.NewServer(mux)
	t.Cleanup(fake.server.Close)
	fake.endpoint = fake.server.URL
	// The gateway: one route, and the deployment's answer for any other path.
	gateway := http.NewServeMux()
	gateway.HandleFunc("GET /mockapi/1.0.0/status", func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		record(r)
		if fake.gatewayStatus != 0 {
			http.Error(w, fake.gatewayBody, fake.gatewayStatus)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+fixtureToken {
			http.Error(w, `{"code":"900901"}`, http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	gateway.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":"900906","message":"No matching resource found for given API Request"}`, http.StatusNotFound)
	})
	fake.gateway = httptest.NewServer(gateway)
	t.Cleanup(fake.gateway.Close)
	return fake
}

func (f *fakeAPIM) run(t *testing.T, command []string, arguments ...string) testkit.Outcome {
	t.Helper()
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   command,
		Arguments: arguments,
		Context:   module.Context{Name: "apim-admin", Endpoint: f.endpoint, GatewayEndpoint: f.gatewayEndpoint},
		Access:    &testkit.Access{Token: fixtureToken, ExpiresAt: time.Now().Add(time.Minute)},
	})
	if outcome.Err != nil {
		t.Fatalf("%v: %v", command, outcome.Err)
	}
	return outcome
}

func (f *fakeAPIM) requestsTo(prefix string) []string {
	var matching []string
	for _, request := range f.requests {
		if strings.HasPrefix(request, prefix) {
			matching = append(matching, request)
		}
	}
	return matching
}

func fieldsOf(outcome testkit.Outcome) map[string]string {
	fields := map[string]string{}
	if outcome.Result == nil {
		return fields
	}
	for _, field := range outcome.Result.Fields {
		fields[field.Name] = field.Value
	}
	return fields
}

func scopesAsked(outcome testkit.Outcome) string {
	if len(outcome.AccessRequests) == 0 {
		return ""
	}
	return strings.Join(outcome.AccessRequests[0].Scopes, " ")
}

// xmlText is the text of the first element with that local name, whatever
// its prefix; empty when there is none.
func xmlText(body, name string) string {
	match := regexp.MustCompile(`<(?:[\w.]+:)?` + name + `(?:\s[^>]*)?>([^<]*)</`).FindStringSubmatch(body)
	if match == nil {
		return ""
	}
	return match[1]
}

// xmlFragment is what sits between the opening and closing tags of the
// first element with that local name.
func xmlFragment(body, name string) string {
	match := regexp.MustCompile(`(?s)<(?:[\w.]+:)?` + name + `>(.*)</(?:[\w.]+:)?` + name + `>`).FindStringSubmatch(body)
	if match == nil {
		return ""
	}
	return match[1]
}

// recordedOAuthApplication is what API Manager 4.7.0's OAuthAdminService
// answers getOAuthApplicationData with, as recorded on 2026-09-06.
const recordedOAuthApplication = `<?xml version='1.0' encoding='UTF-8'?><soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"><soapenv:Body><ns:getOAuthApplicationDataResponse xmlns:ns="http://org.apache.axis2/xsd"><ns:return xmlns:ax2389="http://oauth.identity.carbon.wso2.org/xsd" xmlns:ax2393="http://dto.oauth.identity.carbon.wso2.org/xsd" xmlns:ax2390="http://base.identity.carbon.wso2.org/xsd" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:type="ax2393:OAuthConsumerAppDTO"><ax2393:OAuthVersion>OAuth-2.0</ax2393:OAuthVersion><ax2393:applicationAccessTokenExpiryTime>3600</ax2393:applicationAccessTokenExpiryTime><ax2393:applicationName>%s</ax2393:applicationName><ax2393:backChannelLogoutUrl xsi:nil="true"/><ax2393:bypassClientCredentials>%s</ax2393:bypassClientCredentials><ax2393:callbackUrl>%s</ax2393:callbackUrl><ax2393:frontchannelLogoutUrl xsi:nil="true"/><ax2393:grantTypes>%s</ax2393:grantTypes><ax2393:idTokenEncryptionAlgorithm>null</ax2393:idTokenEncryptionAlgorithm><ax2393:idTokenEncryptionEnabled>false</ax2393:idTokenEncryptionEnabled><ax2393:idTokenEncryptionMethod>null</ax2393:idTokenEncryptionMethod><ax2393:idTokenExpiryTime>3600</ax2393:idTokenExpiryTime><ax2393:oauthConsumerKey>%s</ax2393:oauthConsumerKey><ax2393:oauthConsumerSecret>%s</ax2393:oauthConsumerSecret><ax2393:pkceMandatory>%s</ax2393:pkceMandatory><ax2393:pkceSupportPlain>%s</ax2393:pkceSupportPlain><ax2393:refreshTokenExpiryTime>86400</ax2393:refreshTokenExpiryTime><ax2393:renewRefreshTokenEnabled xsi:nil="true"/><ax2393:requestObjectSignatureValidationEnabled>false</ax2393:requestObjectSignatureValidationEnabled><ax2393:secretDescription xsi:nil="true"/><ax2393:secretExpiryTime xsi:nil="true"/><ax2393:state>ACTIVE</ax2393:state><ax2393:tokenBindingType xsi:nil="true"/><ax2393:tokenBindingValidationEnabled>false</ax2393:tokenBindingValidationEnabled><ax2393:tokenRevocationWithIDPSessionTerminationEnabled>false</ax2393:tokenRevocationWithIDPSessionTerminationEnabled><ax2393:tokenType>%s</ax2393:tokenType><ax2393:userAccessTokenExpiryTime>3600</ax2393:userAccessTokenExpiryTime><ax2393:username>%s</ax2393:username></ns:return></ns:getOAuthApplicationDataResponse></soapenv:Body></soapenv:Envelope>`

// recordedServiceProvider is the service provider dynamic client
// registration leaves behind, as IdentityApplicationManagementService
// getApplication returned it on 2026-09-06: local authentication, consent
// asked. The name and the consumer key are filled in.
const recordedServiceProvider = `<m:applicationID>97</m:applicationID><m:applicationName>%[1]s</m:applicationName><m:applicationResourceId>7b0c4a2e-0000-4000-8000-000000000097</m:applicationResourceId><m:certificateContent xsi:nil="true"/><m:claimConfig xsi:type="m:ClaimConfig"><m:alwaysSendMappedLocalSubjectId>false</m:alwaysSendMappedLocalSubjectId><m:localClaimDialect>true</m:localClaimDialect><m:roleClaimURI xsi:nil="true"/><m:userClaimURI xsi:nil="true"/></m:claimConfig><m:description>Service Provider for application %[1]s</m:description><m:discoverable>false</m:discoverable><m:inboundAuthenticationConfig xsi:type="m:InboundAuthenticationConfig"><m:inboundAuthenticationRequestConfigs xsi:type="m:InboundAuthenticationRequestConfig"><m:friendlyName xsi:nil="true"/><m:inboundAuthKey>%[2]s</m:inboundAuthKey><m:inboundAuthType>oauth2</m:inboundAuthType><m:inboundConfigType>standardAPP</m:inboundConfigType><m:inboundConfiguration xsi:nil="true"/><m:properties xsi:type="m:Property"><m:advanced>false</m:advanced><m:confidential>false</m:confidential><m:defaultValue xsi:nil="true"/><m:description xsi:nil="true"/><m:displayName xsi:nil="true"/><m:displayOrder>0</m:displayOrder><m:groupId>0</m:groupId><m:name>oauthConsumerSecret</m:name><m:required>false</m:required><m:type xsi:nil="true"/><m:value>%[2]s-secret</m:value></m:properties></m:inboundAuthenticationRequestConfigs></m:inboundAuthenticationConfig><m:inboundProvisioningConfig xsi:type="m:InboundProvisioningConfig"><m:dumbMode>false</m:dumbMode><m:provisioningEnabled>false</m:provisioningEnabled><m:provisioningUserStore xsi:nil="true"/></m:inboundProvisioningConfig><m:jwksUri></m:jwksUri><m:localAndOutBoundAuthenticationConfig xsi:type="m:LocalAndOutboundAuthenticationConfig"><m:alwaysSendBackAuthenticatedListOfIdPs>false</m:alwaysSendBackAuthenticatedListOfIdPs><m:authenticationScriptConfig xsi:nil="true"/><m:authenticationStepForAttributes xsi:nil="true"/><m:authenticationStepForSubject xsi:nil="true"/><m:authenticationType>default</m:authenticationType><m:enableAuthorization>false</m:enableAuthorization><m:skipConsent>false</m:skipConsent><m:skipLogoutConsent>false</m:skipLogoutConsent><m:subjectClaimUri xsi:nil="true"/><m:useTenantDomainInLocalSubjectIdentifier>false</m:useTenantDomainInLocalSubjectIdentifier><m:useUserstoreDomainInLocalSubjectIdentifier>false</m:useUserstoreDomainInLocalSubjectIdentifier><m:useUserstoreDomainInRoles>true</m:useUserstoreDomainInRoles></m:localAndOutBoundAuthenticationConfig><m:managementApp>false</m:managementApp><m:outboundProvisioningConfig xsi:type="m:OutboundProvisioningConfig"><m:provisionByRoleList xsi:nil="true"/></m:outboundProvisioningConfig><m:owner xsi:type="m:User"><m:loggableUserId>admin@carbon.super</m:loggableUserId><m:tenantDomain>carbon.super</m:tenantDomain><m:userName>admin</m:userName><m:userStoreDomain>PRIMARY</m:userStoreDomain></m:owner><m:permissionAndRoleConfig xsi:type="m:PermissionsAndRoleConfig"/><m:saasApp>false</m:saasApp><m:spProperties xsi:type="m:ServiceProviderProperty"><m:displayName>Is Management Application</m:displayName><m:name>isManagementApp</m:name><m:value>false</m:value></m:spProperties><m:spProperties xsi:type="m:ServiceProviderProperty"><m:displayName>Skip Consent</m:displayName><m:name>skipConsent</m:name><m:value>false</m:value></m:spProperties><m:templateId></m:templateId><m:tenantDomain>carbon.super</m:tenantDomain>`

// recordedIdentityProvider is an identity provider registered before
// bootstrap learned federation: JWT trust through jwksUri and the claim and
// role mappings, no federated authenticator. It is what IdentityProviderMgtService
// getIdPByName returned on 2026-09-06, the name filled in.
const recordedIdentityProvider = `<m:alias>wso2-cli</m:alias><m:certificate xsi:nil="true"/><m:claimConfig xsi:type="m:ClaimConfig"><m:alwaysSendMappedLocalSubjectId>false</m:alwaysSendMappedLocalSubjectId><m:claimMappings xsi:type="m:ClaimMapping"><m:defaultValue xsi:nil="true"/><m:localClaim xsi:type="m:Claim"><m:claimId>0</m:claimId><m:claimUri>http://wso2.org/claims/role</m:claimUri></m:localClaim><m:mandatory>false</m:mandatory><m:remoteClaim xsi:type="m:Claim"><m:claimId>0</m:claimId><m:claimUri>groups</m:claimUri></m:remoteClaim><m:requested>false</m:requested></m:claimMappings><m:idpClaims xsi:type="m:Claim"><m:claimId>0</m:claimId><m:claimUri>groups</m:claimUri></m:idpClaims><m:localClaimDialect>false</m:localClaimDialect><m:roleClaimURI>groups</m:roleClaimURI><m:userClaimURI>sub</m:userClaimURI></m:claimConfig><m:displayName>%[1]s</m:displayName><m:enable>true</m:enable><m:federationHub>false</m:federationHub><m:homeRealmId xsi:nil="true"/><m:id>5</m:id><m:identityProviderDescription>ThunderID trusted for the CLI single login</m:identityProviderDescription><m:identityProviderName>%[1]s</m:identityProviderName><m:idpProperties xsi:type="m:IdentityProviderProperty"><m:displayName xsi:nil="true"/><m:name>jwksUri</m:name><m:value>http://host.docker.internal:8492/oauth2/jwks</m:value></m:idpProperties><m:idpProperties xsi:type="m:IdentityProviderProperty"><m:displayName xsi:nil="true"/><m:name>idpIssuerName</m:name><m:value>http://localhost:8492</m:value></m:idpProperties><m:justInTimeProvisioningConfig xsi:type="m:JustInTimeProvisioningConfig"><m:dumbMode>false</m:dumbMode><m:modifyUserNameAllowed>false</m:modifyUserNameAllowed><m:passwordProvisioningEnabled>false</m:passwordProvisioningEnabled><m:promptConsent>false</m:promptConsent><m:provisioningEnabled>false</m:provisioningEnabled><m:provisioningUserStore xsi:nil="true"/><m:userStoreClaimUri xsi:nil="true"/></m:justInTimeProvisioningConfig><m:permissionAndRoleConfig xsi:type="m:PermissionsAndRoleConfig"><m:idpRoles>Administrators</m:idpRoles><m:roleMappings xsi:type="m:RoleMapping"><m:localRole xsi:type="m:LocalRole"><m:localRoleName>admin</m:localRoleName><m:userStoreId xsi:nil="true"/></m:localRole><m:remoteRole>Administrators</m:remoteRole></m:roleMappings></m:permissionAndRoleConfig><m:primary>false</m:primary><m:provisioningRole xsi:nil="true"/>`
