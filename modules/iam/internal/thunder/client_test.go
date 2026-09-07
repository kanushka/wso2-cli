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

package thunder

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPostSendsJSONWithTheBearerAndReadsTheAnswer(t *testing.T) {
	var seen struct{ method, path, auth, contentType, body string }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buffer := make([]byte, 1024)
		n, _ := r.Body.Read(buffer)
		seen.method, seen.path, seen.auth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		seen.contentType, seen.body = r.Header.Get("Content-Type"), string(buffer[:n])
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"r-1","name":"Mock API","identifier":"http://x/mock"}`))
	}))
	t.Cleanup(server.Close)

	var created ResourceServer
	err := New(server.URL+"/", "tok").Post(context.Background(), "/resource-servers",
		ResourceServer{Name: "Mock API", Identifier: "http://x/mock"}, &created)
	if err != nil {
		t.Fatal(err)
	}
	if seen.method != "POST" || seen.path != "/resource-servers" || seen.auth != "Bearer tok" ||
		seen.contentType != "application/json" || !strings.Contains(seen.body, `"identifier":"http://x/mock"`) {
		t.Errorf("request = %+v", seen)
	}
	if created.ID != "r-1" {
		t.Errorf("created = %+v", created)
	}
}

func TestErrorsBecomeTheThreeProblems(t *testing.T) {
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"already exists"}`))
	}))
	t.Cleanup(refusing.Close)
	garbled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html>`))
	}))
	t.Cleanup(garbled.Close)

	var into map[string]any
	err := New(refusing.URL, "").Get(context.Background(), "/roles", &into)
	if p := Problem(err, "the role creation"); p.Code != "iam.refused" ||
		!strings.Contains(p.Message, "409") || !strings.Contains(p.Message, "already exists") {
		t.Errorf("refusal problem = %+v", p)
	}
	err = New(garbled.URL, "").Get(context.Background(), "/roles", &into)
	if p := Problem(err, "the role listing"); p.Code != "iam.unreadable" {
		t.Errorf("unreadable problem = %+v", p)
	}
	err = New("http://127.0.0.1:1", "").Get(context.Background(), "/roles", &into)
	if p := Problem(err, "the role listing"); p.Code != "iam.unavailable" {
		t.Errorf("unavailable problem = %+v", p)
	}
}

func TestAnUntrustedCertificateIsNamedWithItsHost(t *testing.T) {
	selfSigned := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(selfSigned.Close)
	var into map[string]any
	err := New(selfSigned.URL, "").Get(context.Background(), "/roles", &into)
	p := Problem(err, "the role listing")
	host := strings.TrimPrefix(selfSigned.URL, "https://")
	if p.Code != "iam.certificate_untrusted" || !strings.Contains(p.Message, host) ||
		!strings.Contains(p.Recovery, "openssl s_client -connect "+host) ||
		!strings.Contains(p.Recovery, "WSO2_CA_FILE") {
		t.Errorf("certificate problem = %+v", p)
	}
}

func TestApplicationReportsItsOAuthClientID(t *testing.T) {
	app := Application{ClientID: "flat", InboundAuth: []InboundAuth{{Type: "oauth2", Config: OAuthConfig{ClientID: "nested"}}}}
	if app.OAuthClientID() != "nested" {
		t.Errorf("nested client id not preferred: %q", app.OAuthClientID())
	}
	if (Application{ClientID: "flat"}).OAuthClientID() != "flat" {
		t.Error("flat client id not used")
	}
}
