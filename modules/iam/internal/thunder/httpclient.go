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
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
)

// CAFileEnvVar is the one variable the shell hands every module: a PEM file
// of certificates to trust beside the system roots.
const CAFileEnvVar = "WSO2_CA_FILE"

// HTTPClient is the client every call goes through. It honours CAFileEnvVar
// the way the shell does, because a module is a separate process and the
// shell's trust does not reach it.
func HTTPClient() *http.Client {
	path := os.Getenv(CAFileEnvVar)
	if path == "" {
		return http.DefaultClient
	}
	pem, err := os.ReadFile(path)
	if err != nil {
		return http.DefaultClient
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	pool.AppendCertsFromPEM(pem)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	return &http.Client{Transport: transport}
}
