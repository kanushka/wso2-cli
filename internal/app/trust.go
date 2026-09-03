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
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"

	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// CAFileEnvVar names a PEM file whose certificates the shell trusts for TLS,
// in addition to the ones the operating system trusts.
//
// Every self-hosted WSO2 product ships a self-signed certificate, and until it
// is trusted the shell cannot read the issuer's discovery document, so login
// fails before a browser opens. The operating system's trust store is the
// ordinary answer, and on a locked-down machine it is not available. Go on
// macOS also ignores SSL_CERT_FILE, so this variable is the only route that
// works the same on every platform. It is read once per invocation. A module
// process is launched with an empty environment and this one variable, so it
// can apply the same trust to its own product calls; see
// modules.SanitizedEnvironment.
const CAFileEnvVar = modules.CAFileEnvVar

// installTrust widens the process-wide HTTP transport's trust to the
// certificates CAFileEnvVar names. It does nothing when the variable is unset,
// and it never narrows trust: the system roots stay trusted beside the file.
func installTrust() error {
	path := os.Getenv(CAFileEnvVar)
	if path == "" {
		return nil
	}
	pem, err := os.ReadFile(path)
	if err != nil {
		return problem.New(problem.CategoryUsage, "shell.ca_file_unreadable",
			fmt.Sprintf("the certificate file %s names, %q, could not be read", CAFileEnvVar, path)).
			WithRecovery(fmt.Sprintf("Point %s at a readable PEM file holding the deployment's certificate, or unset it.", CAFileEnvVar))
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return problem.New(problem.CategoryUsage, "shell.ca_file_unreadable",
			fmt.Sprintf("the certificate file %s names, %q, holds no PEM certificate", CAFileEnvVar, path)).
			WithRecovery("Export the deployment's certificate in PEM form, for example with openssl x509 -outform pem, and name that file.")
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil
	}
	widened := transport.Clone()
	if widened.TLSClientConfig == nil {
		widened.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	widened.TLSClientConfig.RootCAs = pool
	http.DefaultTransport = widened
	return nil
}
