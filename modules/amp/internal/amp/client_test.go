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

package amp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetSendsTheBearerUnderTheAPIPathAndReadsTheAnswer(t *testing.T) {
	var seen struct{ method, path, auth, accept string }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.method, seen.path, seen.auth = r.Method, r.URL.RequestURI(), r.Header.Get("Authorization")
		seen.accept = r.Header.Get("Accept")
		_, _ = w.Write([]byte(`{"total":1,"projects":[{"name":"payments"}]}`))
	}))
	t.Cleanup(server.Close)

	var listed struct {
		Total    int                 `json:"total"`
		Projects []map[string]string `json:"projects"`
	}
	err := New(server.URL+"/", "tok").Get(context.Background(), "/orgs/acme/projects?limit=5", &listed)
	if err != nil {
		t.Fatal(err)
	}
	if seen.method != "GET" || seen.path != "/api/v1/orgs/acme/projects?limit=5" ||
		seen.auth != "Bearer tok" || seen.accept != "application/json" {
		t.Errorf("request = %+v", seen)
	}
	if listed.Total != 1 || listed.Projects[0]["name"] != "payments" {
		t.Errorf("listed = %+v", listed)
	}
}

func TestErrorsBecomeTheThreeProblems(t *testing.T) {
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"missing scope amp:project:read"}`))
	}))
	t.Cleanup(refusing.Close)
	garbled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html>`))
	}))
	t.Cleanup(garbled.Close)

	var into map[string]any
	err := New(refusing.URL, "").Get(context.Background(), "/orgs/acme/projects", &into)
	if p := Problem(err, "the project listing"); p.Code != "amp.refused" ||
		!strings.Contains(p.Message, "403") || !strings.Contains(p.Message, "missing scope") {
		t.Errorf("refusal problem = %+v", p)
	}
	err = New(garbled.URL, "").Get(context.Background(), "/orgs/acme/projects", &into)
	if p := Problem(err, "the project listing"); p.Code != "amp.unreadable" {
		t.Errorf("unreadable problem = %+v", p)
	}
	err = New("http://127.0.0.1:1", "").Get(context.Background(), "/orgs/acme/projects", &into)
	if p := Problem(err, "the project listing"); p.Code != "amp.unavailable" {
		t.Errorf("unavailable problem = %+v", p)
	}
}

func TestAnUntrustedCertificateIsNamedWithItsHost(t *testing.T) {
	selfSigned := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(selfSigned.Close)
	var into map[string]any
	err := New(selfSigned.URL, "").Get(context.Background(), "/orgs/acme/projects", &into)
	p := Problem(err, "the project listing")
	host := strings.TrimPrefix(selfSigned.URL, "https://")
	if p.Code != "amp.certificate_untrusted" || !strings.Contains(p.Message, host) ||
		!strings.Contains(p.Recovery, "openssl s_client -connect "+host) ||
		!strings.Contains(p.Recovery, "WSO2_CA_FILE") {
		t.Errorf("certificate problem = %+v", p)
	}
}
