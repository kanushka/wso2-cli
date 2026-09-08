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
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"

	"github.com/wso2/wso2-cli/sdk/problem"
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

// UntrustedCertificate is a call that failed because the deployment's TLS
// certificate is not trusted by this machine. It carries the host and port
// the certificate came from, which is what the recovery needs and what a
// plain x509 error does not say.
type UntrustedCertificate struct {
	Host string
	Err  error
}

func (u *UntrustedCertificate) Error() string {
	return fmt.Sprintf("the certificate %s presents is not trusted: %v", u.Host, u.Err)
}

func (u *UntrustedCertificate) Unwrap() error { return u.Err }

// classifyTransport wraps a transport error that a certificate caused, so
// Problem can tell it from a deployment that is down. Every other error is
// returned as it was.
func classifyTransport(base string, err error) error {
	if err == nil || !untrusted(err) {
		return err
	}
	host := base
	if parsed, parseErr := url.Parse(base); parseErr == nil && parsed.Host != "" {
		host = parsed.Host
	}
	return &UntrustedCertificate{Host: host, Err: err}
}

func untrusted(err error) bool {
	var verification *tls.CertificateVerificationError
	var unknown x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	return errors.As(err, &verification) || errors.As(err, &unknown) ||
		errors.As(err, &hostname) || errors.As(err, &invalid)
}

// certificateProblem is the refusal for an untrusted certificate. It says
// the same thing the shell says for an identity provider, with the same two
// commands, because the person fixes both the same way.
func certificateProblem(host string) problem.Problem {
	return problem.New(problem.CategoryProductService, "amp.certificate_untrusted",
		fmt.Sprintf("the certificate Agent Manager at %s presents is not trusted by this machine", host)).
		WithRecovery(fmt.Sprintf("Export the certificate with openssl s_client -connect %s -showcerts </dev/null 2>/dev/null "+
			"| awk '/BEGIN CERT/,/END CERT/' > %s.pem and then export WSO2_CA_FILE=$PWD/%s.pem in the shell "+
			"that runs wso2, or add the certificate to the operating system's trust store instead.",
			host, fileStem(host), fileStem(host)))
}

// fileStem turns host:port into a name a file can carry.
func fileStem(host string) string {
	out := []rune(host)
	for i, r := range out {
		if r == ':' || r == '/' {
			out[i] = '-'
		}
	}
	return string(out)
}
