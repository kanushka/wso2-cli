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

// Package issuertrust tells a certificate this machine does not trust apart
// from every other way an issuer request can fail, and owns the one refusal
// the shell gives for it.
//
// Every self-hosted WSO2 product serves a self-signed certificate on a fresh
// install, and the first request the shell makes against it, discovery, is
// where the handshake fails. Reported as an unreadable configuration, that
// failure sends the user to check an issuer URL that is right and a network
// that works. It lives in its own package because three places see the
// failure — the broker's discovery, the login flows' discovery, and wso2
// doctor's issuer check — and none of them may import the others.
package issuertrust

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/wso2/wso2-cli/sdk/problem"
)

// Code is the problem code an untrusted issuer certificate is refused with.
const Code = "auth.certificate_untrusted"

// CAFileEnvVar is the variable the recovery tells the user to export. It is
// spelled here rather than imported so this package stays free of the
// module-launching code that also declares it.
const CAFileEnvVar = "WSO2_CA_FILE"

// Untrusted reports whether err is a failed TLS verification of the peer's
// certificate: one signed by an authority this machine does not know, one
// whose name does not match the host, or one that is invalid on its own
// terms. A refused connection, a timeout, or a bad answer is not one.
//
// crypto/tls wraps every verification failure in CertificateVerificationError
// and net/http wraps that in *url.Error, so errors.As reaches the typed cause
// through both. The bare x509 errors are matched as well, for a caller that
// verifies a chain itself rather than through a handshake.
func Untrusted(err error) bool {
	if err == nil {
		return false
	}
	var verification *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	return errors.As(err, &verification) ||
		errors.As(err, &unknownAuthority) ||
		errors.As(err, &hostname) ||
		errors.As(err, &invalid)
}

// Problem is the refusal for an issuer whose certificate this machine does
// not trust. It names the host the certificate came from and gives the two
// commands that trust it for the shell alone, with the real host and port
// filled in, so the user has nothing to look up.
func Problem(issuer string) problem.Problem {
	host, port := endpoint(issuer)
	connect := host + ":" + port
	file := host + "-" + port + ".pem"
	return problem.New(problem.CategoryAuthPolicy, Code,
		fmt.Sprintf("the certificate the identity provider at %s presents is not trusted by this machine",
			connect)).
		WithRecovery(fmt.Sprintf("Export the certificate with "+
			"openssl s_client -connect %s -showcerts </dev/null 2>/dev/null | awk '/BEGIN CERT/,/END CERT/' > %s "+
			"and then export %s=$PWD/%s in the shell that runs wso2, or add the certificate to the "+
			"operating system's trust store instead.",
			connect, file, CAFileEnvVar, file))
}

// endpoint is the host and port the issuer is dialled on. An issuer that
// states no port is dialled on its scheme's default, and one that cannot be
// parsed is named as given, so the commands still point somewhere the user
// can recognize.
func endpoint(issuer string) (host, port string) {
	parsed, err := url.Parse(issuer)
	if err != nil || parsed.Hostname() == "" {
		return strings.TrimSpace(issuer), "443"
	}
	port = parsed.Port()
	if port == "" {
		port = "443"
		if parsed.Scheme == "http" {
			port = "80"
		}
	}
	return parsed.Hostname(), port
}
