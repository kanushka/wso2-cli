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

package modules

import (
	"os"
	"runtime"
	"slices"
	"strings"
)

// SanitizedEnvironment builds a module process's environment from nothing.
//
// The shell decides what a module may see rather than filtering what it must
// not: a deny list would leak every variable nobody thought of. Three things
// are added back: the entries an operating system needs to start a process
// at all, the certificate file the shell itself trusts (CAFileEnvVar), and
// the variables a user exported under this module's own prefix, WSO2_IAM_ for
// the iam module. The prefix is how a secret a command needs, such as an
// administrator password for a one-time bootstrap, reaches a module that
// cannot prompt: the user names the module in the variable, and no other
// module sees it. Empty values are not passed; an unset variable and an
// empty one mean the same thing to a module.
//
// It lives here rather than beside either caller because there are two, and a
// module process is launched twice in a module's life — once at install, to ask
// what commands it serves, and again for every invocation. Two copies of this
// rule would be two places for it to drift, and the one that drifted would be
// the one nobody was looking at.
//
// withheld names variables that carry the shell's own credentials, read from
// the context document: an identity's client secret variable and a product's
// client id and secret variables. They may well sit under the namespace
// prefix, since a bootstrap suggests WSO2_APIM_CLIENT_SECRET for the apim
// identity, and a module must never see the credential the shell mints its
// access from, so those are withheld whatever their name.
func SanitizedEnvironment(namespace string, withheld ...string) []string {
	names := []string{CAFileEnvVar}
	if runtime.GOOS == "windows" {
		// Windows cannot reliably start a process without these, and neither
		// carries user or credential data.
		names = append(names, "SYSTEMROOT", "SYSTEMDRIVE", "WINDIR")
	}
	environment := []string{}
	for _, name := range names {
		if value, present := os.LookupEnv(name); present && value != "" {
			environment = append(environment, name+"="+value)
		}
	}
	prefix := NamespaceEnvPrefix(namespace)
	for _, entry := range os.Environ() {
		name, value, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, prefix) && value != "" && !slices.Contains(withheld, name) {
			environment = append(environment, entry)
		}
	}
	return environment
}

// NamespaceEnvPrefix is the prefix of the variables a module of this namespace
// is handed: WSO2_IAM_ for iam.
func NamespaceEnvPrefix(namespace string) string {
	return "WSO2_" + strings.ToUpper(namespace) + "_"
}

// CAFileEnvVar is the one WSO2_ variable a module process is handed. It names a
// PEM file of certificates to trust beside the system roots, which is public
// material rather than a credential, and a module calling a self-hosted product
// over TLS needs the same trust the shell used to reach the issuer. The shell
// reads it for its own requests in internal/app.
const CAFileEnvVar = "WSO2_CA_FILE"
