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
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/wso2/wso2-cli/sdk/problem"
)

// keepDefaultTransport restores the process-wide transport after a test that
// widens it, so no other test inherits trust it did not ask for.
func keepDefaultTransport(t *testing.T) {
	t.Helper()
	before := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = before })
}

func writeServerCertificate(t *testing.T, server *httptest.Server) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "deployment.pem")
	block := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(path, block, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTheCAFileMakesASelfSignedDeploymentReachable(t *testing.T) {
	keepDefaultTransport(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	// Without the file the deployment is refused, which is the situation a
	// user of a self-hosted product starts in.
	t.Setenv(CAFileEnvVar, "")
	if err := installTrust(); err != nil {
		t.Fatalf("an unset variable was refused: %v", err)
	}
	if _, err := http.Get(server.URL); err == nil {
		t.Fatal("a self-signed deployment was trusted before any certificate was named")
	}

	t.Setenv(CAFileEnvVar, writeServerCertificate(t, server))
	if err := installTrust(); err != nil {
		t.Fatalf("a readable certificate file was refused: %v", err)
	}
	response, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("the named certificate was not trusted: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusNoContent)
	}
}

func TestAnUnreadableCAFileIsAUsageProblem(t *testing.T) {
	keepDefaultTransport(t)
	for name, path := range map[string]string{
		"missing": filepath.Join(t.TempDir(), "absent.pem"),
		"not a certificate": func() string {
			path := filepath.Join(t.TempDir(), "notes.txt")
			if err := os.WriteFile(path, []byte("not a certificate"), 0o600); err != nil {
				t.Fatal(err)
			}
			return path
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(CAFileEnvVar, path)
			err := installTrust()
			var typed problem.Problem
			if !errors.As(err, &typed) {
				t.Fatalf("err = %v, want a problem", err)
			}
			if typed.Code != "shell.ca_file_unreadable" {
				t.Errorf("code = %q, want shell.ca_file_unreadable", typed.Code)
			}
			if typed.Category != problem.CategoryUsage {
				t.Errorf("category = %q, want usage", typed.Category)
			}
		})
	}
}
